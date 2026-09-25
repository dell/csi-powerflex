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
	"math"
	"sync"

	sio "github.com/Ecosystems/container-storage-modules/src/goscaleio"
	siotypes "github.com/Ecosystems/container-storage-modules/src/goscaleio/types/v1"
)

const bytesPerKiB = 1024

var goscaleioClientLocks sync.Map

// PowerFlexMetricsClient wraps a goscaleio *sio.Client and adapts it to the
// minimal interfaces required by the metrics collectors. Resilience is handled
// by MetricsRuntime, so this adapter only performs data mapping.
type PowerFlexMetricsClient struct {
	arrayID string
	inner   *sio.Client
}

// NewPowerFlexMetricsClient creates a PowerFlexMetricsClient.
func NewPowerFlexMetricsClient(inner *sio.Client, arrayID string) *PowerFlexMetricsClient {
	return &PowerFlexMetricsClient{arrayID: arrayID, inner: inner}
}

func withGoscaleIOContext(ctx context.Context, client *sio.Client, fn func() error) error {
	if client == nil {
		return fmt.Errorf("goscaleio client is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	clientKey := fmt.Sprintf("%p", client)
	lockValue, _ := goscaleioClientLocks.LoadOrStore(clientKey, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	client.WithContext(ctx)
	defer client.ResetContext()

	return fn()
}

// GetStoragePools implements StoragePoolClient.
func (c *PowerFlexMetricsClient) GetStoragePools(ctx context.Context) ([]StoragePoolStats, error) {
	var result []StoragePoolStats
	if c == nil || c.inner == nil {
		return nil, fmt.Errorf("PowerFlexMetricsClient: inner client is nil")
	}

	var system *sio.System
	if err := withGoscaleIOContext(ctx, c.inner, func() error {
		var err error
		system, err = c.inner.FindSystem("", "", "")
		if err != nil {
			return fmt.Errorf("FindSystem: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	var pds []*siotypes.ProtectionDomain
	if err := withGoscaleIOContext(ctx, c.inner, func() error {
		var err error
		pds, err = system.GetProtectionDomain("")
		if err != nil {
			return fmt.Errorf("GetProtectionDomain: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	for _, pdType := range pds {
		pd := sio.NewProtectionDomain(c.inner)
		pd.ProtectionDomain = pdType

		var pools []*siotypes.StoragePool
		if err := withGoscaleIOContext(ctx, c.inner, func() error {
			var err error
			pools, err = pd.GetStoragePool("")
			if err != nil {
				return fmt.Errorf("GetStoragePool: %w", err)
			}
			return nil
		}); err != nil {
			continue
		}

		for _, p := range pools {
			if pdType.GenType == siotypes.GenTypeEC {
				var metrics *siotypes.MetricsResponse
				if err := withGoscaleIOContext(ctx, c.inner, func() error {
					var err error
					metrics, err = c.inner.GetMetrics("storage_pool", []string{p.ID})
					if err != nil {
						return err
					}
					return nil
				}); err == nil && metrics != nil && len(metrics.Resources) > 0 {
					freeBytes := getMetricValue(metrics.Resources[0].Metrics, "physical_free")
					usedBytes := getMetricValue(metrics.Resources[0].Metrics, "physical_used")
					thinRatio := normalizeThinProvisioningRatio(getMetricValue(metrics.Resources[0].Metrics, "thin_provisioning_ratio"))
					maxCapacityInKb := bytesToKiB(freeBytes + usedBytes)
					result = append(result, StoragePoolStats{
						PoolID:                                   p.ID,
						PoolName:                                 p.Name,
						MaxCapacityInKb:                          maxCapacityInKb,
						CapacityInUseInKb:                        bytesToKiB(usedBytes),
						CapacityAvailableForVolumeAllocationInKb: bytesToKiB(freeBytes),
						CompressionRatio:                         getMetricValue(metrics.Resources[0].Metrics, "compression_ratio"),
						ThinCapacityAllocatedInKb:                estimateThinCapacityAllocatedInKb(thinRatio, maxCapacityInKb),
						ThinRatio:                                thinRatio,
						OverallUsageRatio:                        getMetricValue(metrics.Resources[0].Metrics, "data_reduction_ratio"),
					})
					continue
				}

				sp := sio.NewStoragePool(c.inner)
				sp.StoragePool = p
				var stats *siotypes.Statistics
				if err := withGoscaleIOContext(ctx, c.inner, func() error {
					var err error
					stats, err = sp.GetStatistics()
					if err != nil {
						return err
					}
					return nil
				}); err == nil && stats != nil {
					result = append(result, StoragePoolStats{
						PoolID:                                   p.ID,
						PoolName:                                 p.Name,
						MaxCapacityInKb:                          int64(stats.MaxCapacityInKb),
						CapacityInUseInKb:                        int64(stats.CapacityInUseInKb),
						CapacityAvailableForVolumeAllocationInKb: int64(stats.CapacityAvailableForVolumeAllocationInKb),
						SnapCapacityInUseInKb:                    int64(stats.SnapCapacityInUseInKb),
						VolumeAddressSpaceInKb:                   int64(stats.VolumeAddressSpaceInKb),
						NetUserDataCapacityInKb:                  int64(stats.NetUserDataCapacityInKb),
						CompressionRatio:                         stats.CompressedDataCompressionRatio,
						ThinCapacityAllocatedInKb:                int64(stats.ThinCapacityAllocatedInKb),
						OverallUsageRatio:                        stats.OverallUsageRatio,
					})
					continue
				}

				result = append(result, StoragePoolStats{PoolID: p.ID, PoolName: p.Name})
				continue
			}

			sp := sio.NewStoragePool(c.inner)
			sp.StoragePool = p
			var stats *siotypes.Statistics
			if err := withGoscaleIOContext(ctx, c.inner, func() error {
				var err error
				stats, err = sp.GetStatistics()
				if err != nil {
					return err
				}
				return nil
			}); err != nil || stats == nil {
				continue
			}
			result = append(result, StoragePoolStats{
				PoolID:                                   p.ID,
				PoolName:                                 p.Name,
				MaxCapacityInKb:                          int64(stats.MaxCapacityInKb),
				CapacityInUseInKb:                        int64(stats.CapacityInUseInKb),
				CapacityAvailableForVolumeAllocationInKb: int64(stats.CapacityAvailableForVolumeAllocationInKb),
				SnapCapacityInUseInKb:                    int64(stats.SnapCapacityInUseInKb),
				VolumeAddressSpaceInKb:                   int64(stats.VolumeAddressSpaceInKb),
				NetUserDataCapacityInKb:                  int64(stats.NetUserDataCapacityInKb),
				CompressionRatio:                         stats.CompressedDataCompressionRatio,
				ThinCapacityAllocatedInKb:                int64(stats.ThinCapacityAllocatedInKb),
				OverallUsageRatio:                        stats.OverallUsageRatio,
			})
		}
	}
	return result, nil
}

func getMetricValue(metrics []siotypes.Metric, name string) float64 {
	for _, m := range metrics {
		if m.Name == name && len(m.Values) > 0 {
			return m.Values[0]
		}
	}
	return 0
}

func bytesToKiB(v float64) int64 {
	return int64(v / bytesPerKiB)
}

// normalizeThinProvisioningRatio accepts either a ratio in the range [0,1] or a
// percentage in the range [0,100] and returns a normalized ratio in the range [0,1].
// PowerFlex DTAPI responses have been observed to vary between these forms.
func normalizeThinProvisioningRatio(raw float64) float64 {
	if raw < 0 {
		return 0
	}
	if raw > 1 {
		return raw / 100.0
	}
	return raw
}

// estimateThinCapacityAllocatedInKb derives a best-effort allocated-thin-capacity
// value from the normalized ratio and the pool's total capacity. PowerFlex does not
// expose an explicit allocated-thin-capacity field in the Gen2 metrics payload.
func estimateThinCapacityAllocatedInKb(thinRatio float64, maxCapacityInKb int64) int64 {
	if thinRatio <= 0 || maxCapacityInKb <= 0 {
		return 0
	}
	return int64(math.Round(thinRatio * float64(maxCapacityInKb)))
}

// GetRCGStats implements RCGClient.
func (c *PowerFlexMetricsClient) GetRCGStats(ctx context.Context) ([]RCGStats, error) {
	var result []RCGStats
	if c == nil || c.inner == nil {
		return nil, fmt.Errorf("PowerFlexMetricsClient: inner client is nil")
	}

	var rcgs []*siotypes.ReplicationConsistencyGroup
	if err := withGoscaleIOContext(ctx, c.inner, func() error {
		var err error
		rcgs, err = c.inner.GetReplicationConsistencyGroups()
		if err != nil {
			return fmt.Errorf("GetReplicationConsistencyGroups: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	for _, r := range rcgs {
		rcgObj := sio.NewReplicationConsistencyGroup(c.inner)
		rcgObj.ReplicationConsistencyGroup = r

		stats := &siotypes.ReplicationConsistencyGroupStatistics{}
		if err := withGoscaleIOContext(ctx, c.inner, func() error {
			var err error
			stats, err = rcgObj.GetStatistics()
			if err != nil {
				return err
			}
			return nil
		}); err != nil {
			stats = &siotypes.ReplicationConsistencyGroupStatistics{}
		}

		var pairProgress []RCGPairProgress
		pairCount := 0
		var pairs []*siotypes.ReplicationPair
		if err := withGoscaleIOContext(ctx, c.inner, func() error {
			var err error
			pairs, err = rcgObj.GetReplicationPairs()
			if err != nil {
				return err
			}
			return nil
		}); err == nil {
			pairCount = len(pairs)
			for _, pair := range pairs {
				pairObj := sio.NewReplicationPair(c.inner)
				pairObj.ReplicaitonPair = pair
				var pairStats *siotypes.QueryReplicationPairStatistics
				if err := withGoscaleIOContext(ctx, c.inner, func() error {
					var err error
					pairStats, err = pairObj.GetReplicationPairStatistics()
					if err != nil {
						return err
					}
					return nil
				}); err == nil && pairStats != nil {
					pairProgress = append(pairProgress, RCGPairProgress{
						PairID:   pair.ID,
						Progress: pairStats.InitialCopyProgress,
					})
				}
			}
		}

		result = append(result, RCGStats{
			RCGID:                    r.ID,
			RCGName:                  r.Name,
			State:                    r.CurrConsistMode,
			LifetimeState:            r.LifetimeState,
			LocalActivityState:       r.LocalActivityState,
			RemoteActivityState:      r.RemoteActivityState,
			RPOSeconds:               int64(r.RpoInSeconds),
			FreezeState:              boolToInt(r.FreezeState == "Frozen"),
			PauseMode:                boolToInt(r.PauseMode != "" && r.PauseMode != "None"),
			FailoverState:            boolToInt(r.FailoverType != "" && r.FailoverType != "None"),
			LagReceivedMillis:        stats.LagReceivedInMillis,
			LagAppliedMillis:         stats.LagAppliedInMillis,
			LagPersistentMillis:      stats.LagPersistentInMillis,
			TransmitBandwidthKBps:    siotypes.BandwidthKBps(stats.RplTransmitBwc),
			ReceiveBandwidthKBps:     siotypes.BandwidthKBps(stats.RplReceiveBwc),
			RemoteApplyBandwidthKBps: siotypes.BandwidthKBps(stats.RplRemoteApplyBwc),
			TransmitLatencySeconds:   siotypes.LatencySeconds(stats.RplTransmitLatency),
			ReceiveLatencySeconds:    siotypes.LatencySeconds(stats.RplReceiveLatency),
			ApplyLatencySeconds:      siotypes.LatencySeconds(stats.RplApplyLatency),
			PairCount:                pairCount,
			PairProgress:             pairProgress,
		})
	}
	return result, nil
}

// GetVolumes implements VolumeClient.
func (c *PowerFlexMetricsClient) GetVolumes(ctx context.Context) ([]VolumeInfo, error) {
	var result []VolumeInfo
	if c == nil || c.inner == nil {
		return nil, fmt.Errorf("PowerFlexMetricsClient: inner client is nil")
	}

	var vols []*siotypes.Volume
	if err := withGoscaleIOContext(ctx, c.inner, func() error {
		var err error
		vols, err = c.inner.GetVolume("", "", "", "", false)
		if err != nil {
			return fmt.Errorf("GetVolume: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	for _, v := range vols {
		nodeID := ""
		if len(v.MappedSdcInfo) > 0 && v.MappedSdcInfo[0] != nil {
			nodeID = v.MappedSdcInfo[0].SdcID
		}
		result = append(result, VolumeInfo{
			VolumeID:       v.ID,
			VolumeName:     v.Name,
			SizeInKb:       int64(v.SizeInKb),
			StoragePoolID:  v.StoragePoolID,
			AttachedNodeID: nodeID,
			HealthState:    v.VolumeReplicationState,
		})
	}
	return result, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
