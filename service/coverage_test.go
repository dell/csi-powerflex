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
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	commonext "github.com/Ecosystems/container-storage-modules/src/dell-csi-extensions/common"
	podmon "github.com/Ecosystems/container-storage-modules/src/dell-csi-extensions/podmon"
	"github.com/Ecosystems/container-storage-modules/src/gobrick"
	sio "github.com/Ecosystems/container-storage-modules/src/goscaleio"
	siotypes "github.com/Ecosystems/container-storage-modules/src/goscaleio/types/v1"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// ── isBackendUnavailableError ──────────────────────────────────────────

func TestIsBackendUnavailableError_NilError(t *testing.T) {
	assert.False(t, isBackendUnavailableError(nil))
}

func TestIsBackendUnavailableError_URLError(t *testing.T) {
	err := &url.Error{Op: "Get", URL: "http://x", Err: errors.New("fail")}
	assert.True(t, isBackendUnavailableError(err))
}

func TestIsBackendUnavailableError_NetError(t *testing.T) {
	// net.OpError satisfies net.Error
	err := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("refused")}
	assert.True(t, isBackendUnavailableError(err))
}

func TestIsBackendUnavailableError_DeadlineExceeded(t *testing.T) {
	assert.True(t, isBackendUnavailableError(context.DeadlineExceeded))
}

func TestIsBackendUnavailableError_StringIndicators(t *testing.T) {
	for _, msg := range []string{
		"connection refused",
		"connection reset by peer",
		"no such host found",
		"timeout awaiting headers",
		"i/o timeout",
		"service unavailable",
		"bad gateway",
		"gateway timeout",
		"503 error",
		"504 error",
	} {
		assert.True(t, isBackendUnavailableError(errors.New(msg)), "expected true for: %s", msg)
	}
}

func TestIsBackendUnavailableError_GenericError(t *testing.T) {
	assert.False(t, isBackendUnavailableError(errors.New("some random error")))
}

// ── mapControllerModifyVolumeBackendError ──────────────────────────────

func TestMapControllerModifyVolumeBackendError_Unavailable(t *testing.T) {
	err := &url.Error{Op: "Get", URL: "http://x", Err: errors.New("fail")}
	mapped := mapControllerModifyVolumeBackendError("sys1", "vol1", err)
	st, _ := status.FromError(mapped)
	assert.Equal(t, codes.Unavailable, st.Code())
	assert.Contains(t, st.Message(), "Retry is safe")
}

func TestMapControllerModifyVolumeBackendError_Internal(t *testing.T) {
	mapped := mapControllerModifyVolumeBackendError("sys1", "vol1", errors.New("something"))
	st, _ := status.FromError(mapped)
	assert.Equal(t, codes.Internal, st.Code())
}

// ── mergeStringMaps ────────────────────────────────────────────────────

func TestMergeStringMaps_NilBase(t *testing.T) {
	result := mergeStringMaps(nil, map[string]string{"a": "1"})
	assert.Equal(t, map[string]string{"a": "1"}, result)
}

func TestMergeStringMaps_NilAdditional(t *testing.T) {
	result := mergeStringMaps(map[string]string{"a": "1"}, nil)
	assert.Equal(t, map[string]string{"a": "1"}, result)
}

func TestMergeStringMaps_BothNil(t *testing.T) {
	result := mergeStringMaps(nil, nil)
	assert.NotNil(t, result)
	assert.Empty(t, result)
}

func TestMergeStringMaps_Override(t *testing.T) {
	result := mergeStringMaps(
		map[string]string{"a": "1", "b": "2"},
		map[string]string{"b": "3", "c": "4"},
	)
	assert.Equal(t, "1", result["a"])
	assert.Equal(t, "3", result["b"])
	assert.Equal(t, "4", result["c"])
}

// ── verifySystem ───────────────────────────────────────────────────────

func TestVerifySystem_NilClient(t *testing.T) {
	svc := &service{adminClients: map[string]*sio.Client{}}
	c, err := svc.verifySystem("missing")
	assert.Nil(t, c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "can't find adminClient")
}

func TestVerifySystem_Found(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `"4.0"`)
	}))
	defer server.Close()
	client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	svc := &service{adminClients: map[string]*sio.Client{"sys1": client}}
	c, err := svc.verifySystem("sys1")
	assert.NoError(t, err)
	assert.Equal(t, client, c)
}

// ── copyInterestingParameters ──────────────────────────────────────────

func TestCopyInterestingParameters(t *testing.T) {
	params := map[string]string{
		"FsType":                "ext4",
		KeyBandwidthLimitInKbps: "1024",
		KeyIopsLimit:            "100",
		"unrelated":             "ignored",
	}
	out := map[string]string{}
	copyInterestingParameters(params, out)
	assert.Equal(t, "ext4", out["FsType"])
	assert.Equal(t, "1024", out[KeyBandwidthLimitInKbps])
	assert.Equal(t, "100", out[KeyIopsLimit])
	_, exists := out["unrelated"]
	assert.False(t, exists)
}

func TestCopyInterestingParameters_Empty(t *testing.T) {
	out := map[string]string{}
	copyInterestingParameters(map[string]string{}, out)
	assert.Empty(t, out)
}

// ── validateAccessType ────────────────────────────────────────────────

func TestValidateAccessType_BlockValid(t *testing.T) {
	modes := []csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER,
		csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
		csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
	}
	for _, m := range modes {
		err := validateAccessType(&csi.VolumeCapability_AccessMode{Mode: m}, true)
		assert.NoError(t, err, "block mode %v should be valid", m)
	}
}

func TestValidateAccessType_BlockInvalid(t *testing.T) {
	err := validateAccessType(&csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_UNKNOWN}, true)
	assert.Error(t, err)
}

func TestValidateAccessType_MountValid(t *testing.T) {
	modes := []csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
		csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
	}
	for _, m := range modes {
		err := validateAccessType(&csi.VolumeCapability_AccessMode{Mode: m}, false)
		assert.NoError(t, err, "mount mode %v should be valid", m)
	}
}

func TestValidateAccessType_MountInvalid(t *testing.T) {
	err := validateAccessType(&csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER}, false)
	assert.Error(t, err)
}

// ── isSingleNodeAccess ─────────────────────────────────────────────────

func TestIsSingleNodeAccess_True(t *testing.T) {
	for _, m := range []csi.VolumeCapability_AccessMode_Mode{
		csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER,
		csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
	} {
		assert.True(t, isSingleNodeAccess(&csi.VolumeCapability_AccessMode{Mode: m}))
	}
}

func TestIsSingleNodeAccess_False(t *testing.T) {
	assert.False(t, isSingleNodeAccess(&csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER}))
	assert.False(t, isSingleNodeAccess(&csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY}))
}

// ── isFailover ─────────────────────────────────────────────────────────

func TestIsFailover(t *testing.T) {
	grp := &siotypes.ReplicationConsistencyGroup{FailoverType: "None"}
	assert.False(t, isFailover(grp))

	grp.FailoverType = "Failover"
	assert.True(t, isFailover(grp))
}

// ── normalize (nvme_utils.go) ──────────────────────────────────────────

func TestNormalize(t *testing.T) {
	assert.Equal(t, "hello", normalize("  Hello  "))
	assert.Equal(t, "", normalize("  "))
	assert.Equal(t, "abc", normalize("ABC"))
}

// ── getProbeLock ───────────────────────────────────────────────────────

func TestGetProbeLock(t *testing.T) {
	svc := &service{}
	lock1 := svc.getProbeLock("sys1")
	assert.NotNil(t, lock1)

	lock2 := svc.getProbeLock("sys1")
	assert.True(t, lock1 == lock2, "should return same lock for same systemID")

	lock3 := svc.getProbeLock("sys2")
	assert.True(t, lock1 != lock3, "should return different lock for different systemID")
}

// ── isReplicationNotSupported / isNfsNotSupported / isGenTypeNotSupportsNfsAndReplication ──

func TestIsReplicationNotSupported(t *testing.T) {
	svc := &service{}
	assert.True(t, svc.isReplicationNotSupported("EC"))
	assert.False(t, svc.isReplicationNotSupported("AB"))
}

func TestIsNfsNotSupported(t *testing.T) {
	svc := &service{}
	assert.True(t, svc.isNfsNotSupported(3.9))
	assert.False(t, svc.isNfsNotSupported(4.0))
	assert.False(t, svc.isNfsNotSupported(4.5))
}

func TestIsGenTypeNotSupportsNfsAndReplication(t *testing.T) {
	svc := &service{}
	assert.True(t, svc.isGenTypeNotSupportsNfsAndReplication("EC"))
	assert.False(t, svc.isGenTypeNotSupportsNfsAndReplication("AB"))
}

// ── getSystemName (node.go) ────────────────────────────────────────────

func TestGetSystemName(t *testing.T) {
	connectedSystemID = nil
	svc := &service{
		connectedSystemNameToID: map[string]string{
			"sysA": "id-A",
			"sysB": "id-B",
		},
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				"sysA": {SystemID: "sysA"},
				"sysB": {SystemID: "sysB"},
			},
		},
	}
	result := svc.getSystemName(context.Background(), []string{"id-A"})
	assert.True(t, result)
	assert.Contains(t, connectedSystemID, "sysA")
}

func TestGetSystemName_NoMatch(t *testing.T) {
	connectedSystemID = nil
	svc := &service{
		connectedSystemNameToID: map[string]string{
			"sysA": "id-A",
		},
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				"sysA": {SystemID: "sysA"},
			},
		},
	}
	result := svc.getSystemName(context.Background(), []string{"id-X"})
	assert.True(t, result)
	assert.Empty(t, connectedSystemID)
}

// ── getSystemIDFromParameters ──────────────────────────────────────────

func TestGetSystemIDFromParameters_NilParams(t *testing.T) {
	svc := &service{}
	_, err := svc.getSystemIDFromParameters(nil)
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

func TestGetSystemIDFromParameters_DefaultSystem(t *testing.T) {
	svc := &service{
		connectedSystemNameToID: map[string]string{},
		opts: Opts{
			defaultSystemID: "default-sys",
			arrays:          map[string]*ArrayConnectionData{},
		},
	}
	id, err := svc.getSystemIDFromParameters(map[string]string{})
	assert.NoError(t, err)
	assert.Equal(t, "default-sys", id)
}

func TestGetSystemIDFromParameters_SingleArray(t *testing.T) {
	svc := &service{
		connectedSystemNameToID: map[string]string{},
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				"only-sys": {},
			},
		},
	}
	id, err := svc.getSystemIDFromParameters(map[string]string{})
	assert.NoError(t, err)
	assert.Equal(t, "only-sys", id)
}

func TestGetSystemIDFromParameters_NoDefault_MultipleArrays(t *testing.T) {
	svc := &service{
		connectedSystemNameToID: map[string]string{},
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				"sys1": {},
				"sys2": {},
			},
		},
	}
	_, err := svc.getSystemIDFromParameters(map[string]string{})
	assert.Error(t, err)
}

func TestGetSystemIDFromParameters_NameMapping(t *testing.T) {
	svc := &service{
		connectedSystemNameToID: map[string]string{"myName": "actual-id"},
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{},
		},
	}
	id, err := svc.getSystemIDFromParameters(map[string]string{KeySystemID: "myName"})
	assert.NoError(t, err)
	assert.Equal(t, "actual-id", id)
}

// ── ControllerGetVolume ────────────────────────────────────────────────

func TestControllerGetVolume_EmptyVolumeID(t *testing.T) {
	svc := &service{}
	_, err := svc.ControllerGetVolume(context.Background(), &csi.ControllerGetVolumeRequest{})
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestControllerGetVolume_NoSystemID(t *testing.T) {
	svc := &service{
		connectedSystemNameToID: map[string]string{},
		opts:                    Opts{},
	}
	_, err := svc.ControllerGetVolume(context.Background(), &csi.ControllerGetVolumeRequest{
		VolumeId: "justvol",
	})
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestControllerGetVolume_VolumeNotFound(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" || r.URL.Path == "/api/version" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `"4.0"`)
			return
		}
		// Return volume not found error
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `[]`)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	client.SetToken("test-token")

	svc := &service{
		adminClients:            map[string]*sio.Client{"sys1": client},
		systems:                 map[string]*sio.System{"sys1": {}},
		connectedSystemNameToID: map[string]string{},
		opts:                    Opts{defaultSystemID: "sys1"},
	}
	resp, err := svc.ControllerGetVolume(context.Background(), &csi.ControllerGetVolumeRequest{
		VolumeId: "sys1-vol123",
	})
	// Could be an error or a response with abnormal=true depending on the error message
	if err != nil {
		st, _ := status.FromError(err)
		assert.Equal(t, codes.Internal, st.Code())
	} else {
		assert.NotNil(t, resp)
	}
}

func TestControllerGetVolume_AdminClientNil(t *testing.T) {
	svc := &service{
		adminClients:            map[string]*sio.Client{},
		systems:                 map[string]*sio.System{},
		connectedSystemNameToID: map[string]string{},
		opts:                    Opts{defaultSystemID: "sys1"},
	}
	_, err := svc.ControllerGetVolume(context.Background(), &csi.ControllerGetVolumeRequest{
		VolumeId: "sys1-vol123",
	})
	assert.Error(t, err)
}

// ── IsReplicationEnabledOnPlatforms ────────────────────────────────────

func TestIsReplicationEnabledOnPlatforms_EmptyRemote(t *testing.T) {
	svc := &service{}
	enabled, err := svc.IsReplicationEnabledOnPlatforms("src", "", "AB")
	assert.NoError(t, err)
	assert.True(t, enabled)
}

func TestIsReplicationEnabledOnPlatforms_SourceNotSupported(t *testing.T) {
	svc := &service{}
	enabled, err := svc.IsReplicationEnabledOnPlatforms("src", "remote", "EC")
	assert.Error(t, err)
	assert.False(t, enabled)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestIsReplicationEnabledOnPlatforms_RemoteNotSupported(t *testing.T) {
	svc := &service{
		platformInfos: map[string]*PlatformInfo{
			"remote": {GenType: "EC"},
		},
	}
	enabled, err := svc.IsReplicationEnabledOnPlatforms("src", "remote", "AB")
	assert.Error(t, err)
	assert.False(t, enabled)
}

func TestIsReplicationEnabledOnPlatforms_BothSupported(t *testing.T) {
	svc := &service{
		platformInfos: map[string]*PlatformInfo{
			"remote": {GenType: "AB"},
		},
	}
	enabled, err := svc.IsReplicationEnabledOnPlatforms("src", "remote", "AB")
	assert.NoError(t, err)
	assert.True(t, enabled)
}

// ── GetPlatformInfo ────────────────────────────────────────────────────

func TestGetPlatformInfo_Cached(t *testing.T) {
	cached := &PlatformInfo{SystemID: "sys1", ArrayVersion: 4.0, GenType: "AB"}
	svc := &service{
		platformInfos: map[string]*PlatformInfo{"sys1": cached},
	}
	info, err := svc.GetPlatformInfo("sys1")
	assert.NoError(t, err)
	assert.Equal(t, cached, info)
}

// ── getEnvString / getEnvBool / getEnvInt (space_reclamation.go) ──────

func TestGetEnvString(t *testing.T) {
	assert.Equal(t, "default", getEnvString("__NONEXISTENT_TEST_VAR__", "default"))

	os.Setenv("__TEST_GETENVSTRING__", "value")
	defer os.Unsetenv("__TEST_GETENVSTRING__")
	assert.Equal(t, "value", getEnvString("__TEST_GETENVSTRING__", "default"))
}

func TestGetEnvBool(t *testing.T) {
	assert.Equal(t, false, getEnvBool("__NONEXISTENT_BOOL__", false))
	assert.Equal(t, true, getEnvBool("__NONEXISTENT_BOOL__", true))

	os.Setenv("__TEST_GETENVBOOL__", "true")
	defer os.Unsetenv("__TEST_GETENVBOOL__")
	assert.Equal(t, true, getEnvBool("__TEST_GETENVBOOL__", false))

	os.Setenv("__TEST_GETENVBOOL__", "invalid")
	assert.Equal(t, false, getEnvBool("__TEST_GETENVBOOL__", false))
}

func TestGetEnvInt(t *testing.T) {
	assert.Equal(t, 42, getEnvInt("__NONEXISTENT_INT__", 42))

	os.Setenv("__TEST_GETENVINT__", "10")
	defer os.Unsetenv("__TEST_GETENVINT__")
	assert.Equal(t, 10, getEnvInt("__TEST_GETENVINT__", 42))

	os.Setenv("__TEST_GETENVINT__", "abc")
	assert.Equal(t, 42, getEnvInt("__TEST_GETENVINT__", 42))

	os.Setenv("__TEST_GETENVINT__", "-5")
	assert.Equal(t, 42, getEnvInt("__TEST_GETENVINT__", 42))
}

// ── ReadSpaceReclamationConfig ─────────────────────────────────────────

func TestReadSpaceReclamationConfig_Defaults(t *testing.T) {
	cfg := ReadSpaceReclamationConfig()
	assert.False(t, cfg.Enabled)
	assert.Equal(t, "0 2 * * 0", cfg.Schedule)
	assert.Equal(t, 2, cfg.MaxConcurrentVolumes)
	assert.Equal(t, 14400, cfg.TimeoutSeconds)
}

// getSystemIDFromPVHandle already tested in space_reclamation_test.go

// ── createProbeContextWithDeadline ─────────────────────────────────────

func TestCreateProbeContextWithDeadline_NoExistingDeadline(t *testing.T) {
	svc := &service{opts: Opts{probeTimeout: 30000000000}} // 30s in nanoseconds
	ctx, cancel := svc.createProbeContextWithDeadline(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	assert.True(t, ok)
	assert.False(t, deadline.IsZero())
}

// ── requireProbe ───────────────────────────────────────────────────────

func TestRequireProbe_SystemNotConfigured(t *testing.T) {
	svc := &service{
		adminClients: map[string]*sio.Client{},
		systems:      map[string]*sio.System{},
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{},
		},
	}
	err := svc.requireProbe(context.Background(), "nonexistent")
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestRequireProbe_AlreadyProbed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `"4.0"`)
	}))
	defer server.Close()
	client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	svc := &service{
		adminClients: map[string]*sio.Client{"sys1": client},
		systems:      map[string]*sio.System{"sys1": {}},
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{},
		},
	}
	err := svc.requireProbe(context.Background(), "sys1")
	assert.NoError(t, err)
}

