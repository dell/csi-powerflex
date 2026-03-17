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
//

package service

import (
	"context"
	"strings"
	"testing"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGroupControllerGetCapabilities(t *testing.T) {
	gc := &groupControllerService{s: &service{}}
	resp, err := gc.GroupControllerGetCapabilities(context.Background(), &csi.GroupControllerGetCapabilitiesRequest{})

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.Capabilities, 1)

	capability := resp.Capabilities[0].GetRpc()
	assert.NotNil(t, capability)
	assert.Equal(t, csi.GroupControllerServiceCapability_RPC_CREATE_DELETE_GET_VOLUME_GROUP_SNAPSHOT, capability.GetType())
}

func TestGetPluginCapabilities_GroupControllerService(t *testing.T) {
	svc := &service{mode: "controller"}
	resp, err := svc.GetPluginCapabilities(context.Background(), &csi.GetPluginCapabilitiesRequest{})

	assert.NoError(t, err)
	assert.NotNil(t, resp)

	var foundGroupController bool
	for _, capability := range resp.GetCapabilities() {
		if capability.GetService().GetType() == csi.PluginCapability_Service_GROUP_CONTROLLER_SERVICE {
			foundGroupController = true
		}
	}
	assert.True(t, foundGroupController, "Expected GetPluginCapabilities to advertise GROUP_CONTROLLER_SERVICE")
}

func TestGetPluginCapabilities_NodeMode_NoGroupController(t *testing.T) {
	svc := &service{mode: "node"}
	resp, err := svc.GetPluginCapabilities(context.Background(), &csi.GetPluginCapabilitiesRequest{})

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Empty(t, resp.GetCapabilities(), "Node mode should not advertise any controller capabilities")
}

// --- CreateVolumeGroupSnapshot validation tests ---

func TestCreateVolumeGroupSnapshot_MissingName(t *testing.T) {
	gc := &groupControllerService{s: &service{}}
	_, err := gc.CreateVolumeGroupSnapshot(context.Background(), &csi.CreateVolumeGroupSnapshotRequest{
		SourceVolumeIds: []string{"sys1-vol1"},
	})
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "name is required")
}

func TestCreateVolumeGroupSnapshot_EmptySourceVolumeIds(t *testing.T) {
	gc := &groupControllerService{s: &service{}}
	_, err := gc.CreateVolumeGroupSnapshot(context.Background(), &csi.CreateVolumeGroupSnapshotRequest{
		Name:            "test-group",
		SourceVolumeIds: []string{},
	})
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "source_volume_ids")
}

// --- truncateGroupSnapName tests ---

func TestTruncateGroupSnapName_ShortNameFits(t *testing.T) {
	// "short-0" = 7 chars, well under 31
	result := truncateGroupSnapName("short", 0)
	assert.Equal(t, "short-0", result)
	assert.LessOrEqual(t, len(result), maxPowerFlexNameLen)
}

func TestTruncateGroupSnapName_ExactFit(t *testing.T) {
	// Build a name that with suffix "-0" is exactly 31 chars
	// 29 chars + "-0" = 31
	base := "abcdefghijklmnopqrstuvwxyz012" // 29 chars
	result := truncateGroupSnapName(base, 0)
	assert.Equal(t, base+"-0", result)
	assert.Equal(t, maxPowerFlexNameLen, len(result))
}

func TestTruncateGroupSnapName_NeedsTruncation(t *testing.T) {
	// 35-char base + "-0" = 37 chars, exceeds 31
	base := "groupsnapcontent-abcdefghijklmnop" // 33 chars
	result := truncateGroupSnapName(base, 0)
	assert.LessOrEqual(t, len(result), maxPowerFlexNameLen,
		"truncated name %q (%d chars) exceeds %d", result, len(result), maxPowerFlexNameLen)
	assert.Contains(t, result, "-0") // suffix preserved
	// Should be exactly 31 chars
	assert.Equal(t, 31, len(result))
	// Should keep the END of the base name (last 29 chars) + "-0"
	assert.Equal(t, base[len(base)-29:]+"-0", result)
	assert.True(t, strings.HasSuffix(result, "klmnop-0"), "should preserve end of base name")
}

func TestTruncateGroupSnapName_VeryLongName(t *testing.T) {
	base := "snapshot-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // 46 chars
	result := truncateGroupSnapName(base, 99)
	assert.LessOrEqual(t, len(result), maxPowerFlexNameLen)
	assert.Contains(t, result, "-99")
}

func TestTruncateGroupSnapName_LargeIndex(t *testing.T) {
	// With index=99999, suffix "-99999" is 6 chars
	result := truncateGroupSnapName("my-group-snapshot-name", 99999)
	assert.LessOrEqual(t, len(result), maxPowerFlexNameLen)
	assert.Contains(t, result, "-99999")
}

