// Copyright © 2025 Dell Inc. or its subsidiaries. All Rights Reserved.
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

package collectors

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sio "github.com/Ecosystems/container-storage-modules/src/goscaleio"
	siotypes "github.com/Ecosystems/container-storage-modules/src/goscaleio/types/v1"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

type fakeVolumeMetadataProvider struct {
	available  bool
	managed    map[string]bool
	refreshErr error
}

func (f *fakeVolumeMetadataProvider) RefreshCache(context.Context) error { return f.refreshErr }

func (f *fakeVolumeMetadataProvider) IsDriverManaged(_ context.Context, volumeID string) (bool, error) {
	if v, ok := f.managed[volumeID]; ok {
		return v, nil
	}
	return false, nil
}

func (f *fakeVolumeMetadataProvider) IsDriverManagedByName(_ context.Context, volumeName string) (bool, error) {
	if v, ok := f.managed[volumeName]; ok {
		return v, nil
	}
	return false, nil
}

func (f *fakeVolumeMetadataProvider) GetProtocol(context.Context, string) string { return "iscsi" }

func (f *fakeVolumeMetadataProvider) GetAttachmentStateForVolumeID(context.Context, string, string) (bool, error) {
	return true, nil
}

func (f *fakeVolumeMetadataProvider) GetAttachmentStateForPVName(context.Context, string) (bool, error) {
	return true, nil
}

func (f *fakeVolumeMetadataProvider) Available() bool { return f.available }

type stubArrayHealthClient struct {
	version      string
	versionErr   error
	cluster      *siotypes.MdmCluster
	clusterErr   error
	versionCalls int
	clusterCalls int
}

func (s *stubArrayHealthClient) GetVersion(context.Context) (string, error) {
	s.versionCalls++
	return s.version, s.versionErr
}

func (s *stubArrayHealthClient) GetMDMClusterDetails(context.Context) (*siotypes.MdmCluster, error) {
	s.clusterCalls++
	return s.cluster, s.clusterErr
}

func newTestScaleIOClient(t *testing.T, serverURL string) *sio.Client {
	t.Helper()
	client, err := sio.NewClientWithArgs(serverURL, "3.6", math.MaxInt64, true, false, "")
	require.NoError(t, err)
	return client
}

func writeJSON(t *testing.T, w http.ResponseWriter, v interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(v)
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
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

func TestPowerFlexMetricsClient_HelperFunctions(t *testing.T) {
	assert.Equal(t, float64(0), getMetricValue(nil, "missing"))
	assert.Equal(t, float64(42), getMetricValue([]siotypes.Metric{{Name: "present", Values: []float64{42}}}, "present"))
	assert.Equal(t, int64(2), bytesToKiB(2048))
	assert.Equal(t, 1, boolToInt(true))
	assert.Equal(t, 0, boolToInt(false))
}

func TestPowerFlexMetricsClient_NilInnerReturnsError(t *testing.T) {
	client := &PowerFlexMetricsClient{}
	result, err := client.GetVolumes(context.Background())
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "inner client is nil")
}

