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
		if id, ok := s.connectedSystemNameToID[systemID]; ok {
			Log.Printf("Node publish getMappedVol name: %s id: %s", systemID, id)
			systemID = id
		}
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

// getSystemName gets the system name for each system and append it to connectedSystemID variable
func (s *service) getSystemName(ctx context.Context, systems []string) bool {
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
	s.getSystemName(ctx, connectedSystemID)

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

	//these capabilities deal with volume health monitoring, and are only advertised by driver when user sets
	//node.healthMonitor.enabled is set to true in values file
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
// NodeId is the identifier of the node and will match the SDC GUID
// MaxVolumesPerNode (optional) is left as 0 which means unlimited
// AccessibleTopology will be set with the VxFlex OS SystemID
func (s *service) NodeGetInfo(
	ctx context.Context,
	req *csi.NodeGetInfoRequest) (
	*csi.NodeGetInfoResponse, error) {

	// Fetch SDC GUID
	if s.opts.SdcGUID == "" {
		if err := s.nodeProbe(ctx); err != nil {
			return nil, err
		}
	}

	// Fetch Node ID
	if len(connectedSystemID) == 0 {
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

	//validate params first, make sure neither field is empty
	if len(volPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "no volume Path provided")
	}

	if len(csiVolID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "no volume ID  provided")
	}

	//check if volume exists

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
		//volume not known to SDC, next check if it exists at all
		_, _, err := s.listVolumes(systemID, 0, 0, false, false, volID, "")
		if err != nil && strings.Contains(err.Error(), sioGatewayVolumeNotFound) {
			message = fmt.Sprintf("Volume is not found by node driver at %s", time.Now().Format("2006-01-02 15:04:05"))

		} else if err != nil {
			//error was returned, but had nothing to do with the volume not being on the array (may be env related)
			return nil, err
		}
		//volume was found, but was not known to SDC. This is abnormal.
		healthy = false
		if message == "" {
			message = fmt.Sprintf("volume: %s was not mapped to host: %v", volID, err)
		}

	}

	//check if volume path is accessible
	if healthy {
		_, err = os.ReadDir(volPath)
		if err != nil && healthy {
			healthy = false
			message = fmt.Sprintf("volume path: %s is not accessible: %v", volPath, err)
		}
	}

	if healthy {

		//check if path is mounted on node
		mounts, err := getPathMounts(volPath)
		if len(mounts) > 0 {
			for _, m := range mounts {
				if m.Path == volPath {
					Log.Infof("volPath: %s is mounted", volPath)
					mounted = true
				}
			}
		}
		if len(mounts) == 0 || mounted == false || err != nil {
			healthy = false
			message = fmt.Sprintf("volPath: %s is not mounted: %v", volPath, err)
		}

	}

	if healthy {

		availableBytes, totalBytes, usedBytes, totalInodes, freeInodes, usedInodes, err := gofsutil.FsInfo(volPath)
		if err != nil {
			return &csi.NodeGetVolumeStatsResponse{
				Usage: []*csi.VolumeUsage{
					{Available: 0,
						Total: 0,
						Used:  0,
						Unit:  csi.VolumeUsage_UNKNOWN,
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
	err = gofsutil.ResizeFS(context.Background(), volumePath, devicePath, "", fsType)
	if err != nil {
		Log.Errorf("Failed to resize filesystem: mountpoint (%s) device (%s) with error (%s)",
			volumePath, devicePath, err.Error())
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodeExpandVolumeResponse{}, nil
}