func TestTruncateGroupSnapName_DifferentIndicesProduceDifferentResults(t *testing.T) {
	// Same base name with different indices should produce different results
	base := "very-long-group-snapshot-name-xyz"
	result0 := truncateGroupSnapName(base, 0)
	result1 := truncateGroupSnapName(base, 1)
	result2 := truncateGroupSnapName(base, 2)
	assert.NotEqual(t, result0, result1, "different indices should produce different names")
	assert.NotEqual(t, result1, result2, "different indices should produce different names")
	assert.LessOrEqual(t, len(result0), maxPowerFlexNameLen)
	assert.LessOrEqual(t, len(result1), maxPowerFlexNameLen)
	assert.LessOrEqual(t, len(result2), maxPowerFlexNameLen)
}

func TestTruncateGroupSnapName_Deterministic(t *testing.T) {
	// Same input always produces the same output (idempotent for retries)
	base := "groupsnapcontent-869fcba12345678"
	r1 := truncateGroupSnapName(base, 2)
	r2 := truncateGroupSnapName(base, 2)
	assert.Equal(t, r1, r2)
}

// Test end-truncation preserves uniqueness
func TestTruncateGroupSnapName_SingleDigitIndex(t *testing.T) {
	// For index 0-9, suffix is 2 chars ("-0" to "-9")
	// Keeps last 29 chars of base + "-5" = 31 chars
	base := "12345678901234567890123456789012345678901234567890" // 50 chars
	result := truncateGroupSnapName(base, 5)
	assert.Equal(t, 31, len(result), "result should be exactly 31 chars")
	assert.Equal(t, base[len(base)-29:]+"-5", result)
	assert.True(t, strings.HasSuffix(result, "01234567890-5"))
}

func TestTruncateGroupSnapName_TwoDigitIndex(t *testing.T) {
	// For index 10-99, suffix is 3 chars ("-10" to "-99")
	// Keeps last 28 chars of base + "-42" = 31 chars
	base := "12345678901234567890123456789012345678901234567890" // 50 chars
	result := truncateGroupSnapName(base, 42)
	assert.Equal(t, 31, len(result), "result should be exactly 31 chars")
	assert.Equal(t, base[len(base)-28:]+"-42", result)
	assert.True(t, strings.HasSuffix(result, "1234567890-42"))
}

func TestTruncateGroupSnapName_ThreeDigitIndex(t *testing.T) {
	// For index 100-999, suffix is 4 chars ("-100" to "-999")
	// Keeps last 27 chars of base + "-101" = 31 chars
	base := "12345678901234567890123456789012345678901234567890" // 50 chars
	result := truncateGroupSnapName(base, 101)
	assert.Equal(t, 31, len(result), "result should be exactly 31 chars")
	assert.Equal(t, base[len(base)-27:]+"-101", result)
	assert.Equal(t, "101", result[28:], "last 3 chars should be the index")
}

func TestTruncateGroupSnapName_FourDigitIndex(t *testing.T) {
	// For index 1000-9999, suffix is 5 chars ("-1000" to "-9999")
	// Keeps last 26 chars of base + "-1234" = 31 chars
	base := "12345678901234567890123456789012345678901234567890" // 50 chars
	result := truncateGroupSnapName(base, 1234)
	assert.Equal(t, 31, len(result), "result should be exactly 31 chars")
	assert.Equal(t, base[len(base)-26:]+"-1234", result)
	assert.Equal(t, "1234", result[27:], "last 4 chars should be the index")
}

func TestTruncateGroupSnapName_EdgeCase_Index0(t *testing.T) {
	// Index 0 should work correctly
	base := "very-long-base-name-that-exceeds-limit"
	result := truncateGroupSnapName(base, 0)
	assert.Equal(t, 31, len(result))
	assert.Contains(t, result, "-0")
}

func TestTruncateGroupSnapName_EdgeCase_Index999(t *testing.T) {
	// Index 999 (boundary of 3-digit)
	base := "very-long-base-name-that-exceeds-limit"
	result := truncateGroupSnapName(base, 999)
	assert.Equal(t, 31, len(result))
	assert.Contains(t, result, "-999")
}

func TestTruncateGroupSnapName_EdgeCase_Index1000(t *testing.T) {
	// Index 1000 (first 4-digit)
	base := "very-long-base-name-that-exceeds-limit"
	result := truncateGroupSnapName(base, 1000)
	assert.Equal(t, 31, len(result))
	assert.Contains(t, result, "-1000")
}