func TestPowerFlexMetricsClient_GetStoragePools(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/types/System/instances":
			writeJSON(t, w, []*siotypes.System{{
				ID:    "",
				Name:  "",
				Links: []*siotypes.Link{{Rel: "/api/System/relationship/ProtectionDomain", HREF: "/api/System/relationship/ProtectionDomain"}},
			}})
		case "/api/System/relationship/ProtectionDomain":
			writeJSON(t, w, []*siotypes.ProtectionDomain{{
				ID:    "pd-1",
				Name:  "pd-1",
				Links: []*siotypes.Link{{Rel: "/api/ProtectionDomain/relationship/StoragePool", HREF: "/api/ProtectionDomain/relationship/StoragePool"}},
			}})
		case "/api/ProtectionDomain/relationship/StoragePool":
			writeJSON(t, w, []*siotypes.StoragePool{{
				ID:    "pool-1",
				Name:  "pool-1",
				Links: []*siotypes.Link{{Rel: "/api/StoragePool/relationship/Statistics", HREF: "/api/StoragePool/relationship/Statistics"}},
			}})
		case "/api/StoragePool/relationship/Statistics":
			writeJSON(t, w, siotypes.Statistics{
				MaxCapacityInKb:                          1024,
				CapacityInUseInKb:                        256,
				CapacityAvailableForVolumeAllocationInKb: 768,
				SnapCapacityInUseInKb:                    64,
				VolumeAddressSpaceInKb:                   128,
				NetUserDataCapacityInKb:                  512,
				CompressedDataCompressionRatio:           2.5,
				ThinCapacityAllocatedInKb:                512,
				OverallUsageRatio:                        1.75,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := newTestScaleIOClient(t, server.URL)
	metricsClient := NewPowerFlexMetricsClient(client, "system-1")

	pools, err := metricsClient.GetStoragePools(context.Background())
	require.NoError(t, err)
	require.Len(t, pools, 1)
	assert.Equal(t, "pool-1", pools[0].PoolID)
	assert.Equal(t, "pool-1", pools[0].PoolName)
	assert.Equal(t, int64(1024), pools[0].MaxCapacityInKb)
	assert.Equal(t, int64(256), pools[0].CapacityInUseInKb)
	assert.Equal(t, 2.5, pools[0].CompressionRatio)
	assert.Equal(t, int64(512), pools[0].ThinCapacityAllocatedInKb)
	assert.Equal(t, 1.75, pools[0].OverallUsageRatio)
}

func TestPowerFlexMetricsClient_GetStoragePools_Gen2Branches(t *testing.T) {
	t.Run("gen2 metrics path", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/types/System/instances":
				writeJSON(t, w, []*siotypes.System{{ID: "", Name: "", Links: []*siotypes.Link{{Rel: "/api/System/relationship/ProtectionDomain", HREF: "/api/System/relationship/ProtectionDomain"}}}})
			case "/api/System/relationship/ProtectionDomain":
				writeJSON(t, w, []*siotypes.ProtectionDomain{{ID: "pd-1", Name: "pd-1", GenType: siotypes.GenTypeEC, Links: []*siotypes.Link{{Rel: "/api/ProtectionDomain/relationship/StoragePool", HREF: "/api/ProtectionDomain/relationship/StoragePool"}}}})
			case "/api/ProtectionDomain/relationship/StoragePool":
				writeJSON(t, w, []*siotypes.StoragePool{{ID: "pool-1", Name: "pool-1"}})
			case "/dtapi/rest/v1/metrics/query":
				w.Header().Set("Content-Type", "application/json")
				_, err := w.Write([]byte(`{
					"format": "ID_TIMESTAMP_METRIC",
					"resource_type": "storage_pool",
					"timestamps": ["2026-06-28T06:58:04Z"],
					"resources": [
						{
							"id": "pool-1",
							"metrics": [
								{"name": "physical_free", "values": ["1048576"]},
								{"name": "physical_used", "values": ["524288"]},
								{"name": "thin_provisioning_ratio", "values": ["85.73023"]},
								{"name": "compression_ratio", "values": ["0.41805917"]},
								{"name": "data_reduction_ratio", "values": ["0.41805917"]}
							]
						}
					]
				}`))
				require.NoError(t, err)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		client := newTestScaleIOClient(t, server.URL)
		metricsClient := NewPowerFlexMetricsClient(client, "system-1")

		pools, err := metricsClient.GetStoragePools(context.Background())
		require.NoError(t, err)
		require.Len(t, pools, 1)
		assert.Equal(t, "pool-1", pools[0].PoolID)
		assert.Equal(t, int64(1536), pools[0].MaxCapacityInKb)
		assert.Equal(t, int64(512), pools[0].CapacityInUseInKb)
		assert.Equal(t, int64(1024), pools[0].CapacityAvailableForVolumeAllocationInKb)
		assert.InDelta(t, 0.41805917, pools[0].CompressionRatio, 0.000001)
		assert.InDelta(t, 0.41805917, pools[0].OverallUsageRatio, 0.000001)
		assert.Equal(t, int64(1317), pools[0].ThinCapacityAllocatedInKb)
	})

	t.Run("gen2 metrics fallback when response is empty", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/types/System/instances":
				writeJSON(t, w, []*siotypes.System{{ID: "", Name: "", Links: []*siotypes.Link{{Rel: "/api/System/relationship/ProtectionDomain", HREF: "/api/System/relationship/ProtectionDomain"}}}})
			case "/api/System/relationship/ProtectionDomain":
				writeJSON(t, w, []*siotypes.ProtectionDomain{{ID: "pd-1", Name: "pd-1", GenType: siotypes.GenTypeEC, Links: []*siotypes.Link{{Rel: "/api/ProtectionDomain/relationship/StoragePool", HREF: "/api/ProtectionDomain/relationship/StoragePool"}}}})
			case "/api/ProtectionDomain/relationship/StoragePool":
				writeJSON(t, w, []*siotypes.StoragePool{{ID: "pool-1", Name: "pool-1"}})
			case "/dtapi/rest/v1/metrics/query":
				writeJSON(t, w, siotypes.MetricsResponse{ResourceType: "storage_pool", Resources: []siotypes.Resource{}})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		client := newTestScaleIOClient(t, server.URL)
		metricsClient := NewPowerFlexMetricsClient(client, "system-1")

		pools, err := metricsClient.GetStoragePools(context.Background())
		require.NoError(t, err)
		require.Len(t, pools, 1)
		assert.Equal(t, "pool-1", pools[0].PoolID)
		assert.Equal(t, "pool-1", pools[0].PoolName)
		assert.Equal(t, int64(0), pools[0].MaxCapacityInKb)
	})
}

