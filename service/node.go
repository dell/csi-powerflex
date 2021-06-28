package service

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"io/ioutil"

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
)

func (s *service) NodeStageVolume(
	ctx context.Context,
	req *csi.NodeStageVolumeRequest) (
	*csi.NodeStageVolumeResponse, error) {

	return nil, status.Error(codes.Unimplemented, "")
}

// NodeUnstageVolume will cleanup the staging path passed in the request.
// This will only be called by CSM-resliency (podmon), as the driver does not advertise support for STAGE_UNSTAGE_VOLUME in NodeGetCapabilities,
// therefore Kubernetes will not call it.
func (s *service) NodeUnstageVolume(
	ctx context.Context,
	req *csi.NodeUnstageVolumeRequest) (
	*csi.NodeUnstageVolumeResponse, error) {

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
	*csi.NodePublishVolumeResponse, error) {

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

	volID := getVolumeIDFromCsiVolumeID(csiVolID)
	Log.Printf("[NodePublishVolume] volumeID: %s", volID)

	systemID := getSystemIDFromCsiVolumeID(csiVolID)
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

	//ensure no ambiguity if legacy vol
	err := s.checkVolumesMap(csiVolID)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"checkVolumesMap for id: %s failed : %s", csiVolID, err.Error())

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
	*csi.NodeUnpublishVolumeResponse, error) {

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

	var ephemeralVolume bool
	//For ephemeral volumes, kubernetes gives us an internal ID, so we need to use the lockfile to find the Powerflex ID this is mapped to.
	lockFile := ephemeralStagingMountPath + csiVolID + "/id"
	if s.fileExist(lockFile) {
		ephemeralVolume = true
		//while a file is being read from, it's a file determined by volID and is written by the driver
		/* #nosec G304 */
		idFromFile, err := ioutil.ReadFile(lockFile)
		if err != nil && os.IsNotExist(err) {
			Log.Errorf("NodeUnpublish with ephemeral volume. Was unable to read lockfile: %v", err)
			return nil, status.Error(codes.Internal, "NodeUnpublish with ephemeral volume. Was unable to read lockfile")
		}
		//Convert volume id from []byte to string format
		csiVolID = string(idFromFile)
		Log.Infof("Read volume ID: %s from lockfile: %s ", csiVolID, lockFile)

	}

	volID := getVolumeIDFromCsiVolumeID(csiVolID)
	Log.Printf("NodeUnpublishVolume volumeID: %s", volID)

	systemID := getSystemIDFromCsiVolumeID(csiVolID)
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

	//ensure no ambiguity if legacy vol
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
		sdcMappedVol, err = getMappedVol(volumeID, systemID)
		if sdcMappedVol != nil {
			break
		}
		Log.Printf("Node publish getMappedVol retry: %d", i)
		time.Sleep(1 * time.Second)
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

// getDefaultSystemName gets the system name for the default system and append it to connectedSystemID variable
func (s *service) getDefaultSystemName(ctx context.Context, systems []string) error {
	for _, system := range systems {
		array := s.opts.arrays[system]
		if array != nil && array.IsDefault {
			// makes sure it has ScleIO API client
			err := s.systemProbe(ctx, array)
			if err != nil {
				// Could not probe system. Log a message and return
				e := fmt.Errorf("Unable to probe system with ID: %s. Error is %v", array.SystemID, err)
				Log.Error(e)
				return e
			}
			adminClient := s.adminClients[array.SystemID]
			sys, err := adminClient.FindSystem(array.SystemID, array.SystemID, "")
			if err != nil {
				// could not find the name for this system. Log a message and keep going
				e := fmt.Errorf("Unable to find VxFlex OS system name matching system ID: %s. Error is %v", array.SystemID, err)
				Log.Error(e)
			} else {
				if sys.System == nil || sys.System.Name == "" {
					// system does not have a name, this is fine
					Log.Printf("Found system without a name, system ID: %s", array.SystemID)
				} else {
					Log.Printf("Found system Name: %s", sys.System.Name)
					connectedSystemID = append(connectedSystemID, sys.System.Name)
				}
			}
			break
		}
	}
	return nil
}

// nodeProbe fetchs the SDC GUID by drv_cfg and the systemIDs/names by getDefaultSystemName method.
// It also makes sure private directory(privDir) is created
func (s *service) nodeProbe(ctx context.Context) error {

	// make sure the kernel module is loaded
	if !kmodLoaded(s.opts) {
		return status.Error(codes.FailedPrecondition,
			"scini kernel module not loaded")
	}

	// fetch the SDC GUID
	if s.opts.SdcGUID == "" {
		// try to query the SDC GUID
		guid, err := goscaleio.DrvCfgQueryGUID()

		if err != nil {
			return status.Error(codes.FailedPrecondition,
				"unable to get SDC GUID via config or automatically")
		}

		s.opts.SdcGUID = guid
		Log.WithField("guid", s.opts.SdcGUID).Info("set SDC GUID")
	}

	// fetch the systemIDs
	var err error
	if len(connectedSystemID) == 0 {
		connectedSystemID, err = getSystemsKnownToSDC(s.opts)
		if err != nil {
			return status.Errorf(codes.FailedPrecondition, "%s", err)
		}
	}

	// get all the system names and IDs.
	// ignore the errors here as all the information is supplementary
	/* #nosec G104 */
	s.getDefaultSystemName(ctx, connectedSystemID)

	// make sure privDir is pre-created
	if _, err := mkdir(s.privDir); err != nil {
		return status.Errorf(codes.Internal,
			"plugin private dir: %s creation error: %s",
			s.privDir, err.Error())
	}

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

func getSystemsKnownToSDC(opts Opts) ([]string, error) {
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
	ctx context.Context,
	req *csi.NodeGetCapabilitiesRequest) (
	*csi.NodeGetCapabilitiesResponse, error) {

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
					},
				},
			},
		},
	}, nil

}

