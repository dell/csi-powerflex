// Copyright © 2021-2026 Dell Inc. or its subsidiaries. All Rights Reserved.
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
//

package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Ecosystems/container-storage-modules/src/dell-csi-extensions/replication"
	sio "github.com/Ecosystems/container-storage-modules/src/goscaleio"
	siotypes "github.com/Ecosystems/container-storage-modules/src/goscaleio/types/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestRCGServer creates an httptest.Server that serves:
//   - GET /api/instances/ReplicationConsistencyGroup::{id} -> the given RCG
//   - GET /api/instances/ReplicationConsistencyGroup::{id}/relationships/Statistics -> stats (or error)
//   - GET /api/instances/ReplicationConsistencyGroup::{id}/relationships/ReplicationPair -> pairs
//
// If statsErr is true, the Statistics endpoint returns HTTP 500.
func newTestRCGServer(
	rcgID string,
	rcg *siotypes.ReplicationConsistencyGroup,
	stats *siotypes.ReplicationConsistencyGroupStatistics,
	statsErr bool,
	pairs []*siotypes.ReplicationPair,
) *httptest.Server {
	handler := http.NewServeMux()

	// Login endpoint for client authentication
	handler.HandleFunc("/api/login", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `"fake-token"`)
	})

	// Version endpoint
	handler.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "4.0")
	})

	// RCG instance endpoint - with self link
	rcgPath := fmt.Sprintf("/api/instances/ReplicationConsistencyGroup::%s", rcgID)
	handler.HandleFunc(rcgPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		encoder := json.NewEncoder(w)
		_ = encoder.Encode(rcg)
	})

	// Statistics endpoint
	statsPath := fmt.Sprintf("/api/instances/ReplicationConsistencyGroup::%s/relationships/Statistics", rcgID)
	handler.HandleFunc(statsPath, func(w http.ResponseWriter, _ *http.Request) {
		if statsErr {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"message":"statistics not supported"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		encoder := json.NewEncoder(w)
		_ = encoder.Encode(stats)
	})

	// ReplicationPair endpoint
	pairsPath := fmt.Sprintf("/api/instances/ReplicationConsistencyGroup::%s/relationships/ReplicationPair", rcgID)
	handler.HandleFunc(pairsPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		encoder := json.NewEncoder(w)
		_ = encoder.Encode(pairs)
	})

	return httptest.NewServer(handler)
}

// makeRCG creates a ReplicationConsistencyGroup with the specified parameters and proper Links.
func makeRCG(id, name, consistMode, abstractState, pauseMode, failoverType, replicationDirection string) *siotypes.ReplicationConsistencyGroup {
	return &siotypes.ReplicationConsistencyGroup{
		ID:                   id,
		Name:                 name,
		CurrConsistMode:      consistMode,
		AbstractState:        abstractState,
		PauseMode:            pauseMode,
		FailoverType:         failoverType,
		ReplicationDirection: replicationDirection,
		Links: []*siotypes.Link{
			{
				Rel:  "self",
				HREF: fmt.Sprintf("/api/instances/ReplicationConsistencyGroup::%s", id),
			},
			{
				Rel:  "/api/ReplicationConsistencyGroup/relationship/Statistics",
				HREF: fmt.Sprintf("/api/instances/ReplicationConsistencyGroup::%s/relationships/Statistics", id),
			},
			{
				Rel:  "/api/ReplicationConsistencyGroup/relationship/ReplicationPair",
				HREF: fmt.Sprintf("/api/instances/ReplicationConsistencyGroup::%s/relationships/ReplicationPair", id),
			},
		},
	}
}

func TestGetStorageProtectionGroupStatus_WithStatistics(t *testing.T) {
	rcgID := "test-rcg-001"
	rcg := makeRCG(rcgID, "test-rcg", sio.Consistent, "Ok", "None", "None", "LocalToRemote")

	stats := &siotypes.ReplicationConsistencyGroupStatistics{
		LagReceivedInMillis: 5000, // 5 seconds
		RplTransmitBwc: siotypes.BWC{
			TotalWeightInKb: 102400, // 102400 KB over 10 seconds = 10240 KB/s
			NumOccured:      100,
			NumSeconds:      10,
		},
	}

	pairs := []*siotypes.ReplicationPair{
		{ID: "pair-1", Name: "rp-test"},
	}

	server := newTestRCGServer(rcgID, rcg, stats, false, pairs)
	defer server.Close()

	client, err := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	require.NoError(t, err)
	client.SetToken("test-token")

	svc := &service{
		adminClients: map[string]*sio.Client{"sys1": client},
		opts:         Opts{replicationContextPrefix: "replication.storage.dell.com/"},
	}

	req := &replication.GetStorageProtectionGroupStatusRequest{
		ProtectionGroupId: rcgID,
		ProtectionGroupAttributes: map[string]string{
			"replication.storage.dell.com/systemName": "sys1",
		},
	}

	resp, err := svc.GetStorageProtectionGroupStatus(nil, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Status)

	assert.Equal(t, replication.StorageProtectionGroupStatus_SYNCHRONIZED, resp.Status.State)
	assert.True(t, resp.Status.IsSource)
	assert.Equal(t, int64(5), resp.Status.LagSeconds)                    // 5000ms / 1000 = 5s
	assert.Equal(t, int64(10240*1024), resp.Status.BandwidthBytesPerSec) // 10240 KB/s * 1024 = 10485760 B/s
}