func TestPowerFlexMetricsClient_GetRCGStats(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/types/ReplicationConsistencyGroup/instances":
			writeJSON(t, w, []*siotypes.ReplicationConsistencyGroup{{
				ID:                  "rcg-1",
				Name:                "rcg-1",
				CurrConsistMode:     "Consistent",
				LifetimeState:       "Active",
				LocalActivityState:  "Local",
				RemoteActivityState: "Remote",
				RpoInSeconds:        30,
				FreezeState:         "Frozen",
				PauseMode:           "StopDataTransfer",
				FailoverState:       "Done",
				FailoverType:        "Failover",
				Links:               []*siotypes.Link{{Rel: "self", HREF: "/api/instances/ReplicationConsistencyGroup::rcg-1"}},
			}})
		case "/api/instances/ReplicationConsistencyGroup::rcg-1/relationships/Statistics":
			writeJSON(t, w, siotypes.ReplicationConsistencyGroupStatistics{
				LagReceivedInMillis:   1,
				LagAppliedInMillis:    2,
				LagPersistentInMillis: 3,
				RplTransmitBwc:        siotypes.BWC{TotalWeightInKb: 100, NumSeconds: 10},
				RplReceiveBwc:         siotypes.BWC{TotalWeightInKb: 80, NumSeconds: 10},
				RplRemoteApplyBwc:     siotypes.BWC{TotalWeightInKb: 60, NumSeconds: 10},
				RplTransmitLatency:    siotypes.BWC{TotalWeightInKb: 4, NumSeconds: 1},
				RplReceiveLatency:     siotypes.BWC{TotalWeightInKb: 5, NumSeconds: 1},
				RplApplyLatency:       siotypes.BWC{TotalWeightInKb: 6, NumSeconds: 1},
			})
		case "/api/instances/ReplicationConsistencyGroup::rcg-1/relationships/ReplicationPair":
			writeJSON(t, w, []*siotypes.ReplicationPair{{ID: "pair-1"}, {ID: "pair-2"}})
		case "/api/instances/ReplicationPair::pair-1/relationships/Statistics":
			writeJSON(t, w, siotypes.QueryReplicationPairStatistics{InitialCopyProgress: 50})
		case "/api/instances/ReplicationPair::pair-2/relationships/Statistics":
			writeJSON(t, w, siotypes.QueryReplicationPairStatistics{InitialCopyProgress: 75})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := newTestScaleIOClient(t, server.URL)
	metricsClient := NewPowerFlexMetricsClient(client, "system-1")

	rcgs, err := metricsClient.GetRCGStats(context.Background())
	require.NoError(t, err)
	require.Len(t, rcgs, 1)
	assert.Equal(t, "rcg-1", rcgs[0].RCGID)
	assert.Equal(t, int64(30), rcgs[0].RPOSeconds)
	assert.Equal(t, 2, rcgs[0].PairCount)
	require.Len(t, rcgs[0].PairProgress, 2)
	assert.Equal(t, "pair-1", rcgs[0].PairProgress[0].PairID)
	assert.Equal(t, float64(50), rcgs[0].PairProgress[0].Progress)
	// FreezeState=Frozen → 1, PauseMode=StopDataTransfer → 1, FailoverType=Failover → 1
	assert.Equal(t, 1, rcgs[0].FreezeState, "FreezeState should be 1 when freezeState=Frozen")
	assert.Equal(t, 1, rcgs[0].PauseMode, "PauseMode should be 1 when pauseMode=StopDataTransfer")
	assert.Equal(t, 1, rcgs[0].FailoverState, "FailoverState should be 1 when failoverType=Failover")
}

