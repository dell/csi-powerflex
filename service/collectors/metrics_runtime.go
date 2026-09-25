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

	csmcache "github.com/Ecosystems/container-storage-modules/src/csm-metrics-common/pkg/cache"
	"github.com/Ecosystems/container-storage-modules/src/csm-metrics-common/pkg/middleware"
)

const (
	defaultMetricsTimeout        = 30 * time.Second
	defaultMetricsCacheTTL       = 25 * time.Second
	defaultMetricsRateLimit      = 100
	defaultMetricsCBThreshold    = 3
	defaultMetricsCBResetTimeout = 30 * time.Second
)

// RuntimeConfig controls metrics resilience behavior for a specific array.
type RuntimeConfig struct {
	Timeout        time.Duration
	CacheTTL       time.Duration
	RateLimit      int
	CBThreshold    int
	CBResetTimeout time.Duration
	StaleReporter  func(globalID string, stale bool)
}

// MetricsRuntime wraps PowerFlex metrics calls with timeout, rate limiting,
// circuit breaking, and response caching. It is scoped per systemID so that
// failures on one array do not affect others.
type MetricsRuntime struct {
	globalID       string
	timeout        time.Duration
	cache          *csmcache.ResponseCache
	rateLimiter    *middleware.RateLimiter
	circuitBreaker *middleware.CircuitBreaker
	staleReporter  func(globalID string, stale bool)
}

// NewMetricsRuntime creates a MetricsRuntime using the standard defaults.
func NewMetricsRuntime(globalID string, staleReporter func(globalID string, stale bool)) *MetricsRuntime {
	return NewMetricsRuntimeWithConfig(globalID, RuntimeConfig{StaleReporter: staleReporter})
}

// NewMetricsRuntimeWithConfig creates a MetricsRuntime using a caller-provided config.
// Zero values are replaced with the standard defaults to keep the runtime safe.
func NewMetricsRuntimeWithConfig(globalID string, cfg RuntimeConfig) *MetricsRuntime {
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultMetricsTimeout
	}
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = defaultMetricsCacheTTL
	}
	if cfg.RateLimit <= 0 {
		cfg.RateLimit = defaultMetricsRateLimit
	}
	if cfg.CBThreshold <= 0 {
		cfg.CBThreshold = defaultMetricsCBThreshold
	}
	if cfg.CBResetTimeout <= 0 {
		cfg.CBResetTimeout = defaultMetricsCBResetTimeout
	}

	return &MetricsRuntime{
		globalID:       globalID,
		timeout:        cfg.Timeout,
		cache:          csmcache.NewResponseCache(cfg.CacheTTL),
		rateLimiter:    middleware.NewRateLimiter(cfg.RateLimit),
		circuitBreaker: middleware.NewCircuitBreaker(globalID, cfg.CBThreshold, cfg.CBResetTimeout),
		staleReporter:  cfg.StaleReporter,
	}
}

// Do executes fn through rate limiting, timeout, and circuit breaking, then
// caches the result. On failure, a cached result is served when available and
// the stale indicator is set. On success the stale indicator is cleared.
func (r *MetricsRuntime) Do(ctx context.Context, endpoint, cacheKey string, fn func(context.Context) (any, error)) (any, error) {
	if r == nil {
		return nil, fmt.Errorf("metrics runtime is nil")
	}

	if err := r.rateLimiter.Wait(ctx, endpoint); err != nil {
		return r.serveStaleOrError(cacheKey, fmt.Errorf("rate limiter cancelled for %s/%s: %w", r.globalID, endpoint, err))
	}

	callCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	var result any
	cbErr := r.circuitBreaker.Call(func() error {
		value, err := fn(callCtx)
		if err != nil {
			return err
		}
		result = value
		return nil
	})
	if cbErr != nil {
		return r.serveStaleOrError(cacheKey, fmt.Errorf("%s/%s: %w", r.globalID, endpoint, cbErr))
	}

	r.cache.Set(cacheKey, result)
	r.setStale(false)
	return result, nil
}

// serveStaleOrError returns a cached result (marking metrics stale) when one
// is available, or propagates err when the cache is empty.
func (r *MetricsRuntime) serveStaleOrError(cacheKey string, err error) (any, error) {
	r.setStale(true)
	if cached, ok := r.cache.Get(cacheKey); ok {
		return cached, nil
	}
	return nil, err
}

func (r *MetricsRuntime) setStale(stale bool) {
	if r.staleReporter != nil {
		r.staleReporter(r.globalID, stale)
	}
}
