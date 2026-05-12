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

package metrics

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// mockGatewayClient — hand-written mock following csi-powerflex convention.
// ---------------------------------------------------------------------------

type mockGatewayClient struct {
	mu        sync.Mutex
	callCount int
	returnVer string
	returnErr error
	sequence  []struct {
		ver string
		err error
	}
	// blockCh: if set, GetVersion() blocks until channel is closed.
	blockCh chan struct{}
}

func (m *mockGatewayClient) GetVersion() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	if m.blockCh != nil {
		ch := m.blockCh
		m.mu.Unlock()
		<-ch // block until closed
		m.mu.Lock()
	}
	if len(m.sequence) > 0 {
		next := m.sequence[0]
		m.sequence = m.sequence[1:]
		return next.ver, next.err
	}
	return m.returnVer, m.returnErr
}

func (m *mockGatewayClient) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// newTestGatewayMonitor returns a fresh GatewayMonitor for test isolation.
// Does NOT call Start() or registerMetrics().
func newTestGatewayMonitor(t *testing.T, entries map[string]GatewayEntry, cfg Config) *GatewayMonitor {
	t.Helper()
	return NewGatewayMonitor(entries, cfg)
}

// ---------------------------------------------------------------------------
// U-009: TestNewGatewayMonitor_ReturnsNonNil
// NewGatewayMonitor() constructs monitor with provided client map and config.
// ---------------------------------------------------------------------------
func TestNewGatewayMonitor_ReturnsNonNil(t *testing.T) {
	mock := &mockGatewayClient{returnVer: "4.0.0"}
	entries := map[string]GatewayEntry{"sys1": {Client: mock, IP: "10.0.0.1"}}
	cfg := Config{
		PollInterval: 30 * time.Second,
	}

	gm := NewGatewayMonitor(entries, cfg)

	require.NotNil(t, gm, "NewGatewayMonitor must return non-nil")
	assert.Len(t, gm.entries, 1, "entries map must have 1 entry")
	assert.Nil(t, gm.cancelFunc, "cancelFunc must be nil before Start()")
}

// ---------------------------------------------------------------------------
// U-010: TestGatewayMonitor_RegisterMetrics_Success
// registerMetrics() registers all six prometheus metrics without error.
// ---------------------------------------------------------------------------
func TestGatewayMonitor_RegisterMetrics_Success(t *testing.T) {
	mock := &mockGatewayClient{returnVer: "4.0.0"}
	entries := map[string]GatewayEntry{"sys1": {Client: mock, IP: "10.0.0.1"}}
	cfg := Config{PollInterval: 30 * time.Second}

	gm := NewGatewayMonitor(entries, cfg)
	require.NotNil(t, gm)

	err := gm.registerMetrics()
	require.NoError(t, err, "registerMetrics() must return nil error")

	// Verify all six metric descriptors are non-nil
	require.NotNil(t, gm.metrics, "gm.metrics must be non-nil after registerMetrics()")
	assert.NotNil(t, gm.metrics.up, "_up GaugeVec must be non-nil")
	assert.NotNil(t, gm.metrics.probeDuration, "_probe_duration_seconds HistogramVec must be non-nil")
	assert.NotNil(t, gm.metrics.errors, "_errors_total CounterVec must be non-nil")
	assert.NotNil(t, gm.metrics.lastSuccessfulProbeTimestamp,
		"_last_successful_probe_timestamp_seconds GaugeVec must be non-nil")
	assert.NotNil(t, gm.metrics.consecutiveFailures, "_consecutive_failures GaugeVec must be non-nil")
	assert.NotNil(t, gm.metrics.stateTransitions, "_state_transitions_total CounterVec must be non-nil")
}

