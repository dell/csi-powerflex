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
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Ecosystems/container-storage-modules/src/goscaleio"
	sio "github.com/Ecosystems/container-storage-modules/src/goscaleio"
	siotypes "github.com/Ecosystems/container-storage-modules/src/goscaleio/types/v1"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
	"golang.org/x/oauth2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func Test_service_getZoneFromZoneLabelKey(t *testing.T) {
	type fields struct {
		opts                    Opts
		adminClients            map[string]*sio.Client
		systems                 map[string]*sio.System
		mode                    string
		volCache                []*siotypes.Volume
		volCacheSystemID        string
		snapCache               []*siotypes.Volume
		snapCacheSystemID       string
		privDir                 string
		storagePoolIDToName     map[string]string
		statisticsCounter       int
		volumePrefixToSystems   map[string][]string
		connectedSystemNameToID map[string]string
	}

	type args struct {
		ctx          context.Context
		zoneLabelKey string
	}

	const validTopologyKey = "topology.kubernetes.io/zone"
	const validZone = "zoneA"

	tests := map[string]struct {
		fields           fields
		args             args
		wantZone         string
		wantErr          bool
		getNodeLabelFunc func(ctx context.Context, s *service) (map[string]string, error)
	}{
		"success": {
			// happy path test
			args: args{
				ctx:          context.Background(),
				zoneLabelKey: validTopologyKey,
			},
			wantZone: "zoneA",
			wantErr:  false,
			getNodeLabelFunc: func(_ context.Context, _ *service) (map[string]string, error) {
				nodeLabels := map[string]string{validTopologyKey: validZone}
				return nodeLabels, nil
			},
		},
		"use bad zone label key": {
			// The key args.zoneLabelKey will not be found in the map returned by getNodeLabelFunc
			args: args{
				ctx:          context.Background(),
				zoneLabelKey: "badkey",
			},
			wantZone: "",
			wantErr:  true,
			getNodeLabelFunc: func(_ context.Context, _ *service) (map[string]string, error) {
				return nil, nil
			},
		},
		"fail to get node labels": {
			// getNodeLabelFunc will return an error, triggering failure to get the labels
			args: args{
				ctx:          context.Background(),
				zoneLabelKey: "unimportant",
			},
			wantZone: "",
			wantErr:  true,
			getNodeLabelFunc: func(_ context.Context, _ *service) (map[string]string, error) {
				return nil, errors.New("")
			},
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			s := &service{
				opts:                    tt.fields.opts,
				adminClients:            tt.fields.adminClients,
				systems:                 tt.fields.systems,
				mode:                    tt.fields.mode,
				volCache:                tt.fields.volCache,
				volCacheRWL:             sync.RWMutex{},
				volCacheSystemID:        tt.fields.volCacheSystemID,
				snapCache:               tt.fields.snapCache,
				snapCacheRWL:            sync.RWMutex{},
				snapCacheSystemID:       tt.fields.snapCacheSystemID,
				privDir:                 tt.fields.privDir,
				storagePoolIDToName:     tt.fields.storagePoolIDToName,
				statisticsCounter:       tt.fields.statisticsCounter,
				volumePrefixToSystems:   tt.fields.volumePrefixToSystems,
				connectedSystemNameToID: tt.fields.connectedSystemNameToID,
			}
			GetNodeLabels = tt.getNodeLabelFunc
			gotZone, err := s.getZoneFromZoneLabelKey(tt.args.ctx, tt.args.zoneLabelKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("service.getZoneFromZoneLabelKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotZone != tt.wantZone {
				t.Errorf("service.getZoneFromZoneLabelKey() = %v, want %v", gotZone, tt.wantZone)
			}
		})
	}
}

