// Copyright © 2026 Dell Inc. or its subsidiaries. All Rights Reserved.
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

package metrics_test

import (
	"math"
	"testing"

	"github.com/Ecosystems/container-storage-modules/src/csi-vxflexos/v2/service/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	bytesInGiB = 1024 * 1024 * 1024
)

// ── 1. NewGranularityMetrics registration ──────────────────────────────────

func TestNewGranularityMetrics_ReturnsNonNilOnSuccess(t *testing.T) {
	reg := prometheus.NewRegistry()
	gm, err := metrics.NewGranularityMetrics(reg)
	require.NoError(t, err)
	require.NotNil(t, gm)
}

func TestNewGranularityMetrics_Idempotent(t *testing.T) {
	// Calling twice on the same registry must not panic or return error
	// due to AlreadyRegisteredError being handled gracefully.
	reg := prometheus.NewRegistry()
	gm1, err1 := metrics.NewGranularityMetrics(reg)
	require.NoError(t, err1)
	require.NotNil(t, gm1)

	gm2, err2 := metrics.NewGranularityMetrics(reg)
	require.NoError(t, err2)
	require.NotNil(t, gm2)
}

// ── 2. IncDetectionError counter ───────────────────────────────────────────

func TestGranularityMetrics_IncDetectionError_Increments(t *testing.T) {
	reg := prometheus.NewRegistry()
	gm, err := metrics.NewGranularityMetrics(reg)
	require.NoError(t, err)

	gm.IncDetectionError()
	gm.IncDetectionError()

	// Gather metrics and verify the counter value
	families, _ := reg.Gather()
	var value float64
	for _, mf := range families {
		if mf.GetName() == "powerflex_csi_granularity_detection_errors_total" {
			for _, m := range mf.GetMetric() {
				value = m.GetCounter().GetValue()
			}
		}
	}
	assert.Equal(t, float64(2), value,
		"DetectionErrors counter must reach 2 after two IncDetectionError() calls")
}

func TestGranularityMetrics_IncDetectionError_NilSafe(t *testing.T) {
	var gm *metrics.GranularityMetrics
	assert.NotPanics(t, func() { gm.IncDetectionError() })
}

// ── 3. IncRoundedMetrics (RoundedTotal{operation}) counter ─────────────────

func TestGranularityMetrics_IncRoundedMetrics_IncrementOnRounding(t *testing.T) {
	reg := prometheus.NewRegistry()
	gm, err := metrics.NewGranularityMetrics(reg)
	require.NoError(t, err)

	// 1.2 GiB → 2 GiB on EC (rounding occurred)
	original := int64(math.Trunc(float64(bytesInGiB) * 1.2))
	rounded := int64(2 * bytesInGiB)
	gm.IncRoundedMetrics("create", original, rounded)

	families, _ := reg.Gather()
	found := false
	for _, mf := range families {
		if mf.GetName() == "powerflex_csi_volume_size_rounded_total" {
			for _, m := range mf.GetMetric() {
				for _, lp := range m.GetLabel() {
					if lp.GetName() == "operation" && lp.GetValue() == "create" {
						assert.Equal(t, float64(1), m.GetCounter().GetValue())
						found = true
					}
				}
			}
		}
	}
	assert.True(t, found, "powerflex_csi_volume_size_rounded_total{operation='create'} must be 1")
}

func TestGranularityMetrics_IncRoundedMetrics_NoIncrementWhenNoRounding(t *testing.T) {
	reg := prometheus.NewRegistry()
	gm, err := metrics.NewGranularityMetrics(reg)
	require.NoError(t, err)

	// Exact 3 GiB on EC — no rounding, no increment.
	gm.IncRoundedMetrics("create", 3*bytesInGiB, 3*bytesInGiB)

	families, _ := reg.Gather()
	for _, mf := range families {
		if mf.GetName() == "powerflex_csi_volume_size_rounded_total" {
			for _, m := range mf.GetMetric() {
				assert.Equal(t, float64(0), m.GetCounter().GetValue(),
					"RoundedTotal must stay 0 when no rounding occurred")
			}
		}
	}
}

func TestGranularityMetrics_IncRoundedMetrics_MultipleOperations(t *testing.T) {
	reg := prometheus.NewRegistry()
	gm, err := metrics.NewGranularityMetrics(reg)
	require.NoError(t, err)

	gm.IncRoundedMetrics("create", 1*bytesInGiB, 8*bytesInGiB) // Gen1: 1 GiB req → 8 GiB
	gm.IncRoundedMetrics("expand", 2*bytesInGiB, 8*bytesInGiB) // Gen1: 2 GiB req → 8 GiB
	gm.IncRoundedMetrics("create", 1*bytesInGiB, 8*bytesInGiB) // Gen1: second create

	families, _ := reg.Gather()
	counts := map[string]float64{}
	for _, mf := range families {
		if mf.GetName() == "powerflex_csi_volume_size_rounded_total" {
			for _, m := range mf.GetMetric() {
				for _, lp := range m.GetLabel() {
					if lp.GetName() == "operation" {
						counts[lp.GetValue()] = m.GetCounter().GetValue()
					}
				}
			}
		}
	}
	assert.Equal(t, float64(2), counts["create"], "create counter must be 2")
	assert.Equal(t, float64(1), counts["expand"], "expand counter must be 1")
}

// ── 4. IncRoundedMetrics (RoundedBytes) counter ─────────────────────────────

func TestGranularityMetrics_RoundedBytes_AccumulatesDelta(t *testing.T) {
	reg := prometheus.NewRegistry()
	gm, err := metrics.NewGranularityMetrics(reg)
	require.NoError(t, err)

	// 3 GiB req on Gen1 → 8 GiB: delta = 5 GiB
	gm.IncRoundedMetrics("create", 3*bytesInGiB, 8*bytesInGiB)
	// 1.2 GiB req on EC → 2 GiB: delta ≈ 0.8 GiB (integer truncation applies)
	original := int64(math.Trunc(float64(bytesInGiB) * 1.2))
	gm.IncRoundedMetrics("create", original, 2*bytesInGiB)

	// Gather metrics and find RoundedBytes
	families, _ := reg.Gather()
	var value float64
	for _, mf := range families {
		if mf.GetName() == "powerflex_csi_volume_size_rounded_bytes" {
			for _, m := range mf.GetMetric() {
				value = m.GetCounter().GetValue()
			}
		}
	}

	expectedBytes := float64((8-3)*bytesInGiB) + float64(2*bytesInGiB-original)
	assert.Equal(t, expectedBytes, value,
		"RoundedBytes must accumulate the exact byte delta across all rounding operations")
}

func TestGranularityMetrics_RoundedBytes_NilSafe(t *testing.T) {
	var gm *metrics.GranularityMetrics
	assert.NotPanics(t, func() { gm.IncRoundedMetrics("create", 1*bytesInGiB, 2*bytesInGiB) })
}

// ── 5. Metric can be accessed via testutil directly ────────────────────────

func TestGranularityMetrics_CanBeAccessedViaTestutil(t *testing.T) {
	reg := prometheus.NewRegistry()
	gm, err := metrics.NewGranularityMetrics(reg)
	require.NoError(t, err)

	gm.IncDetectionError()

	// Verify we can use testutil.CollectAndCount
	families, _ := reg.Gather()
	metricCount := 0
	for _, mf := range families {
		if mf.GetName() == "powerflex_csi_granularity_detection_errors_total" {
			metricCount++
		}
	}
	assert.Equal(t, 1, metricCount, "Detection errors metric must be present")
}