func TestPowerFlexMetricsClient_GetVolumes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/types/Volume/instances":
			writeJSON(t, w, []*siotypes.Volume{{
				ID:                     "vol-1",
				Name:                   "vol-1",
				SizeInKb:               100,
				StoragePoolID:          "pool-1",
				VolumeReplicationState: "Normal",
				MappedSdcInfo:          []*siotypes.MappedSdcInfo{{SdcID: "node-1"}},
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := newTestScaleIOClient(t, server.URL)
	metricsClient := NewPowerFlexMetricsClient(client, "system-1")

	volumes, err := metricsClient.GetVolumes(context.Background())
	require.NoError(t, err)
	require.Len(t, volumes, 1)
	assert.Equal(t, "vol-1", volumes[0].VolumeID)
	assert.Equal(t, "node-1", volumes[0].AttachedNodeID)
	assert.Equal(t, int64(100), volumes[0].SizeInKb)

	t.Run("missing mapped SDCs leaves attached node empty", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/types/Volume/instances":
				writeJSON(t, w, []*siotypes.Volume{{ID: "vol-2", Name: "vol-2", SizeInKb: 200, StoragePoolID: "pool-1", VolumeReplicationState: "Normal"}})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		client := newTestScaleIOClient(t, server.URL)
		metricsClient := NewPowerFlexMetricsClient(client, "system-1")

		volumes, err := metricsClient.GetVolumes(context.Background())
		require.NoError(t, err)
		require.Len(t, volumes, 1)
		assert.Equal(t, "", volumes[0].AttachedNodeID)
	})
}

func TestPowerFlexMetricsClient_HelperBranches(t *testing.T) {
	assert.Equal(t, float64(0), getMetricValue(nil, "missing"))
	assert.Equal(t, float64(7), getMetricValue([]siotypes.Metric{{Name: "present", Values: []float64{7}}}, "present"))
	assert.Equal(t, int64(4), bytesToKiB(4096))
	assert.Equal(t, float64(0), normalizeThinProvisioningRatio(-1))
	assert.Equal(t, float64(0.85), normalizeThinProvisioningRatio(85))
	assert.Equal(t, float64(0.8573023), normalizeThinProvisioningRatio(0.8573023))
	assert.Equal(t, int64(0), estimateThinCapacityAllocatedInKb(0, 1024))
	assert.Equal(t, int64(250), estimateThinCapacityAllocatedInKb(0.25, 1000))
	assert.Equal(t, 1, boolToInt(true))
	assert.Equal(t, 0, boolToInt(false))
	assert.Equal(t, float64(0), volumeHealthy("Error"))
	assert.Equal(t, float64(0), volumeHealthy("ReplicationPaused"))
	assert.Equal(t, float64(1), volumeHealthy("Normal"))
	assert.Equal(t, float64(1), encodeState("SomeState"))
	assert.Equal(t, float64(0), encodeState(""))
	assert.Equal(t, float64(1), encodeActivityState("Active"))
	assert.Equal(t, float64(0), encodeActivityState("Inactive"))
}

func TestK8sMetadataChecker_HelperBranches(t *testing.T) {
	assert.Equal(t, "", extractVolumeIDFromHandle(""))
	assert.Equal(t, "array", extractVolumeIDFromHandle("array/volume-1"))
	assert.Equal(t, "volume-1", extractVolumeIDFromHandle("array-volume-1"))
	assert.Equal(t, "plain", extractVolumeIDFromHandle("plain"))

	assert.Equal(t, "", extractArrayIDFromHandle(""))
	assert.Equal(t, "volume", extractArrayIDFromHandle("array/volume/handle"))
	assert.Equal(t, "array", extractArrayIDFromHandle("array-volume-1"))
	assert.Equal(t, "", extractArrayIDFromHandle("plain"))

	assert.Equal(t, "iSCSI", normalizeProtocol("iscsi"))
	assert.Equal(t, "iSCSI", normalizeProtocol("scsi"))
	assert.Equal(t, "FC", normalizeProtocol("fc"))
	assert.Equal(t, "NVMeTCP", normalizeProtocol("nvme_tcp"))
	assert.Equal(t, "NFS", normalizeProtocol("nfs"))
	assert.Equal(t, "unknown", normalizeProtocol("something-else"))
}

func TestK8sMetadataChecker_RefreshCacheAndErrors(t *testing.T) {
	pvName := "pv-managed"
	attachedPVName := pvName
	client := fake.NewSimpleClientset(
		&v1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: pvName},
			Spec: v1.PersistentVolumeSpec{
				PersistentVolumeSource: v1.PersistentVolumeSource{
					CSI: &v1.CSIPersistentVolumeSource{
						Driver:           "csi-vxflexos.dellemc.com",
						VolumeHandle:     "array1-vol1",
						VolumeAttributes: map[string]string{"Protocol": "iSCSI"},
					},
				},
			},
		},
		&v1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "pv-foreign"},
			Spec: v1.PersistentVolumeSpec{
				PersistentVolumeSource: v1.PersistentVolumeSource{
					CSI: &v1.CSIPersistentVolumeSource{
						Driver:       "other-driver",
						VolumeHandle: "array2-vol2",
					},
				},
			},
		},
		&v1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "pv-invalid"},
			Spec: v1.PersistentVolumeSpec{
				PersistentVolumeSource: v1.PersistentVolumeSource{
					CSI: &v1.CSIPersistentVolumeSource{
						Driver:       "csi-vxflexos.dellemc.com",
						VolumeHandle: "",
					},
				},
			},
		},
		&storagev1.VolumeAttachment{
			ObjectMeta: metav1.ObjectMeta{Name: "va-1"},
			Spec: storagev1.VolumeAttachmentSpec{
				Attacher: "csi-vxflexos.dellemc.com",
				Source: storagev1.VolumeAttachmentSource{
					PersistentVolumeName: &attachedPVName,
				},
			},
			Status: storagev1.VolumeAttachmentStatus{Attached: true},
		},
	)

	checker := NewK8sMetadataChecker(client, "csi-vxflexos.dellemc.com")
	require.True(t, checker.Available())
	require.NoError(t, checker.RefreshCache(context.Background()))

	managed, err := checker.IsDriverManaged(context.Background(), "array1-vol1")
	require.NoError(t, err)
	assert.True(t, managed)

	managedByName, err := checker.IsDriverManagedByName(context.Background(), pvName)
	require.NoError(t, err)
	assert.True(t, managedByName)

	protocol := checker.GetProtocol(context.Background(), "array1-vol1")
	assert.Equal(t, "iSCSI", protocol)

	attached, err := checker.GetAttachmentStateForVolumeID(context.Background(), "array1-vol1", "array1")
	require.NoError(t, err)
	assert.True(t, attached)

	attached, err = checker.GetAttachmentStateForVolumeID(context.Background(), "array1-vol1", "other-array")
	require.NoError(t, err)
	assert.False(t, attached)

	attached, err = checker.GetAttachmentStateForPVName(context.Background(), pvName)
	require.NoError(t, err)
	assert.True(t, attached)

	checker.MarkDeleteComplete("array1-vol1")
	checker.MarkDeleteCompleteByName(pvName)

	checker.cacheMu.RLock()
	_, hasVolume := checker.volumeIDCache["vol1"]
	_, hasName := checker.volumeNameCache[pvName]
	_, hasProtocol := checker.protocolCache["vol1"]
	_, hasPV := checker.pvNameCache["vol1"]
	_, hasArray := checker.arrayIDCache["vol1"]
	_, hasAttachment := checker.attachmentCache[pvName]
	checker.cacheMu.RUnlock()

	assert.False(t, hasVolume)
	assert.False(t, hasName)
	assert.False(t, hasProtocol)
	assert.False(t, hasPV)
	assert.False(t, hasArray)
	assert.False(t, hasAttachment)

	t.Run("pv list error", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		client.PrependReactor("list", "persistentvolumes", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("pv list failed")
		})
		checker := NewK8sMetadataChecker(client, "csi-vxflexos.dellemc.com")
		err := checker.RefreshCache(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list PVs")
	})

	t.Run("volume attachment list error", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		client.PrependReactor("list", "volumeattachments", func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errors.New("va list failed")
		})
		checker := NewK8sMetadataChecker(client, "csi-vxflexos.dellemc.com")
		err := checker.RefreshCache(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list VolumeAttachments")
	})
}

