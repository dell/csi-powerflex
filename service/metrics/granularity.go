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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metric name constants for ER-K8S-BR64714-001-powerflex-gen2-1gb-granularity granularity metrics (FR-7).
const (
	metricGranularityDetectionErrors = "powerflex_csi_granularity_detection_errors_total"
	metricVolumeSizeRoundedTotal     = "powerflex_csi_volume_size_rounded_total"
	metricVolumeSizeRoundedBytes     = "powerflex_csi_volume_size_rounded_bytes"
)

// NewGranularityMetrics creates a GranularityMetrics instance and registers
// the three ER-K8S-BR64714-001-powerflex-gen2-1gb-granularity Prometheus counters with the provided registry.
// Returns an error if any metric fails to register (non-AlreadyRegisteredError).
// The caller is responsible for logging registration failures.
func NewGranularityMetrics(reg *prometheus.Registry) (*GranularityMetrics, error) {
	detectionErrors, err := registerOrGet(reg, prometheus.NewCounter(prometheus.CounterOpts{
		Name: metricGranularityDetectionErrors,
		Help: "Total number of times an unrecognised array genType was returned by the PFMP API.",
	}))
	if err != nil {
		return nil, err
	}

	roundedTotal, err := registerOrGet(reg, prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: metricVolumeSizeRoundedTotal,
		Help: "Total number of volume size rounding operations by operation type.",
	}, []string{"operation"}))
	if err != nil {
		return nil, err
	}

	roundedBytes, err := registerOrGet(reg, prometheus.NewCounter(prometheus.CounterOpts{
		Name: metricVolumeSizeRoundedBytes,
		Help: "Total bytes added across all volume size rounding operations.",
	}))
	if err != nil {
		return nil, err
	}

	return &GranularityMetrics{
		detectionErrors: detectionErrors,
		roundedTotal:    roundedTotal,
		roundedBytes:    roundedBytes,
	}, nil
}

// IncDetectionError increments the detection error counter.
// This method is nil-safe; calling it on a nil receiver is a no-op.
func (gm *GranularityMetrics) IncDetectionError() {
	if gm == nil {
		return
	}
	gm.detectionErrors.Inc()
}

// IncRoundedMetrics increments both RoundedTotal{operation} and RoundedBytes
// by the number of bytes added when rounding from originalBytes to roundedBytes.
// This method is nil-safe and a no-op if gm is nil or no rounding occurred.
func (gm *GranularityMetrics) IncRoundedMetrics(operation string, originalBytes, roundedBytes int64) {
	if gm == nil || originalBytes >= roundedBytes {
		return
	}
	gm.roundedTotal.WithLabelValues(operation).Inc()
	gm.roundedBytes.Add(float64(roundedBytes - originalBytes))
}
