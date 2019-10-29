package service

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"strings"
	"time"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/goscaleio"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	drvCfg = "/opt/emc/scaleio/sdc/bin/drv_cfg"
)

var (
	getMappedVolMaxRetry = 20
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

	// If not found immediately, give a little time for controller to
	// communicate with SDC that it has volume
	var sdcMappedVol *goscaleio.SdcMappedVolume
	var err error
	for i := 0; i < getMappedVolMaxRetry; i++ {
		sdcMappedVol, err = getMappedVol(id)
		if sdcMappedVol != nil {
			break
		}
		log.Printf("Node publish getMappedVol retry: %d", i)
		time.Sleep(1 * time.Second)
	}
	if err != nil {
		log.Printf("NodePublishVolume returning not published to node: %s", id)
		return nil, err
	}

	if err := publishVolume(req, s.privDir, sdcMappedVol.SdcDevice); err != nil {
		return nil, err
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (s *service) NodeUnpublishVolume(
	ctx context.Context,
	req *csi.NodeUnpublishVolumeRequest) (
	*csi.NodeUnpublishVolumeResponse, error) {

	s.logStatistics()
	id := req.GetVolumeId()
	log.Printf("NodeUnublishVolume id: %s", id)

	sdcMappedVol, err := getMappedVol(id)
	if err != nil {
		// Idempotent need to return ok if not published
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	if err := unpublishVolume(req, s.privDir, sdcMappedVol.SdcDevice); err != nil {
		return nil, err
	}

	privTgt := req.GetTargetPath()
	if privTgt == "" {
		return nil, status.Error(codes.InvalidArgument, "A Staging Target argument is required")
	}
	removeWithRetry(privTgt)

	return &csi.NodeUnpublishVolumeResponse{}, nil
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

func (s *service) nodeProbe(ctx context.Context) error {

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

	if !kmodLoaded(s.opts) {
		return status.Error(codes.FailedPrecondition,
			"scini kernel module not loaded")
	}

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

func (s *service) NodeGetCapabilities(
	ctx context.Context,
	req *csi.NodeGetCapabilitiesRequest) (
	*csi.NodeGetCapabilitiesResponse, error) {

	return &csi.NodeGetCapabilitiesResponse{}, nil
}

// Minimal version of NodeGetInfo. Returns NodeId
// MaxVolumesPerNode (optional) is left as 0 which means unlimited, and AccessibleTopology is left nil.
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
	return &csi.NodeGetInfoResponse{NodeId: s.opts.SdcGUID}, nil
}

func (s *service) NodeGetVolumeStats(
	ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")

}


func (s *service) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "")
}
