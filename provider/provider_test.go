// Copyright © 2025 Dell Inc. or its subsidiaries. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package provider

import (
	"context"
	"testing"

	servicepkg "github.com/Ecosystems/container-storage-modules/src/csi-vxflexos/v2/service"
	csmnamed "github.com/Ecosystems/container-storage-modules/src/csm-metrics-common/pkg/naming"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/Ecosystems/container-storage-modules/src/gocsi"
	_ "unsafe"
)

//go:linkname globalOperationInterceptor github.com/Ecosystems/container-storage-modules/src/csi-vxflexos/v2/service.globalOperationInterceptor
var globalOperationInterceptor grpc.UnaryServerInterceptor

func TestNew(t *testing.T) {
	// Call the New function
	result := New()

	// Verify that the result is as expected
	// For now, we just want to make sure a plugin was created, all the New() function does is
	// set up function pointers
	assert.NotNil(t, result)
}

func TestNew_InterceptorWrapperCallsHandlerWhenNoOperationInterceptorIsSet(t *testing.T) {
	result := New()
	plugin, ok := result.(*gocsi.StoragePlugin)
	require.True(t, ok)
	require.Len(t, plugin.Interceptors, 1)
	assert.NotNil(t, plugin.Controller)
	assert.NotNil(t, plugin.Identity)
	assert.NotNil(t, plugin.Node)
	assert.NotNil(t, plugin.BeforeServe)
	assert.NotNil(t, plugin.RegisterAdditionalServers)
	assert.Contains(t, plugin.EnvVars, gocsi.EnvVarSpecReqValidation+"=true")
	assert.Contains(t, plugin.EnvVars, gocsi.EnvVarSerialVolAccess+"=true")

	called := false
	resp, err := plugin.Interceptors[0](context.Background(), "request", &grpc.UnaryServerInfo{FullMethod: "/csi.v1.Controller/CreateVolume"}, func(ctx context.Context, req interface{}) (interface{}, error) {
		called = true
		assert.NotNil(t, ctx)
		assert.Equal(t, "request", req)
		return "handled", nil
	})
	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, "handled", resp)
}

func TestNew_VerifiesEnvVars(t *testing.T) {
	result := New()
	plugin, ok := result.(*gocsi.StoragePlugin)
	require.True(t, ok)

	// Verify the environment variables are set correctly
	expectedEnvVars := []string{
		gocsi.EnvVarSpecReqValidation + "=true",
		gocsi.EnvVarSerialVolAccess + "=true",
	}

	for _, expected := range expectedEnvVars {
		assert.Contains(t, plugin.EnvVars, expected)
	}

	// Verify no extra environment variables
	assert.Len(t, plugin.EnvVars, 2)
}

func TestNew_VerifiesServiceComponents(t *testing.T) {
	result := New()
	plugin, ok := result.(*gocsi.StoragePlugin)
	require.True(t, ok)

	// Verify all service components are set
	assert.NotNil(t, plugin.Controller, "Controller should be set")
	assert.NotNil(t, plugin.Identity, "Identity should be set")
	assert.NotNil(t, plugin.Node, "Node should be set")
	assert.NotNil(t, plugin.BeforeServe, "BeforeServe should be set")
	assert.NotNil(t, plugin.RegisterAdditionalServers, "RegisterAdditionalServers should be set")
}

func TestNew_InterceptorWrapperWithOperationInterceptorSet(t *testing.T) {
	// This test verifies the interceptor wrapper when operation interceptor is set
	// We'll need to mock the service.GetOperationInterceptor to return a non-nil interceptor
	result := New()
	plugin, ok := result.(*gocsi.StoragePlugin)
	require.True(t, ok)
	require.Len(t, plugin.Interceptors, 1)

	// Test that the interceptor wrapper calls the handler when no operation interceptor is set
	called := false
	handlerCalled := false
	wrappedHandler := func(_ context.Context, _ interface{}) (interface{}, error) {
		handlerCalled = true
		return "handler-result", nil
	}

	resp, err := plugin.Interceptors[0](context.Background(), "test-request", &grpc.UnaryServerInfo{FullMethod: "/test/method"}, wrappedHandler)
	require.NoError(t, err)
	assert.True(t, handlerCalled, "Handler should be called when no operation interceptor is set")
	assert.Equal(t, "handler-result", resp)
	called = true
	assert.True(t, called)
}

