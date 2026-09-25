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
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/Ecosystems/container-storage-modules/src/csi-vxflexos/v2/k8sutils"
	"github.com/Ecosystems/container-storage-modules/src/csm-metrics-common/pkg/naming"
	csmlog "github.com/Ecosystems/container-storage-modules/src/csmlog"
	"github.com/prometheus/client_golang/prometheus"
	k8score "k8s.io/api/core/v1"
)

// DriverHealthCollector collects PowerFlex driver health metrics.
type DriverHealthCollector struct {
	systemID             string
	startTime            time.Time
	uptimeGauge          *prometheus.GaugeVec
	restartTotal         *prometheus.GaugeVec
	goroutineCount       *prometheus.GaugeVec
	driverGoroutines     *prometheus.GaugeVec
	connPoolActive       *prometheus.GaugeVec
	driverConnPoolActive *prometheus.GaugeVec
	cpuUsage             *prometheus.GaugeVec
	memUsage             *prometheus.GaugeVec
	apiSuccessRate       *prometheus.GaugeVec
	prometheusGather     prometheus.Gatherer
}

// NewDriverHealthCollector creates a DriverHealthCollector and registers its Prometheus metrics.
func NewDriverHealthCollector(reg prometheus.Registerer, systemID string) (*DriverHealthCollector, error) {
	uptimeGauge, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: naming.MetricCSIDriverUptimeSeconds,
		Help: "Driver uptime in seconds since last restart.",
	}, []string{"driver", "pod"}))
	if err != nil {
		return nil, err
	}
	restartTotal, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: naming.MetricCSIDriverRestartTotal,
		Help: "Total driver restarts from Kubernetes Pod API.",
	}, []string{"driver", "pod"}))
	if err != nil {
		return nil, err
	}
	goroutineCount, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: naming.MetricCSIGoroutineCount,
		Help: "Number of active goroutines.",
	}, []string{"driver", "pod"}))
	if err != nil {
		return nil, err
	}
	driverGoroutines, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_csi_driver_goroutines",
		Help: "Number of active goroutines.",
	}, []string{"driver", "pod"}))
	if err != nil {
		return nil, err
	}
	connPoolActive, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: naming.MetricCSIConnectionPoolActive,
		Help: "Active connections to PowerFlex array.",
	}, []string{"driver", "pod"}))
	if err != nil {
		return nil, err
	}
	driverConnPoolActive, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_csi_driver_connection_pool_active",
		Help: "Active connections to PowerFlex array.",
	}, []string{"driver", "array_id"}))
	if err != nil {
		return nil, err
	}
	cpuUsage, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_csi_driver_cpu_usage_percent",
		Help: "CSI driver CPU usage percentage from Kubernetes metrics API.",
	}, []string{"driver", "pod"}))
	if err != nil {
		return nil, err
	}
	memUsage, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_csi_driver_memory_usage_bytes",
		Help: "CSI driver memory usage in bytes from Kubernetes metrics API.",
	}, []string{"driver", "pod"}))
	if err != nil {
		return nil, err
	}
	apiSuccessRate, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_csi_api_success_rate_percent",
		Help: "PowerFlex API call success rate percentage.",
	}, []string{"driver", "pod"}))
	if err != nil {
		return nil, err
	}

	c := &DriverHealthCollector{
		systemID:             systemID,
		startTime:            time.Now().Add(-time.Millisecond),
		uptimeGauge:          uptimeGauge,
		restartTotal:         restartTotal,
		goroutineCount:       goroutineCount,
		driverGoroutines:     driverGoroutines,
		connPoolActive:       connPoolActive,
		driverConnPoolActive: driverConnPoolActive,
		cpuUsage:             cpuUsage,
		memUsage:             memUsage,
		apiSuccessRate:       apiSuccessRate,
	}
	if gatherer, ok := reg.(prometheus.Gatherer); ok {
		c.prometheusGather = gatherer
	}
	return c, nil
}

// getPodIdentityLabels returns the pod identity labels for metrics
func (c *DriverHealthCollector) getPodIdentityLabels() []string {
	podName := os.Getenv("X_CSI_POD_NAME")

	// Provide default value if env var is not set
	if podName == "" {
		podName = "unknown"
	}

	return []string{"csi-vxflexos", podName}
}

// Collect updates driver health metrics.
func (c *DriverHealthCollector) Collect(ctx context.Context) error {
	labels := c.getPodIdentityLabels()
	c.uptimeGauge.WithLabelValues(labels...).Set(time.Since(c.startTime).Seconds())
	c.goroutineCount.WithLabelValues(labels...).Set(float64(runtime.NumGoroutine()))
	c.driverGoroutines.WithLabelValues(labels...).Set(float64(runtime.NumGoroutine()))

	// Initialize restart counter to 0 if not set by Kubernetes
	c.restartTotal.WithLabelValues(labels...).Set(0)

	// Collect restart count from Kubernetes Pod API (skip if not available)
	c.collectRestartCount(ctx)

	// Use Kubernetes metrics API (skip if not available)
	c.collectKubernetesMetrics(ctx)

	// Calculate API success rate from prometheus metrics
	c.collectAPISuccessRate()

	// Connection pool active - set to 1 as proxy for client being active
	c.connPoolActive.WithLabelValues(labels...).Set(1)
	c.driverConnPoolActive.WithLabelValues("csi-vxflexos", c.systemID).Set(1)

	return nil
}

