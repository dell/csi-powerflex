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

// Package collectors provides Prometheus metric collectors for Dell PowerFlex.
package collectors

import (
	"context"
	"fmt"
	"strings"

	sio "github.com/Ecosystems/container-storage-modules/src/goscaleio"
	siotypes "github.com/Ecosystems/container-storage-modules/src/goscaleio/types/v1"
	"github.com/prometheus/client_golang/prometheus"
)

// ArrayHealthClient defines the PowerFlex client methods required by ArrayHealthCollector.
type ArrayHealthClient interface {
	GetVersion(context.Context) (string, error)
	GetMDMClusterDetails(context.Context) (*siotypes.MdmCluster, error)
}

// powerFlexArrayHealthClient adapts a goscaleio client to ArrayHealthClient.
type powerFlexArrayHealthClient struct {
	client *sio.Client
}

// NewPowerFlexArrayHealthClient creates an adapter for a goscaleio client.
func NewPowerFlexArrayHealthClient(client *sio.Client) ArrayHealthClient {
	return &powerFlexArrayHealthClient{client: client}
}

// GetVersion returns the PowerFlex version from the underlying goscaleio client.
func (a *powerFlexArrayHealthClient) GetVersion(ctx context.Context) (string, error) {
	var version string
	if err := withGoscaleIOContext(ctx, a.client, func() error {
		var err error
		version, err = a.client.GetVersion()
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return "", err
	}
	return version, nil
}

// GetMDMClusterDetails returns the MDM cluster details for the underlying system.
func (a *powerFlexArrayHealthClient) GetMDMClusterDetails(ctx context.Context) (*siotypes.MdmCluster, error) {
	var cluster *siotypes.MdmCluster
	if err := withGoscaleIOContext(ctx, a.client, func() error {
		var system *sio.System
		var err error
		system, err = a.client.FindSystem("", "", "")
		if err != nil {
			return err
		}
		cluster, err = system.GetMDMClusterDetails()
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return cluster, nil
}

// ArrayHealthCollector collects array-level health metrics for PowerFlex.
type ArrayHealthCollector struct {
	client              ArrayHealthClient
	runtime             *MetricsRuntime
	systemID            string
	managementEndpoint  string
	arrayHealthy        *prometheus.GaugeVec
	managementEndpointM *prometheus.GaugeVec
	mdmClusterState     *prometheus.GaugeVec
	gatewayReachable    *prometheus.GaugeVec
}

// NewArrayHealthCollector creates an ArrayHealthCollector and registers its Prometheus metrics.
func NewArrayHealthCollector(client ArrayHealthClient, reg prometheus.Registerer, systemID, managementEndpoint string) (*ArrayHealthCollector, error) {
	if managementEndpoint == "" {
		managementEndpoint = "unknown"
	}
	arrayHealthy, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_array_healthy",
		Help: "Whether the PowerFlex array is healthy (1) or unhealthy (0).",
	}, []string{"system_id"}))
	if err != nil {
		return nil, err
	}
	managementEndpointM, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_array_management_endpoint",
		Help: "PowerFlex management endpoint info metric.",
	}, []string{"system_id", "management_endpoint"}))
	if err != nil {
		return nil, err
	}
	mdmClusterState, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_powerflex_mdm_cluster_state",
		Help: "PowerFlex MDM cluster state, encoded as an info-style metric by state label.",
	}, []string{"system_id", "cluster_state"}))
	if err != nil {
		return nil, err
	}
	gatewayReachable, err := registerOrGet(reg, prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "dell_csi_gateway_status",
		Help: "Whether the PowerFlex management gateway is reachable (1) or unreachable (0).",
	}, []string{"driver", "gateway_address"}))
	if err != nil {
		return nil, err
	}

	return &ArrayHealthCollector{
		client:              client,
		systemID:            systemID,
		managementEndpoint:  managementEndpoint,
		arrayHealthy:        arrayHealthy,
		managementEndpointM: managementEndpointM,
		mdmClusterState:     mdmClusterState,
		gatewayReachable:    gatewayReachable,
	}, nil
}

// SetRuntime configures the resilience runtime used when collecting metrics.
func (c *ArrayHealthCollector) SetRuntime(runtime *MetricsRuntime) {
	c.runtime = runtime
}