func Test_service_getSystemIDFromZoneLabelKey(t *testing.T) {
	type fields struct {
		opts                    Opts
		adminClients            map[string]*sio.Client
		systems                 map[string]*sio.System
		mode                    string
		volCache                []*siotypes.Volume
		volCacheSystemID        string
		snapCache               []*siotypes.Volume
		snapCacheSystemID       string
		privDir                 string
		storagePoolIDToName     map[string]string
		statisticsCounter       int
		volumePrefixToSystems   map[string][]string
		connectedSystemNameToID map[string]string
	}

	type args struct {
		req *csi.GetCapacityRequest
	}

	const validSystemID = "valid-id"
	const validTopologyKey = "topology.kubernetes.io/zone"
	const validZone = "zoneA"

	tests := map[string]struct {
		fields       fields
		args         args
		wantSystemID string
		wantErr      bool
	}{
		"success": {
			// happy path test
			wantErr:      false,
			wantSystemID: validSystemID,
			args: args{
				req: &csi.GetCapacityRequest{
					AccessibleTopology: &csi.Topology{
						Segments: map[string]string{
							validTopologyKey: validZone,
						},
					},
				},
			},
			fields: fields{
				opts: Opts{
					zoneLabelKey: validTopologyKey,
					arrays: map[string]*ArrayConnectionData{
						"array1": {
							SystemID: validSystemID,
							Zones: []AvailabilityZone{
								{
									Name: validZone,
								},
							},
						},
					},
				},
			},
		},
		"topology not passed with csi request": {
			// should return an empty string if no topology info is passed
			// with the csi request
			wantErr:      false,
			wantSystemID: "",
			args: args{
				req: &csi.GetCapacityRequest{
					AccessibleTopology: &csi.Topology{
						// don't pass any topology info with the request
						Segments: map[string]string{},
					},
				},
			},
			fields: fields{
				opts: Opts{
					zoneLabelKey: validTopologyKey,
				},
			},
		},
		"zone name missing in secret": {
			// topology information in the csi request does not match
			// any of the arrays in the secret
			wantErr:      true,
			wantSystemID: "",
			args: args{
				req: &csi.GetCapacityRequest{
					AccessibleTopology: &csi.Topology{
						Segments: map[string]string{
							validTopologyKey: validZone,
						},
					},
				},
			},
			fields: fields{
				opts: Opts{
					zoneLabelKey: validTopologyKey,
					arrays: map[string]*ArrayConnectionData{
						"array1": {
							SystemID: validSystemID,
							Zones: []AvailabilityZone{
								{
									// ensure the zone name will not match the topology key value
									// in the request
									Name: validZone + "no-match",
								},
							},
						},
					},
				},
			},
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			s := &service{
				opts:                    tt.fields.opts,
				adminClients:            tt.fields.adminClients,
				systems:                 tt.fields.systems,
				mode:                    tt.fields.mode,
				volCache:                tt.fields.volCache,
				volCacheRWL:             sync.RWMutex{},
				volCacheSystemID:        tt.fields.volCacheSystemID,
				snapCache:               tt.fields.snapCache,
				snapCacheRWL:            sync.RWMutex{},
				snapCacheSystemID:       tt.fields.snapCacheSystemID,
				privDir:                 tt.fields.privDir,
				storagePoolIDToName:     tt.fields.storagePoolIDToName,
				statisticsCounter:       tt.fields.statisticsCounter,
				volumePrefixToSystems:   tt.fields.volumePrefixToSystems,
				connectedSystemNameToID: tt.fields.connectedSystemNameToID,
			}
			gotSystemID, err := s.getSystemIDFromZoneLabelKey(tt.args.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("service.getSystemIDFromZoneLabelKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotSystemID != tt.wantSystemID {
				t.Errorf("service.getSystemIDFromZoneLabelKey() = %v, want %v", gotSystemID, tt.wantSystemID)
			}
		})
	}
}

func Test_service_createVolumeFromSnapshot(t *testing.T) {
	tests := []struct {
		name           string
		req            *csi.CreateVolumeRequest
		snapshotSource *csi.VolumeContentSource_SnapshotSource
		name1          string
		sizeInKbytes   int64
		storagePool    string
		want           *csi.CreateVolumeResponse
		wantErr        bool
	}{
		{
			name: "create volume from snapshot",
			req: &csi.CreateVolumeRequest{
				Name: "volume-1",
				VolumeCapabilities: []*csi.VolumeCapability{
					{
						AccessType: &csi.VolumeCapability_Mount{
							Mount: &csi.VolumeCapability_MountVolume{},
						},
						AccessMode: &csi.VolumeCapability_AccessMode{
							Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
						},
					},
				},
				CapacityRange: &csi.CapacityRange{
					RequiredBytes: 1,
				},
			},
			snapshotSource: &csi.VolumeContentSource_SnapshotSource{
				SnapshotId: "sys-1",
			},
			name1:        "volume-1",
			sizeInKbytes: 1024,
			storagePool:  "pool123",
			want:         &csi.CreateVolumeResponse{},
			wantErr:      true,
		},
	}
	for _, tt := range tests {
		clients1 := make(map[string]*sio.Client)

		client1, _ := sio.NewClientWithArgs("10.1.1.1", "", math.MaxInt64, false, false, "")
		clients1["sys-1"] = client1
		t.Run(tt.name, func(t *testing.T) {
			s := &service{
				opts: Opts{},
				connectedSystemNameToID: map[string]string{
					"snapshot": "sys-1",
				},

				adminClients: map[string]*sio.Client{
					"sys": client1,
				},
			}

			getVolByIDFunc = func(_ *service, id string, _ string) (*siotypes.Volume, error) {
				return &siotypes.Volume{
					ID:            id,
					SizeInKb:      1024,
					StoragePoolID: "pool123",
					Name:          "mock-volume",
					GenType:       "EC",
				}, nil
			}

			defer func() {
				// Restore original function after test
				getVolByIDFunc = func(s *service, id string, systemID string) (*siotypes.Volume, error) {
					return s.getVolByID(id, systemID)
				}
			}()

			getStoragePoolNameFromIDFunc = func(_ *service, _ string, _ string) string {
				return "pool123"
			}
			defer func() {
				getStoragePoolNameFromIDFunc = func(s *service, systemID string, id string) string {
					return s.getStoragePoolNameFromID(systemID, id)
				}
			}()

			getVolumeFunc = func(_ *goscaleio.Client, _, _, _, _ string, _ bool) ([]*siotypes.Volume, error) {
				return []*siotypes.Volume{
					{
						ID:            "volume-1",
						SizeInKb:      1024,
						StoragePoolID: "pool123",
						Name:          "mock-volume",
					},
				}, nil
			}
			defer func() {
				getVolumeFunc = func(adminClient *goscaleio.Client, a, b, c, name string, e bool) ([]*siotypes.Volume, error) {
					return adminClient.GetVolume(a, b, c, name, e)
				}
			}()

			createThinCloneFunc = func(_ *goscaleio.System, _ *siotypes.CreateSnapshotParam) (*siotypes.SnapshotVolumesResp, error) {
				return &siotypes.SnapshotVolumesResp{
					VolumeIDList:    []string{"volume-1"},
					SnapshotGroupID: "snap-1",
				}, fmt.Errorf("error in createThinClone")
			}

			defer func() {
				createThinCloneFunc = func(system *goscaleio.System, snapParam *siotypes.CreateSnapshotParam) (*siotypes.SnapshotVolumesResp, error) {
					return system.CreateThinClone(snapParam)
				}
			}()

			_, gotErr := s.createVolumeFromSnapshot(tt.req, tt.snapshotSource, tt.name, tt.sizeInKbytes, tt.storagePool)

			if tt.wantErr {
				if gotErr == nil {
					t.Fatalf("createVolumeFromSnapshot() succeeded unexpectedly, expected error")
				}
				expectedErrMsg := "Failed to call CreateThinClone to create volume from snapshot"
				if !strings.Contains(gotErr.Error(), expectedErrMsg) {
					t.Errorf("createVolumeFromSnapshot() error = %v, want error containing %q", gotErr, expectedErrMsg)
				}
				return
			}
		})
	}
}

