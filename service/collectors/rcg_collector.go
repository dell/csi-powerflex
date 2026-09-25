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
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// defaultRCGStaleDuration is the maximum time stale RCG gauge values are kept alive
// after a series of consecutive API failures.  Once this deadline passes the gauges
// are cleared so that obsolete RCG states (e.g. a deleted RCG that has FreezeState=1)
// do not continue to fire critical alerts indefinitely.
//
// The value is ten times the default collection interval (30 s) = 5 minutes.  It is
// intentionally larger than the scrape interval to tolerate transient gateway outages
// without prematurely silencing in-flight alerts, while still ensuring that truly gone
// or inaccessible RCGs do not produce stale alert noise for hours.
const defaultRCGStaleDuration = 5 * time.Minute

// RCGPairProgress holds per-pair initial copy progress.
type RCGPairProgress struct {
	PairID   string
	Progress float64 // 0.0 to 1.0
}

// RCGStats holds Replication Consistency Group statistics.
type RCGStats struct {
	RCGID                    string
	RCGName                  string
	State                    string
	LifetimeState            string
	LocalActivityState       string
	RemoteActivityState      string
	RPOSeconds               int64
	FreezeState              int // 0=unfrozen, 1=frozen
	PauseMode                int // 0=not paused, 1=paused
	FailoverState            int // 0=none, 1=failover active
	LagReceivedMillis        int64
	LagAppliedMillis         int64
	LagPersistentMillis      int64
	TransmitBandwidthKBps    float64
	ReceiveBandwidthKBps     float64
	RemoteApplyBandwidthKBps float64
	TransmitLatencySeconds   float64
	ReceiveLatencySeconds    float64
	ApplyLatencySeconds      float64
	PairCount                int
	PairProgress             []RCGPairProgress
}

// RCGClient is the minimal interface for goscaleio RCG calls.
type RCGClient interface {
	GetRCGStats(ctx context.Context) ([]RCGStats, error)
}

// RCGCollector collects PowerFlex Replication Consistency Group metrics.
type RCGCollector struct {
	client   RCGClient
	runtime  *MetricsRuntime
	systemID string
	// lastSuccess records when the most recent successful GetRCGStats call completed.
	// After staleDuration has elapsed without a successful call the gauges are cleared
	// to avoid indefinitely alerting on data that may no longer reflect reality.
	lastSuccess        time.Time
	staleDuration      time.Duration
	rcgState           *prometheus.GaugeVec
	rcgRPO             *prometheus.GaugeVec
	rcgFreezeState     *prometheus.GaugeVec
	rcgPauseMode       *prometheus.GaugeVec
	rcgFailoverState   *prometheus.GaugeVec
	rcgLifetimeState   *prometheus.GaugeVec
	rcgActivityState   *prometheus.GaugeVec
	rcgLagReceived     *prometheus.GaugeVec
	rcgLagApplied      *prometheus.GaugeVec
	rcgLagPersistent   *prometheus.GaugeVec
	rcgTransmitBW      *prometheus.GaugeVec
	rcgReceiveBW       *prometheus.GaugeVec
	rcgRemoteApplyBW   *prometheus.GaugeVec
	rcgTransmitLatency *prometheus.GaugeVec
	rcgReceiveLatency  *prometheus.GaugeVec
	rcgApplyLatency    *prometheus.GaugeVec
	rcgPairTotal       *prometheus.GaugeVec
	rcgPairProgress    *prometheus.GaugeVec
}

