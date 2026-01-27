// Copyright © 2025 Dell Inc. or its subsidiaries. All Rights Reserved.
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
	"strconv"

	"github.com/dell/goscaleio"
	siotypes "github.com/dell/goscaleio/types/v1"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// VolumePublisher allows to publish a volume
type VolumePublisher interface {
	// Publish does the steps necessary for volume to be available on the node
	Publish(ctx context.Context, req *csi.ControllerPublishVolumeRequest, adminClient *goscaleio.Client, systemID, csiVolID string) (*csi.ControllerPublishVolumeResponse, error)
}

// NVMePublisher implementation of VolumePublisher for NVMe volumes
type NVMePublisher struct {
	svc *service
	vol *siotypes.Volume
}

func (p *NVMePublisher) Publish(ctx context.Context, req *csi.ControllerPublishVolumeRequest, adminClient *goscaleio.Client, systemID, csiVolID string) (*csi.ControllerPublishVolumeResponse, error) {
	log.Debugf("ControllerPublish - in NVMePublisher")
	volumeContext := req.GetVolumeContext()
	nodeID := req.GetNodeId()
	am := req.GetVolumeCapability().GetAccessMode()
	vcs := []*csi.VolumeCapability{req.GetVolumeCapability()}
	isBlock := accTypeIsBlock(vcs)

	nvmeHost, err := p.svc.systems[systemID].FindSdc("Name", nodeID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error finding NVMe host %s. Error: %s", nodeID, err.Error())
	}

	if len(p.vol.MappedSdcInfo) > 0 {
		for _, mappedSdcInfo := range p.vol.MappedSdcInfo {
			if mappedSdcInfo.SdcName == nvmeHost.Sdc.Name {
				log.Debug("volume already mapped")
				if err := validateAndCompareQoS(volumeContext, p.vol.Name, mappedSdcInfo); err != nil {
					return nil, err
				}
				return &csi.ControllerPublishVolumeResponse{}, nil
			}
		}
		if isSingleNodeAccess(am) {
			return nil, status.Errorf(codes.FailedPrecondition, "volume already published to NVMe host: %s", p.vol.MappedSdcInfo[0].SdcName)
		}
	}

	// All remaining cases are MULTI_NODE:
	// This original code precludes block multi-writers,
	// and is based on a faulty test that the Volume MappingToAllSdcsEnabled
	// attribute must be set to allow multiple writers, which is not true.
	// The proper way to control multiple mappings is with the allowMultipleMappings
	// attribute passed in the MapVolumeSdcParameter. Unfortunately you cannot
	// read this parameter back.
	allowMultipleMappings, err := shouldAllowMultipleMappings(isBlock, am)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s", err.Error())
	}

	if err := validateAccessType(am, isBlock); err != nil {
		return nil, err
	}

	// Publish volume to NVMe host
	targetVolume := goscaleio.NewVolume(adminClient)
	targetVolume.Volume = &siotypes.Volume{ID: p.vol.ID}

	mapVolumeNVMeParam := &siotypes.MapVolumeNVMeParam{
		HostID:                nvmeHost.Sdc.ID,
		AllowMultipleMappings: allowMultipleMappings,
		AllHosts:              "",
	}

	err = targetVolume.MapVolumeNVMe(mapVolumeNVMeParam)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error mapping volume to nvme host %s. Error: %s", req.NodeId, err.Error())
	}

	if err := setQoSIfNeeded(ctx, p.svc, systemID, nvmeHost.Sdc.ID, p.vol.Name, csiVolID, nodeID, volumeContext); err != nil {
		return nil, err
	}

	return &csi.ControllerPublishVolumeResponse{}, nil
}

// SDCPublisher implementation of VolumePublisher for SDC volumes
type SDCPublisher struct {
	svc *service
	vol *siotypes.Volume
}