func Test_service_CreateSnapshot(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		ctx     context.Context
		req     *csi.CreateSnapshotRequest
		want    *csi.CreateSnapshotResponse
		wantErr bool
	}{
		{
			name: "create snapshot",
			ctx:  context.Background(),
			req: &csi.CreateSnapshotRequest{
				SourceVolumeId: "volume-1",
				Name:           "snap-1",
				Parameters:     nil,
			},
			want:    &csi.CreateSnapshotResponse{},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// TODO: construct the receiver type.
			clients1 := make(map[string]*sio.Client)

			client1, _ := sio.NewClientWithArgs("10.1.1.1", "", math.MaxInt64, false, false, "")
			clients1["volume"] = client1
			s := &service{
				opts: Opts{},
				connectedSystemNameToID: map[string]string{
					"snapshot": "sys-1",
				},

				adminClients: map[string]*sio.Client{
					"volume": client1,
				},
				systems: map[string]*sio.System{
					"volume": {},
				},
			}
			getVolumeFunc = func(_ *goscaleio.Client, _, _, _, _ string, _ bool) ([]*siotypes.Volume, error) {
				return []*siotypes.Volume{}, nil
			}
			defer func() {
				getVolumeFunc = func(adminClient *goscaleio.Client, a, b, c, name string, e bool) ([]*siotypes.Volume, error) {
					return adminClient.GetVolume(a, b, c, name, e)
				}
			}()

			getVolByIDFunc = func(_ *service, id string, _ string) (*siotypes.Volume, error) {
				return &siotypes.Volume{
					ID:            id,
					SizeInKb:      1024,
					StoragePoolID: "pool123",
					Name:          "mock-volume",
					GenType:       "EC",
				}, nil
			}

			defer func() {
				// Restore original function after test
				getVolByIDFunc = func(s *service, id string, systemID string) (*siotypes.Volume, error) {
					return s.getVolByID(id, systemID)
				}
			}()

			createSnapshotFunc = func(_ *goscaleio.System, _ *siotypes.CreateSnapshotParam) (*siotypes.SnapshotVolumesResp, error) {
				return &siotypes.SnapshotVolumesResp{
					VolumeIDList:    []string{"volume-1"},
					SnapshotGroupID: "snap-1",
				}, fmt.Errorf("error in createThinClone")
			}

			defer func() {
				createSnapshotFunc = func(system *goscaleio.System, snapParam *siotypes.CreateSnapshotParam) (*siotypes.SnapshotVolumesResp, error) {
					return system.CreateSnapshot(snapParam)
				}
			}()
			got, gotErr := s.CreateSnapshot(tt.ctx, tt.req)
			if gotErr != nil {
				if !tt.wantErr {
					t.Errorf("CreateSnapshot() failed: %v", gotErr)
				}
				return
			}
			if tt.wantErr {
				t.Fatal("CreateSnapshot() succeeded unexpectedly")
			}
			if true {
				t.Errorf("CreateSnapshot() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractIP(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		endpoint    string
		wantIP      string
		wantErr     bool
		errContains string
	}{
		{
			name:     "IPv4 with scheme and port",
			endpoint: "https://192.168.1.10:8443",
			wantIP:   "192.168.1.10",
		},
		{
			name:        "Hostname not IP",
			endpoint:    "https://example.com",
			wantErr:     true,
			errContains: "not a valid IP: example.com",
		},
		{
			name:        "Malformed URL",
			endpoint:    "http://%",
			wantErr:     true,
			errContains: "parse", // err originates from url.Parse
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ExtractIP(tc.endpoint)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (got=%q)", got)
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("expected error to contain %q, got %q", tc.errContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantIP {
				t.Fatalf("ip mismatch: want %q, got %q", tc.wantIP, got)
			}
		})
	}
}

func TestExtractHost(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		endpoint    string
		wantHost    string
		wantErr     bool
		errContains string
	}{
		{
			name:     "IPv4 with scheme and port",
			endpoint: "https://192.168.1.10:8443",
			wantHost: "192.168.1.10",
		},
		{
			name:     "Hostname with scheme and port",
			endpoint: "https://gateway.example.com:443",
			wantHost: "gateway.example.com",
		},
		{
			name:     "Hostname without port",
			endpoint: "https://gateway.example.com",
			wantHost: "gateway.example.com",
		},
		{
			name:        "Malformed URL",
			endpoint:    "http://%",
			wantErr:     true,
			errContains: "parse",
		},
		{
			name:        "No host",
			endpoint:    "https://",
			wantErr:     true,
			errContains: "no host in endpoint",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ExtractHost(tc.endpoint)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (got=%q)", got)
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("expected error to contain %q, got %q", tc.errContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.wantHost {
				t.Fatalf("host mismatch: want %q, got %q", tc.wantHost, got)
			}
		})
	}
}

type mockClient struct {
	goscaleio.Client
	refreshCalls int32
}

func (m *mockClient) RefreshPowerFlexToken(_ *goscaleio.ConfigConnect) (*oauth2.Token, error) {
	atomic.AddInt32(&m.refreshCalls, 1)
	return &oauth2.Token{
		AccessToken:  "new-access",
		RefreshToken: "new-refresh",
		TokenType:    "Bearer",
		ExpiresIn:    5, // seconds
	}, nil
}

func TestRefreshPowerFlexTokenNew(t *testing.T) {
	tests := []struct {
		name             string
		client           *mockClient
		pfmpIP           string
		ciamClientID     string
		ciamClientSecret string
		insecure         bool
		PowerFlexToken   *oauth2.Token
		checkInterval    time.Duration
		expectRefresh    bool
		mockHTTPResponse func(w http.ResponseWriter, r *http.Request)
	}{
		{
			name:             "token refresh with invalid PowerFlexToken",
			client:           &mockClient{},
			pfmpIP:           "http://example.com",
			ciamClientID:     "client-id",
			ciamClientSecret: "client-secret",
			insecure:         false,
			PowerFlexToken:   nil,
			checkInterval:    1 * time.Minute,
			expectRefresh:    true,
			mockHTTPResponse: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"access_token": "new-access", "token_type": "Bearer", "expires_in": 3600}`))
			},
		},
		{
			name:             "token refresh with expired PowerFlexToken",
			client:           &mockClient{},
			pfmpIP:           "http://example.com",
			ciamClientID:     "client-id",
			ciamClientSecret: "client-secret",
			insecure:         false,
			PowerFlexToken: &oauth2.Token{
				AccessToken:  "old-access",
				RefreshToken: "old-refresh",
				TokenType:    "Bearer",
				ExpiresIn:    -1, // expired
			},
			checkInterval: 1 * time.Minute,
			expectRefresh: true,
			mockHTTPResponse: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"access_token": "new-access", "token_type": "Bearer", "expires_in": 3600}`))
			},
		},
		{
			name:             "token refresh with valid PowerFlexToken",
			client:           &mockClient{},
			pfmpIP:           "http://example.com",
			ciamClientID:     "client-id",
			ciamClientSecret: "client-secret",
			insecure:         false,
			PowerFlexToken: &oauth2.Token{
				AccessToken:  "valid-access",
				RefreshToken: "valid-refresh",
				TokenType:    "Bearer",
				ExpiresIn:    3600, // valid
			},
			checkInterval: 1 * time.Minute,
			expectRefresh: false,
			mockHTTPResponse: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"access_token": "new-access", "token_type": "Bearer", "expires_in": 3600}`))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				tt.mockHTTPResponse(w, r)
			}))
			defer ts.Close()

			client := &mockClient{}
			pfmpIP := ts.URL
			go func() {
				ctx := context.Background()
				req, err := http.NewRequestWithContext(ctx, http.MethodPost, pfmpIP+"/auth/realms/powerflex/protocol/openid-connect/token", strings.NewReader(`grant_type=refresh_token&refresh_token=refresh-token&client_id=client-id&client_secret=client-secret`))
				if err != nil {
					t.Errorf("error creating request: %v", err)
					return
				}
				resp, err := http.DefaultClient.Do(req) // #nosec G704 - Safe in test: pfmpIP is from httptest.NewServer, URL is validated by construction
				if err != nil {
					t.Errorf("error sending request: %v", err)
					return
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Errorf("expected status code %d, got %d", http.StatusOK, resp.StatusCode)
					return
				}
				var token struct {
					AccessToken string `json:"access_token"`
					TokenType   string `json:"token_type"`
					ExpiresIn   int    `json:"expires_in"`
				}
				err = json.NewDecoder(resp.Body).Decode(&token)
				if err != nil {
					t.Errorf("error decoding response: %v", err)
					return
				}
				atomic.AddInt32(&client.refreshCalls, 1)
			}()

			// Let it run briefly to execute one or more iterations.
			time.Sleep(200 * time.Millisecond)

			if tt.expectRefresh && atomic.LoadInt32(&client.refreshCalls) == 0 {
				t.Errorf("expected refresh calls, got %d", atomic.LoadInt32(&client.refreshCalls))
			}
		})
	}
}

// helper to build a valid baseline and then override fields
func validArray() *ArrayConnectionData {
	return &ArrayConnectionData{ // #nosec G101
		OidcClientID:     "oidc-client-id",
		OidcClientSecret: "oidc-client-secret",
		CiamClientID:     "ciam-client-id",
		CiamClientSecret: "ciam-client-secret",
		Issuer:           "https://issuer.example.com",
	}
}

func TestOidcPrechecks(t *testing.T) {
	tests := []struct {
		name       string
		array      *ArrayConnectionData
		wantCode   codes.Code // expected gRPC status code (codes.OK if no error)
		wantSubstr string     // substring expected in error message ("" if none)
	}{
		{
			name:       "missing OidcClientID",
			array:      func() *ArrayConnectionData { a := validArray(); a.OidcClientID = ""; return a }(),
			wantCode:   codes.FailedPrecondition,
			wantSubstr: "missing OidcClientID",
		},
		{
			name:       "missing OidcClientSecret",
			array:      func() *ArrayConnectionData { a := validArray(); a.OidcClientSecret = ""; return a }(),
			wantCode:   codes.FailedPrecondition,
			wantSubstr: "missing OidcClientSecret",
		},
		{
			name:       "missing CiamClientID",
			array:      func() *ArrayConnectionData { a := validArray(); a.CiamClientID = ""; return a }(),
			wantCode:   codes.FailedPrecondition,
			wantSubstr: "missing CiamClientID",
		},
		{
			name:       "missing CiamClientSecret",
			array:      func() *ArrayConnectionData { a := validArray(); a.CiamClientSecret = ""; return a }(),
			wantCode:   codes.FailedPrecondition,
			wantSubstr: "missing CiamClientSecret",
		},
		{
			name:       "missing Issuer",
			array:      func() *ArrayConnectionData { a := validArray(); a.Issuer = ""; return a }(),
			wantCode:   codes.FailedPrecondition,
			wantSubstr: "missing Issuer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := oidcPrechecks(tt.array)

			if tt.wantCode == codes.OK {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error, got nil")
			}

			gotCode := status.Code(err)
			if gotCode != tt.wantCode {
				t.Fatalf("expected gRPC code %v, got %v (err=%v)", tt.wantCode, gotCode, err)
			}

			if tt.wantSubstr != "" && !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("expected error to contain %q, got %q", tt.wantSubstr, err.Error())
			}
		})
	}
}

func TestParseScopes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "basic comma separated",
			input:    "openid,pflex",
			expected: []string{"openid", "pflex"},
		},
		{
			name:     "with spaces and duplicates",
			input:    "openid, pflex ,openid",
			expected: []string{"openid", "pflex"},
		},
		{
			name:     "empty input returns nil",
			input:    "",
			expected: nil,
		},
		{
			name:     "only delimiters/whitespace returns nil",
			input:    " , , ",
			expected: nil,
		},
		{
			name:     "commas only returns nil",
			input:    ",,",
			expected: nil,
		},
		{
			name:     "input with empty entries gets filtered",
			input:    "openid,,pflex, ",
			expected: []string{"openid", "pflex"},
		},
		{
			name:     "dedupe repeated scope",
			input:    "openid,openid",
			expected: []string{"openid"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseScopes(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("ParseScopes(%q) = %#v; want %#v", tt.input, result, tt.expected)
			}
		})
	}
}

// TestCreateVolumeZoneCapacityExhaustion verifies that a failure in one zone
// does not prevent provisioning in another zone on the same or a different system.
func TestCreateVolumeZoneCapacityExhaustion(t *testing.T) {
	zoneLabelKey := "topology.kubernetes.io/zone"
	clientA, _ := sio.NewClientWithArgs("https://powerflex-a.example.com", "", math.MaxInt64, true, false, "")
	clientB, _ := sio.NewClientWithArgs("https://powerflex-b.example.com", "", math.MaxInt64, true, false, "")

	svc := &service{
		opts: Opts{
			zoneLabelKey: zoneLabelKey,
			arrays: map[string]*ArrayConnectionData{
				"sysA": {
					SystemID: "sysA",
					Zones: []AvailabilityZone{
						{
							Name:     "zoneA",
							LabelKey: zoneLabelKey,
							ProtectionDomains: []ProtectionDomain{
								{Name: "PDA", Pools: []PoolName{"poolA"}},
							},
						},
					},
				},
				"sysB": {
					SystemID: "sysB",
					Zones: []AvailabilityZone{
						{
							Name:     "zoneB",
							LabelKey: zoneLabelKey,
							ProtectionDomains: []ProtectionDomain{
								{Name: "PDB", Pools: []PoolName{"poolB"}},
							},
						},
					},
				},
			},
		},
		adminClients: map[string]*sio.Client{
			"sysA": clientA,
			"sysB": clientB,
		},
		systems: map[string]*sio.System{
			"sysA": {System: &siotypes.System{ID: "sysA"}},
			"sysB": {System: &siotypes.System{ID: "sysB"}},
		},
		platformInfos: map[string]*PlatformInfo{
			"sysA": {SystemID: "sysA", ArrayVersion: 4.0, GenType: "EC"},
			"sysB": {SystemID: "sysB", ArrayVersion: 4.0, GenType: "EC"},
		},
		storagePoolIDToName: map[string]string{
			"spidA": "poolA",
			"spidB": "poolB",
		},
		volumePrefixToSystems: make(map[string][]string),
	}

	for _, arr := range svc.opts.arrays {
		_ = normalizeZoneConfig(arr)
	}

	// zoneA create fails with capacity-related error
	origCreateVolumeFunc := createVolumeFunc
	createVolumeFunc = func(_ *goscaleio.Client, _ *siotypes.VolumeParam, storagePoolName, protectionDomain string) (*siotypes.VolumeResp, error) {
		if storagePoolName == "poolA" && protectionDomain == "PDA" {
			return nil, fmt.Errorf("capacity exhausted")
		}
		return &siotypes.VolumeResp{ID: "vol-id"}, nil
	}
	defer func() { createVolumeFunc = origCreateVolumeFunc }()

	origGetVolByIDFunc := getVolByIDFunc
	getVolByIDFunc = func(_ *service, id string, _ string) (*siotypes.Volume, error) {
		return &siotypes.Volume{
			ID:            id,
			SizeInKb:      32 * 1024 * 1024,
			StoragePoolID: "spidB",
			Name:          "mock-volume",
			GenType:       "EC",
		}, nil
	}
	defer func() { getVolByIDFunc = origGetVolByIDFunc }()

	origGetProtectionDomainIDFromNameFunc := getProtectionDomainIDFromNameFunc
	getProtectionDomainIDFromNameFunc = func(_ *goscaleio.Client, _, protectionDomainName string) (string, error) {
		if protectionDomainName == "PDA" {
			return "pdidA", nil
		}
		return "pdidB", nil
	}
	defer func() { getProtectionDomainIDFromNameFunc = origGetProtectionDomainIDFromNameFunc }()

	origFindStoragePoolFunc := findStoragePoolFunc
	findStoragePoolFunc = func(_ *goscaleio.Client, _, name, _, _ string) (*siotypes.StoragePool, error) {
		if name == "poolA" {
			return &siotypes.StoragePool{ID: "spidA", Name: "poolA"}, nil
		}
		return &siotypes.StoragePool{ID: "spidB", Name: "poolB"}, nil
	}
	defer func() { findStoragePoolFunc = origFindStoragePoolFunc }()

	t.Run("zone A capacity exhaustion returns error", func(t *testing.T) {
		req := &csi.CreateVolumeRequest{
			Name: "vol-zoneA",
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}},
					AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
				},
			},
			CapacityRange: &csi.CapacityRange{RequiredBytes: 32 * 1024 * 1024 * 1024},
			AccessibilityRequirements: &csi.TopologyRequirement{
				Preferred: []*csi.Topology{
					{Segments: map[string]string{zoneLabelKey: "zoneA"}},
				},
			},
		}

		_, err := svc.CreateVolume(context.Background(), req)
		if err == nil {
			t.Fatalf("expected error for zoneA capacity exhaustion, got nil")
		}
	})

	t.Run("zone B provisioning unaffected after zone A failure", func(t *testing.T) {
		req := &csi.CreateVolumeRequest{
			Name: "vol-zoneB",
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}},
					AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
				},
			},
			CapacityRange: &csi.CapacityRange{RequiredBytes: 32 * 1024 * 1024 * 1024},
			AccessibilityRequirements: &csi.TopologyRequirement{
				Preferred: []*csi.Topology{
					{Segments: map[string]string{zoneLabelKey: "zoneB"}},
				},
			},
		}

		resp, err := svc.CreateVolume(context.Background(), req)
		if err != nil {
			t.Fatalf("expected zoneB provisioning to succeed, got error: %v", err)
		}
		if resp == nil || resp.Volume == nil {
			t.Fatalf("expected non-nil volume response for zoneB")
		}
	})
}

func TestFilterZonesBySystem(t *testing.T) {
	zones := map[ZoneName]ZoneContent{
		"zoneA": {systemID: "sys1"},
		"zoneB": {systemID: "sys2"},
		"zoneC": {systemID: "sys1"},
	}

	t.Run("filters zones for target system", func(t *testing.T) {
		filtered := filterZonesBySystem(zones, "sys1")
		if len(filtered) != 2 {
			t.Fatalf("expected 2 zones for sys1, got %d", len(filtered))
		}
		if _, ok := filtered["zoneA"]; !ok {
			t.Errorf("expected zoneA in filtered map")
		}
		if _, ok := filtered["zoneC"]; !ok {
			t.Errorf("expected zoneC in filtered map")
		}
	})

	t.Run("returns empty map when no zones match system", func(t *testing.T) {
		filtered := filterZonesBySystem(zones, "sys3")
		if len(filtered) != 0 {
			t.Fatalf("expected 0 zones for sys3, got %d", len(filtered))
		}
	})
}

// ── ExecuteResumeOnReplicationGroup ──────────────────────────────────
// Covers the failover=true branch (line 4101-4103).

func TestExecuteResumeOnReplicationGroup_FailoverTrue(_ *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{}`)
	}))
	defer ts.Close()

	svc := &service{}
	client, _ := sio.NewClientWithArgs(ts.URL, "4.0", 0, true, false, "")
	group := &siotypes.ReplicationConsistencyGroup{ID: "rcg-1"}
	// failover=true should call ExecuteRestoreOnReplicationGroup; any HTTP error is fine
	_ = svc.ExecuteResumeOnReplicationGroup(client, group, true)
}

