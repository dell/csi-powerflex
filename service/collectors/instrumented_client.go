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
	"errors"
	"fmt"
	"sync"

	"github.com/Ecosystems/container-storage-modules/src/csm-metrics-common/pkg/naming"
	goscaleioapi "github.com/Ecosystems/container-storage-modules/src/goscaleio/api"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	powerFlexAPIMetricsOnce sync.Once
	powerFlexAPIMetricsMu   sync.Mutex
	powerFlexAPIMetrics     *powerFlexAPIMetricsHolder
)

type powerFlexAPIMetricsHolder struct {
	apiTotal    *prometheus.CounterVec
	apiDuration *prometheus.HistogramVec
}

// PowerFlexAPIObserver records PowerFlex REST request totals using the standard metrics.
type PowerFlexAPIObserver struct {
	globalID string
	apiTotal *prometheus.CounterVec
	apiDur   *prometheus.HistogramVec
}

// NewPowerFlexAPIObserver creates or reuses the API request metrics for a registry.
func NewPowerFlexAPIObserver(reg prometheus.Registerer, globalID string) (*PowerFlexAPIObserver, error) {
	if reg == nil {
		return nil, fmt.Errorf("powerflex api observer: registry is nil")
	}

	metrics, err := getOrCreatePowerFlexAPIMetrics(reg)
	if err != nil {
		return nil, err
	}

	return &PowerFlexAPIObserver{
		globalID: globalID,
		apiTotal: metrics.apiTotal,
		apiDur:   metrics.apiDuration,
	}, nil
}

func getOrCreatePowerFlexAPIMetrics(reg prometheus.Registerer) (*powerFlexAPIMetricsHolder, error) {
	powerFlexAPIMetricsMu.Lock()
	defer powerFlexAPIMetricsMu.Unlock()

	powerFlexAPIMetricsOnce.Do(func() {
		powerFlexAPIMetrics = &powerFlexAPIMetricsHolder{
			apiTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Name: "dell_csi_driver_api_call_total",
				Help: "Total PowerFlex API calls.",
			}, []string{"driver", "array_id", "method", "status"}),
			apiDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Name:    "dell_csi_driver_api_call_duration_seconds",
				Help:    "PowerFlex API call duration.",
				Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5},
			}, []string{"driver", "array_id", "method"}),
		}
	})

	for _, collector := range []prometheus.Collector{powerFlexAPIMetrics.apiTotal, powerFlexAPIMetrics.apiDuration} {
		if err := reg.Register(collector); err != nil {
			var alreadyRegistered prometheus.AlreadyRegisteredError
			if errors.As(err, &alreadyRegistered) {
				continue
			}
			return nil, fmt.Errorf("powerflex api observer: register collector: %w", err)
		}
	}

	return powerFlexAPIMetrics, nil
}

// ObserveRequest implements goscaleioapi.RequestObserver.
func (o *PowerFlexAPIObserver) ObserveRequest(obs goscaleioapi.RequestObservation) {
	if o == nil || o.apiTotal == nil || o.apiDur == nil {
		return
	}

	endpoint := obs.Endpoint
	if endpoint == "" {
		endpoint = "unknown"
	}

	status := naming.LabelStatusSuccess
	if obs.Err != nil || obs.StatusCode >= 400 {
		status = naming.LabelStatusFailure
	}

	o.apiDur.WithLabelValues("csi-vxflexos", o.globalID, endpoint).Observe(obs.Duration.Seconds())
	o.apiTotal.WithLabelValues("csi-vxflexos", o.globalID, endpoint, status).Inc()
}