// NodeGetInfo returns Node information
// NodeId is the identifier of the node and will match the SDC GUID
// MaxVolumesPerNode (optional) is left as 0 which means unlimited
// AccessibleTopology will be set with the VxFlex OS SystemID
func (s *service) NodeGetInfo(
	ctx context.Context,
	req *csi.NodeGetInfoRequest) (
	*csi.NodeGetInfoResponse, error) {

	// Fetch SDC GUID
	if s.opts.SdcGUID == "" {
		if !s.opts.AutoProbe {
			return nil, status.Error(codes.FailedPrecondition,
				"Unable to get Node ID. Either it is not configured, "+
					"or Node Service has not been probed")
		}
		if err := s.nodeProbe(ctx); err != nil {
			return nil, err
		}
	}

	// Fetch Node ID
	if len(connectedSystemID) == 0 {
		if !s.opts.AutoProbe {
			return nil, status.Error(codes.FailedPrecondition,
				"Unable to get Node ID. Either it is not configured, "+
					"or Node Service has not been probed")
		}
		if err := s.nodeProbe(ctx); err != nil {
			return nil, err
		}
	}

	// Create the topology keys
	// csi-vxflexos.dellemc.com/<systemID>: <provisionerName>
	topology := map[string]string{}
	for _, sysID := range connectedSystemID {
		topology[Name+"/"+sysID] = SystemTopologySystemValue
	}

	return &csi.NodeGetInfoResponse{
		NodeId: s.opts.SdcGUID,
		AccessibleTopology: &csi.Topology{
			Segments: topology,
		},
	}, nil
}

func (s *service) NodeGetVolumeStats(
	ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")

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
		return nil, err
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
	//ensure no ambiguity if legacy vol
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

	systemID := getSystemIDFromCsiVolumeID(csiVolID)
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
	err = gofsutil.ResizeFS(context.Background(), volumePath, devicePath, "", fsType)
	if err != nil {
		Log.Errorf("Failed to resize filesystem: mountpoint (%s) device (%s) with error (%s)",
			volumePath, devicePath, err.Error())
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodeExpandVolumeResponse{}, nil
}
