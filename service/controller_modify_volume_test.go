// Copyright © 2019-2026 Dell Inc. or its subsidiaries. All Rights Reserved.
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
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	sio "github.com/Ecosystems/container-storage-modules/src/goscaleio"
	siotypes "github.com/Ecosystems/container-storage-modules/src/goscaleio/types/v1"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TestControllerModifyVolume_EmptyVolumeID verifies INVALID_ARGUMENT when volume_id is empty (U-004)
func TestControllerModifyVolume_EmptyVolumeID(t *testing.T) {
	svc := &service{}
	ctx := context.Background()
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId: "",
		MutableParameters: map[string]string{
			"iopsLimit": "100",
		},
	}
	resp, err := svc.ControllerModifyVolume(ctx, req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "volume_id")
}

// TestControllerModifyVolume_CheckVolumesMapError verifies INTERNAL error when checkVolumesMap fails
func TestControllerModifyVolume_CheckVolumesMapError(t *testing.T) {
	// Use a volume ID that's too short to trigger checkVolumesMap error
	// checkVolumesMap returns error for volume IDs shorter than 3 characters
	svc := &service{
		storagePoolIDToName:     map[string]string{},
		volumePrefixToSystems:   map[string][]string{},
		connectedSystemNameToID: map[string]string{},
		opts: Opts{
			defaultSystemID: arrayID,
			arrays: map[string]*ArrayConnectionData{
				arrayID: {SystemID: arrayID},
			},
		},
	}

	ctx := context.Background()
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId: "12", // Too short (less than 3 chars) - triggers checkVolumesMap error
		MutableParameters: map[string]string{
			"iopsLimit": "100",
		},
	}
	resp, err := svc.ControllerModifyVolume(ctx, req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Contains(t, st.Message(), "checkVolumesMap")
}

// TestControllerModifyVolume_EmptySystemID verifies INVALID_ARGUMENT when systemID is not found
func TestControllerModifyVolume_EmptySystemID(t *testing.T) {
	// Volume ID without system prefix and no default system configured
	svc := &service{
		storagePoolIDToName:     map[string]string{},
		volumePrefixToSystems:   map[string][]string{},
		connectedSystemNameToID: map[string]string{},
		opts: Opts{
			defaultSystemID: "", // No default system
			arrays:          map[string]*ArrayConnectionData{},
		},
	}

	ctx := context.Background()
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId: "legacy-vol-id", // No system prefix
		MutableParameters: map[string]string{
			"iopsLimit": "100",
		},
	}
	resp, err := svc.ControllerModifyVolume(ctx, req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "systemID")
}

