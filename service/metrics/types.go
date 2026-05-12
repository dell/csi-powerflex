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
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// GatewayClient is the interface for probing a PowerFlex gateway.
type GatewayClient interface {
	GetVersion() (string, error)
}

// Config holds gateway monitoring configuration.
type Config struct {
	PollInterval time.Duration
}

// SharedMetricsServer is a shared HTTP Prometheus metrics server.
type SharedMetricsServer struct {
	registry *prometheus.Registry
	mux      *http.ServeMux
	server   *http.Server
	mu       sync.Mutex
	addr     string
}

// gatewayMetrics holds all prometheus metric vectors for gateway monitoring.
type gatewayMetrics struct {
	up                           *prometheus.GaugeVec
	probeDuration                *prometheus.HistogramVec
	errors                       *prometheus.CounterVec
	lastSuccessfulProbeTimestamp *prometheus.GaugeVec
	consecutiveFailures          *prometheus.GaugeVec
	stateTransitions             *prometheus.CounterVec
}

// gatewayState tracks per-system gateway state for transition detection.
type gatewayState struct {
	current  bool
	previous bool
}

// GatewayEntry bundles a GatewayClient with its IP address for monitoring.
type GatewayEntry struct {
	Client GatewayClient
	IP     string
}

// GatewayMonitor polls gateways and updates prometheus metrics.
type GatewayMonitor struct {
	entries    map[string]GatewayEntry
	config     Config
	registry   *prometheus.Registry
	metrics    *gatewayMetrics
	states     map[string]gatewayState
	cancelFunc context.CancelFunc
	mu         sync.RWMutex
}

// credentialLabelKeys is the set of label key names that must not appear in metric labels.
var credentialLabelKeys = map[string]struct{}{
	"password":   {},
	"token":      {},
	"secret":     {},
	"credential": {},
	"username":   {},
}

// IsCredentialKey returns true if a label key name is a known credential pattern
// that must not appear in metric label sets.
func IsCredentialKey(key string) bool {
	_, found := credentialLabelKeys[key]
	return found
}
