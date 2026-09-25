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
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Ecosystems/container-storage-modules/src/csi-vxflexos/v2/service/collectors"
	sio "github.com/Ecosystems/container-storage-modules/src/goscaleio"
	siotypes "github.com/Ecosystems/container-storage-modules/src/goscaleio/types/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockArrayHealthClient struct {
	version      string
	versionErr   error
	cluster      *siotypes.MdmCluster
	clusterErr   error
	versionCalls int
	clusterCalls int
}

func (m *mockArrayHealthClient) GetVersion(context.Context) (string, error) {
	m.versionCalls++
	return m.version, m.versionErr
}

func (m *mockArrayHealthClient) GetMDMClusterDetails(context.Context) (*siotypes.MdmCluster, error) {
	m.clusterCalls++
	return m.cluster, m.clusterErr
}

type arrayHealthFailingRegisterer struct{ err error }

func (f arrayHealthFailingRegisterer) Register(prometheus.Collector) error  { return f.err }
func (f arrayHealthFailingRegisterer) MustRegister(...prometheus.Collector) {}
func (f arrayHealthFailingRegisterer) Unregister(prometheus.Collector) bool { return false }

func TestArrayHealthCollector_Collect_Success(t *testing.T) {
	client := &mockArrayHealthClient{
		version: "4.0.0",
		cluster: &siotypes.MdmCluster{ClusterState: "Optimal"},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewArrayHealthCollector(client, reg, "system-1", "https://gateway.example")
	require.NoError(t, err)

	err = c.Collect(context.Background())
	require.NoError(t, err)

	healthMF := gatherMetric(t, reg, "dell_powerflex_array_healthy")
	require.NotNil(t, healthMF)
	health, ok := findGaugeValue(healthMF, map[string]string{"system_id": "system-1"})
	require.True(t, ok)
	assert.Equal(t, 1.0, health)

	gwMF := gatherMetric(t, reg, "dell_csi_gateway_status")
	require.NotNil(t, gwMF)
	gw, ok := findGaugeValue(gwMF, map[string]string{"driver": "csi-vxflexos", "gateway_address": "https://gateway.example"})
	require.True(t, ok)
	assert.Equal(t, 1.0, gw)

	endpointMF := gatherMetric(t, reg, "dell_powerflex_array_management_endpoint")
	require.NotNil(t, endpointMF)
	endpoint, ok := findGaugeValue(endpointMF, map[string]string{"system_id": "system-1", "management_endpoint": "https://gateway.example"})
	require.True(t, ok)
	assert.Equal(t, 1.0, endpoint)

	stateMF := gatherMetric(t, reg, "dell_powerflex_mdm_cluster_state")
	require.NotNil(t, stateMF)
	state, ok := findGaugeValue(stateMF, map[string]string{"system_id": "system-1", "cluster_state": "online"})
	require.True(t, ok)
	assert.Equal(t, 1.0, state)

	assert.Equal(t, 1, client.versionCalls)
	assert.Equal(t, 1, client.clusterCalls)
}

func TestArrayHealthCollector_Collect_RuntimeSuccess(t *testing.T) {
	client := &mockArrayHealthClient{
		version: "4.0.0",
		cluster: &siotypes.MdmCluster{ClusterState: "Configured"},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewArrayHealthCollector(client, reg, "system-1", "https://gateway.example")
	require.NoError(t, err)
	c.SetRuntime(collectors.NewMetricsRuntime("system-1", nil))

	require.NoError(t, c.Collect(context.Background()))

	healthMF := gatherMetric(t, reg, "dell_powerflex_array_healthy")
	require.NotNil(t, healthMF)
	health, ok := findGaugeValue(healthMF, map[string]string{"system_id": "system-1"})
	require.True(t, ok)
	assert.Equal(t, 1.0, health)

	stateMF := gatherMetric(t, reg, "dell_powerflex_mdm_cluster_state")
	require.NotNil(t, stateMF)
	state, ok := findGaugeValue(stateMF, map[string]string{"system_id": "system-1", "cluster_state": "configured"})
	require.True(t, ok)
	assert.Equal(t, 1.0, state)

	assert.Equal(t, 1, client.versionCalls)
	assert.Equal(t, 1, client.clusterCalls)
}

func TestArrayHealthCollector_Collect_ClusteredNormal(t *testing.T) {
	client := &mockArrayHealthClient{
		version: "4.0.0",
		cluster: &siotypes.MdmCluster{ClusterState: "ClusteredNormal"},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewArrayHealthCollector(client, reg, "system-1", "https://gateway.example")
	require.NoError(t, err)

	err = c.Collect(context.Background())
	require.NoError(t, err)

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

	assert.Equal(t, 1, client.versionCalls)
	assert.Equal(t, 1, client.clusterCalls)
}

// TestArrayHealthCollector_Collect_UnstableState verifies that "unstable" maps to
// "degraded" and not "online" (a pre-fix regression where Contains("stable") would
// match before the degraded branch).
func TestArrayHealthCollector_Collect_UnstableState(t *testing.T) {
	client := &mockArrayHealthClient{
		version: "4.0.0",
		cluster: &siotypes.MdmCluster{ClusterState: "Unstable"},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewArrayHealthCollector(client, reg, "system-1", "https://gateway.example")
	require.NoError(t, err)

	err = c.Collect(context.Background())
	require.NoError(t, err)

	healthMF := gatherMetric(t, reg, "dell_powerflex_array_healthy")
	require.NotNil(t, healthMF)
	health, ok := findGaugeValue(healthMF, map[string]string{"system_id": "system-1"})
	require.True(t, ok)
	assert.Equal(t, 0.0, health, "unstable state should report array as unhealthy")

	stateMF := gatherMetric(t, reg, "dell_powerflex_mdm_cluster_state")
	require.NotNil(t, stateMF)
	_, wrongClass := findGaugeValue(stateMF, map[string]string{"system_id": "system-1", "cluster_state": "online"})
	assert.False(t, wrongClass, "unstable should NOT be classified as online")
	degraded, ok := findGaugeValue(stateMF, map[string]string{"system_id": "system-1", "cluster_state": "degraded"})
	require.True(t, ok)
	assert.Equal(t, 1.0, degraded)
}

// TestArrayHealthCollector_Collect_AbnormalState verifies that "abnormal" maps to
// "unknown" rather than "online" (HasSuffix("normal") guard).
func TestArrayHealthCollector_Collect_AbnormalState(t *testing.T) {
	client := &mockArrayHealthClient{
		version: "4.0.0",
		cluster: &siotypes.MdmCluster{ClusterState: "Abnormal"},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewArrayHealthCollector(client, reg, "system-1", "https://gateway.example")
	require.NoError(t, err)

	err = c.Collect(context.Background())
	require.NoError(t, err)

	stateMF := gatherMetric(t, reg, "dell_powerflex_mdm_cluster_state")
	require.NotNil(t, stateMF)
	_, online := findGaugeValue(stateMF, map[string]string{"system_id": "system-1", "cluster_state": "online"})
	assert.False(t, online, "abnormal should NOT be classified as online")
	unknown, ok := findGaugeValue(stateMF, map[string]string{"system_id": "system-1", "cluster_state": "unknown"})
	require.True(t, ok)
	assert.Equal(t, 1.0, unknown)
}

func TestPowerFlexArrayHealthClient_AdapterMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode("4.0.0"))
		case "/api/types/System/instances":
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode([]*siotypes.System{{ID: "", Name: "", Links: []*siotypes.Link{{Rel: "/api/System/relationship/ProtectionDomain", HREF: "/api/System/relationship/ProtectionDomain"}}}}))
		case "/api/instances/System/queryMdmCluster":
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(siotypes.MdmCluster{ClusterState: "Optimal"}))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := sio.NewClientWithArgs(server.URL, "3.6", math.MaxInt64, true, false, "")
	require.NoError(t, err)

	adapter := collectors.NewPowerFlexArrayHealthClient(client)
	version, err := adapter.GetVersion(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "4.0", version)

	cluster, err := adapter.GetMDMClusterDetails(context.Background())
	require.NoError(t, err)
	require.NotNil(t, cluster)
	assert.Equal(t, "Optimal", cluster.ClusterState)
}

func TestArrayHealthCollector_ConstructorError(t *testing.T) {
	_, err := collectors.NewArrayHealthCollector(&mockArrayHealthClient{}, arrayHealthFailingRegisterer{err: errors.New("register failed")}, "system-1", "https://gateway.example")
	require.Error(t, err)
}

func TestArrayHealthCollector_Collect_GatewayFailure(t *testing.T) {
	client := &mockArrayHealthClient{versionErr: errors.New("connection refused")}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewArrayHealthCollector(client, reg, "system-1", "https://gateway.example")
	require.NoError(t, err)

	err = c.Collect(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gateway probe failed")

	healthMF := gatherMetric(t, reg, "dell_powerflex_array_healthy")
	require.NotNil(t, healthMF)
	health, ok := findGaugeValue(healthMF, map[string]string{"system_id": "system-1"})
	require.True(t, ok)
	assert.Equal(t, 0.0, health)

	gwMF := gatherMetric(t, reg, "dell_csi_gateway_status")
	require.NotNil(t, gwMF)
	gw, ok := findGaugeValue(gwMF, map[string]string{"driver": "csi-vxflexos", "gateway_address": "https://gateway.example"})
	require.True(t, ok)
	assert.Equal(t, 0.0, gw)

	stateMF := gatherMetric(t, reg, "dell_powerflex_mdm_cluster_state")
	require.NotNil(t, stateMF)
	state, ok := findGaugeValue(stateMF, map[string]string{"system_id": "system-1", "cluster_state": "unknown"})
	require.True(t, ok)
	assert.Equal(t, 1.0, state)
}

func TestArrayHealthCollector_Name(t *testing.T) {
	reg := prometheus.NewRegistry()
	client := &mockArrayHealthClient{
		version: "4.0.0",
		cluster: &siotypes.MdmCluster{},
	}
	c, err := collectors.NewArrayHealthCollector(client, reg, "system-1", "https://gateway.example")
	require.NoError(t, err)
	assert.Equal(t, "ArrayHealthCollector", c.Name())
}