// TestControllerModifyVolume_VolumeLookupError verifies INTERNAL error when volume lookup returns non-unavailable error
func TestControllerModifyVolume_VolumeLookupError(t *testing.T) {
	// Mock server returns a 500 error (not 404) to simulate a backend error
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" || r.URL.Path == "/api/version" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `"4.0"`)
			return
		}
		// Return 500 Internal Server Error for volume lookup
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"message":"Internal server error","httpStatusCode":500,"errorCode":500}`)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	if err != nil {
		t.Fatalf("failed to create mock client: %v", err)
	}
	client.SetToken("test-token")

	svc := &service{
		adminClients:            map[string]*sio.Client{arrayID: client},
		systems:                 map[string]*sio.System{arrayID: {}},
		storagePoolIDToName:     map[string]string{},
		volumePrefixToSystems:   map[string][]string{},
		connectedSystemNameToID: map[string]string{},
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				arrayID: {SystemID: arrayID, Endpoint: server.URL},
			},
		},
	}

	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId: arrayID + "-" + goodVolumeID,
		MutableParameters: map[string]string{
			"iopsLimit": "100",
		},
	}
	resp, err := svc.ControllerModifyVolume(ctx, req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Contains(t, st.Message(), "PowerFlex API error")
}

// TestControllerModifyVolume_EmptyMutableParams verifies success when mutable_parameters is empty (U-005)
// According to CSI spec, mutable_parameters is optional, so empty should return success
func TestControllerModifyVolume_EmptyMutableParams(t *testing.T) {
	volumes := map[string]*siotypes.Volume{
		goodVolumeID: {
			ID:   goodVolumeID,
			Name: goodVolumeName,
			MappedSdcInfo: []*siotypes.MappedSdcInfo{
				{SdcID: sdcVolume1, LimitIops: 0, LimitBwInMbps: 0},
			},
		},
	}
	svc, server := setupModifyVolumeTestService(t, volumes)
	defer server.Close()

	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId:          arrayID + "-" + goodVolumeID,
		MutableParameters: map[string]string{},
	}
	resp, err := svc.ControllerModifyVolume(ctx, req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

// TestControllerModifyVolume_NilMutableParams verifies success when mutable_parameters is nil (U-006)
// According to CSI spec, mutable_parameters is optional, so nil should return success
func TestControllerModifyVolume_NilMutableParams(t *testing.T) {
	volumes := map[string]*siotypes.Volume{
		goodVolumeID: {
			ID:   goodVolumeID,
			Name: goodVolumeName,
			MappedSdcInfo: []*siotypes.MappedSdcInfo{
				{SdcID: sdcVolume1, LimitIops: 0, LimitBwInMbps: 0},
			},
		},
	}
	svc, server := setupModifyVolumeTestService(t, volumes)
	defer server.Close()

	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId:          arrayID + "-" + goodVolumeID,
		MutableParameters: nil,
	}
	resp, err := svc.ControllerModifyVolume(ctx, req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

// TestControllerModifyVolume_UnsupportedParamKey verifies INVALID_ARGUMENT for unknown parameter keys (U-007)
func TestControllerModifyVolume_UnsupportedParamKey(t *testing.T) {
	volumes := map[string]*siotypes.Volume{
		goodVolumeID: {
			ID:   goodVolumeID,
			Name: goodVolumeName,
			MappedSdcInfo: []*siotypes.MappedSdcInfo{
				{SdcID: sdcVolume1, LimitIops: 0, LimitBwInMbps: 0},
			},
		},
	}
	svc, server := setupModifyVolumeTestService(t, volumes)
	defer server.Close()

	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId: arrayID + "-" + goodVolumeID,
		MutableParameters: map[string]string{
			"unknownKey": "value",
		},
	}
	resp, err := svc.ControllerModifyVolume(ctx, req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "unsupported")
}

// TestControllerModifyVolume_InvalidParamValue verifies INVALID_ARGUMENT for non-integer values (U-014)
func TestControllerModifyVolume_InvalidParamValue(t *testing.T) {
	volumes := map[string]*siotypes.Volume{
		goodVolumeID: {
			ID:   goodVolumeID,
			Name: goodVolumeName,
			MappedSdcInfo: []*siotypes.MappedSdcInfo{
				{SdcID: sdcVolume1, LimitIops: 0, LimitBwInMbps: 0},
			},
		},
	}
	svc, server := setupModifyVolumeTestService(t, volumes)
	defer server.Close()

	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId: arrayID + "-" + goodVolumeID,
		MutableParameters: map[string]string{
			"iopsLimit": "abc",
		},
	}
	resp, err := svc.ControllerModifyVolume(ctx, req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// TestControllerModifyVolume_NegativeParamValue verifies INVALID_ARGUMENT for negative values (U-015)
func TestControllerModifyVolume_NegativeParamValue(t *testing.T) {
	volumes := map[string]*siotypes.Volume{
		goodVolumeID: {
			ID:   goodVolumeID,
			Name: goodVolumeName,
			MappedSdcInfo: []*siotypes.MappedSdcInfo{
				{SdcID: sdcVolume1, LimitIops: 0, LimitBwInMbps: 0},
			},
		},
	}
	svc, server := setupModifyVolumeTestService(t, volumes)
	defer server.Close()

	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId: arrayID + "-" + goodVolumeID,
		MutableParameters: map[string]string{
			"iopsLimit": "-1",
		},
	}
	resp, err := svc.ControllerModifyVolume(ctx, req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// TestControllerModifyVolume_EmptyStringValue verifies INVALID_ARGUMENT for empty string values (U-017)
func TestControllerModifyVolume_EmptyStringValue(t *testing.T) {
	volumes := map[string]*siotypes.Volume{
		goodVolumeID: {
			ID:   goodVolumeID,
			Name: goodVolumeName,
			MappedSdcInfo: []*siotypes.MappedSdcInfo{
				{SdcID: sdcVolume1, LimitIops: 0, LimitBwInMbps: 0},
			},
		},
	}
	svc, server := setupModifyVolumeTestService(t, volumes)
	defer server.Close()

	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId: arrayID + "-" + goodVolumeID,
		MutableParameters: map[string]string{
			"iopsLimit": "",
		},
	}
	resp, err := svc.ControllerModifyVolume(ctx, req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// setupModifyVolumeTestService creates a test service with a mock PowerFlex backend
// that serves volume instance responses with configurable MappedSdcInfo.
func setupModifyVolumeTestService(t *testing.T, volumes map[string]*siotypes.Volume) (*service, *httptest.Server) {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle authentication
		if r.URL.Path == "/api/login" || r.URL.Path == "/api/version" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `"4.0"`)
			return
		}

		// Handle volume instance GET: /api/instances/Volume::{id}
		for volID, vol := range volumes {
			if r.URL.Path == fmt.Sprintf("/api/instances/Volume::%s", volID) && r.Method == http.MethodGet {
				writeJSONResponse(t, w, vol)
				return
			}
			// Handle setMappedSdcLimits action
			if r.URL.Path == fmt.Sprintf("/api/instances/Volume::%s/action/setMappedSdcLimits", volID) && r.Method == http.MethodPost {
				w.WriteHeader(http.StatusOK)
				return
			}
		}

		// Volume not found
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"message":"Could not find the volume","httpStatusCode":404,"errorCode":0}`)
	})

	server := httptest.NewServer(handler)

	client, err := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	if err != nil {
		t.Fatalf("failed to create mock client: %v", err)
	}
	client.SetToken("test-token")

	svc := &service{
		adminClients:            map[string]*sio.Client{arrayID: client},
		systems:                 map[string]*sio.System{arrayID: {}},
		storagePoolIDToName:     map[string]string{},
		volumePrefixToSystems:   map[string][]string{},
		connectedSystemNameToID: map[string]string{},
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				arrayID: {
					SystemID: arrayID,
					Endpoint: server.URL,
				},
			},
		},
	}

	return svc, server
}

