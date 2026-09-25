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
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockStoragePoolClient implements collectors.StoragePoolClient for testing.
type mockStoragePoolClient struct {
	pools []collectors.StoragePoolStats
	err   error
}

type storagePoolFailingRegisterer struct{ err error }

func (f *storagePoolFailingRegisterer) Register(prometheus.Collector) error  { return f.err }
func (f *storagePoolFailingRegisterer) MustRegister(...prometheus.Collector) {}
func (f *storagePoolFailingRegisterer) Unregister(prometheus.Collector) bool { return false }

func (m *mockStoragePoolClient) GetStoragePools(_ context.Context) ([]collectors.StoragePoolStats, error) {
	return m.pools, m.err
}

func gatherMetric(t *testing.T, reg prometheus.Gatherer, name string) *dto.MetricFamily {
	t.Helper()
	mfs, err := reg.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf
		}
	}
	return nil
}

func findCounterValue(mf *dto.MetricFamily, labels map[string]string) (float64, bool) {
	if mf == nil {
		return 0, false
	}
	for _, m := range mf.GetMetric() {
		got := make(map[string]string)
		for _, lp := range m.GetLabel() {
			got[lp.GetName()] = lp.GetValue()
		}
		match := true
		for k, v := range labels {
			if got[k] != v {
				match = false
				break
			}
		}
		if match {
			return m.GetCounter().GetValue(), true
		}
	}
	return 0, false
}

func findGaugeValue(mf *dto.MetricFamily, labels map[string]string) (float64, bool) {
	if mf == nil {
		return 0, false
	}
	for _, m := range mf.GetMetric() {
		got := make(map[string]string)
		for _, lp := range m.GetLabel() {
			got[lp.GetName()] = lp.GetValue()
		}
		match := true
		for k, v := range labels {
			if got[k] != v {
				match = false
				break
			}
		}
		if match {
			return m.GetGauge().GetValue(), true
		}
	}
	return 0, false
}