// Collect fetches array health metrics from PowerFlex.
func (c *ArrayHealthCollector) Collect(ctx context.Context) error {
	labels := c.getLabels()

	// Clear previous values so the collector always reports the latest state.
	c.arrayHealthy.Reset()
	c.managementEndpointM.Reset()
	c.mdmClusterState.Reset()
	c.gatewayReachable.Reset()

	c.managementEndpointM.WithLabelValues(labels.systemID, labels.managementEndpoint).Set(1)

	var version string
	var err error
	if c.runtime == nil {
		version, err = c.client.GetVersion(ctx)
	} else {
		var result any
		result, err = c.runtime.Do(ctx, "array-version", c.systemID+":version", func(callCtx context.Context) (any, error) {
			return c.client.GetVersion(callCtx)
		})
		if err == nil {
			var ok bool
			version, ok = result.(string)
			if !ok {
				return fmt.Errorf("ArrayHealthCollector: unexpected runtime result type %T", result)
			}
		}
	}
	if err != nil {
		c.arrayHealthy.WithLabelValues(labels.systemID).Set(0)
		c.gatewayReachable.WithLabelValues("csi-vxflexos", labels.managementEndpoint).Set(0)
		c.mdmClusterState.WithLabelValues(labels.systemID, "unknown").Set(1)
		return fmt.Errorf("ArrayHealthCollector: gateway probe failed: %w", err)
	}

	// GetVersion is used solely as a gateway reachability probe; the version string itself is not needed.
	_ = version
	c.gatewayReachable.WithLabelValues("csi-vxflexos", labels.managementEndpoint).Set(1)

	var cluster *siotypes.MdmCluster
	if c.runtime == nil {
		cluster, err = c.client.GetMDMClusterDetails(ctx)
	} else {
		var result any
		result, err = c.runtime.Do(ctx, "array-mdm-cluster", c.systemID+":mdm-cluster", func(callCtx context.Context) (any, error) {
			return c.client.GetMDMClusterDetails(callCtx)
		})
		if err == nil {
			var ok bool
			cluster, ok = result.(*siotypes.MdmCluster)
			if !ok {
				return fmt.Errorf("ArrayHealthCollector: unexpected runtime result type %T", result)
			}
		}
	}
	if err != nil {
		c.arrayHealthy.WithLabelValues(labels.systemID).Set(0)
		c.mdmClusterState.WithLabelValues(labels.systemID, "unknown").Set(1)
		return fmt.Errorf("ArrayHealthCollector: failed to get MDM cluster details: %w", err)
	}

	clusterState := normalizeClusterState(cluster.ClusterState)
	c.mdmClusterState.WithLabelValues(labels.systemID, clusterState).Set(1)
	if clusterState == "online" || clusterState == "configured" {
		c.arrayHealthy.WithLabelValues(labels.systemID).Set(1)
	} else {
		c.arrayHealthy.WithLabelValues(labels.systemID).Set(0)
	}

	return nil
}

// Name returns the collector name.
func (c *ArrayHealthCollector) Name() string { return "ArrayHealthCollector" }

type arrayHealthLabels struct {
	systemID           string
	managementEndpoint string
}

func (c *ArrayHealthCollector) getLabels() arrayHealthLabels {
	labels := arrayHealthLabels{systemID: c.systemID, managementEndpoint: c.managementEndpoint}
	if strings.TrimSpace(labels.systemID) == "" {
		labels.systemID = "unknown"
	}
	if strings.TrimSpace(labels.managementEndpoint) == "" {
		labels.managementEndpoint = "unknown"
	}
	return labels
}

func normalizeClusterState(state string) string {
	s := strings.ToLower(strings.TrimSpace(state))
	if s == "" {
		return "unknown"
	}
	// Check degraded before healthy: "unstable" contains "stable" so the degraded
	// check must come first to avoid misclassifying unhealthy states as "online".
	if strings.Contains(s, "degrad") || strings.Contains(s, "warn") ||
		strings.Contains(s, "partial") || strings.Contains(s, "error") ||
		strings.Contains(s, "fail") || strings.Contains(s, "unstable") {
		return "degraded"
	}
	// Use HasSuffix for "normal" rather than Contains to avoid "abnormal" being
	// misclassified as "online".  The canonical PowerFlex MDM state is "ClusteredNormal"
	// (lower-cased: "clusterednormal") whose suffix is "normal".  The string "abnormal"
	// also ends with "normal", so we guard with !HasPrefix("ab") to exclude it.
	// This still correctly maps bare "Normal" → "online" as a future-safe fallback.
	// All other terms in this branch are unambiguous substrings.
	if strings.Contains(s, "optimal") || strings.Contains(s, "online") ||
		strings.Contains(s, "healthy") || strings.Contains(s, "stable") ||
		strings.Contains(s, "ready") || strings.Contains(s, "ok") ||
		strings.Contains(s, "clustered") ||
		(strings.HasSuffix(s, "normal") && !strings.HasPrefix(s, "ab")) {
		return "online"
	}
	if strings.Contains(s, "config") || strings.Contains(s, "initial") || strings.Contains(s, "init") {
		return "configured"
	}
	return "unknown"
}
