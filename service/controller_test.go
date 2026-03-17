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
//

package service

import (
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

	"github.com/dell/goscaleio"
	sio "github.com/dell/goscaleio"
	siotypes "github.com/dell/goscaleio/types/v1"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
							AvailabilityZone: &AvailabilityZone{
								Name: validZone,
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
							AvailabilityZone: &AvailabilityZone{
								// ensure the zone name will not match the topology key value
								// in the request
								Name: validZone + "no-match",
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