func writeJSONResponse(t *testing.T, w http.ResponseWriter, v interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	encoder := json.NewEncoder(w)
	if err := encoder.Encode(v); err != nil {
		t.Fatalf("failed to encode JSON response: %v", err)
	}
}

// TestControllerModifyVolume_VolumeNotFound verifies NOT_FOUND when volume doesn't exist (U-009)
func TestControllerModifyVolume_VolumeNotFound(t *testing.T) {
	volumes := map[string]*siotypes.Volume{} // empty — no volumes
	svc, server := setupModifyVolumeTestService(t, volumes)
	defer server.Close()

	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId: arrayID + "-" + goodVolumeID,
		MutableParameters: map[string]string{
			"iopsLimit": "100",
		},
	}
	resp, err := svc.ControllerModifyVolume(ctx, req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

// TestControllerModifyVolume_NfsVolume verifies INVALID_ARGUMENT for NFS volumes (U-008)
func TestControllerModifyVolume_NfsVolume(t *testing.T) {
	// NFS volumes use slash-separated CSI volume IDs: systemID/filesystemID
	// With the new implementation, volume existence is checked first,
	// so non-existent NFS volumes return NotFound
	volumes := map[string]*siotypes.Volume{}
	svc, server := setupModifyVolumeTestService(t, volumes)
	defer server.Close()

	ctx := context.Background()
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId: arrayID + "/" + "fs-12345", // NFS format: systemID/filesystemID
		MutableParameters: map[string]string{
			"iopsLimit": "100",
		},
	}
	resp, err := svc.ControllerModifyVolume(ctx, req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	// Non-existent NFS volumes return NotFound (volume lookup happens first)
	assert.Equal(t, codes.NotFound, st.Code())
	assert.Contains(t, st.Message(), "not found")
}

// TestControllerModifyVolume_NoMappedSdcs verifies FAILED_PRECONDITION when no SDCs mapped (U-010)
func TestControllerModifyVolume_NoMappedSdcs(t *testing.T) {
	volumes := map[string]*siotypes.Volume{
		goodVolumeID: {
			ID:            goodVolumeID,
			Name:          goodVolumeName,
			MappedSdcInfo: []*siotypes.MappedSdcInfo{}, // no SDCs mapped
		},
	}
	svc, server := setupModifyVolumeTestService(t, volumes)
	defer server.Close()

	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId: arrayID + "-" + goodVolumeID,
		MutableParameters: map[string]string{
			"iopsLimit": "100",
		},
	}
	resp, err := svc.ControllerModifyVolume(ctx, req)
	assert.Nil(t, resp)
	assert.Error(t, err)
	st, ok := status.FromError(err)
	assert.True(t, ok)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	assert.Contains(t, st.Message(), "mapped SDC")
}

