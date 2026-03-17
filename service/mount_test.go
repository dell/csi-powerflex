// Copyright © 2019-2025 Dell Inc. or its subsidiaries. All Rights Reserved.
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
	"testing"

	"github.com/dell/csi-metadata-retriever/retriever"
	"github.com/dell/gofsutil"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// mockMetadataRetrieverClient is a mock for retriever.MetadataRetrieverClient
type mockMetadataRetrieverClient struct {
	mock.Mock
}

func (m *mockMetadataRetrieverClient) GetPVCLabels(ctx context.Context, req *retriever.GetPVCLabelsRequest) (*retriever.GetPVCLabelsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*retriever.GetPVCLabelsResponse), args.Error(1)
}

func (m *mockMetadataRetrieverClient) GetPVCLabelsByPVName(ctx context.Context, req *retriever.GetPVCLabelsByPVNameRequest) (*retriever.GetPVCLabelsByPVNameResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*retriever.GetPVCLabelsByPVNameResponse), args.Error(1)
}

// helper to restore package-level FSCK vars after each test
func saveFsckGlobals() (bool, string) {
	return mountFsCheckEnabled, mountFsCheckMode
}

func restoreFsckGlobals(enabled bool, mode string) {
	mountFsCheckEnabled = enabled
	mountFsCheckMode = mode
	metadataRetrieverClient = nil
}

// --- Tests for parsePVNameFromTargetPath ---

func TestParsePVNameFromTargetPath(t *testing.T) {
	tests := []struct {
		name       string
		targetPath string
		wantPV     string
	}{
		{
			name:       "Valid kubelet target path",
			targetPath: "/var/lib/kubelet/pods/abc-123/volumes/kubernetes.io~csi/pvc-xyz/mount",
			wantPV:     "pvc-xyz",
		},
		{
			name:       "Invalid kubelet target path",
			targetPath: "/var/lib/kubelet/pods/uid/volumes/mount",
			wantPV:     "",
		},
		{
			name:       "Empty path",
			targetPath: "",
			wantPV:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePVNameFromTargetPath(tt.targetPath)
			assert.Equal(t, tt.wantPV, got)
		})
	}
}

// --- Tests for fsCheckPVCObserver.OnEvent ---

func TestFsCheckPVCObserver_OnEvent(t *testing.T) {
	events := []struct {
		name         string
		event        string
		wantTimedOut bool
	}{
		{"StartedFSCheckEvent", gofsutil.StartedFSCheckEvent, false},
		{"FoundNoErrorsEvent", gofsutil.FoundNoErrorsEvent, false},
		{"FinishedFSRepairEvent", gofsutil.FinishedFSRepairEvent, false},
		{"FSCheckFailedEvent", gofsutil.FSCheckFailedEvent, false},
		{"FSCheckTimedOutEvent", gofsutil.FSCheckTimedOutEvent, true},
		{"FSRepairTimedOutEvent", gofsutil.FSRepairTimedOutEvent, true},
		{"FSRepairFailedEvent", gofsutil.FSRepairFailedEvent, false},
		{"UnknownEvent", "some unexpected event", false},
	}

	for _, tt := range events {
		t.Run(tt.name, func(t *testing.T) {
			obs := &fsCheckPVCObserver{
				pvcName:      "test-pvc",
				pvcNamespace: "test-ns",
				devicePath:   "/dev/sda",
				fsType:       "ext4",
				logFields:    map[string]interface{}{},
				timedOut:     false,
			}
			obs.OnEvent(tt.event)
			assert.Equal(t, tt.wantTimedOut, obs.timedOut)
		})
	}
}

// --- Tests for runPreMountFsck ---

