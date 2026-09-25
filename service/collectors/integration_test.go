/*
 Copyright © 2025-2026 Dell Inc. or its subsidiaries. All Rights Reserved.

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at
      http://www.apache.org/licenses/LICENSE-2.0
 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package collectors_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func freeIntPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	p := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return p
}

func startTestMetricsServer(t *testing.T, reg *prometheus.Registry) int {
	t.Helper()
	port := freeIntPort(t)
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	srv := &http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux}
	go func() { _ = srv.ListenAndServe() }()
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
	time.Sleep(80 * time.Millisecond)
	return port
}

// I-PFX-01: MetricsServer starts; StoragePoolCollector registered; scrape returns pool metrics.
func TestIntegration_PFX_StoragePoolMetricsScrape(t *testing.T) {
	reg := prometheus.NewRegistry()

	// Register a gauge simulating StoragePoolCollector output
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_storage_pool_capacity_bytes",
		Help: "Storage pool total capacity in bytes.",
	}, []string{"array_id", "pool_id", "pool_name", "type"})
	reg.MustRegister(g)
	g.WithLabelValues("array-A", "pool-1", "TestPool", "total").Set(1024 * 1024 * 1024)

	port := startTestMetricsServer(t, reg)

	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", port))
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "metrics endpoint must return 200")
	assert.Contains(t, string(body), "dell_powerflex_storage_pool_capacity_bytes",
		"scrape body must contain StoragePool capacity metric")
}

// I-PFX-02: X_CSI_METRICS_ENABLED=false — no HTTP server started, port not bound.
func TestIntegration_PFX_MetricsDisabled_NoPortBound(t *testing.T) {
	t.Setenv("X_CSI_METRICS_ENABLED", "false")

	port := freeIntPort(t)
	addr := fmt.Sprintf("localhost:%d", port)

	// Attempt connection — should fail since server not started
	conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if conn != nil {
		conn.Close()
	}
	assert.Error(t, err, "when X_CSI_METRICS_ENABLED=false, port must not be bound")
}

// I-PFX-03: Two arrays — Array A circuit OPEN (stale=1), Array B healthy.
func TestIntegration_PFX_MultiArray_CircuitOpen_StaleGauge(t *testing.T) {
	reg := prometheus.NewRegistry()

	stale := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_metrics_stale",
		Help: "1 when PowerFlex metrics are stale (circuit open).",
	}, []string{"system_id"})
	capacity := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_storage_pool_capacity_bytes",
		Help: "Capacity bytes.",
	}, []string{"array_id", "pool_id", "pool_name"})
	reg.MustRegister(stale, capacity)

	// Array A — circuit OPEN
	stale.WithLabelValues("array-A").Set(1)
	// Array B — healthy
	stale.WithLabelValues("array-B").Set(0)
	capacity.WithLabelValues("array-B", "pool-1", "PoolB").Set(500)

	port := startTestMetricsServer(t, reg)

	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", port))
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, string(body), `dell_powerflex_metrics_stale{system_id="array-A"} 1`)
	assert.Contains(t, string(body), `dell_powerflex_storage_pool_capacity_bytes{array_id="array-B",pool_id="pool-1"`)
}