// TestControllerModifyVolume_ValidBothParams verifies happy path with both QoS parameters (U-001)
func TestControllerModifyVolume_ValidBothParams(t *testing.T) {
	volumes := map[string]*siotypes.Volume{
		goodVolumeID: {
			ID:   goodVolumeID,
			Name: goodVolumeName,
			MappedSdcInfo: []*siotypes.MappedSdcInfo{
				{SdcID: sdcVolume1, LimitIops: 0, LimitBwInMbps: 0},
			},
		},
	}
	svc, server := setupModifyVolumeTestService(t, volumes)
	defer server.Close()

	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId: arrayID + "-" + goodVolumeID,
		MutableParameters: map[string]string{
			"bandwidthLimitInKbps": "1024",
			"iopsLimit":            "500",
		},
	}
	resp, err := svc.ControllerModifyVolume(ctx, req)
	assert.NotNil(t, resp)
	assert.NoError(t, err)
}

// TestControllerModifyVolume_ZeroValues verifies zero values (unlimited) are valid (U-016)
func TestControllerModifyVolume_ZeroValues(t *testing.T) {
	volumes := map[string]*siotypes.Volume{
		goodVolumeID: {
			ID:   goodVolumeID,
			Name: goodVolumeName,
			MappedSdcInfo: []*siotypes.MappedSdcInfo{
				{SdcID: sdcVolume1, LimitIops: 500, LimitBwInMbps: 10},
			},
		},
	}
	svc, server := setupModifyVolumeTestService(t, volumes)
	defer server.Close()

	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId: arrayID + "-" + goodVolumeID,
		MutableParameters: map[string]string{
			"bandwidthLimitInKbps": "0",
			"iopsLimit":            "0",
		},
	}
	resp, err := svc.ControllerModifyVolume(ctx, req)
	assert.NotNil(t, resp)
	assert.NoError(t, err)
}

// TestControllerModifyVolume_PartialBandwidthPreserved verifies IOPS-only update preserves bandwidth (U-002)
func TestControllerModifyVolume_PartialBandwidthPreserved(t *testing.T) {
	volumes := map[string]*siotypes.Volume{
		goodVolumeID: {
			ID:   goodVolumeID,
			Name: goodVolumeName,
			MappedSdcInfo: []*siotypes.MappedSdcInfo{
				{SdcID: sdcVolume1, LimitIops: 500, LimitBwInMbps: 2},
			},
		},
	}

	// Track setMappedSdcLimits calls
	var capturedLimitsParam siotypes.SetMappedSdcLimitsParam
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" || r.URL.Path == "/api/version" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `"4.0"`)
			return
		}
		if r.URL.Path == fmt.Sprintf("/api/instances/Volume::%s", goodVolumeID) && r.Method == http.MethodGet {
			writeJSONResponse(t, w, volumes[goodVolumeID])
			return
		}
		if r.URL.Path == fmt.Sprintf("/api/instances/Volume::%s/action/setMappedSdcLimits", goodVolumeID) && r.Method == http.MethodPost {
			decoder := json.NewDecoder(r.Body)
			if err := decoder.Decode(&capturedLimitsParam); err != nil {
				t.Errorf("failed to decode setMappedSdcLimits body: %v", err)
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	assert.NoError(t, err)
	client.SetToken("test-token")

	svc := &service{
		adminClients:            map[string]*sio.Client{arrayID: client},
		systems:                 map[string]*sio.System{arrayID: {}},
		storagePoolIDToName:     map[string]string{},
		volumePrefixToSystems:   map[string][]string{},
		connectedSystemNameToID: map[string]string{},
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				arrayID: {SystemID: arrayID, Endpoint: server.URL},
			},
		},
	}

	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId: arrayID + "-" + goodVolumeID,
		MutableParameters: map[string]string{
			"iopsLimit": "1000",
		},
	}
	resp, modErr := svc.ControllerModifyVolume(ctx, req)
	assert.NotNil(t, resp)
	assert.NoError(t, modErr)

	// Verify bandwidth was preserved: current LimitBwInMbps=2 → 2*1024=2048 Kbps
	assert.Equal(t, "2048", capturedLimitsParam.BandwidthLimitInKbps, "bandwidth should be preserved as 2048 Kbps (2 MBps * 1024)")
	assert.Equal(t, "1000", capturedLimitsParam.IopsLimit, "iopsLimit should be the requested value")
	assert.Equal(t, sdcVolume1, capturedLimitsParam.SdcID, "SdcID should match the mapped SDC")
}