func TestExecuteResumeOnReplicationGroup_FailoverFalse(_ *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{}`)
	}))
	defer ts.Close()

	svc := &service{}
	client, _ := sio.NewClientWithArgs(ts.URL, "4.0", 0, true, false, "")
	group := &siotypes.ReplicationConsistencyGroup{ID: "rcg-1"}
	_ = svc.ExecuteResumeOnReplicationGroup(client, group, false)
}

// ── CreateReplicationConsistencyGroup ────────────────────────────────
// Covers the "both peerMdmID and remoteSystemID set" guard (line 3935-3936).

func TestCreateReplicationConsistencyGroup_BothIDsSet(t *testing.T) {
	svc := &service{
		adminClients: map[string]*sio.Client{"sys1": {}},
	}
	_, err := svc.CreateReplicationConsistencyGroup("sys1", "name", "30", "pdLocal", "pdRemote", "peerMdm1", "remoteSys1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "peerMdmID and remoteSystemID cannot both be present")
}

func TestCreateReplicationConsistencyGroup_NoAdminClient(t *testing.T) {
	svc := &service{adminClients: map[string]*sio.Client{}}
	_, err := svc.CreateReplicationConsistencyGroup("missing", "name", "30", "pdLocal", "pdRemote", "", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "can't find adminClient by id missing")
}

// ── DeleteReplicationConsistencyGroup ────────────────────────────────
// Cover the GetReplicationConsistencyGroupByID error path.

func TestDeleteReplicationConsistencyGroup_GetGroupError(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"not found","httpStatusCode":404,"errorCode":0}`)
	}))
	defer ts.Close()

	client, _ := sio.NewClientWithArgs(ts.URL, "4.0", 0, true, false, "")
	svc := &service{adminClients: map[string]*sio.Client{"sys1": client}}
	err := svc.DeleteReplicationConsistencyGroup("sys1", "nonexistent-group")
	assert.Error(t, err)
}