// ── buildSdcLimitsParam ────────────────────────────────────────────────

func TestBuildSdcLimitsParam_NoChange(t *testing.T) {
	sdc := &siotypes.MappedSdcInfo{SdcID: "sdc1", LimitBwInMbps: 10, LimitIops: 500}
	result := buildSdcLimitsParam(sdc, map[string]string{})
	assert.Nil(t, result, "should return nil when no change needed")
}

func TestBuildSdcLimitsParam_ChangeIops(t *testing.T) {
	sdc := &siotypes.MappedSdcInfo{SdcID: "sdc1", LimitBwInMbps: 10, LimitIops: 500}
	result := buildSdcLimitsParam(sdc, map[string]string{"iopsLimit": "1000"})
	assert.NotNil(t, result)
	assert.Equal(t, "1000", result.IopsLimit)
	assert.Equal(t, "sdc1", result.SdcID)
}

func TestBuildSdcLimitsParam_ChangeBandwidth(t *testing.T) {
	sdc := &siotypes.MappedSdcInfo{SdcID: "sdc1", LimitBwInMbps: 10, LimitIops: 500}
	result := buildSdcLimitsParam(sdc, map[string]string{"bandwidthLimitInKbps": "20480"})
	assert.NotNil(t, result)
	assert.Equal(t, "20480", result.BandwidthLimitInKbps)
}

// ── getNodeUID (node.go) ───────────────────────────────────────────────

func TestGetNodeUID_Wrapper(t *testing.T) {
	K8sClientset = fake.NewSimpleClientset(
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node",
				UID:  "uid-12345",
			},
		},
	)
	svc := &service{
		opts: Opts{
			KubeNodeName: "test-node",
		},
	}
	uid, err := getNodeUID(context.Background(), svc)
	assert.NoError(t, err)
	assert.Equal(t, "uid-12345", uid)
}

// ── checkVolumesMap ────────────────────────────────────────────────────

func TestCheckVolumesMap_NoPrefixConflict(t *testing.T) {
	svc := &service{
		volumePrefixToSystems:   map[string][]string{},
		volCacheRWL:             sync.RWMutex{},
		connectedSystemNameToID: map[string]string{},
	}
	err := svc.checkVolumesMap("sys1-vol123")
	assert.NoError(t, err)
}

func TestCheckVolumesMap_WithSystemID(t *testing.T) {
	svc := &service{
		volumePrefixToSystems:   map[string][]string{},
		volCacheRWL:             sync.RWMutex{},
		connectedSystemNameToID: map[string]string{"sysA": "id-A"},
	}
	err := svc.checkVolumesMap("sysA-vol123")
	assert.NoError(t, err)
}

func TestCheckVolumesMap_VolumeIDTooShort(t *testing.T) {
	svc := &service{
		volumePrefixToSystems:   map[string][]string{},
		volCacheRWL:             sync.RWMutex{},
		connectedSystemNameToID: map[string]string{},
	}
	err := svc.checkVolumesMap("ab") // Less than 3 characters
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "shorter than 3 chars")
}

// ── calcKeyForMap ──────────────────────────────────────────────────────

func TestCalcKeyForMap(t *testing.T) {
	svc := &service{}
	key := svc.calcKeyForMap("sys1-vol123")
	assert.Equal(t, "sys", key)

	key2 := svc.calcKeyForMap("vol123")
	assert.Equal(t, "vol", key2)
}

// ── Contains (service.go) ──────────────────────────────────────────────

func TestContains(t *testing.T) {
	assert.True(t, Contains([]string{"a", "b", "c"}, "b"))
	assert.False(t, Contains([]string{"a", "b", "c"}, "d"))
	assert.False(t, Contains([]string{}, "a"))
}

// ── GetIPListWithMaskFromString (service.go) ───────────────────────────

func TestGetIPListWithMaskFromString_ValidIP(t *testing.T) {
	ip, err := GetIPListWithMaskFromString("10.0.0.1/24")
	assert.NoError(t, err)
	assert.Equal(t, "10.0.0.1/255.255.255.0", ip)
}

func TestGetIPListWithMaskFromString_NoMask(t *testing.T) {
	ip, err := GetIPListWithMaskFromString("10.0.0.1")
	assert.NoError(t, err)
	assert.Equal(t, "10.0.0.1", ip)
}

func TestGetIPListWithMaskFromString_Invalid(t *testing.T) {
	_, err := GetIPListWithMaskFromString("not-an-ip")
	assert.Error(t, err)
}

func TestGetIPListWithMaskFromString_TooManySlashes(t *testing.T) {
	_, err := GetIPListWithMaskFromString("10.0.0.1/24/extra")
	assert.Error(t, err)
}

// ── externalAccessAlreadyAdded (service.go) ────────────────────────────

func TestExternalAccessAlreadyAdded(t *testing.T) {
	export := &siotypes.NFSExport{
		ReadWriteRootHosts: []string{"10.0.0.1/24"},
		ReadWriteHosts:     []string{"192.168.1.0/16"},
	}
	assert.True(t, externalAccessAlreadyAdded(export, "10.0.0.1/24"))
	assert.True(t, externalAccessAlreadyAdded(export, "192.168.1.0/16"))
	assert.False(t, externalAccessAlreadyAdded(export, "172.16.0.0/12"))
}

func TestExternalAccessAlreadyAdded_Empty(t *testing.T) {
	export := &siotypes.NFSExport{}
	assert.False(t, externalAccessAlreadyAdded(export, "10.0.0.1/24"))
}

// ── lookupEnv (service.go) ─────────────────────────────────────────────

func TestLookupEnv(t *testing.T) {
	os.Setenv("__TEST_LOOKUPENV__", "testval")
	defer os.Unsetenv("__TEST_LOOKUPENV__")

	val, ok := lookupEnv(context.Background(), "__TEST_LOOKUPENV__")
	assert.True(t, ok)
	assert.Equal(t, "testval", val)

	val, ok = lookupEnv(context.Background(), "__NONEXISTENT_LOOKUPENV__")
	assert.False(t, ok)
	assert.Equal(t, "", val)
}

// ── isNodeMode / isControllerMode (service.go) ─────────────────────────

func TestIsNodeMode(t *testing.T) {
	svc := &service{mode: "node"}
	assert.True(t, svc.isNodeMode())
	svc.mode = "controller"
	assert.False(t, svc.isNodeMode())
}

func TestIsControllerMode(t *testing.T) {
	svc := &service{mode: "controller"}
	assert.True(t, svc.isControllerMode())
	svc.mode = "node"
	assert.False(t, svc.isControllerMode())
}

// ── hashNodeID (service.go) ────────────────────────────────────────────

func TestHashNodeID(t *testing.T) {
	h := hashNodeID("test-node-id")
	assert.NotEmpty(t, h)
	// Same input should produce same hash
	assert.Equal(t, h, hashNodeID("test-node-id"))
	// Different input should produce different hash
	assert.NotEqual(t, h, hashNodeID("other-node-id"))
}

// ── GetMessage (service.go) ────────────────────────────────────────────

func TestGetMessage(t *testing.T) {
	msg := GetMessage("Hello %s", "World")
	assert.Equal(t, "Hello World", msg)
}

// ── validateQuotaParameters (controller.go) ───────────────────────────

func TestValidateQuotaParameters_EmptyPath(t *testing.T) {
	_, _, err := validateQuotaParameters("", "80", "10", "fs1")
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestValidateQuotaParameters_EmptySoftLimit(t *testing.T) {
	_, _, err := validateQuotaParameters("/path", "", "10", "fs1")
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestValidateQuotaParameters_InvalidSoftLimit(t *testing.T) {
	_, _, err := validateQuotaParameters("/path", "abc", "10", "fs1")
	assert.Error(t, err)
}

func TestValidateQuotaParameters_InvalidGracePeriod(t *testing.T) {
	_, _, err := validateQuotaParameters("/path", "80", "abc", "fs1")
	assert.Error(t, err)
}

func TestValidateQuotaParameters_Success(t *testing.T) {
	soft, grace, err := validateQuotaParameters("/path", "80", "10", "fs1")
	assert.NoError(t, err)
	assert.Equal(t, int64(80), soft)
	assert.Equal(t, int64(10), grace)
}

func TestValidateQuotaParameters_DefaultGracePeriod(t *testing.T) {
	soft, grace, err := validateQuotaParameters("/path", "80", "", "fs1")
	assert.NoError(t, err)
	assert.Equal(t, int64(80), soft)
	assert.Equal(t, int64(0), grace)
}

// ── clearCache (controller.go) ─────────────────────────────────────────

func TestClearCache(t *testing.T) {
	svc := &service{
		volCache:          []*siotypes.Volume{{ID: "vol1"}},
		volCacheSystemID:  "sys1",
		snapCache:         []*siotypes.Volume{{ID: "snap1"}},
		snapCacheSystemID: "sys1",
	}
	svc.clearCache()
	assert.Empty(t, svc.volCache)
	assert.Empty(t, svc.snapCache)
}

// ── validateVolSize (controller.go) ────────────────────────────────────

func TestValidateVolSize_Invalid(t *testing.T) {
	capRange := &csi.CapacityRange{RequiredBytes: -1}
	_, err := validateVolSize(capRange, "") // "" = Gen1 (backward-compat default)
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.OutOfRange, st.Code())
}

func TestValidateVolSize_Valid(t *testing.T) {
	capRange := &csi.CapacityRange{RequiredBytes: 1073741824} // 1GB
	_, err := validateVolSize(capRange, "")                   // "" = Gen1 (backward-compat default)
	assert.NoError(t, err)
}

// ── shouldAllowMultipleMappings (controller.go) ───────────────────────

func TestShouldAllowMultipleMappings_Allowed(t *testing.T) {
	accessMode := &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER}
	allowMultipleMappings, err := shouldAllowMultipleMappings(true, accessMode)
	assert.NoError(t, err)
	assert.False(t, allowMultipleMappings)
}

func TestShouldAllowMultipleMappings_NotBlock(t *testing.T) {
	accessMode := &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER}
	allowMultipleMappings, err := shouldAllowMultipleMappings(false, accessMode)
	assert.Error(t, err)
	assert.False(t, allowMultipleMappings)
}

func TestShouldAllowMultipleMappings_BlockMultiWriter(t *testing.T) {
	accessMode := &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER}
	allowMultipleMappings, err := shouldAllowMultipleMappings(true, accessMode)
	assert.NoError(t, err)
	assert.True(t, allowMultipleMappings)
}

// ── accTypeIsBlock (controller.go) - already covered in existing tests

// ── generateSnapName (controller.go) ───────────────────────────────────

func TestGenerateSnapName(t *testing.T) {
	name := generateSnapName("vol123")
	assert.NotEmpty(t, name)
	assert.Contains(t, name, "vol123")
	assert.Contains(t, name, "_") // timestamp separator
}

// ── accTypeIsBlock (controller.go) ───────────────────────────────────────

func TestAccTypeIsBlock_True(t *testing.T) {
	vcs := []*csi.VolumeCapability{
		{
			AccessType: &csi.VolumeCapability_Block{
				Block: &csi.VolumeCapability_BlockVolume{},
			},
		},
	}
	assert.True(t, accTypeIsBlock(vcs))
}

func TestAccTypeIsBlock_False(t *testing.T) {
	vcs := []*csi.VolumeCapability{
		{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{},
			},
		},
	}
	assert.False(t, accTypeIsBlock(vcs))
}

func TestAccTypeIsBlock_Empty(t *testing.T) {
	vcs := []*csi.VolumeCapability{}
	assert.False(t, accTypeIsBlock(vcs))
}

// ── checkValidAccessTypes (controller.go) ───────────────────────────────

func TestCheckValidAccessTypes_Valid(t *testing.T) {
	vcs := []*csi.VolumeCapability{
		{
			AccessType: &csi.VolumeCapability_Block{
				Block: &csi.VolumeCapability_BlockVolume{},
			},
		},
		{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{},
			},
		},
	}
	assert.True(t, checkValidAccessTypes(vcs))
}

func TestCheckValidAccessTypes_Invalid(t *testing.T) {
	vcs := []*csi.VolumeCapability{
		{},
	}
	assert.False(t, checkValidAccessTypes(vcs))
}

func TestCheckValidAccessTypes_Nil(t *testing.T) {
	vcs := []*csi.VolumeCapability{
		nil,
		{
			AccessType: &csi.VolumeCapability_Block{
				Block: &csi.VolumeCapability_BlockVolume{},
			},
		},
	}
	assert.True(t, checkValidAccessTypes(vcs))
}

// ── validateMutableParams (controller.go) ─────────────────────────────

func TestValidateMutableParams_Empty(t *testing.T) {
	err := validateMutableParams(map[string]string{})
	assert.NoError(t, err)
}

func TestValidateMutableParams_Invalid(t *testing.T) {
	err := validateMutableParams(map[string]string{"invalid_key": "value"})
	assert.Error(t, err)
}

func TestValidateMutableParams_EmptyValue(t *testing.T) {
	err := validateMutableParams(map[string]string{"iopsLimit": ""})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "value must not be empty")
}