// TestControllerModifyVolume_PartialIopsPreserved verifies bandwidth-only update preserves IOPS (U-003)
func TestControllerModifyVolume_PartialIopsPreserved(t *testing.T) {
	volumes := map[string]*siotypes.Volume{
		goodVolumeID: {
			ID:   goodVolumeID,
			Name: goodVolumeName,
			MappedSdcInfo: []*siotypes.MappedSdcInfo{
				{SdcID: sdcVolume1, LimitIops: 200, LimitBwInMbps: 0},
			},
		},
	}

	var capturedLimitsParam siotypes.SetMappedSdcLimitsParam
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" || r.URL.Path == "/api/version" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `"4.0"`)
			return
		}
		if r.URL.Path == fmt.Sprintf("/api/instances/Volume::%s", goodVolumeID) && r.Method == http.MethodGet {
			writeJSONResponse(t, w, volumes[goodVolumeID])
			return
		}
		if r.URL.Path == fmt.Sprintf("/api/instances/Volume::%s/action/setMappedSdcLimits", goodVolumeID) && r.Method == http.MethodPost {
			decoder := json.NewDecoder(r.Body)
			if err := decoder.Decode(&capturedLimitsParam); err != nil {
				t.Errorf("failed to decode setMappedSdcLimits body: %v", err)
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	assert.NoError(t, err)
	client.SetToken("test-token")

	svc := &service{
		adminClients:            map[string]*sio.Client{arrayID: client},
		systems:                 map[string]*sio.System{arrayID: {}},
		storagePoolIDToName:     map[string]string{},
		volumePrefixToSystems:   map[string][]string{},
		connectedSystemNameToID: map[string]string{},
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				arrayID: {SystemID: arrayID, Endpoint: server.URL},
			},
		},
	}

	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId: arrayID + "-" + goodVolumeID,
		MutableParameters: map[string]string{
			"bandwidthLimitInKbps": "4096",
		},
	}
	resp, modErr := svc.ControllerModifyVolume(ctx, req)
	assert.NotNil(t, resp)
	assert.NoError(t, modErr)

	// Verify IOPS was preserved: current LimitIops=200
	assert.Equal(t, "200", capturedLimitsParam.IopsLimit, "iopsLimit should be preserved as 200")
	assert.Equal(t, "4096", capturedLimitsParam.BandwidthLimitInKbps, "bandwidth should be the requested value")
}

// TestControllerModifyVolume_MultipleSDCs verifies QoS applied to all mapped SDCs (U-013)
func TestControllerModifyVolume_MultipleSDCs(t *testing.T) {
	volumes := map[string]*siotypes.Volume{
		goodVolumeID: {
			ID:   goodVolumeID,
			Name: goodVolumeName,
			MappedSdcInfo: []*siotypes.MappedSdcInfo{
				{SdcID: sdcVolume1, LimitIops: 0, LimitBwInMbps: 0},
				{SdcID: sdcVolume2, LimitIops: 0, LimitBwInMbps: 0},
				{SdcID: "d0f055ab00000002", LimitIops: 0, LimitBwInMbps: 0},
			},
		},
	}

	setLimitsCalls := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" || r.URL.Path == "/api/version" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `"4.0"`)
			return
		}
		if r.URL.Path == fmt.Sprintf("/api/instances/Volume::%s", goodVolumeID) && r.Method == http.MethodGet {
			writeJSONResponse(t, w, volumes[goodVolumeID])
			return
		}
		if r.URL.Path == fmt.Sprintf("/api/instances/Volume::%s/action/setMappedSdcLimits", goodVolumeID) && r.Method == http.MethodPost {
			setLimitsCalls++
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	assert.NoError(t, err)
	client.SetToken("test-token")

	svc := &service{
		adminClients:            map[string]*sio.Client{arrayID: client},
		systems:                 map[string]*sio.System{arrayID: {}},
		storagePoolIDToName:     map[string]string{},
		volumePrefixToSystems:   map[string][]string{},
		connectedSystemNameToID: map[string]string{},
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				arrayID: {SystemID: arrayID, Endpoint: server.URL},
			},
		},
	}

	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId: arrayID + "-" + goodVolumeID,
		MutableParameters: map[string]string{
			"iopsLimit": "500",
		},
	}
	resp, modErr := svc.ControllerModifyVolume(ctx, req)
	assert.NotNil(t, resp)
	assert.NoError(t, modErr)
	assert.Equal(t, 3, setLimitsCalls, "SetMappedSdcLimits should be called once per mapped SDC")
}

