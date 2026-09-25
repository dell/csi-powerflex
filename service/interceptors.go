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

package service

import (
	"context"
	"strings"
	"time"

	csmnamed "github.com/Ecosystems/container-storage-modules/src/csm-metrics-common/pkg/naming"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type operationMetrics struct {
	total    *prometheus.CounterVec
	duration *prometheus.HistogramVec
	failure  *prometheus.CounterVec
}

func newOperationMetrics(reg prometheus.Registerer, totalName, durationName, failureName string, idLabel string) *operationMetrics {
	metrics := &operationMetrics{
		total: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: totalName,
			Help: "Total CSI operations.",
		}, []string{idLabel, csmnamed.LabelOperation, csmnamed.LabelStatus}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    durationName,
			Help:    "CSI operation duration.",
			Buckets: csmnamed.HistogramBuckets,
		}, []string{idLabel, csmnamed.LabelOperation}),
		failure: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: failureName,
			Help: "Total CSI operation failures.",
		}, []string{idLabel, csmnamed.LabelOperation, csmnamed.LabelErrorCode}),
	}
	reg.MustRegister(metrics.total, metrics.duration, metrics.failure)
	return metrics
}

func (m *operationMetrics) observe(id, operation string, duration time.Duration) {
	m.duration.WithLabelValues(id, operation).Observe(duration.Seconds())
}

func (m *operationMetrics) recordSuccess(id, operation string) {
	m.total.WithLabelValues(id, operation, "success").Inc()
}

func (m *operationMetrics) recordFailure(id, operation string, err error) {
	m.total.WithLabelValues(id, operation, "failure").Inc()
	m.failure.WithLabelValues(id, operation, classifyGRPCError(err)).Inc()
}

var allowedOperations = map[string]bool{
	"CreateVolume":              true,
	"DeleteVolume":              true,
	"ControllerPublishVolume":   true,
	"ControllerUnpublishVolume": true,
	"NodeStageVolume":           true,
	"NodeUnstageVolume":         true,
	"NodePublishVolume":         true,
	"NodeUnpublishVolume":       true,
}

// NewOperationInterceptor returns a gRPC UnaryServerInterceptor that records
// standardized CSI operation metrics for the 8 core CSI volume lifecycle operations.
// The legacy system_id-labeled metrics are retained for dashboard compatibility,
// while the new global_id-labeled series are emitted under additive metric names.
func NewOperationInterceptor(reg prometheus.Registerer, systemID, driverID string) grpc.UnaryServerInterceptor {
	if systemID == "" {
		systemID = "unknown"
	}
	if driverID == "" {
		driverID = "unknown"
	}

	legacyMetrics := newOperationMetrics(reg,
		csmnamed.MetricCSIOperationTotal,
		csmnamed.MetricCSIOperationDurationSeconds,
		csmnamed.MetricCSIOperationFailureTotal,
		csmnamed.LabelSystemID,
	)
	globalMetrics := newOperationMetrics(reg,
		csmnamed.MetricCSIOperationTotal+"_by_global_id",
		csmnamed.MetricCSIOperationDurationSeconds+"_by_global_id",
		csmnamed.MetricCSIOperationFailureTotal+"_by_global_id",
		csmnamed.LabelGlobalID,
	)

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		operation := extractOperationName(info.FullMethod)
		if shouldSkipOperation(operation) {
			return handler(ctx, req)
		}

		resp, err := handler(ctx, req)
		duration := time.Since(start)

		legacyMetrics.observe(systemID, operation, duration)
		globalMetrics.observe(driverID, operation, duration)

		if err != nil {
			if isContextCancelled(err) {
				// context-cancelled calls are not counted as failures
				return resp, err
			}
			legacyMetrics.recordFailure(systemID, operation, err)
			globalMetrics.recordFailure(driverID, operation, err)
		} else {
			legacyMetrics.recordSuccess(systemID, operation)
			globalMetrics.recordSuccess(driverID, operation)
		}
		return resp, err
	}
}

func shouldSkipOperation(operation string) bool {
	return !allowedOperations[operation]
}

func extractOperationName(fullMethod string) string {
	parts := strings.Split(fullMethod, "/")
	if len(parts) == 0 {
		return "unknown"
	}
	return parts[len(parts)-1]
}

func classifyGRPCError(err error) string {
	if err == nil {
		return "none"
	}
	s, ok := status.FromError(err)
	if !ok {
		return "unknown"
	}
	switch s.Code() {
	case codes.DeadlineExceeded, codes.Canceled:
		return "timeout"
	case codes.Unauthenticated, codes.PermissionDenied:
		return "auth_failure"
	case codes.NotFound:
		return "not_found"
	case codes.Internal, codes.Unavailable:
		return "server_error"
	default:
		return "unknown"
	}
}

func isContextCancelled(err error) bool {
	if err == context.Canceled {
		return true
	}
	s, ok := status.FromError(err)
	return ok && s.Code() == codes.Canceled
}
