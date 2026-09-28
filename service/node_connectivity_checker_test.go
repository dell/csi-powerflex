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
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

func TestApiRouter1(t *testing.T) {
	local := service{}
	local.opts.PodmonPort = ":abc"
	local.apiRouter(context.Background())

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:8083/node-status")
	if err == nil || resp != nil {
		t.Errorf("Error while probing node status")
	}
}

func TestApiRouter2(t *testing.T) {
	// Use a local service instance to avoid data races with BDD tests
	local := service{}
	local.opts.PodmonPort = ":8083"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go local.apiRouter(ctx)
	time.Sleep(2 * time.Second)

	resp4, err := http.Get("http://localhost:8083/array-status")
	if err != nil || resp4.StatusCode != 500 {
		t.Errorf("Error while probing array status %v", err)
	}
	// fill some invalid dummy data in the cache and try to fetch
	if local.probeStatus == nil {
		local.probeStatus = new(sync.Map)
	} else {
		local.probeStatus.Clear()
	}
	local.probeStatus.Store("SystemID2", "status")

	resp5, err := http.Get("http://localhost:8083/array-status")
	if err != nil || resp5.StatusCode != 500 {
		t.Errorf("Error while probing array status %v, %d", err, resp5.StatusCode)
	}

	// fill some dummy data in the cache and try to fetch
	var status ArrayConnectivityStatus
	status.LastSuccess = time.Now().Unix()
	status.LastAttempt = time.Now().Unix()
	local.probeStatus.Clear()
	local.probeStatus.Store("SystemID", status)

	// array status
	resp2, err := http.Get("http://localhost:8083/array-status")
	if err != nil || resp2.StatusCode != 200 {
		t.Errorf("Error while probing array status %v", err)
	}

	resp3, err := http.Get("http://localhost:8083/array-status/SymIDNotPresent")
	if err != nil || resp3.StatusCode != 404 {
		t.Errorf("Error while probing array status %v", err)
	}
	value := make(chan int)
	local.probeStatus.Store("SystemID3", value)
	resp9, err := http.Get("http://localhost:8083/array-status/SystemID3")
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
	local := service{}
	local.opts.IsPodmonEnabled = false
	local.startAPIService(context.Background())
}

func TestStartAPIService(_ *testing.T) {
	local := service{}
	local.opts.IsPodmonEnabled = true
	local.opts.PodmonPort = ":0"
	os.Setenv(EnvPodmonArrayConnectivityPollRate, "60")
	defer os.Unsetenv(EnvPodmonArrayConnectivityPollRate)
	local.opts.arrays = map[string]*ArrayConnectionData{
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
	local.probeStatus = new(sync.Map)
	local.probeStatus.Store("SystemID", status)
	defer local.probeStatus.Delete("SystemID")

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	local.startAPIService(ctx)
}

func TestSetPollingFrequencyDefaultPollRate(_ *testing.T) {
	local := service{}
	local.opts.PodmonPollingFreq = fmt.Sprintf("%d", DefaultPodmonPollRate)
	SetPollingFrequency(context.Background())
}

// TestPodmonAuthMiddleware manipulates the package-level PodmonAPIToken; it must not run in parallel.
func TestPodmonAuthMiddleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	tests := []struct {
		name       string
		token      string
		authHeader string
		wantStatus int
	}{
		{"no token configured", "", "", http.StatusOK},
		{"valid bearer token", "test-token", "Bearer test-token", http.StatusOK},
		{"valid bearer token with extra whitespace", "test-token", "Bearer test-token   ", http.StatusOK},
		{"valid lowercase bearer token (RFC 6750)", "test-token", "bearer test-token", http.StatusOK},
		{"valid mixed case bearer token (RFC 6750)", "test-token", "BEARER test-token", http.StatusOK},
		{"missing authorization header", "test-token", "", http.StatusUnauthorized},
		{"invalid bearer token", "test-token", "Bearer wrong-token", http.StatusUnauthorized},
		{"malformed authorization header", "test-token", "test-token", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := PodmonAPIToken
			PodmonAPIToken = tt.token
			defer func() { PodmonAPIToken = old }()

			req := httptest.NewRequest(http.MethodGet, "/array-status", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()

			podmonAuthMiddleware(next).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}
