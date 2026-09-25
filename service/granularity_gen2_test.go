// Copyright © 2026 Dell Inc. or its subsidiaries. All Rights Reserved.
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

package service

// ── Phase 1: GetGenType error handling ─────────────────────────────────────
//
// These tests define the CORRECT behaviour for GetGenType after the FR-3 fix:
//   • nil system   → non-nil error  (was returning ("", nil) — a bug)
//   • zero PDs     → non-nil error  (was returning ("", nil) — a bug)
//   • "" + PDs     → ("", nil)      — valid Gen1 signal, unchanged
//   • "EC" + PDs   → ("EC", nil)    — Gen2/EC
//   • "XYZ" + PDs  → ("XYZ", nil)  — unknown value returned as-is; caller validates

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/record"

	sio "github.com/Ecosystems/container-storage-modules/src/goscaleio"
	siotypes "github.com/Ecosystems/container-storage-modules/src/goscaleio/types/v1"
)

const (
	testToken = "test-token"
)

// makeGenTypeSystem builds a *sio.System whose /api/types/ProtectionDomain/instances
// handler returns the supplied JSON body.
func makeGenTypeSystem(t *testing.T, pdJSON string) *sio.System {
	t.Helper()
	handler := http.NewServeMux()
	handler.HandleFunc("/api/types/ProtectionDomain/instances", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, pdJSON)
	})
	handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client, err := sio.NewClientWithArgs(srv.URL, "4.0", 0, true, false, "")
	client.SetToken(testToken)
	require.NoError(t, err)

	sys := sio.NewSystem(client)
	sys.System = &siotypes.System{
		Links: []*siotypes.Link{
			{Rel: "/api/System/relationship/ProtectionDomain", HREF: "/api/types/ProtectionDomain/instances"},
		},
	}
	return sys
}

// ── Test: nil system returns non-nil error ────────────────────────────────

func TestGetGenType_NilSystemReturnsError(t *testing.T) {
	svc := &service{systems: map[string]*sio.System{}}
	_, err := svc.GetGenType("sys1")
	require.Error(t, err, "GetGenType with nil system must return an error (FR-3)")
	assert.Contains(t, err.Error(), "sys1")
}

// ── Test: zero protection domains returns non-nil error ───────────────────

func TestGetGenType_ZeroPDsReturnsError(t *testing.T) {
	sys := makeGenTypeSystem(t, `[]`) // empty PD list
	svc := &service{systems: map[string]*sio.System{"sys1": sys}}
	_, err := svc.GetGenType("sys1")
	require.Error(t, err, "GetGenType with zero protection domains must return an error (FR-3)")
	assert.Contains(t, err.Error(), "protection domain")
}

// ── Test: "EC" genType returns ("EC", nil) — Gen2/EC ─────────────────────

func TestGetGenType_ECReturnsEC(t *testing.T) {
	sys := makeGenTypeSystem(t, `[{"id":"pd1","name":"PD1","genType":"EC"}]`)
	svc := &service{systems: map[string]*sio.System{"sys1": sys}}
	genType, err := svc.GetGenType("sys1")
	assert.NoError(t, err)
	assert.Equal(t, "EC", genType)
}

// ── Test: "" genType + PDs returns ("", nil) — Gen1 signal ───────────────

func TestGetGenType_EmptyWithPDsReturnsEmptyNoError(t *testing.T) {
	sys := makeGenTypeSystem(t, `[{"id":"pd1","name":"PD1","genType":""}]`)
	svc := &service{systems: map[string]*sio.System{"sys1": sys}}
	genType, err := svc.GetGenType("sys1")
	assert.NoError(t, err, "GetGenType with '' genType and PDs present must succeed (Gen1 signal)")
	assert.Equal(t, "", genType)
}

// ── Test: unrecognized genType is returned as-is for caller to validate ───