// ---------------------------------------------------------------------------
// U-011: TestGatewayMonitor_Probe_Success_UpdatesMetrics
// probe() on a healthy client sets gateway_up=1, records duration, resets
// consecutive_failures to 0, updates last_successful_probe_timestamp.
// ---------------------------------------------------------------------------
func TestGatewayMonitor_Probe_Success_UpdatesMetrics(t *testing.T) {
	mockClient := &mockGatewayClient{returnVer: "4.0.0"}
	entries := map[string]GatewayEntry{"sys1": {Client: mockClient, IP: "10.0.0.1"}}
	cfg := Config{PollInterval: 30 * time.Second}

	gm := NewGatewayMonitor(entries, cfg)
	require.NotNil(t, gm)
	require.NoError(t, gm.registerMetrics())

	ctx := context.Background()
	gm.probe(ctx, "sys1", "10.0.0.1", mockClient)

	// gateway_up should be 1
	upVal := testutil.ToFloat64(gm.metrics.up.WithLabelValues("sys1", "10.0.0.1"))
	assert.Equal(t, float64(1), upVal, "gateway_up must be 1 after successful probe")

	// consecutive_failures should be 0
	failVal := testutil.ToFloat64(gm.metrics.consecutiveFailures.WithLabelValues("sys1", "10.0.0.1"))
	assert.Equal(t, float64(0), failVal, "consecutive_failures must be 0 after success")

	// last_successful_probe_timestamp must be > 0
	tsVal := testutil.ToFloat64(gm.metrics.lastSuccessfulProbeTimestamp.WithLabelValues("sys1", "10.0.0.1"))
	assert.Greater(t, tsVal, float64(0), "last_successful_probe_timestamp must be > 0")

	// probe_duration_seconds histogram must have at least 1 observation
	histCount := testutil.CollectAndCount(gm.metrics.probeDuration)
	assert.Greater(t, histCount, 0, "probeDuration histogram must have at least one observation")
}

// ---------------------------------------------------------------------------
// U-012: TestGatewayMonitor_Probe_Failure_UpdatesMetrics
// probe() on a failing client sets gateway_up=0, increments errors_total,
// increments consecutive_failures.
// ---------------------------------------------------------------------------
func TestGatewayMonitor_Probe_Failure_UpdatesMetrics(t *testing.T) {
	mockClient := &mockGatewayClient{returnErr: errors.New("connection refused")}
	entries := map[string]GatewayEntry{"sys1": {Client: mockClient, IP: "10.0.0.1"}}
	cfg := Config{PollInterval: 30 * time.Second}

	gm := NewGatewayMonitor(entries, cfg)
	require.NotNil(t, gm)
	require.NoError(t, gm.registerMetrics())

	ctx := context.Background()
	gm.probe(ctx, "sys1", "10.0.0.1", mockClient)

	// gateway_up should be 0
	upVal := testutil.ToFloat64(gm.metrics.up.WithLabelValues("sys1", "10.0.0.1"))
	assert.Equal(t, float64(0), upVal, "gateway_up must be 0 after failed probe")

	// errors_total should be incremented by 1
	errVal := testutil.CollectAndCount(gm.metrics.errors)
	assert.Greater(t, errVal, 0, "errors_total must be incremented after failure")

	// consecutive_failures should be 1
	failVal := testutil.ToFloat64(gm.metrics.consecutiveFailures.WithLabelValues("sys1", "10.0.0.1"))
	assert.Equal(t, float64(1), failVal, "consecutive_failures must be 1 after first failure")
}

// ---------------------------------------------------------------------------
// U-013: TestGatewayMonitor_CategorizeError_DNSFailure
// categorizeError() maps DNS-related error strings to "dns_failure".
// ---------------------------------------------------------------------------
func TestGatewayMonitor_CategorizeError_DNSFailure(t *testing.T) {
	err := errors.New("no such host")
	result := categorizeError(err)
	assert.Equal(t, "dns_failure", result,
		"DNS error 'no such host' must map to 'dns_failure'")
}

// ---------------------------------------------------------------------------
// U-014: TestGatewayMonitor_CategorizeError_TCPFailure
// categorizeError() maps TCP connection-refused to "tcp_failure".
// ---------------------------------------------------------------------------
func TestGatewayMonitor_CategorizeError_TCPFailure(t *testing.T) {
	err := errors.New("connection refused")
	result := categorizeError(err)
	assert.Equal(t, "tcp_failure", result,
		"TCP error 'connection refused' must map to 'tcp_failure'")
}

// ---------------------------------------------------------------------------
// U-015: TestGatewayMonitor_CategorizeError_TLSFailure
// categorizeError() maps TLS handshake errors to "tls_failure".
// ---------------------------------------------------------------------------
func TestGatewayMonitor_CategorizeError_TLSFailure(t *testing.T) {
	err := errors.New("tls: handshake failure")
	result := categorizeError(err)
	assert.Equal(t, "tls_failure", result,
		"TLS error 'tls: handshake failure' must map to 'tls_failure'")
}

