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

package collectors

import (
	"context"
	"fmt"
	"time"

	"github.com/Ecosystems/container-storage-modules/src/csm-metrics-common/pkg/collector"
	"github.com/prometheus/client_golang/prometheus"
)

// Collector is the lightweight collector interface used by driver-local collectors.
// It intentionally omits Register so constructors can register metrics atomically.
type Collector interface {
	Collect(ctx context.Context) error
	Name() string
}

// collectorAdapter bridges a Collector to csmcollector.MetricsCollector.
type collectorAdapter struct {
	Collector
}

func (a *collectorAdapter) Register(_ prometheus.Registerer) error { return nil }

// Ensure collectorAdapter satisfies the csm-metrics-common collector contract.
var _ collector.MetricsCollector = (*collectorAdapter)(nil)

// cleanupCollector is implemented by collectors that need cleanup during Stop().
type cleanupCollector interface {
	Cleanup()
}

// CollectorManager manages drivers' collectors using csm-metrics-common's manager.
type CollectorManager struct {
	collectors []Collector
	csmMgr     *collector.Manager
}

// NewCollectorManager creates an empty CollectorManager.
func NewCollectorManager() *CollectorManager {
	return &CollectorManager{}
}

// Register adds a Collector.
func (m *CollectorManager) Register(c Collector) {
	m.collectors = append(m.collectors, c)
}

// Collectors returns registered collectors.
func (m *CollectorManager) Collectors() []Collector {
	out := make([]Collector, len(m.collectors))
	copy(out, m.collectors)
	return out
}

// CollectAll runs Collect on every collector and aggregates errors.
func (m *CollectorManager) CollectAll(ctx context.Context) error {
	var errs []string
	for _, c := range m.collectors {
		if err := c.Collect(ctx); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", c.Name(), err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("CollectorManager errors: %v", errs)
	}
	return nil
}

// Start spawns collectors via csmcollector.Manager (per-goroutine runner).
func (m *CollectorManager) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}

	adapters := make([]collector.MetricsCollector, len(m.collectors))
	for i, c := range m.collectors {
		adapters[i] = &collectorAdapter{c}
	}
	m.csmMgr = collector.NewManager(adapters, interval)
	m.csmMgr.Start(ctx)
}

// Stop stops the manager and calls Cleanup on collectors that implement it.
func (m *CollectorManager) Stop() {
	if m.csmMgr != nil {
		m.csmMgr.Stop()
	}

	for _, c := range m.collectors {
		if cleaner, ok := c.(cleanupCollector); ok {
			cleaner.Cleanup()
		}
	}
}
