// Copyright © 2019-2022 Dell Inc. or its subsidiaries. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//      http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/gofsutil"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Variables set only for unit testing.
var unitTestEmulateBlockDevice bool

// Variables populdated from the environment
var mountAllowRWOMultiPodAccess bool

// Device is a struct for holding details about a block device
type Device struct {
	FullPath string
	Name     string
	RealDev  string
}

// GetDevice returns a Device struct with info about the given device, or
// an error if it doesn't exist or is not a block device
func GetDevice(path string) (*Device, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}

	// eval any symlinks and make sure it points to a device
	d, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}

	// TODO does EvalSymlinks throw error if link is to non-
	// existent file? assuming so by masking error below
	ds, _ := os.Stat(d)
	dm := ds.Mode()
	if unitTestEmulateBlockDevice {
		// For unit testing only, emulate a block device on windows
		dm = dm | os.ModeDevice
	}
	if dm&os.ModeDevice == 0 {
		return nil, fmt.Errorf(
			"%s is not a block device", path)
	}

	return &Device{
		Name:     fi.Name(),
		FullPath: replaceBackslashWithSlash(path),
		RealDev:  replaceBackslashWithSlash(d),
	}, nil
}

// publishVolume uses the parameters in req to bindmount the underlying block
// device to the requested target path. A private mount is performed first
// within the given privDir directory.
//
// publishVolume handles both Mount and Block access types
func publishVolume(
	req *csi.NodePublishVolumeRequest,
	privDir, device string, reqID string,
) error {
	id := req.GetVolumeId()

	target := req.GetTargetPath()
	if target == "" {
		return status.Error(codes.InvalidArgument,
			"target path required")
	}

	ro := req.GetReadonly()

	volCap := req.GetVolumeCapability()
	if volCap == nil {
		return status.Error(codes.InvalidArgument,
			"volume capability required")
	}

	// make sure device is valid
	sysDevice, err := GetDevice(device)
	if err != nil {
		return status.Errorf(codes.Internal,
			"error getting block device for volume: %s, err: %s",
			id, err.Error())
	}

	isBlock, mntVol, accMode, multiAccessFlag, err := validateVolumeCapability(volCap, ro)
	if err != nil {
		return err
	}

	// Make sure target is created. The spec says the driver is responsible
	// for creating the target, but Kubernetes generallly creates the target.
	privTgt := getPrivateMountPoint(privDir, id)
	err = createTarget(target, isBlock)
	if err != nil {
		// Unmount and remove the private directory for the retry so clean start next time.
		// K8S probably removed part of the path.
		PrivtgtErr := cleanupPrivateTarget(sysDevice, reqID, privTgt)
		if PrivtgtErr != nil {
			Log.Infof("Error removing private target or directory: %s", privTgt)
		}
		return status.Error(codes.FailedPrecondition, fmt.Sprintf("Could not create %s: %s", target, err.Error()))
	}

	// make sure privDir exists and is a directory
	if _, err := mkdir(privDir); err != nil {
		return err
	}

	// Handle block as a short cut
	if isBlock {
		// BLOCK only
		mntFlags := mntVol.GetMountFlags()
		err = mountBlock(sysDevice, target, mntFlags, singleAccessMode(accMode))
		return err
	}

	// check that target is right type for vol type
	// Path to mount device to

	f := logrus.Fields{
		"id":           id,
		"volumePath":   sysDevice.FullPath,
		"device":       sysDevice.RealDev,
		"CSIRequestID": reqID,
		"target":       target,
		"privateMount": privTgt,
	}
	Log.WithFields(f).Debugf("fields")

	ctx := context.WithValue(context.Background(), gofsutil.ContextKey("RequestID"), reqID)

	// Check if device is already mounted
	devMnts, err := getDevMounts(sysDevice)
	if err != nil {
		return status.Errorf(codes.Internal,
			"could not reliably determine existing mount status: %s",
			err.Error())
	}

	if len(devMnts) == 0 {
		// Device isn't mounted anywhere, do the private mount
		Log.WithFields(f).Printf("attempting mount to private area")

		// Make sure private mount point exists
		created, err := mkdir(privTgt)
		if err != nil {
			return status.Errorf(codes.Internal,
				"Unable to create private mount point: %s",
				err.Error())
		}
		alreadyMounted := false
		if !created {
			Log.WithFields(f).Printf("directory for private mount target already exists")

			// The place where our device is supposed to be mounted
			// already exists, but we also know that our device is not mounted anywhere
			// Either something didn't clean up correctly, or something else is mounted
			// If the private mount is not in use, it's okay to re-use it. But make sure
			// it's not in use first

			mnts, err := gofsutil.GetMounts(ctx)
			if err != nil {
				return status.Errorf(codes.Internal,
					"could not reliably determine existing mount status: %s",
					err.Error())
			}
			if len(mnts) == 0 {
				return status.Errorf(codes.Unavailable, "volume %s not published to node", id)
			}
			for _, m := range mnts {
				if m.Path == privTgt {
					Log.Debug(fmt.Sprintf("MOUNT: %#v", m))
					resolvedMountDevice := evalSymlinks(m.Device)
					if resolvedMountDevice != sysDevice.RealDev {
						Log.WithFields(f).WithField("mountedDevice", m.Device).Error(
							"mount point already in use by device")
						return status.Error(codes.Internal,
							"Mount point already in use by device")
					}
					alreadyMounted = true
				}
			}
		}

		if !alreadyMounted {
			fs := mntVol.GetFsType()
			mntFlags := mntVol.GetMountFlags()
			if fs == "xfs" {
				mntFlags = append(mntFlags, "nouuid")
			}
			if ro {
				mntFlags = append(mntFlags, "ro")
			}
			fsFormatOption := req.GetVolumeContext()[KeyMkfsFormatOption]
			if err := handlePrivFSMount(
				ctx, accMode, sysDevice, mntFlags, fs, privTgt, fsFormatOption); err != nil {
				// K8S may have removed the desired mount point. Clean up the private target.
				PrivtgtErr := cleanupPrivateTarget(sysDevice, reqID, privTgt)
				if PrivtgtErr != nil {
					Log.Infof("Error removing private target or directory: %s", privTgt)
				}
				return err
			}
		}

	} else {
		// Device is already mounted. Need to ensure that it is already
		// mounted to the expected private mount, with correct rw/ro perms
		mounted := false
		for _, m := range devMnts {
			if m.Path == target {
				Log.Printf("mount %#v already mounted to requested target %s", m, target)
			} else if m.Path == privTgt {
				Log.WithFields(f).Printf("mount Path %s Source %s Device %s Opts %v", m.Path, m.Source, m.Device, m.Opts)
				mounted = true
				rwo := multiAccessFlag
				if ro {
					rwo = "ro"
				}
				if rwo == "" || contains(m.Opts, rwo) {
					Log.WithFields(f).Printf("private mount already in place")
				} else {
					Log.WithFields(f).Printf("mount %#v rwo %s", m, rwo)
					return status.Error(codes.InvalidArgument,
						"Access mode conflicts with existing mounts")
				}
			} else if singleAccessMode(accMode) {
				return status.Error(codes.FailedPrecondition,
					fmt.Sprintf("Access mode conflicts with existing mounts for privTgt %s", privTgt))
			}
		}
		if !mounted {
			return status.Error(codes.Internal,
				fmt.Sprintf("Device already in use and mounted elsewhere for privTgt %s", privTgt))
		}
	}

	// Private mount in place, now bind mount to target path
	targetMnts, err := getPathMounts(target)
	if err != nil {
		return status.Errorf(codes.Internal,
			"could not reliably determine existing mount status: %s",
			err.Error())
	}

	// If mounts already existed for this device, check if mount to
	// target path was already there
	if len(targetMnts) > 0 {
		for _, m := range targetMnts {
			if m.Path == target {
				// volume already published to target
				// if mount options look good, do nothing
				rwo := multiAccessFlag
				if ro {
					rwo = "ro"
				}
				if rwo != "" && !contains(m.Opts, rwo) {
					Log.WithFields(f).Printf("mount %#v rwo %s\n", m, rwo)
					return status.Error(codes.Internal,
						"volume previously published with different options")
				}
				// Existing mount satisfies request
				Log.WithFields(f).Debug("volume already published to target")
				return nil
			}
		}
	}

	// Recheck that target is created. k8s has this awful habit of deleting the target if it times out the request.
	// This will narrow the window.
	err = createTarget(target, isBlock)
	if err != nil {
		// Unmount and remove the private directory for the retry so clean start next time.
		// K8S probably removed part of the path.
		PrivtgtErr := cleanupPrivateTarget(sysDevice, reqID, privTgt)
		if PrivtgtErr != nil {
			Log.Infof("Error removing private target or directory: %s", privTgt)
		}
		return status.Error(codes.FailedPrecondition, fmt.Sprintf("Could not create %s: %s", target, err.Error()))
	}

	var mntFlags []string
	mntFlags = mntVol.GetMountFlags()
	if mntVol.FsType == "xfs" {
		mntFlags = append(mntFlags, "nouuid")
	}
	if ro || accMode.GetMode() == csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY {
		mntFlags = append(mntFlags, "ro")
	}

	if err := gofsutil.BindMount(ctx, privTgt, target, mntFlags...); err != nil {
		// Unmount and remove the private directory for the retry so clean start next time.
		// K8S probably removed part of the path.
		PrivtgtErr := cleanupPrivateTarget(sysDevice, reqID, privTgt)
		if PrivtgtErr != nil {
			Log.Infof("Error removing private target or directory: %s", privTgt)
		}
		return status.Errorf(codes.Internal,
			"error publish volume to target path: %s",
			err.Error())
	}

	return nil
}

