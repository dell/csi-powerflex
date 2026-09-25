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
	"time"

	"github.com/Ecosystems/container-storage-modules/src/csi-vxflexos/v2/service/collectors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockRCGClient struct {
	rcgs []collectors.RCGStats
	err  error
}

type rcgFailingRegisterer struct{ err error }

func (f rcgFailingRegisterer) Register(prometheus.Collector) error  { return f.err }
func (f rcgFailingRegisterer) MustRegister(...prometheus.Collector) {}
func (f rcgFailingRegisterer) Unregister(prometheus.Collector) bool { return false }

func (m *mockRCGClient) GetRCGStats(_ context.Context) ([]collectors.RCGStats, error) {
	return m.rcgs, m.err
}

// U-PFX-10: 1 RCG with statistics — all key metrics set
func TestRCGCollector_Collect_OneRCGWithStats(t *testing.T) {
	client := &mockRCGClient{
		rcgs: []collectors.RCGStats{
			{
				RCGID:                 "rcg-1",
				RCGName:               "RCG1",
				State:                 "Consistent",
				RPOSeconds:            30,
				LagReceivedMillis:     5000,
				LagAppliedMillis:      4000,
				LagPersistentMillis:   3000,
				TransmitBandwidthKBps: 100.5,
				ReceiveBandwidthKBps:  80.3,
				PairCount:             3,
			},
		},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewRCGCollector(client, reg, "system-1")
	require.NoError(t, err)

	err = c.Collect(context.Background())
	require.NoError(t, err)

	// Verify rcg_state
	mfState := gatherMetric(t, reg, "dell_powerflex_rcg_state")
	require.NotNil(t, mfState, "dell_powerflex_rcg_state must be emitted")
	v, ok := findGaugeValue(mfState, map[string]string{
		"system_id": "system-1", "rcg_id": "rcg-1", "rcg_name": "RCG1", "state": "Consistent",
	})
	require.True(t, ok)
	assert.Equal(t, 1.0, v)

	// Verify rpo_seconds
	mfRPO := gatherMetric(t, reg, "dell_powerflex_rcg_rpo_seconds")
	require.NotNil(t, mfRPO, "dell_powerflex_rcg_rpo_seconds must be emitted")
	rpo, ok := findGaugeValue(mfRPO, map[string]string{"system_id": "system-1", "rcg_id": "rcg-1"})
	require.True(t, ok)
	assert.Equal(t, float64(30), rpo)

	// Verify lag_received_seconds (millis / 1000)
	mfLag := gatherMetric(t, reg, "dell_powerflex_rcg_lag_received_seconds")
	require.NotNil(t, mfLag)
	lag, ok := findGaugeValue(mfLag, map[string]string{"system_id": "system-1", "rcg_id": "rcg-1"})
	require.True(t, ok)
	assert.InDelta(t, 5.0, lag, 0.001, "lag_received should be 5000ms / 1000 = 5.0s")

	// Verify transmit_bandwidth_kbps
	mfBW := gatherMetric(t, reg, "dell_powerflex_rcg_transmit_bandwidth_kbps")
	require.NotNil(t, mfBW)
	bw, ok := findGaugeValue(mfBW, map[string]string{"system_id": "system-1", "rcg_id": "rcg-1"})
	require.True(t, ok)
	assert.InDelta(t, 100.5, bw, 0.001)

	// Verify pair_total
	mfPairs := gatherMetric(t, reg, "dell_powerflex_replication_pair_total")
	require.NotNil(t, mfPairs)
	pairs, ok := findGaugeValue(mfPairs, map[string]string{"system_id": "system-1", "rcg_id": "rcg-1"})
	require.True(t, ok)
	assert.Equal(t, float64(3), pairs)
}

// U-PFX-11: No RCGs — no metrics emitted, no error
func TestRCGCollector_Collect_NoRCGs(t *testing.T) {
	client := &mockRCGClient{rcgs: []collectors.RCGStats{}}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewRCGCollector(client, reg, "system-1")
	require.NoError(t, err)

	err = c.Collect(context.Background())
	assert.NoError(t, err)

	mf := gatherMetric(t, reg, "dell_powerflex_rcg_state")
	if mf != nil {
		assert.Empty(t, mf.GetMetric(), "no RCG state metrics should be emitted for empty RCG list")
	}
}

// U-PFX-12: GetRCGStats returns error — wrapped error returned
func TestRCGCollector_Collect_APIError(t *testing.T) {
	client := &mockRCGClient{err: errors.New("RCG stats unavailable")}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewRCGCollector(client, reg, "system-1")
	require.NoError(t, err)

	err = c.Collect(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RCGCollector")
	assert.Contains(t, err.Error(), "RCG stats unavailable")
}

// U-PFX-13: RCG with extended metrics — lifetime_state, activity_state, latencies, remote_apply_bw
func TestRCGCollector_Collect_ExtendedMetrics(t *testing.T) {
	client := &mockRCGClient{
		rcgs: []collectors.RCGStats{
			{
				RCGID:                    "rcg-1",
				RCGName:                  "RCG1",
				State:                    "Consistent",
				LifetimeState:            "Normal",
				LocalActivityState:       "Active",
				RemoteActivityState:      "Inactive",
				RemoteApplyBandwidthKBps: 200.0,
				TransmitLatencySeconds:   0.005,
				ReceiveLatencySeconds:    0.003,
				ApplyLatencySeconds:      0.002,
			},
		},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewRCGCollector(client, reg, "system-1")
	require.NoError(t, err)

	require.NoError(t, c.Collect(context.Background()))

	// lifetime_state: non-empty → 1
	mfLifetime := gatherMetric(t, reg, "dell_powerflex_rcg_lifetime_state")
	require.NotNil(t, mfLifetime, "dell_powerflex_rcg_lifetime_state must be emitted")
	ltVal, ok := findGaugeValue(mfLifetime, map[string]string{"system_id": "system-1", "rcg_id": "rcg-1"})
	require.True(t, ok)
	assert.Equal(t, 1.0, ltVal, "non-empty LifetimeState → 1")

	// activity_state local=Active → 1, remote=Inactive → 0
	mfActivity := gatherMetric(t, reg, "dell_powerflex_rcg_activity_state")
	require.NotNil(t, mfActivity, "dell_powerflex_rcg_activity_state must be emitted")
	localActivity, ok := findGaugeValue(mfActivity, map[string]string{
		"system_id": "system-1", "rcg_id": "rcg-1", "direction": "local",
	})
	require.True(t, ok)
	assert.Equal(t, 1.0, localActivity, "LocalActivityState=Active → 1")

	remoteActivity, ok := findGaugeValue(mfActivity, map[string]string{
		"system_id": "system-1", "rcg_id": "rcg-1", "direction": "remote",
	})
	require.True(t, ok)
	assert.Equal(t, 0.0, remoteActivity, "RemoteActivityState=Inactive → 0")

	// remote_apply_bandwidth_kbps
	mfRemoteBW := gatherMetric(t, reg, "dell_powerflex_rcg_remote_apply_bandwidth_kbps")
	require.NotNil(t, mfRemoteBW, "dell_powerflex_rcg_remote_apply_bandwidth_kbps must be emitted")
	bw, ok := findGaugeValue(mfRemoteBW, map[string]string{"system_id": "system-1", "rcg_id": "rcg-1"})
	require.True(t, ok)
	assert.InDelta(t, 200.0, bw, 0.001)

	// transmit_latency_seconds
	mfTxLatency := gatherMetric(t, reg, "dell_powerflex_rcg_transmit_latency_seconds")
	require.NotNil(t, mfTxLatency)
	txLat, ok := findGaugeValue(mfTxLatency, map[string]string{"system_id": "system-1", "rcg_id": "rcg-1"})
	require.True(t, ok)
	assert.InDelta(t, 0.005, txLat, 0.0001)

	// apply_latency_seconds
	mfApplyLatency := gatherMetric(t, reg, "dell_powerflex_rcg_apply_latency_seconds")
	require.NotNil(t, mfApplyLatency)
	applyLat, ok := findGaugeValue(mfApplyLatency, map[string]string{"system_id": "system-1", "rcg_id": "rcg-1"})
	require.True(t, ok)
	assert.InDelta(t, 0.002, applyLat, 0.0001)
}

// U-PFX-14: RCG with pair progress — pair_initial_copy_progress emitted per pair
func TestRCGCollector_Collect_PairProgress(t *testing.T) {
	client := &mockRCGClient{
		rcgs: []collectors.RCGStats{
			{
				RCGID:   "rcg-1",
				RCGName: "RCG1",
				State:   "Consistent",
				PairProgress: []collectors.RCGPairProgress{
					{PairID: "pair-1", Progress: 0.75},
					{PairID: "pair-2", Progress: 1.0},
				},
			},
		},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewRCGCollector(client, reg, "system-1")
	require.NoError(t, err)

	require.NoError(t, c.Collect(context.Background()))

	mf := gatherMetric(t, reg, "dell_powerflex_replication_pair_initial_copy_progress")
	require.NotNil(t, mf, "pair_initial_copy_progress must be emitted")

	p1, ok := findGaugeValue(mf, map[string]string{
		"system_id": "system-1", "rcg_id": "rcg-1", "pair_id": "pair-1",
	})
	require.True(t, ok)
	assert.InDelta(t, 0.75, p1, 0.001, "pair-1 progress should be 0.75")

	p2, ok := findGaugeValue(mf, map[string]string{
		"system_id": "system-1", "rcg_id": "rcg-1", "pair_id": "pair-2",
	})
	require.True(t, ok)
	assert.InDelta(t, 1.0, p2, 0.001, "pair-2 progress should be 1.0 (complete)")
}

// U-PFX-15: RCG lifetime_state empty — encoded as 0
func TestRCGCollector_Collect_EmptyLifetimeState(t *testing.T) {
	client := &mockRCGClient{
		rcgs: []collectors.RCGStats{
			{RCGID: "rcg-1", RCGName: "RCG1", State: "Consistent", LifetimeState: ""},
		},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewRCGCollector(client, reg, "system-1")
	require.NoError(t, err)

	require.NoError(t, c.Collect(context.Background()))

	mf := gatherMetric(t, reg, "dell_powerflex_rcg_lifetime_state")
	require.NotNil(t, mf)
	v, ok := findGaugeValue(mf, map[string]string{"system_id": "system-1", "rcg_id": "rcg-1"})
	require.True(t, ok)
	assert.Equal(t, 0.0, v, "empty LifetimeState → 0")
}

// U-PFX-16: Verifies PF-19 fix — activity_state metric is preserved (not dropped to absent)
// when a subsequent API call fails.  Before the fix, Reset() was called before the API call,
// causing all metrics to disappear on error.  After the fix, Reset() is called only after a
// successful response, so callers see the last known values during transient gateway outages.
func TestRCGCollector_Collect_ActivityStatePreservedOnAPIError(t *testing.T) {
	// First call succeeds with an inactive local direction (activity_state{local}=0)
	client := &mockRCGClient{
		rcgs: []collectors.RCGStats{
			{
				RCGID:              "rcg-1",
				RCGName:            "RCG1",
				State:              "Consistent",
				LocalActivityState: "Inactive",
			},
		},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewRCGCollector(client, reg, "system-1")
	require.NoError(t, err)

	require.NoError(t, c.Collect(context.Background()))

	// Verify activity_state is 0 after the first (successful) poll
	mf := gatherMetric(t, reg, "dell_powerflex_rcg_activity_state")
	require.NotNil(t, mf)
	v, ok := findGaugeValue(mf, map[string]string{
		"system_id": "system-1", "rcg_id": "rcg-1", "direction": "local",
	})
	require.True(t, ok)
	assert.Equal(t, 0.0, v, "Inactive → 0 on first successful poll")

	// Second call fails (transient gateway error)
	client.rcgs = nil
	client.err = errors.New("gateway timeout")

	err = c.Collect(context.Background())
	require.Error(t, err, "expected error from API failure")

	// Metric must still be present with last known value (0), not absent
	mfAfter := gatherMetric(t, reg, "dell_powerflex_rcg_activity_state")
	require.NotNil(t, mfAfter, "activity_state metric must not disappear on API error")
	vAfter, ok := findGaugeValue(mfAfter, map[string]string{
		"system_id": "system-1", "rcg_id": "rcg-1", "direction": "local",
	})
	require.True(t, ok, "activity_state{local} must still be present after API error")
	assert.Equal(t, 0.0, vAfter, "activity_state must retain last known value (0) on API error")
}

func TestRCGCollector_ConstructorAndAccessors(t *testing.T) {
	reg := prometheus.NewRegistry()
	c, err := collectors.NewRCGCollector(&mockRCGClient{}, reg, "system-1")
	require.NoError(t, err)
	assert.Equal(t, "RCGCollector", c.Name())
	c.SetRuntime(nil)
	require.NoError(t, c.Collect(context.Background()))
	c.SetRuntime(collectors.NewMetricsRuntime("system-1", nil))
	require.NoError(t, c.Collect(context.Background()))

	_, err = collectors.NewRCGCollector(&mockRCGClient{}, &rcgFailingRegisterer{err: errors.New("register failed")}, "system-1")
	require.Error(t, err)
}

func TestRCGCollector_Constructor_FirstMetricError(t *testing.T) {
	// Test error path when first metric registration fails
	failReg := &rcgFailingRegisterer{err: errors.New("first metric failed")}
	_, err := collectors.NewRCGCollector(&mockRCGClient{}, failReg, "system-1")
	require.Error(t, err)
}

func TestRCGCollector_NewRCGCollector_EmptyRCGList(t *testing.T) {
	client := &mockRCGClient{
		rcgs: []collectors.RCGStats{},
	}
	reg := prometheus.NewRegistry()
	c, err := collectors.NewRCGCollector(client, reg, "system-1")
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, "RCGCollector", c.Name())
}

// TestRCGCollector_Collect_StaleGaugesResetAfterDeadline verifies that gauges are cleared
// when the staleness deadline passes after repeated API failures.
func TestRCGCollector_Collect_StaleGaugesResetAfterDeadline(t *testing.T) {
	client := &mockRCGClient{
		rcgs: []collectors.RCGStats{
			{
				RCGID:         "rcg-1",
				RCGName:       "RCG1",
				State:         "Consistent",
				FreezeState:   1, // frozen — would fire alert if kept stale
				FailoverState: 1, // failover active — would fire alert if kept stale
			},
		},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewRCGCollector(client, reg, "system-1")
	require.NoError(t, err)

	// First call succeeds: freeze_state and failover_state are populated.
	require.NoError(t, c.Collect(context.Background()))

	mf := gatherMetric(t, reg, "dell_powerflex_rcg_freeze_state")
	require.NotNil(t, mf, "freeze_state must be present after successful poll")

	// Set a very short stale duration so we can test expiry without sleeping.
	c.SetStaleDuration(1 * time.Nanosecond)

	// API now fails persistently.
	client.rcgs = nil
	client.err = errors.New("gateway unreachable")

	// Allow the staleness deadline to pass.
	time.Sleep(10 * time.Millisecond)

	// Next collect attempt should reset gauges (deadline exceeded).
	err = c.Collect(context.Background())
	require.Error(t, err, "collect must return API error")

	mfAfterExpiry := gatherMetric(t, reg, "dell_powerflex_rcg_freeze_state")
	// After reset, the metric family should be nil or contain no data points.
	if mfAfterExpiry != nil {
		assert.Empty(t, mfAfterExpiry.Metric, "freeze_state gauges must be cleared after stale deadline")
	}
}

// TestRCGCollector_Collect_StaleGaugesKeptWithinDeadline verifies that gauges are NOT
// cleared when failures are within the staleness deadline (transient outage tolerance).
func TestRCGCollector_Collect_StaleGaugesKeptWithinDeadline(t *testing.T) {
	client := &mockRCGClient{
		rcgs: []collectors.RCGStats{
			{
				RCGID:       "rcg-1",
				RCGName:     "RCG1",
				State:       "Consistent",
				FreezeState: 0,
			},
		},
	}

	reg := prometheus.NewRegistry()
	c, err := collectors.NewRCGCollector(client, reg, "system-1")
	require.NoError(t, err)

	// First call succeeds.
	require.NoError(t, c.Collect(context.Background()))

	// Set a long stale duration to simulate the within-deadline scenario.
	c.SetStaleDuration(10 * time.Minute)

	// API fails immediately after first success (within deadline).
	client.rcgs = nil
	client.err = errors.New("transient error")

	err = c.Collect(context.Background())
	require.Error(t, err, "collect must return API error")

	// Gauges must still be present (transient failure within deadline).
	mf := gatherMetric(t, reg, "dell_powerflex_rcg_freeze_state")
	require.NotNil(t, mf, "freeze_state must be preserved within stale deadline")
	_, ok := findGaugeValue(mf, map[string]string{
		"system_id": "system-1", "rcg_id": "rcg-1",
	})
	assert.True(t, ok, "freeze_state value must still be present within stale deadline")
}