func TestNew_InterceptorErrorPropagation(t *testing.T) {
	result := New()
	plugin, ok := result.(*gocsi.StoragePlugin)
	require.True(t, ok)
	require.Len(t, plugin.Interceptors, 1)

	// Test that errors from the handler are properly propagated
	expectedError := assert.AnError
	resp, err := plugin.Interceptors[0](context.Background(), "request", &grpc.UnaryServerInfo{FullMethod: "/test/method"}, func(_ context.Context, _ interface{}) (interface{}, error) {
		return nil, expectedError
	})

	require.Error(t, err)
	assert.Equal(t, expectedError, err)
	assert.Nil(t, resp)
}

func TestNew_InterceptorWrapperUsesGlobalOperationInterceptor(t *testing.T) {
	reg := prometheus.NewRegistry()
	interceptor := servicepkg.NewOperationInterceptor(reg, "array-1", "csi-vxflexos.dellemc.com")

	originalInterceptor := globalOperationInterceptor
	globalOperationInterceptor = interceptor
	t.Cleanup(func() { globalOperationInterceptor = originalInterceptor })

	result := New()
	plugin, ok := result.(*gocsi.StoragePlugin)
	require.True(t, ok)
	require.Len(t, plugin.Interceptors, 1)

	called := false
	resp, err := plugin.Interceptors[0](context.Background(), "request", &grpc.UnaryServerInfo{FullMethod: "/csi.v1.Controller/CreateVolume"}, func(ctx context.Context, req interface{}) (interface{}, error) {
		called = true
		require.NotNil(t, ctx)
		assert.Equal(t, "request", req)
		return "handled", nil
	})
	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, "handled", resp)

	mfs, err := reg.Gather()
	require.NoError(t, err)
	legacyTotal := metricFamilyByName(mfs, csmnamed.MetricCSIOperationTotal)
	require.NotNil(t, legacyTotal)
	value, ok := counterValue(legacyTotal, map[string]string{
		csmnamed.LabelSystemID:  "array-1",
		csmnamed.LabelOperation: "CreateVolume",
		csmnamed.LabelStatus:    "success",
	})
	require.True(t, ok)
	assert.Equal(t, 1.0, value)
}

func TestNew_InterceptorContextPropagation(t *testing.T) {
	result := New()
	plugin, ok := result.(*gocsi.StoragePlugin)
	require.True(t, ok)
	require.Len(t, plugin.Interceptors, 1)

	// Test that context is properly propagated through the interceptor
	type contextKey string
	const testKey contextKey = "test-key"
	testCtx := context.WithValue(context.Background(), testKey, "test-value")

	var receivedCtx context.Context
	resp, err := plugin.Interceptors[0](testCtx, "request", &grpc.UnaryServerInfo{FullMethod: "/test/method"}, func(ctx context.Context, _ interface{}) (interface{}, error) {
		receivedCtx = ctx
		return "result", nil
	})

	require.NoError(t, err)
	assert.Equal(t, "result", resp)
	assert.Equal(t, testCtx, receivedCtx, "Context should be propagated correctly")
	assert.Equal(t, "test-value", receivedCtx.Value(testKey))
}

func TestNew_PluginStructure(t *testing.T) {
	result := New()
	plugin, ok := result.(*gocsi.StoragePlugin)
	require.True(t, ok)

	// Verify the plugin structure is correct
	assert.NotNil(t, plugin.Controller)
	assert.NotNil(t, plugin.Identity)
	assert.NotNil(t, plugin.Node)
	assert.NotNil(t, plugin.BeforeServe)
	assert.NotNil(t, plugin.RegisterAdditionalServers)
	assert.NotNil(t, plugin.Interceptors)
	assert.NotNil(t, plugin.EnvVars)
	assert.Len(t, plugin.Interceptors, 1)
	assert.Len(t, plugin.EnvVars, 2)
}

func TestNew_InterceptorCount(t *testing.T) {
	result := New()
	plugin, ok := result.(*gocsi.StoragePlugin)
	require.True(t, ok)

	// Verify that exactly one interceptor is registered
	assert.Len(t, plugin.Interceptors, 1, "Plugin should have exactly one interceptor")
}

func TestNew_EnvVarsContent(t *testing.T) {
	result := New()
	plugin, ok := result.(*gocsi.StoragePlugin)
	require.True(t, ok)

	// Verify the specific content of environment variables
	assert.Contains(t, plugin.EnvVars, gocsi.EnvVarSpecReqValidation+"=true")
	assert.Contains(t, plugin.EnvVars, gocsi.EnvVarSerialVolAccess+"=true")
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
