// Copyright © 2019-2024 Dell Inc. or its subsidiaries. All Rights Reserved.
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
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/gofsutil"
	"github.com/dell/goscaleio"
	"github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var (
	// slice of the connected PowerFlex systems
	connectedSystemID             = make([]string, 0)
	publishGetMappedVolMaxRetry   = 30
	unpublishGetMappedVolMaxRetry = 5
	getMappedVolDelay             = (1 * time.Second)

	// GetNodeLabels - Get the node labels
	GetNodeLabels = getNodelabels
	GetNodeUID    = getNodeUID
)

const (
	maxVxflexosVolumesPerNodeLabel = "max-vxflexos-volumes-per-node"
)

func (s *service) NodeStageVolume(
	_ context.Context,
	_ *csi.NodeStageVolumeRequest) (
	*csi.NodeStageVolumeResponse, error,
) {
	return nil, status.Error(codes.Unimplemented, "")
}

// NodeUnstageVolume will cleanup the staging path passed in the request.
// This will only be called by CSM-resliency (podmon), as the driver does not advertise support for STAGE_UNSTAGE_VOLUME in NodeGetCapabilities,
// therefore Kubernetes will not call it.
func (s *service) NodeUnstageVolume(
	ctx context.Context,
	req *csi.NodeUnstageVolumeRequest) (
	*csi.NodeUnstageVolumeResponse, error,
) {
	var reqID string
	headers, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if req, ok := headers["csi.requestid"]; ok && len(req) > 0 {
			reqID = req[0]
		}
	}

	stagingTargetPath := req.GetStagingTargetPath()
	if stagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "StagingTargetPath is required")
	}
	csiVolID := req.GetVolumeId()
	if csiVolID == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID is required")
	}

	fields := map[string]interface{}{
		"CSI Request":         "NodeUnstageVolume",
		"CSI Volume ID":       csiVolID,
		"Staging Target Path": stagingTargetPath,
		"Request ID":          reqID,
	}

	// Skip ephemeral volumes. For ephemeral volumes, kubernetes gives us an internal ID, so we use the lockfile to find the Powerflex ID this is mapped to.
	lockFile := ephemeralStagingMountPath + csiVolID + "/id"
	if s.fileExist(lockFile) {
		Log.WithFields(fields).Info("Skipping ephemeral volume")
		return &csi.NodeUnstageVolumeResponse{}, nil
	}

	// Unmount the staging target path.
	Log.WithFields(fields).Info("unmounting directory")
	if err := gofsutil.Unmount(ctx, stagingTargetPath); err != nil && !os.IsNotExist(err) {
		Log.Errorf("Unable to Unmount staging target path: %s", err)
	}

	Log.WithFields(fields).Info("removing directory")
	if err := os.Remove(stagingTargetPath); err != nil && !os.IsNotExist(err) {
		Log.Errorf("Unable to remove staging target path: %v", err)
		err := fmt.Errorf("Unable to remove staging target path: %s error: %v", stagingTargetPath, err)
		return &csi.NodeUnstageVolumeResponse{}, err
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (s *service) NodePublishVolume(
	ctx context.Context,
	req *csi.NodePublishVolumeRequest) (
	*csi.NodePublishVolumeResponse, error,
) {
	var reqID string
	headers, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if req, ok := headers["csi.requestid"]; ok && len(req) > 0 {
			reqID = req[0]
		}
	}
	s.logStatistics()
	volumeContext := req.GetVolumeContext()
	if volumeContext != nil {
		Log.Info("VolumeContext:")
		for key, value := range volumeContext {
			Log.WithFields(logrus.Fields{key: value}).Info("found in VolumeContext")
		}
	}

	ephemeral, ok := req.VolumeContext["csi.storage.k8s.io/ephemeral"]
	if ok && strings.ToLower(ephemeral) == "true" {
		resp, err := s.ephemeralNodePublish(ctx, req)
		if err != nil {
			Log.Errorf("ephemeralNodePublish returned error: %v", err)
		}
		return resp, err
	}

	csiVolID := req.GetVolumeId()
	if csiVolID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"volume ID is required")
	}
	Log.Printf("[NodePublishVolume] csiVolID: %s", csiVolID)

	// Check for NFS protocol
	fsType := volumeContext[KeyFsType]
	isNFS := false
	if fsType == "nfs" {
		isNFS = true
	}

	volID := getVolumeIDFromCsiVolumeID(csiVolID)
	Log.Printf("[NodePublishVolume] volumeID: %s", volID)

	systemID := s.getSystemIDFromCsiVolumeID(csiVolID)
	Log.Printf("[NodePublishVolume] systemID: %s harvested from csiVolID: %s", systemID, csiVolID)
	if systemID == "" {
		// use default system
		systemID = s.opts.defaultSystemID
	}
	if systemID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"systemID is not found in the request and there is no default system")
	}

	Log.Printf("[NodePublishVolume] We are about to probe the system with systemID %s", systemID)
	// Probe the system to make sure it is managed by driver
	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	// ensure no ambiguity if legacy vol
	err := s.checkVolumesMap(csiVolID)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"checkVolumesMap for id: %s failed : %s", csiVolID, err.Error())
	}
	// handle NFS nodePublish separately
	if isNFS {
		fsID := getFilesystemIDFromCsiVolumeID(csiVolID)

		fs, err := s.getFilesystemByID(fsID, systemID)
		if err != nil {
			if strings.EqualFold(err.Error(), sioGatewayFileSystemNotFound) || strings.Contains(err.Error(), "must be a hexadecimal number") {
				return nil, status.Error(codes.NotFound,
					"filesystem not found")
			}
		}

		client := s.adminClients[systemID]

		NFSExport, err := s.getNFSExport(fs, client)
		if err != nil {
			return nil, err
		}

		fileInterface, err := s.getFileInterface(systemID, fs, client)
		if err != nil {
			return nil, err
		}
		// Formulating nfsExportURl
		// NFSExportURL = "nas_server_ip:NFSExport_Path"
		// NFSExportURL = 10.1.1.1.1:/nfs-volume
		path := fmt.Sprintf("%s:%s", fileInterface.IPAddress, NFSExport.Path)

		if err := publishNFS(ctx, req, path); err != nil {
			return nil, err
		}

		return &csi.NodePublishVolumeResponse{}, nil
	}

	sdcMappedVol, err := s.getSDCMappedVol(volID, systemID, publishGetMappedVolMaxRetry)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if err := publishVolume(req, s.privDir, sdcMappedVol.SdcDevice, reqID); err != nil {
		return nil, err
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (s *service) NodeUnpublishVolume(
	ctx context.Context,
	req *csi.NodeUnpublishVolumeRequest) (
	*csi.NodeUnpublishVolumeResponse, error,
) {
	var reqID string
	headers, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if req, ok := headers["csi.requestid"]; ok && len(req) > 0 {
			reqID = req[0]
		}
	}

	targetPath := req.GetTargetPath()
	if targetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "A target path argument is required")
	}

	s.logStatistics()

	csiVolID := req.GetVolumeId()
	if csiVolID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"volume ID is required")
	}

	isNFS := strings.Contains(csiVolID, "/")
	var ephemeralVolume bool
	// For ephemeral volumes, kubernetes gives us an internal ID, so we need to use the lockfile to find the Powerflex ID this is mapped to.
	lockFile := ephemeralStagingMountPath + csiVolID + "/id"
	if s.fileExist(lockFile) {
		ephemeralVolume = true
		//while a file is being read from, it's a file determined by volID and is written by the driver
		/* #nosec G304 */
		idFromFile, err := os.ReadFile(lockFile)
		if err != nil && os.IsNotExist(err) {
			Log.Errorf("NodeUnpublish with ephemeral volume. Was unable to read lockfile: %v", err)
			return nil, status.Error(codes.Internal, "NodeUnpublish with ephemeral volume. Was unable to read lockfile")
		}
		// Convert volume id from []byte to string format
		csiVolID = string(idFromFile)
		Log.Infof("Read volume ID: %s from lockfile: %s ", csiVolID, lockFile)

	}

	if isNFS {
		fsID := getFilesystemIDFromCsiVolumeID(csiVolID)
		Log.Printf("NodeUnpublishVolume fileSystemID: %s", fsID)

		systemID := s.getSystemIDFromCsiVolumeID(csiVolID)
		if systemID == "" {
			// use default system
			systemID = s.opts.defaultSystemID
		}
		Log.Printf("NodeUnpublishVolume systemID: %s", systemID)
		if systemID == "" {
			return nil, status.Error(codes.InvalidArgument,
				"systemID is not found in the request and there is no default system")
		}

		fs, err := s.getFilesystemByID(fsID, systemID)
		if err != nil {
			if strings.EqualFold(err.Error(), sioGatewayFileSystemNotFound) || strings.Contains(err.Error(), "must be a hexadecimal number") {
				return nil, status.Error(codes.NotFound,
					"filesystem not found")
			}
		}

		// Probe the system to make sure it is managed by driver
		if err := s.requireProbe(ctx, systemID); err != nil {
			return nil, err
		}

		// ensure no ambiguity if legacy vol
		err = s.checkVolumesMap(csiVolID)
		if err != nil {
			return nil, status.Errorf(codes.Internal,
				"checkVolumesMap for id: %s failed : %s", csiVolID, err.Error())
		}

		if err := unpublishNFS(ctx, req, fs.Name); err != nil {
			return nil, err
		}

		return &csi.NodeUnpublishVolumeResponse{}, nil

	}

	volID := getVolumeIDFromCsiVolumeID(csiVolID)
	Log.Printf("NodeUnpublishVolume volumeID: %s", volID)

	systemID := s.getSystemIDFromCsiVolumeID(csiVolID)
	if systemID == "" {
		// use default system
		systemID = s.opts.defaultSystemID
	}
	Log.Printf("NodeUnpublishVolume systemID: %s", systemID)
	if systemID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"systemID is not found in the request and there is no default system")
	}

	// Probe the system to make sure it is managed by driver
	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	// ensure no ambiguity if legacy vol
	err := s.checkVolumesMap(csiVolID)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"checkVolumesMap for id: %s failed : %s", csiVolID, err.Error())
	}

	sdcMappedVol, err := s.getSDCMappedVol(volID, systemID, unpublishGetMappedVolMaxRetry)
	if err != nil {
		Log.Infof("Error from getSDCMappedVol is: %#v", err)
		Log.Infof("Error message from getSDCMappedVol is: %s", err.Error())
		// fix k8s 19 bug: ControllerUnpublishVolume is called before NodeUnpublishVolume
		// cleanup target from pod
		if err := gofsutil.Unmount(ctx, targetPath); err != nil {
			Log.Errorf("cleanup target mount: %s", err.Error())
		}

		if err := removeWithRetry(targetPath); err != nil {
			Log.Errorf("cleanup target path: %s", err.Error())
		}
		// dont cleanup pvtMount in case it is in use elsewhere on the node

		if ephemeralVolume {
			Log.Info("Detected ephemeral")
			err := s.ephemeralNodeUnpublish(ctx, req)
			if err != nil {
				Log.Errorf("ephemeralNodeUnpublish returned error: %s", err.Error())
				return nil, err
			}
		}

		// Idempotent need to return ok if not published
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	if err := unpublishVolume(req, s.privDir, sdcMappedVol.SdcDevice, reqID); err != nil {
		return nil, err
	}

	if ephemeralVolume {
		Log.Info("Detected ephemeral")
		err := s.ephemeralNodeUnpublish(ctx, req)
		if err != nil {
			Log.Errorf("ephemeralNodeUnpublish returned error: %v", err)
			return nil, err
		}

	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// Get sdc mapped volume from the given volume ID/systemID
func (s *service) getSDCMappedVol(volumeID string, systemID string, maxRetry int) (*goscaleio.SdcMappedVolume, error) {
	// If not found immediately, give a little time for controller to
	// communicate with SDC that it has volume
	var sdcMappedVol *goscaleio.SdcMappedVolume
	var err error
	for i := 0; i < maxRetry; i++ {
		if id, ok := s.connectedSystemNameToID[systemID]; ok {
			Log.Printf("Node publish getMappedVol name: %s id: %s", systemID, id)
			systemID = id
		}
		sdcMappedVol, err = getMappedVol(volumeID, systemID)
		if sdcMappedVol != nil {
			break
		}
		Log.Printf("Node publish getMappedVol retry: %d", i)
		time.Sleep(getMappedVolDelay)
	}
	if err != nil {
		Log.Printf("SDC returned volume %s on system %s not published to node", volumeID, systemID)
		return nil, err
	}
	return sdcMappedVol, err
}

// Get the volumes published to the SDC (given by SdcMappedVolume) and scan for requested vol id
func getMappedVol(volID string, systemID string) (*goscaleio.SdcMappedVolume, error) {
	// get source path of volume/device
	localVols, _ := goscaleio.GetLocalVolumeMap()
	var sdcMappedVol *goscaleio.SdcMappedVolume
	if len(localVols) == 0 {
		Log.Printf("Length of localVols (goscaleio.GetLocalVolumeMap()) is 0 \n")
	}
	for _, v := range localVols {
		if v.VolumeID == volID && v.MdmID == systemID {
			sdcMappedVol = v
			Log.Printf("Found matching SDC mapped volume %v", sdcMappedVol)
			break
		}
	}
	if sdcMappedVol == nil {
		return nil, status.Errorf(codes.Unavailable,
			"volume: %s on system: %s not published to node", volID, systemID)
	}
	return sdcMappedVol, nil
}

// getSystemName gets the system name for each system and append it to connectedSystemID variable
func (s *service) getSystemName(_ context.Context, systems []string) bool {
	for systemID := range s.opts.arrays {
		if id, ok := s.connectedSystemNameToID[systemID]; ok {
			for _, system := range systems {
				if id == system {
					Log.Printf("nodeProbe found system Name: %s with id %s", systemID, id)
					connectedSystemID = append(connectedSystemID, systemID)
				}
			}
		}
	}
	return true
}

// nodeProbe fetchs the SDC GUID by drv_cfg and the systemIDs/names by getSystemName method.
// It also makes sure private directory(privDir) is created
func (s *service) nodeProbe(ctx context.Context) error {

	// make sure the kernel module is loaded
	if kmodLoaded(s.opts) {
		// fetch the SDC GUID
		if s.opts.SdcGUID == "" {
			// try to query the SDC GUID
			guid, err := goscaleio.DrvCfgQueryGUID()
			if err != nil {
				return status.Errorf(codes.FailedPrecondition,
					"unable to get SDC GUID via config or automatically, error: %s", err.Error())
			}

			s.opts.SdcGUID = guid
			Log.WithField("guid", s.opts.SdcGUID).Info("set SDC GUID")
		}

		// fetch the systemIDs
		var err error
		if len(connectedSystemID) == 0 {
			connectedSystemID, err = getSystemsKnownToSDC()
			if err != nil {
				return status.Errorf(codes.FailedPrecondition, "%s", err.Error())
			}
		}

		// 	rename SDC
		//	case1: if IsSdcRenameEnabled=true and prefix given then set the prefix+worker_node_name for sdc name.
		//	case2: if IsSdcRenameEnabled=true and prefix not given then set worker_node_name for sdc name.
		//
		if s.opts.IsSdcRenameEnabled {
			err = s.renameSDC(s.opts)
			if err != nil {
				return err
			}
		}

		// support for pre-approved guid
		if s.opts.IsApproveSDCEnabled {
			Log.Infof("Approve SDC enabled")
			if err := s.approveSDC(s.opts); err != nil {
				return err
			}
		}

		// get all the system names and IDs.
		s.getSystemName(ctx, connectedSystemID)

		// make sure privDir is pre-created
		if _, err := mkdir(s.privDir); err != nil {
			return status.Errorf(codes.Internal,
				"plugin private dir: %s creation error: %s",
				s.privDir, err.Error())
		}
	} else {
		Log.Infof("scini module not loaded, perhaps it was intentional")
	}

	return nil
}

func (s *service) approveSDC(opts Opts) error {
	for _, systemID := range connectedSystemID {
		system := s.systems[systemID]

		if system == nil {
			continue
		}

		// fetch SDC details
		sdc, err := s.systems[systemID].FindSdc("SdcGUID", opts.SdcGUID)
		if err != nil {
			return status.Errorf(codes.FailedPrecondition, "%s", err)
		}

		// fetch the restrictedSdcMode
		if system.System.RestrictedSdcMode == "Guid" {
			if !sdc.Sdc.SdcApproved {
				resp, err := system.ApproveSdcByGUID(sdc.Sdc.SdcGUID)
				if err != nil {
					return status.Errorf(codes.FailedPrecondition, "%s", err)
				}
				Log.Infof("SDC Approved, SDC Id: %s and SDC GUID: %s", resp.SdcID, sdc.Sdc.SdcGUID)
			} else {
				Log.Infof("SDC already approved, SDC GUID: %s", sdc.Sdc.SdcGUID)
			}
		} else {
			if !sdc.Sdc.SdcApproved {
				return status.Errorf(codes.FailedPrecondition, "Array RestrictedSdcMode is %s, driver only supports GUID RestrictedSdcMode cannot approve SDC %s",
					system.System.RestrictedSdcMode, sdc.Sdc.SdcGUID)
			}
			Log.Warnf("Array RestrictedSdcMode is %s, driver only supports GUID RestrictedSdcMode If SDC becomes restricted again, driver will not be able to approve",
				system.System.RestrictedSdcMode)
		}

	}
	return nil
}

func (s *service) renameSDC(opts Opts) error {
	// fetch hostname
	hostName, ok := os.LookupEnv("HOSTNAME")
	if !ok {
		return status.Errorf(codes.FailedPrecondition, "%s not set", "HOSTNAME")
	}

	// fetch SDC details
	for _, systemID := range connectedSystemID {
		if s.systems[systemID] == nil {
			continue
		}
		sdc, err := s.systems[systemID].FindSdc("SdcGUID", opts.SdcGUID)
		if err != nil {
			return status.Errorf(codes.FailedPrecondition, "%s", err)
		}
		sdcID := sdc.Sdc.ID

		var newName string
		if len(opts.SdcPrefix) > 0 {
			// case1: if IsSdcRenameEnabled=true and prefix given then set the prefix+worker_node_name for sdc name.
			newName = opts.SdcPrefix + "-" + hostName
		} else {
			// case2: if IsSdcRenameEnabled=true and prefix not given then set worker_node_name for sdc name.
			newName = hostName
		}
		if sdc.Sdc.Name == newName {
			Log.Infof("SDC is already named: %s.", newName)
		} else {
			Log.Infof("Assigning name: %s to SDC with GUID %s on system %s", newName, s.opts.SdcGUID,
				systemID)
			err = s.adminClients[systemID].RenameSdc(sdcID, newName)
			if err != nil {
				return status.Errorf(codes.FailedPrecondition, "Failed to rename SDC: %s", err)
			}
			err = s.getSDCName(opts.SdcGUID, systemID)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *service) getSDCName(sdcGUID string, systemID string) error {
	sdc, err := s.systems[systemID].FindSdc("SdcGUID", sdcGUID)
	if err != nil {
		return status.Errorf(codes.FailedPrecondition, "%s", err)
	}
	Log.Infof("SDC name set to: %s.", sdc.Sdc.Name)
	return nil
}

func kmodLoaded(opts Opts) bool {
	// opts.Lsmod is introduced solely for unit testing.
	var out []byte
	var err error
	if opts.Lsmod == "" {
		out, err = exec.Command("lsmod").CombinedOutput()
		if err != nil {
			Log.WithError(err).Error("error from lsmod")
			return false
		}
	} else {
		out = []byte(opts.Lsmod)
	}

	r := bytes.NewReader(out)
	s := bufio.NewScanner(r)

	for s.Scan() {
		l := s.Text()
		words := strings.Split(l, " ")
		if words[0] == "scini" {
			return true
		}
	}

	return false
}

func getSystemsKnownToSDC() ([]string, error) {
	systems := make([]string, 0)

	discoveredSystems, err := goscaleio.DrvCfgQuerySystems()
	if err != nil {
		return systems, err
	}

	set := make(map[string]struct{}, len(*discoveredSystems))

	for _, s := range *discoveredSystems {
		_, ok := set[s.SystemID]
		// duplicate SDC ID found
		if ok {
			return nil, fmt.Errorf("duplicate systems found that are known to SDC: %s", s.SystemID)
		}
		set[s.SystemID] = struct{}{}

		systems = append(systems, s.SystemID)
		Log.WithField("ID", s.SystemID).Info("Found connected system")
	}

	return systems, nil
}

func (s *service) NodeGetCapabilities(
	_ context.Context,
	_ *csi.NodeGetCapabilitiesRequest) (
	*csi.NodeGetCapabilitiesResponse, error,
) {
	// these capabilities deal with volume health monitoring, and are only advertised by driver when user sets
	// node.healthMonitor.enabled is set to true in values file
	healthMonitorCapabalities := []*csi.NodeServiceCapability{
		{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_VOLUME_CONDITION,
				},
			},
		},
		{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
				},
			},
		},
	}

	nodeCapabalities := []*csi.NodeServiceCapability{
		{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
				},
			},
		},
		{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_SINGLE_NODE_MULTI_WRITER,
				},
			},
		},
	}

	if s.opts.IsHealthMonitorEnabled {
		nodeCapabalities = append(nodeCapabalities, healthMonitorCapabalities...)
	}
	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: nodeCapabalities,
	}, nil
}

