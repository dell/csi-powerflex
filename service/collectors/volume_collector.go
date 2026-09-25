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
	"strings"

	"github.com/prometheus/client_golang/prometheus"
)

// VolumeInfo holds per-volume data for metrics.
type VolumeInfo struct {
	VolumeID           string
	VolumeName         string
	SizeInKb           int64
	ReadBandwidthKBps  float64
	WriteBandwidthKBps float64
	StoragePoolID      string
	StoragePoolName    string
	AttachedNodeID     string
	HealthState        string
	MappedSDCCount     int
	PVName             string
	PVCName            string
	Namespace          string
	StorageClass       string
}

// VolumeClient is the minimal interface for goscaleio volume calls.
type VolumeClient interface {
	GetVolumes(ctx context.Context) ([]VolumeInfo, error)
}

// VolumeCollector collects PowerFlex volume metrics.
type VolumeCollector struct {
	client           VolumeClient
	runtime          *MetricsRuntime
	systemID         string
	volumeTotal      *prometheus.GaugeVec
	volumeCount      *prometheus.GaugeVec
	volumeSize       *prometheus.GaugeVec
	readBandwidth    *prometheus.GaugeVec
	writeBandwidth   *prometheus.GaugeVec
	mappedSDCs       *prometheus.GaugeVec
	attachmentStatus *prometheus.GaugeVec
	volumeHealthy    *prometheus.GaugeVec
}

// NewVolumeCollector creates a VolumeCollector and registers its Prometheus metrics.
func NewVolumeCollector(client VolumeClient, reg prometheus.Registerer, systemID string) (*VolumeCollector, error) {
	volumeTotal, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_csi_volume_total",
		Help: "Total volumes managed by the driver.",
	}, []string{"array_id"}))
	if err != nil {
		return nil, err
	}
	volumeCount, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_volume_count",
		Help: "Count of volumes per pool and storage class.",
	}, []string{"array_id", "pool_id", "storage_class"}))
	if err != nil {
		return nil, err
	}
	volumeSize, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_volume_size_bytes",
		Help: "PowerFlex volume size in bytes.",
	}, []string{"array_id", "volume_id", "pv_name", "pvc_name", "namespace"}))
	if err != nil {
		return nil, err
	}
	readBandwidth, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_volume_read_bandwidth_kbps",
		Help: "PowerFlex volume read bandwidth in KB/s.",
	}, []string{"array_id", "volume_id", "pv_name", "pvc_name", "namespace"}))
	if err != nil {
		return nil, err
	}
	writeBandwidth, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_volume_write_bandwidth_kbps",
		Help: "PowerFlex volume write bandwidth in KB/s.",
	}, []string{"array_id", "volume_id", "pv_name", "pvc_name", "namespace"}))
	if err != nil {
		return nil, err
	}
	mappedSDCs, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_volume_mapped_sdcs",
		Help: "Number of SDCs mapped to the volume.",
	}, []string{"array_id", "volume_id", "pv_name"}))
	if err != nil {
		return nil, err
	}
	attachmentStatus, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_volume_attachment_status",
		Help: "Volume attachment status (1=attached, 0=detached).",
	}, []string{"system_id", "volume_id", "volume_name"}))
	if err != nil {
		return nil, err
	}
	volumeHealthy, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_volume_health",
		Help: "Volume health (1=healthy, 0=unhealthy).",
	}, []string{"array_id", "volume_id"}))
	if err != nil {
		return nil, err
	}

	return &VolumeCollector{
		client:           client,
		systemID:         systemID,
		volumeTotal:      volumeTotal,
		volumeCount:      volumeCount,
		volumeSize:       volumeSize,
		readBandwidth:    readBandwidth,
		writeBandwidth:   writeBandwidth,
		mappedSDCs:       mappedSDCs,
		attachmentStatus: attachmentStatus,
		volumeHealthy:    volumeHealthy,
	}, nil
}

// SetRuntime configures the resilience runtime used when collecting metrics.
func (c *VolumeCollector) SetRuntime(runtime *MetricsRuntime) {
	c.runtime = runtime
}

// Collect fetches volume metrics.
func (c *VolumeCollector) Collect(ctx context.Context) error {
	var volumes []VolumeInfo
	c.volumeTotal.Reset()
	c.volumeCount.Reset()
	c.volumeSize.Reset()
	c.readBandwidth.Reset()
	c.writeBandwidth.Reset()
	c.mappedSDCs.Reset()
	c.attachmentStatus.Reset()
	c.volumeHealthy.Reset()
	if c.runtime == nil {
		var err error
		volumes, err = c.client.GetVolumes(ctx)
		if err != nil {
			return fmt.Errorf("VolumeCollector: failed to get volumes: %w", err)
		}
	} else {
		result, err := c.runtime.Do(ctx, "volumes", c.systemID+":volumes", func(callCtx context.Context) (any, error) {
			return c.client.GetVolumes(callCtx)
		})
		if err != nil {
			return fmt.Errorf("VolumeCollector: failed to get volumes: %w", err)
		}
		var ok bool
		volumes, ok = result.([]VolumeInfo)
		if !ok {
			return fmt.Errorf("VolumeCollector: unexpected runtime result type %T", result)
		}
	}
	c.volumeTotal.WithLabelValues(c.systemID).Set(float64(len(volumes)))

	// Count volumes per pool and storage class
	poolSCCounts := make(map[string]map[string]int)
	for _, v := range volumes {
		if poolSCCounts[v.StoragePoolID] == nil {
			poolSCCounts[v.StoragePoolID] = make(map[string]int)
		}
		poolSCCounts[v.StoragePoolID][v.StorageClass]++
	}
	for poolID, scCounts := range poolSCCounts {
		for sc, count := range scCounts {
			c.volumeCount.WithLabelValues(c.systemID, poolID, sc).Set(float64(count))
		}
	}

	for _, v := range volumes {
		// Volume size with Kubernetes labels
		c.volumeSize.WithLabelValues(c.systemID, v.VolumeID, v.PVName, v.PVCName, v.Namespace).Set(float64(v.SizeInKb * 1024))
		c.readBandwidth.WithLabelValues(c.systemID, v.VolumeID, v.PVName, v.PVCName, v.Namespace).Set(v.ReadBandwidthKBps)
		c.writeBandwidth.WithLabelValues(c.systemID, v.VolumeID, v.PVName, v.PVCName, v.Namespace).Set(v.WriteBandwidthKBps)

		// Mapped SDCs count
		c.mappedSDCs.WithLabelValues(c.systemID, v.VolumeID, v.PVName).Set(float64(v.MappedSDCCount))

		// Attachment status (legacy metric, kept for backward compatibility)
		attached := 0.0
		if v.AttachedNodeID != "" {
			attached = 1.0
		}
		c.attachmentStatus.WithLabelValues(c.systemID, v.VolumeID, v.VolumeName).Set(attached)

		// Volume health
		healthy := volumeHealthy(v.HealthState)
		c.volumeHealthy.WithLabelValues(c.systemID, v.VolumeID).Set(healthy)
	}
	return nil
}

// volumeHealthy returns 1.0 (healthy) by default; 0.0 only when replication state
// contains known error indicators per spec.
func volumeHealthy(replicationState string) float64 {
	if replicationState == "" {
		return 1.0
	}
	for _, indicator := range []string{"Error", "Failed", "Paused"} {
		if strings.Contains(replicationState, indicator) {
			return 0.0
		}
	}
	return 1.0
}

// Name returns the collector name.
func (c *VolumeCollector) Name() string { return "VolumeCollector" }