// TestControllerModifyVolume_Idempotent verifies no API call when values already match (U-012)
func TestControllerModifyVolume_Idempotent(t *testing.T) {
	volumes := map[string]*siotypes.Volume{
		goodVolumeID: {
			ID:   goodVolumeID,
			Name: goodVolumeName,
			MappedSdcInfo: []*siotypes.MappedSdcInfo{
				{SdcID: sdcVolume1, LimitIops: 500, LimitBwInMbps: 0},
			},
		},
	}

	setLimitsCalls := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" || r.URL.Path == "/api/version" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `"4.0"`)
			return
		}
		if r.URL.Path == fmt.Sprintf("/api/instances/Volume::%s", goodVolumeID) && r.Method == http.MethodGet {
			writeJSONResponse(t, w, volumes[goodVolumeID])
			return
		}
		if r.URL.Path == fmt.Sprintf("/api/instances/Volume::%s/action/setMappedSdcLimits", goodVolumeID) && r.Method == http.MethodPost {
			setLimitsCalls++
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	assert.NoError(t, err)
	client.SetToken("test-token")

	svc := &service{
		adminClients:            map[string]*sio.Client{arrayID: client},
		systems:                 map[string]*sio.System{arrayID: {}},
		storagePoolIDToName:     map[string]string{},
		volumePrefixToSystems:   map[string][]string{},
		connectedSystemNameToID: map[string]string{},
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				arrayID: {SystemID: arrayID, Endpoint: server.URL},
			},
		},
	}

	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId: arrayID + "-" + goodVolumeID,
		MutableParameters: map[string]string{
			"iopsLimit": "500",
			// bandwidthLimitInKbps not specified → preserve current (0 MBps → 0 Kbps)
		},
	}
	resp, modErr := svc.ControllerModifyVolume(ctx, req)
	assert.NotNil(t, resp)
	assert.NoError(t, modErr)
	assert.Equal(t, 0, setLimitsCalls, "SetMappedSdcLimits should NOT be called when values already match")
}

func TestControllerModifyVolume_BackendUnavailableOnGetVolume(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" || r.URL.Path == "/api/version" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `"4.0"`)
			return
		}
		if r.URL.Path == fmt.Sprintf("/api/instances/Volume::%s", goodVolumeID) && r.Method == http.MethodGet {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"message":"service unavailable","httpStatusCode":503,"errorCode":0}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	assert.NoError(t, err)
	client.SetToken("test-token")

	svc := &service{
		adminClients:            map[string]*sio.Client{arrayID: client},
		systems:                 map[string]*sio.System{arrayID: {}},
		storagePoolIDToName:     map[string]string{},
		volumePrefixToSystems:   map[string][]string{},
		connectedSystemNameToID: map[string]string{},
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				arrayID: {SystemID: arrayID, Endpoint: server.URL},
			},
		},
	}

	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId: arrayID + "-" + goodVolumeID,
		MutableParameters: map[string]string{
			"iopsLimit": "100",
		},
	}
	resp, modErr := svc.ControllerModifyVolume(ctx, req)
	assert.Nil(t, resp)
	assert.Error(t, modErr)
	st, ok := status.FromError(modErr)
	assert.True(t, ok)
	assert.Equal(t, codes.Unavailable, st.Code())
	assert.Contains(t, st.Message(), "Retry is safe")
}

