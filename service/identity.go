// Copyright © 2019-2026 Dell Inc. or its subsidiaries. All Rights Reserved.
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
	"strings"

	csmlog "github.com/Ecosystems/container-storage-modules/src/csmlog"
	commonext "github.com/Ecosystems/container-storage-modules/src/dell-csi-extensions/common"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func (s *service) GetPluginInfo(
	_ context.Context,
	_ *csi.GetPluginInfoRequest) (
	*csi.GetPluginInfoResponse, error,
) {
	return &csi.GetPluginInfoResponse{
		Name:          Name,
		VendorVersion: ManifestSemver,
		Manifest:      Manifest,
	}, nil
}

func (s *service) GetPluginCapabilities(
	_ context.Context,
	_ *csi.GetPluginCapabilitiesRequest) (
	*csi.GetPluginCapabilitiesResponse, error,
) {
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
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_GROUP_CONTROLLER_SERVICE,
					},
				},
			},
		}
	}
	return &rep, nil
}

func (s *service) Probe(
	ctx context.Context,
	_ *csi.ProbeRequest) (
	*csi.ProbeResponse, error,
) {
	if !strings.EqualFold(s.mode, "node") {
		csmlog.Debug("systemProbe")
		if err := s.systemProbeAll(ctx); err != nil {
			csmlog.WithContext(ctx).Errorf("system probe failed: %v", err)
			return nil, err
		}
	}
	if !strings.EqualFold(s.mode, "controller") {
		csmlog.Debug("nodeProbe")
		if err := s.nodeProbe(ctx); err != nil {
			csmlog.WithContext(ctx).Errorf("node probe failed: %v", err)
			return nil, err
		}
	}
	rep := &csi.ProbeResponse{
		Ready: wrapperspb.Bool(true),
	}
	csmlog.Debug(fmt.Sprintf("Probe returning: %v", rep.Ready.GetValue()))

	return rep, nil
}

func (s *service) ProbeController(ctx context.Context,
	_ *commonext.ProbeControllerRequest) (
	*commonext.ProbeControllerResponse, error,
) {
	if !strings.EqualFold(s.mode, "node") {
		csmlog.WithContext(ctx).Debug("systemProbe")
		if err := s.systemProbeAll(ctx); err != nil {
			csmlog.WithContext(ctx).Infof("error in systemProbeAll: %s", err.Error())
			return nil, err
		}
	}

	rep := new(commonext.ProbeControllerResponse)
	rep.Name = Name
	rep.VendorVersion = ManifestSemver
	Manifest["semver"] = ManifestSemver

	rep.Manifest = Manifest

	return rep, nil
}
