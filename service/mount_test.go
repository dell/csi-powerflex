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
	"sync"
	"testing"

	"github.com/dell/csi-metadata-retriever/retriever"
	"github.com/dell/csi-vxflexos/v2/k8sutils"
	"github.com/dell/gofsutil"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
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
	mountFsCheckEventRecorder = nil
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
	// Should not panic with nil eventRecorder and empty pvcName; timedOut stays false.
	obs := &fsCheckPVCObserver{
		pvcName:   "",
		logFields: map[string]interface{}{},
	}
	obs.OnEvent(gofsutil.StartedFSCheckEvent)
	obs.OnEvent(gofsutil.FoundNoErrorsEvent)
	obs.OnEvent(gofsutil.FSCheckTimedOutEvent)
	assert.False(t, obs.timedOut)
}

func TestFsCheckPVCObserver_OnEvent_WithRecorder(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	broadcaster := record.NewBroadcaster()
	recorder := broadcaster.NewRecorder(scheme, corev1.EventSource{Component: "test"})

	obs := &fsCheckPVCObserver{
		pvcName:       "test-pvc",
		pvcNamespace:  "test-ns",
		devicePath:    "/dev/sda",
		fsType:        "ext4",
		logFields:     map[string]interface{}{},
		eventRecorder: recorder,
	}

	events := []string{
		gofsutil.StartedFSCheckEvent,
		gofsutil.FoundNoErrorsEvent,
		gofsutil.FinishedFSRepairEvent,
		gofsutil.FoundErrorsEvent,
		gofsutil.FSCheckFailedEvent,
		gofsutil.FSCheckTimedOutEvent,
		gofsutil.FSRepairTimedOutEvent,
		gofsutil.FSRepairFailedEvent,
		gofsutil.StartFSRepairEvent,
		gofsutil.FoundDirtyLogEvent,
		gofsutil.StartLogReplayEvent,
		gofsutil.LogReplayFailedEvent,
		gofsutil.LogReplayDoneEvent,
		"unknown-event",
	}

	for _, ev := range events {
		obs.OnEvent(ev)
	}
	assert.True(t, obs.timedOut)
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
			wantErrContains: "File system check failed",
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
		{
			name:           "EXT4 Filesystem Check Fails with PVC Event Recording",
			fsCheckEnabled: true,
			fsCheckMode:    "checkOnly",
			devicePath:     "/dev/sdc",
			fsType:         "ext4",
			accMode:        &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
			pvName:         "pvc-test-fail",
			volumeID:       "vol-003",
			metadataRetriever: func() *mockMetadataRetrieverClient {
				mockClient := &mockMetadataRetrieverClient{}
				mockClient.On("GetPVCLabelsByPVName", mock.Anything, mock.Anything).
					Return(&retriever.GetPVCLabelsByPVNameResponse{
						PVCName:      "test-pvc-fail",
						PVCNamespace: "test-namespace",
						Parameters: map[string]string{
							"csi.dell.com/fs_check_enabled": "true",
							"csi.dell.com/fs_check_mode":    "checkonly",
						},
					}, nil)
				return mockClient
			}(),
			osExecFn: func(_ context.Context, _ string, _ ...string) (int, error) {
				return 4, fmt.Errorf("exit status 4")
			},
			wantErr:         true,
			wantErrContains: "File system check failed",
			expectMockCall:  true,
		},
		{
			name:           "XFS Filesystem Check Fails with PVC Event Recording",
			fsCheckEnabled: true,
			fsCheckMode:    "checkOnly",
			devicePath:     "/dev/sdd",
			fsType:         "xfs",
			accMode:        &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
			pvName:         "pvc-test-xfs-fail",
			volumeID:       "vol-004",
			metadataRetriever: func() *mockMetadataRetrieverClient {
				mockClient := &mockMetadataRetrieverClient{}
				mockClient.On("GetPVCLabelsByPVName", mock.Anything, mock.Anything).
					Return(&retriever.GetPVCLabelsByPVNameResponse{
						PVCName:      "test-pvc-xfs-fail",
						PVCNamespace: "test-namespace",
						Parameters: map[string]string{
							"csi.dell.com/fs_check_enabled": "true",
							"csi.dell.com/fs_check_mode":    "checkonly",
						},
					}, nil)
				return mockClient
			}(),
			osExecFn: func(_ context.Context, _ string, _ ...string) (int, error) {
				return 1, fmt.Errorf("xfs_repair failed")
			},
			wantErr:         true,
			wantErrContains: "File system check failed",
			expectMockCall:  true,
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

				scheme := runtime.NewScheme()
				_ = corev1.AddToScheme(scheme)
				broadcaster := record.NewBroadcaster()
				mountFsCheckEventRecorder = broadcaster.NewRecorder(scheme, corev1.EventSource{Component: "test"})
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

// --- Tests for initFsCheckEventRecorder ---

func TestNewFsCheckEventRecorder(t *testing.T) {
	tests := []struct {
		name           string
		setupClientset func() kubernetes.Interface
		expectError    bool
		errorContains  string
	}{
		{
			name: "Nil Clientset - Should Return Error",
			setupClientset: func() kubernetes.Interface {
				return nil
			},
			expectError:   true,
			errorContains: "kubernetes clientset is not initialized",
		},
		{
			name: "Valid Clientset - Should Return Recorder",
			setupClientset: func() kubernetes.Interface {
				return fake.NewClientset()
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origClientset := k8sutils.Clientset
			defer func() { k8sutils.Clientset = origClientset }()

			k8sutils.Clientset = tt.setupClientset()
			recorder, err := newFsCheckEventRecorder()

			if tt.expectError {
				assert.Nil(t, recorder)
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, recorder)
			}
		})
	}
}

func TestInitFsCheckEventRecorder(t *testing.T) {
	tests := []struct {
		name           string
		setupClientset func() kubernetes.Interface
		expectNil      bool
	}{
		{
			name: "Nil Clientset - Should Return Nil Recorder",
			setupClientset: func() kubernetes.Interface {
				return nil
			},
			expectNil: true,
		},
		{
			name: "Valid Clientset - Should Return Valid Recorder",
			setupClientset: func() kubernetes.Interface {
				return fake.NewClientset()
			},
			expectNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origClientset := k8sutils.Clientset
			defer func() {
				k8sutils.Clientset = origClientset
				cachedFsCheckEventRecorder = nil
				fsCheckEventRecorderOnce = sync.Once{}
			}()

			k8sutils.Clientset = tt.setupClientset()
			recorder := initFsCheckEventRecorder()

			if tt.expectNil {
				assert.Nil(t, recorder)
			} else {
				assert.NotNil(t, recorder)
			}

			recorder2 := initFsCheckEventRecorder()
			if tt.expectNil {
				assert.Nil(t, recorder2)
			} else {
				assert.NotNil(t, recorder2)
				assert.Equal(t, recorder, recorder2)
			}
		})
	}
}
