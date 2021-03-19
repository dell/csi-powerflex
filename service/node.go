package service

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/gofsutil"
	"github.com/dell/goscaleio"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"io/ioutil"
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

func (s *service) NodeUnstageVolume(
	ctx context.Context,
	req *csi.NodeUnstageVolumeRequest) (
	*csi.NodeUnstageVolumeResponse, error) {

	return nil, status.Error(codes.Unimplemented, "")
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
		log.Printf("VolumeContext:")
		for key, value := range volumeContext {
			log.Printf("    [%s]=%s", key, value)
		}
	}

	var ephemeralVolume bool
	ephemeral, ok := req.VolumeContext["csi.storage.k8s.io/ephemeral"]
	if ok {
		ephemeralVolume = strings.ToLower(ephemeral) == "true"
	}
	if ephemeralVolume {
		resp, err := s.ephemeralNodePublish(ctx, req)
		if err != nil {
			log.Errorf("ephemeralNodePublish returned error: %v", err)
		}
		return resp, err
	}

	csiVolID := req.GetVolumeId()
	if csiVolID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"volume ID is required")
	}

	volID := getVolumeIDFromCsiVolumeID(csiVolID)
	log.Printf("NodePublishVolume id: %s", volID)
	//ensure no ambiguity if legacy vol
	err := s.checkVolumesMap(csiVolID)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"checkVolumesMap for id: %s failed : %s", csiVolID, err.Error())

	}

	sdcMappedVol, err := s.getSDCMappedVol(volID, publishGetMappedVolMaxRetry)
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
			log.Errorf("NodeUnpublish with ephemeral volume. Was unable to read lockfile: %v", err)
			return nil, status.Error(codes.Internal, "NodeUnpublish with ephemeral volume. Was unable to read lockfile")
		}
		//Convert volume id from []byte to string format
		csiVolID = string(idFromFile)
		log.Infof("Read volume ID: %s from lockfile: %s ", csiVolID, lockFile)

	}

	volID := getVolumeIDFromCsiVolumeID(csiVolID)

	log.Printf("NodeUnublishVolume id: %s", volID)
	//ensure no ambiguity if legacy vol
	err := s.checkVolumesMap(csiVolID)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"checkVolumesMap for id: %s failed : %s", csiVolID, err.Error())

	}

	sdcMappedVol, err := s.getSDCMappedVol(volID, unpublishGetMappedVolMaxRetry)

	log.Infof("Err from getSDCMappedVol is: %v", err)

	if err != nil {
		log.Infof("Err from getSDCMappedVol is: %s", err.Error())
		// fix k8s 19 bug: ControllerUnpublishVolume is called before NodeUnpublishVolume
		// cleanup target from pod
		if err := gofsutil.Unmount(ctx, targetPath); err != nil {
			log.Errorf("cleanup target mount: %s", err.Error())
		}

		if err := removeWithRetry(targetPath); err != nil {
			log.Errorf("cleanup target path: %s", err.Error())
		}
		// dont cleanup pvtMount in case it is in use elsewhere on the node

		if ephemeralVolume {
			log.Info("Detected ephemeral")
			err := s.ephemeralNodeUnpublish(ctx, req)
			if err != nil {
				log.Errorf("ephemeralNodeUnpublish returned error: %s", err.Error())
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
		log.Info("Detected ephemeral")
		err := s.ephemeralNodeUnpublish(ctx, req)
		if err != nil {
			log.Errorf("ephemeralNodeUnpublish returned error: %v", err)
			return nil, err
		}

	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// Get sdc mapped volume from the given volume ID
func (s *service) getSDCMappedVol(volumeID string, maxRetry int) (*goscaleio.SdcMappedVolume, error) {
	// If not found immediately, give a little time for controller to
	// communicate with SDC that it has volume
	var sdcMappedVol *goscaleio.SdcMappedVolume
	var err error
	for i := 0; i < maxRetry; i++ {
		sdcMappedVol, err = getMappedVol(volumeID)
		if sdcMappedVol != nil {
			break
		}
		log.Printf("Node publish getMappedVol retry: %d", i)
		time.Sleep(1 * time.Second)
	}
	if err != nil {
		log.Printf("SDC returned volume %s not published to node", volumeID)
		return nil, err
	}
	return sdcMappedVol, err
}

// Get the volumes published to the SDC (given by SdcMappedVolume) and scan for requested vol id
func getMappedVol(id string) (*goscaleio.SdcMappedVolume, error) {
	// get source path of volume/device
	localVols, err := goscaleio.GetLocalVolumeMap()
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"unable to get locally mapped ScaleIO volumes: %s",
			err.Error())
	}
	var sdcMappedVol *goscaleio.SdcMappedVolume
	if len(localVols) == 0 {
		log.Printf("Length of localVols (goscaleio.GetLocalVolumeMap()) is 0 \n")
	}
	for _, v := range localVols {
		if v.VolumeID == id {
			sdcMappedVol = v
			break
		}
	}
	if sdcMappedVol == nil {
		return nil, status.Errorf(codes.Unavailable,
			"volume: %s not published to node", id)
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
				log.Error(e)
				return e
			}
			adminClient := s.adminClients[array.SystemID]
			sys, err := adminClient.FindSystem(array.SystemID, array.SystemID, "")
			if err != nil {
				// could not find the name for this system. Log a message and keep going
				e := fmt.Errorf("Unable to find VxFlex OS system name matching system ID: %s. Error is %v", array.SystemID, err)
				log.Error(e)
			} else {
				if sys.System == nil || sys.System.Name == "" {
					// system does not have a name, this is fine
					log.Printf("Found system without a name, system ID: %s", array.SystemID)
				} else {
					log.Printf("Found system Name: %s", sys.System.Name)
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
		log.WithField("guid", s.opts.SdcGUID).Info("set SDC GUID")
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
			log.WithError(err).Error("error from lsmod")
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

	for _, s := range *discoveredSystems {
		systems = append(systems, s.SystemID)
		log.WithField("ID", s.SystemID).Info("Found connected system")
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
		log.Error("nodeProbe failed with error :" + err.Error())
		return nil, err
	}

	volumePath := req.GetVolumePath()
	if volumePath == "" {
		log.Error("Volume path required")
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
		log.Infof("Volume path %s is not a directory- assuming a raw block device mount", volumePath)
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

	volID := getVolumeIDFromCsiVolumeID(csiVolID)

	if volID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"volume ID is required")
	}

	sdcMappedVolume, err := s.getSDCMappedVol(volID, publishGetMappedVolMaxRetry)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	log.Infof("sdcMappedVolume %+v", sdcMappedVolume)
	sdcDevice := strings.Replace(sdcMappedVolume.SdcDevice, "/dev/", "", 1)
	log.Infof("sdcDevice %s", sdcDevice)
	devicePath := sdcMappedVolume.SdcDevice

	size := req.GetCapacityRange().GetRequiredBytes()

	f := log.Fields{
		"CSIRequestID": reqID,
		"DevicePath":   devicePath,
		"VolumeID":     csiVolID,
		"VolumePath":   volumePath,
		"Size":         size,
	}
	log.WithFields(f).Info("resizing volume")
	fsType, err := gofsutil.FindFSType(context.Background(), volumePath)
	if err != nil {
		log.Errorf("Failed to fetch filesystem type for mount (%s) with error (%s)", volumePath, err.Error())
		return nil, status.Error(codes.Internal, err.Error())
	}
	log.Infof("Found %s filesystem mounted on volume %s", fsType, volumePath)

	// Resize the filesystem
	err = gofsutil.ResizeFS(context.Background(), volumePath, devicePath, "", fsType)
	if err != nil {
		log.Errorf("Failed to resize filesystem: mountpoint (%s) device (%s) with error (%s)",
			volumePath, devicePath, err.Error())
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodeExpandVolumeResponse{}, nil
}