func TestGetStorageProtectionGroupStatus_StatisticsError_GracefulFallback(t *testing.T) {
	rcgID := "test-rcg-002"
	rcg := makeRCG(rcgID, "test-rcg-err", sio.Consistent, "Ok", "None", "None", "LocalToRemote")

	pairs := []*siotypes.ReplicationPair{
		{ID: "pair-1", Name: "rp-test"},
	}

	// statsErr=true simulates PowerFlex < 4.0 not supporting statistics
	server := newTestRCGServer(rcgID, rcg, nil, true, pairs)
	defer server.Close()

	client, err := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	require.NoError(t, err)
	client.SetToken("test-token")

	svc := &service{
		adminClients: map[string]*sio.Client{"sys1": client},
		opts:         Opts{replicationContextPrefix: "replication.storage.dell.com/"},
	}

	req := &replication.GetStorageProtectionGroupStatusRequest{
		ProtectionGroupId: rcgID,
		ProtectionGroupAttributes: map[string]string{
			"replication.storage.dell.com/systemName": "sys1",
		},
	}

	resp, err := svc.GetStorageProtectionGroupStatus(nil, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Status)

	// State and IsSource should still be correct
	assert.Equal(t, replication.StorageProtectionGroupStatus_SYNCHRONIZED, resp.Status.State)
	assert.True(t, resp.Status.IsSource)
	// Lag and bandwidth should be 0 (graceful fallback)
	assert.Equal(t, int64(0), resp.Status.LagSeconds)
	assert.Equal(t, int64(0), resp.Status.BandwidthBytesPerSec)
}

func TestGetStorageProtectionGroupStatus_ZeroStatistics(t *testing.T) {
	rcgID := "test-rcg-003"
	rcg := makeRCG(rcgID, "test-rcg-zero", sio.Consistent, "Ok", "None", "None", "RemoteToLocal")

	stats := &siotypes.ReplicationConsistencyGroupStatistics{
		LagReceivedInMillis: 0,
		RplTransmitBwc: siotypes.BWC{
			TotalWeightInKb: 0,
			NumOccured:      0,
			NumSeconds:      0,
		},
	}

	pairs := []*siotypes.ReplicationPair{
		{ID: "pair-1", Name: "rp-test"},
	}

	server := newTestRCGServer(rcgID, rcg, stats, false, pairs)
	defer server.Close()

	client, err := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	require.NoError(t, err)
	client.SetToken("test-token")

	svc := &service{
		adminClients: map[string]*sio.Client{"sys1": client},
		opts:         Opts{replicationContextPrefix: "replication.storage.dell.com/"},
	}

	req := &replication.GetStorageProtectionGroupStatusRequest{
		ProtectionGroupId: rcgID,
		ProtectionGroupAttributes: map[string]string{
			"replication.storage.dell.com/systemName": "sys1",
		},
	}

	resp, err := svc.GetStorageProtectionGroupStatus(nil, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Status)

	assert.Equal(t, replication.StorageProtectionGroupStatus_SYNCHRONIZED, resp.Status.State)
	assert.False(t, resp.Status.IsSource) // RemoteToLocal
	assert.Equal(t, int64(0), resp.Status.LagSeconds)
	assert.Equal(t, int64(0), resp.Status.BandwidthBytesPerSec)
}