// NewRCGCollector creates an RCGCollector and registers its Prometheus metrics.
func NewRCGCollector(client RCGClient, reg prometheus.Registerer, systemID string) (*RCGCollector, error) {
	rcgState, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_rcg_state",
		Help: "RCG state.",
	}, []string{"system_id", "rcg_id", "rcg_name", "state"}))
	if err != nil {
		return nil, err
	}
	rcgRPO, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_rcg_rpo_seconds",
		Help: "RCG RPO in seconds.",
	}, []string{"system_id", "rcg_id", "rcg_name"}))
	if err != nil {
		return nil, err
	}
	rcgFreezeState, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_rcg_freeze_state",
		Help: "RCG freeze state (0=unfrozen, 1=frozen).",
	}, []string{"system_id", "rcg_id", "rcg_name"}))
	if err != nil {
		return nil, err
	}
	rcgPauseMode, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_rcg_pause_mode",
		Help: "RCG pause mode (0=not paused, 1=paused).",
	}, []string{"system_id", "rcg_id", "rcg_name"}))
	if err != nil {
		return nil, err
	}
	rcgFailoverState, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_rcg_failover_state",
		Help: "RCG failover state (0=none, 1=failover active).",
	}, []string{"system_id", "rcg_id", "rcg_name"}))
	if err != nil {
		return nil, err
	}
	rcgLagReceived, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_rcg_lag_received_seconds",
		Help: "RCG received lag in seconds.",
	}, []string{"system_id", "rcg_id", "rcg_name"}))
	if err != nil {
		return nil, err
	}
	rcgLagApplied, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_rcg_lag_applied_seconds",
		Help: "RCG applied lag in seconds.",
	}, []string{"system_id", "rcg_id", "rcg_name"}))
	if err != nil {
		return nil, err
	}
	rcgLagPersistent, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_rcg_lag_persistent_seconds",
		Help: "RCG persistent lag in seconds.",
	}, []string{"system_id", "rcg_id", "rcg_name"}))
	if err != nil {
		return nil, err
	}
	rcgLifetimeState, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_rcg_lifetime_state",
		Help: "RCG lifetime state as numeric encoding.",
	}, []string{"system_id", "rcg_id", "rcg_name"}))
	if err != nil {
		return nil, err
	}
	rcgActivityState, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_rcg_activity_state",
		Help: "RCG activity state (1=active, 0=inactive) per direction.",
	}, []string{"system_id", "rcg_id", "rcg_name", "direction"}))
	if err != nil {
		return nil, err
	}
	rcgTransmitBW, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_rcg_transmit_bandwidth_kbps",
		Help: "RCG transmit bandwidth in KB/s.",
	}, []string{"system_id", "rcg_id", "rcg_name"}))
	if err != nil {
		return nil, err
	}
	rcgReceiveBW, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_rcg_receive_bandwidth_kbps",
		Help: "RCG receive bandwidth in KB/s.",
	}, []string{"system_id", "rcg_id", "rcg_name"}))
	if err != nil {
		return nil, err
	}
	rcgRemoteApplyBW, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_rcg_remote_apply_bandwidth_kbps",
		Help: "RCG remote apply bandwidth in KB/s.",
	}, []string{"system_id", "rcg_id", "rcg_name"}))
	if err != nil {
		return nil, err
	}
	rcgTransmitLatency, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_rcg_transmit_latency_seconds",
		Help: "RCG average transmit latency in seconds.",
	}, []string{"system_id", "rcg_id", "rcg_name"}))
	if err != nil {
		return nil, err
	}
	rcgReceiveLatency, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_rcg_receive_latency_seconds",
		Help: "RCG average receive latency in seconds.",
	}, []string{"system_id", "rcg_id", "rcg_name"}))
	if err != nil {
		return nil, err
	}
	rcgApplyLatency, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_rcg_apply_latency_seconds",
		Help: "RCG average apply latency in seconds.",
	}, []string{"system_id", "rcg_id", "rcg_name"}))
	if err != nil {
		return nil, err
	}
	rcgPairTotal, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_replication_pair_total",
		Help: "Total replication pairs per RCG.",
	}, []string{"system_id", "rcg_id"}))
	if err != nil {
		return nil, err
	}
	rcgPairProgress, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_replication_pair_initial_copy_progress",
		Help: "Replication pair initial copy progress (0.0 to 1.0).",
	}, []string{"system_id", "rcg_id", "pair_id"}))
	if err != nil {
		return nil, err
	}

	return &RCGCollector{
		client:             client,
		runtime:            nil,
		systemID:           systemID,
		staleDuration:      defaultRCGStaleDuration,
		rcgState:           rcgState,
		rcgRPO:             rcgRPO,
		rcgFreezeState:     rcgFreezeState,
		rcgPauseMode:       rcgPauseMode,
		rcgFailoverState:   rcgFailoverState,
		rcgLifetimeState:   rcgLifetimeState,
		rcgActivityState:   rcgActivityState,
		rcgLagReceived:     rcgLagReceived,
		rcgLagApplied:      rcgLagApplied,
		rcgLagPersistent:   rcgLagPersistent,
		rcgTransmitBW:      rcgTransmitBW,
		rcgReceiveBW:       rcgReceiveBW,
		rcgRemoteApplyBW:   rcgRemoteApplyBW,
		rcgTransmitLatency: rcgTransmitLatency,
		rcgReceiveLatency:  rcgReceiveLatency,
		rcgApplyLatency:    rcgApplyLatency,
		rcgPairTotal:       rcgPairTotal,
		rcgPairProgress:    rcgPairProgress,
	}, nil
}