// ---------------------------------------------------------------------------
// U-016: TestGatewayMonitor_CategorizeError_AuthFailure
// categorizeError() maps unauthorized/forbidden HTTP responses to "auth_failure".
// ---------------------------------------------------------------------------
func TestGatewayMonitor_CategorizeError_AuthFailure(t *testing.T) {
	err := errors.New("401 unauthorized")
	result := categorizeError(err)
	assert.Equal(t, "auth_failure", result,
		"HTTP 401 unauthorized error must map to 'auth_failure'")
}

// ---------------------------------------------------------------------------
// U-017: TestGatewayMonitor_CategorizeError_Timeout
// categorizeError() maps context deadline exceeded to "timeout".
// ---------------------------------------------------------------------------
func TestGatewayMonitor_CategorizeError_Timeout(t *testing.T) {
	result := categorizeError(context.DeadlineExceeded)
	assert.Equal(t, "timeout", result,
		"context.DeadlineExceeded must map to 'timeout'")
}

// ---------------------------------------------------------------------------
// U-018: TestGatewayMonitor_CategorizeError_Unknown
// categorizeError() maps unrecognized errors to "unknown".
// ---------------------------------------------------------------------------
func TestGatewayMonitor_CategorizeError_Unknown(t *testing.T) {
	err := errors.New("some weird error that matches nothing")
	result := categorizeError(err)
	assert.Equal(t, "unknown", result,
		"unrecognized error must map to 'unknown'")
}

// ---------------------------------------------------------------------------
// U-019: TestGatewayMonitor_UpdateState_UpToDown_IncrementsTransitionCounter
// updateState() increments state_transitions_total when state changes Up→Down.
// ---------------------------------------------------------------------------
func TestGatewayMonitor_UpdateState_UpToDown_IncrementsTransitionCounter(t *testing.T) {
	mockClient := &mockGatewayClient{returnVer: "4.0.0"}
	entries := map[string]GatewayEntry{"sys1": {Client: mockClient, IP: "10.0.0.1"}}
	cfg := Config{PollInterval: 30 * time.Second}

	gm := NewGatewayMonitor(entries, cfg)
	require.NotNil(t, gm)
	require.NoError(t, gm.registerMetrics())

	// Establish initial Up state
	gm.updateState("sys1", "10.0.0.1", true)

	// Transition to Down
	gm.updateState("sys1", "10.0.0.1", false)

	// state_transitions_total{from_state="up",to_state="down"} must be 1
	transVal := testutil.ToFloat64(
		gm.metrics.stateTransitions.WithLabelValues("sys1", "10.0.0.1", "up", "down"),
	)
	assert.Equal(t, float64(1), transVal,
		"state_transitions_total{from=up,to=down} must be 1 after Up→Down transition")
}

// ---------------------------------------------------------------------------
// U-020: TestGatewayMonitor_UpdateState_DownToUp_IncrementsTransitionCounter
// updateState() increments transition counter for Down→Up.
// ---------------------------------------------------------------------------
func TestGatewayMonitor_UpdateState_DownToUp_IncrementsTransitionCounter(t *testing.T) {
	mockClient := &mockGatewayClient{returnVer: "4.0.0"}
	entries := map[string]GatewayEntry{"sys1": {Client: mockClient, IP: "10.0.0.1"}}
	cfg := Config{PollInterval: 30 * time.Second}

	gm := NewGatewayMonitor(entries, cfg)
	require.NotNil(t, gm)
	require.NoError(t, gm.registerMetrics())

	// Establish initial Down state
	gm.updateState("sys1", "10.0.0.1", false)

	// Transition to Up
	gm.updateState("sys1", "10.0.0.1", true)

	// state_transitions_total{from_state="down",to_state="up"} must be 1
	transVal := testutil.ToFloat64(
		gm.metrics.stateTransitions.WithLabelValues("sys1", "10.0.0.1", "down", "up"),
	)
	assert.Equal(t, float64(1), transVal,
		"state_transitions_total{from=down,to=up} must be 1 after Down→Up transition")
}