// publishNFS mounts the NFS Volume to the targetpath
func publishNFS(ctx context.Context, req *csi.NodePublishVolumeRequest, nfsExportURL string) error {
	volCap := req.GetVolumeCapability()

	if volCap == nil {
		return status.Error(codes.InvalidArgument,
			"Volume Capability is required")
	}

	am := volCap.GetAccessMode()

	if am == nil {
		return status.Error(codes.InvalidArgument,
			"Volume Access Mode is required")
	}

	mountVol := volCap.GetMount()

	if mountVol == nil {
		return status.Error(codes.InvalidArgument, "Invalid access type")
	}

	var mntOptions []string
	mntOptions = mountVol.GetMountFlags()
	Log.Infof("The mountOptions received are: %s", mntOptions)

	target := req.GetTargetPath()
	if target == "" {
		return status.Error(codes.InvalidArgument,
			"Target Path is required")
	}

	// make sure target is created
	_, err := mkdir(target)
	if err != nil {
		return status.Error(codes.FailedPrecondition, fmt.Sprintf("Could not create '%s': '%s'", target, err.Error()))
	}
	roFlag := req.GetReadonly()
	rwOption := "rw"
	if roFlag {
		rwOption = "ro"
	}

	mntOptions = append(mntOptions, rwOption)

	fields := map[string]interface{}{
		"ID":         req.VolumeId,
		"TargetPath": target,
		"ExportPath": nfsExportURL,
		"AccessMode": am.GetMode(),
	}
	Log.WithFields(fields).Info("Node publish volume params ")

	mnts, err := gofsutil.GetMounts(ctx)
	if err != nil {
		return status.Errorf(codes.Internal,
			"could not reliably determine existing mount status: '%s'",
			err.Error())
	}

	if len(mnts) != 0 {
		for _, m := range mnts {
			// check for idempotency
			// same volume
			if m.Device == nfsExportURL {
				if m.Path == target {
					// as per specs, T1=T2, P1=P2 - return OK
					if contains(m.Opts, rwOption) {
						Log.WithFields(fields).Debug(
							"mount already in place with same options")
						return nil
					}
					// T1=T2, P1!=P2 - return AlreadyExists
					Log.WithFields(fields).Error("Mount point already in use by device with different options")
					return status.Error(codes.AlreadyExists, "Mount point already in use by device with different options")
				}
				// T1!=T2, P1==P2 || P1 != P2 - return FailedPrecondition for single node
				if am.GetMode() == csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER ||
					am.GetMode() == csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY ||
					am.GetMode() == csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER {
					Log.WithFields(fields).Error("Mount point already in use for same device")
					return status.Error(codes.FailedPrecondition, "Mount point already in use for same device")
				}
			}
		}
	}

	Log.Infof("The mountOptions being used for mount are: %s", mntOptions)
	if err := gofsutil.Mount(context.Background(), nfsExportURL, target, "nfs", mntOptions...); err != nil {
		count := 0
		errmsg := err.Error()
		// Both substring validation is for NFSv3 and NFSv4 errors resp.
		for (strings.Contains(strings.ToLower(errmsg), "access denied by server while mounting") || (strings.Contains(strings.ToLower(errmsg), "no such file or directory"))) && count < 5 {
			time.Sleep(2 * time.Second)
			Log.Infof("Mount re-trial attempt-%d", count)
			err = gofsutil.Mount(context.Background(), nfsExportURL, target, "nfs", mntOptions...)
			if err != nil {
				errmsg = err.Error()
			} else {
				break
			}
			count++
		}
		if err != nil {
			Log.Errorf("%v", err)
			return err
		}
	}
	return nil
}

