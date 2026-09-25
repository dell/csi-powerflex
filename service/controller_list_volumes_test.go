// Copyright © 2024-2026 Dell Inc. or its subsidiaries. All Rights Reserved.
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
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	sio "github.com/Ecosystems/container-storage-modules/src/goscaleio"
	siotypes "github.com/Ecosystems/container-storage-modules/src/goscaleio/types/v1"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
)

// newListVolumesTestServer creates a mock PowerFlex API server that returns
// a list of volumes, systems, and storage pools for ListVolumes tests.
func newListVolumesTestServer(t *testing.T, volumes, systems, storagePools string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/types/Volume/instances":
			fmt.Fprint(w, volumes)
		case "/api/types/System/instances":
			fmt.Fprint(w, systems)
		case "/api/types/StoragePool/instances":
			fmt.Fprint(w, storagePools)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func Test_service_listVolumes_CacheSystemIDMismatchFetchesFresh(t *testing.T) {
	systemID := "sys1"
	volumesJSON := `[
		{"id":"vol0","name":"vol0","sizeInKb":1048576,"storagePoolId":"sp1","ancestorVolumeId":"","creationTime":0},
		{"id":"vol1","name":"vol1","sizeInKb":1048576,"storagePoolId":"sp1","ancestorVolumeId":"","creationTime":0},
		{"id":"vol2","name":"vol2","sizeInKb":1048576,"storagePoolId":"sp1","ancestorVolumeId":"","creationTime":0},
		{"id":"vol3","name":"vol3","sizeInKb":1048576,"storagePoolId":"sp1","ancestorVolumeId":"","creationTime":0},
		{"id":"vol4","name":"vol4","sizeInKb":1048576,"storagePoolId":"sp1","ancestorVolumeId":"","creationTime":0}
	]`

	server := newListVolumesTestServer(t, volumesJSON, `[{"id":"sys1","installId":"install-1"}]`, `[{"id":"sp1","name":"pool1"}]`)
	defer server.Close()

	client, err := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	client.SetToken("test-token")

	s := &service{
		adminClients:     map[string]*sio.Client{systemID: client},
		volCache:         []*siotypes.Volume{{ID: "cached"}},
		volCacheRWL:      sync.RWMutex{},
		volCacheSystemID: "other-sys",
	}

	vols, nextToken, err := s.listVolumes(systemID, 1, 1, true, false, "", "")
	if err != nil {
		t.Fatalf("listVolumes returned error: %v", err)
	}
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(vols))
	}
	if vols[0] == nil || vols[0].ID != "vol1" {
		t.Fatalf("expected volume id vol1, got %v", vols[0])
	}
	if nextToken != "2" {
		t.Fatalf("expected nextToken 2, got %s", nextToken)
	}
}

func Test_service_ListVolumes_MultipleArrays(t *testing.T) {
	server1 := newListVolumesTestServer(t,
		`[{"id":"vol1","name":"vol1","sizeInKb":1048576,"storagePoolId":"sp1","ancestorVolumeId":"","creationTime":0}]`,
		`[{"id":"sys1","installId":"install-1"}]`,
		`[{"id":"sp1","name":"pool1"}]`)
	defer server1.Close()

	server2 := newListVolumesTestServer(t,
		`[{"id":"vol2","name":"vol2","sizeInKb":1048576,"storagePoolId":"sp2","ancestorVolumeId":"","creationTime":0}]`,
		`[{"id":"sys2","installId":"install-2"}]`,
		`[{"id":"sp2","name":"pool2"}]`)
	defer server2.Close()

	client1, err := sio.NewClientWithArgs(server1.URL, "4.0", 0, true, false, "")
	if err != nil {
		t.Fatalf("failed to create client 1: %v", err)
	}
	client1.SetToken("test-token")

	client2, err := sio.NewClientWithArgs(server2.URL, "4.0", 0, true, false, "")
	if err != nil {
		t.Fatalf("failed to create client 2: %v", err)
	}
	client2.SetToken("test-token")

	s := &service{
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				"sys1": {SystemID: "sys1"},
				"sys2": {SystemID: "sys2"},
			},
		},
		adminClients: map[string]*sio.Client{
			"sys1": client1,
			"sys2": client2,
		},
		systems: map[string]*sio.System{
			"sys1": {},
			"sys2": {},
		},
		storagePoolIDToName: map[string]string{
			"sp1": "pool1",
			"sp2": "pool2",
		},
	}

	resp, err := s.ListVolumes(context.Background(), &csi.ListVolumesRequest{})
	if err != nil {
		t.Fatalf("ListVolumes returned error: %v", err)
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(resp.Entries))
	}

	ids := make(map[string]bool)
	for _, e := range resp.Entries {
		if e == nil || e.Volume == nil {
			t.Fatalf("expected non-nil entry, got %v", e)
		}
		ids[e.Volume.VolumeId] = true
	}
	if !ids["sys1-vol1"] || !ids["sys2-vol2"] {
		t.Fatalf("expected volume ids sys1-vol1 and sys2-vol2, got %v", ids)
	}

	// Verify pagination across the combined list.
	page1, err := s.ListVolumes(context.Background(), &csi.ListVolumesRequest{MaxEntries: 1})
	if err != nil {
		t.Fatalf("ListVolumes first page returned error: %v", err)
	}
	if len(page1.Entries) != 1 {
		t.Fatalf("expected 1 entry on first page, got %d", len(page1.Entries))
	}
	if page1.NextToken == "" {
		t.Fatalf("expected NextToken on first page")
	}

	page2, err := s.ListVolumes(context.Background(), &csi.ListVolumesRequest{MaxEntries: 1, StartingToken: page1.NextToken})
	if err != nil {
		t.Fatalf("ListVolumes second page returned error: %v", err)
	}
	if len(page2.Entries) != 1 {
		t.Fatalf("expected 1 entry on second page, got %d", len(page2.Entries))
	}
	if page2.NextToken != "" {
		t.Fatalf("expected empty NextToken on last page, got %s", page2.NextToken)
	}
}