// U-PFX-05: 2 storage pools — capacity bytes set per pool
func TestStoragePoolCollector_Collect_TwoPools(t *testing.T) {
	client := &mockStoragePoolClient{
		pools: []collectors.StoragePoolStats{
			{
				PoolID: "pool-1", PoolName: "Pool1",
				MaxCapacityInKb:                          1024 * 1024, // 1GB
				CapacityInUseInKb:                        512 * 1024,
				CapacityAvailableForVolumeAllocationInKb: 512 * 1024,
				CompressionRatio:                         2.0,
				ThinCapacityAllocatedInKb:                256 * 1024,
				OverallUsageRatio:                        1.8,
			},
			{
				PoolID: "pool-2", PoolName: "Pool2",
				MaxCapacityInKb:           2048 * 1024,
				CapacityInUseInKb:         1024 * 1024,
				CompressionRatio:          1.5,
				ThinCapacityAllocatedInKb: 512 * 1024,
				OverallUsageRatio:         1.6,
			},
		},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewStoragePoolCollector(client, reg, "system-1")
	require.NoError(t, err)

	err = c.Collect(context.Background())
	require.NoError(t, err)

	mf := gatherMetric(t, reg, "dell_powerflex_storage_pool_capacity_bytes")
	require.NotNil(t, mf, "dell_powerflex_storage_pool_capacity_bytes should be emitted")

	v, ok := findGaugeValue(mf, map[string]string{
		"array_id": "system-1", "pool_id": "pool-1", "type": "total",
	})
	require.True(t, ok, "total capacity for pool-1 should be present")
	assert.Equal(t, float64(1024*1024*1024), v, "total capacity should be MaxCapacityInKb × 1024")

	utilMF := gatherMetric(t, reg, "dell_powerflex_storage_pool_utilization_ratio")
	require.NotNil(t, utilMF, "dell_powerflex_storage_pool_utilization_ratio should be emitted")
	util, ok := findGaugeValue(utilMF, map[string]string{"array_id": "system-1", "pool_id": "pool-1", "pool_name": "Pool1"})
	require.True(t, ok)
	assert.InDelta(t, 0.5, util, 0.001)

	thinMF := gatherMetric(t, reg, "dell_powerflex_storage_pool_thin_ratio")
	require.NotNil(t, thinMF, "dell_powerflex_storage_pool_thin_ratio should be emitted")
	thin, ok := findGaugeValue(thinMF, map[string]string{"array_id": "system-1", "pool_id": "pool-1", "pool_name": "Pool1"})
	require.True(t, ok)
	assert.InDelta(t, 0.25, thin, 0.001)

	compressMF := gatherMetric(t, reg, "dell_powerflex_storage_pool_compression_ratio")
	require.NotNil(t, compressMF, "dell_powerflex_storage_pool_compression_ratio should be emitted")
	compress, ok := findGaugeValue(compressMF, map[string]string{"array_id": "system-1", "pool_id": "pool-1", "pool_name": "Pool1"})
	require.True(t, ok)
	assert.InDelta(t, 2.0, compress, 0.001)

	dataRedMF := gatherMetric(t, reg, "dell_powerflex_storage_pool_data_reduction_ratio")
	require.NotNil(t, dataRedMF, "dell_powerflex_storage_pool_data_reduction_ratio should be emitted")
	dataRed, ok := findGaugeValue(dataRedMF, map[string]string{"array_id": "system-1", "pool_id": "pool-1", "pool_name": "Pool1"})
	require.True(t, ok)
	assert.InDelta(t, 1.8, dataRed, 0.001)
}

// U-PFX-06: Empty pool list — no metrics emitted, no error
func TestStoragePoolCollector_Collect_EmptyPoolList(t *testing.T) {
	client := &mockStoragePoolClient{pools: []collectors.StoragePoolStats{}}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewStoragePoolCollector(client, reg, "system-1")
	require.NoError(t, err)

	err = c.Collect(context.Background())
	assert.NoError(t, err)

	mf := gatherMetric(t, reg, "dell_powerflex_storage_pool_capacity_bytes")
	if mf != nil {
		assert.Empty(t, mf.GetMetric(), "no metrics should be emitted for empty pool list")
	}
}

// U-PFX-07: MaxCapacityInKb == 0 — utilization_ratio NOT emitted
func TestStoragePoolCollector_Collect_ZeroMaxCapacity_OmitsUtilization(t *testing.T) {
	client := &mockStoragePoolClient{
		pools: []collectors.StoragePoolStats{
			{PoolID: "pool-1", PoolName: "Pool1", MaxCapacityInKb: 0, CapacityInUseInKb: 0},
		},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewStoragePoolCollector(client, reg, "system-1")
	require.NoError(t, err)
	_ = c.Collect(context.Background())

	mf := gatherMetric(t, reg, "dell_powerflex_storage_pool_utilization_ratio")
	if mf != nil {
		_, ok := findGaugeValue(mf, map[string]string{"array_id": "system-1", "pool_id": "pool-1"})
		assert.False(t, ok, "utilization_ratio must NOT be emitted when MaxCapacityInKb == 0")
	}
}

// U-PFX-08: CompressionRatio set from extended struct field
func TestStoragePoolCollector_Collect_CompressionRatioFromExtendedStats(t *testing.T) {
	client := &mockStoragePoolClient{
		pools: []collectors.StoragePoolStats{
			{
				PoolID: "pool-1", PoolName: "Pool1",
				MaxCapacityInKb:  1024,
				CompressionRatio: 2.5,
			},
		},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewStoragePoolCollector(client, reg, "system-1")
	require.NoError(t, err)
	_ = c.Collect(context.Background())

	mf := gatherMetric(t, reg, "dell_powerflex_storage_pool_compression_ratio")
	require.NotNil(t, mf, "compression_ratio metric must be emitted")
	v, ok := findGaugeValue(mf, map[string]string{"array_id": "system-1", "pool_id": "pool-1"})
	require.True(t, ok)
	assert.InDelta(t, 2.5, v, 0.001)
}

// U-PFX-08b: ThinCapacityAllocatedInKb drives thin ratio from MaxCapacityInKb.
func TestStoragePoolCollector_Collect_ThinRatioFromAllocatedCapacity(t *testing.T) {
	client := &mockStoragePoolClient{
		pools: []collectors.StoragePoolStats{{
			PoolID:                    "pool-1",
			PoolName:                  "Pool1",
			MaxCapacityInKb:           2000,
			ThinCapacityAllocatedInKb: 500,
		}},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewStoragePoolCollector(client, reg, "system-1")
	require.NoError(t, err)
	_ = c.Collect(context.Background())

	mf := gatherMetric(t, reg, "dell_powerflex_storage_pool_thin_ratio")
	require.NotNil(t, mf)
	v, ok := findGaugeValue(mf, map[string]string{"array_id": "system-1", "pool_id": "pool-1", "pool_name": "Pool1"})
	require.True(t, ok)
	assert.InDelta(t, 0.25, v, 0.001)
}

// U-PFX-08c: OverallUsageRatio drives data reduction ratio directly.
func TestStoragePoolCollector_Collect_DataReductionRatioFromOverallUsage(t *testing.T) {
	client := &mockStoragePoolClient{
		pools: []collectors.StoragePoolStats{{
			PoolID:            "pool-1",
			PoolName:          "Pool1",
			OverallUsageRatio: 1.75,
		}},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewStoragePoolCollector(client, reg, "system-1")
	require.NoError(t, err)
	_ = c.Collect(context.Background())

	mf := gatherMetric(t, reg, "dell_powerflex_storage_pool_data_reduction_ratio")
	require.NotNil(t, mf)
	v, ok := findGaugeValue(mf, map[string]string{"array_id": "system-1", "pool_id": "pool-1", "pool_name": "Pool1"})
	require.True(t, ok)
	assert.InDelta(t, 1.75, v, 0.001)
}

// U-PFX-09: GetStoragePools returns error — error wrapped and returned
func TestStoragePoolCollector_Collect_APIError(t *testing.T) {
	client := &mockStoragePoolClient{err: errors.New("connection refused")}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewStoragePoolCollector(client, reg, "system-1")
	require.NoError(t, err)

	err = c.Collect(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "StoragePoolCollector")
	assert.Contains(t, err.Error(), "connection refused")
}

func TestStoragePoolCollector_RuntimeAndAccessors(t *testing.T) {
	client := &mockStoragePoolClient{
		pools: []collectors.StoragePoolStats{{PoolID: "pool-1", PoolName: "Pool1", MaxCapacityInKb: 2048, CapacityInUseInKb: 1024, CapacityAvailableForVolumeAllocationInKb: 1024, CompressionRatio: 1.8, NetUserDataCapacityInKb: 512, VolumeAddressSpaceInKb: 256}},
	}
	reg := prometheus.NewRegistry()
	c, err := collectors.NewStoragePoolCollector(client, reg, "system-1")
	require.NoError(t, err)
	assert.Equal(t, "StoragePoolCollector", c.Name())
	c.SetRuntime(collectors.NewMetricsRuntime("system-1", nil))
	require.NoError(t, c.Register(prometheus.NewRegistry()))
	require.NoError(t, c.Collect(context.Background()))

	mf := gatherMetric(t, reg, "dell_powerflex_storage_pool_capacity_bytes")
	require.NotNil(t, mf)
	v, ok := findGaugeValue(mf, map[string]string{"array_id": "system-1", "pool_id": "pool-1", "type": "total"})
	require.True(t, ok)
	assert.Equal(t, float64(2048*1024), v)
}

func TestStoragePoolCollector_ConstructorErrorBranch(t *testing.T) {
	_, err := collectors.NewStoragePoolCollector(&mockStoragePoolClient{}, &storagePoolFailingRegisterer{err: errors.New("register failed")}, "system-1")
	require.Error(t, err)
}

func TestStoragePoolCollector_ConstructorAndAccessors(t *testing.T) {
	reg := prometheus.NewRegistry()
	client := &mockStoragePoolClient{pools: []collectors.StoragePoolStats{{PoolID: "pool-1", PoolName: "Pool1"}}}
	c, err := collectors.NewStoragePoolCollector(client, reg, "system-1")
	require.NoError(t, err)
	assert.Equal(t, "StoragePoolCollector", c.Name())
	c.SetRuntime(nil)
	require.NoError(t, c.Register(prometheus.NewRegistry()))
	require.NoError(t, c.Collect(context.Background()))
	c.SetRuntime(collectors.NewMetricsRuntime("system-1", nil))
	require.NoError(t, c.Collect(context.Background()))

	failReg := &storagePoolFailingRegisterer{err: errors.New("register failed")}
	_, err = collectors.NewStoragePoolCollector(client, failReg, "system-1")
	require.Error(t, err)
	require.Error(t, c.Register(failReg))
}

func TestStoragePoolCollector_Constructor_FirstMetricError(t *testing.T) {
	// Test error path when first metric registration fails
	client := &mockStoragePoolClient{pools: []collectors.StoragePoolStats{{PoolID: "pool-1", PoolName: "Pool1"}}}
	failReg := &storagePoolFailingRegisterer{err: errors.New("first metric failed")}
	_, err := collectors.NewStoragePoolCollector(client, failReg, "system-1")
	require.Error(t, err)
}

func TestStoragePoolCollector_NewStoragePoolCollector_EmptyPoolList(t *testing.T) {
	client := &mockStoragePoolClient{
		pools: []collectors.StoragePoolStats{},
	}
	reg := prometheus.NewRegistry()
	c, err := collectors.NewStoragePoolCollector(client, reg, "system-1")
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, "StoragePoolCollector", c.Name())
}