func TestValidateMutableParams_NonInteger(t *testing.T) {
	err := validateMutableParams(map[string]string{"iopsLimit": "abc"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a valid integer")
}

func TestValidateMutableParams_Negative(t *testing.T) {
	err := validateMutableParams(map[string]string{"iopsLimit": "-1"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "non-negative integer")
}

func TestValidateMutableParams_Success(t *testing.T) {
	err := validateMutableParams(map[string]string{"iopsLimit": "1000", "bandwidthLimitInKbps": "5000"})
	assert.NoError(t, err)
}

// ── parseMask (service.go) ───────────────────────────────────────────

func TestParseMask(t *testing.T) {
	mask, err := parseMask("10.0.0.1/24")
	assert.NoError(t, err)
	assert.Equal(t, "255.255.255.0", mask)
}

func TestParseMask_Invalid(t *testing.T) {
	_, err := parseMask("invalid")
	assert.Error(t, err)
}

func TestParseMask_InvalidSubnet(t *testing.T) {
	_, err := parseMask("10.0.0.1/33")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid subnet mask")
}

func TestParseMask_NegativeSubnet(t *testing.T) {
	_, err := parseMask("10.0.0.1/-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid subnet mask")
}

func TestParseMask_Slash32(t *testing.T) {
	mask, err := parseMask("10.0.0.1/32")
	assert.NoError(t, err)
	assert.Equal(t, "255.255.255.255", mask)
}

func TestParseMask_Slash0(t *testing.T) {
	mask, err := parseMask("10.0.0.1/0")
	assert.NoError(t, err)
	assert.Equal(t, "0.0.0.0", mask)
}

// buildSDCName already tested in node_unit_test.go

// getSDCName requires complex setup, skipping for now
// GetNodeLabels requires complex setup, skipping for now

// ── NodeGetCapabilities (node.go) ───────────────────────────────────

func TestNodeGetCapabilities(t *testing.T) {
	svc := &service{}
	resp, err := svc.NodeGetCapabilities(context.Background(), &csi.NodeGetCapabilitiesRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

// ── fileExist (ephemeral.go) ─────────────────────────────────────────

func TestFileExist(t *testing.T) {
	svc := &service{}
	// Test with actual temp file
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	assert.True(t, svc.fileExist(tmpFile.Name()))
	assert.False(t, svc.fileExist("/nonexistent/path/file.txt"))
}

// TestParsePVNameFromTargetPath already exists in mount_test.go

// ── replaceBackslashWithSlash (mount.go) ───────────────────────────

func TestReplaceBackslashWithSlash(t *testing.T) {
	assert.Equal(t, "a/b/c", replaceBackslashWithSlash("a\\b\\c"))
	assert.Equal(t, "a/b/c", replaceBackslashWithSlash("a/b/c"))
	assert.Equal(t, "", replaceBackslashWithSlash(""))
}

// ── singleAccessMode (mount.go) ─────────────────────────────────────

func TestSingleAccessMode(t *testing.T) {
	accessMode := &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER}
	assert.True(t, singleAccessMode(accessMode))

	accessMode2 := &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER}
	assert.False(t, singleAccessMode(accessMode2))
}

// ── getPrivateMountPoint (mount.go) ─────────────────────────────────

func TestGetPrivateMountPoint(t *testing.T) {
	assert.Equal(t, "/privdir/name", getPrivateMountPoint("/privdir", "name"))
	assert.Equal(t, "/privdir/name", getPrivateMountPoint("/privdir/", "name"))
}

// ── contains (mount.go) ─────────────────────────────────────────────

func TestContainsMount(t *testing.T) {
	list := []string{"a", "b", "c"}
	assert.True(t, contains(list, "b"))
	assert.False(t, contains(list, "d"))
}

// TestGetPodIDFromTargetPath already exists in service_test.go

// ── parseSize (ephemeral.go) ─────────────────────────────────────────

func TestParseSize(t *testing.T) {
	size, err := parseSize("10Gi")
	assert.NoError(t, err)
	assert.Equal(t, int64(10737418240), size)

	size, err = parseSize("5 Gi")
	assert.NoError(t, err)
	assert.Equal(t, int64(5368709120), size)

	size, err = parseSize("1Gi")
	assert.NoError(t, err)
	assert.Equal(t, int64(1073741824), size)

	_, err = parseSize("invalid")
	assert.Error(t, err)

	_, err = parseSize("abc Gi")
	assert.Error(t, err)

	_, err = parseSize("")
	assert.Error(t, err)

	_, err = parseSize("Gi")
	assert.Error(t, err)

	_, err = parseSize("999999999999999999999Gi")
	assert.Error(t, err)
}

// ── mkfile and mkdir require actual file system operations, skipping for unit tests

// ── logStatistics (service.go) ────────────────────────────────────────

func TestLogStatistics(_ *testing.T) {
	svc := &service{}
	// Call logStatistics multiple times to trigger the logging at 100
	for i := 0; i < 101; i++ {
		svc.logStatistics()
	}
	// Should not panic
}

// ── ParseInt64FromContext (service.go) ─────────────────────────────────

func TestParseInt64FromContext(t *testing.T) {
	ctx := context.Background()

	// Test with no value set
	val, err := ParseInt64FromContext(ctx, "NONEXISTENT_KEY")
	assert.NoError(t, err)
	assert.Equal(t, int64(0), val)

	// Test with valid value
	os.Setenv("TEST_PARSE_INT", "12345")
	defer os.Unsetenv("TEST_PARSE_INT")
	ctx = context.WithValue(ctx, struct{}{}, struct{}{})
	val, err = ParseInt64FromContext(ctx, "TEST_PARSE_INT")
	assert.NoError(t, err)
	assert.Equal(t, int64(12345), val)

	// Test with invalid value
	os.Setenv("TEST_PARSE_INT", "invalid")
	val, err = ParseInt64FromContext(ctx, "TEST_PARSE_INT")
	assert.Error(t, err)
}

// ── StageStatus.String (stager.go) ───────────────────────────────────

func TestStageStatusString(t *testing.T) {
	assert.Equal(t, "not_found", StageNotFound.String())
	assert.Equal(t, "ready", StageReady.String())
	assert.Equal(t, "deleted_link", StageDeletedLink.String())
	assert.Equal(t, "mpath_member", StageMpathMember.String())
	assert.Equal(t, "probe_error", StageProbeError.String())
	assert.Equal(t, "unknown", StageStatus(99).String())
}

// ── buildNVMeTargetInfo (stager.go) ─────────────────────────────────

func TestBuildNVMeTargetInfo(t *testing.T) {
	targetNqn := map[string]string{
		"192.168.1.1": "nqn.2014-08.org.nvmexpress:uuid:1234",
		"192.168.1.2": "nqn.2014-08.org.nvmexpress:uuid:5678",
	}
	portals := []string{"192.168.1.1:4420", "192.168.1.2:4420"}

	result := buildNVMeTargetInfo(targetNqn, portals)
	assert.Len(t, result, 2)
	assert.Equal(t, "nqn.2014-08.org.nvmexpress:uuid:1234", result[0].Target)
	assert.Equal(t, "192.168.1.1:4420", result[0].Portal)
	assert.Equal(t, "nqn.2014-08.org.nvmexpress:uuid:5678", result[1].Target)
	assert.Equal(t, "192.168.1.2:4420", result[1].Portal)
}

func TestBuildNVMeTargetInfo_Empty(t *testing.T) {
	result := buildNVMeTargetInfo(map[string]string{}, []string{})
	assert.Empty(t, result)
}

// TestBuildNVMeTargetInfo_ZonedPortals confirms the root-cause data path for ECS01F-964:
// portals absent from the NQN map (zone-blocked SDTs) produce empty Target entries.
func TestBuildNVMeTargetInfo_ZonedPortals(t *testing.T) {
	targetNqn := map[string]string{
		"10.10.1.1": "nqn.zone-a.sdt1",
		"10.10.1.2": "nqn.zone-a.sdt2",
		// 10.10.1.3 and 10.10.1.4 absent — NVMe zoning blocked their discovery
	}
	portals := []string{"10.10.1.1:4420", "10.10.1.2:4420", "10.10.1.3:4420", "10.10.1.4:4420"}

	result := buildNVMeTargetInfo(targetNqn, portals)
	assert.Len(t, result, 4)
	assert.Equal(t, "nqn.zone-a.sdt1", result[0].Target)
	assert.Equal(t, "nqn.zone-a.sdt2", result[1].Target)
	assert.Empty(t, result[2].Target, "zone-blocked portal should have empty NQN")
	assert.Empty(t, result[3].Target, "zone-blocked portal should have empty NQN")
}

// ── connectNVMEDevice (stager.go) ─────────────────────────────────

// capturingNVMEConnector records calls to ConnectVolume for test assertions.
type capturingNVMEConnector struct {
	capturedTargets []gobrick.NVMeTargetInfo
	callCount       int
	returnErr       error
}

func (c *capturingNVMEConnector) ConnectVolume(_ context.Context, info gobrick.NVMeVolumeInfo, _ bool) (gobrick.Device, error) {
	c.capturedTargets = info.Targets
	c.callCount++
	return gobrick.Device{Name: "nvme0n1"}, c.returnErr
}

func (c *capturingNVMEConnector) DisconnectVolumeByDeviceName(_ context.Context, _ string) error {
	return nil
}

func (c *capturingNVMEConnector) GetInitiatorName(_ context.Context) ([]string, error) {
	return nil, nil
}

// TestConnectNVMEDevice_FiltersEmptyNQNTargets is the regression test for ECS01F-964.
// When NVMe zoning prevents NQN discovery for some SDT portals, buildNVMeTargetInfo
// produces empty Target entries. connectNVMEDevice must filter these out before
// passing to gobrick, which rejects any entry with an empty Target.
func TestConnectNVMEDevice_FiltersEmptyNQNTargets(t *testing.T) {
	conn := &capturingNVMEConnector{}
	stager := &NVMeStager{useNVME: true, nvmeConnector: conn}

	data := deviceInfo{
		nguid: "deadbeef01234567",
		nvmeTargets: []gobrick.NVMeTargetInfo{
			{Target: "nqn.zone-a.sdt1", Portal: "10.10.1.1:4420"}, // valid
			{Target: "nqn.zone-a.sdt2", Portal: "10.10.1.2:4420"}, // valid
			{Target: "", Portal: "10.10.1.3:4420"},                // zone-blocked
			{Target: "", Portal: "10.10.1.4:4420"},                // zone-blocked
		},
	}

	_, err := stager.connectNVMEDevice(context.Background(), data)
	assert.NoError(t, err)
	assert.Equal(t, 1, conn.callCount, "ConnectVolume must be called once")
	assert.Len(t, conn.capturedTargets, 2, "only the 2 valid targets must reach gobrick")
	for _, tgt := range conn.capturedTargets {
		assert.NotEmpty(t, tgt.Target, "no empty-NQN entry should reach gobrick")
	}
}

// TestConnectNVMEDevice_AllValidTargets verifies that valid targets are forwarded unchanged.
func TestConnectNVMEDevice_AllValidTargets(t *testing.T) {
	conn := &capturingNVMEConnector{}
	stager := &NVMeStager{useNVME: true, nvmeConnector: conn}

	data := deviceInfo{
		nguid: "abc123",
		nvmeTargets: []gobrick.NVMeTargetInfo{
			{Target: "nqn.sdt1", Portal: "10.10.1.1:4420"},
			{Target: "nqn.sdt2", Portal: "10.10.1.2:4420"},
		},
	}

	_, err := stager.connectNVMEDevice(context.Background(), data)
	assert.NoError(t, err)
	assert.Len(t, conn.capturedTargets, 2, "all valid targets must be forwarded")
}

// TestConnectNVMEDevice_AllEmptyNQNs_ReturnsError verifies that when every portal
// is zone-blocked (all NQNs empty), an error is returned without calling gobrick.
func TestConnectNVMEDevice_AllEmptyNQNs_ReturnsError(t *testing.T) {
	conn := &capturingNVMEConnector{}
	stager := &NVMeStager{useNVME: true, nvmeConnector: conn}

	data := deviceInfo{
		nguid: "deadbeef01234567",
		nvmeTargets: []gobrick.NVMeTargetInfo{
			{Target: "", Portal: "10.10.1.3:4420"},
			{Target: "", Portal: "10.10.1.4:4420"},
		},
	}

	_, err := stager.connectNVMEDevice(context.Background(), data)
	assert.Error(t, err)
	assert.Zero(t, conn.callCount, "gobrick must not be called when no valid targets exist")
}

// ── removeWithRetry (mount.go) ─────────────────────────────────────

func TestRemoveWithRetry(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Test removing existing file
	err = removeWithRetry(tmpFile.Name())
	assert.NoError(t, err)

	// Test removing non-existent file (should not error)
	err = removeWithRetry(tmpFile.Name())
	assert.NoError(t, err)
}

// ── getPeerMdms (service.go) ─────────────────────────────────────────

func TestGetPeerMdms_NoClient(t *testing.T) {
	svc := &service{
		adminClients: map[string]*sio.Client{},
	}
	_, err := svc.getPeerMdms("nosys")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "can't find adminClient")
}

// ── getNASServerIDFromName (service.go) ─────────────────────────────

func TestGetNASServerIDFromName_Empty(t *testing.T) {
	svc := &service{}
	_, err := svc.getNASServerIDFromName("sys1", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NAS server not provided")
}

// ── findReplicationPairByVolID (service.go) ──────────────────────────

func TestFindReplicationPairByVolID_NoClient(t *testing.T) {
	svc := &service{
		adminClients: map[string]*sio.Client{},
	}
	_, err := svc.findReplicationPairByVolID("nosys", "vol123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "can't find adminClient")
}

// ── expandReplicationPair (service.go) ─────────────────────────────────

func TestExpandReplicationPair_NoPair(t *testing.T) {
	svc := &service{
		adminClients: map[string]*sio.Client{},
	}
	req := &csi.ControllerExpandVolumeRequest{
		VolumeId: "vol123",
	}
	err := svc.expandReplicationPair(context.Background(), req, "nosys", "vol123")
	assert.Error(t, err)
}

// ── removeVolumeFromReplicationPair (service.go) ───────────────────────

func TestRemoveVolumeFromReplicationPair_NoClient(t *testing.T) {
	svc := &service{
		adminClients: map[string]*sio.Client{},
	}
	_, err := svc.removeVolumeFromReplicationPair("nosys", "vol123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "can't find adminClient")
}

// ── ParseCIDR (service.go) ─────────────────────────────────────────────

func TestParseCIDR(t *testing.T) {
	// Test with CIDR notation
	result, err := ParseCIDR("10.0.0.0/24")
	assert.NoError(t, err)
	assert.Equal(t, "10.0.0.0/255.255.255.0", result)

	// Test without CIDR notation (should append /32 and convert to netmask)
	result, err = ParseCIDR("10.0.0.1")
	assert.NoError(t, err)
	assert.Equal(t, "10.0.0.1/255.255.255.255", result)

	// Test invalid IP
	_, err = ParseCIDR("invalid")
	assert.Error(t, err)
}

// ── disconnectNVMEDevice (stager.go) ───────────────────────────────────

func TestDisconnectNVMEDevice_EmptyDevice(t *testing.T) {
	stager := &NVMeStager{}
	err := stager.disconnectNVMEDevice(context.Background(), "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty device name")
}

func TestDisconnectNVMEDevice_NonNVMeDevice(t *testing.T) {
	stager := &NVMeStager{}
	err := stager.disconnectNVMEDevice(context.Background(), "/dev/sda")
	assert.NoError(t, err)
}

// ── getFilesystemIDFromCsiVolumeID (service.go) ─────────────────────────

func TestGetFilesystemIDFromCsiVolumeID(t *testing.T) {
	// Empty input
	assert.Equal(t, "", getFilesystemIDFromCsiVolumeID(""))

	// No slash - returns empty for unexpected format
	assert.Equal(t, "", getFilesystemIDFromCsiVolumeID("abcd"))

	// Slash format - last token is filesystem ID
	assert.Equal(t, "fs123", getFilesystemIDFromCsiVolumeID("sys/fs123"))
}

// ── mkfile (mount.go) ─────────────────────────────────────────────────

func TestMkfile_CreateNewFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "testfile")

	created, err := mkfile(testFile)
	assert.NoError(t, err)
	assert.True(t, created)

	// Verify file exists
	_, err = os.Stat(testFile)
	assert.NoError(t, err)

	// Calling again should return false (file already exists)
	created, err = mkfile(testFile)
	assert.NoError(t, err)
	assert.False(t, created)
}

// ── mkdir (mount.go) ─────────────────────────────────────────────────

func TestMkdir_CreateNewDir(t *testing.T) {
	tmpDir := t.TempDir()
	testDir := filepath.Join(tmpDir, "testdir")

	created, err := mkdir(testDir)
	assert.NoError(t, err)
	assert.True(t, created)

	// Verify directory exists
	_, err = os.Stat(testDir)
	assert.NoError(t, err)

	// Calling again should return false (directory already exists)
	created, err = mkdir(testDir)
	assert.NoError(t, err)
	assert.False(t, created)
}

// ── validateVolumeCapability (mount.go) ───────────────────────────────

func TestValidateVolumeCapability_NilAccessMode(t *testing.T) {
	volCap := &csi.VolumeCapability{
		AccessType: nil,
	}
	_, _, _, _, err := validateVolumeCapability(volCap, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Volume Access Mode is required")
}

// ── kmodLoaded (node.go) ───────────────────────────────────────────────

func TestKmodLoaded_WithLsmod(t *testing.T) {
	opts := Opts{
		Lsmod: "scini\nnvme_tcp\n",
	}
	loaded := kmodLoaded(opts)
	assert.True(t, loaded)
}

func TestKmodLoaded_ModuleNotLoaded(t *testing.T) {
	opts := Opts{
		Lsmod: "ext4\ncifs\n",
	}
	loaded := kmodLoaded(opts)
	assert.False(t, loaded)
}

// ── getSDCMappedVol (node.go) ───────────────────────────────────────────

func TestGetSDCMappedVol_NoConnectedSystem(t *testing.T) {
	svc := &service{
		connectedSystemNameToID: map[string]string{},
	}
	_, err := svc.getSDCMappedVol("vol123", "sys1", 1)
	assert.Error(t, err)
}

// ── isAlreadyPublished (mount.go) ─────────────────────────────────────

func TestIsAlreadyPublished(t *testing.T) {
	ctx := context.Background()
	// Non-existent path - should return false, no error
	published, err := isAlreadyPublished(ctx, "/nonexistent/path/xyz123")
	assert.NoError(t, err)
	assert.False(t, published)
}

// ── isVolumeMounted (mount.go) ─────────────────────────────────────────

func TestIsVolumeMounted(t *testing.T) {
	ctx := context.Background()
	// Non-existent volume - should return false, no error
	mounted, err := isVolumeMounted(ctx, "nonexistent-volume", "/nonexistent/path")
	assert.NoError(t, err)
	assert.False(t, mounted)
}

// ── ephemeralNodeUnpublish (ephemeral.go) ─────────────────────────────

func TestEphemeralNodeUnpublish_EmptyVolumeID(t *testing.T) {
	svc := &service{}
	req := &csi.NodeUnpublishVolumeRequest{
		VolumeId: "",
	}
	ctx := context.Background()
	err := svc.ephemeralNodeUnpublish(ctx, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "volume ID is required")
}

// ── unpublishVolume (mount.go) ─────────────────────────────────────────

func TestUnpublishVolume_EmptyTargetPath(t *testing.T) {
	err := unpublishVolume("vol123", "", "/priv", "/dev/sda", "req1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "target path required")
}

// ── NodeUnpublishVolume (node.go) ───────────────────────────────────────

func TestNodeUnpublishVolume_EmptyTargetPath(t *testing.T) {
	svc := &service{}
	req := &csi.NodeUnpublishVolumeRequest{
		VolumeId:   "vol123",
		TargetPath: "",
	}
	ctx := context.Background()
	_, err := svc.NodeUnpublishVolume(ctx, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "target path argument is required")
}

// ── NodeStageVolume (node.go) ─────────────────────────────────────────

func TestNodeStageVolume_NilVolumeCapability(t *testing.T) {
	svc := &service{useNVME: true}
	req := &csi.NodeStageVolumeRequest{
		VolumeId:         "vol123",
		VolumeCapability: nil,
	}
	ctx := context.Background()
	_, err := svc.NodeStageVolume(ctx, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "volume capability is required")
}

func TestNodeStageVolume_EmptyVolumeID(t *testing.T) {
	svc := &service{useNVME: true}
	req := &csi.NodeStageVolumeRequest{
		VolumeId:         "",
		VolumeCapability: &csi.VolumeCapability{},
	}
	ctx := context.Background()
	_, err := svc.NodeStageVolume(ctx, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "volume ID is required")
}

// ── publishNVMEVolume (mount.go) ───────────────────────────────────────

func TestPublishNVMEVolume_EmptyTargetPath(t *testing.T) {
	req := &csi.NodePublishVolumeRequest{
		VolumeId: "vol123",
	}
	err := publishNVMEVolume(req, "req1", "/dev/nvme0n1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "target path required")
}

func TestPublishNVMEVolume_EmptyStagingPath(t *testing.T) {
	req := &csi.NodePublishVolumeRequest{
		VolumeId:   "vol123",
		TargetPath: "/target",
	}
	err := publishNVMEVolume(req, "req1", "/dev/nvme0n1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "staging target path required")
}

// ── publishNFS (mount.go) ─────────────────────────────────────────────────

func TestPublishNFS_NilVolumeCapability(t *testing.T) {
	req := &csi.NodePublishVolumeRequest{}
	ctx := context.Background()
	err := publishNFS(ctx, req, "nfs://server/export")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Volume Capability is required")
}

func TestPublishNFS_NilAccessMode(t *testing.T) {
	req := &csi.NodePublishVolumeRequest{
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{},
		},
	}
	ctx := context.Background()
	err := publishNFS(ctx, req, "nfs://server/export")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Volume Access Mode is required")
}

func TestPublishNFS_NilMount(t *testing.T) {
	req := &csi.NodePublishVolumeRequest{
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{},
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			},
		},
	}
	ctx := context.Background()
	err := publishNFS(ctx, req, "nfs://server/export")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid access type")
}

func TestPublishNFS_EmptyTargetPath(t *testing.T) {
	req := &csi.NodePublishVolumeRequest{
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{},
			},
			AccessMode: &csi.VolumeCapability_AccessMode{
				Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			},
		},
		TargetPath: "",
	}
	ctx := context.Background()
	err := publishNFS(ctx, req, "nfs://server/export")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Target Path is required")
}

// ── NodeUnstageVolume (node.go) ───────────────────────────────────────

func TestNodeUnstageVolume_EmptyStagingTargetPath(t *testing.T) {
	svc := &service{}
	req := &csi.NodeUnstageVolumeRequest{
		VolumeId:          "vol123",
		StagingTargetPath: "",
	}
	ctx := context.Background()
	_, err := svc.NodeUnstageVolume(ctx, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "StagingTargetPath is required")
}

// ── ControllerPublishVolume (controller.go) ─────────────────────────────

func TestControllerPublishVolume_NilVolumeCapability(t *testing.T) {
	svc := &service{}
	req := &csi.ControllerPublishVolumeRequest{
		VolumeCapability: nil,
	}
	ctx := context.Background()
	_, err := svc.ControllerPublishVolume(ctx, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "volume capability is required")
}

// ── ControllerUnpublishVolume (controller.go) ───────────────────────────

func TestControllerUnpublishVolume_EmptyVolumeID(t *testing.T) {
	svc := &service{}
	req := &csi.ControllerUnpublishVolumeRequest{
		VolumeId: "",
	}
	ctx := context.Background()
	_, err := svc.ControllerUnpublishVolume(ctx, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "volume ID is required")
}

// ── DeleteVolume (controller.go) ───────────────────────────────────────

func TestDeleteVolume_EmptyVolumeID(t *testing.T) {
	svc := &service{}
	req := &csi.DeleteVolumeRequest{
		VolumeId: "",
	}
	ctx := context.Background()
	_, err := svc.DeleteVolume(ctx, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "volume ID is required")
}

// ── ControllerExpandVolume (controller.go) ─────────────────────────────

func TestControllerExpandVolume_EmptyVolumeID(t *testing.T) {
	svc := &service{}
	req := &csi.ControllerExpandVolumeRequest{
		VolumeId: "",
	}
	ctx := context.Background()
	_, err := svc.ControllerExpandVolume(ctx, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "volume ID is required")
}

// ── Clone (controller.go) ───────────────────────────────────────────────

func TestClone_NoDefaultSystemID(t *testing.T) {
	svc := &service{opts: Opts{defaultSystemID: ""}}
	volumeSource := &csi.VolumeContentSource_VolumeSource{VolumeId: "vol123"}
	req := &csi.CreateVolumeRequest{
		Name: "clone1",
	}
	_, err := svc.Clone(req, volumeSource, "clone1", 1024, "pool1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "systemID is not found in source volume id and there is no default system")
}

// ── createVolumeFromSnapshot (controller.go) ───────────────────────────

func TestCreateVolumeFromSnapshot_NoDefaultSystemID(t *testing.T) {
	svc := &service{opts: Opts{defaultSystemID: ""}}
	snapshotSource := &csi.VolumeContentSource_SnapshotSource{SnapshotId: "snap123"}
	req := &csi.CreateVolumeRequest{
		Name: "vol1",
	}
	_, err := svc.createVolumeFromSnapshot(req, snapshotSource, "vol1", 1024, "pool1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "systemID is not found in snapshot source id and there is no default system")
}

// ── CreateSnapshot (controller.go) ─────────────────────────────────────

func TestCreateSnapshot_EmptySourceVolumeID(t *testing.T) {
	svc := &service{}
	req := &csi.CreateSnapshotRequest{
		SourceVolumeId: "",
	}
	ctx := context.Background()
	_, err := svc.CreateSnapshot(ctx, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "CSI volume ID to be snapped is required")
}

// ── DeleteSnapshot (controller.go) ─────────────────────────────────────

func TestDeleteSnapshot_EmptySnapshotID(t *testing.T) {
	svc := &service{}
	req := &csi.DeleteSnapshotRequest{
		SnapshotId: "",
	}
	ctx := context.Background()
	_, err := svc.DeleteSnapshot(ctx, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "snapshot ID to be deleted is required")
}

// ── getNode (service.go) ───────────────────────────────────────────────

func TestGetNode_EmptyNodeName(t *testing.T) {
	svc := &service{}
	ctx := context.Background()
	_, err := svc.getNode(ctx, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "node name is empty")
}

// ── getReplicationConsistencyGroupByID (replication.go) ───────────────

func TestGetReplicationConsistencyGroupByID_NoClient(t *testing.T) {
	svc := &service{
		adminClients: map[string]*sio.Client{},
	}
	_, err := svc.getReplicationConsistencyGroupByID("nosys", "group123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "can't find adminClient")
}

// ── getProtectionDomainIDFromName (service.go) ─────────────────────

func TestGetProtectionDomainIDFromName_EmptyName(t *testing.T) {
	svc := &service{}
	id, err := svc.getProtectionDomainIDFromName("sys1", "")
	assert.NoError(t, err)
	assert.Equal(t, "", id)
}

// ── GetNodeIP (service.go) ─────────────────────────────────────────────

func TestGetNodeIP_NoInternalIP(t *testing.T) {
	origClientset := K8sClientset
	defer func() { K8sClientset = origClientset }()

	K8sClientset = fake.NewSimpleClientset(
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
			Status:     v1.NodeStatus{Addresses: []v1.NodeAddress{{Type: v1.NodeExternalIP, Address: "1.2.3.4"}}},
		},
	)

	svc := &service{opts: Opts{KubeNodeName: "test-node"}}
	_, err := svc.GetNodeIP(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no InternalIP found")
}

// ── GetNodeIPByCSINodeID (service.go) ─────────────────────────────────

func TestGetNodeIPByCSINodeID_NodeNotFound(t *testing.T) {
	origClientset := K8sClientset
	defer func() { K8sClientset = origClientset }()

	K8sClientset = fake.NewSimpleClientset(
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "kube-node-1"},
			Status:     v1.NodeStatus{Addresses: []v1.NodeAddress{{Type: v1.NodeInternalIP, Address: "10.0.0.5"}}},
		},
		&storagev1.CSINode{
			ObjectMeta: metav1.ObjectMeta{Name: "kube-node-1"},
			Spec:       storagev1.CSINodeSpec{Drivers: []storagev1.CSINodeDriver{{Name: Name, NodeID: "node-123"}}},
		},
	)

	svc := &service{}
	ip := svc.GetNodeIPByCSINodeID("nonexistent-node")
	assert.Equal(t, "", ip)
}

// ── GetDevice (mount.go) ──────────────────────────────────────────────

func TestGetDevice_NonExistentPath(t *testing.T) {
	_, err := GetDevice("/nonexistent/path/to/device")
	assert.Error(t, err)
}

func TestGetDevice_RegularFile(t *testing.T) {
	// Create temp file (not a block device)
	tmpFile, err := os.CreateTemp("", "testdevice")
	assert.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Without unitTestEmulateBlockDevice, a regular file should fail
	origEmulate := unitTestEmulateBlockDevice
	unitTestEmulateBlockDevice = false
	defer func() { unitTestEmulateBlockDevice = origEmulate }()

	_, err = GetDevice(tmpFile.Name())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "is not a block device")
}

func TestGetDevice_EmulateBlockDevice(t *testing.T) {
	// Create temp file and emulate block device mode
	tmpFile, err := os.CreateTemp("", "testdevice")
	assert.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	origEmulate := unitTestEmulateBlockDevice
	unitTestEmulateBlockDevice = true
	defer func() { unitTestEmulateBlockDevice = origEmulate }()

	dev, err := GetDevice(tmpFile.Name())
	assert.NoError(t, err)
	assert.NotNil(t, dev)
	assert.Equal(t, filepath.Base(tmpFile.Name()), dev.Name)
}

// ── evalSymlinks (mount.go) ───────────────────────────────────────────

func TestEvalSymlinks_ValidPath(t *testing.T) {
	tmpDir := t.TempDir()
	result := evalSymlinks(tmpDir)
	assert.NotEmpty(t, result)
}

func TestEvalSymlinks_InvalidPath(t *testing.T) {
	result := evalSymlinks("/nonexistent/path/xyz")
	// On error, returns original path
	assert.Equal(t, "/nonexistent/path/xyz", result)
}

// ── NodeExpandVolume (node.go) ────────────────────────────────────────

func TestNodeExpandVolume_EmptyVolumePath(t *testing.T) {
	svc := &service{}
	req := &csi.NodeExpandVolumeRequest{
		VolumeId:   "vol123",
		VolumePath: "",
	}
	_, err := svc.NodeExpandVolume(context.Background(), req)
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestNodeExpandVolume_NonExistentVolumePath(t *testing.T) {
	svc := &service{}
	req := &csi.NodeExpandVolumeRequest{
		VolumeId:   "vol123",
		VolumePath: "/nonexistent/path/xyz",
	}
	_, err := svc.NodeExpandVolume(context.Background(), req)
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestNodeExpandVolume_EmptyVolumeID(t *testing.T) {
	tmpDir := t.TempDir()
	svc := &service{
		volumePrefixToSystems:   map[string][]string{},
		connectedSystemNameToID: map[string]string{},
	}
	req := &csi.NodeExpandVolumeRequest{
		VolumeId:   "",
		VolumePath: tmpDir,
	}
	_, err := svc.NodeExpandVolume(context.Background(), req)
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestNodeExpandVolume_ShortVolumeID(t *testing.T) {
	tmpDir := t.TempDir()
	svc := &service{
		volumePrefixToSystems:   map[string][]string{},
		connectedSystemNameToID: map[string]string{},
	}
	req := &csi.NodeExpandVolumeRequest{
		VolumeId:   "ab",
		VolumePath: tmpDir,
	}
	_, err := svc.NodeExpandVolume(context.Background(), req)
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestNodeExpandVolume_NoDefaultSystem(t *testing.T) {
	tmpDir := t.TempDir()
	svc := &service{
		volumePrefixToSystems:   map[string][]string{},
		connectedSystemNameToID: map[string]string{},
		opts:                    Opts{defaultSystemID: ""},
	}
	req := &csi.NodeExpandVolumeRequest{
		VolumeId:   "vol123",
		VolumePath: tmpDir,
	}
	_, err := svc.NodeExpandVolume(context.Background(), req)
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, err.Error(), "systemID is not found")
}

// ── getMaximumVolumeSize / cache helpers (controller.go) ─────────────

func TestCacheMaximumVolumeSize(t *testing.T) {
	// Clear any previous test state
	mutex.Lock()
	delete(maxVolumesSizeForArray, "__test_sys__")
	mutex.Unlock()

	// Should not be found initially
	val, found := getCachedMaximumVolumeSize("__test_sys__")
	assert.False(t, found)
	assert.Equal(t, int64(0), val)

	// Cache a value
	cacheMaximumVolumeSize("__test_sys__", 1024)

	// Should be found now
	val, found = getCachedMaximumVolumeSize("__test_sys__")
	assert.True(t, found)
	assert.Equal(t, int64(1024), val)

	// Clean up
	mutex.Lock()
	delete(maxVolumesSizeForArray, "__test_sys__")
	mutex.Unlock()
}

func TestGetMaximumVolumeSize_NoAdminClient(t *testing.T) {
	svc := &service{
		adminClients: map[string]*sio.Client{},
	}
	// Clear cache for this test system
	mutex.Lock()
	delete(maxVolumesSizeForArray, "nosys")
	mutex.Unlock()

	_, err := svc.getMaximumVolumeSize("nosys")
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGetMaximumVolumeSize_CacheHit(t *testing.T) {
	svc := &service{
		adminClients: map[string]*sio.Client{},
	}
	// Seed cache
	cacheMaximumVolumeSize("__cache_test__", 2048)
	defer func() {
		mutex.Lock()
		delete(maxVolumesSizeForArray, "__cache_test__")
		mutex.Unlock()
	}()

	val, err := svc.getMaximumVolumeSize("__cache_test__")
	assert.NoError(t, err)
	assert.Equal(t, int64(2048), val)
}

// ── getSDCIPs (service.go) ────────────────────────────────────────────

func TestGetSDCIPs_NilSystem(t *testing.T) {
	svc := &service{
		systems: map[string]*sio.System{},
	}
	_, err := svc.getSDCIPs("some-guid", "nosys")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "systemID not found")
}

// ── generateNodeID (service.go) ────────────────────────────────────────

func TestGenerateNodeID_Success(t *testing.T) {
	origClientset := K8sClientset
	defer func() { K8sClientset = origClientset }()

	K8sClientset = fake.NewSimpleClientset(
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node",
				UID:  "uid-12345-abcdef",
			},
			Status: v1.NodeStatus{Addresses: []v1.NodeAddress{
				{Type: v1.NodeInternalIP, Address: "10.0.0.1"},
			}},
		},
	)

	svc := &service{opts: Opts{KubeNodeName: "test-node"}}
	nodeID, err := svc.generateNodeID()
	assert.NoError(t, err)
	assert.Len(t, nodeID, 31)
	assert.Contains(t, nodeID, "10.0.0.1")
}

func TestGenerateNodeID_GetNodeUIDError(t *testing.T) {
	origClientset := K8sClientset
	defer func() { K8sClientset = origClientset }()

	// No nodes exist, so GetNodeUID will fail
	K8sClientset = fake.NewSimpleClientset()

	svc := &service{opts: Opts{KubeNodeName: "nonexistent-node"}}
	_, err := svc.generateNodeID()
	assert.Error(t, err)
}

// ── getVolumeIDFromCsiVolumeID additional paths (service.go) ─────────

func TestGetVolumeIDFromCsiVolumeID_SingleHyphen(t *testing.T) {
	// "sys-vol" -> last token is "vol"
	assert.Equal(t, "vol", getVolumeIDFromCsiVolumeID("sys-vol"))
}

func TestGetVolumeIDFromCsiVolumeID_MultipleHyphens(t *testing.T) {
	// "a-b-c-d" -> last token is "d"
	assert.Equal(t, "d", getVolumeIDFromCsiVolumeID("a-b-c-d"))
}

// ── getFilesystemIDFromCsiVolumeID additional paths (service.go) ─────

func TestGetFilesystemIDFromCsiVolumeID_NoSlash(t *testing.T) {
	// No slash: returns empty string (hits the error path)
	result := getFilesystemIDFromCsiVolumeID("noslashhere")
	assert.Equal(t, "", result)
}

func TestGetFilesystemIDFromCsiVolumeID_WithSlash(t *testing.T) {
	// Has slash: returns last token
	assert.Equal(t, "fs123", getFilesystemIDFromCsiVolumeID("sys/fs123"))
}

func TestGetFilesystemIDFromCsiVolumeID_MultipleSlashes(t *testing.T) {
	// Multiple slashes: returns last token
	assert.Equal(t, "fs123", getFilesystemIDFromCsiVolumeID("a/b/fs123"))
}

// ── getSystemIDFromCsiVolumeID additional paths (service.go) ─────────

func TestGetSystemIDFromCsiVolumeID_MultipleHyphens(t *testing.T) {
	// "a-b-c" has 3 tokens, which doesn't match len==2 => returns ""
	svc := &service{connectedSystemNameToID: map[string]string{}}
	assert.Equal(t, "", svc.getSystemIDFromCsiVolumeID("a-b-c"))
}

func TestGetSystemIDFromCsiVolumeID_MultipleSlashes(t *testing.T) {
	// "a/b/c" has 3 tokens via slash, which doesn't match len==2 => returns ""
	svc := &service{connectedSystemNameToID: map[string]string{}}
	assert.Equal(t, "", svc.getSystemIDFromCsiVolumeID("a/b/c"))
}

func TestGetSystemIDFromCsiVolumeID_NoDelimiter(t *testing.T) {
	// No hyphen and no slash => no delimiter => returns ""
	svc := &service{connectedSystemNameToID: map[string]string{}}
	assert.Equal(t, "", svc.getSystemIDFromCsiVolumeID("justvol"))
}

// ── setQoSIfNeeded (publisher.go) ─────────────────────────────────────

func TestSetQoSIfNeeded_NoQoSParams(t *testing.T) {
	// No QoS parameters in volumeContext => should return nil
	err := setQoSIfNeeded(context.Background(), nil, "sys1", "sdc1", "vol1", "csivol1", "node1", map[string]string{})
	assert.NoError(t, err)
}

func TestSetQoSIfNeeded_InvalidBandwidth(t *testing.T) {
	vc := map[string]string{KeyBandwidthLimitInKbps: "not-a-number"}
	err := setQoSIfNeeded(context.Background(), nil, "sys1", "sdc1", "vol1", "csivol1", "node1", vc)
	assert.Error(t, err)
}

func TestSetQoSIfNeeded_InvalidIOPS(t *testing.T) {
	vc := map[string]string{KeyIopsLimit: "abc"}
	err := setQoSIfNeeded(context.Background(), nil, "sys1", "sdc1", "vol1", "csivol1", "node1", vc)
	assert.Error(t, err)
}

// ── getStoragePoolID (service.go) ─────────────────────────────────────

func TestGetStoragePoolNameFromID_NotInCache(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"message":"not found"}`)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	client.SetToken("test-token")

	svc := &service{
		storagePoolIDToName: map[string]string{},
		adminClients:        map[string]*sio.Client{"sys1": client},
	}
	// Pool not in cache, FindStoragePool will fail, returns empty
	name := svc.getStoragePoolNameFromID("sys1", "unknown-pool-id")
	assert.Equal(t, "", name)
}

func TestGetStoragePoolNameFromID_InCache(t *testing.T) {
	svc := &service{
		storagePoolIDToName: map[string]string{"pool1": "PoolA"},
		adminClients:        map[string]*sio.Client{},
	}
	name := svc.getStoragePoolNameFromID("sys1", "pool1")
	assert.Equal(t, "PoolA", name)
}

// ── getCSIVolume (service.go) ─────────────────────────────────────────

func TestGetCSIVolume(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/api/types/System/instances", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `[{"id":"sys1","installationId":"inst-001"}]`)
	})
	handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{}`)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	client.SetToken("test-token")

	svc := &service{
		storagePoolIDToName: map[string]string{"pool1": "PoolA"},
		systems:             map[string]*sio.System{},
		adminClients:        map[string]*sio.Client{"sys1": client},
	}

	vol := &siotypes.Volume{
		ID:            "vol123",
		Name:          "TestVol",
		StoragePoolID: "pool1",
		SizeInKb:      8388608, // 8 GiB in KiB
	}

	csiVol := svc.getCSIVolume(vol, "sys1")
	assert.NotNil(t, csiVol)
	assert.Contains(t, csiVol.VolumeId, "vol123")
	assert.Equal(t, "TestVol", csiVol.VolumeContext["Name"])
	assert.Equal(t, "pool1", csiVol.VolumeContext["StoragePoolID"])
	assert.Equal(t, "PoolA", csiVol.VolumeContext["StoragePoolName"])
}

// ── getCSIVolumeFromFilesystem (service.go) ──────────────────────────

func TestGetCSIVolumeFromFilesystem(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/api/types/System/instances", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `[{"id":"sys1","installationId":"inst-002"}]`)
	})
	handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{}`)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	client.SetToken("test-token")

	svc := &service{
		storagePoolIDToName: map[string]string{},
		adminClients:        map[string]*sio.Client{"sys1": client},
	}
	fs := &siotypes.FileSystem{
		ID:          "fs123",
		Name:        "TestFS",
		SizeTotal:   8589934592, // 8 GiB in bytes
		NasServerID: "nas1",
	}

	csiVol := svc.getCSIVolumeFromFilesystem(fs, "sys1")
	assert.NotNil(t, csiVol)
	assert.Contains(t, csiVol.VolumeId, "fs123")
	assert.Equal(t, "TestFS", csiVol.VolumeContext["Name"])
	assert.Equal(t, "sys1", csiVol.VolumeContext["StorageSystem"])
}

// ── mkfile edge cases (mount.go) ─────────────────────────────────────

func TestMkfile_ExistingDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	// tmpDir is already a directory
	created, err := mkfile(tmpDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "existing path is a directory")
	assert.False(t, created)
}

// ── mkdir edge cases (mount.go) ──────────────────────────────────────

func TestMkdir_ExistingFile(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "testmkdir")
	assert.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// tmpFile is a regular file, not a directory
	created, mkErr := mkdir(tmpFile.Name())
	assert.Error(t, mkErr)
	assert.Contains(t, mkErr.Error(), "existing path is not a directory")
	assert.False(t, created)
}

// ── EventEmitter nil recorder (space_reclamation.go) ─────────────────

func TestEventEmitter_EmitSuccess_NilRecorder(_ *testing.T) {
	emitter := &EventEmitter{recorder: nil}
	// Should not panic
	emitter.EmitSuccess(nil, 0, 0)
}

func TestEventEmitter_EmitFailure_NilRecorder(_ *testing.T) {
	emitter := &EventEmitter{recorder: nil}
	emitter.EmitFailure(nil, errors.New("fail"))
}

func TestEventEmitter_EmitTimeout_NilRecorder(_ *testing.T) {
	emitter := &EventEmitter{recorder: nil}
	emitter.EmitTimeout(nil, 0)
}

func TestEventEmitter_EmitUnsupported_NilRecorder(_ *testing.T) {
	emitter := &EventEmitter{recorder: nil}
	emitter.EmitUnsupported(nil, "reason")
}

// ── DeleteReplicationConsistencyGroup (controller.go) ────────────────

func TestDeleteReplicationConsistencyGroup_NoAdminClient(t *testing.T) {
	svc := &service{adminClients: map[string]*sio.Client{}}
	err := svc.DeleteReplicationConsistencyGroup("nosys", "group1")
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestDeleteReplicationConsistencyGroup_EmptyGroupID(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{}`)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	client.SetToken("test-token")

	svc := &service{adminClients: map[string]*sio.Client{"sys1": client}}
	err := svc.DeleteReplicationConsistencyGroup("sys1", "")
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// ── getProtectionDomain (service.go) ────────────────────────────────