func unpublishNFS(ctx context.Context, req *csi.NodeUnpublishVolumeRequest, filterStr string) error {
	target := req.GetTargetPath()

	Log.Debugf("attempting to unmount '%s'", target)
	isMounted, err := isVolumeMounted(ctx, filterStr, target)
	if err != nil {
		return err
	}
	if !isMounted {
		return nil
	}
	if err := gofsutil.Unmount(context.Background(), target); err != nil {
		return status.Errorf(codes.Internal,
			"error unmounting target'%s': '%s'", target, err.Error())
	}
	Log.Debugf("unmounting '%s' succeeded", target)

	return nil
}

func isVolumeMounted(ctx context.Context, filterStr string, target string) (bool, error) {
	mnts, err := gofsutil.GetMounts(ctx)
	if err != nil {
		return false, status.Errorf(codes.Internal,
			"could not reliably determine existing mount status: '%s'",
			err.Error())
	}

	if len(mnts) != 0 {
		// Idempotence check not to return error if not published
		for _, m := range mnts {
			if strings.Contains(m.Device, filterStr) {
				if m.Path == target {
					return true, nil
				}
			}
		}
		Log.Debugf("target '%s' does not exist", target)
		return false, nil
	}

	// No mount exists also means not published
	Log.Debugf("target '%s' does not exist", target)
	return false, nil
}

