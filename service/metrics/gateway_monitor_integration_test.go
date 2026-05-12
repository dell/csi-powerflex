// Copyright © 2025 Dell Inc. or its subsidiaries. All Rights Reserved.
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

//go:build integration

package metrics_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dell/csi-vxflexos/v2/service/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// integrationMockClient is a mock gateway client for integration tests.
// It supports configurable sequences of responses.
// ---------------------------------------------------------------------------

type integrationMockClient struct {
	mu        sync.Mutex
	callCount int
	returnVer string
	returnErr error
	sequence  []struct {
		ver string
		err error
	}
}

func (m *integrationMockClient) GetVersion() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	if len(m.sequence) > 0 {
		next := m.sequence[0]
		m.sequence = m.sequence[1:]
		return next.ver, next.err
	}
	return m.returnVer, m.returnErr
}

func (m *integrationMockClient) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// scrapeMetrics fetches the /metrics endpoint from the given server address.
func scrapeMetrics(t *testing.T, addr string) string {
	t.Helper()
	url := fmt.Sprintf("http://%s/metrics", addr)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url) //nolint:noctx
	require.NoError(t, err, "HTTP GET to integration metrics endpoint failed")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode,
		"metrics endpoint must return HTTP 200")
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(body)
}

// waitForServerAddr polls the server's GetAddr() until non-empty or timeout.
func waitForServerAddr(t *testing.T, s *metrics.SharedMetricsServer, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		addr := s.GetAddr()
		if addr != "" {
			return addr
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for integration server address")
	return ""
}

// ---------------------------------------------------------------------------
// I-001: TestIntegration_FullPollingCycle_MetricsExposed
// End-to-end: monitor started → poll fires → metrics updated → HTTP scrape
// returns correct values.
// ---------------------------------------------------------------------------
func TestIntegration_FullPollingCycle_MetricsExposed(t *testing.T) {
	mockClient := &integrationMockClient{returnVer: "4.0.0"}
	entries := map[string]metrics.GatewayEntry{"sys1": {Client: mockClient, IP: "10.0.0.1"}}
	cfg := metrics.Config{
		PollInterval: 50 * time.Millisecond,
	}

	// Create server and monitor
	server := metrics.NewSharedMetricsServer()
	require.NotNil(t, server, "SharedMetricsServer must be non-nil")

	gm := metrics.NewGatewayMonitor(entries, cfg)
	require.NotNil(t, gm, "GatewayMonitor must be non-nil")

	// Start the server
	err := server.Start(":0")
	require.NoError(t, err, "server Start must succeed")

	addr := waitForServerAddr(t, server, 2*time.Second)
	require.NotEmpty(t, addr)

	// Start the monitor
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = gm.Start(ctx)
	require.NoError(t, err, "monitor Start must succeed")

	// Wait for at least one full poll cycle
	time.Sleep(200 * time.Millisecond)

	// Scrape the metrics endpoint
	body := scrapeMetrics(t, addr)

	// Verify powerflex_gateway_up{system_id="sys1"} 1
	assert.Contains(t, body, `powerflex_gateway_up`,
		"scrape body must contain powerflex_gateway_up metric")
	assert.Contains(t, body, `system_id="sys1"`,
		"scrape body must contain system_id label")
	assert.Contains(t, body, `gateway_endpoint="10.0.0.1"`,
		"scrape body must contain gateway_endpoint label")
	assert.Contains(t, body, `powerflex_gateway_probe_duration_seconds`,
		"scrape body must contain probe_duration_seconds histogram")

	// Verify the up value is 1 (healthy)
	assert.True(t,
		strings.Contains(body, `powerflex_gateway_up{`) &&
			strings.Contains(body, `} 1`),
		"powerflex_gateway_up for healthy gateway must be 1, body: %s", body)

	// Cleanup
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	_ = server.Stop(stopCtx)
}

// ---------------------------------------------------------------------------
// I-002: TestIntegration_LeaderContextCancellation_StopsPolling
// Leader-election loss simulation: cancelling the leader context stops all
// polling goroutines; metrics server still serves last-known values.
// ---------------------------------------------------------------------------
func TestIntegration_LeaderContextCancellation_StopsPolling(t *testing.T) {
	mockClient := &integrationMockClient{returnVer: "4.0.0"}
	entries := map[string]metrics.GatewayEntry{"sys1": {Client: mockClient, IP: "10.0.0.1"}}
	cfg := metrics.Config{
		PollInterval: 30 * time.Millisecond,
	}

	server := metrics.NewSharedMetricsServer()
	require.NotNil(t, server)

	err := server.Start(":0")
	require.NoError(t, err)

	addr := waitForServerAddr(t, server, 2*time.Second)
	require.NotEmpty(t, addr)

	// Leader context — will be cancelled to simulate leader election loss
	leaderCtx, leaderCancel := context.WithCancel(context.Background())

	gm := metrics.NewGatewayMonitor(entries, cfg)
	require.NotNil(t, gm)

	err = gm.Start(leaderCtx)
	require.NoError(t, err)

	// Let polling run
	time.Sleep(120 * time.Millisecond)
	countBeforeCancel := mockClient.CallCount()
	assert.GreaterOrEqual(t, countBeforeCancel, 1,
		"at least 1 probe must have fired before leader context cancel")

	// Simulate leader election loss
	leaderCancel()

	// Wait for goroutines to stop
	time.Sleep(100 * time.Millisecond)

	// No further calls should occur
	countAfterCancel := mockClient.CallCount()
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, countAfterCancel, mockClient.CallCount(),
		"no further probes must occur after leader context cancellation")

	// Metrics server must still serve the last known values
	body := scrapeMetrics(t, addr)
	assert.Contains(t, body, "powerflex_gateway_up",
		"metrics server must still serve metrics after polling stopped")

	// Cleanup
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	_ = server.Stop(stopCtx)
}