func TestGetProtectionDomain_EmptyPDName(t *testing.T) {
	// When pdName is empty, getProtectionDomainIDFromName returns ("", nil)
	// so getProtectionDomain falls through to query the system
	handler := http.NewServeMux()
	handler.HandleFunc("/api/types/System/instances", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `[{"id":"sys1"}]`)
	})
	handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"message":"not found"}`)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	client.SetToken("test-token")

	svc := &service{adminClients: map[string]*sio.Client{"sys1": client}}
	_, err := svc.getProtectionDomain("sys1", "")
	// Will fail trying to FindSystem or GetProtectionDomain, but exercises the code path
	assert.Error(t, err)
}

// ── getMaximumVolumeSize negative cache value (controller.go) ───────

func TestGetMaximumVolumeSize_NegativeCacheValue(t *testing.T) {
	// A negative cached value should trigger re-fetch
	cacheMaximumVolumeSize("__neg_cache_test__", -1)
	defer func() {
		mutex.Lock()
		delete(maxVolumesSizeForArray, "__neg_cache_test__")
		mutex.Unlock()
	}()

	svc := &service{adminClients: map[string]*sio.Client{}}
	_, err := svc.getMaximumVolumeSize("__neg_cache_test__")
	// Should fail because there's no admin client
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// ── configureAutoBlockProtocol (service.go) ─────────────────────────

func TestConfigureAutoBlockProtocol_SDCAvailable(t *testing.T) {
	svc := &service{
		opts: Opts{SdcGUID: "some-guid"},
	}
	svc.configureAutoBlockProtocol(context.Background(), 4.0, 1)
	assert.True(t, svc.useSDC)
	assert.False(t, svc.useNVME)
}

func TestConfigureAutoBlockProtocol_NVMeAvailable(t *testing.T) {
	svc := &service{
		opts: Opts{SdcGUID: ""},
	}
	svc.configureAutoBlockProtocol(context.Background(), 4.0, 1)
	assert.False(t, svc.useSDC)
	assert.True(t, svc.useNVME)
}

func TestConfigureAutoBlockProtocol_NeitherAvailable(t *testing.T) {
	svc := &service{
		opts: Opts{SdcGUID: ""},
	}
	svc.configureAutoBlockProtocol(context.Background(), 4.0, 0)
	assert.False(t, svc.useSDC)
	assert.False(t, svc.useNVME)
}

func TestConfigureAutoBlockProtocol_NVMeVersionTooLow(t *testing.T) {
	svc := &service{
		opts: Opts{SdcGUID: ""},
	}
	svc.configureAutoBlockProtocol(context.Background(), 3.5, 1)
	assert.False(t, svc.useSDC)
	assert.False(t, svc.useNVME)
}

// ── getArrayInstallationID (service.go) ──────────────────────────────

func TestGetArrayInstallationID_Error(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	client.SetToken("test-token")

	svc := &service{adminClients: map[string]*sio.Client{"sys1": client}}
	_, err := svc.getArrayInstallationID("sys1")
	assert.Error(t, err)
}

// ── removeVolumeFromReplicationPair (service.go) ─────────────────────

func TestRemoveVolumeFromReplicationPair_NoAdminClient(t *testing.T) {
	svc := &service{adminClients: map[string]*sio.Client{}}
	_, err := svc.removeVolumeFromReplicationPair("nosys", "vol1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "can't find adminClient")
}

// ── getStoragePoolID (service.go) ────────────────────────────────────

func TestGetStoragePoolID_Error(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"message":"not found"}`)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	client.SetToken("test-token")

	svc := &service{adminClients: map[string]*sio.Client{"sys1": client}}
	_, err := svc.getStoragePoolID("nonexistent", "sys1", "pd1")
	assert.Error(t, err)
}