func TestMetricsRuntime_AdditionalBranches(t *testing.T) {
	t.Run("nil runtime returns error", func(t *testing.T) {
		var runtime *MetricsRuntime
		result, err := runtime.Do(context.Background(), "endpoint", "cache-key", func(context.Context) (any, error) {
			return "unused", nil
		})
		assert.Nil(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "metrics runtime is nil")
	})

	t.Run("canceled context serves cached result", func(t *testing.T) {
		var staleStates []bool
		runtime := NewMetricsRuntime("array-1", func(_ string, stale bool) {
			staleStates = append(staleStates, stale)
		})
		runtime.cache.Set("array-1:endpoint", "cached-value")

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		result, err := runtime.Do(ctx, "endpoint", "array-1:endpoint", func(context.Context) (any, error) {
			return nil, nil
		})
		require.NoError(t, err)
		assert.Equal(t, "cached-value", result)
		assert.Equal(t, []bool{true}, staleStates)
	})

	t.Run("serveStaleOrError returns error when cache is empty", func(t *testing.T) {
		runtime := NewMetricsRuntime("array-1", nil)
		result, err := runtime.serveStaleOrError("missing", errors.New("boom"))
		assert.Nil(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "boom")
	})

	t.Run("timeout path records stale fallback", func(t *testing.T) {
		var staleStates []bool
		runtime := NewMetricsRuntime("array-1", func(_ string, stale bool) { staleStates = append(staleStates, stale) })
		runtime.timeout = time.Millisecond
		result, err := runtime.Do(context.Background(), "endpoint", "timeout-key", func(ctx context.Context) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		})
		assert.Nil(t, result)
		require.Error(t, err)
		assert.Equal(t, []bool{true}, staleStates)
	})
}

