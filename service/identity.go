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
	"fmt"
	"strings"

	"golang.org/x/net/context"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	commonext "github.com/dell/dell-csi-extensions/common"
	wrappers "github.com/golang/protobuf/ptypes/wrappers"

	"github.com/dell/csi-vxflexos/v2/core"
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

	Log.Debug("Probe called")
	if !strings.EqualFold(s.mode, "node") {
		Log.Debug("systemProbe")
		if err := s.systemProbeAll(ctx); err != nil {
			Log.Printf("error in systemProbeAll: %s", err.Error())
			return nil, err
		}
	}
	if !strings.EqualFold(s.mode, "controller") {
		Log.Debug("nodeProbe")
		if err := s.nodeProbe(ctx); err != nil {
			Log.Printf("error in nodeProbe: %s", err.Error())
			return nil, err
		}
	}
	ready := new(wrappers.BoolValue)
	ready.Value = true
	rep := new(csi.ProbeResponse)
	rep.Ready = ready
	Log.Debug(fmt.Sprintf("Probe returning: %v", rep.Ready.GetValue()))

	return rep, nil
}

func (s *service) ProbeController(ctx context.Context,
	req *commonext.ProbeControllerRequest) (
	*commonext.ProbeControllerResponse, error) {

	if !strings.EqualFold(s.mode, "node") {
		Log.Debug("systemProbe")
		if err := s.systemProbeAll(ctx); err != nil {
			Log.Printf("error in systemProbeAll: %s", err.Error())
			return nil, err
		}
	}

	rep := new(commonext.ProbeControllerResponse)
	rep.Name = Name
	rep.VendorVersion = core.SemVer
	rep.Manifest = Manifest

	return rep, nil
}