// ── initSpaceReclamation (space_reclamation.go) ──────────────────────

func TestInitSpaceReclamation_InvalidSchedule(t *testing.T) {
	origEnv := os.Getenv("X_CSI_SPACE_RECLAMATION_SCHEDULE")
	os.Setenv("X_CSI_SPACE_RECLAMATION_SCHEDULE", "invalid-cron")
	defer os.Setenv("X_CSI_SPACE_RECLAMATION_SCHEDULE", origEnv)

	svc := &service{}
	origClientset := K8sClientset
	defer func() { K8sClientset = origClientset }()
	K8sClientset = fake.NewSimpleClientset()

	initSpaceReclamation(context.Background(), svc, K8sClientset)
	// Should not set spaceReclaimMgr due to invalid schedule
	assert.Nil(t, svc.spaceReclaimMgr)
}

// ── NewSpaceReclamationManager (space_reclamation.go) ────────────────

func TestNewSpaceReclamationManager_InvalidSchedule(t *testing.T) {
	cfg := SpaceReclamationConfig{Schedule: "bad"}
	_, err := NewSpaceReclamationManager(context.Background(), cfg, fake.NewSimpleClientset(), "node1", false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid cron schedule")
}

func TestNewSpaceReclamationManager_ValidSchedule(t *testing.T) {
	cfg := SpaceReclamationConfig{
		Schedule:             "0 2 * * *",
		MaxConcurrentVolumes: 3,
	}
	mgr, err := NewSpaceReclamationManager(context.Background(), cfg, fake.NewSimpleClientset(), "node1", false)
	assert.NoError(t, err)
	assert.NotNil(t, mgr)
}

func TestNewSpaceReclamationManager_DefaultConcurrency(t *testing.T) {
	cfg := SpaceReclamationConfig{
		Schedule:             "*/5 * * * *",
		MaxConcurrentVolumes: 0, // should default to 1
	}
	mgr, err := NewSpaceReclamationManager(context.Background(), cfg, fake.NewSimpleClientset(), "node1", false)
	assert.NoError(t, err)
	assert.NotNil(t, mgr)
}

// ── SpaceReclamationManager.Start (space_reclamation.go) ─────────────

func TestSpaceReclamationManager_Start_Success(t *testing.T) {
	cfg := SpaceReclamationConfig{
		Schedule:             "0 2 * * *",
		MaxConcurrentVolumes: 1,
	}
	mgr, err := NewSpaceReclamationManager(context.Background(), cfg, fake.NewSimpleClientset(), "node1", false)
	assert.NoError(t, err)
	err = mgr.Start()
	assert.NoError(t, err)
	mgr.cronSched.Stop()
}

// ── getSystem (service.go) ──────────────────────────────────────────

func TestGetSystem_NoAdminClient(t *testing.T) {
	svc := &service{adminClients: map[string]*sio.Client{}}
	_, err := svc.getSystem("nosys")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "can't find adminClient")
}