// SetRuntime configures the resilience runtime used when collecting metrics.
func (c *RCGCollector) SetRuntime(runtime *MetricsRuntime) {
	c.runtime = runtime
}

// SetStaleDuration overrides the default staleness deadline (defaultRCGStaleDuration).
// Primarily used in tests to accelerate staleness expiry without sleeping.
func (c *RCGCollector) SetStaleDuration(d time.Duration) {
	c.staleDuration = d
}

// resetAllGauges clears every GaugeVec managed by this collector.
// Called when stale data has exceeded the staleDuration deadline so that
// obsolete RCG label sets do not continue to fire alerts indefinitely.
func (c *RCGCollector) resetAllGauges() {
	c.rcgState.Reset()
	c.rcgRPO.Reset()
	c.rcgFreezeState.Reset()
	c.rcgPauseMode.Reset()
	c.rcgFailoverState.Reset()
	c.rcgLifetimeState.Reset()
	c.rcgActivityState.Reset()
	c.rcgLagReceived.Reset()
	c.rcgLagApplied.Reset()
	c.rcgLagPersistent.Reset()
	c.rcgTransmitBW.Reset()
	c.rcgReceiveBW.Reset()
	c.rcgRemoteApplyBW.Reset()
	c.rcgTransmitLatency.Reset()
	c.rcgReceiveLatency.Reset()
	c.rcgApplyLatency.Reset()
	c.rcgPairTotal.Reset()
	c.rcgPairProgress.Reset()
}