func TestGetGenType_UnknownGenTypePassedThrough(t *testing.T) {
	sys := makeGenTypeSystem(t, `[{"id":"pd1","name":"PD1","genType":"XYZ"}]`)
	svc := &service{systems: map[string]*sio.System{"sys1": sys}}
	genType, err := svc.GetGenType("sys1")
	assert.NoError(t, err, "GetGenType must not error on unknown genType — caller validates")
	assert.Equal(t, "XYZ", genType)
}

// ── Phase 1: platformInfos map thread-safety (CG-RC-001) ──────────────────
//
// GetPlatformInfo must guard its first-write path with getProbeLock so that
// concurrent goroutines racing on the same un-cached systemID cannot trigger a
// map write race.  Run this test with -race to confirm.

func TestGetPlatformInfo_ConcurrentFetchNoRace(t *testing.T) {
	// Build a handler that serves both version and protection-domain endpoints.
	handler := http.NewServeMux()
	handler.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `"4.5"`)
	})
	handler.HandleFunc("/api/types/ProtectionDomain/instances", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"id":"pd1","name":"PD1","genType":"EC"}]`)
	})
	handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client, err := sio.NewClientWithArgs(srv.URL, "4.0", 0, true, false, "")
	require.NoError(t, err)
	client.SetToken(testToken)

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

	const goroutines = 10
	errs := make(chan error, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			_, e := svc.GetPlatformInfo("sys1")
			errs <- e
		}()
	}
	for i := 0; i < goroutines; i++ {
		assert.NoError(t, <-errs)
	}
}

// ── Phase 2: genType-aware validateVolSize ─────────────────────────────────
//
// FR-1: validateVolSize(cr, genType) must apply:
//   • genType=="EC"  → 1 GiB ceiling rounding (Gen2/EC)
//   • genType==""    → 8 GiB multiple rounding (Gen1, backward-compat)
//   • any other      → 8 GiB multiple rounding (default-safe)

func TestValidateVolSize_Gen2EC_ExactIntegerGB(t *testing.T) {
	// 3 GB exact on Gen2/EC → stays at 3 GiB (no rounding needed)
	cr := &csi.CapacityRange{RequiredBytes: 3 * bytesInGiB}
	sizeKiB, _ := validateVolSize(cr, "EC")
	assert.Equal(t, int64(3*kiBytesInGiB), sizeKiB)
}

func TestValidateVolSize_Gen2EC_NonIntegerGB_RoundsUp(t *testing.T) {
	// 1.2 GB on Gen2/EC → rounds up to 2 GiB
	reqBytes := int64(math.Trunc(float64(bytesInGiB) * 1.2)) // math.Trunc forces runtime eval of non-exact float
	cr := &csi.CapacityRange{RequiredBytes: reqBytes}
	sizeKiB, _ := validateVolSize(cr, "EC")
	assert.Equal(t, int64(2*kiBytesInGiB), sizeKiB)
}

func TestValidateVolSize_Gen2EC_SubOneGB_RoundsUpToOneGiB(t *testing.T) {
	// 512 MiB on Gen2/EC → rounds up to 1 GiB minimum
	cr := &csi.CapacityRange{RequiredBytes: 512 * 1024 * 1024}
	sizeKiB, _ := validateVolSize(cr, "EC")
	assert.Equal(t, int64(1*kiBytesInGiB), sizeKiB)
}

func TestValidateVolSize_Gen2EC_BoundaryExactOneGB(t *testing.T) {
	// Exactly 1 GB on Gen2/EC → returns 1 GiB (no rounding)
	cr := &csi.CapacityRange{RequiredBytes: 1 * bytesInGiB}
	sizeKiB, _ := validateVolSize(cr, "EC")
	assert.Equal(t, int64(1*kiBytesInGiB), sizeKiB)
}

func TestValidateVolSize_Gen2EC_LargeNonInteger(t *testing.T) {
	// 12.2 GB on Gen2/EC → rounds up to 13 GiB
	reqBytes := int64(math.Trunc(float64(bytesInGiB) * 12.2))
	cr := &csi.CapacityRange{RequiredBytes: reqBytes}
	sizeKiB, _ := validateVolSize(cr, "EC")
	assert.Equal(t, int64(13*kiBytesInGiB), sizeKiB)
}

func TestValidateVolSize_Gen1_StillUsesEightGiBMultiple(t *testing.T) {
	// Gen1 ("" genType): 1 GB → 8 GiB, 3 GB → 8 GiB, 8 GB → 8 GiB, 12.2 GB → 16 GiB
	cases := []struct {
		reqBytes int64
		wantGiB  int64
	}{
		{1 * bytesInGiB, 8},
		{3 * bytesInGiB, 8},
		{8 * bytesInGiB, 8},
		{int64(math.Trunc(float64(bytesInGiB) * 12.2)), 16},
	}
	for _, tc := range cases {
		cr := &csi.CapacityRange{RequiredBytes: tc.reqBytes}
		sizeKiB, _ := validateVolSize(cr, "")
		assert.Equal(t, tc.wantGiB*int64(kiBytesInGiB), sizeKiB,
			"Gen1: reqBytes=%d expected %d GiB", tc.reqBytes, tc.wantGiB)
	}
}

func TestValidateVolSize_Gen1Default_EmptyGenType(t *testing.T) {
	// Backward compatibility: empty genType uses 8 GiB granularity
	cr := &csi.CapacityRange{RequiredBytes: 3 * bytesInGiB}
	sizeKiB, _ := validateVolSize(cr, "")
	assert.Equal(t, int64(8*kiBytesInGiB), sizeKiB)
}

func TestValidateVolSize_NegativeSize_ReturnsOutOfRange(t *testing.T) {
	cr := &csi.CapacityRange{RequiredBytes: -1}
	_, err := validateVolSize(cr, "EC")
	assert.Error(t, err)
	_, err2 := validateVolSize(cr, "")
	assert.Error(t, err2)
}

func TestValidateVolSize_MaxSizeExceeded_ReturnsError(t *testing.T) {
	// Limit below what would be rounded up to
	cr := &csi.CapacityRange{
		RequiredBytes: 3 * bytesInGiB,
		LimitBytes:    4 * bytesInGiB, // EC rounds to 3 GiB — OK; Gen1 rounds to 8 GiB — exceeds 4 GiB limit
	}
	// Gen2/EC: 3 GiB ≤ 4 GiB limit → success
	_, _ = validateVolSize(cr, "EC")
	// Gen1: 8 GiB > 4 GiB limit → error
	_, err2 := validateVolSize(cr, "")
	assert.Error(t, err2)
}

// ── Phase 2: genType identifier validation ─────────────────────────────────
//
// FR-3: known genTypes ("" = Gen1, "EC" = Gen2) proceed normally.
// Unknown genTypes trigger WARN + abort (detected via isKnownGenType helper).

func TestIsKnownGenType_KnownEC(t *testing.T) {
	assert.True(t, isKnownGenType("EC"), "\"EC\" must be a known genType")
}

func TestIsKnownGenType_KnownEmpty(t *testing.T) {
	assert.True(t, isKnownGenType(""), "\"\" must be a known genType (Gen1 signal)")
}

func TestIsKnownGenType_Unknown(t *testing.T) {
	assert.False(t, isKnownGenType("XYZ"), "\"XYZ\" must not be a known genType")
	assert.False(t, isKnownGenType("Default"), "\"Default\" must not be a known genType")
}

// ── Phase 3: Volume Operation Integration ──────────────────────────────────
//
// These tests verify the integration contracts for Phase 3 at unit-test level:
//
//   1. Error messages from validateVolSize include the rounded size in bytes
//      (FR-2 "Include rounded size in all volume operation failure error messages")
//   2. Clone receives genType-aware size: validateVolSize with "EC" produces 1 GiB
//      multiples, which is exactly the sizeInKbytes that Clone receives (FR-2)
//   3. Restore receives genType-aware size: same flow as clone (FR-2)
//   4. Ephemeral volumes inherit Gen2/EC granularity via CreateVolume (Q2, FR-2)
//   5. CreateVolume and ControllerExpandVolume error messages include rounded size (FR-2)

// ── 3.1  validateVolSize error message contains the rounded size ────────────

func TestValidateVolSize_LimitExceeded_ErrorContainsRoundedSize(t *testing.T) {
	// 3 GiB request on Gen2/EC rounds to 3 GiB → 3*bytesInGiB bytes.
	// Set limit to 2 GiB → exceeds limit → OutOfRange error whose message
	// must contain the rounded size (3221225472) so operators can diagnose.
	cr := &csi.CapacityRange{
		RequiredBytes: 3 * bytesInGiB,
		LimitBytes:    2 * bytesInGiB,
	}
	_, err := validateVolSize(cr, "EC")
	require.Error(t, err)
	errMsg := err.Error()
	roundedSizeStr := fmt.Sprintf("%d", 3*bytesInGiB)
	assert.Contains(t, errMsg, roundedSizeStr,
		"error message must contain the rounded size (%s bytes) so operators can diagnose", roundedSizeStr)
}

func TestValidateVolSize_LimitExceeded_Gen1ErrorContainsRoundedSize(t *testing.T) {
	// 3 GiB request on Gen1 rounds to 8 GiB → 8*bytesInGiB bytes.
	// Set limit to 4 GiB → exceeds limit → error must contain 8 GiB in bytes.
	cr := &csi.CapacityRange{
		RequiredBytes: 3 * bytesInGiB,
		LimitBytes:    4 * bytesInGiB,
	}
	_, err := validateVolSize(cr, "")
	require.Error(t, err)
	errMsg := err.Error()
	roundedSizeStr := fmt.Sprintf("%d", 8*bytesInGiB)
	assert.Contains(t, errMsg, roundedSizeStr,
		"error message must contain the rounded size (%s bytes)", roundedSizeStr)
}

// ── 3.2  Clone path: sizeInKbytes received == validateVolSize output ────────
//
// Clone() receives sizeInKbytes from CreateVolume, which already called
// validateVolSize(cr, genType). Verify that for Gen2/EC the KiB value is
// correctly aligned to 1 GiB (not 8 GiB).

func TestClonePath_Gen2EC_SizeIsOneGiBAligned(t *testing.T) {
	// 3 GiB request on Gen2/EC must yield 3 GiB (not 8 GiB) as sizeInKbytes.
	cr := &csi.CapacityRange{RequiredBytes: 3 * bytesInGiB}
	sizeKiB, _ := validateVolSize(cr, "EC")
	assert.Equal(t, int64(3*kiBytesInGiB), sizeKiB,
		"Clone sizeInKbytes for 3 GiB on Gen2/EC must be 3 GiB, not 8 GiB")
}

func TestClonePath_Gen2EC_NonIntegerGiBRoundsUp(t *testing.T) {
	// 1.2 GB (truncated to 1 GiB in bytes) on Gen2/EC → rounds up to 2 GiB.
	reqBytes := int64(math.Trunc(float64(bytesInGiB) * 1.2))
	cr := &csi.CapacityRange{RequiredBytes: reqBytes}
	sizeKiB, _ := validateVolSize(cr, "EC")
	assert.Equal(t, int64(2*kiBytesInGiB), sizeKiB,
		"Clone sizeInKbytes for 1.2 GiB on Gen2/EC must be 2 GiB")
}

func TestClonePath_Gen1_SizeIsEightGiBAligned(t *testing.T) {
	// 3 GiB request on Gen1 must yield 8 GiB (backward-compat).
	cr := &csi.CapacityRange{RequiredBytes: 3 * bytesInGiB}
	sizeKiB, _ := validateVolSize(cr, "")
	assert.Equal(t, int64(8*kiBytesInGiB), sizeKiB,
		"Clone sizeInKbytes for 3 GiB on Gen1 must be 8 GiB")
}

// ── 3.3  Restore (snapshot) path: same size routing as clone ───────────────
//
// createVolumeFromSnapshot() also receives sizeInKbytes from CreateVolume, so
// the same genType-aware size applies.

func TestRestorePath_Gen2EC_SizeIsOneGiBAligned(t *testing.T) {
	// Same assertion as clone: the sizeInKbytes handed to createVolumeFromSnapshot
	// must be 1 GiB-aligned for EC, not 8 GiB-aligned.
	cr := &csi.CapacityRange{RequiredBytes: 3 * bytesInGiB}
	sizeKiB, _ := validateVolSize(cr, "EC")
	assert.Equal(t, int64(3*kiBytesInGiB), sizeKiB)
}

// ── 3.4  Ephemeral volume path ──────────────────────────────────────────────
//
// Ephemeral inline volumes are created via NodePublishVolume →
// ephemeralNodePublish → CreateVolume (ephemeral.go). Because they share the
// same CreateVolume code path, genType-aware rounding is inherited automatically
// with no additional code change needed.
//
// This test documents the static architecture contract: the KiB size produced
// by validateVolSize for a 3 GiB request on Gen2/EC is 3 GiB — the same value
// CreateVolume would pass to the PowerFlex API for both regular and ephemeral
// volumes.

func TestEphemeralPath_InheritsGen2ECSizeViaCreateVolume(t *testing.T) {
	cr := &csi.CapacityRange{RequiredBytes: 3 * bytesInGiB}
	sizeKiB, _ := validateVolSize(cr, "EC")
	// For a regular 3 GiB PVC on EC, CreateVolume would receive 3 GiB in KiB.
	// An ephemeral volume with the same spec follows the identical code path.
	assert.Equal(t, int64(3*kiBytesInGiB), sizeKiB,
		"Ephemeral volumes must inherit 1 GiB granularity for EC via shared CreateVolume path")
}

// ── Phase 4: Size Surfacing & Kubernetes Events ────────────────────────────

// ── 4.1  capacity_bytes in RPC responses ───────────────────────────────────
//
// CreateVolumeResponse.volume.capacity_bytes is set from vol.SizeInKb*bytesInKiB
// (see service.go getCSIVolume). Because the array provisions the rounded size,
// capacity_bytes automatically reflects the rounded value.
//
// ControllerExpandVolumeResponse.capacity_bytes = requestedSize * bytesInKiB,
// where requestedSize is the output of validateVolSize — i.e. the rounded size.
//
// These tests verify the arithmetic contract so a future refactor cannot break
// the guarantee accidentally.

func TestCapacityBytes_CreateVolume_Gen2EC_ReflectsRoundedSize(t *testing.T) {
	// 3 GiB request on Gen2/EC → 3 GiB rounded → capacity_bytes = 3 GiB in bytes.
	cr := &csi.CapacityRange{RequiredBytes: 3 * bytesInGiB}
	sizeKiB, _ := validateVolSize(cr, "EC")
	capacityBytes := sizeKiB * bytesInKiB
	assert.Equal(t, int64(3*bytesInGiB), capacityBytes,
		"CreateVolumeResponse.capacity_bytes must equal rounded size (3 GiB for 3 GiB EC request)")
}

func TestCapacityBytes_CreateVolume_Gen2EC_NonInteger_ReflectsRoundedUp(t *testing.T) {
	// 1.2 GiB (truncated) on Gen2/EC → rounds up to 2 GiB → capacity_bytes = 2 GiB.
	reqBytes := int64(math.Trunc(float64(bytesInGiB) * 1.2))
	cr := &csi.CapacityRange{RequiredBytes: reqBytes}
	sizeKiB, _ := validateVolSize(cr, "EC")
	capacityBytes := sizeKiB * bytesInKiB
	assert.Equal(t, int64(2*bytesInGiB), capacityBytes,
		"CreateVolumeResponse.capacity_bytes must equal 2 GiB for 1.2 GiB EC request")
}

func TestCapacityBytes_ExpandVolume_Gen2EC_NoRoundingCase(t *testing.T) {
	// Exact 5 GiB on Gen2/EC → no rounding → capacity_bytes = 5 GiB exactly.
	cr := &csi.CapacityRange{RequiredBytes: 5 * bytesInGiB}
	sizeKiB, _ := validateVolSize(cr, "EC")
	capacityBytes := sizeKiB * bytesInKiB
	assert.Equal(t, int64(5*bytesInGiB), capacityBytes,
		"ControllerExpandVolumeResponse.capacity_bytes must match requested size when no rounding needed")
}

// ── 4.2  RoundingEventEmitter ──────────────────────────────────────────────
//
// FR-5: RoundingEventEmitter must emit Normal K8s events on PVCs when volume
// size is rounded. When no rounding occurs (originalBytes == roundedBytes),
// no event must be emitted.

func TestRoundingEventEmitter_EmitsEventWhenRoundingOccurs(t *testing.T) {
	fakeRecorder := record.NewFakeRecorder(10)
	e := &RoundingEventEmitter{recorder: fakeRecorder}

	e.EmitRounded("my-pvc", "default",
		int64(math.Trunc(float64(bytesInGiB)*1.2)), // original: 1.2 GiB truncated
		2*bytesInGiB, // rounded:  2 GiB
		"create", "EC")

	select {
	case event := <-fakeRecorder.Events:
		assert.Contains(t, event, "Normal")
		assert.Contains(t, event, EventReasonVolumeSizeRounded)
		assert.Contains(t, event, "create")
		assert.Contains(t, event, "EC")
	default:
		t.Fatal("expected a VolumeSizeRounded event but none was emitted")
	}
}

func TestRoundingEventEmitter_NoEventWhenNoRounding(t *testing.T) {
	fakeRecorder := record.NewFakeRecorder(10)
	e := &RoundingEventEmitter{recorder: fakeRecorder}

	// originalBytes == roundedBytes → no rounding → no event.
	e.EmitRounded("my-pvc", "default", 3*bytesInGiB, 3*bytesInGiB, "create", "EC")

	select {
	case event := <-fakeRecorder.Events:
		t.Fatalf("expected no event but got: %s", event)
	default:
		// correct: no event emitted
	}
}

func TestRoundingEventEmitter_NilRecorder_NoOp(t *testing.T) {
	// Emitter with nil recorder must not panic.
	e := &RoundingEventEmitter{recorder: nil}
	assert.NotPanics(t, func() {
		e.EmitRounded("pvc", "ns", 1*bytesInGiB, 2*bytesInGiB, "create", "EC")
	})
}

func TestRoundingEventEmitter_EventMessageContainsOriginalAndRoundedSize(t *testing.T) {
	fakeRecorder := record.NewFakeRecorder(10)
	e := &RoundingEventEmitter{recorder: fakeRecorder}

	original := int64(math.Trunc(float64(bytesInGiB) * 12.2))
	rounded := int64(13 * bytesInGiB)
	e.EmitRounded("pvc-x", "ns-y", original, rounded, "expand", "EC")

	select {
	case event := <-fakeRecorder.Events:
		assert.Contains(t, event, fmt.Sprintf("%d", original), "event must contain original size")
		assert.Contains(t, event, fmt.Sprintf("%d", rounded), "event must contain rounded size")
		assert.Contains(t, event, "expand", "event must contain operation name")
		assert.Contains(t, event, "EC", "event must contain genType")
	default:
		t.Fatal("expected event but none was emitted")
	}
}

func TestNewRoundingEventEmitter_NilClientset_ReturnsNoopEmitter(t *testing.T) {
	e := NewRoundingEventEmitter(nil, "csi-vxflexos.dellemc.com")
	require.NotNil(t, e)
	// recorder is nil → EmitRounded must be a no-op (no panic).
	assert.NotPanics(t, func() {
		e.EmitRounded("pvc", "ns", 1*bytesInGiB, 2*bytesInGiB, "create", "EC")
	})
}
