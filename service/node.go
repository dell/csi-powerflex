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
)

const (
	drvCfg = "/opt/emc/scaleio/sdc/bin/drv_cfg"
)

var (
	getMappedVolMaxRetry = 30
	connectedSystemID    = make([]string, 0)
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

	id := req.GetVolumeId()
	log.Printf("NodePublishVolume id: %s", id)

	sdcMappedVol, err := s.getSDCMappedVol(id)
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
	id := req.GetVolumeId()
	log.Printf("NodeUnublishVolume id: %s", id)

	sdcMappedVol, err := s.getSDCMappedVol(id)
	if err != nil {
		// fix k8s 19 bug: ControllerUnpublishVolume is called before NodeUnpublishVolume
		_ = gofsutil.Unmount(ctx, targetPath)

		// Idempotent need to return ok if not published
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	if err := unpublishVolume(req, s.privDir, sdcMappedVol.SdcDevice, reqID); err != nil {
		return nil, err
	}

	_ = gofsutil.Unmount(ctx, targetPath)

	if err := removeWithRetry(targetPath); err != nil {
		log.Errorf("Unable to remove target path: %v", err)
	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (s *service) getSDCMappedVol(volumeID string) (*goscaleio.SdcMappedVolume, error) {
	// If not found immediately, give a little time for controller to
	// communicate with SDC that it has volume
	var sdcMappedVol *goscaleio.SdcMappedVolume
	var err error
	for i := 0; i < getMappedVolMaxRetry; i++ {
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

func (s *service) getAllSystems(ctx context.Context, systems []string) error {
	// Create our ScaleIO API client, if needed
	if s.adminClient == nil {
		// create a new client
		c, err := goscaleio.NewClientWithArgs(
			s.opts.Endpoint, "", s.opts.Insecure, !s.opts.DisableCerts)
		if err != nil {
			e := fmt.Errorf("Unable to create ScaleIO client: %s", err.Error())
			log.Error(e)
			return e
		}
		// authenticate to this client
		_, err = c.Authenticate(&goscaleio.ConfigConnect{
			Endpoint: s.opts.Endpoint,
			Username: s.opts.User,
			Password: s.opts.Password,
		})
		if err != nil {
			e := fmt.Errorf("Unable to create ScaleIO client: %s", err.Error())
			log.Error(e)
			return e
		}
		// success! Save the client for later use
		s.adminClient = c
	}

	// get the systemNames for all of the systemIDs in connectedSystemID
	if s.adminClient != nil {
		connectedSystemName := make([]string, 0)
		for _, i := range systems {
			sys, err := s.adminClient.FindSystem(i, i, "")
			if err != nil {
				// could not find the name for this system. Log a message and keep going
				e := fmt.Errorf("Unable to find VxFlex OS system name matching system ID: %s. Error is %v", i, err)
				log.Error(e)
			} else {
				if sys.System == nil || sys.System.Name == "" {
					// system does not have a name, this is fine
					log.Printf("Found system without a name, system ID: %s", i)
				} else {
					log.Printf("Found system Name: %s", sys.System.Name)
					connectedSystemName = append(connectedSystemName, sys.System.Name)
				}
			}
		}
		for _, n := range connectedSystemName {
			connectedSystemID = append(connectedSystemID, n)
		}
	}
	return nil
}

func (s *service) nodeProbe(ctx context.Context) error {

	// make sure the kernel module is loaded
	if !kmodLoaded(s.opts) {
		return status.Error(codes.FailedPrecondition,
			"scini kernel module not loaded")
	}

	// fetch the SDC GUID
	if s.opts.SdcGUID == "" {
		// try to get GUID using `drv_cfg` binary
		if _, err := os.Stat(drvCfg); os.IsNotExist(err) {
			return status.Error(codes.FailedPrecondition,
				"unable to get SDC GUID via config or drv_cfg binary")
		}

		out, err := exec.Command(drvCfg, "--query_guid").CombinedOutput()
		if err != nil {
			return status.Errorf(codes.FailedPrecondition,
				"error getting SDC GUID: %s", err.Error())
		}

		s.opts.SdcGUID = strings.TrimSpace(string(out))
		log.WithField("guid", s.opts.SdcGUID).Info("set SDC GUID")
	}

	// fetch the systemIDs
	var err error
	connectedSystemID, err = getSystemsKnownToSDC(s.opts)
	if err != nil {
		return status.Errorf(codes.FailedPrecondition, "%s", err)
	}

	// get all the system names and IDs.
	// ignore the errors here as all the information is supplementary
	/* #nosec G104 */
	s.getAllSystems(ctx, connectedSystemID)

	// make sure privDir is pre-created
	if _, err := mkdir(s.privDir); err != nil {
		return status.Errorf(codes.Internal,
			"plugin private dir: %s creation error: %s",
			s.privDir, err.Error())
	}

	return nil
}

// getStringInBetween returns empty string if no start or end string found
func getStringInBetween(str string, start string, end string) (result string) {
	s := strings.Index(str, start)
	if s == -1 {
		return
	}
	s += len(start)
	e := strings.Index(str[s:], end)
	if e == -1 {
		return
	}

	contents := str[s : s+e]
	return strings.TrimSpace(contents)
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
	var out []byte
	var err error
	systems := make([]string, 0)

	// fetch the systemIDs
	if opts.drvCfgQueryMDM == "" {
		// try to get system name using `drv_cfg` binary
		if _, err := os.Stat(drvCfg); os.IsNotExist(err) {
			return systems, status.Error(codes.FailedPrecondition,
				"unable to get System Name via config or drv_cfg binary")
		}

		out, err = exec.Command(drvCfg, "--query_mdms").CombinedOutput()
		if err != nil {
			return systems, status.Errorf(codes.FailedPrecondition,
				"error getting System ID: %s", err.Error())
		}
	} else {
		out = []byte(opts.drvCfgQueryMDM)
	}

	r := bytes.NewReader(out)
	s := bufio.NewScanner(r)

	for s.Scan() {
		// the System ID is the field titled "Installation ID"
		sysID := getStringInBetween(s.Text(), "MDM-ID", "SDC")
		if sysID != "" {
			systems = append(systems, sysID)
			log.WithField("ID", sysID).Info("Found connected system")
		}
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

	// Get the Node ID
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

	// Get the Node ID
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

	volID := req.GetVolumeId()
	if volID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"Volume ID is required")
	}

	sdcMappedVolume, err := s.getSDCMappedVol(volID)
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
		"VolumeID":     volID,
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