// ---------------------------------------------------------------------------
// U-021: TestGatewayMonitor_UpdateState_NoTransition_NoCounter
// updateState() does NOT increment transitions when state unchanged (Up→Up).
// ---------------------------------------------------------------------------
func TestGatewayMonitor_UpdateState_NoTransition_NoCounter(t *testing.T) {
	mockClient := &mockGatewayClient{returnVer: "4.0.0"}
	entries := map[string]GatewayEntry{"sys1": {Client: mockClient, IP: "10.0.0.1"}}
	cfg := Config{PollInterval: 30 * time.Second}

	gm := NewGatewayMonitor(entries, cfg)
	require.NotNil(t, gm)
	require.NoError(t, gm.registerMetrics())

	// Two consecutive Up states
	gm.updateState("sys1", "10.0.0.1", true)
	gm.updateState("sys1", "10.0.0.1", true)

	// state_transitions_total{from=up,to=up} should not exist or be 0
	transVal := testutil.ToFloat64(
		gm.metrics.stateTransitions.WithLabelValues("sys1", "10.0.0.1", "up", "up"),
	)
	assert.Equal(t, float64(0), transVal,
		"state_transitions_total must NOT increment when state does not change (Up→Up)")
}

// ---------------------------------------------------------------------------
// U-022: TestGatewayMonitor_Start_LaunchesPollingGoroutines
// Start() launches one goroutine per client entry; context cancellation stops all.
// ---------------------------------------------------------------------------
func TestGatewayMonitor_Start_LaunchesPollingGoroutines(t *testing.T) {
	// Use a very short poll interval so probes fire quickly
	mock1 := &mockGatewayClient{returnVer: "4.0.0"}
	mock2 := &mockGatewayClient{returnVer: "4.0.0"}
	entries := map[string]GatewayEntry{
		"sys1": {Client: mock1, IP: "10.0.0.1"},
		"sys2": {Client: mock2, IP: "10.0.0.2"},
	}
	cfg := Config{
		PollInterval: 20 * time.Millisecond,
	}

	gm := NewGatewayMonitor(entries, cfg)
	require.NotNil(t, gm)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := gm.Start(ctx)
	require.NoError(t, err, "Start() must return nil error")

	// Allow polling goroutines time to fire
	time.Sleep(100 * time.Millisecond)

	// Both mocks must have received at least one GetVersion() call
	assert.GreaterOrEqual(t, mock1.CallCount(), 1,
		"sys1 mock must receive at least 1 GetVersion() call")
	assert.GreaterOrEqual(t, mock2.CallCount(), 1,
		"sys2 mock must receive at least 1 GetVersion() call")

	// Cancel the context to stop polling
	cancel()

	// Wait for goroutines to exit
	time.Sleep(50 * time.Millisecond)

	// Record call counts after cancel
	count1After := mock1.CallCount()
	count2After := mock2.CallCount()

	// After a further pause, call counts must not increase
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, count1After, mock1.CallCount(),
		"no further calls to sys1 after context cancel")
	assert.Equal(t, count2After, mock2.CallCount(),
		"no further calls to sys2 after context cancel")
}

// ---------------------------------------------------------------------------
// U-023: TestGatewayMonitor_Stop_CancelsContext
// Stop() cancels the monitoring context; polling goroutines exit within deadline.
// ---------------------------------------------------------------------------
func TestGatewayMonitor_Stop_CancelsContext(t *testing.T) {
	mockClient := &mockGatewayClient{returnVer: "4.0.0"}
	entries := map[string]GatewayEntry{"sys1": {Client: mockClient, IP: "10.0.0.1"}}
	cfg := Config{
		PollInterval: 50 * time.Millisecond, // Longer interval to reduce race conditions
	}

	gm := NewGatewayMonitor(entries, cfg)
	require.NotNil(t, gm)

	ctx := context.Background()
	err := gm.Start(ctx)
	require.NoError(t, err)

	// Let at least one probe fire, but wait longer to ensure stable state
	time.Sleep(120 * time.Millisecond) // Wait > 2 intervals to ensure multiple probes

	// Record call count before Stop() to establish baseline
	countBeforeStop := mockClient.CallCount()
	require.GreaterOrEqual(t, countBeforeStop, 2, "Should have had at least 2 probes by now")

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()

	err = gm.Stop(stopCtx)
	assert.NoError(t, err, "Stop() must return nil error")

	// Wait a reasonable time for any in-flight operations to complete
	time.Sleep(200 * time.Millisecond)

	// Verify that no NEW calls have occurred since Stop()
	countAfterStop := mockClient.CallCount()
	assert.Equal(t, countBeforeStop, countAfterStop,
		"no new GetVersion() calls must occur after Stop() (baseline: %d, after: %d)",
		countBeforeStop, countAfterStop)

	// Additional verification: wait longer and ensure still no new calls
	time.Sleep(200 * time.Millisecond)
	countFinal := mockClient.CallCount()
	assert.Equal(t, countAfterStop, countFinal,
		"no GetVersion() calls must occur even after extended wait")
}