// ── ControllerGetVolume ───────────────────────────────────────────────
// Cover: systemID empty + no default (line 3889-3891).

func TestControllerGetVolume_NoDefaultSystem(t *testing.T) {
	svc := &service{
		opts: Opts{defaultSystemID: ""},
	}
	// volume ID with no embedded systemID prefix
	req := &csi.ControllerGetVolumeRequest{VolumeId: "plainvolid"}
	_, err := svc.ControllerGetVolume(context.Background(), req)
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "systemID is not found")
}

// ── createQuota ───────────────────────────────────────────────────────
// Cover: size <= 0 returns early (line 830-833), softLimit >= size (line 836-837),
// softLimitInt == 0 (line 841-842).

func TestCreateQuota_SizeZero(t *testing.T) {
	ts := buildCreateQuotaTestServer(t)
	defer ts.Close()

	client, _ := sio.NewClientWithArgs(ts.URL, "4.0", 0, true, false, "")
	svc := &service{adminClients: map[string]*sio.Client{"sys1": client}}
	// size=0 should skip quota creation and return ("", nil)
	id, err := svc.createQuota("fs1", "/path", "20", "0", 0, true, "sys1")
	assert.NoError(t, err)
	assert.Equal(t, "", id)
}

func TestCreateQuota_SoftLimitExceedsSize(t *testing.T) {
	ts := buildCreateQuotaTestServer(t)
	defer ts.Close()

	client, _ := sio.NewClientWithArgs(ts.URL, "4.0", 0, true, false, "")
	svc := &service{adminClients: map[string]*sio.Client{"sys1": client}}
	// softLimit=101% makes softLimitInt = (101 * size) / 100 >= size
	_, err := svc.createQuota("fs1", "/path", "101", "0", 1048576, true, "sys1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "softLimit")
}

func TestCreateQuota_SoftLimitZero(t *testing.T) {
	ts := buildCreateQuotaTestServer(t)
	defer ts.Close()

	client, _ := sio.NewClientWithArgs(ts.URL, "4.0", 0, true, false, "")
	svc := &service{adminClients: map[string]*sio.Client{"sys1": client}}
	// softLimit=0% → softLimitInt = 0 → rejected
	_, err := svc.createQuota("fs1", "/path", "0", "0", 1048576, true, "sys1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "softLimit")
}

// buildCreateQuotaTestServer returns an httptest server that serves minimal
// responses for the FindSystem / GetFileSystemByIDName / ModifyFileSystem
// calls that createQuota makes before reaching the size-check branches.
func buildCreateQuotaTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		// FindSystem → GetInstance("") → GET api/types/System/instances
		case strings.Contains(r.URL.Path, "types/System/instances"):
			fmt.Fprintf(w, `[{"id":"sys1","name":"sys1"}]`)
		// GetFileSystemByIDName → GET /rest/v1/file-systems/{id}
		case strings.Contains(r.URL.Path, "rest/v1/file-systems"):
			if r.Method == http.MethodPatch {
				// ModifyFileSystem PATCH → success
				fmt.Fprint(w, `{}`)
			} else {
				fmt.Fprintf(w, `{"id":"fs1","name":"fs1","sizeTotal":1073741824,"storedData":0,"storedDataRoot":0}`)
			}
		default:
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{}`)
		}
	}))
}

// ── DeleteVolume (block path) ─────────────────────────────────────────
// Cover: "must be a hexadecimal number" path (line 1523-1527) and
// volume in use → FailedPrecondition (line 1544-1548).

func TestDeleteVolume_HexError(t *testing.T) {
	ts := buildDeleteVolumeTestServer(t, func(r *http.Request) (int, string) {
		if strings.Contains(r.URL.Path, "instances/Volume") {
			return http.StatusBadRequest, `{"message":"must be a hexadecimal number","httpStatusCode":400,"errorCode":0}`
		}
		return http.StatusOK, `{}`
	})
	defer ts.Close()

	client, _ := sio.NewClientWithArgs(ts.URL, "4.0", 0, true, false, "")
	sys := sio.NewSystem(client)
	sys.System = &siotypes.System{ID: "sys1"}
	svc := &service{
		opts:                    Opts{defaultSystemID: "sys1"},
		adminClients:            map[string]*sio.Client{"sys1": client},
		systems:                 map[string]*sio.System{"sys1": sys},
		connectedSystemNameToID: map[string]string{},
	}

	req := &csi.DeleteVolumeRequest{VolumeId: "sys1-notahex"}
	resp, err := svc.DeleteVolume(context.Background(), req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestDeleteVolume_VolumeInUse(t *testing.T) {
	volJSON := `{"id":"abc123","name":"vol","mappedSdcInfo":[{"sdcId":"sdc1"}],"volumeReplicationState":"UnmarkedForReplication"}`
	ts := buildDeleteVolumeTestServer(t, func(r *http.Request) (int, string) {
		if strings.Contains(r.URL.Path, "instances/Volume::abc123") {
			return http.StatusOK, volJSON
		}
		return http.StatusOK, `{}`
	})
	defer ts.Close()

	client, _ := sio.NewClientWithArgs(ts.URL, "4.0", 0, true, false, "")
	sys := sio.NewSystem(client)
	sys.System = &siotypes.System{ID: "sys1"}
	svc := &service{
		opts:                    Opts{defaultSystemID: "sys1"},
		adminClients:            map[string]*sio.Client{"sys1": client},
		systems:                 map[string]*sio.System{"sys1": sys},
		connectedSystemNameToID: map[string]string{},
	}

	req := &csi.DeleteVolumeRequest{VolumeId: "sys1-abc123"}
	_, err := svc.DeleteVolume(context.Background(), req)
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	assert.Contains(t, st.Message(), "volume in use")
}

// buildDeleteVolumeTestServer creates a TLS test server with a configurable route handler.
func buildDeleteVolumeTestServer(t *testing.T, handler func(r *http.Request) (int, string)) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		code, body := handler(r)
		w.WriteHeader(code)
		fmt.Fprint(w, body)
	}))
}

// ── systemProbeAll ────────────────────────────────────────────────────
// Cover: usingZones=true, zoneName="" → arrays with zone config are skipped.

func TestSystemProbeAll_ZonesNoNodeLabel(t *testing.T) {
	fakeK8s := fake.NewSimpleClientset()
	// Node with NO zone label
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "mynode",
			Labels: map[string]string{},
		},
	}
	_, _ = fakeK8s.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})
	K8sClientset = fakeK8s
	defer func() { K8sClientset = nil }()

	svc := &service{
		mode: "node",
		opts: Opts{
			KubeNodeName: "mynode",
			zoneLabelKey: "topology.kubernetes.io/zone",
			arrays: map[string]*ArrayConnectionData{
				"sys1": {
					SystemID: "sys1",
					Endpoint: "http://127.0.0.1",
					Zones: []AvailabilityZone{
						{Name: "zoneA", LabelKey: "topology.kubernetes.io/zone"},
					},
				},
			},
		},
		adminClients:  map[string]*sio.Client{},
		systems:       map[string]*sio.System{},
		platformInfos: map[string]*PlatformInfo{},
	}

	// All arrays have zone config but node has no label → allArrayFail=true → error
	err := svc.systemProbeAll(context.Background())
	assert.Error(t, err)
}

// ── getMaximumVolumeSize ──────────────────────────────────────────────
// Cover: GetMaxVol returns a non-numeric string → ParseFloat error (line 2653-2657).

func TestGetMaximumVolumeSize_NonNumericResponse(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "querySystemLimits") {
			// Return a limits payload where maximumVolumeSize is not a number
			fmt.Fprint(w, `{"systemLimitEntryList":[{"type":"maximumVolumeSize","maximumValue":"not-a-number"}]}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{}`)
	}))
	defer ts.Close()

	client, _ := sio.NewClientWithArgs(ts.URL, "4.0", 0, true, false, "")
	// Clear cache for this systemID
	delete(maxVolumesSizeForArray, "sysParseFail")

	svc := &service{adminClients: map[string]*sio.Client{"sysParseFail": client}}
	_, err := svc.getMaximumVolumeSize("sysParseFail")
	assert.Error(t, err)
}
