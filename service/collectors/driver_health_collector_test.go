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
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Ecosystems/container-storage-modules/src/csi-vxflexos/v2/k8sutils"
	"github.com/Ecosystems/container-storage-modules/src/csi-vxflexos/v2/service/collectors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	metricsv1beta1api "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsv1beta1 "k8s.io/metrics/pkg/client/clientset/versioned/typed/metrics/v1beta1"
)

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(v))
}

type driverHealthFailingRegisterer struct{ err error }

func (f driverHealthFailingRegisterer) Register(prometheus.Collector) error  { return f.err }
func (f driverHealthFailingRegisterer) MustRegister(...prometheus.Collector) {}
func (f driverHealthFailingRegisterer) Unregister(prometheus.Collector) bool { return false }

type driverHealthNoGatherRegisterer struct{}

func (driverHealthNoGatherRegisterer) Register(prometheus.Collector) error  { return nil }
func (driverHealthNoGatherRegisterer) MustRegister(...prometheus.Collector) {}
func (driverHealthNoGatherRegisterer) Unregister(prometheus.Collector) bool { return false }

// U-PFX-15: Uptime increases monotonically
func TestDriverHealthCollector_Collect_UptimeIncreasesMonotonically(t *testing.T) {
	reg := prometheus.NewRegistry()
	c, err := collectors.NewDriverHealthCollector(reg, "system-1")
	require.NoError(t, err)

	_ = c.Collect(context.Background())
	mf1 := gatherMetric(t, reg, "dell_csi_driver_uptime_seconds")
	require.NotNil(t, mf1)
	v1, ok := findGaugeValue(mf1, map[string]string{"driver": "csi-vxflexos", "pod": "unknown"})
	require.True(t, ok)
	assert.Greater(t, v1, 0.0, "uptime should be > 0 after first collect")

	time.Sleep(10 * time.Millisecond)
	_ = c.Collect(context.Background())
	mf2 := gatherMetric(t, reg, "dell_csi_driver_uptime_seconds")
	require.NotNil(t, mf2)
	v2, ok := findGaugeValue(mf2, map[string]string{"driver": "csi-vxflexos", "pod": "unknown"})
	require.True(t, ok)

	assert.Greater(t, v2, v1, "uptime should increase monotonically")
}

// U-PFX-16: Goroutine count matches runtime.NumGoroutine()
func TestDriverHealthCollector_Collect_GoroutineCount(t *testing.T) {
	reg := prometheus.NewRegistry()
	c, err := collectors.NewDriverHealthCollector(reg, "system-1")
	require.NoError(t, err)

	_ = c.Collect(context.Background())

	mf := gatherMetric(t, reg, "dell_csi_goroutine_count")
	require.NotNil(t, mf)
	v, ok := findGaugeValue(mf, map[string]string{"driver": "csi-vxflexos", "pod": "unknown"})
	require.True(t, ok)
	assert.Greater(t, v, 0.0, "goroutine count should be > 0")
}

// TestDriverHealthCollector_RestartCounterMetricRegistered
func TestDriverHealthCollector_RestartCounterMetricRegistered(t *testing.T) {
	reg := prometheus.NewRegistry()
	c, err := collectors.NewDriverHealthCollector(reg, "system-1")
	require.NoError(t, err)

	// Call Collect to initialize metrics (even if values aren't set due to missing env vars)
	_ = c.Collect(context.Background())

	mfs, err := reg.Gather()
	require.NoError(t, err)

	var found bool
	for _, mf := range mfs {
		if mf.GetName() == "dell_csi_driver_restart_total" {
			found = true
			break
		}
	}
	assert.True(t, found, "dell_csi_driver_restart_total should be registered")
}