func Test_service_ListVolumes_NegativeStartingToken(t *testing.T) {
	server := newListVolumesTestServer(t,
		`[{"id":"vol1","name":"vol1","sizeInKb":1048576,"storagePoolId":"sp1","ancestorVolumeId":"","creationTime":0}]`,
		`[{"id":"sys1","installId":"install-1"}]`,
		`[{"id":"sp1","name":"pool1"}]`)
	defer server.Close()

	client, err := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	client.SetToken("test-token")

	s := &service{
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				"sys1": {SystemID: "sys1"},
			},
		},
		adminClients:        map[string]*sio.Client{"sys1": client},
		systems:             map[string]*sio.System{"sys1": {}},
		storagePoolIDToName: map[string]string{"sp1": "pool1"},
	}

	_, err = s.ListVolumes(context.Background(), &csi.ListVolumesRequest{StartingToken: "-1"})
	if err == nil {
		t.Fatalf("expected error for negative StartingToken, got nil")
	}
}

func Test_service_ListVolumes_NegativeMaxEntries(t *testing.T) {
	server := newListVolumesTestServer(t,
		`[{"id":"vol1","name":"vol1","sizeInKb":1048576,"storagePoolId":"sp1","ancestorVolumeId":"","creationTime":0}]`,
		`[{"id":"sys1","installId":"install-1"}]`,
		`[{"id":"sp1","name":"pool1"}]`)
	defer server.Close()

	client, err := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	client.SetToken("test-token")

	s := &service{
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				"sys1": {SystemID: "sys1"},
			},
		},
		adminClients:        map[string]*sio.Client{"sys1": client},
		systems:             map[string]*sio.System{"sys1": {}},
		storagePoolIDToName: map[string]string{"sp1": "pool1"},
	}

	_, err = s.ListVolumes(context.Background(), &csi.ListVolumesRequest{MaxEntries: -5})
	if err == nil {
		t.Fatalf("expected error for negative MaxEntries, got nil")
	}
}

func Test_service_ListSnapshots_SkipsNilVolume(t *testing.T) {
	s := &service{
		opts: Opts{
			defaultSystemID: "sys1",
			arrays: map[string]*ArrayConnectionData{
				"sys1": {SystemID: "sys1"},
			},
		},
		adminClients: map[string]*sio.Client{"sys1": {}},
		systems:      map[string]*sio.System{"sys1": {}},
		snapCacheRWL: sync.RWMutex{},
		snapCache: []*siotypes.Volume{
			{ID: "vol0", Name: "vol0", SizeInKb: 1048576, StoragePoolID: "sp1"},
			nil,
		},
		snapCacheSystemID: "sys1",
	}

	resp, err := s.ListSnapshots(context.Background(), &csi.ListSnapshotsRequest{MaxEntries: 1, StartingToken: "1"})
	if err != nil {
		t.Fatalf("ListSnapshots returned error: %v", err)
	}
	if len(resp.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(resp.Entries))
	}
}