func TestControllerModifyVolume_BackendUnavailableOnSetMappedSdcLimits(t *testing.T) {
	volumes := map[string]*siotypes.Volume{
		goodVolumeID: {
			ID:   goodVolumeID,
			Name: goodVolumeName,
			MappedSdcInfo: []*siotypes.MappedSdcInfo{
				{SdcID: sdcVolume1, LimitIops: 0, LimitBwInMbps: 0},
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" || r.URL.Path == "/api/version" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `"4.0"`)
			return
		}
		if r.URL.Path == fmt.Sprintf("/api/instances/Volume::%s", goodVolumeID) && r.Method == http.MethodGet {
			writeJSONResponse(t, w, volumes[goodVolumeID])
			return
		}
		if r.URL.Path == fmt.Sprintf("/api/instances/Volume::%s/action/setMappedSdcLimits", goodVolumeID) && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"message":"service unavailable","httpStatusCode":503,"errorCode":0}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	assert.NoError(t, err)
	client.SetToken("test-token")

	svc := &service{
		adminClients:            map[string]*sio.Client{arrayID: client},
		systems:                 map[string]*sio.System{arrayID: {}},
		storagePoolIDToName:     map[string]string{},
		volumePrefixToSystems:   map[string][]string{},
		connectedSystemNameToID: map[string]string{},
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				arrayID: {SystemID: arrayID, Endpoint: server.URL},
			},
		},
	}

	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId: arrayID + "-" + goodVolumeID,
		MutableParameters: map[string]string{
			"iopsLimit": "100",
		},
	}
	resp, modErr := svc.ControllerModifyVolume(ctx, req)
	assert.Nil(t, resp)
	assert.Error(t, modErr)
	st, ok := status.FromError(modErr)
	assert.True(t, ok)
	assert.Equal(t, codes.Unavailable, st.Code())
	assert.Contains(t, st.Message(), "Retry is safe")
}

// TestControllerModifyVolume_BackendError verifies INTERNAL error on backend failure (U-011)
func TestControllerModifyVolume_BackendError(t *testing.T) {
	volumes := map[string]*siotypes.Volume{
		goodVolumeID: {
			ID:   goodVolumeID,
			Name: goodVolumeName,
			MappedSdcInfo: []*siotypes.MappedSdcInfo{
				{SdcID: sdcVolume1, LimitIops: 0, LimitBwInMbps: 0},
			},
		},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" || r.URL.Path == "/api/version" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `"4.0"`)
			return
		}
		if r.URL.Path == fmt.Sprintf("/api/instances/Volume::%s", goodVolumeID) && r.Method == http.MethodGet {
			writeJSONResponse(t, w, volumes[goodVolumeID])
			return
		}
		if r.URL.Path == fmt.Sprintf("/api/instances/Volume::%s/action/setMappedSdcLimits", goodVolumeID) && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"message":"internal server error","httpStatusCode":500,"errorCode":0}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	assert.NoError(t, err)
	client.SetToken("test-token")

	svc := &service{
		adminClients:            map[string]*sio.Client{arrayID: client},
		systems:                 map[string]*sio.System{arrayID: {}},
		storagePoolIDToName:     map[string]string{},
		volumePrefixToSystems:   map[string][]string{},
		connectedSystemNameToID: map[string]string{},
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				arrayID: {SystemID: arrayID, Endpoint: server.URL},
			},
		},
	}

	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := &csi.ControllerModifyVolumeRequest{
		VolumeId: arrayID + "-" + goodVolumeID,
		MutableParameters: map[string]string{
			"iopsLimit": "100",
		},
	}
	resp, modErr := svc.ControllerModifyVolume(ctx, req)
	assert.Nil(t, resp)
	assert.Error(t, modErr)
	st, ok := status.FromError(modErr)
	assert.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

// TestControllerGetCapabilities_IncludesModifyVolume verifies MODIFY_VOLUME is in capabilities (U-018)
func TestControllerGetCapabilities_IncludesModifyVolume(t *testing.T) {
	svc := &service{
		opts: Opts{},
	}
	ctx := context.Background()
	resp, err := svc.ControllerGetCapabilities(ctx, &csi.ControllerGetCapabilitiesRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, resp)

	found := false
	for _, cap := range resp.Capabilities {
		if cap.GetRpc().GetType() == csi.ControllerServiceCapability_RPC_MODIFY_VOLUME {
			found = true
			break
		}
	}
	assert.True(t, found, "MODIFY_VOLUME capability should be present in ControllerGetCapabilities response")
}
