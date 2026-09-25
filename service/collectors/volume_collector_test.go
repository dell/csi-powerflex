// Copyright © 2024 Dell Inc. or its subsidiaries. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package collectors_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Ecosystems/container-storage-modules/src/csi-vxflexos/v2/service/collectors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockVolumeClient struct {
	volumes []collectors.VolumeInfo
	err     error
}

type volumeFailingRegisterer struct{ err error }

func (f volumeFailingRegisterer) Register(prometheus.Collector) error  { return f.err }
func (f volumeFailingRegisterer) MustRegister(...prometheus.Collector) {}
func (f volumeFailingRegisterer) Unregister(prometheus.Collector) bool { return false }

func (m *mockVolumeClient) GetVolumes(_ context.Context) ([]collectors.VolumeInfo, error) {
	return m.volumes, m.err
}

// U-PFX-13: 3 volumes, 1 attached, 2 detached — attachment_status set correctly
func TestVolumeCollector_Collect_AttachmentStatus(t *testing.T) {
	client := &mockVolumeClient{
		volumes: []collectors.VolumeInfo{
			{VolumeID: "v1", VolumeName: "Vol1", AttachedNodeID: "node-1", HealthState: "Normal", StoragePoolID: "pool-1", PVName: "pv-1", PVCName: "pvc-1", Namespace: "default", StorageClass: "sc-1", MappedSDCCount: 1},
			{VolumeID: "v2", VolumeName: "Vol2", AttachedNodeID: "", HealthState: "Normal", StoragePoolID: "pool-1", PVName: "pv-2", PVCName: "pvc-2", Namespace: "default", StorageClass: "sc-1", MappedSDCCount: 0},
			{VolumeID: "v3", VolumeName: "Vol3", AttachedNodeID: "", HealthState: "Normal", StoragePoolID: "pool-1", PVName: "pv-3", PVCName: "pvc-3", Namespace: "default", StorageClass: "sc-1", MappedSDCCount: 0},
		},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewVolumeCollector(client, reg, "system-1")
	require.NoError(t, err)

	err = c.Collect(context.Background())
	require.NoError(t, err)

	mf := gatherMetric(t, reg, "dell_powerflex_volume_attachment_status")
	require.NotNil(t, mf, "attachment_status metric must be emitted")

	v1Attached, ok := findGaugeValue(mf, map[string]string{
		"system_id": "system-1", "volume_id": "v1",
	})
	require.True(t, ok)
	assert.Equal(t, 1.0, v1Attached, "v1 is attached — should be 1")

	v2Attached, ok := findGaugeValue(mf, map[string]string{
		"system_id": "system-1", "volume_id": "v2",
	})
	require.True(t, ok)
	assert.Equal(t, 0.0, v2Attached, "v2 is detached — should be 0")

	// Verify dell_csi_volume_total = 3
	totalMF := gatherMetric(t, reg, "dell_csi_volume_total")
	require.NotNil(t, totalMF)
	total, ok := findGaugeValue(totalMF, map[string]string{"array_id": "system-1"})
	require.True(t, ok)
	assert.Equal(t, 3.0, total)
}

// U-PFX-14: Unhealthy volume (replication error state) — volume_healthy = 0
func TestVolumeCollector_Collect_UnhealthyVolume(t *testing.T) {
	client := &mockVolumeClient{
		volumes: []collectors.VolumeInfo{
			{VolumeID: "v1", VolumeName: "Vol1", HealthState: "ReplicationError", StoragePoolID: "pool-1", PVName: "pv-1", PVCName: "pvc-1", Namespace: "default", StorageClass: "sc-1"},
			{VolumeID: "v2", VolumeName: "Vol2", HealthState: "Normal", StoragePoolID: "pool-1", PVName: "pv-2", PVCName: "pvc-2", Namespace: "default", StorageClass: "sc-1"},
		},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewVolumeCollector(client, reg, "system-1")
	require.NoError(t, err)

	_ = c.Collect(context.Background())

	mf := gatherMetric(t, reg, "dell_powerflex_volume_health")
	require.NotNil(t, mf)

	v1Health, ok := findGaugeValue(mf, map[string]string{"array_id": "system-1", "volume_id": "v1"})
	require.True(t, ok)
	assert.Equal(t, 0.0, v1Health, "v1 has ReplicationError — health should be 0")

	v2Health, ok := findGaugeValue(mf, map[string]string{"array_id": "system-1", "volume_id": "v2"})
	require.True(t, ok)
	assert.Equal(t, 1.0, v2Health, "v2 is Normal — health should be 1")
}

// U-PFX-15: Volume with empty HealthState — volume_healthy defaults to 1 (healthy)
func TestVolumeCollector_Collect_EmptyHealthState_DefaultsHealthy(t *testing.T) {
	client := &mockVolumeClient{
		volumes: []collectors.VolumeInfo{
			{VolumeID: "v1", VolumeName: "Vol1", HealthState: "", StoragePoolID: "pool-1", PVName: "pv-1", PVCName: "pvc-1", Namespace: "default", StorageClass: "sc-1"},
		},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewVolumeCollector(client, reg, "system-1")
	require.NoError(t, err)

	require.NoError(t, c.Collect(context.Background()))

	mf := gatherMetric(t, reg, "dell_powerflex_volume_health")
	require.NotNil(t, mf)
	health, ok := findGaugeValue(mf, map[string]string{"array_id": "system-1", "volume_id": "v1"})
	require.True(t, ok)
	assert.Equal(t, 1.0, health, "empty HealthState must default to healthy=1 per spec")
}

// U-PFX-16: volume_size_bytes emitted with correct Kubernetes labels and byte value
func TestVolumeCollector_Collect_SizeBytes(t *testing.T) {
	client := &mockVolumeClient{
		volumes: []collectors.VolumeInfo{
			{VolumeID: "v1", VolumeName: "Vol1", SizeInKb: 10240, StoragePoolID: "pool-1", PVName: "pv-1", PVCName: "pvc-1", Namespace: "default", StorageClass: "sc-1"},
		},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewVolumeCollector(client, reg, "system-1")
	require.NoError(t, err)

	require.NoError(t, c.Collect(context.Background()))

	mf := gatherMetric(t, reg, "dell_powerflex_volume_size_bytes")
	require.NotNil(t, mf, "dell_powerflex_volume_size_bytes must be emitted")
	v, ok := findGaugeValue(mf, map[string]string{
		"array_id": "system-1", "volume_id": "v1", "pv_name": "pv-1", "pvc_name": "pvc-1", "namespace": "default",
	})
	require.True(t, ok)
	assert.Equal(t, float64(10240*1024), v, "10240 KB * 1024 = 10485760 bytes")
}

func TestVolumeCollector_Collect_BandwidthKBps(t *testing.T) {
	client := &mockVolumeClient{
		volumes: []collectors.VolumeInfo{
			{VolumeID: "v1", VolumeName: "Vol1", ReadBandwidthKBps: 512.5, WriteBandwidthKBps: 256.25, PVName: "pv-1", PVCName: "pvc-1", Namespace: "default", StoragePoolID: "pool-1", StorageClass: "sc-1"},
		},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewVolumeCollector(client, reg, "system-1")
	require.NoError(t, err)

	require.NoError(t, c.Collect(context.Background()))

	readMF := gatherMetric(t, reg, "dell_powerflex_volume_read_bandwidth_kbps")
	require.NotNil(t, readMF, "dell_powerflex_volume_read_bandwidth_kbps must be emitted")
	readValue, ok := findGaugeValue(readMF, map[string]string{"array_id": "system-1", "volume_id": "v1", "pv_name": "pv-1", "pvc_name": "pvc-1", "namespace": "default"})
	require.True(t, ok)
	assert.InDelta(t, 512.5, readValue, 0.001)

	writeMF := gatherMetric(t, reg, "dell_powerflex_volume_write_bandwidth_kbps")
	require.NotNil(t, writeMF, "dell_powerflex_volume_write_bandwidth_kbps must be emitted")
	writeValue, ok := findGaugeValue(writeMF, map[string]string{"array_id": "system-1", "volume_id": "v1", "pv_name": "pv-1", "pvc_name": "pvc-1", "namespace": "default"})
	require.True(t, ok)
	assert.InDelta(t, 256.25, writeValue, 0.001)
}

// U-PFX-17: Paused or Failed replication state — volume_healthy = 0
func TestVolumeCollector_Collect_PausedFailedHealthState(t *testing.T) {
	client := &mockVolumeClient{
		volumes: []collectors.VolumeInfo{
			{VolumeID: "v1", VolumeName: "Vol1", HealthState: "ReplicationPaused", StoragePoolID: "pool-1", PVName: "pv-1", PVCName: "pvc-1", Namespace: "default", StorageClass: "sc-1"},
			{VolumeID: "v2", VolumeName: "Vol2", HealthState: "ReplicationFailed", StoragePoolID: "pool-1", PVName: "pv-2", PVCName: "pvc-2", Namespace: "default", StorageClass: "sc-1"},
			{VolumeID: "v3", VolumeName: "Vol3", HealthState: "", StoragePoolID: "pool-1", PVName: "pv-3", PVCName: "pvc-3", Namespace: "default", StorageClass: "sc-1"},
		},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewVolumeCollector(client, reg, "system-1")
	require.NoError(t, err)

	require.NoError(t, c.Collect(context.Background()))

	mf := gatherMetric(t, reg, "dell_powerflex_volume_health")
	require.NotNil(t, mf)

	v1, ok := findGaugeValue(mf, map[string]string{"array_id": "system-1", "volume_id": "v1"})
	require.True(t, ok)
	assert.Equal(t, 0.0, v1, "ReplicationPaused → unhealthy")

	v2, ok := findGaugeValue(mf, map[string]string{"array_id": "system-1", "volume_id": "v2"})
	require.True(t, ok)
	assert.Equal(t, 0.0, v2, "ReplicationFailed → unhealthy")

	v3, ok := findGaugeValue(mf, map[string]string{"array_id": "system-1", "volume_id": "v3"})
	require.True(t, ok)
	assert.Equal(t, 1.0, v3, "empty state → healthy")
}

// U-PFX-18: volume_count metric — count volumes per pool and storage class
func TestVolumeCollector_Collect_VolumeCount(t *testing.T) {
	client := &mockVolumeClient{
		volumes: []collectors.VolumeInfo{
			{VolumeID: "v1", StoragePoolID: "pool-1", StorageClass: "sc-fast", PVName: "pv-1", PVCName: "pvc-1", Namespace: "default"},
			{VolumeID: "v2", StoragePoolID: "pool-1", StorageClass: "sc-fast", PVName: "pv-2", PVCName: "pvc-2", Namespace: "default"},
			{VolumeID: "v3", StoragePoolID: "pool-1", StorageClass: "sc-slow", PVName: "pv-3", PVCName: "pvc-3", Namespace: "default"},
			{VolumeID: "v4", StoragePoolID: "pool-2", StorageClass: "sc-fast", PVName: "pv-4", PVCName: "pvc-4", Namespace: "default"},
		},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewVolumeCollector(client, reg, "system-1")
	require.NoError(t, err)

	require.NoError(t, c.Collect(context.Background()))

	mf := gatherMetric(t, reg, "dell_powerflex_volume_count")
	require.NotNil(t, mf, "dell_powerflex_volume_count must be emitted")

	// pool-1, sc-fast: 2 volumes
	count1, ok := findGaugeValue(mf, map[string]string{"array_id": "system-1", "pool_id": "pool-1", "storage_class": "sc-fast"})
	require.True(t, ok)
	assert.Equal(t, 2.0, count1, "pool-1 with sc-fast should have 2 volumes")

	// pool-1, sc-slow: 1 volume
	count2, ok := findGaugeValue(mf, map[string]string{"array_id": "system-1", "pool_id": "pool-1", "storage_class": "sc-slow"})
	require.True(t, ok)
	assert.Equal(t, 1.0, count2, "pool-1 with sc-slow should have 1 volume")

	// pool-2, sc-fast: 1 volume
	count3, ok := findGaugeValue(mf, map[string]string{"array_id": "system-1", "pool_id": "pool-2", "storage_class": "sc-fast"})
	require.True(t, ok)
	assert.Equal(t, 1.0, count3, "pool-2 with sc-fast should have 1 volume")
}

// U-PFX-19: volume_mapped_sdcs metric — count of mapped SDCs per volume
func TestVolumeCollector_Collect_MappedSDCs(t *testing.T) {
	client := &mockVolumeClient{
		volumes: []collectors.VolumeInfo{
			{VolumeID: "v1", MappedSDCCount: 3, PVName: "pv-1", PVCName: "pvc-1", Namespace: "default", StoragePoolID: "pool-1", StorageClass: "sc-1"},
			{VolumeID: "v2", MappedSDCCount: 0, PVName: "pv-2", PVCName: "pvc-2", Namespace: "default", StoragePoolID: "pool-1", StorageClass: "sc-1"},
			{VolumeID: "v3", MappedSDCCount: 1, PVName: "pv-3", PVCName: "pvc-3", Namespace: "default", StoragePoolID: "pool-1", StorageClass: "sc-1"},
		},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewVolumeCollector(client, reg, "system-1")
	require.NoError(t, err)

	require.NoError(t, c.Collect(context.Background()))

	mf := gatherMetric(t, reg, "dell_powerflex_volume_mapped_sdcs")
	require.NotNil(t, mf, "dell_powerflex_volume_mapped_sdcs must be emitted")

	// v1: 3 mapped SDCs
	count1, ok := findGaugeValue(mf, map[string]string{"array_id": "system-1", "volume_id": "v1", "pv_name": "pv-1"})
	require.True(t, ok)
	assert.Equal(t, 3.0, count1, "v1 should have 3 mapped SDCs")

	// v2: 0 mapped SDCs
	count2, ok := findGaugeValue(mf, map[string]string{"array_id": "system-1", "volume_id": "v2", "pv_name": "pv-2"})
	require.True(t, ok)
	assert.Equal(t, 0.0, count2, "v2 should have 0 mapped SDCs")

	// v3: 1 mapped SDC
	count3, ok := findGaugeValue(mf, map[string]string{"array_id": "system-1", "volume_id": "v3", "pv_name": "pv-3"})
	require.True(t, ok)
	assert.Equal(t, 1.0, count3, "v3 should have 1 mapped SDC")
}

func TestVolumeCollector_ConstructorAndAccessors(t *testing.T) {
	reg := prometheus.NewRegistry()
	client := &mockVolumeClient{volumes: []collectors.VolumeInfo{{VolumeID: "v1", VolumeName: "Vol1", HealthState: "Normal", StoragePoolID: "pool-1", PVName: "pv-1", PVCName: "pvc-1", Namespace: "default", StorageClass: "sc-1"}}}
	c, err := collectors.NewVolumeCollector(client, reg, "system-1")
	require.NoError(t, err)
	assert.Equal(t, "VolumeCollector", c.Name())
	c.SetRuntime(nil)
	require.NoError(t, c.Collect(context.Background()))
	c.SetRuntime(collectors.NewMetricsRuntime("system-1", nil))
	require.NoError(t, c.Collect(context.Background()))

	_, err = collectors.NewVolumeCollector(client, &volumeFailingRegisterer{err: errors.New("register failed")}, "system-1")
	require.Error(t, err)
}

func TestVolumeCollector_Constructor_FirstMetricError(t *testing.T) {
	// Test error path when first metric registration fails
	client := &mockVolumeClient{volumes: []collectors.VolumeInfo{{VolumeID: "v1", VolumeName: "Vol1", HealthState: "Normal", StoragePoolID: "pool-1", PVName: "pv-1", PVCName: "pvc-1", Namespace: "default", StorageClass: "sc-1"}}}
	failReg := &volumeFailingRegisterer{err: errors.New("first metric failed")}
	_, err := collectors.NewVolumeCollector(client, failReg, "system-1")
	require.Error(t, err)
}

func TestVolumeCollector_NewVolumeCollector_EmptyVolumeList(t *testing.T) {
	client := &mockVolumeClient{
		volumes: []collectors.VolumeInfo{},
	}
	reg := prometheus.NewRegistry()
	c, err := collectors.NewVolumeCollector(client, reg, "system-1")
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, "VolumeCollector", c.Name())
}