func handlePrivFSMount(
	ctx context.Context,
	accMode *csi.VolumeCapability_AccessMode,
	sysDevice *Device,
	mntFlags []string,
	fs, privTgt, fsFormatOption string,
) error {
	// Invoke the formats with a No Discard option to reduce formatting time
	formatCtx := context.WithValue(ctx, gofsutil.ContextKey(gofsutil.NoDiscard), gofsutil.NoDiscard)

	// If read-only access mode, we don't allow formatting
	switch accMode.GetMode() {
	case csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY, csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY:
		mntFlags = append(mntFlags, "ro")
		if err := gofsutil.Mount(ctx, sysDevice.FullPath, privTgt, fs, mntFlags...); err != nil {
			return status.Errorf(codes.Internal,
				"error performing private mount: %s",
				err.Error())
		}
		return nil
	case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER:
		if fsFormatOption != "" {
			mntFlags = append(mntFlags, "fsFormatOption:"+fsFormatOption)
		}
		if err := gofsutil.FormatAndMount(formatCtx, sysDevice.FullPath, privTgt, fs, mntFlags...); err != nil {
			return status.Errorf(codes.Internal,
				"error performing private mount: %s",
				err.Error())
		}
		return nil
	default:
		return status.Error(codes.Internal, "Invalid access mode")
	}
}

func getPrivateMountPoint(privDir string, name string) string {
	return filepath.Join(privDir, name)
}

func contains(list []string, item string) bool {
	for _, x := range list {
		if x == item {
			return true
		}
	}
	return false
}

// mkfile creates a file specified by the path if needed.
// return pair is a bool flag of whether file was created, and an error
func mkfile(path string) (bool, error) {
	st, err := os.Stat(path)
	if err != nil {
		Log.Warnf("Unable to check stat of file: %s with error: %v", path, err.Error())
		if os.IsNotExist(err) {
			/* #nosec G302 G304 */
			file, err := os.OpenFile(path, os.O_CREATE, 0o755)
			if err != nil {
				Log.WithField("dir", path).WithError(
					err).Error("Unable to create dir")
				return false, err
			}
			err = file.Close()
			if err != nil {
				// Log the error but keep going
				Log.WithField("file", path).WithError(
					err).Error("Unable to close file")
			}
			Log.WithField("path", path).Debug("created file")
			return true, nil
		}
	}
	if st.IsDir() {
		return false, fmt.Errorf("existing path is a directory")
	}
	return false, nil
}

