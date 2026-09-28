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

	"github.com/prometheus/client_golang/prometheus"
)

// StoragePoolStats represents storage pool statistics from the PowerFlex array.
type StoragePoolStats struct {
	PoolID                                   string
	PoolName                                 string
	MaxCapacityInKb                          int64
	CapacityInUseInKb                        int64
	CapacityAvailableForVolumeAllocationInKb int64
	SnapCapacityInUseInKb                    int64
	VolumeAddressSpaceInKb                   int64
	NetUserDataCapacityInKb                  int64
	CompressionRatio                         float64
	ThinCapacityAllocatedInKb                int64
	ThinRatio                                float64
	OverallUsageRatio                        float64
}

// StoragePoolClient is the minimal interface for goscaleio calls needed by StoragePoolCollector.
type StoragePoolClient interface {
	GetStoragePools(ctx context.Context) ([]StoragePoolStats, error)
}

// StoragePoolCollector collects PowerFlex storage pool metrics.
type StoragePoolCollector struct {
	client             StoragePoolClient
	runtime            *MetricsRuntime
	systemID           string
	capacityBytes      *prometheus.GaugeVec
	utilizationRatio   *prometheus.GaugeVec
	thinProvRatio      *prometheus.GaugeVec
	compressionRatio   *prometheus.GaugeVec
	dataReductionRatio *prometheus.GaugeVec
}

// NewStoragePoolCollector creates a new StoragePoolCollector with constructor-time metric registration.
func NewStoragePoolCollector(client StoragePoolClient, reg prometheus.Registerer, systemID string) (*StoragePoolCollector, error) {
	capacityBytes, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_storage_pool_capacity_bytes",
		Help: "PowerFlex storage pool capacity in bytes.",
	}, []string{"array_id", "pool_id", "pool_name", "type"}))
	if err != nil {
		return nil, err
	}
	utilizationRatio, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_storage_pool_utilization_ratio",
		Help: "PowerFlex storage pool utilization ratio.",
	}, []string{"array_id", "pool_id", "pool_name"}))
	if err != nil {
		return nil, err
	}
	thinProvRatio, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_storage_pool_thin_ratio",
		Help: "PowerFlex thin provisioning ratio.",
	}, []string{"array_id", "pool_id", "pool_name"}))
	if err != nil {
		return nil, err
	}
	compressionRatio, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_storage_pool_compression_ratio",
		Help: "PowerFlex compression ratio.",
	}, []string{"array_id", "pool_id", "pool_name"}))
	if err != nil {
		return nil, err
	}
	dataReductionRatio, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_storage_pool_data_reduction_ratio",
		Help: "PowerFlex data reduction ratio.",
	}, []string{"array_id", "pool_id", "pool_name"}))
	if err != nil {
		return nil, err
	}

	return &StoragePoolCollector{
		client:             client,
		systemID:           systemID,
		capacityBytes:      capacityBytes,
		utilizationRatio:   utilizationRatio,
		thinProvRatio:      thinProvRatio,
		compressionRatio:   compressionRatio,
		dataReductionRatio: dataReductionRatio,
	}, nil
}

// SetRuntime configures the resilience runtime used when collecting metrics.
func (c *StoragePoolCollector) SetRuntime(runtime *MetricsRuntime) {
	c.runtime = runtime
}

// Register implements collector.MetricsCollector and registers all Prometheus metrics.
func (c *StoragePoolCollector) Register(reg prometheus.Registerer) error {
	for _, m := range []prometheus.Collector{
		c.capacityBytes, c.utilizationRatio, c.thinProvRatio, c.compressionRatio, c.dataReductionRatio,
	} {
		if err := reg.Register(m); err != nil {
			return err
		}
	}
	return nil
}

// Collect fetches storage pool metrics.
func (c *StoragePoolCollector) Collect(ctx context.Context) error {
	c.capacityBytes.Reset()
	c.utilizationRatio.Reset()
	c.thinProvRatio.Reset()
	c.compressionRatio.Reset()
	c.dataReductionRatio.Reset()

	apply := func(pools []StoragePoolStats) {
		for _, pool := range pools {
			c.capacityBytes.WithLabelValues(c.systemID, pool.PoolID, pool.PoolName, "total").Set(float64(pool.MaxCapacityInKb * 1024))
			c.capacityBytes.WithLabelValues(c.systemID, pool.PoolID, pool.PoolName, "used").Set(float64(pool.CapacityInUseInKb * 1024))
			c.capacityBytes.WithLabelValues(c.systemID, pool.PoolID, pool.PoolName, "available").Set(float64(pool.CapacityAvailableForVolumeAllocationInKb * 1024))
			c.capacityBytes.WithLabelValues(c.systemID, pool.PoolID, pool.PoolName, "snap").Set(float64(pool.SnapCapacityInUseInKb * 1024))

			if pool.MaxCapacityInKb > 0 {
				c.utilizationRatio.WithLabelValues(c.systemID, pool.PoolID, pool.PoolName).
					Set(float64(pool.CapacityInUseInKb) / float64(pool.MaxCapacityInKb))
			}
			if pool.ThinRatio > 0 {
				c.thinProvRatio.WithLabelValues(c.systemID, pool.PoolID, pool.PoolName).Set(pool.ThinRatio)
			} else if pool.MaxCapacityInKb > 0 && pool.ThinCapacityAllocatedInKb > 0 {
				c.thinProvRatio.WithLabelValues(c.systemID, pool.PoolID, pool.PoolName).
					Set(float64(pool.ThinCapacityAllocatedInKb) / float64(pool.MaxCapacityInKb))
			}
			c.compressionRatio.WithLabelValues(c.systemID, pool.PoolID, pool.PoolName).Set(pool.CompressionRatio)
			c.dataReductionRatio.WithLabelValues(c.systemID, pool.PoolID, pool.PoolName).Set(pool.OverallUsageRatio)
		}
	}

	if c.runtime == nil {
		pools, err := c.client.GetStoragePools(ctx)
		if err != nil {
			return fmt.Errorf("StoragePoolCollector: failed to get storage pools: %w", err)
		}
		apply(pools)
		return nil
	}

	result, err := c.runtime.Do(ctx, "storage-pools", c.systemID+":storage-pools", func(callCtx context.Context) (any, error) {
		return c.client.GetStoragePools(callCtx)
	})
	if err != nil {
		return fmt.Errorf("StoragePoolCollector: failed to get storage pools: %w", err)
	}
	pools, ok := result.([]StoragePoolStats)
	if !ok {
		return fmt.Errorf("StoragePoolCollector: unexpected runtime result type %T", result)
	}
	apply(pools)
	return nil
}

// Name returns the collector name.
func (c *StoragePoolCollector) Name() string { return "StoragePoolCollector" }