func TestRunPreMountFsck(t *testing.T) {
	tests := []struct {
		name              string
		fsCheckEnabled    bool
		fsCheckMode       string
		devicePath        string
		fsType            string
		accMode           *csi.VolumeCapability_AccessMode
		pvName            string
		volumeID          string
		metadataRetriever *mockMetadataRetrieverClient
		osExecFn          func(context.Context, string, ...string) (int, error)
		wantErr           bool
		wantErrContains   string
		expectMockCall    bool
	}{
		{
			name:           "Disabled Globally - Returns No Error",
			fsCheckEnabled: false,
			fsCheckMode:    "checkOnly",
			devicePath:     "/dev/sda",
			fsType:         "ext4",
			accMode:        &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
			pvName:         "pvc-test",
			volumeID:       "vol-001",
			wantErr:        false,
		},
		{
			name:           "Unsupported Filesystem - Returns No Error",
			fsCheckEnabled: true,
			fsCheckMode:    "checkOnly",
			devicePath:     "/dev/sda",
			fsType:         "ext",
			accMode:        &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
			pvName:         "pvc-test",
			volumeID:       "vol-001",
			wantErr:        false,
		},
		{
			name:           "Unsupported Access Mode - Returns No Error",
			fsCheckEnabled: true,
			fsCheckMode:    "checkOnly",
			devicePath:     "/dev/sda",
			fsType:         "ext4",
			accMode:        &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY},
			pvName:         "pvc-test",
			volumeID:       "vol-001",
			wantErr:        false,
		},
		{
			name:           "Metadata Retriever Error - Uses Global Settings",
			fsCheckEnabled: true,
			fsCheckMode:    "checkOnly",
			devicePath:     "/dev/sda",
			fsType:         "ext4",
			accMode:        &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
			pvName:         "pvc-test",
			volumeID:       "vol-001",
			metadataRetriever: func() *mockMetadataRetrieverClient {
				mockClient := &mockMetadataRetrieverClient{}
				mockClient.On("GetPVCLabelsByPVName", mock.Anything, mock.Anything).
					Return(nil, fmt.Errorf("not able to retrieve PVC labels"))
				return mockClient
			}(),
			osExecFn:       func(_ context.Context, _ string, _ ...string) (int, error) { return 0, nil },
			wantErr:        false,
			expectMockCall: true,
		},
		{
			name:           "Disable FS Check via PVC Labels",
			fsCheckEnabled: true,
			fsCheckMode:    "checkOnly",
			devicePath:     "/dev/sda",
			fsType:         "ext4",
			accMode:        &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
			pvName:         "pvc-test",
			volumeID:       "vol-001",
			metadataRetriever: func() *mockMetadataRetrieverClient {
				mockClient := &mockMetadataRetrieverClient{}
				mockClient.On("GetPVCLabelsByPVName", mock.Anything, mock.Anything).
					Return(&retriever.GetPVCLabelsByPVNameResponse{
						PVCName:      "my-pvc",
						PVCNamespace: "default",
						Parameters: map[string]string{
							"csi.dell.com/fs_check_enabled": "false",
						},
					}, nil)
				return mockClient
			}(),
			osExecFn:       func(_ context.Context, _ string, _ ...string) (int, error) { return 0, nil },
			wantErr:        false,
			expectMockCall: true,
		},
		{
			name:           "Enable FS Check with CheckOnly mode via PVC Labels",
			fsCheckEnabled: true,
			fsCheckMode:    "checkOnly",
			devicePath:     "/dev/sda",
			fsType:         "ext4",
			accMode:        &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
			pvName:         "pvc-test",
			volumeID:       "vol-001",
			metadataRetriever: func() *mockMetadataRetrieverClient {
				mockClient := &mockMetadataRetrieverClient{}
				mockClient.On("GetPVCLabelsByPVName", mock.Anything, mock.Anything).
					Return(&retriever.GetPVCLabelsByPVNameResponse{
						PVCName:      "my-pvc",
						PVCNamespace: "default",
						Parameters: map[string]string{
							"csi.dell.com/fs_check_enabled": "true",
							"csi.dell.com/fs_check_mode":    "checkonly",
						},
					}, nil)
				return mockClient
			}(),
			osExecFn:       func(_ context.Context, _ string, _ ...string) (int, error) { return 0, nil },
			wantErr:        false,
			expectMockCall: true,
		},
		{
			name:           "PVC label enables fscheck with checkandrepair mode",
			fsCheckEnabled: true,
			fsCheckMode:    "checkOnly",
			devicePath:     "/dev/sda",
			fsType:         "xfs",
			accMode:        &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER},
			pvName:         "pvc-test",
			volumeID:       "vol-001",
			metadataRetriever: func() *mockMetadataRetrieverClient {
				mockClient := &mockMetadataRetrieverClient{}
				mockClient.On("GetPVCLabelsByPVName", mock.Anything, mock.Anything).
					Return(&retriever.GetPVCLabelsByPVNameResponse{
						PVCName:      "my-pvc",
						PVCNamespace: "default",
						Parameters: map[string]string{
							"csi.dell.com/fs_check_enabled": "true",
							"csi.dell.com/fs_check_mode":    "checkandrepair",
						},
					}, nil)
				return mockClient
			}(),
			osExecFn:       func(_ context.Context, _ string, _ ...string) (int, error) { return 0, nil },
			wantErr:        false,
			expectMockCall: true,
		},
		{
			name:           "EXT4 Filesystem Check Fails",
			fsCheckEnabled: true,
			fsCheckMode:    "checkOnly",
			devicePath:     "/dev/sda",
			fsType:         "ext4",
			accMode:        &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
			pvName:         "",
			volumeID:       "vol-001",
			osExecFn: func(_ context.Context, _ string, _ ...string) (int, error) {
				return 4, fmt.Errorf("exit status 4")
			},
			wantErr:         true,
			wantErrContains: "file system check failed",
		},
		{
			name:           "EXT4 Filesystem Check Passes",
			fsCheckEnabled: true,
			fsCheckMode:    "checkOnly",
			devicePath:     "/dev/sdb",
			fsType:         "ext4",
			accMode:        &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER},
			pvName:         "",
			volumeID:       "vol-002",
			osExecFn:       func(_ context.Context, _ string, _ ...string) (int, error) { return 0, nil },
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saved, savedMode := saveFsckGlobals()
			defer restoreFsckGlobals(saved, savedMode)

			mountFsCheckEnabled = tt.fsCheckEnabled
			mountFsCheckMode = tt.fsCheckMode

			if tt.metadataRetriever != nil {
				metadataRetrieverClient = tt.metadataRetriever
				defer func() { metadataRetrieverClient = nil }()
			} else {
				metadataRetrieverClient = nil
			}

			if tt.osExecFn != nil {
				orig := gofsutil.OSExecFn
				gofsutil.OSExecFn = tt.osExecFn
				defer func() { gofsutil.OSExecFn = orig }()
			}

			err := runPreMountFsck(context.Background(), tt.devicePath, tt.fsType, tt.accMode, tt.pvName, tt.volumeID)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrContains)
			} else {
				assert.NoError(t, err)
			}

			if tt.expectMockCall {
				tt.metadataRetriever.AssertExpectations(t)
			}
		})
	}
}