// mkdir creates the directory specified by path if needed.
// return pair is a bool flag of whether dir was created, and an error
func mkdir(path string) (bool, error) {
	st, err := os.Stat(path)
	if err != nil {
		Log.Warnf("Unable to check stat of file: %s with error: %v", path, err.Error())
		if os.IsNotExist(err) {
			err := os.Mkdir(path, 0o755) // #nosec G301
			if err != nil {
				Log.WithField("dir", path).WithError(
					err).Error("Unable to create dir")
				return false, err
			}
			Log.WithField("path", path).Debug("created directory")
			return true, nil
		}
	}
	if !st.IsDir() {
		return false, fmt.Errorf("existing path is not a directory")
	}
	return false, nil
}

// unpublishVolume removes the bind mount to the target path, and also removes
// the mount to the private mount directory if the volume is no longer in use.
// It determines this by checking to see if the volume is mounted anywhere else
// other than the private mount.
func unpublishVolume(
	volumeID, targetPath string,
	privDir, device string, reqID string,
) error {
	ctx := context.Background()

	if targetPath == "" {
		return status.Error(codes.InvalidArgument,
			"target path required")
	}

	// make sure device is valid
	sysDevice, err := GetDevice(device)
	if err != nil {
		return status.Errorf(codes.Internal,
			"error getting block device for volume: %s, err: %s",
			volumeID, err.Error())
	}

	// Path to mount device to
	privTgt := getPrivateMountPoint(privDir, volumeID)

	f := logrus.Fields{
		"device":       sysDevice.RealDev,
		"privTgt":      privTgt,
		"CSIRequestID": reqID,
		"target":       targetPath,
	}

	mnts, err := gofsutil.GetMounts(ctx)
	if err != nil {
		return status.Errorf(codes.Internal,
			"could not reliably determine existing mount status: %s",
			err.Error())
	}

	tgtMntExist := false
	privMntExist := false
	keepPrivMnt := false
	var deviceMount gofsutil.Info
	for _, m := range mnts {
		if m.Source == sysDevice.RealDev || m.Device == sysDevice.RealDev || m.Device == sysDevice.FullPath {
			if m.Path == privTgt {
				privMntExist = true
				Log.Printf("Found private mount for device %#v, private mount path: %s .", sysDevice, privTgt)
			} else if m.Path == targetPath {
				tgtMntExist = true
				deviceMount = m
				Log.Printf("Found target mount for device %#v, target mount path: %s .", sysDevice, targetPath)
			} else {
				// Check if this is a target mount for another pod in which case we should not unmount the private target
				thisPodID := getPodIDFromTargetPath(targetPath)
				thatPodID := getPodIDFromTargetPath(m.Path)
				if thisPodID != "" && thatPodID != "" && thisPodID != thatPodID {
					Log.Infof("Will not unmount the private mount since another pod is using this volume: %s", m.Path)
					keepPrivMnt = true
				}
			}
		}
	}
	if tgtMntExist && !privMntExist {
		Log.Warnf("Device %#v has target mount without private mount. Target mount %#v", sysDevice, deviceMount)
	}

	if tgtMntExist {
		Log.WithFields(f).Debug(fmt.Sprintf("Unmounting %s", targetPath))
		if err := gofsutil.Unmount(ctx, targetPath); err != nil {
			return status.Errorf(codes.Internal,
				"Error unmounting target: %s", err.Error())
		}
		if err := removeWithRetry(targetPath); err != nil {
			return status.Errorf(codes.Internal,
				"Error remove target folder: %s", err.Error())
		}
	}

	if privMntExist && !keepPrivMnt {
		Log.WithFields(f).Debug(fmt.Sprintf("Unmounting %s", privTgt))
		if err := unmountPrivMount(ctx, sysDevice, privTgt); err != nil {
			return status.Errorf(codes.Internal,
				"Error unmounting private mount: %s", err.Error())
		}
	}

	return nil
}

var getTargetPathPrefix = func() string {
	return "/var/lib/kubelet/pods/"
}

// Parse the target path to get the pod ID
// Assume target path looks like: /var/lib/kubelet/pods/{podID}/volumes/...
func getPodIDFromTargetPath(targetPath string) string {
	if strings.HasPrefix(targetPath, getTargetPathPrefix()) {
		targetPath = strings.TrimPrefix(targetPath, getTargetPathPrefix())
		parts := strings.Split(targetPath, "/")
		if len(parts) > 1 {
			// check if parts[0] is a UUID
			if _, err := uuid.Parse(parts[0]); err == nil {
				return parts[0]
			}
		}
	}
	return ""
}

