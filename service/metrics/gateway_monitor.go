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
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// NewGatewayMonitor creates a new GatewayMonitor with the given entries and config.
// If cfg.PollInterval is 0, it is defaulted to 30 seconds.
// If entries is nil, it is treated as an empty map.
func NewGatewayMonitor(entries map[string]GatewayEntry, config Config) *GatewayMonitor {
	if entries == nil {
		entries = make(map[string]GatewayEntry)
	}
	if config.PollInterval == 0 {
		config.PollInterval = 30 * time.Second
	}
	return &GatewayMonitor{
		entries:  entries,
		config:   config,
		registry: prometheus.NewRegistry(),
		metrics:  &gatewayMetrics{},
		states:   make(map[string]gatewayState),
	}
}

// registerOrGet registers the collector with the registry, or returns the existing
// collector if already registered (handles AlreadyRegisteredError gracefully).
func registerOrGet[T prometheus.Collector](r *prometheus.Registry, c T) (T, error) {
	if err := r.Register(c); err != nil {
		var are prometheus.AlreadyRegisteredError
		if errors.As(err, &are) {
			if existing, ok := are.ExistingCollector.(T); ok {
				return existing, nil
			}
		}
		return c, err
	}
	return c, nil
}

// registerMetrics creates and registers all six prometheus metric vectors on gm.registry.
func (gm *GatewayMonitor) registerMetrics() error {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	up := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "powerflex_gateway_up",
		Help: "Whether the PowerFlex gateway is up (1) or down (0).",
	}, []string{"system_id", "gateway_endpoint"})

	probeDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "powerflex_gateway_probe_duration_seconds",
		Help:    "Duration of gateway probe in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"system_id", "gateway_endpoint"})

	errs := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "powerflex_gateway_errors_total",
		Help: "Total number of gateway probe errors, partitioned by reason.",
	}, []string{"system_id", "gateway_endpoint", "reason"})

	lastSuccessfulProbeTimestamp := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "powerflex_gateway_last_successful_probe_timestamp_seconds",
		Help: "Unix timestamp of the last successful gateway probe.",
	}, []string{"system_id", "gateway_endpoint"})

	consecutiveFailures := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "powerflex_gateway_consecutive_failures",
		Help: "Number of consecutive failed gateway probes.",
	}, []string{"system_id", "gateway_endpoint"})

	stateTransitions := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "powerflex_gateway_state_transitions_total",
		Help: "Total number of gateway state transitions.",
	}, []string{"system_id", "gateway_endpoint", "from_state", "to_state"})

	var err error

	up, err = registerOrGet(gm.registry, up)
	if err != nil {
		return err
	}

	probeDuration, err = registerOrGet(gm.registry, probeDuration)
	if err != nil {
		return err
	}

	errs, err = registerOrGet(gm.registry, errs)
	if err != nil {
		return err
	}

	lastSuccessfulProbeTimestamp, err = registerOrGet(gm.registry, lastSuccessfulProbeTimestamp)
	if err != nil {
		return err
	}

	consecutiveFailures, err = registerOrGet(gm.registry, consecutiveFailures)
	if err != nil {
		return err
	}

	stateTransitions, err = registerOrGet(gm.registry, stateTransitions)
	if err != nil {
		return err
	}

	gm.metrics.up = up
	gm.metrics.probeDuration = probeDuration
	gm.metrics.errors = errs
	gm.metrics.lastSuccessfulProbeTimestamp = lastSuccessfulProbeTimestamp
	gm.metrics.consecutiveFailures = consecutiveFailures
	gm.metrics.stateTransitions = stateTransitions

	return nil
}