// NodeGetInfo returns Node information
// NodeId is the identifier of the node. If SDC is installed, SDC GUID will be appended to NodeId
// MaxVolumesPerNode (optional) is left as 0 which means unlimited
// AccessibleTopology will be set with the VxFlex OS SystemID
func (s *service) NodeGetInfo(
	ctx context.Context,
	_ *csi.NodeGetInfoRequest) (
	*csi.NodeGetInfoResponse, error,
) {
	// Fetch SDC GUID
	if s.opts.SdcGUID == "" {
		if err := s.nodeProbe(ctx); err != nil {
			Log.Infof("failed to probe node: %s", err)
		}
	}

	// Fetch Node ID
	if len(connectedSystemID) == 0 {
		if err := s.nodeProbe(ctx); err != nil {
			Log.Infof("failed to probe node: %s", err)
		}
	}

	var maxVxflexosVolumesPerNode int64
	if len(connectedSystemID) != 0 {
		// Check for node label 'max-vxflexos-volumes-per-node'. If present set 'MaxVolumesPerNode' to this value.
		// If node label is not present, set 'MaxVolumesPerNode' to default value i.e., 0

		labels, err := GetNodeLabels(ctx, s)
		if err != nil {
			return nil, err
		}

		if val, ok := labels[maxVxflexosVolumesPerNodeLabel]; ok {
			maxVxflexosVolumesPerNode, err = strconv.ParseInt(val, 10, 64)
			if err != nil {
				return nil, status.Error(codes.InvalidArgument, GetMessage("invalid value '%s' specified for 'max-vxflexos-volumes-per-node' node label", val))
			}
		} else {
			// As per the csi spec the plugin MUST NOT set negative values to
			// 'MaxVolumesPerNode' in the NodeGetInfoResponse response
			if s.opts.MaxVolumesPerNode < 0 {
				return nil, status.Error(codes.InvalidArgument, GetMessage("maxVxflexosVolumesPerNode MUST NOT be set to negative value"))
			}
			maxVxflexosVolumesPerNode = s.opts.MaxVolumesPerNode
		}
	}

	Log.Debugf("MaxVolumesPerNode: %v\n", maxVxflexosVolumesPerNode)

	// Create the topology keys
	// csi-vxflexos.dellemc.com/<systemID>: <provisionerName>
	Log.Infof("Arrays: %+v", s.opts.arrays)
	topology := map[string]string{}
	for _, array := range s.opts.arrays {
		isNFS, err := s.checkNFS(ctx, array.SystemID)
		if err != nil {
			return nil, err
		}
		if isNFS {
			topology[Name+"/"+array.SystemID+"-nfs"] = "true"
		}
		topology[Name+"/"+array.SystemID] = SystemTopologySystemValue
	}

	nodeUID, err := GetNodeUID(ctx, s)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, GetMessage("Could not fetch node UID"))
	}

	if s.opts.SdcGUID != "" {
		nodeUID = fmt.Sprintf("%s-%s", nodeUID, s.opts.SdcGUID)
	}

	Log.Debugf("NodeId: %v\n", nodeUID)
	return &csi.NodeGetInfoResponse{
		NodeId: nodeUID,
		AccessibleTopology: &csi.Topology{
			Segments: topology,
		},
		MaxVolumesPerNode: maxVxflexosVolumesPerNode,
	}, nil
}

