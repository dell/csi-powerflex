package service

import (
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"golang.org/x/net/context"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	wrappers "github.com/golang/protobuf/ptypes/wrappers"

	"github.com/dell/csi-vxflexos/core"
)

func (s *service) GetPluginInfo(
	ctx context.Context,
	req *csi.GetPluginInfoRequest) (
	*csi.GetPluginInfoResponse, error) {

	return &csi.GetPluginInfoResponse{
		Name:          Name,
		VendorVersion: core.SemVer,
		Manifest:      Manifest,
	}, nil
}

func (s *service) GetPluginCapabilities(
	ctx context.Context,
	req *csi.GetPluginCapabilitiesRequest) (
	*csi.GetPluginCapabilitiesResponse, error) {

	var rep csi.GetPluginCapabilitiesResponse
	if !strings.EqualFold(s.mode, "node") {
		rep.Capabilities = []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			},
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_VOLUME_ACCESSIBILITY_CONSTRAINTS,
					},
				},
			},
			{
				Type: &csi.PluginCapability_VolumeExpansion_{
					VolumeExpansion: &csi.PluginCapability_VolumeExpansion{
						Type: csi.PluginCapability_VolumeExpansion_ONLINE,
					},
				},
			},
		}
	}
	return &rep, nil
}

func (s *service) Probe(
	ctx context.Context,
	req *csi.ProbeRequest) (
	*csi.ProbeResponse, error) {

	log.Debug("Probe called")
	if !strings.EqualFold(s.mode, "node") {
		log.Debug("systemProbe")
		if err := s.systemProbeAll(ctx); err != nil {
			log.Printf("error in systemProbeAll: %s", err.Error())
			return nil, err
		}
	}
	if !strings.EqualFold(s.mode, "controller") {
		log.Debug("nodeProbe")
		if err := s.nodeProbe(ctx); err != nil {
			log.Printf("error in nodeProbe: %s", err.Error())
			return nil, err
		}
	}
	ready := new(wrappers.BoolValue)
	ready.Value = true
	rep := new(csi.ProbeResponse)
	rep.Ready = ready
	log.Debug(fmt.Sprintf("Probe returning: %v", rep.Ready.GetValue()))

	return rep, nil
}