// probe performs a single gateway health check and updates metrics.
//
// GetVersion is used as the liveness probe because it is a lightweight,
// authenticated REST call that exercises the full request path (TCP, TLS,
// authentication, response parsing) without producing any side effects on
// the array. Any failure mode that would impact CSI operations -- network
// outage, expired auth token, gateway 5xx -- will surface as an error here,
// so checking err from GetVersion is a reliable signal of gateway health.
func (gm *GatewayMonitor) probe(_ context.Context, systemID, ip string, client GatewayClient) {
	start := time.Now()
	_, err := client.GetVersion()
	duration := time.Since(start).Seconds()

	gm.metrics.probeDuration.WithLabelValues(systemID, ip).Observe(duration)

	if err == nil {
		gm.metrics.up.WithLabelValues(systemID, ip).Set(1)
		gm.metrics.lastSuccessfulProbeTimestamp.WithLabelValues(systemID, ip).Set(float64(time.Now().Unix()))
		gm.metrics.consecutiveFailures.WithLabelValues(systemID, ip).Set(0)
		gm.updateState(systemID, ip, true)
	} else {
		gm.metrics.up.WithLabelValues(systemID, ip).Set(0)
		reason := categorizeError(err)
		gm.metrics.errors.WithLabelValues(systemID, ip, reason).Inc()
		gm.metrics.consecutiveFailures.WithLabelValues(systemID, ip).Inc()
		gm.updateState(systemID, ip, false)
	}
}

// pollGateway runs the polling loop for one gateway.
func (gm *GatewayMonitor) pollGateway(ctx context.Context, systemID, ip string, client GatewayClient) {
	ticker := time.NewTicker(gm.config.PollInterval)
	defer ticker.Stop()

	// poll immediately
	gm.probe(ctx, systemID, ip, client)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			gm.probe(ctx, systemID, ip, client)
		}
	}
}

// Start launches one polling goroutine per configured gateway.
func (gm *GatewayMonitor) Start(ctx context.Context) error {
	// If a SharedMetricsServer was created before this monitor, use its registry
	// so that the gateway metrics appear in the server's /metrics endpoint.
	latestServerMu.Lock()
	srv := latestServer
	latestServerMu.Unlock()
	if srv != nil {
		srv.mu.Lock()
		gm.registry = srv.registry
		srv.mu.Unlock()
	}

	if err := gm.registerMetrics(); err != nil {
		return err
	}

	subCtx, cancel := context.WithCancel(ctx)

	gm.mu.Lock()
	gm.cancelFunc = cancel
	gm.mu.Unlock()

	for systemID, entry := range gm.entries {
		go gm.pollGateway(subCtx, systemID, entry.IP, entry.Client)
	}

	return nil
}

// Stop cancels all polling goroutines.
func (gm *GatewayMonitor) Stop(_ context.Context) error {
	gm.mu.Lock()
	cancelFunc := gm.cancelFunc
	gm.mu.Unlock()

	if cancelFunc == nil {
		return nil
	}
	cancelFunc()
	return nil
}

// categorizeError maps an error to a stable reason label.
func categorizeError(err error) string {
	if err == nil {
		return ""
	}

	// Check for context.DeadlineExceeded via errors.Is
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}

	msg := strings.ToLower(err.Error())

	if strings.Contains(msg, "no such host") {
		return "dns_failure"
	}
	if strings.Contains(msg, "connection refused") {
		return "tcp_failure"
	}
	if strings.Contains(msg, "tls:") || strings.Contains(msg, "tls handshake") {
		return "tls_failure"
	}
	if strings.Contains(msg, "unauthorized") || strings.Contains(msg, "401") {
		return "auth_failure"
	}
	if strings.Contains(msg, "context deadline exceeded") {
		return "timeout"
	}

	return "unknown"
}

// updateState tracks gateway state transitions and updates metric counters.
func (gm *GatewayMonitor) updateState(systemID, ip string, isUp bool) {
	gm.mu.Lock()
	defer gm.mu.Unlock()

	current := gm.states[systemID]
	if current.current != isUp {
		fromState := "down"
		if current.current {
			fromState = "up"
		}
		toState := "down"
		if isUp {
			toState = "up"
		}
		gm.metrics.stateTransitions.WithLabelValues(systemID, ip, fromState, toState).Inc()
		gm.states[systemID] = gatewayState{current: isUp, previous: current.current}
	}
}

// validateLabels checks that the config labels contain no credentials.
// It strips credential keys (using IsCredentialKey) and returns a sanitized map.
func validateLabels(labels map[string]string) (map[string]string, error) {
	sanitized := make(map[string]string)
	for k, v := range labels {
		if !IsCredentialKey(strings.ToLower(k)) {
			sanitized[k] = v
		}
	}
	return sanitized, nil
}
