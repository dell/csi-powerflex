// Copyright © 2024 Dell Inc. or its subsidiaries. All Rights Reserved.
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

package collectors

import (
	"context"
	"fmt"

	goscaleio "github.com/Ecosystems/container-storage-modules/src/goscaleio"
	siotypes "github.com/Ecosystems/container-storage-modules/src/goscaleio/types/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// PowerFlexVolumeClient implements VolumeClient interface using goscaleio client.
type PowerFlexVolumeClient struct {
	client    *goscaleio.Client
	k8sClient kubernetes.Interface
	metadata  VolumeMetadataProvider
	systemID  string
}

// NewPowerFlexVolumeClient creates a new PowerFlexVolumeClient.
func NewPowerFlexVolumeClient(client *goscaleio.Client, k8sClient kubernetes.Interface, metadata VolumeMetadataProvider, systemID string) *PowerFlexVolumeClient {
	return &PowerFlexVolumeClient{
		client:    client,
		k8sClient: k8sClient,
		metadata:  metadata,
		systemID:  systemID,
	}
}

// GetVolumes fetches all volumes from PowerFlex and enriches with Kubernetes metadata.
func (p *PowerFlexVolumeClient) GetVolumes(ctx context.Context) ([]VolumeInfo, error) {
	managedFilterEnabled := false
	if p.metadata != nil && p.metadata.Available() {
		if err := p.metadata.RefreshCache(ctx); err == nil {
			managedFilterEnabled = true
		}
	}

	// Get all volumes from PowerFlex
	// GetVolume("href", "volumeID", "ancestorVolumeID", "volumeName", "snapshotOnly")
	var volumes []*siotypes.Volume
	if err := withGoscaleIOContext(ctx, p.client, func() error {
		var err error
		volumes, err = p.client.GetVolume("", "", "", "", false)
		if err != nil {
			return fmt.Errorf("failed to get volumes from PowerFlex: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if managedFilterEnabled {
		filtered := make([]*siotypes.Volume, 0, len(volumes))
		for _, vol := range volumes {
			managed, err := p.metadata.IsDriverManaged(ctx, vol.ID)
			if err != nil || !managed {
				continue
			}
			filtered = append(filtered, vol)
		}
		volumes = filtered
	}

	// Build PV lookup map
	pvMap := make(map[string]*v1.PersistentVolume)
	if p.k8sClient != nil {
		pvList, err := p.k8sClient.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
		if err == nil {
			for i := range pvList.Items {
				pv := &pvList.Items[i]
				if pv.Spec.CSI != nil && pv.Spec.CSI.Driver == "csi-vxflexos.dellemc.com" {
					volumeHandle := pv.Spec.CSI.VolumeHandle
					pvMap[volumeHandle] = pv
				}
			}
		}
	}

	isGen2, err := p.isGen2System(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to detect PowerFlex generation: %w", err)
	}

	result := make([]VolumeInfo, 0, len(volumes))
	for _, vol := range volumes {
		info := p.convertToVolumeInfo(ctx, vol, pvMap, isGen2)
		result = append(result, info)
	}
	return result, nil
}

// convertToVolumeInfo converts a goscaleio Volume to VolumeInfo with Kubernetes metadata.
func (p *PowerFlexVolumeClient) convertToVolumeInfo(ctx context.Context, vol *siotypes.Volume, pvMap map[string]*v1.PersistentVolume, isGen2 bool) VolumeInfo {
	info := VolumeInfo{
		VolumeID:       vol.ID,
		VolumeName:     vol.Name,
		SizeInKb:       int64(vol.SizeInKb),
		StoragePoolID:  vol.StoragePoolID,
		MappedSDCCount: len(vol.MappedSdcInfo),
		HealthState:    vol.VolumeReplicationState,
	}

	// Determine attachment status
	if len(vol.MappedSdcInfo) > 0 {
		info.AttachedNodeID = vol.MappedSdcInfo[0].SdcID
	}

	if isGen2 {
		var metrics *siotypes.MetricsResponse
		if err := withGoscaleIOContext(ctx, p.client, func() error {
			var err error
			metrics, err = p.client.GetMetrics("volume", []string{vol.ID})
			if err != nil {
				return err
			}
			return nil
		}); err == nil && metrics != nil && len(metrics.Resources) > 0 {
			info.ReadBandwidthKBps = getMetricValue(metrics.Resources[0].Metrics, "host_read_bandwidth")
			info.WriteBandwidthKBps = getMetricValue(metrics.Resources[0].Metrics, "host_write_bandwidth")
		}
	} else {
		volume := goscaleio.NewVolume(p.client)
		volume.Volume = vol
		var stats *siotypes.VolumeStatistics
		if err := withGoscaleIOContext(ctx, p.client, func() error {
			var err error
			stats, err = volume.GetVolumeStatistics()
			if err != nil {
				return err
			}
			return nil
		}); err == nil && stats != nil {
			info.ReadBandwidthKBps = bwcToKBps(stats.UserDataReadBwc.TotalWeightInKb, stats.UserDataReadBwc.NumSeconds)
			info.WriteBandwidthKBps = bwcToKBps(stats.UserDataWriteBwc.TotalWeightInKb, stats.UserDataWriteBwc.NumSeconds)
		}
	}

	volumeHandle := p.systemID + "-" + vol.ID
	if pv, exists := pvMap[volumeHandle]; exists {
		info.PVName = pv.Name

		if pv.Spec.StorageClassName != "" {
			info.StorageClass = pv.Spec.StorageClassName
		}

		if pv.Spec.ClaimRef != nil {
			info.PVCName = pv.Spec.ClaimRef.Name
			info.Namespace = pv.Spec.ClaimRef.Namespace
		}
	}

	return info
}

func (p *PowerFlexVolumeClient) isGen2System(ctx context.Context) (bool, error) {
	var system *goscaleio.System
	if err := withGoscaleIOContext(ctx, p.client, func() error {
		var err error
		system, err = p.client.FindSystem("", "", "")
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return false, err
	}

	var pds []*siotypes.ProtectionDomain
	if err := withGoscaleIOContext(ctx, p.client, func() error {
		var err error
		pds, err = system.GetProtectionDomain("")
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return false, err
	}
	for _, pd := range pds {
		if pd.GenType == siotypes.GenTypeEC {
			return true, nil
		}
	}
	return false, nil
}

func bwcToKBps(totalWeightInKb, numSeconds int) float64 {
	if numSeconds == 0 {
		return 0
	}
	return float64(totalWeightInKb) / float64(numSeconds)
}