// ---------------------------------------------------------------------------
// U-024: TestGatewayMonitor_Stop_BeforeStart_IsNoOp
// Stop() before Start() is a safe no-op.
// ---------------------------------------------------------------------------
func TestGatewayMonitor_Stop_BeforeStart_IsNoOp(t *testing.T) {
	mockClient := &mockGatewayClient{returnVer: "4.0.0"}
	entries := map[string]GatewayEntry{"sys1": {Client: mockClient, IP: "10.0.0.1"}}
	cfg := Config{PollInterval: 30 * time.Second}

	gm := NewGatewayMonitor(entries, cfg)
	require.NotNil(t, gm)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	assert.NotPanics(t, func() {
		err := gm.Stop(ctx)
		assert.NoError(t, err, "Stop() before Start() must return nil error")
	}, "Stop() before Start() must not panic")
}

// ---------------------------------------------------------------------------
// U-025: TestGatewayMonitor_ContextCancellation_MidProbe_Exits
// When context is cancelled mid-probe, polling goroutine exits without panic.
// ---------------------------------------------------------------------------
func TestGatewayMonitor_ContextCancellation_MidProbe_Exits(t *testing.T) {
	// blockCh will keep GetVersion() blocking until we close it
	blockCh := make(chan struct{})
	mockClient := &mockGatewayClient{
		returnVer: "4.0.0",
		blockCh:   blockCh,
	}
	entries := map[string]GatewayEntry{"sys1": {Client: mockClient, IP: "10.0.0.1"}}
	cfg := Config{
		PollInterval: 10 * time.Millisecond,
	}

	gm := NewGatewayMonitor(entries, cfg)
	require.NotNil(t, gm)

	ctx, cancel := context.WithCancel(context.Background())

	err := gm.Start(ctx)
	require.NoError(t, err)

	// Give polling goroutine time to enter probe (and block on blockCh)
	time.Sleep(30 * time.Millisecond)

	// Cancel the parent context while probe is blocking
	cancel()

	// Unblock GetVersion() so goroutine can actually see ctx.Done()
	close(blockCh)

	// The goroutine should exit cleanly — test with a WaitGroup or timeout
	done := make(chan struct{})
	go func() {
		// Poll until goroutine exits by checking no further calls are made
		prev := mockClient.CallCount()
		for i := 0; i < 20; i++ {
			time.Sleep(20 * time.Millisecond)
			curr := mockClient.CallCount()
			if curr == prev {
				close(done)
				return
			}
			prev = curr
		}
		// If we get here, calls are still incrementing — test will timeout
	}()

	select {
	case <-done:
		// success: goroutine stopped
	case <-time.After(2 * time.Second):
		t.Error("polling goroutine did not exit within 2 seconds after context cancel")
	}
}

// ---------------------------------------------------------------------------
// U-026: TestMetricLabels_NoCredentials
// Metric label sets must never include credentials, tokens, or passwords.
// validateLabels() must reject or strip credential-containing keys.
// ---------------------------------------------------------------------------
func TestMetricLabels_NoCredentials(t *testing.T) {
	credentialLabels := map[string]string{
		"username":  "admin",
		"password":  "secret123",
		"token":     "bearer-abc",
		"system_id": "sys1",
	}

	// validateLabels must reject the label map or strip credential keys
	sanitized, err := validateLabels(credentialLabels)
	if err != nil {
		// Acceptable: return an error indicating credentials are not allowed
		assert.Error(t, err, "validateLabels must return error for credential-containing labels")
		return
	}

	// If it returns sanitized labels without error, credentials must be stripped
	require.NotNil(t, sanitized, "sanitized labels must not be nil")
	_, hasUsername := sanitized["username"]
	_, hasPassword := sanitized["password"]
	_, hasToken := sanitized["token"]

	assert.False(t, hasUsername, "sanitized labels must not contain 'username'")
	assert.False(t, hasPassword, "sanitized labels must not contain 'password'")
	assert.False(t, hasToken, "sanitized labels must not contain 'token'")

	// system_id is safe and must be preserved
	assert.Equal(t, "sys1", sanitized["system_id"],
		"safe labels like 'system_id' must be preserved")
}