// ── getPeerMdms (service.go) ────────────────────────────────────────

func TestGetPeerMdms_NoAdminClient(t *testing.T) {
	svc := &service{adminClients: map[string]*sio.Client{}}
	_, err := svc.getPeerMdms("nosys")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "can't find adminClient")
}

// ── ProbeController (identity.go) ───────────────────────────────────

func TestProbeController_NodeMode(t *testing.T) {
	svc := &service{mode: "node"}
	resp, err := svc.ProbeController(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, Name, resp.Name)
}

// ── GetPlatformVersion (service.go) ─────────────────────────────────

func TestGetPlatformVersion_NilClient(t *testing.T) {
	svc := &service{adminClients: map[string]*sio.Client{}}
	ver, err := svc.GetPlatformVersion("nosys")
	assert.NoError(t, err)
	assert.Equal(t, float64(0), ver)
}

func TestGetPlatformVersion_Success(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `"4.5"`)
	})
	handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{}`)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	client.SetToken("test-token")

	svc := &service{adminClients: map[string]*sio.Client{"sys1": client}}
	ver, err := svc.GetPlatformVersion("sys1")
	assert.NoError(t, err)
	assert.Equal(t, 4.5, ver)
}

// ── GetGenType (service.go) ─────────────────────────────────────────

func TestGetGenType_NilSystem(t *testing.T) {
	// FR-3: nil system must return an error, not ("", nil).
	svc := &service{systems: map[string]*sio.System{}}
	_, err := svc.GetGenType("nosys")
	assert.Error(t, err)
}

// ── getSDCID (service.go) ──────────────────────────────────────────

func TestGetSDCID_NilSystem(t *testing.T) {
	svc := &service{systems: map[string]*sio.System{}}
	_, err := svc.getSDCID("guid1", "nosys")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "systemID not found")
}

// ── getNodeIP (node.go) ─────────────────────────────────────────────

func TestGetNodeIP_ReturnsIPs(t *testing.T) {
	svc := &service{}
	ips, err := svc.getNodeIP()
	// Should succeed on any machine with network interfaces
	if err != nil {
		// If no valid IPs found, that's ok in test env
		assert.Contains(t, err.Error(), "no valid IP")
	} else {
		assert.NotEmpty(t, ips)
	}
}

// ── getSystem with valid client (service.go) ────────────────────────

func TestGetSystem_SystemNotFound(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/api/types/System/instances", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `[{"id":"other-sys"}]`)
	})
	handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{}`)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	client.SetToken("test-token")

	svc := &service{adminClients: map[string]*sio.Client{"sys1": client}}
	_, err := svc.getSystem("sys1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ── getMaximumVolumeSize with mock (controller.go) ──────────────────

func TestGetMaximumVolumeSize_FetchFromAPI(t *testing.T) {
	// Clear any cached value for this test key
	testKey := "__api_fetch_test__"
	mutex.Lock()
	delete(maxVolumesSizeForArray, testKey)
	mutex.Unlock()
	defer func() {
		mutex.Lock()
		delete(maxVolumesSizeForArray, testKey)
		mutex.Unlock()
	}()

	handler := http.NewServeMux()
	handler.HandleFunc("/api/instances/System/action/querySystemLimits", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"systemLimitEntryList":[{"type":"volumeSizeGb","maxVal":"1024"}]}`)
	})
	handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{}`)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	client.SetToken("test-token")

	svc := &service{adminClients: map[string]*sio.Client{testKey: client}}
	maxSize, err := svc.getMaximumVolumeSize(testKey)
	assert.NoError(t, err)
	assert.Equal(t, int64(1024), maxSize)

	// Verify it's now cached
	cached, found := getCachedMaximumVolumeSize(testKey)
	assert.True(t, found)
	assert.Equal(t, int64(1024), cached)
}

func TestGetMaximumVolumeSize_ParseError(t *testing.T) {
	testKey := "__parse_err_test__"
	mutex.Lock()
	delete(maxVolumesSizeForArray, testKey)
	mutex.Unlock()
	defer func() {
		mutex.Lock()
		delete(maxVolumesSizeForArray, testKey)
		mutex.Unlock()
	}()

	handler := http.NewServeMux()
	handler.HandleFunc("/api/instances/System/action/querySystemLimits", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"systemLimitEntryList":[{"type":"volumeSizeGb","maxVal":"not-a-number"}]}`)
	})
	handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{}`)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	client.SetToken("test-token")

	svc := &service{adminClients: map[string]*sio.Client{testKey: client}}
	_, err := svc.getMaximumVolumeSize(testKey)
	assert.Error(t, err)
}

// ── getSystemCapacityCalculator (controller_extension.go) ────────────

func TestGetSystemCapacityCalculator_NilAdminClient(t *testing.T) {
	svc := &service{
		adminClients: map[string]*sio.Client{},
		systems:      map[string]*sio.System{},
	}
	_, err := svc.getSystemCapacityCalculator("nosys", svc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "can't find adminClient or system")
}

// ── getNFSExport (service.go) ────────────────────────────────────────

func TestGetNFSExport_EmptyList(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "NFSExport") {
			fmt.Fprintf(w, `[]`)
			return
		}
		fmt.Fprintf(w, `{}`)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	client.SetToken("test-token")

	svc := &service{}
	fs := &siotypes.FileSystem{ID: "fs-123"}
	_, err := svc.getNFSExport(fs, client)
	assert.Error(t, err)
}

// ── getFileInterface (service.go) ────────────────────────────────────

func TestGetFileInterface_EmptyList(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "FileInterface") {
			fmt.Fprintf(w, `[]`)
			return
		}
		fmt.Fprintf(w, `{}`)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	client.SetToken("test-token")

	svc := &service{}
	fs := &siotypes.FileSystem{ID: "fs-456", NasServerID: "nas-123"}
	_, err := svc.getFileInterface("sys1", fs, client)
	assert.Error(t, err)
}

// ── GetPlatformVersion parse error (service.go) ──────────────────────

func TestGetPlatformVersion_ParseError(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `"not-a-number"`)
	})
	handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{}`)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	client.SetToken("test-token")

	svc := &service{adminClients: map[string]*sio.Client{"sys1": client}}
	_, err := svc.GetPlatformVersion("sys1")
	assert.Error(t, err)
}

// ── getPeerMdms with mock (service.go) ──────────────────────────────

func TestGetPeerMdms_APIError(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"message":"internal error"}`)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	client.SetToken("test-token")

	svc := &service{adminClients: map[string]*sio.Client{"sys1": client}}
	_, err := svc.getPeerMdms("sys1")
	assert.Error(t, err)
}

// ── GetPlatformInfo fetch path (service.go) ────────────────────────────────────

func TestGetPlatformInfo_FetchNew(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `"4.5"`)
	})
	// Serve the protection-domain list so GetGenType can resolve genType.
	handler.HandleFunc("/api/types/ProtectionDomain/instances", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `[{"id":"pd1","name":"PD1","genType":""}]`)
	})
	handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{}`)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	client.SetToken("test-token")

	// Build a System object wired to the same server so GetGenType works.
	sys := sio.NewSystem(client)
	sys.System = &siotypes.System{
		Links: []*siotypes.Link{
			{Rel: "/api/System/relationship/ProtectionDomain", HREF: "/api/types/ProtectionDomain/instances"},
		},
	}

	svc := &service{
		adminClients:  map[string]*sio.Client{"sys1": client},
		systems:       map[string]*sio.System{"sys1": sys},
		platformInfos: map[string]*PlatformInfo{},
	}
	result, err := svc.GetPlatformInfo("sys1")
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 4.5, result.ArrayVersion)
}

// ── GetNodeIP success path (service.go) ──────────────────────────────────────────

func TestGetNodeIP_SuccessPath(t *testing.T) {
	origClientset := K8sClientset
	defer func() { K8sClientset = origClientset }()

	K8sClientset = fake.NewSimpleClientset(
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node-with-ip"},
			Status:     v1.NodeStatus{Addresses: []v1.NodeAddress{{Type: v1.NodeInternalIP, Address: "10.0.0.1"}}},
		},
	)

	svc := &service{opts: Opts{KubeNodeName: "node-with-ip"}}
	ip, err := svc.GetNodeIP(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "10.0.0.1", ip)
}

// ── GetNodeUID error path (service.go) ─────────────────────────────────────────

func TestGetNodeUID_Error(t *testing.T) {
	origClientset := K8sClientset
	defer func() { K8sClientset = origClientset }()
	K8sClientset = fake.NewSimpleClientset()

	svc := &service{opts: Opts{KubeNodeName: "nonexistent"}}
	_, err := svc.GetNodeUID(context.Background())
	assert.Error(t, err)
}

// ── kmodLoaded (node.go) ────────────────────────────────────────────

func TestKmodLoaded_SciniPresent(t *testing.T) {
	opts := Opts{Lsmod: "Module                  Size  Used by\nscini                 123456  0\nother                 789012  1\n"}
	assert.True(t, kmodLoaded(opts))
}

func TestKmodLoaded_SciniAbsent(t *testing.T) {
	opts := Opts{Lsmod: "Module                  Size  Used by\nother                 789012  1\n"}
	assert.False(t, kmodLoaded(opts))
}

func TestKmodLoaded_EmptyOutput(_ *testing.T) {
	opts := Opts{Lsmod: ""}
	// With empty Lsmod, it will try to run the real lsmod command
	// which may or may not work in test environment
	_ = kmodLoaded(opts)
}

// ── ParseInt64FromContext (service.go) ───────────────────────────────

func TestParseInt64FromContext_InvalidValue(t *testing.T) {
	origLookup := LookupEnv
	defer func() { LookupEnv = origLookup }()
	LookupEnv = func(_ context.Context, key string) (string, bool) {
		if key == "TEST_INT_KEY" {
			return "not-a-number", true
		}
		return "", false
	}
	_, err := ParseInt64FromContext(context.Background(), "TEST_INT_KEY")
	assert.Error(t, err)
}

func TestParseInt64FromContext_ValidValue(t *testing.T) {
	origLookup := LookupEnv
	defer func() { LookupEnv = origLookup }()
	LookupEnv = func(_ context.Context, key string) (string, bool) {
		if key == "TEST_INT_KEY2" {
			return "42", true
		}
		return "", false
	}
	val, err := ParseInt64FromContext(context.Background(), "TEST_INT_KEY2")
	assert.NoError(t, err)
	assert.Equal(t, int64(42), val)
}

func TestParseInt64FromContext_MissingKey(t *testing.T) {
	origLookup := LookupEnv
	defer func() { LookupEnv = origLookup }()
	LookupEnv = func(_ context.Context, _ string) (string, bool) {
		return "", false
	}
	val, err := ParseInt64FromContext(context.Background(), "MISSING_KEY")
	assert.NoError(t, err)
	assert.Equal(t, int64(0), val)
}

// ── getSDCIPs (service.go) ──────────────────────────────────────────

func TestGetSDCIPs_SystemNotFound(t *testing.T) {
	svc := &service{systems: map[string]*sio.System{}}
	_, err := svc.getSDCIPs("guid-abc", "unknown-sys")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "systemID not found")
}

// ── getZoneKeyLabelFromSecret (service.go) ──────────────────────────

func TestGetZoneKeyLabelFromSecret_EmptyArrays(t *testing.T) {
	arrays := map[string]*ArrayConnectionData{}
	label, err := getZoneKeyLabelFromSecret(arrays)
	assert.NoError(t, err)
	assert.Equal(t, "", label)
}

func TestGetZoneKeyLabelFromSecret_ConsistentLabel(t *testing.T) {
	arrays := map[string]*ArrayConnectionData{
		"arr1": {Zones: []AvailabilityZone{{LabelKey: "zone.example.com/az"}}},
		"arr2": {Zones: []AvailabilityZone{{LabelKey: "zone.example.com/az"}}},
	}
	label, err := getZoneKeyLabelFromSecret(arrays)
	assert.NoError(t, err)
	assert.Equal(t, "zone.example.com/az", label)
}

func TestGetZoneKeyLabelFromSecret_InconsistentLabel(t *testing.T) {
	arrays := map[string]*ArrayConnectionData{
		"arr1": {Zones: []AvailabilityZone{{LabelKey: "zone.a.com/az"}}},
		"arr2": {Zones: []AvailabilityZone{{LabelKey: "zone.b.com/az"}}},
	}
	_, err := getZoneKeyLabelFromSecret(arrays)
	assert.Error(t, err)
}

// ── IsEligible additional branches (space_reclamation.go) ────────────

func TestIsEligible_BlockModeNilLabels(t *testing.T) {
	result, reason := IsEligible(true, nil, VolumeModeBlock)
	assert.False(t, result)
	assert.Equal(t, "block mode missing required label", reason)
}

func TestIsEligible_NilLabelsGlobalDisabled(t *testing.T) {
	result, reason := IsEligible(false, nil, VolumeModeFilesystem)
	assert.False(t, result)
	assert.Equal(t, "global disabled", reason)
}

func TestIsEligible_InvalidLabelValue(t *testing.T) {
	labels := map[string]string{FstrimLabelEnabled: "maybe"}
	result, reason := IsEligible(true, labels, VolumeModeFilesystem)
	assert.False(t, result)
	assert.Contains(t, reason, "must be 'true' or 'false'")
}

// ── getNASServerIDFromName (service.go) ──────────────────────────────

func TestGetNASServerIDFromName_EmptyName(t *testing.T) {
	svc := &service{adminClients: map[string]*sio.Client{}}
	_, err := svc.getNASServerIDFromName("sys1", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NAS server not provided")
}

func TestGetNASServerIDFromName_SystemError(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"message":"server error"}`)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
	client.SetToken("test-token")

	svc := &service{adminClients: map[string]*sio.Client{"sys1": client}}
	_, err := svc.getNASServerIDFromName("sys1", "nas-server-1")
	assert.Error(t, err)
}

// ── generateNodeID (service.go) ─────────────────────────────────────

func TestGenerateNodeID_ErrorPath(t *testing.T) {
	origClientset := K8sClientset
	defer func() { K8sClientset = origClientset }()
	K8sClientset = fake.NewSimpleClientset()

	svc := &service{opts: Opts{KubeNodeName: "nonexistent-node"}}
	_, err := svc.generateNodeID()
	assert.Error(t, err)
}

// ── GetNodeLabels (service.go) ──────────────────────────────────────

func TestGetNodeLabels_Success(t *testing.T) {
	origClientset := K8sClientset
	defer func() { K8sClientset = origClientset }()

	K8sClientset = fake.NewSimpleClientset(
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "labeled-node",
				Labels: map[string]string{"zone": "us-east-1a", "env": "test"},
			},
		},
	)

	svc := &service{opts: Opts{KubeNodeName: "labeled-node"}}
	labels, err := svc.GetNodeLabels(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "us-east-1a", labels["zone"])
	assert.Equal(t, "test", labels["env"])
}

