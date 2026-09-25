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

package service

import (
	"context"
	"testing"

	csmnamed "github.com/Ecosystems/container-storage-modules/src/csm-metrics-common/pkg/naming"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNewOperationInterceptor_EmitsLegacyAndGlobalMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	interceptor := NewOperationInterceptor(reg, "array-1", "csi-vxflexos.dellemc.com")

	successInfo := &grpc.UnaryServerInfo{FullMethod: "/csi.v1.Controller/CreateVolume"}
	successHandler := func(context.Context, interface{}) (interface{}, error) {
		return nil, nil
	}
	_, err := interceptor(context.Background(), nil, successInfo, successHandler)
	require.NoError(t, err)

	failureInfo := &grpc.UnaryServerInfo{FullMethod: "/csi.v1.Controller/DeleteVolume"}
	failureHandler := func(context.Context, interface{}) (interface{}, error) {
		return nil, status.Error(codes.Internal, "boom")
	}
	_, err = interceptor(context.Background(), nil, failureInfo, failureHandler)
	require.Error(t, err)

	mfs, err := reg.Gather()
	require.NoError(t, err)

	legacyTotal := metricFamilyByName(mfs, csmnamed.MetricCSIOperationTotal)
	require.NotNil(t, legacyTotal)
	globalTotal := metricFamilyByName(mfs, csmnamed.MetricCSIOperationTotal+"_by_global_id")
	require.NotNil(t, globalTotal)

	legacyCreateSuccess, ok := counterValue(legacyTotal, map[string]string{
		csmnamed.LabelSystemID:  "array-1",
		csmnamed.LabelOperation: "CreateVolume",
		csmnamed.LabelStatus:    "success",
	})
	require.True(t, ok)
	assert.Equal(t, 1.0, legacyCreateSuccess)

	legacyDeleteFailure, ok := counterValue(legacyTotal, map[string]string{
		csmnamed.LabelSystemID:  "array-1",
		csmnamed.LabelOperation: "DeleteVolume",
		csmnamed.LabelStatus:    "failure",
	})
	require.True(t, ok)
	assert.Equal(t, 1.0, legacyDeleteFailure)

	globalCreateSuccess, ok := counterValue(globalTotal, map[string]string{
		csmnamed.LabelGlobalID:  "csi-vxflexos.dellemc.com",
		csmnamed.LabelOperation: "CreateVolume",
		csmnamed.LabelStatus:    "success",
	})
	require.True(t, ok)
	assert.Equal(t, 1.0, globalCreateSuccess)

	globalDeleteFailure, ok := counterValue(globalTotal, map[string]string{
		csmnamed.LabelGlobalID:  "csi-vxflexos.dellemc.com",
		csmnamed.LabelOperation: "DeleteVolume",
		csmnamed.LabelStatus:    "failure",
	})
	require.True(t, ok)
	assert.Equal(t, 1.0, globalDeleteFailure)

	legacyDuration := metricFamilyByName(mfs, csmnamed.MetricCSIOperationDurationSeconds)
	require.NotNil(t, legacyDuration)
	assert.Len(t, legacyDuration.GetMetric(), 2)
	globalDuration := metricFamilyByName(mfs, csmnamed.MetricCSIOperationDurationSeconds+"_by_global_id")
	require.NotNil(t, globalDuration)
	assert.Len(t, globalDuration.GetMetric(), 2)
}

func TestNewOperationInterceptor_SkipsUnsupportedOperation(t *testing.T) {
	reg := prometheus.NewRegistry()
	interceptor := NewOperationInterceptor(reg, "array-1", "csi-vxflexos.dellemc.com")

	called := false
	resp, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/csi.v1.Controller/ListVolumes"}, func(context.Context, interface{}) (interface{}, error) {
		called = true
		return "handled", nil
	})
	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, "handled", resp)

	mfs, err := reg.Gather()
	require.NoError(t, err)
	legacyTotal := metricFamilyByName(mfs, csmnamed.MetricCSIOperationTotal)
	if legacyTotal != nil {
		_, ok := counterValue(legacyTotal, map[string]string{
			csmnamed.LabelSystemID:  "array-1",
			csmnamed.LabelOperation: "ListVolumes",
			csmnamed.LabelStatus:    "success",
		})
		assert.False(t, ok, "unsupported operations should bypass metric recording")
	}
}

func TestClassifyGRPCError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "nil", err: nil, want: "none"},
		{name: "deadline exceeded", err: status.Error(codes.DeadlineExceeded, "timeout"), want: "timeout"},
		{name: "canceled", err: status.Error(codes.Canceled, "canceled"), want: "timeout"},
		{name: "unauthenticated", err: status.Error(codes.Unauthenticated, "unauth"), want: "auth_failure"},
		{name: "permission denied", err: status.Error(codes.PermissionDenied, "denied"), want: "auth_failure"},
		{name: "not found", err: status.Error(codes.NotFound, "missing"), want: "not_found"},
		{name: "server error", err: status.Error(codes.Unavailable, "down"), want: "server_error"},
		{name: "unknown status", err: status.Error(codes.InvalidArgument, "bad input"), want: "unknown"},
		{name: "non-status error", err: context.DeadlineExceeded, want: "unknown"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, classifyGRPCError(tc.err))
		})
	}
}

func TestIsContextCancelled(t *testing.T) {
	assert.True(t, isContextCancelled(context.Canceled))
	assert.True(t, isContextCancelled(status.Error(codes.Canceled, "canceled")))
	assert.False(t, isContextCancelled(status.Error(codes.Internal, "boom")))
	assert.False(t, isContextCancelled(assert.AnError))
}

func TestShouldSkipOperation(t *testing.T) {
	assert.False(t, shouldSkipOperation("CreateVolume"))
	assert.True(t, shouldSkipOperation("ListVolumes"))
}

func metricFamilyByName(mfs []*dto.MetricFamily, name string) *dto.MetricFamily {
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf
		}
	}
	return nil
}

func counterValue(mf *dto.MetricFamily, labels map[string]string) (float64, bool) {
	if mf == nil {
		return 0, false
	}
	for _, m := range mf.GetMetric() {
		got := make(map[string]string, len(m.GetLabel()))
		for _, lp := range m.GetLabel() {
			got[lp.GetName()] = lp.GetValue()
		}
		match := true
		for k, v := range labels {
			if got[k] != v {
				match = false
				break
			}
		}
		if match {
			return m.GetCounter().GetValue(), true
		}
	}
	return 0, false
}