// NodeGetVolumeStats will check the status of a volume given its ID and path
// if volume is healthy, stats on volume usage will be returned
// if volume is unhealthy, a message will be returned detailing the issue
// To determine if volume is healthy, this method checks: volume known to array, volume known to SDC, volume path readable, and volume path mounted
// Note: kubelet only calls this method when feature gate: CSIVolumeHealth=true
func (s *service) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	csiVolID := req.GetVolumeId()
	volPath := req.GetVolumePath()
	mounted := false
	healthy := true
	message := ""

	// validate params first, make sure neither field is empty
	if len(volPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "no volume Path provided")
	}

	if len(csiVolID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "no volume ID  provided")
	}

	// check if volume exists

	volID := getVolumeIDFromCsiVolumeID(csiVolID)
	systemID := s.getSystemIDFromCsiVolumeID(csiVolID)

	if systemID == "" {
		// use default system
		systemID = s.opts.defaultSystemID
	}

	if systemID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"systemID is not found in the request and there is no default system")
	}

	_, err := s.getSDCMappedVol(volID, systemID, 30)
	if err != nil {
		// volume not known to SDC, next check if it exists at all
		_, _, err := s.listVolumes(systemID, 0, 0, false, false, volID, "")
		if err != nil && strings.Contains(err.Error(), sioGatewayVolumeNotFound) {
			message = fmt.Sprintf("Volume is not found by node driver at %s", time.Now().Format("2006-01-02 15:04:05"))
		} else if err != nil {
			// error was returned, but had nothing to do with the volume not being on the array (may be env related)
			return nil, err
		}
		// volume was found, but was not known to SDC. This is abnormal.
		healthy = false
		if message == "" {
			message = fmt.Sprintf("volume: %s was not mapped to host: %v", volID, err)
		}

	}

	// check if volume path is accessible
	if healthy {
		_, err = os.ReadDir(volPath)
		if err != nil && healthy {
			healthy = false
			message = fmt.Sprintf("volume path: %s is not accessible: %v", volPath, err)
		}
	}

	if healthy {

		// check if path is mounted on node
		mounts, err := getPathMounts(volPath)
		if len(mounts) > 0 {
			for _, m := range mounts {
				if m.Path == volPath {
					Log.Infof("volPath: %s is mounted", volPath)
					mounted = true
				}
			}
		}
		if len(mounts) == 0 || !mounted || err != nil {
			healthy = false
			message = fmt.Sprintf("volPath: %s is not mounted: %v", volPath, err)
		}

	}

	if healthy {

		availableBytes, totalBytes, usedBytes, totalInodes, freeInodes, usedInodes, err := gofsutil.FsInfo(ctx, volPath)
		if err != nil {
			return &csi.NodeGetVolumeStatsResponse{
				Usage: []*csi.VolumeUsage{
					{
						Available: 0,
						Total:     0,
						Used:      0,
						Unit:      csi.VolumeUsage_UNKNOWN,
					},
				},
				VolumeCondition: &csi.VolumeCondition{
					Abnormal: true,
					Message:  fmt.Sprintf("failed to get metrics for volume with error: %v", err),
				},
			}, nil
		}
		return &csi.NodeGetVolumeStatsResponse{
			Usage: []*csi.VolumeUsage{
				{
					Available: availableBytes,
					Total:     totalBytes,
					Used:      usedBytes,
					Unit:      csi.VolumeUsage_BYTES,
				},
				{
					Available: freeInodes,
					Total:     totalInodes,
					Used:      usedInodes,
					Unit:      csi.VolumeUsage_INODES,
				},
			},
			VolumeCondition: &csi.VolumeCondition{
				Abnormal: !healthy,
				Message:  message,
			},
		}, nil

	}

	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{
				Available: 0,
				Total:     0,
				Used:      0,
				Unit:      csi.VolumeUsage_UNKNOWN,
			},
		},
		VolumeCondition: &csi.VolumeCondition{
			Abnormal: !healthy,
			Message:  message,
		},
	}, nil
}