// collectRestartCount fetches the restart count from Kubernetes Pod API
func (c *DriverHealthCollector) collectRestartCount(ctx context.Context) {
	// Get pod name and namespace from environment variables
	podName := os.Getenv("X_CSI_POD_NAME")
	namespace := os.Getenv("X_CSI_DRIVER_NAMESPACE")

	if podName == "" || namespace == "" {
		return
	}

	// Check if Kubeclient is initialized
	if k8sutils.Kubeclient == nil {
		return
	}

	// Get restart count from Kubernetes Pod API (auto-detect container name)
	restartCount, err := k8sutils.Kubeclient.GetPodRestartCountAuto(ctx, namespace, podName)
	if err != nil {
		csmlog.WithFields(csmlog.Fields{
			csmlog.FieldComponent: "driver_health_collector",
			csmlog.FieldOperation: "collectRestartCount",
			csmlog.FieldError:     err.Error(),
		}).Warn("failed to get restart count")
		return
	}

	// Set the restart count metric with pod identity labels
	labels := c.getPodIdentityLabels()
	c.restartTotal.WithLabelValues(labels...).Set(float64(restartCount))
}

// collectKubernetesMetrics fetches CPU/memory metrics from Kubernetes Metrics API
func (c *DriverHealthCollector) collectKubernetesMetrics(ctx context.Context) {
	// Get pod name and namespace from environment variables
	podName := os.Getenv("X_CSI_POD_NAME")
	namespace := os.Getenv("X_CSI_DRIVER_NAMESPACE")

	if podName == "" || namespace == "" {
		return
	}

	// Check if Kubeclient is initialized
	if k8sutils.Kubeclient == nil {
		return
	}

	// Get pod metrics from Kubernetes
	podMetrics, err := k8sutils.Kubeclient.GetPodMetrics(ctx, namespace, podName)
	if err != nil {
		return
	}

	// Find the CSI driver container metrics
	for _, container := range podMetrics.Containers {
		// Match container name (typically "csi-vxflexos" or "driver")
		if strings.Contains(container.Name, "csi-vxflexos") || strings.Contains(container.Name, "driver") {
			// Extract CPU usage (in cores, convert to percentage)
			cpuUsage := container.Usage[k8score.ResourceCPU]
			cpuPercent := float64(cpuUsage.MilliValue()) / 10.0 // Convert millicores to percentage

			// Extract memory usage (in bytes)
			memUsage := container.Usage[k8score.ResourceMemory]

			// Set metrics with pod identity labels
			labels := c.getPodIdentityLabels()
			c.cpuUsage.WithLabelValues(labels...).Set(cpuPercent)
			c.memUsage.WithLabelValues(labels...).Set(float64(memUsage.Value()))

			return
		}
	}
}

// collectAPISuccessRate calculates the API success rate from prometheus metrics
func (c *DriverHealthCollector) collectAPISuccessRate() {
	if c.prometheusGather == nil {
		return
	}

	// Gather metrics from prometheus
	metricFamilies, err := c.prometheusGather.Gather()
	if err != nil {
		return
	}

	var totalSuccess, totalFailure float64

	// Find the dell_csi_driver_api_call_total metric and sum success/failure for this system
	for _, mf := range metricFamilies {
		if mf.GetName() == "dell_csi_driver_api_call_total" {
			for _, m := range mf.GetMetric() {
				// Check if this metric is for our systemID (now called array_id)
				var isOurSystem bool
				var status string
				for _, label := range m.GetLabel() {
					if label.GetName() == "array_id" && label.GetValue() == c.systemID {
						isOurSystem = true
					}
					if label.GetName() == naming.LabelStatus {
						status = label.GetValue()
					}
				}

				if isOurSystem {
					value := m.GetCounter().GetValue()
					if status == naming.LabelStatusSuccess {
						totalSuccess += value
					} else if status == naming.LabelStatusFailure {
						totalFailure += value
					}
				}
			}
		}
	}

	// Calculate success rate percentage
	total := totalSuccess + totalFailure
	var successRate float64
	if total > 0 {
		successRate = (totalSuccess / total) * 100.0
	} else {
		// No API calls yet, report 100% (no failures)
		successRate = 100.0
	}

	// Set the metric
	labels := c.getPodIdentityLabels()
	c.apiSuccessRate.WithLabelValues(labels...).Set(successRate)
}

// Name returns the collector name.
func (c *DriverHealthCollector) Name() string { return "DriverHealthCollector" }