func TestTruncateGroupSnapName_EndTruncationPreventsCollisions(t *testing.T) {
	// Different base names should produce different truncated results
	// This is the key fix - keeping the END preserves UUID uniqueness
	base1 := "groupsnapshot-005f08b0-edda-4b29-9b56-bd37c6b7280c"
	base2 := "groupsnapshot-c59d9b11-7218-4f0e-8578-e07058bf85e6"
	base3 := "groupsnapshot-76d40fb2-8bac-44dd-a8d0-aa84b53eb6f5"

	result1_0 := truncateGroupSnapName(base1, 0)
	result1_1 := truncateGroupSnapName(base1, 1)
	result2_0 := truncateGroupSnapName(base2, 0)
	result2_1 := truncateGroupSnapName(base2, 1)
	result3_0 := truncateGroupSnapName(base3, 0)
	result3_1 := truncateGroupSnapName(base3, 1)

	// All results should be unique because we keep the unique UUID suffix
	results := []string{result1_0, result1_1, result2_0, result2_1, result3_0, result3_1}
	uniqueMap := make(map[string]bool)
	for _, r := range results {
		assert.False(t, uniqueMap[r], "duplicate truncated name detected: %s", r)
		uniqueMap[r] = true
		assert.LessOrEqual(t, len(r), maxPowerFlexNameLen)
	}

	// Verify all 6 results are unique
	assert.Equal(t, 6, len(uniqueMap), "all truncated names should be unique")

	// Verify they preserve the unique UUID suffixes
	assert.True(t, strings.Contains(result1_0, "bd37c6b7280c"), "should contain unique UUID suffix")
	assert.True(t, strings.Contains(result2_0, "e07058bf85e6"), "should contain unique UUID suffix")
	assert.True(t, strings.Contains(result3_0, "aa84b53eb6f5"), "should contain unique UUID suffix")
}

func TestCreateVolumeGroupSnapshot_NoSystemID(t *testing.T) {
	gc := &groupControllerService{s: &service{
		opts: Opts{defaultSystemID: ""},
	}}
	// Volume ID without system prefix and no default system
	_, err := gc.CreateVolumeGroupSnapshot(context.Background(), &csi.CreateVolumeGroupSnapshotRequest{
		Name:            "test-group",
		SourceVolumeIds: []string{"vol1"},
	})
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "systemID is not found")
}

// --- DeleteVolumeGroupSnapshot validation tests ---

func TestDeleteVolumeGroupSnapshot_MissingGroupSnapshotId(t *testing.T) {
	gc := &groupControllerService{s: &service{}}
	_, err := gc.DeleteVolumeGroupSnapshot(context.Background(), &csi.DeleteVolumeGroupSnapshotRequest{})
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "group_snapshot_id is required")
}

func TestDeleteVolumeGroupSnapshot_InvalidGroupSnapshotId(t *testing.T) {
	gc := &groupControllerService{s: &service{}}
	_, err := gc.DeleteVolumeGroupSnapshot(context.Background(), &csi.DeleteVolumeGroupSnapshotRequest{
		GroupSnapshotId: "nohyphen",
	})
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "invalid group_snapshot_id")
}

func TestGetVolumeGroupSnapshot_MissingGroupSnapshotId(t *testing.T) {
	gc := &groupControllerService{s: &service{}}
	_, err := gc.GetVolumeGroupSnapshot(context.Background(), &csi.GetVolumeGroupSnapshotRequest{})
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "group_snapshot_id is required")
}

func TestGetVolumeGroupSnapshot_InvalidGroupSnapshotId(t *testing.T) {
	gc := &groupControllerService{s: &service{}}
	_, err := gc.GetVolumeGroupSnapshot(context.Background(), &csi.GetVolumeGroupSnapshotRequest{
		GroupSnapshotId: "invalid",
	})
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "invalid group_snapshot_id")
}

// --- parseGroupSnapshotID tests ---

func TestParseGroupSnapshotID_Valid(t *testing.T) {
	sysID, cgID, err := parseGroupSnapshotID("systemABC-cg123")
	assert.NoError(t, err)
	assert.Equal(t, "systemABC", sysID)
	assert.Equal(t, "cg123", cgID)
}

func TestParseGroupSnapshotID_NoDash(t *testing.T) {
	_, _, err := parseGroupSnapshotID("nohyphen")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected format")
}

func TestParseGroupSnapshotID_DashAtStart(t *testing.T) {
	_, _, err := parseGroupSnapshotID("-cg123")
	assert.Error(t, err)
}

func TestParseGroupSnapshotID_DashAtEnd(t *testing.T) {
	_, _, err := parseGroupSnapshotID("system-")
	assert.Error(t, err)
}

func TestParseGroupSnapshotID_Empty(t *testing.T) {
	_, _, err := parseGroupSnapshotID("")
	assert.Error(t, err)
}