func unmountPrivMount(
	ctx context.Context,
	dev *Device,
	target string,
) error {
	mnts, err := getDevMounts(dev)
	if err != nil {
		return err
	}
	// remove private mount if we can
	for _, m := range mnts {
		if m.Path != target {
			continue
		}
		if err := gofsutil.Unmount(ctx, target); err != nil {
			return err
		}
		Log.WithField("directory", target).Debug(
			"removing directory")
		if err := os.Remove(target); err != nil {
			Log.Errorf("Unable to remove directory: %v", err)
		}
	}
	return nil
}

func getDevMounts(
	sysDevice *Device,
) ([]gofsutil.Info, error) {
	ctx := context.Background()
	devMnts := make([]gofsutil.Info, 0)

	mnts, err := gofsutil.GetMounts(ctx)
	if err != nil {
		return devMnts, err
	}
	for _, m := range mnts {
		if m.Device == sysDevice.RealDev || (m.Device == "devtmpfs" && m.Source == sysDevice.RealDev) {
			devMnts = append(devMnts, m)
		}
	}
	return devMnts, nil
}

// For Windows testing, replace any paths with \\ to have /
func replaceBackslashWithSlash(input string) string {
	return strings.Replace(input, "\\", "/", -1)
}

// getPathMounts finds all the mounts for a given path.
func getPathMounts(path string) ([]gofsutil.Info, error) {
	ctx := context.Background()
	devMnts := make([]gofsutil.Info, 0)

	mnts, err := gofsutil.GetMounts(ctx)
	if err != nil {
		return devMnts, err
	}
	for _, m := range mnts {
		if m.Path == path {
			devMnts = append(devMnts, m)
		}
	}
	return devMnts, nil
}

func removeWithRetry(target string) error {
	var err error
	for i := 0; i < 3; i++ {
		err = os.Remove(target)
		if err != nil && !os.IsNotExist(err) {
			Log.Error("error removing private mount target: " + err.Error())
			err = os.RemoveAll(target)
			if err != nil {
				Log.Errorf("Error removing directory: %v", err.Error())
			}
			time.Sleep(3 * time.Second)
		} else {
			err = nil
			break
		}
	}
	return err
}

// Evaulate symlinks to a resolution. In case of an error,
// logs the error but returns the original path.
func evalSymlinks(path string) string {
	// eval any symlinks and make sure it points to a device
	d, err := filepath.EvalSymlinks(path)
	if err != nil {
		Log.Error("Could not evaluate symlinks for path: " + path)
		return path
	}
	return d
}

// Given a volume capability, validates it and returns:
// boolean isBlock -- the capability is for a block device
// csi.VolumeCapability_MountVolume - contains FsType and MountFlags
// csi.VolumeCapability_AccessMode accMode gives the access mode
// string multiAccessFlag - "rw" or "ro" or "" as appropriate
// error
func validateVolumeCapability(volCap *csi.VolumeCapability, readOnly bool) (bool, *csi.VolumeCapability_MountVolume, *csi.VolumeCapability_AccessMode, string, error) {
	var mntVol *csi.VolumeCapability_MountVolume
	isBlock := false
	isMount := false
	multiAccessFlag := ""
	accMode := volCap.GetAccessMode()
	if accMode == nil {
		return false, mntVol, nil, "", status.Error(codes.InvalidArgument, "Volume Access Mode is required")
	}
	if blockVol := volCap.GetBlock(); blockVol != nil {
		isBlock = true
		switch accMode.GetMode() {
		case csi.VolumeCapability_AccessMode_UNKNOWN:
			return true, mntVol, accMode, "", status.Error(codes.InvalidArgument, "Unknown Access Mode")
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER:
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY:
		case csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY:
			multiAccessFlag = "ro"
		case csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER:
		case csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER:
			multiAccessFlag = "rw"
		}
		if readOnly {
			Log.Warnf("read only for Block Volume is not recommended")
		}
	}
	mntVol = volCap.GetMount()
	if mntVol != nil {
		isMount = true
		switch accMode.GetMode() {
		case csi.VolumeCapability_AccessMode_UNKNOWN:
			return false, mntVol, accMode, "", status.Error(codes.InvalidArgument, "Unknown Access Mode")
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER:
			if mountAllowRWOMultiPodAccess {
				multiAccessFlag = "rw"
			}
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY:
			if mountAllowRWOMultiPodAccess {
				multiAccessFlag = "ro"
			}
		case csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY:
			multiAccessFlag = "ro"
		case csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER:
		case csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER:
			return false, mntVol, accMode, "", status.Error(codes.AlreadyExists, "Mount volumes do not support AccessMode MULTI_NODE_MULTI_WRITER")
		}
	}

	if !isBlock && !isMount {
		return false, mntVol, accMode, "", status.Error(codes.InvalidArgument, "Volume Access Type is required")
	}
	return isBlock, mntVol, accMode, multiAccessFlag, nil
}

