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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsRuntime_DoCachesSuccessfulResult(t *testing.T) {
	var staleStates []bool
	runtime := NewMetricsRuntime("array-1", func(_ string, stale bool) {
		staleStates = append(staleStates, stale)
	})

	result, err := runtime.Do(context.Background(), "volumes", "array-1:volumes", func(context.Context) (any, error) {
		return []string{"one", "two"}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"one", "two"}, result)
	assert.Equal(t, []bool{false}, staleStates)

	cached, err := runtime.Do(context.Background(), "volumes", "array-1:volumes", func(context.Context) (any, error) {
		return nil, errors.New("forced failure")
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"one", "two"}, cached)
	assert.Equal(t, []bool{false, true}, staleStates)
}

func TestMetricsRuntime_DoReturnsErrorWithoutCache(t *testing.T) {
	var staleStates []bool
	runtime := NewMetricsRuntime("array-1", func(_ string, stale bool) {
		staleStates = append(staleStates, stale)
	})

	result, err := runtime.Do(context.Background(), "storage-pools", "array-1:storage-pools", func(context.Context) (any, error) {
		return nil, errors.New("boom")
	})
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Equal(t, []bool{true}, staleStates)
}

func TestMetricsRuntimeWithConfig_DefaultsAndCustomValues(t *testing.T) {
	custom := NewMetricsRuntimeWithConfig("array-1", RuntimeConfig{
		Timeout:        7 * time.Second,
		CacheTTL:       11 * time.Second,
		RateLimit:      3,
		CBThreshold:    4,
		CBResetTimeout: 5 * time.Second,
	})
	require.NotNil(t, custom)

	result, err := custom.Do(context.Background(), "volumes", "array-1:volumes:config", func(context.Context) (any, error) {
		return "ok", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", result)

	defaults := NewMetricsRuntimeWithConfig("array-1", RuntimeConfig{})
	require.NotNil(t, defaults)
	result, err = defaults.Do(context.Background(), "volumes", "array-1:volumes:defaults", func(context.Context) (any, error) {
		return "ok-default", nil
	})
	require.NoError(t, err)
	assert.Equal(t, "ok-default", result)
}

func TestMetricsRuntime_DoNilRuntime(t *testing.T) {
	var runtime *MetricsRuntime
	result, err := runtime.Do(context.Background(), "volumes", "array-1:volumes:nil", func(context.Context) (any, error) {
		return "never", nil
	})
	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "metrics runtime is nil")
}