func TestGetStorageProtectionGroupStatus_SubSecondLag(t *testing.T) {
	rcgID := "test-rcg-004"
	rcg := makeRCG(rcgID, "test-rcg-subsec", sio.Consistent, "Ok", "None", "None", "LocalToRemote")

	stats := &siotypes.ReplicationConsistencyGroupStatistics{
		LagReceivedInMillis: 500, // 500ms -> 0 seconds, but should be rounded up to 1
		RplTransmitBwc: siotypes.BWC{
			TotalWeightInKb: 0,
			NumOccured:      0,
			NumSeconds:      0,
		},
	}

	pairs := []*siotypes.ReplicationPair{
		{ID: "pair-1", Name: "rp-test"},
	}

	server := newTestRCGServer(rcgID, rcg, stats, false, pairs)
	defer server.Close()

	client, err := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	require.NoError(t, err)
	client.SetToken("test-token")

	svc := &service{
		adminClients: map[string]*sio.Client{"sys1": client},
		opts:         Opts{replicationContextPrefix: "replication.storage.dell.com/"},
	}

	req := &replication.GetStorageProtectionGroupStatusRequest{
		ProtectionGroupId: rcgID,
		ProtectionGroupAttributes: map[string]string{
			"replication.storage.dell.com/systemName": "sys1",
		},
	}

	resp, err := svc.GetStorageProtectionGroupStatus(nil, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Status)

	// Sub-second lag (500ms) should be rounded up to minimum of 1 second
	assert.Equal(t, int64(1), resp.Status.LagSeconds)
}

func TestGetStorageProtectionGroupStatus_FailedoverState(t *testing.T) {
	rcgID := "test-rcg-005"
	rcg := makeRCG(rcgID, "test-rcg-fo", sio.Consistent, "StoppedByUser", "None", "Failover", "RemoteToLocal")

	stats := &siotypes.ReplicationConsistencyGroupStatistics{
		LagReceivedInMillis: 10000,
		RplTransmitBwc: siotypes.BWC{
			TotalWeightInKb: 51200,
			NumOccured:      50,
			NumSeconds:      10,
		},
	}

	pairs := []*siotypes.ReplicationPair{
		{ID: "pair-1", Name: "rp-test"},
	}

	server := newTestRCGServer(rcgID, rcg, stats, false, pairs)
	defer server.Close()

	client, err := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	require.NoError(t, err)
	client.SetToken("test-token")

	svc := &service{
		adminClients: map[string]*sio.Client{"sys1": client},
		opts:         Opts{replicationContextPrefix: "replication.storage.dell.com/"},
	}

	req := &replication.GetStorageProtectionGroupStatusRequest{
		ProtectionGroupId: rcgID,
		ProtectionGroupAttributes: map[string]string{
			"replication.storage.dell.com/systemName": "sys1",
		},
	}

	resp, err := svc.GetStorageProtectionGroupStatus(nil, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Status)

	assert.Equal(t, replication.StorageProtectionGroupStatus_FAILEDOVER, resp.Status.State)
	assert.False(t, resp.Status.IsSource)                               // RemoteToLocal
	assert.Equal(t, int64(10), resp.Status.LagSeconds)                  // 10000ms / 1000 = 10s
	assert.Equal(t, int64(5120*1024), resp.Status.BandwidthBytesPerSec) // 5120 KB/s * 1024
}

func TestGetStorageProtectionGroupStatus_SuspendedState(t *testing.T) {
	rcgID := "test-rcg-006"
	rcg := makeRCG(rcgID, "test-rcg-suspended", sio.Consistent, "StoppedByUser", "Paused", "None", "LocalToRemote")

	stats := &siotypes.ReplicationConsistencyGroupStatistics{
		LagReceivedInMillis: 3000,
		RplTransmitBwc: siotypes.BWC{
			TotalWeightInKb: 0,
			NumOccured:      0,
			NumSeconds:      10,
		},
	}

	pairs := []*siotypes.ReplicationPair{
		{ID: "pair-1", Name: "rp-test"},
	}

	server := newTestRCGServer(rcgID, rcg, stats, false, pairs)
	defer server.Close()

	client, err := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	require.NoError(t, err)
	client.SetToken("test-token")

	svc := &service{
		adminClients: map[string]*sio.Client{"sys1": client},
		opts:         Opts{replicationContextPrefix: "replication.storage.dell.com/"},
	}

	req := &replication.GetStorageProtectionGroupStatusRequest{
		ProtectionGroupId: rcgID,
		ProtectionGroupAttributes: map[string]string{
			"replication.storage.dell.com/systemName": "sys1",
		},
	}

	resp, err := svc.GetStorageProtectionGroupStatus(nil, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Status)

	assert.Equal(t, replication.StorageProtectionGroupStatus_SUSPENDED, resp.Status.State)
	assert.True(t, resp.Status.IsSource)                        // LocalToRemote
	assert.Equal(t, int64(3), resp.Status.LagSeconds)           // 3000ms / 1000 = 3s
	assert.Equal(t, int64(0), resp.Status.BandwidthBytesPerSec) // 0 KB/s bandwidth when suspended
}