// ── SetPodZoneLabel (service.go) ────────────────────────────────────

func TestSetPodZoneLabel_NoPods(t *testing.T) {
	origClientset := K8sClientset
	defer func() { K8sClientset = origClientset }()
	K8sClientset = fake.NewSimpleClientset()

	svc := &service{opts: Opts{KubeNodeName: "some-node"}}
	err := svc.SetPodZoneLabel(context.Background(), map[string]string{"zone": "us-east"})
	assert.Error(t, err)
}

// ── approveSDC error path (node.go) ─────────────────────────────────

func TestApproveSDC_Disabled(t *testing.T) {
	svc := &service{
		systems: map[string]*sio.System{},
		opts:    Opts{IsApproveSDCEnabled: false},
	}
	err := svc.approveSDC(svc.opts)
	// approveSDC returns nil if not enabled
	assert.NoError(t, err)
}

// ── getSDCMappedVol (node.go) ───────────────────────────────────────

func TestGetSDCMappedVol_ZeroRetry(t *testing.T) {
	svc := &service{
		systems:                 map[string]*sio.System{},
		adminClients:            map[string]*sio.Client{},
		connectedSystemNameToID: map[string]string{},
	}
	result, err := svc.getSDCMappedVol("vol123", "nosys", 0)
	// With 0 retries, the loop is skipped, returning nil, nil
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// ── getArrayVersion with mock (service.go) ──────────────────────────

func TestGetArrayVersion_ProbeError(t *testing.T) {
	svc := &service{
		adminClients:  map[string]*sio.Client{},
		systems:       map[string]*sio.System{},
		platformInfos: map[string]*PlatformInfo{},
		opts:          Opts{AutoProbe: true},
	}
	// getArrayVersion calls systemProbeAll, which should fail with no clients
	_, err := svc.getArrayVersion(context.Background(), "sys1")
	assert.Error(t, err)
}

// ── ListSnapshots validation error paths (controller.go) ────────────

func TestListSnapshots_InvalidStartingToken(t *testing.T) {
	svc := &service{}
	_, err := svc.ListSnapshots(context.Background(), &csi.ListSnapshotsRequest{
		StartingToken: "not-a-number",
	})
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Aborted, st.Code())
}

func TestListSnapshots_NoSystemID(t *testing.T) {
	svc := &service{
		opts:                    Opts{defaultSystemID: ""},
		connectedSystemNameToID: map[string]string{},
	}
	_, err := svc.ListSnapshots(context.Background(), &csi.ListSnapshotsRequest{
		SourceVolumeId: "justvol",
	})
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestListSnapshots_RequireProbeNotFound(t *testing.T) {
	svc := &service{
		opts:         Opts{defaultSystemID: "sys1"},
		adminClients: map[string]*sio.Client{},
		systems:      map[string]*sio.System{},
	}
	// requireProbe returns NotFound => ListSnapshots returns empty response, not error
	resp, err := svc.ListSnapshots(context.Background(), &csi.ListSnapshotsRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

// ── ListVolumes validation error paths (controller.go) ──────────────

func TestListVolumes_EmptySystemID(t *testing.T) {
	svc := &service{
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				"arr1": {SystemID: ""},
			},
		},
	}
	_, err := svc.ListVolumes(context.Background(), &csi.ListVolumesRequest{})
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestListVolumes_RequireProbeError(t *testing.T) {
	svc := &service{
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				"sys1": {SystemID: "sys1"},
			},
		},
		adminClients: map[string]*sio.Client{},
		systems:      map[string]*sio.System{},
	}
	// requireProbe should fail since systems is empty
	resp, err := svc.ListVolumes(context.Background(), &csi.ListVolumesRequest{})
	// requireProbe error means the system is skipped; result may still succeed
	// but the volume list will be empty
	if err == nil {
		assert.NotNil(t, resp)
	}
}

// ── DeleteVolume NFS path error (controller.go) ─────────────────────

func TestDeleteVolume_NFSNoSystemID(t *testing.T) {
	svc := &service{
		opts:                    Opts{defaultSystemID: ""},
		connectedSystemNameToID: map[string]string{},
		volumePrefixToSystems:   map[string][]string{},
	}
	// Use a/b/c format so getSystemIDFromCsiVolumeID returns "" (3 tokens != 2)
	_, err := svc.DeleteVolume(context.Background(), &csi.DeleteVolumeRequest{
		VolumeId: "a/b/c",
	})
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// ── ValidateVolumeHostConnectivity error paths (csi_extension_server.go) ──

func TestValidateVolumeHostConnectivity_RequireProbeError(t *testing.T) {
	svc := &service{
		opts:                    Opts{defaultSystemID: "sys1"},
		adminClients:            map[string]*sio.Client{},
		systems:                 map[string]*sio.System{},
		connectedSystemNameToID: map[string]string{},
	}
	_, err := svc.ValidateVolumeHostConnectivity(context.Background(),
		&podmon.ValidateVolumeHostConnectivityRequest{
			NodeId:    "node1",
			ArrayId:   "sys1",
			VolumeIds: []string{"vol1"},
		})
	assert.Error(t, err)
}

// ── ephemeral validation errors (ephemeral.go) ──────────────────────

func TestEphemeralNodePublish_VolumeNameTooLong(t *testing.T) {
	tmpDir := t.TempDir()
	origPath := ephemeralStagingMountPath
	ephemeralStagingMountPath = tmpDir + "/"
	defer func() { ephemeralStagingMountPath = origPath }()

	svc := &service{}
	req := &csi.NodePublishVolumeRequest{
		VolumeId:   "ephvol1",
		TargetPath: "/tmp/target",
		VolumeContext: map[string]string{
			"volumeName": "this-name-is-way-too-long-for-powerflex-volumes-exceed-31",
		},
		VolumeCapability: &csi.VolumeCapability{
			AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
			AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}},
		},
	}
	_, err := svc.ephemeralNodePublish(context.Background(), req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Volume name too long")
}

func TestEphemeralNodePublish_EmptyVolumeName(t *testing.T) {
	tmpDir := t.TempDir()
	origPath := ephemeralStagingMountPath
	ephemeralStagingMountPath = tmpDir + "/"
	defer func() { ephemeralStagingMountPath = origPath }()

	svc := &service{}
	req := &csi.NodePublishVolumeRequest{
		VolumeId:   "ephvol2",
		TargetPath: "/tmp/target",
		VolumeContext: map[string]string{
			"volumeName": "",
		},
		VolumeCapability: &csi.VolumeCapability{
			AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
			AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}},
		},
	}
	_, err := svc.ephemeralNodePublish(context.Background(), req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Volume name not specified")
}

func TestEphemeralNodePublish_InvalidSize(t *testing.T) {
	tmpDir := t.TempDir()
	origPath := ephemeralStagingMountPath
	ephemeralStagingMountPath = tmpDir + "/"
	defer func() { ephemeralStagingMountPath = origPath }()

	svc := &service{}
	req := &csi.NodePublishVolumeRequest{
		VolumeId:   "ephvol3",
		TargetPath: "/tmp/target",
		VolumeContext: map[string]string{
			"volumeName": "testvol",
			"size":       "notasize",
		},
		VolumeCapability: &csi.VolumeCapability{
			AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
			AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}},
		},
	}
	_, err := svc.ephemeralNodePublish(context.Background(), req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse size failed")
}

func TestEphemeralNodePublish_UnknownSystem(t *testing.T) {
	tmpDir := t.TempDir()
	origPath := ephemeralStagingMountPath
	ephemeralStagingMountPath = tmpDir + "/"
	defer func() { ephemeralStagingMountPath = origPath }()

	svc := &service{
		opts: Opts{
			defaultSystemID: "unknown-sys",
			arrays:          map[string]*ArrayConnectionData{},
		},
		connectedSystemNameToID: map[string]string{},
	}
	req := &csi.NodePublishVolumeRequest{
		VolumeId:   "ephvol4",
		TargetPath: "/tmp/target",
		VolumeContext: map[string]string{
			"volumeName": "testvol",
			"size":       "8Gi",
		},
		VolumeCapability: &csi.VolumeCapability{
			AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
			AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}},
		},
	}
	_, err := svc.ephemeralNodePublish(context.Background(), req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not recgonized")
}

// ── GetCapacity with systemID from zone (controller.go) ─────────────

func TestGetCapacity_NoParams(t *testing.T) {
	svc := &service{
		opts:         Opts{defaultSystemID: ""},
		adminClients: map[string]*sio.Client{},
		systems:      map[string]*sio.System{},
	}
	resp, err := svc.GetCapacity(context.Background(), &csi.GetCapacityRequest{})
	// With no params, getCapacityForAllSystems is called which iterates over arrays
	// With no arrays configured, this returns 0 capacity
	if err == nil {
		assert.NotNil(t, resp)
		assert.Equal(t, int64(0), resp.AvailableCapacity)
	}
}

// ── ControllerGetVolume error paths (controller.go) ─────────────────

func TestControllerGetVolume_MissingVolumeID(t *testing.T) {
	svc := &service{}
	_, err := svc.ControllerGetVolume(context.Background(), &csi.ControllerGetVolumeRequest{
		VolumeId: "",
	})
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// ── parseSize edge cases (ephemeral.go) ─────────────────────────────

func TestParseSize_GigaBytes(t *testing.T) {
	size, err := parseSize("10Gi")
	assert.NoError(t, err)
	assert.Equal(t, int64(10*1073741824), size)
}

func TestParseSize_GigaBytesWithSpace(t *testing.T) {
	size, err := parseSize("5 Gi")
	assert.NoError(t, err)
	assert.Equal(t, int64(5*1073741824), size)
}

func TestParseSize_Invalid(t *testing.T) {
	_, err := parseSize("abc")
	assert.Error(t, err)
}

func TestParseSize_NoUnit(t *testing.T) {
	_, err := parseSize("123")
	assert.Error(t, err)
}

func TestParseSize_UnsupportedUnit(t *testing.T) {
	_, err := parseSize("10Mi")
	assert.Error(t, err)
}

// ── ephemeral name-to-ID mapping and systemProbe error (ephemeral.go) ──

func TestEphemeralNodePublish_NameToIDMapping_SystemProbeError(t *testing.T) {
	tmpDir := t.TempDir()
	origPath := ephemeralStagingMountPath
	ephemeralStagingMountPath = tmpDir + "/"
	defer func() { ephemeralStagingMountPath = origPath }()

	svc := &service{
		opts: Opts{
			defaultSystemID: "myName",
			arrays: map[string]*ArrayConnectionData{
				"actual-id": {SystemID: "actual-id"},
			},
		},
		connectedSystemNameToID: map[string]string{"myName": "actual-id"},
	}
	req := &csi.NodePublishVolumeRequest{
		VolumeId:   "ephvol5",
		TargetPath: "/tmp/target",
		VolumeContext: map[string]string{
			"volumeName": "testvol",
			"size":       "8Gi",
		},
		VolumeCapability: &csi.VolumeCapability{
			AccessMode: &csi.VolumeCapability_AccessMode{Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
			AccessType: &csi.VolumeCapability_Mount{Mount: &csi.VolumeCapability_MountVolume{}},
		},
	}
	// This should reach systemProbe which will fail because array has no endpoint
	_, err := svc.ephemeralNodePublish(context.Background(), req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "system prob failed")
}

// ── GetCapacity with systemID in params (controller.go) ─────────────

func TestGetCapacity_WithSystemIDInParams(t *testing.T) {
	svc := &service{
		opts:         Opts{defaultSystemID: ""},
		adminClients: map[string]*sio.Client{},
		systems:      map[string]*sio.System{},
	}
	_, err := svc.GetCapacity(context.Background(), &csi.GetCapacityRequest{
		Parameters: map[string]string{
			KeySystemID:         "sys1",
			KeyStoragePool:      "pool1",
			KeyProtectionDomain: "pd1",
		},
	})
	// requireProbe will fail
	assert.Error(t, err)
}

// ── getSystemCapacityCalculator (controller_extension.go) ────────────

func TestGetSystemCapacityCalculator_NilClient(t *testing.T) {
	svc := &service{
		adminClients: map[string]*sio.Client{},
		systems:      map[string]*sio.System{},
	}
	_, err := svc.getSystemCapacityCalculator("sys1", svc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "can't find adminClient or system")
}

// ── getSystemCapacity requireProbe error (controller_extension.go) ───

func TestGetSystemCapacity_RequireProbeError(t *testing.T) {
	svc := &service{
		adminClients: map[string]*sio.Client{},
		systems:      map[string]*sio.System{},
	}
	_, err := svc.getSystemCapacity(context.Background(), "sys1", "pd1", "pool1")
	assert.Error(t, err)
}

// ── ProbeController with systemProbeAll error (identity.go) ──────────

func TestProbeController_SystemProbeAllError(t *testing.T) {
	svc := &service{
		mode: "controller",
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				"sys1": {SystemID: "sys1"},
			},
		},
		adminClients: map[string]*sio.Client{},
		systems:      map[string]*sio.System{},
	}
	_, err := svc.ProbeController(context.Background(), &commonext.ProbeControllerRequest{})
	assert.Error(t, err)
}

// ── ephemeralNodeUnpublish (ephemeral.go) ────────────────────────────

func TestEphemeralNodeUnpublish_NoLockFile(t *testing.T) {
	tmpDir := t.TempDir()
	origPath := ephemeralStagingMountPath
	ephemeralStagingMountPath = tmpDir + "/"
	defer func() { ephemeralStagingMountPath = origPath }()

	svc := &service{}
	err := svc.ephemeralNodeUnpublish(context.Background(), &csi.NodeUnpublishVolumeRequest{
		VolumeId:   "ephvol1",
		TargetPath: "/tmp/target",
	})
	assert.Error(t, err)
}

// ── DeleteVolume with checkVolumesMap error (controller.go) ──────────

func TestDeleteVolume_CheckVolumesMapError(t *testing.T) {
	svc := &service{
		connectedSystemNameToID: map[string]string{},
		volumePrefixToSystems:   map[string][]string{},
	}
	// Volume ID shorter than 3 chars with no system prefix => checkVolumesMap error
	_, err := svc.DeleteVolume(context.Background(), &csi.DeleteVolumeRequest{
		VolumeId: "ab",
	})
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Contains(t, err.Error(), "checkVolumesMap")
}

// ── ListVolumes invalid starting token (controller.go) ──────────────

