// Copyright © 2025 Dell Inc. or its subsidiaries. All Rights Reserved.
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
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testCollector struct {
	name      string
	collectFn func(context.Context) error
	cleanupFn func()
}

func (c *testCollector) Collect(ctx context.Context) error {
	if c.collectFn != nil {
		return c.collectFn(ctx)
	}
	return nil
}

func (c *testCollector) Name() string { return c.name }

func (c *testCollector) Cleanup() {
	if c.cleanupFn != nil {
		c.cleanupFn()
	}
}

func TestCollectorManager_RegisterCollectorsAndCopy(t *testing.T) {
	m := NewCollectorManager()
	first := &testCollector{name: "first"}
	second := &testCollector{name: "second"}

	m.Register(first)
	m.Register(second)

	collectors := m.Collectors()
	require.Len(t, collectors, 2)
	assert.Same(t, first, collectors[0])
	assert.Same(t, second, collectors[1])

	collectors[0] = &testCollector{name: "mutated"}
	assert.Same(t, first, m.Collectors()[0], "Collectors should return a copy")
}

func TestCollectorManager_CollectAllAggregatesErrors(t *testing.T) {
	m := NewCollectorManager()
	m.Register(&testCollector{name: "ok"})
	m.Register(&testCollector{
		name: "bad",
		collectFn: func(context.Context) error {
			return errors.New("boom")
		},
	})

	err := m.CollectAll(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad: boom")
	assert.Contains(t, err.Error(), "CollectorManager errors")
}

func TestCollectorManager_StartAndStop(t *testing.T) {
	cleanupCalled := false
	m := NewCollectorManager()
	m.Register(&testCollector{
		name:      "cleanup",
		collectFn: func(context.Context) error { return nil },
		cleanupFn: func() { cleanupCalled = true },
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m.Start(ctx, time.Millisecond)
	require.NotNil(t, m.csmMgr)

	m.Stop()
	assert.True(t, cleanupCalled)
}

func TestCollectorAdapter_RegisterNoOp(t *testing.T) {
	adapter := &collectorAdapter{Collector: &testCollector{name: "adapter"}}
	reg := prometheus.NewRegistry()
	assert.NoError(t, adapter.Register(reg))
}

func TestRegisterOrGet(t *testing.T) {
	t.Run("returns registered collector", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		gauge := prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_register_or_get", Help: "help"})

		got, err := registerOrGet(reg, gauge)
		require.NoError(t, err)
		assert.Same(t, gauge, got)
	})

	t.Run("returns existing collector when already registered", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		gauge := prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_register_or_get_existing", Help: "help"})

		first, err := registerOrGet(reg, gauge)
		require.NoError(t, err)
		second, err := registerOrGet(reg, prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_register_or_get_existing", Help: "help"}))
		require.NoError(t, err)
		assert.Same(t, first, second)
	})

	t.Run("returns error for generic register failure", func(t *testing.T) {
		reg := failingRegisterer{err: fmt.Errorf("register failed")}
		gauge := prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_register_or_get_failure", Help: "help"})

		got, err := registerOrGet(reg, gauge)
		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "register failed")
	})
}

type failingRegisterer struct {
	err error
}

func (f failingRegisterer) Register(prometheus.Collector) error  { return f.err }
func (f failingRegisterer) MustRegister(...prometheus.Collector) {}
func (f failingRegisterer) Unregister(prometheus.Collector) bool { return false }
