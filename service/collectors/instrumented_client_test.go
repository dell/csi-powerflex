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

package collectors_test

import (
	"errors"
	"testing"
	"time"

	"github.com/Ecosystems/container-storage-modules/src/csi-vxflexos/v2/service/collectors"
	goscaleioapi "github.com/Ecosystems/container-storage-modules/src/goscaleio/api"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// U-PFX-17: Successful API observation increments success counter.
func TestPowerFlexAPIObserver_ObserveRequest_Success(t *testing.T) {
	reg := prometheus.NewRegistry()
	observer, err := collectors.NewPowerFlexAPIObserver(reg, "system-1")
	require.NoError(t, err)

	observer.ObserveRequest(goscaleioapi.RequestObservation{
		Endpoint:   "/api/instances/Volume",
		Method:     "GET",
		StatusCode: 200,
		Duration:   42 * time.Millisecond,
	})

	mf := gatherMetric(t, reg, "dell_csi_driver_api_call_total")
	require.NotNil(t, mf)
	v, ok := findCounterValue(mf, map[string]string{
		"driver": "csi-vxflexos", "array_id": "system-1", "method": "/api/instances/Volume", "status": "success",
	})
	require.True(t, ok)
	assert.Equal(t, 1.0, v)
}

// U-PFX-18: Failed API observation increments failure counter.
func TestPowerFlexAPIObserver_ObserveRequest_Failure(t *testing.T) {
	reg := prometheus.NewRegistry()
	observer, err := collectors.NewPowerFlexAPIObserver(reg, "system-1")
	require.NoError(t, err)

	observer.ObserveRequest(goscaleioapi.RequestObservation{
		Endpoint:   "/api/instances/StoragePool",
		Method:     "GET",
		StatusCode: 503,
		Duration:   15 * time.Millisecond,
		Err:        errors.New("service unavailable"),
	})

	mf := gatherMetric(t, reg, "dell_csi_driver_api_call_total")
	require.NotNil(t, mf)
	v, ok := findCounterValue(mf, map[string]string{
		"driver": "csi-vxflexos", "array_id": "system-1", "method": "/api/instances/StoragePool", "status": "failure",
	})
	require.True(t, ok)
	assert.Equal(t, 1.0, v)
}

// U-PFX-19: nil registry returns an error.
func TestPowerFlexAPIObserver_NilRegistry_ReturnsError(t *testing.T) {
	observer, err := collectors.NewPowerFlexAPIObserver(nil, "system-1")
	require.Error(t, err)
	assert.Nil(t, observer)
	assert.Contains(t, err.Error(), "registry is nil")
}

func TestPowerFlexAPIObserver_ObserveRequest_NilObserverAndEmptyEndpoint(t *testing.T) {
	var observer *collectors.PowerFlexAPIObserver
	observer.ObserveRequest(goscaleioapi.RequestObservation{Endpoint: "", StatusCode: 200, Duration: 5 * time.Millisecond})

	reg := prometheus.NewRegistry()
	observer, err := collectors.NewPowerFlexAPIObserver(reg, "system-1")
	require.NoError(t, err)

	observer.ObserveRequest(goscaleioapi.RequestObservation{Endpoint: "", StatusCode: 200, Duration: 7 * time.Millisecond})

	mf := gatherMetric(t, reg, "dell_csi_driver_api_call_total")
	require.NotNil(t, mf)
	v, ok := findCounterValue(mf, map[string]string{"driver": "csi-vxflexos", "array_id": "system-1", "method": "unknown", "status": "success"})
	require.True(t, ok)
	assert.Equal(t, 1.0, v)
}

func TestPowerFlexAPIObserver_AlreadyRegisteredMetricsAreReused(t *testing.T) {
	reg := prometheus.NewRegistry()
	first, err := collectors.NewPowerFlexAPIObserver(reg, "system-1")
	require.NoError(t, err)
	second, err := collectors.NewPowerFlexAPIObserver(reg, "system-1")
	require.NoError(t, err)
	require.NotNil(t, first)
	require.NotNil(t, second)

	first.ObserveRequest(goscaleioapi.RequestObservation{Endpoint: "first", StatusCode: 200, Duration: time.Millisecond})
	second.ObserveRequest(goscaleioapi.RequestObservation{Endpoint: "second", StatusCode: 200, Duration: time.Millisecond})

	mf := gatherMetric(t, reg, "dell_csi_driver_api_call_total")
	require.NotNil(t, mf)
	firstCount, ok := findCounterValue(mf, map[string]string{"driver": "csi-vxflexos", "array_id": "system-1", "method": "first", "status": "success"})
	require.True(t, ok)
	secondCount, ok := findCounterValue(mf, map[string]string{"driver": "csi-vxflexos", "array_id": "system-1", "method": "second", "status": "success"})
	require.True(t, ok)
	assert.Equal(t, 1.0, firstCount)
	assert.Equal(t, 1.0, secondCount)
}

type apiObserverFailingRegisterer struct{ err error }

func (f apiObserverFailingRegisterer) Register(prometheus.Collector) error  { return f.err }
func (f apiObserverFailingRegisterer) MustRegister(...prometheus.Collector) {}
func (f apiObserverFailingRegisterer) Unregister(prometheus.Collector) bool { return false }

func TestPowerFlexAPIObserver_RegisterError(t *testing.T) {
	observer, err := collectors.NewPowerFlexAPIObserver(apiObserverFailingRegisterer{err: errors.New("register failed")}, "system-1")
	require.Error(t, err)
	assert.Nil(t, observer)
	assert.Contains(t, err.Error(), "register collector")
}