func TestArrayHealthCollector_HelperAndRuntimeBranches(t *testing.T) {
	collector, err := NewArrayHealthCollector(&stubArrayHealthClient{version: "4.0", cluster: &siotypes.MdmCluster{ClusterState: "Optimal"}}, prometheus.NewRegistry(), "", "")
	require.NoError(t, err)
	assert.Equal(t, "unknown", collector.managementEndpoint)
	collector.SetRuntime(nil)
	assert.Nil(t, collector.runtime)

	reg := prometheus.NewRegistry()
	successCollector, err := NewArrayHealthCollector(&stubArrayHealthClient{version: "4.0", cluster: &siotypes.MdmCluster{ClusterState: "Optimal"}}, reg, "system-1", "https://gateway.example")
	require.NoError(t, err)
	require.NoError(t, successCollector.Collect(context.Background()))

	healthMF := gatherMetric(t, reg, "dell_powerflex_array_healthy")
	require.NotNil(t, healthMF)
	health, ok := findGaugeValue(healthMF, map[string]string{"system_id": "system-1"})
	require.True(t, ok)
	assert.Equal(t, 1.0, health)

	stateMF := gatherMetric(t, reg, "dell_powerflex_mdm_cluster_state")
	require.NotNil(t, stateMF)
	state, ok := findGaugeValue(stateMF, map[string]string{"system_id": "system-1", "cluster_state": "online"})
	require.True(t, ok)
	assert.Equal(t, 1.0, state)

	failReg := prometheus.NewRegistry()
	failingCollector, err := NewArrayHealthCollector(&stubArrayHealthClient{versionErr: errors.New("connection refused")}, failReg, "system-1", "https://gateway.example")
	require.NoError(t, err)
	err = failingCollector.Collect(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gateway probe failed")

	healthMF = gatherMetric(t, failReg, "dell_powerflex_array_healthy")
	require.NotNil(t, healthMF)
	health, ok = findGaugeValue(healthMF, map[string]string{"system_id": "system-1"})
	require.True(t, ok)
	assert.Equal(t, 0.0, health)

	stateMF = gatherMetric(t, failReg, "dell_powerflex_mdm_cluster_state")
	require.NotNil(t, stateMF)
	state, ok = findGaugeValue(stateMF, map[string]string{"system_id": "system-1", "cluster_state": "unknown"})
	require.True(t, ok)
	assert.Equal(t, 1.0, state)

	labels := collector.getLabels()
	assert.Equal(t, "unknown", labels.systemID)
	assert.Equal(t, "unknown", labels.managementEndpoint)
	assert.Equal(t, "unknown", normalizeClusterState(""))
	assert.Equal(t, "degraded", normalizeClusterState("Warning"))
	assert.Equal(t, "configured", normalizeClusterState("configured"))
	// ClusteredNormal is the canonical healthy PowerFlex MDM state.
	assert.Equal(t, "online", normalizeClusterState("ClusteredNormal"))
	// "Abnormal" must NOT be misclassified as "online" via the "normal" substring.
	assert.Equal(t, "unknown", normalizeClusterState("Abnormal"))
	// Plain "Normal" suffix (no "ab" prefix) should still map to "online".
	assert.Equal(t, "online", normalizeClusterState("Normal"))
	// Unstable must stay degraded (regression: was misclassified as "online" via "stable" before fix).
	assert.Equal(t, "degraded", normalizeClusterState("Unstable"))
}

func TestPowerFlexVolumeClient_GetVolumes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/types/System/instances":
			writeJSON(t, w, []*siotypes.System{{
				ID:    "",
				Name:  "",
				Links: []*siotypes.Link{{Rel: "/api/System/relationship/ProtectionDomain", HREF: "/api/System/relationship/ProtectionDomain"}},
			}})
		case "/api/System/relationship/ProtectionDomain":
			writeJSON(t, w, []*siotypes.ProtectionDomain{{
				ID:    "pd-1",
				Name:  "pd-1",
				Links: []*siotypes.Link{{Rel: "/api/ProtectionDomain/relationship/StoragePool", HREF: "/api/ProtectionDomain/relationship/StoragePool"}},
			}})
		case "/api/types/Volume/instances":
			writeJSON(t, w, []*siotypes.Volume{{
				ID:                     "vol-1",
				Name:                   "vol-1",
				SizeInKb:               100,
				StoragePoolID:          "pool-1",
				VolumeReplicationState: "Normal",
				MappedSdcInfo:          []*siotypes.MappedSdcInfo{{SdcID: "node-1"}},
				Links:                  []*siotypes.Link{{Rel: "/api/Volume/relationship/Statistics", HREF: "/api/Volume/relationship/Statistics"}},
			}})
		case "/api/Volume/relationship/Statistics":
			writeJSON(t, w, siotypes.VolumeStatistics{
				UserDataReadBwc:  siotypes.BWC{TotalWeightInKb: 2048, NumSeconds: 2},
				UserDataWriteBwc: siotypes.BWC{TotalWeightInKb: 1024, NumSeconds: 4},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := newTestScaleIOClient(t, server.URL)
	metadata := &fakeVolumeMetadataProvider{available: true, managed: map[string]bool{"vol-1": true}}
	k8sClient := fake.NewSimpleClientset(&v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-1"},
		Spec: v1.PersistentVolumeSpec{
			StorageClassName: "sc-1",
			PersistentVolumeSource: v1.PersistentVolumeSource{
				CSI: &v1.CSIPersistentVolumeSource{
					Driver:       "csi-vxflexos.dellemc.com",
					VolumeHandle: "system-1-vol-1",
				},
			},
			ClaimRef: &v1.ObjectReference{Name: "pvc-1", Namespace: "default"},
		},
	})

	volumeClient := NewPowerFlexVolumeClient(client, k8sClient, metadata, "system-1")
	volumes, err := volumeClient.GetVolumes(context.Background())
	require.NoError(t, err)
	require.Len(t, volumes, 1)
	assert.Equal(t, "vol-1", volumes[0].VolumeID)
	assert.Equal(t, "pv-1", volumes[0].PVName)
	assert.Equal(t, "pvc-1", volumes[0].PVCName)
	assert.Equal(t, "default", volumes[0].Namespace)
	assert.Equal(t, "sc-1", volumes[0].StorageClass)
	assert.Equal(t, "node-1", volumes[0].AttachedNodeID)
	assert.InDelta(t, 1024.0, volumes[0].ReadBandwidthKBps, 0.001)
	assert.InDelta(t, 256.0, volumes[0].WriteBandwidthKBps, 0.001)
}

func TestPowerFlexVolumeClient_bwcToKBps(t *testing.T) {
	// Test bwcToKBps helper function
	tests := []struct {
		name     string
		totalKB  int
		seconds  int
		expected float64
	}{
		{"Valid calculation", 5000, 10, 500.0},
		{"Zero seconds", 1000, 0, 0.0},
		{"Zero total", 0, 10, 0.0},
		{"Large values", 1000000, 100, 10000.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := bwcToKBps(tt.totalKB, tt.seconds)
			assert.InDelta(t, tt.expected, result, 0.001)
		})
	}
}

func TestWithGoscaleIOContext(t *testing.T) {
	t.Run("returns error when client is nil", func(t *testing.T) {
		err := withGoscaleIOContext(context.Background(), nil, func() error {
			return nil
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "goscaleio client is nil")
	})

	t.Run("uses background context when ctx is nil", func(t *testing.T) {
		client := newTestScaleIOClient(t, "http://127.0.0.1")
		called := false
		err := withGoscaleIOContext(nil, client, func() error {
			called = true
			return nil
		})
		require.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("propagates function errors", func(t *testing.T) {
		client := newTestScaleIOClient(t, "http://127.0.0.1")
		expectedError := errors.New("test error")
		err := withGoscaleIOContext(context.Background(), client, func() error {
			return expectedError
		})
		require.Error(t, err)
		assert.Equal(t, expectedError, err)
	})
}