// singleAccessMode returns true if only a single access is allowed SINGLE_NODE_WRITER, SINGLE_NODE_READER_ONLY, or SINGLE_NODE_SINGLE_WRITER
func singleAccessMode(accMode *csi.VolumeCapability_AccessMode) bool {
	if mountAllowRWOMultiPodAccess {
		// User specifically asks for multi-pod access on same nodes
		return false
	}
	switch accMode.GetMode() {
	case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER:
		return true
	}
	return false
}

func createTarget(target string, isBlock bool) error {
	var err error
	// Make sure target is created. The spec says the driver is responsible
	// for creating the target, but Kubernetes generallly creates the target.
	if isBlock {
		_, err = mkfile(target)
		if err != nil {
			return status.Error(codes.FailedPrecondition, fmt.Sprintf("Could not create %s: %s", target, err.Error()))
		}
	} else {
		_, err = mkdir(target)
		if err != nil {
			return status.Error(codes.FailedPrecondition, fmt.Sprintf("Could not create %s: %s", target, err.Error()))
		}
	}
	return nil
}

// cleanupPrivateTarget unmounts and removes the private directory for the retry so clean start next time.
func cleanupPrivateTarget(dev *Device, reqID, privTgt string) error {
	Log.WithField("CSIRequestID", reqID).WithField("privTgt", privTgt).Info("Cleaning up private target")
	mnts, err := getDevMounts(dev)
	if err != nil {
		return err
	}
	if len(mnts) == 1 && mnts[0].Path == privTgt {
		if privErr := gofsutil.Unmount(context.Background(), privTgt); privErr != nil {
			Log.WithField("CSIRequestID", reqID).Printf("Error unmounting privTgt %s: %s", privTgt, privErr)
			return privErr
		}
		if privErr := removeWithRetry(privTgt); privErr != nil {
			Log.WithField("CSIRequestID", reqID).Printf("Error removing privTgt %s: %s", privTgt, privErr)
			return privErr
		}
	} else if len(mnts) == 0 {
		st, err := os.Stat(privTgt)
		if err != nil {
			return err
		}
		if st.IsDir() {
			if privErr := removeWithRetry(privTgt); privErr != nil {
				Log.WithField("CSIRequestID", reqID).Printf("Error removing privTgt %s: %s", privTgt, privErr)
				return privErr
			}
		}
	} else {
		Log.WithField("CSIRequestID", reqID).Printf("Cannot delete private mount because there are target mounts : %s", privTgt)
		return status.Error(codes.Internal, "Cannot delete private mount as target mount exist")
	}
	return nil
}

// mountBlock bind mounts the device to the required target
func mountBlock(device *Device, target string, mntFlags []string, singleAccess bool) error {
	Log.Printf("mountBlock called device %#v target %s mntFlags %#v", device, target, mntFlags)
	// Check to see if already mounted
	mnts, err := getDevMounts(device)
	if err != nil {
		return status.Errorf(codes.Internal, "Could not getDevMounts for: %s", device.RealDev)
	}
	for _, mnt := range mnts {
		if mnt.Path == target {
			Log.Info("Block volume target is already mounted")
			return nil
		} else if singleAccess {
			return status.Error(codes.InvalidArgument, "Access mode conflicts with existing mounts")
		}
	}
	err = createTarget(target, true)
	if err != nil {
		return status.Error(codes.FailedPrecondition, fmt.Sprintf("Could not create %s: %s", target, err.Error()))
	}
	err = gofsutil.BindMount(context.Background(), device.RealDev, target, mntFlags...)
	if err != nil {
		return status.Errorf(codes.Internal, "error bind mounting to target path: %s", target)
	}
	return nil
}