// ---------------------------------------------------------------------------
// Additional edge-case: EC-2 (Zero poll interval)
// Config with zero PollInterval should be rejected or defaulted.
// ---------------------------------------------------------------------------
func TestNewGatewayMonitor_ZeroPollInterval_DefaultsOrErrors(t *testing.T) {
	mockClient := &mockGatewayClient{returnVer: "4.0.0"}
	entries := map[string]GatewayEntry{"sys1": {Client: mockClient, IP: "10.0.0.1"}}
	cfg := Config{
		PollInterval: 0, // zero — must not spin CPU at 100%
	}

	// Either NewGatewayMonitor panics (unimplemented) or when implemented,
	// must either default to 30s or return an error. For the stub, we just
	// ensure NewGatewayMonitor doesn't silently accept 0 without remedy.
	// Since stub panics, this test is expected to panic during TDD red phase.
	gm := NewGatewayMonitor(entries, cfg)
	require.NotNil(t, gm, "NewGatewayMonitor must return non-nil")
	// If PollInterval is 0, implementation must default or the Start() must error
	if gm.config.PollInterval == 0 {
		// This is the problematic state — start should return error or default
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := gm.Start(ctx)
		// Either Start fails with an error, OR config was defaulted to 30s
		if err == nil {
			assert.Equal(t, 30*time.Second, gm.config.PollInterval,
				"zero PollInterval must be defaulted to 30s when Start() does not error")
		}
	}
}

// ---------------------------------------------------------------------------
// EC-1: Nil clients map edge case
// ---------------------------------------------------------------------------
func TestNewGatewayMonitor_NilClients_IsNoopOnStart(t *testing.T) {
	cfg := Config{PollInterval: 30 * time.Second}

	// Nil entries map — should not panic
	gm := NewGatewayMonitor(nil, cfg)
	require.NotNil(t, gm, "NewGatewayMonitor(nil, cfg) must return non-nil")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start with nil clients must be a no-op (zero goroutines launched)
	err := gm.Start(ctx)
	assert.NoError(t, err, "Start() with nil clients must not error")
}

// ---------------------------------------------------------------------------
// EC-9: Consecutive failures counter resets to 0 on recovery
// ---------------------------------------------------------------------------
func TestGatewayMonitor_ConsecutiveFailures_ResetsOnRecovery(t *testing.T) {
	// Fail twice, then succeed
	mockClient := &mockGatewayClient{
		sequence: []struct {
			ver string
			err error
		}{
			{ver: "", err: errors.New("connection refused")},
			{ver: "", err: errors.New("connection refused")},
			{ver: "4.0.0", err: nil},
		},
	}
	entries := map[string]GatewayEntry{"sys1": {Client: mockClient, IP: "10.0.0.1"}}
	cfg := Config{PollInterval: 30 * time.Second}

	gm := NewGatewayMonitor(entries, cfg)
	require.NotNil(t, gm)
	require.NoError(t, gm.registerMetrics())

	ctx := context.Background()

	// Two failures
	gm.probe(ctx, "sys1", "10.0.0.1", mockClient)
	gm.probe(ctx, "sys1", "10.0.0.1", mockClient)

	failVal := testutil.ToFloat64(gm.metrics.consecutiveFailures.WithLabelValues("sys1", "10.0.0.1"))
	assert.Equal(t, float64(2), failVal, "consecutive_failures must be 2 after two failures")

	// Recovery
	gm.probe(ctx, "sys1", "10.0.0.1", mockClient)

	failValAfter := testutil.ToFloat64(gm.metrics.consecutiveFailures.WithLabelValues("sys1", "10.0.0.1"))
	assert.Equal(t, float64(0), failValAfter,
		"consecutive_failures must reset to 0 after successful probe (not decrement)")
}

// ---------------------------------------------------------------------------
// Verify prometheus testutil package is importable (compilation guard)
// ---------------------------------------------------------------------------
var (
	_ = testutil.ToFloat64
	_ = prometheus.NewRegistry
)
