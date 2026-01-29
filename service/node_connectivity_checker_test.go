/*
 *
 * Copyright © 2025 Dell Inc. or its subsidiaries. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package service

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"
)

var s service

func TestApiRouter1(t *testing.T) {
	s.opts.PodmonPort = ":abc"
	s.apiRouter(context.Background())

	resp, err := http.Get("http://localhost:8083/node-status")
	if err == nil || resp != nil {
		t.Errorf("Error while probing node status")
	}
}

func TestApiRouter2(t *testing.T) {
	s.opts.PodmonPort = ":8084"
	go s.apiRouter(context.Background())
	time.Sleep(2 * time.Second)

	resp4, err := http.Get("http://localhost:8084/array-status")
	if err != nil || resp4.StatusCode != 500 {
		t.Errorf("Error while probing array status %v", err)
	}
	// fill some invalid dummy data in the cache and try to fetch
	if s.probeStatus == nil {
		s.probeStatus = new(sync.Map)
	} else {
		s.probeStatus.Clear()
	}
	s.probeStatus.Store("SystemID2", "status")

	resp5, err := http.Get("http://localhost:8084/array-status")
	if err != nil || resp5.StatusCode != 500 {
		t.Errorf("Error while probing array status %v, %d", err, resp5.StatusCode)
	}

	// fill some dummy data in the cache and try to fetch
	var status ArrayConnectivityStatus
	status.LastSuccess = time.Now().Unix()
	status.LastAttempt = time.Now().Unix()
	s.probeStatus.Clear()
	s.probeStatus.Store("SystemID", status)

	// array status
	resp2, err := http.Get("http://localhost:8084/array-status")
	if err != nil || resp2.StatusCode != 200 {
		t.Errorf("Error while probing array status %v", err)
	}

	resp3, err := http.Get("http://localhost:8084/array-status/SymIDNotPresent")
	if err != nil || resp3.StatusCode != 404 {
		t.Errorf("Error while probing array status %v", err)
	}
	value := make(chan int)
	s.probeStatus.Store("SystemID3", value)
	resp9, err := http.Get("http://localhost:8084/array-status/SystemID3")
	if err != nil || resp9.StatusCode != 500 {
		t.Errorf("Error while probing array status %v", err)
	}
}

func TestMarshalSyncMapToJSON(t *testing.T) {
	type args struct {
		m *sync.Map
	}
	sample := new(sync.Map)
	sample2 := new(sync.Map)
	var status ArrayConnectivityStatus
	status.LastSuccess = time.Now().Unix()
	status.LastAttempt = time.Now().Unix()

	sample.Store("SystemID", status)
	sample2.Store("key", "2.adasd")

	tests := []struct {
		name string
		args args
	}{
		{"storing valid value in map cache", args{m: sample}},
		{"storing valid value in map cache", args{m: sample2}},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, _ := MarshalSyncMapToJSON(tt.args.m)
			if len(data) == 0 && i == 0 {
				t.Errorf("MarshalSyncMapToJSON() expecting some data from cache in the response")
				return
			}
		})
	}
}

func TestStartAPIServiceNoPodmon(_ *testing.T) {
	s.opts.IsPodmonEnabled = false
	s.startAPIService(context.Background())
}

func TestStartAPIService(_ *testing.T) {
	s.opts.IsPodmonEnabled = true
	os.Setenv(EnvPodmonArrayConnectivityPollRate, "60")
	defer os.Unsetenv(EnvPodmonArrayConnectivityPollRate)
	s.opts.arrays = map[string]*ArrayConnectionData{
		"array1": {
			SystemID: "array1",
		},
	}

	// Create a valid ArrayConnectivityStatus instance
	status := ArrayConnectivityStatus{
		LastSuccess: time.Now().Unix(),
		LastAttempt: time.Now().Unix(),
	}

	// Store valid data in probeStatus
	if s.probeStatus == nil {
		s.probeStatus = new(sync.Map)
	} else {
		s.probeStatus.Clear()
	}
	s.probeStatus.Store("SystemID", status)
	defer s.probeStatus.Delete("SystemID")

	s.startAPIService(context.Background())
}

func TestSetPollingFrequencyDefaultPollRate(_ *testing.T) {
	s.opts.PodmonPollingFreq = fmt.Sprintf("%d", DefaultPodmonPollRate)
	SetPollingFrequency(context.Background())
}