func (s *service) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	var reqID string
	var err error
	headers, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if req, ok := headers["csi.requestid"]; ok && len(req) > 0 {
			reqID = req[0]
		}
	}

	err = s.nodeProbe(ctx)
	if err != nil {
		Log.Error("nodeProbe failed with error :" + err.Error())
		//return nil, err
	}

	volumePath := req.GetVolumePath()
	if volumePath == "" {
		Log.Error("Volume path required")
		return nil, status.Error(codes.InvalidArgument,
			"Volume path required")
	}

	// Check if volume path is a directory.
	// Mount type volumes are always mounted on a directory.
	// If not a directory, assume it's a raw block device mount and return ok.
	volumePathInfo, err := os.Lstat(volumePath)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "Could not stat volume path: "+volumePath)
	}
	if !volumePathInfo.Mode().IsDir() {
		Log.Infof("Volume path %s is not a directory- assuming a raw block device mount", volumePath)
		return &csi.NodeExpandVolumeResponse{}, nil
	}

	csiVolID := req.GetVolumeId()
	if csiVolID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"volume ID is required")
	}
	// ensure no ambiguity if legacy vol
	err = s.checkVolumesMap(csiVolID)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"checkVolumesMap for id: %s failed : %s", csiVolID, err.Error())
	}

	volumeID := getVolumeIDFromCsiVolumeID(csiVolID)
	Log.Printf("NodeExpandVolume volumeID: %s", volumeID)

	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"volume ID is required")
	}

	systemID := s.getSystemIDFromCsiVolumeID(csiVolID)
	if systemID == "" {
		// use default system
		systemID = s.opts.defaultSystemID
	}
	Log.Printf("NodeExpandVolume systemID: %s", systemID)
	if systemID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"systemID is not found in the request and there is no default system")
	}

	// Probe the system to make sure it is managed by driver
	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	sdcMappedVolume, err := s.getSDCMappedVol(volumeID, systemID, publishGetMappedVolMaxRetry)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	Log.Infof("sdcMappedVolume %+v", sdcMappedVolume)
	sdcDevice := strings.Replace(sdcMappedVolume.SdcDevice, "/dev/", "", 1)
	Log.Infof("sdcDevice %s", sdcDevice)
	devicePath := sdcMappedVolume.SdcDevice
	Log.Infof("devicePath %s", devicePath)
	size := req.GetCapacityRange().GetRequiredBytes()

	f := logrus.Fields{
		"CSIRequestID": reqID,
		"DevicePath":   devicePath,
		"VolumeID":     csiVolID,
		"VolumePath":   volumePath,
		"Size":         size,
	}
	Log.WithFields(f).Info("resizing volume")

	rc, err := goscaleio.DrvCfgQueryRescan()
	Log.Infof("Rescan all SDC devices")
	if err != nil {
		Log.Errorf("Rescan failed with ioctl error code %s with error %s, Run rescan manually on Powerflex host", rc, err.Error())
	}

	fsType, err := gofsutil.FindFSType(context.Background(), volumePath)
	if err != nil {
		Log.Errorf("Failed to fetch filesystem type for mount (%s) with error (%s)", volumePath, err.Error())
		return nil, status.Error(codes.Internal, err.Error())
	}
	Log.Infof("Found %s filesystem mounted on volume %s", fsType, volumePath)

	// Resize the filesystem
	err = gofsutil.ResizeFS(context.Background(), volumePath, devicePath, "", "", fsType)
	if err != nil {
		Log.Errorf("Failed to resize filesystem: mountpoint (%s) device (%s) with error (%s)",
			volumePath, devicePath, err.Error())
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodeExpandVolumeResponse{}, nil
}

func getNodelabels(ctx context.Context, s *service) (map[string]string, error) {
	return s.GetNodeLabels(ctx)
}

func getNodeUID(ctx context.Context, s *service) (string, error) {
	return s.GetNodeUID(ctx)
}