func (p *SDCPublisher) Publish(ctx context.Context, req *csi.ControllerPublishVolumeRequest, adminClient *goscaleio.Client, systemID, csiVolID string) (*csi.ControllerPublishVolumeResponse, error) {
	log.Debugf("ControllerPublish - in SDCPublisher")
	volumeContext := req.GetVolumeContext()
	nodeID := req.GetNodeId()
	am := req.GetVolumeCapability().GetAccessMode()
	vcs := []*csi.VolumeCapability{req.GetVolumeCapability()}
	isBlock := accTypeIsBlock(vcs)

	sdcID, err := p.svc.getSDCID(nodeID, systemID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%s", err.Error())
	}

	if len(p.vol.MappedSdcInfo) > 0 {
		for _, mappedSdcInfo := range p.vol.MappedSdcInfo {
			if mappedSdcInfo.SdcID == sdcID {
				log.Debug("volume already mapped")
				if err := validateAndCompareQoS(volumeContext, p.vol.Name, mappedSdcInfo); err != nil {
					return nil, err
				}
				return &csi.ControllerPublishVolumeResponse{}, nil
			}
		}
		if isSingleNodeAccess(am) {
			return nil, status.Errorf(codes.FailedPrecondition, "volume already published to SDC id: %s", p.vol.MappedSdcInfo[0].SdcID)
		}
	}

	// All remaining cases are MULTI_NODE:
	// This original code precludes block multi-writers,
	// and is based on a faulty test that the Volume MappingToAllSdcsEnabled
	// attribute must be set to allow multiple writers, which is not true.
	// The proper way to control multiple mappings is with the allowMultipleMappings
	// attribute passed in the MapVolumeSdcParameter. Unfortunately you cannot
	// read this parameter back.
	allowMultipleMappings, err := shouldAllowMultipleMappings(isBlock, am)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s", err.Error())
	}

	if err := validateAccessType(am, isBlock); err != nil {
		return nil, err
	}

	// Publish volume to SDC
	targetVolume := goscaleio.NewVolume(adminClient)
	targetVolume.Volume = &siotypes.Volume{ID: p.vol.ID}

	mapVolumeSdcParam := &siotypes.MapVolumeSdcParam{
		SdcID:                 sdcID,
		AllowMultipleMappings: allowMultipleMappings,
		AllSdcs:               "",
	}
	err = targetVolume.MapVolumeSdc(mapVolumeSdcParam)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error mapping volume to node: %s", err.Error())
	}

	if err := setQoSIfNeeded(ctx, p.svc, systemID, sdcID, p.vol.Name, csiVolID, nodeID, volumeContext); err != nil {
		return nil, err
	}

	return &csi.ControllerPublishVolumeResponse{}, nil
}

func validateAndCompareQoS(volumeContext map[string]string, volName string, sdcInfo *siotypes.MappedSdcInfo) error {
	bandwidthLimit := volumeContext[KeyBandwidthLimitInKbps]
	iopsLimit := volumeContext[KeyIopsLimit]

	if err := validateQoSParameters(bandwidthLimit, iopsLimit, volName); err != nil {
		return err
	}

	if len(bandwidthLimit) > 0 && strconv.Itoa(sdcInfo.LimitBwInMbps*1024) != bandwidthLimit {
		return status.Errorf(codes.InvalidArgument,
			"volume %s already published with bandwidth limit: %d, but does not match the requested bandwidth limit: %s",
			volName, sdcInfo.LimitBwInMbps*1024, bandwidthLimit)
	}
	if len(iopsLimit) > 0 && strconv.Itoa(sdcInfo.LimitIops) != iopsLimit {
		return status.Errorf(codes.InvalidArgument,
			"volume %s already published with IOPS limit: %d, but does not match the requested IOPS limits: %s",
			volName, sdcInfo.LimitIops, iopsLimit)
	}
	return nil
}

// isSingleNodeAccess checks if the given access mode is a single node access mode.
func isSingleNodeAccess(am *csi.VolumeCapability_AccessMode) bool {
	switch am.Mode {
	case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY:
		return true
	}
	return false
}

func setQoSIfNeeded(ctx context.Context, svc *service, systemID, sdcID, volName, csiVolID, nodeID string, volumeContext map[string]string) error {
	bandwidthLimit := volumeContext[KeyBandwidthLimitInKbps]
	iopsLimit := volumeContext[KeyIopsLimit]

	// validate requested QoS parameters
	if err := validateQoSParameters(bandwidthLimit, iopsLimit, volName); err != nil {
		return err
	}

	// check for atleast one of the QoS params should exist in storage class
	if len(bandwidthLimit) > 0 || len(iopsLimit) > 0 {
		return svc.setQoSParameters(ctx, systemID, sdcID, bandwidthLimit, iopsLimit, volName, csiVolID, nodeID)
	}
	return nil
}