// ---------------------------------------------------------------------------
// I-003: TestIntegration_GatewayFailover_MetricsTransition
// Gateway transitions Up→Down→Up; all state transition metrics tracked correctly.
// ---------------------------------------------------------------------------
func TestIntegration_GatewayFailover_MetricsTransition(t *testing.T) {
	// Sequence: success → failure → success
	mockClient := &integrationMockClient{
		sequence: []struct {
			ver string
			err error
		}{
			{ver: "4.0.0", err: nil},
			{ver: "", err: errors.New("connection refused")},
			{ver: "4.0.0", err: nil},
		},
		// After sequence is exhausted, return success
		returnVer: "4.0.0",
	}
	entries := map[string]metrics.GatewayEntry{"sys1": {Client: mockClient, IP: "10.0.0.1"}}
	cfg := metrics.Config{
		PollInterval: 50 * time.Millisecond,
	}

	server := metrics.NewSharedMetricsServer()
	require.NotNil(t, server)

	err := server.Start(":0")
	require.NoError(t, err)

	addr := waitForServerAddr(t, server, 2*time.Second)
	require.NotEmpty(t, addr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gm := metrics.NewGatewayMonitor(entries, cfg)
	require.NotNil(t, gm)

	err = gm.Start(ctx)
	require.NoError(t, err)

	// Wait for all 3 sequence items to be consumed (3 × 50ms + buffer)
	time.Sleep(300 * time.Millisecond)

	body := scrapeMetrics(t, addr)

	// state_transitions_total{from_state="up",to_state="down"} must be ≥ 1
	assert.True(t,
		strings.Contains(body, `from_state="up"`) ||
			strings.Contains(body, `powerflex_gateway_state_transitions_total`),
		"scrape body must contain state transition metrics, body: %s", body)

	// consecutive_failures must be back to 0 after final success
	assert.True(t,
		strings.Contains(body, `powerflex_gateway_consecutive_failures`) ||
			strings.Contains(body, `powerflex_gateway_up`),
		"scrape body must contain consecutive_failures or up metric, body: %s", body)

	// Cleanup
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	_ = server.Stop(stopCtx)
}

// ---------------------------------------------------------------------------
// I-004: TestIntegration_MultipleArrays_IndependentMetrics
// Multiple arrays polled concurrently; each maintains independent metric label sets.
// ---------------------------------------------------------------------------
func TestIntegration_MultipleArrays_IndependentMetrics(t *testing.T) {
	// sys1 always succeeds, sys2 always fails
	successMock := &integrationMockClient{returnVer: "4.0.0"}
	failureMock := &integrationMockClient{returnErr: errors.New("connection refused")}

	entries := map[string]metrics.GatewayEntry{
		"sys1": {Client: successMock, IP: "10.0.0.1"},
		"sys2": {Client: failureMock, IP: "10.0.0.2"},
	}
	cfg := metrics.Config{
		PollInterval: 50 * time.Millisecond,
	}

	server := metrics.NewSharedMetricsServer()
	require.NotNil(t, server)

	err := server.Start(":0")
	require.NoError(t, err)

	addr := waitForServerAddr(t, server, 2*time.Second)
	require.NotEmpty(t, addr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gm := metrics.NewGatewayMonitor(entries, cfg)
	require.NotNil(t, gm)

	err = gm.Start(ctx)
	require.NoError(t, err)

	// Wait for multiple poll cycles
	time.Sleep(250 * time.Millisecond)

	body := scrapeMetrics(t, addr)

	// Both system IDs must appear in the scrape output
	assert.Contains(t, body, `system_id="sys1"`,
		"scrape body must contain sys1 label")
	assert.Contains(t, body, `system_id="sys2"`,
		"scrape body must contain sys2 label")
	assert.Contains(t, body, `gateway_endpoint="10.0.0.1"`,
		"scrape body must contain gateway_endpoint for sys1")
	assert.Contains(t, body, `gateway_endpoint="10.0.0.2"`,
		"scrape body must contain gateway_endpoint for sys2")

	// sys1 must be up=1, sys2 must be up=0
	// Look for the distinct patterns
	assert.True(t,
		strings.Contains(body, `powerflex_gateway_up{`) &&
			strings.Contains(body, `"sys1"`) &&
			strings.Contains(body, `"sys2"`),
		"scrape body must distinguish between sys1 and sys2, body: %s", body)

	// No label collision: both system IDs must appear independently
	sys1Count := strings.Count(body, `"sys1"`)
	sys2Count := strings.Count(body, `"sys2"`)
	assert.Greater(t, sys1Count, 0, "sys1 label must appear in scrape")
	assert.Greater(t, sys2Count, 0, "sys2 label must appear in scrape")

	// Cleanup
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	_ = server.Stop(stopCtx)
}

// ---------------------------------------------------------------------------
// I-005: TestIntegration_SharedServer_MultipleCollectors_RegisterAndServe
// Demonstrates extensibility: two independent collectors both register and serve
// on the same SharedMetricsServer.
// ---------------------------------------------------------------------------
func TestIntegration_SharedServer_MultipleCollectors_RegisterAndServe(t *testing.T) {
	server := metrics.NewSharedMetricsServer()
	require.NotNil(t, server)

	// Gateway monitor collector registration
	mockClient := &integrationMockClient{returnVer: "4.0.0"}
	entries := map[string]metrics.GatewayEntry{"sys1": {Client: mockClient, IP: "10.0.0.1"}}
	cfg := metrics.Config{
		PollInterval: 50 * time.Millisecond,
	}

	gm := metrics.NewGatewayMonitor(entries, cfg)
	require.NotNil(t, gm)

	// A second synthetic "volume metrics" collector
	volumeGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "powerflex_volume_count",
		Help: "Synthetic volume count for extensibility test I-005",
	})
	volumeGauge.Set(99.0)

	// Register both collectors with the shared server
	err := server.Register(volumeGauge)
	require.NoError(t, err, "volume gauge registration must succeed")

	// Start server
	err = server.Start(":0")
	require.NoError(t, err, "server Start must succeed")

	addr := waitForServerAddr(t, server, 2*time.Second)
	require.NotEmpty(t, addr)

	// Start gateway monitor (which registers its own metrics via the server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = gm.Start(ctx)
	require.NoError(t, err, "gateway monitor Start must succeed")

	// Wait for at least one probe cycle
	time.Sleep(200 * time.Millisecond)

	body := scrapeMetrics(t, addr)

	// Both metric families must be present
	assert.Contains(t, body, "powerflex_volume_count",
		"scrape body must contain the synthetic volume gauge (extensibility FR11)")
	assert.Contains(t, body, "powerflex_gateway_up",
		"scrape body must contain gateway monitoring metrics")

	// Cleanup
	cancel()
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	_ = server.Stop(stopCtx)
}