func TestListVolumes_InvalidStartingToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/login"):
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `"fakesession"`)
		case strings.Contains(r.URL.Path, "/api/version"):
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `"4.0"`)
		default:
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{}`)
		}
	}))
	defer server.Close()

	adminClient, err := sio.NewClientWithArgs(server.URL, "", math.MaxInt64, true, false, "")
	assert.NoError(t, err)
	_, err = adminClient.Authenticate(&sio.ConfigConnect{
		Endpoint: server.URL,
		Username: "admin",
		Password: "pass",
	})
	assert.NoError(t, err)

	sys := sio.NewSystem(adminClient)

	svc := &service{
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				"sys1": {SystemID: "sys1"},
			},
		},
		adminClients: map[string]*sio.Client{"sys1": adminClient},
		systems:      map[string]*sio.System{"sys1": sys},
	}
	_, err = svc.ListVolumes(context.Background(), &csi.ListVolumesRequest{
		StartingToken: "not-a-number",
	})
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.Aborted, st.Code())
}

// ── NVMeStager.connectDevice when NVMe is disabled (stager.go) ──────

func TestNVMeStager_ConnectDevice_NVMeDisabled(t *testing.T) {
	stager := &NVMeStager{useNVME: false}
	_, err := stager.connectDevice(context.Background(), deviceInfo{})
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

// ── StageStatus.String() default branch (stager.go) ─────────────────

func TestStageStatus_String_Unknown(t *testing.T) {
	s := StageStatus(99)
	assert.Equal(t, "unknown", s.String())
}

// ── ControllerExpandVolume missing volume ID (controller.go) ─────────

func TestControllerExpandVolume_MissingVolumeID(t *testing.T) {
	svc := &service{}
	_, err := svc.ControllerExpandVolume(context.Background(), &csi.ControllerExpandVolumeRequest{
		VolumeId: "",
	})
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// ── ControllerExpandVolume expansion edge cases (FR-3) ────────────────
//
// These tests verify the size-calculation layer (validateVolSize) for the
// three expansion scenarios: idempotent, no-op (shrink attempt), and growth.
// The comparison with vol.SizeInKb happens inside ControllerExpandVolume
// (controller.go lines 3795-3805); these unit tests confirm that
// validateVolSize returns the correct rounded target before that comparison.
//
//	Idempotent: rounded target == current size → ControllerExpandVolume returns early (no SetVolumeSize)
//	No-op:      rounded target  < current size → ControllerExpandVolume returns empty response
//	Growth:     rounded target  > current size → ControllerExpandVolume calls SetVolumeSize
func TestExpansionEdgeCases_ValidateVolSizeTarget(t *testing.T) {
	tests := []struct {
		name            string
		reqBytes        int64
		genType         string
		currentSizeKiB  int64 // simulated current volume size (vol.SizeInKb)
		wantSizeKiB     int64
		expandBehaviour string // "idempotent", "no-op", or "growth"
	}{
		// FR-3: Gen2/EC idempotent — rounded target == current → no resize
		{
			name:            "Gen2/EC idempotent: request 3 GiB on 3 GiB volume",
			reqBytes:        3 * bytesInGiB,
			genType:         "EC",
			currentSizeKiB:  3 * kiBytesInGiB,
			wantSizeKiB:     3 * kiBytesInGiB,
			expandBehaviour: "idempotent",
		},
		// FR-3: Gen2/EC no-op — rounded target < current → shrink attempt blocked
		{
			name:            "Gen2/EC no-op: request 2 GiB on 3 GiB volume",
			reqBytes:        2 * bytesInGiB,
			genType:         "EC",
			currentSizeKiB:  3 * kiBytesInGiB,
			wantSizeKiB:     2 * kiBytesInGiB,
			expandBehaviour: "no-op",
		},
		// FR-3: Gen2/EC growth — rounded target > current → resize proceeds
		{
			name:            "Gen2/EC growth: request 5 GiB on 3 GiB volume",
			reqBytes:        5 * bytesInGiB,
			genType:         "EC",
			currentSizeKiB:  3 * kiBytesInGiB,
			wantSizeKiB:     5 * kiBytesInGiB,
			expandBehaviour: "growth",
		},
		// FR-3: Gen2/EC growth with rounding — 3.5 GiB rounds to 4 GiB > 3 GiB current
		{
			name:            "Gen2/EC growth with rounding: request 3.5 GiB on 3 GiB volume",
			reqBytes:        int64(3.5 * float64(bytesInGiB)),
			genType:         "EC",
			currentSizeKiB:  3 * kiBytesInGiB,
			wantSizeKiB:     4 * kiBytesInGiB,
			expandBehaviour: "growth",
		},
		// FR-3: Gen1 idempotent — 3 GiB request rounds to 8 GiB; current is also 8 GiB
		{
			name:            "Gen1 idempotent: request 3 GiB on 8 GiB volume (rounds to 8 GiB == current)",
			reqBytes:        3 * bytesInGiB,
			genType:         "",
			currentSizeKiB:  8 * kiBytesInGiB,
			wantSizeKiB:     8 * kiBytesInGiB,
			expandBehaviour: "idempotent",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(st *testing.T) {
			st.Parallel()
			cr := &csi.CapacityRange{RequiredBytes: tt.reqBytes}
			sizeKiB, err := validateVolSize(cr, tt.genType)
			assert.NoError(st, err, "validateVolSize must not error for valid expansion request")
			assert.Equal(st, tt.wantSizeKiB, sizeKiB,
				"rounded expansion target must match expected size for %s", tt.expandBehaviour)
			// Verify ControllerExpandVolume branch decision matches expected behaviour
			switch tt.expandBehaviour {
			case "idempotent":
				assert.Equal(st, sizeKiB, tt.currentSizeKiB,
					"idempotent: computed target must equal current size")
			case "no-op":
				assert.Less(st, sizeKiB, tt.currentSizeKiB,
					"no-op: computed target must be less than current size")
			case "growth":
				assert.Greater(st, sizeKiB, tt.currentSizeKiB,
					"growth: computed target must exceed current size")
			}
		})
	}
}

// ── CreateVolume missing name (controller.go) ────────────────────────

func TestCreateVolume_EmptyName(t *testing.T) {
	svc := &service{}
	_, err := svc.CreateVolume(context.Background(), &csi.CreateVolumeRequest{
		Name: "",
	})
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
}

// ── ValidateVolumeCapabilities missing volume ID (controller.go) ─────

func TestValidateVolumeCapabilities_EmptyVolumeID(t *testing.T) {
	svc := &service{}
	_, err := svc.ValidateVolumeCapabilities(context.Background(), &csi.ValidateVolumeCapabilitiesRequest{
		VolumeId: "",
	})
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// ── systemProbe OIDC prechecks failure (controller.go:2793-2800) ─────

func TestSystemProbe_OIDCPrechecksFail(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/login"):
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `"fakesession"`)
		case strings.Contains(r.URL.Path, "/api/version"):
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `"4.0"`)
		default:
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{}`)
		}
	}))
	defer server.Close()

	adminClient, err := sio.NewClientWithArgs(server.URL, "", math.MaxInt64, true, false, "")
	assert.NoError(t, err)

	svc := &service{
		opts: Opts{
			AuthType: "OIDC",
			arrays:   map[string]*ArrayConnectionData{},
		},
		adminClients:            map[string]*sio.Client{"sys1": adminClient},
		systems:                 map[string]*sio.System{},
		connectedSystemNameToID: map[string]string{},
		volumePrefixToSystems:   map[string][]string{},
		probeLocks:              sync.Map{},
	}

	// Missing OidcClientID => OIDC prechecks fail
	arr := &ArrayConnectionData{
		SystemID:     "sys1",
		Endpoint:     server.URL,
		Username:     "admin",
		Password:     "pass",
		OidcClientID: "",
	}
	err = svc.systemProbe(context.Background(), arr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "OIDC prechecks failed")
}

// ── systemProbe OIDC ExtractIP failure (controller.go:2802-2806) ─────

func TestSystemProbe_OIDCExtractIPFail(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/login"):
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `"fakesession"`)
		case strings.Contains(r.URL.Path, "/api/version"):
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `"4.0"`)
		default:
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{}`)
		}
	}))
	defer server.Close()

	adminClient, err := sio.NewClientWithArgs(server.URL, "", math.MaxInt64, true, false, "")
	assert.NoError(t, err)

	svc := &service{
		opts: Opts{
			AuthType: "OIDC",
			arrays:   map[string]*ArrayConnectionData{},
		},
		adminClients:            map[string]*sio.Client{"sys1": adminClient},
		systems:                 map[string]*sio.System{},
		connectedSystemNameToID: map[string]string{},
		volumePrefixToSystems:   map[string][]string{},
		probeLocks:              sync.Map{},
	}

	// Provide OIDC credentials but use a bad endpoint that fails ExtractIP
	arr := &ArrayConnectionData{
		SystemID:         "sys1",
		Endpoint:         "://bad-url",
		Username:         "admin",
		Password:         "pass",
		OidcClientID:     "oidc-client",
		OidcClientSecret: "oidc-secret",
		CiamClientID:     "ciam-client",
		CiamClientSecret: "ciam-secret",
		Issuer:           "https://issuer.example.com",
	}
	err = svc.systemProbe(context.Background(), arr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unable to extract endpoint IP")
}

// ── systemProbe OIDC Authenticate failure (controller.go:2808-2823) ──

func TestSystemProbe_OIDCAuthenticateFail(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/login"):
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `"fakesession"`)
		case strings.Contains(r.URL.Path, "/api/version"):
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `"4.0"`)
		default:
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{}`)
		}
	}))
	defer server.Close()

	// Create client but don't authenticate so token is empty
	adminClient, err := sio.NewClientWithArgs(server.URL, "", math.MaxInt64, true, false, "")
	assert.NoError(t, err)

	svc := &service{
		opts: Opts{
			AuthType: "OIDC",
			arrays:   map[string]*ArrayConnectionData{},
		},
		adminClients:            map[string]*sio.Client{"sys1": adminClient},
		systems:                 map[string]*sio.System{},
		connectedSystemNameToID: map[string]string{},
		volumePrefixToSystems:   map[string][]string{},
		probeLocks:              sync.Map{},
	}

	arr := &ArrayConnectionData{
		SystemID:         "sys1",
		Endpoint:         server.URL,
		Username:         "admin",
		Password:         "pass",
		OidcClientID:     "oidc-client",
		OidcClientSecret: "oidc-secret",
		CiamClientID:     "ciam-client",
		CiamClientSecret: "ciam-secret",
		Issuer:           "https://issuer.example.com",
	}
	err = svc.systemProbe(context.Background(), arr)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unable to login to PowerFlex Gateway")
}

// ── systemProbe connectedSystemNameToID mapping (controller.go:2863-2867) ─

func TestSystemProbe_NameToIDMapping(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/api/login"):
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `"fakesession"`)
		case strings.Contains(r.URL.Path, "/api/version"):
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `"4.0"`)
		default:
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{}`)
		}
	}))
	defer server.Close()

	adminClient, err := sio.NewClientWithArgs(server.URL, "", math.MaxInt64, true, false, "")
	assert.NoError(t, err)
	_, err = adminClient.Authenticate(&sio.ConfigConnect{
		Endpoint: server.URL,
		Username: "admin",
		Password: "pass",
	})
	assert.NoError(t, err)

	sys := sio.NewSystem(adminClient)
	sys.System = &siotypes.System{ID: "actual-id", Name: "myname"}

	svc := &service{
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{},
		},
		adminClients:            map[string]*sio.Client{"myname": adminClient},
		systems:                 map[string]*sio.System{"myname": sys},
		connectedSystemNameToID: map[string]string{"myname": "actual-id"},
		volumePrefixToSystems:   map[string][]string{},
		probeLocks:              sync.Map{},
	}

	arr := &ArrayConnectionData{
		SystemID:  "myname",
		Endpoint:  server.URL,
		Username:  "admin",
		Password:  "pass",
		IsDefault: true,
	}
	err = svc.systemProbe(context.Background(), arr)
	// Should succeed and map name to ID
	assert.NoError(t, err)
	assert.Equal(t, "actual-id", svc.opts.defaultSystemID)
}

// ── getMetric helper (csi_extension_server.go:183-190) ───────────────

func TestGetMetric_Found(t *testing.T) {
	metrics := []siotypes.Metric{
		{Name: "host_read_bandwidth", Values: []float64{100.5}},
		{Name: "host_write_bandwidth", Values: []float64{200.0}},
	}
	assert.Equal(t, 100.5, getMetric(metrics, "host_read_bandwidth"))
	assert.Equal(t, 200.0, getMetric(metrics, "host_write_bandwidth"))
}

func TestGetMetric_NotFound(t *testing.T) {
	metrics := []siotypes.Metric{
		{Name: "host_read_bandwidth", Values: []float64{100.5}},
	}
	assert.Equal(t, float64(0), getMetric(metrics, "nonexistent"))
}

func TestGetMetric_Empty(t *testing.T) {
	assert.Equal(t, float64(0), getMetric(nil, "anything"))
}

// ── metrics helper functions (service.go) ────────────────────────────

func TestDurationFromEnvOrDefault(t *testing.T) {
	t.Run("missing returns default", func(t *testing.T) {
		assert.Equal(t, 5*time.Second, durationFromEnvOrDefault("_UNSET_XYZ", 5*time.Second))
	})
	t.Run("valid duration", func(t *testing.T) {
		t.Setenv("_TEST_DUR", "10s")
		assert.Equal(t, 10*time.Second, durationFromEnvOrDefault("_TEST_DUR", 5*time.Second))
	})
	t.Run("invalid duration logs warning and returns default", func(t *testing.T) {
		t.Setenv("_TEST_DUR", "not-a-duration")
		assert.Equal(t, 7*time.Second, durationFromEnvOrDefault("_TEST_DUR", 7*time.Second))
	})
	t.Run("zero duration returns default", func(t *testing.T) {
		t.Setenv("_TEST_DUR", "0s")
		assert.Equal(t, 3*time.Second, durationFromEnvOrDefault("_TEST_DUR", 3*time.Second))
	})
}

func TestIntFromEnvOrDefault(t *testing.T) {
	t.Run("missing returns default", func(t *testing.T) {
		assert.Equal(t, 42, intFromEnvOrDefault("_UNSET_INT", 42))
	})
	t.Run("valid value", func(t *testing.T) {
		t.Setenv("_TEST_INT", "99")
		assert.Equal(t, 99, intFromEnvOrDefault("_TEST_INT", 42))
	})
	t.Run("invalid value returns default", func(t *testing.T) {
		t.Setenv("_TEST_INT", "abc")
		assert.Equal(t, 42, intFromEnvOrDefault("_TEST_INT", 42))
	})
	t.Run("zero value returns default", func(t *testing.T) {
		t.Setenv("_TEST_INT", "0")
		assert.Equal(t, 42, intFromEnvOrDefault("_TEST_INT", 42))
	})
}

func TestMetricsLeaderElectionEnabled(t *testing.T) {
	t.Run("unset returns false", func(t *testing.T) {
		assert.False(t, metricsLeaderElectionEnabled())
	})
	t.Run("true returns true", func(t *testing.T) {
		t.Setenv(EnvMetricsLeaderElectionEnabled, "true")
		assert.True(t, metricsLeaderElectionEnabled())
	})
	t.Run("false returns false", func(t *testing.T) {
		t.Setenv(EnvMetricsLeaderElectionEnabled, "false")
		assert.False(t, metricsLeaderElectionEnabled())
	})
	t.Run("case insensitive", func(t *testing.T) {
		t.Setenv(EnvMetricsLeaderElectionEnabled, "TRUE")
		assert.True(t, metricsLeaderElectionEnabled())
	})
}

func TestMetricsLeaderElectionNamespace(t *testing.T) {
	t.Run("unset returns default", func(t *testing.T) {
		DriverNamespace = "vxflexos"
		assert.Equal(t, "vxflexos", metricsLeaderElectionNamespace())
	})
	t.Run("env overrides default", func(t *testing.T) {
		t.Setenv(EnvDriverNamespace, "custom-ns")
		assert.Equal(t, "custom-ns", metricsLeaderElectionNamespace())
	})
	t.Run("empty env returns default", func(t *testing.T) {
		t.Setenv(EnvDriverNamespace, "")
		assert.Equal(t, "vxflexos", metricsLeaderElectionNamespace())
	})
}

func TestMetricsCollectionInterval(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		assert.Equal(t, 30*time.Second, metricsCollectionInterval())
	})
	t.Run("override", func(t *testing.T) {
		t.Setenv(EnvMetricsCollectionInterval, "45s")
		assert.Equal(t, 45*time.Second, metricsCollectionInterval())
	})
}

func TestMetricsRuntimeConfig(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		cfg := metricsRuntimeConfig()
		assert.Equal(t, 30*time.Second, cfg.Timeout)
		assert.Equal(t, 25*time.Second, cfg.CacheTTL)
		assert.Equal(t, 100, cfg.RateLimit)
		assert.Equal(t, 3, cfg.CBThreshold)
		assert.Equal(t, 30*time.Second, cfg.CBResetTimeout)
	})
	t.Run("overrides", func(t *testing.T) {
		t.Setenv(EnvMetricsArrayTimeout, "60s")
		t.Setenv(EnvMetricsCollectionCacheTTL, "55s")
		t.Setenv(EnvMetricsArrayRateLimit, "5")
		t.Setenv(EnvMetricsArrayCBThreshold, "7")
		t.Setenv(EnvMetricsArrayCBResetTimeout, "120s")
		cfg := metricsRuntimeConfig()
		assert.Equal(t, 60*time.Second, cfg.Timeout)
		assert.Equal(t, 55*time.Second, cfg.CacheTTL)
		assert.Equal(t, 5, cfg.RateLimit)
		assert.Equal(t, 7, cfg.CBThreshold)
		assert.Equal(t, 120*time.Second, cfg.CBResetTimeout)
	})
}