// Collect fetches RCG metrics.
func (c *RCGCollector) Collect(ctx context.Context) error {
	var rcgs []RCGStats

	// Fetch stats first.  If the API call fails we return early without resetting
	// any metrics so that Prometheus continues to serve the last known values.
	// This is especially important for rcgActivityState: an absent metric would
	// make the PF-19 alert (activity_state == 0) unable to fire during transient
	// gateway outages.
	//
	// However, holding stale values indefinitely is also dangerous: a deleted RCG
	// with freeze_state=1 or failover_state=1 would continue alerting forever.
	// To bound this, if no successful API call has completed within staleDuration,
	// all gauges are reset so the stale data expires gracefully.
	if c.runtime == nil {
		var err error
		rcgs, err = c.client.GetRCGStats(ctx)
		if err != nil {
			if !c.lastSuccess.IsZero() && time.Since(c.lastSuccess) > c.staleDuration {
				c.resetAllGauges()
			}
			return fmt.Errorf("RCGCollector: failed to get RCG stats: %w", err)
		}
	} else {
		result, err := c.runtime.Do(ctx, "rcg-stats", c.systemID+":rcg-stats", func(callCtx context.Context) (any, error) {
			return c.client.GetRCGStats(callCtx)
		})
		if err != nil {
			if !c.lastSuccess.IsZero() && time.Since(c.lastSuccess) > c.staleDuration {
				c.resetAllGauges()
			}
			return fmt.Errorf("RCGCollector: failed to get RCG stats: %w", err)
		}
		var ok bool
		rcgs, ok = result.([]RCGStats)
		if !ok {
			return fmt.Errorf("RCGCollector: unexpected runtime result type %T", result)
		}
	}
	c.lastSuccess = time.Now()

	// Reset all gauges only after a successful API call so stale label sets
	// from deleted RCGs are removed while still preserving the last values
	// during transient failures (see comment above).
	c.rcgState.Reset()
	c.rcgRPO.Reset()
	c.rcgFreezeState.Reset()
	c.rcgPauseMode.Reset()
	c.rcgFailoverState.Reset()
	c.rcgLifetimeState.Reset()
	c.rcgActivityState.Reset()
	c.rcgLagReceived.Reset()
	c.rcgLagApplied.Reset()
	c.rcgLagPersistent.Reset()
	c.rcgTransmitBW.Reset()
	c.rcgReceiveBW.Reset()
	c.rcgRemoteApplyBW.Reset()
	c.rcgTransmitLatency.Reset()
	c.rcgReceiveLatency.Reset()
	c.rcgApplyLatency.Reset()
	c.rcgPairTotal.Reset()
	c.rcgPairProgress.Reset()

	for _, rcg := range rcgs {
		c.rcgState.WithLabelValues(c.systemID, rcg.RCGID, rcg.RCGName, rcg.State).Set(1)
		c.rcgRPO.WithLabelValues(c.systemID, rcg.RCGID, rcg.RCGName).Set(float64(rcg.RPOSeconds))
		c.rcgFreezeState.WithLabelValues(c.systemID, rcg.RCGID, rcg.RCGName).Set(float64(rcg.FreezeState))
		c.rcgPauseMode.WithLabelValues(c.systemID, rcg.RCGID, rcg.RCGName).Set(float64(rcg.PauseMode))
		c.rcgFailoverState.WithLabelValues(c.systemID, rcg.RCGID, rcg.RCGName).Set(float64(rcg.FailoverState))
		c.rcgLagReceived.WithLabelValues(c.systemID, rcg.RCGID, rcg.RCGName).Set(float64(rcg.LagReceivedMillis) / 1000.0)
		c.rcgLagApplied.WithLabelValues(c.systemID, rcg.RCGID, rcg.RCGName).Set(float64(rcg.LagAppliedMillis) / 1000.0)
		c.rcgLagPersistent.WithLabelValues(c.systemID, rcg.RCGID, rcg.RCGName).Set(float64(rcg.LagPersistentMillis) / 1000.0)
		c.rcgLifetimeState.WithLabelValues(c.systemID, rcg.RCGID, rcg.RCGName).Set(encodeState(rcg.LifetimeState))
		c.rcgActivityState.WithLabelValues(c.systemID, rcg.RCGID, rcg.RCGName, "local").Set(encodeActivityState(rcg.LocalActivityState))
		c.rcgActivityState.WithLabelValues(c.systemID, rcg.RCGID, rcg.RCGName, "remote").Set(encodeActivityState(rcg.RemoteActivityState))
		c.rcgTransmitBW.WithLabelValues(c.systemID, rcg.RCGID, rcg.RCGName).Set(rcg.TransmitBandwidthKBps)
		c.rcgReceiveBW.WithLabelValues(c.systemID, rcg.RCGID, rcg.RCGName).Set(rcg.ReceiveBandwidthKBps)
		c.rcgRemoteApplyBW.WithLabelValues(c.systemID, rcg.RCGID, rcg.RCGName).Set(rcg.RemoteApplyBandwidthKBps)
		c.rcgTransmitLatency.WithLabelValues(c.systemID, rcg.RCGID, rcg.RCGName).Set(rcg.TransmitLatencySeconds)
		c.rcgReceiveLatency.WithLabelValues(c.systemID, rcg.RCGID, rcg.RCGName).Set(rcg.ReceiveLatencySeconds)
		c.rcgApplyLatency.WithLabelValues(c.systemID, rcg.RCGID, rcg.RCGName).Set(rcg.ApplyLatencySeconds)
		c.rcgPairTotal.WithLabelValues(c.systemID, rcg.RCGID).Set(float64(rcg.PairCount))
		for _, pp := range rcg.PairProgress {
			c.rcgPairProgress.WithLabelValues(c.systemID, rcg.RCGID, pp.PairID).Set(pp.Progress)
		}
	}
	return nil
}

// encodeState maps a state string to a numeric value (1 if non-empty, 0 if empty).
func encodeState(state string) float64 {
	if state == "" {
		return 0
	}
	return 1
}

// encodeActivityState maps an activity state string to 1 (Active) or 0 (Inactive/other).
func encodeActivityState(state string) float64 {
	if state == "Active" {
		return 1
	}
	return 0
}

// Name returns the collector name.
func (c *RCGCollector) Name() string { return "RCGCollector" }