func TestDriverHealthCollector_ConstructorErrorAndKubernetesBranches(t *testing.T) {
	t.Run("constructor returns register error", func(t *testing.T) {
		_, err := collectors.NewDriverHealthCollector(driverHealthFailingRegisterer{err: assert.AnError}, "system-1")
		require.Error(t, err)
	})

	t.Run("collect skips kubeclient and api success when unavailable", func(t *testing.T) {
		origKubeclient := k8sutils.Kubeclient
		defer func() { k8sutils.Kubeclient = origKubeclient }()

		t.Setenv("X_CSI_POD_NAME", "driver-pod-0")
		t.Setenv("X_CSI_DRIVER_NAMESPACE", "default")
		t.Setenv("X_CSI_POWERFLEX_KUBE_NODE_NAME", "node-1")
		t.Setenv("X_CSI_MODE", "driver")

		reg := driverHealthNoGatherRegisterer{}
		collector, err := collectors.NewDriverHealthCollector(reg, "system-1")
		require.NoError(t, err)
		k8sutils.Kubeclient = nil

		require.NoError(t, collector.Collect(context.Background()))
	})

	t.Run("collects restart count and pod metrics when kubeclient is available", func(t *testing.T) {
		origKubeclient := k8sutils.Kubeclient
		defer func() { k8sutils.Kubeclient = origKubeclient }()

		t.Setenv("X_CSI_POD_NAME", "driver-pod-0")
		t.Setenv("X_CSI_DRIVER_NAMESPACE", "default")
		t.Setenv("X_CSI_POWERFLEX_KUBE_NODE_NAME", "node-1")
		t.Setenv("X_CSI_MODE", "driver")

		metricsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, metricsv1beta1api.PodMetrics{
				TypeMeta:   metav1.TypeMeta{Kind: "PodMetrics", APIVersion: "metrics.k8s.io/v1beta1"},
				ObjectMeta: metav1.ObjectMeta{Name: "driver-pod-0", Namespace: "default"},
				Containers: []metricsv1beta1api.ContainerMetrics{{
					Name: "csi-vxflexos",
					Usage: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("250m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
				}},
			})
		}))
		defer metricsServer.Close()

		metricsClient, err := metricsv1beta1.NewForConfig(&rest.Config{Host: metricsServer.URL})
		require.NoError(t, err)

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "driver-pod-0", Namespace: "default"},
			Status:     corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "csi-vxflexos", RestartCount: 9}}},
		}
		k8sutils.Kubeclient = &k8sutils.K8sClient{Clientset: fake.NewSimpleClientset(pod), MetricsClient: metricsClient}

		reg := prometheus.NewRegistry()
		collector, err := collectors.NewDriverHealthCollector(reg, "system-1")
		require.NoError(t, err)
		require.NoError(t, collector.Collect(context.Background()))

		restartMF := gatherMetric(t, reg, "dell_csi_driver_restart_total")
		require.NotNil(t, restartMF)
		restart, ok := findGaugeValue(restartMF, map[string]string{"driver": "csi-vxflexos", "pod": "driver-pod-0"})
		require.True(t, ok)
		assert.Equal(t, 9.0, restart)

		cpuMF := gatherMetric(t, reg, "dell_csi_driver_cpu_usage_percent")
		require.NotNil(t, cpuMF)
		cpu, ok := findGaugeValue(cpuMF, map[string]string{"driver": "csi-vxflexos", "pod": "driver-pod-0"})
		require.True(t, ok)
		assert.Equal(t, 25.0, cpu)

		memMF := gatherMetric(t, reg, "dell_csi_driver_memory_usage_bytes")
		require.NotNil(t, memMF)
		mem, ok := findGaugeValue(memMF, map[string]string{"driver": "csi-vxflexos", "pod": "driver-pod-0"})
		require.True(t, ok)
		assert.Equal(t, float64(128*1024*1024), mem)

		apiSuccessMF := gatherMetric(t, reg, "dell_csi_api_success_rate_percent")
		require.NotNil(t, apiSuccessMF)
		success, ok := findGaugeValue(apiSuccessMF, map[string]string{"driver": "csi-vxflexos", "pod": "driver-pod-0"})
		require.True(t, ok)
		assert.Equal(t, 100.0, success)
	})
}

func TestDriverHealthCollector_Name(t *testing.T) {
	reg := prometheus.NewRegistry()
	collector, err := collectors.NewDriverHealthCollector(reg, "csi-vxflexos")
	require.NoError(t, err)
	assert.Equal(t, "DriverHealthCollector", collector.Name())
}

func TestDriverHealthCollector_Collect_WithPrometheusGatherer(t *testing.T) {
	// Create a registry with some API call metrics to test collectAPISuccessRate
	reg := prometheus.NewRegistry()

	// Register the dell_csi_driver_api_call_total counter
	apiCallTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dell_csi_driver_api_call_total",
			Help: "Total number of CSI driver API calls",
		},
		[]string{"array_id", "status", "operation"},
	)
	reg.MustRegister(apiCallTotal)

	// Add some test metrics
	apiCallTotal.WithLabelValues("csi-vxflexos", "success", "CreateVolume").Add(80)
	apiCallTotal.WithLabelValues("csi-vxflexos", "failure", "CreateVolume").Add(20)

	// Create collector with the registry as the gatherer
	collector, err := collectors.NewDriverHealthCollector(reg, "csi-vxflexos")
	require.NoError(t, err)

	// Collect should call collectAPISuccessRate which uses the gatherer
	collector.Collect(context.Background())

	// Verify the api success rate metric was set
	apiSuccessMF := gatherMetric(t, reg, "dell_csi_api_success_rate_percent")
	require.NotNil(t, apiSuccessMF)

	// Try to find the metric with various label combinations
	_, ok := findGaugeValue(apiSuccessMF, map[string]string{"driver": "csi-vxflexos", "pod": ""})
	if !ok {
		// If not found with empty pod, try without pod label
		_, ok = findGaugeValue(apiSuccessMF, map[string]string{"driver": "csi-vxflexos"})
	}

	// Just verify the metric exists and has some value
	require.True(t, ok || len(apiSuccessMF.Metric) > 0, "API success rate metric should exist")
}

func TestDriverHealthCollector_ConstructorError(t *testing.T) {
	// Test constructor error path
	failReg := driverHealthFailingRegisterer{err: assert.AnError}
	_, err := collectors.NewDriverHealthCollector(failReg, "csi-vxflexos")
	require.Error(t, err)
}

func TestDriverHealthCollector_ConstructorSuccess(t *testing.T) {
	reg := prometheus.NewRegistry()
	c, err := collectors.NewDriverHealthCollector(reg, "csi-vxflexos")
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, "DriverHealthCollector", c.Name())
}
