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
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	goscaleio "github.com/dell/goscaleio"
	siotypes "github.com/dell/goscaleio/types/v1"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
)

const (
	// maxPowerFlexNameLen is the maximum length of a volume/snapshot name on PowerFlex.
	maxPowerFlexNameLen = 31
)

// groupControllerService implements the CSI GroupControllerServer interface.
// It is kept as a separate type to cleanly encapsulate group controller logic.
type groupControllerService struct {
	csi.UnimplementedGroupControllerServer
	s *service
}

func (gc *groupControllerService) GroupControllerGetCapabilities(
	_ context.Context,
	_ *csi.GroupControllerGetCapabilitiesRequest,
) (*csi.GroupControllerGetCapabilitiesResponse, error) {
	return &csi.GroupControllerGetCapabilitiesResponse{
		Capabilities: []*csi.GroupControllerServiceCapability{
			{
				Type: &csi.GroupControllerServiceCapability_Rpc{
					Rpc: &csi.GroupControllerServiceCapability_RPC{
						Type: csi.GroupControllerServiceCapability_RPC_CREATE_DELETE_GET_VOLUME_GROUP_SNAPSHOT,
					},
				},
			},
		},
	}, nil
}

// CreateVolumeGroupSnapshot creates a crash-consistent group snapshot of multiple volumes
// on the PowerFlex array using a Snapshot Consistency Group.
func (gc *groupControllerService) CreateVolumeGroupSnapshot(
	ctx context.Context,
	req *csi.CreateVolumeGroupSnapshotRequest,
) (*csi.CreateVolumeGroupSnapshotResponse, error) {
	log.Infof("CSI GroupController CreateVolumeGroupSnapshot called with name: %s, sourceVolumeIds: %v", req.GetName(), req.GetSourceVolumeIds())

	// Validate request
	if req.GetName() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "name is required")
	}
	if len(req.GetSourceVolumeIds()) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "source_volume_ids is required and cannot be empty")
	}

	s := gc.s

	// Determine systemID from the first volume; all volumes must be on the same system
	systemID := s.getSystemIDFromCsiVolumeID(req.GetSourceVolumeIds()[0])
	if systemID == "" {
		systemID = s.opts.defaultSystemID
	}
	if systemID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "systemID is not found in volume ID and there is no default system")
	}

	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	log.Infof("Creating Snapshot Consistency Group on system: %s", systemID)

	// Build snapshot definitions for each source volume
	baseName := req.GetName()
	sourceVolumeIDs := req.GetSourceVolumeIds()
	numVolumes := len(sourceVolumeIDs)
	snapshotDefs := make([]*siotypes.SnapshotDef, 0, numVolumes)

	for index, csiVolID := range sourceVolumeIDs {
		volSystemID := strings.TrimSpace(s.getSystemIDFromCsiVolumeID(csiVolID))
		if volSystemID != "" && volSystemID != systemID {
			return nil, status.Errorf(codes.InvalidArgument,
				"source volumes must be on the same system but vol %s is not on system %s", csiVolID, systemID)
		}

		if err := s.checkVolumesMap(csiVolID); err != nil {
			return nil, status.Errorf(codes.Internal, "checkVolumesMap for id %s failed: %s", csiVolID, err.Error())
		}

		volID := getVolumeIDFromCsiVolumeID(csiVolID)
		if _, err := s.getVolByID(volID, systemID); err != nil {
			return nil, status.Errorf(codes.NotFound, "source volume %s not found: %s", csiVolID, err.Error())
		}

		snapName := truncateGroupSnapName(baseName, index)
		snapshotDefs = append(snapshotDefs, &siotypes.SnapshotDef{VolumeID: volID, SnapshotName: snapName})
	}

	// Check for idempotent request
	groupSnapshot, err := gc.checkCSIIdempotency(ctx, snapshotDefs, systemID, req.GetName())
	if err != nil {
		return nil, err
	}
	if groupSnapshot != nil {
		return &csi.CreateVolumeGroupSnapshotResponse{GroupSnapshot: groupSnapshot}, nil
	}

	// Create the consistency group snapshot on the array
	snapParam := &siotypes.SnapshotVolumesParam{SnapshotDefs: snapshotDefs}
	snapResponse, err := s.systems[systemID].CreateSnapshotConsistencyGroup(snapParam)
	if err != nil {
		var failedSnaps []string
		for _, sd := range snapshotDefs {
			failedSnaps = append(failedSnaps, sd.SnapshotName)
		}
		return nil, status.Errorf(codes.Internal, "failed to create group snapshot with snapshots %v: %s", failedSnaps, err.Error())
	}
	log.Infof("CreateSnapshotConsistencyGroup response: %v", snapResponse)

	// Build CSI response from array response
	groupSnapshot, err = gc.buildCSIGroupSnapshot(ctx, snapResponse, systemID)
	if err != nil {
		return nil, err
	}

	log.Infof("CSI GroupController CreateVolumeGroupSnapshot response: group_snapshot_id=%s, snapshots=%d",
		groupSnapshot.GroupSnapshotId, len(groupSnapshot.Snapshots))
	return &csi.CreateVolumeGroupSnapshotResponse{GroupSnapshot: groupSnapshot}, nil
}

// DeleteVolumeGroupSnapshot deletes a volume group snapshot and all its member snapshots.
// Honors the deletionPolicy from VolumeGroupSnapshotClass:
// - "Delete": Removes snapshots from the array (default behavior)
// - "Retain": Keeps snapshots on the array, only removes CSI objects
func (gc *groupControllerService) DeleteVolumeGroupSnapshot(
	ctx context.Context,
	req *csi.DeleteVolumeGroupSnapshotRequest,
) (*csi.DeleteVolumeGroupSnapshotResponse, error) {
	log.Infof("CSI GroupController DeleteVolumeGroupSnapshot called with group_snapshot_id: %s, snapshot_ids: %v",
		req.GetGroupSnapshotId(), req.GetSnapshotIds())

	if req.GetGroupSnapshotId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "group_snapshot_id is required")
	}

	s := gc.s

	// Parse systemID and consistency group ID from the composite group snapshot ID (format: systemID-cgID)
	systemID, cgID, err := parseGroupSnapshotID(req.GetGroupSnapshotId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid group_snapshot_id: %s", err.Error())
	}

	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	adminClient := s.adminClients[systemID]

	// Find all snapshots in this consistency group
	// PowerFlex REST API does not provide an endpoint to filter volumes by consistency group ID.
	allVols, err := adminClient.GetVolume("", "", "", "", true)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list volumes: %s", err.Error())
	}

	var cgVols []*siotypes.Volume
	for _, vol := range allVols {
		if vol.ConsistencyGroupID == cgID {
			cgVols = append(cgVols, vol)
		}
	}

	if len(cgVols) == 0 {
		// Already deleted — idempotent success
		log.Infof("No snapshots found for consistency group %s on system %s; treating as already deleted", cgID, systemID)
		return &csi.DeleteVolumeGroupSnapshotResponse{}, nil
	}

	// Check that none of the snapshots are in use (mapped to SDCs)
	var exposedVols []string
	for _, vol := range cgVols {
		if len(vol.MappedSdcInfo) > 0 {
			exposedVols = append(exposedVols, fmt.Sprintf("%s (%s)", vol.Name, vol.ID))
		}
	}
	if len(exposedVols) > 0 {
		return nil, status.Errorf(codes.FailedPrecondition,
			"one or more snapshots in the group are in use: %v", exposedVols)
	}

	// Delete each snapshot in the consistency group
	for _, vol := range cgVols {
		tgtVol := goscaleio.NewVolume(adminClient)
		tgtVol.Volume = vol
		if err := tgtVol.RemoveVolume(removeModeOnlyMe); err != nil {
			return nil, status.Errorf(codes.Internal, "error removing snapshot %s (%s): %s", vol.Name, vol.ID, err.Error())
		}
		log.Infof("Deleted snapshot %s (%s) from consistency group %s", vol.Name, vol.ID, cgID)
	}

	s.clearCache()
	log.Infof("Successfully deleted all snapshots in group %s on system %s", cgID, systemID)
	return &csi.DeleteVolumeGroupSnapshotResponse{}, nil
}

// GetVolumeGroupSnapshot returns information about a volume group snapshot.
func (gc *groupControllerService) GetVolumeGroupSnapshot(
	ctx context.Context,
	req *csi.GetVolumeGroupSnapshotRequest,
) (*csi.GetVolumeGroupSnapshotResponse, error) {
	log.Infof("CSI GroupController GetVolumeGroupSnapshot called with group_snapshot_id: %s, snapshot_ids: %v",
		req.GetGroupSnapshotId(), req.GetSnapshotIds())

	if req.GetGroupSnapshotId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "group_snapshot_id is required")
	}

	s := gc.s

	systemID, cgID, err := parseGroupSnapshotID(req.GetGroupSnapshotId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid group_snapshot_id: %s", err.Error())
	}

	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	adminClient := s.adminClients[systemID]

	// Find all snapshots in this consistency group
	// PowerFlex REST API does not provide an endpoint to filter volumes by consistency group ID.
	allVols, err := adminClient.GetVolume("", "", "", "", true)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list volumes: %s", err.Error())
	}

	var cgVols []*siotypes.Volume
	for _, vol := range allVols {
		if vol.ConsistencyGroupID == cgID {
			cgVols = append(cgVols, vol)
		}
	}

	if len(cgVols) == 0 {
		return nil, status.Errorf(codes.NotFound, "group snapshot %s not found", req.GetGroupSnapshotId())
	}

	// Validate provided snapshot_ids if any
	if len(req.GetSnapshotIds()) > 0 {
		providedIDs := make(map[string]bool, len(req.GetSnapshotIds()))
		for _, sid := range req.GetSnapshotIds() {
			providedIDs[sid] = false
		}
		for _, vol := range cgVols {
			csiSnapID := systemID + "-" + vol.ID
			if _, ok := providedIDs[csiSnapID]; ok {
				providedIDs[csiSnapID] = true
			}
		}
		for sid, found := range providedIDs {
			if !found {
				return nil, status.Errorf(codes.InvalidArgument,
					"snapshot %s is not part of group snapshot %s", sid, req.GetGroupSnapshotId())
			}
		}
	}

	// Build response
	var snapshots []*csi.Snapshot
	var creationTime *timestamppb.Timestamp
	allReady := true

	for _, vol := range cgVols {
		snap := s.getCSISnapshot(vol, systemID)
		snapshots = append(snapshots, snap)
		if creationTime == nil {
			creationTime = snap.CreationTime
		}
		if !snap.ReadyToUse {
			allReady = false
		}
	}

	groupSnapshot := &csi.VolumeGroupSnapshot{
		GroupSnapshotId: req.GetGroupSnapshotId(),
		Snapshots:       snapshots,
		CreationTime:    creationTime,
		ReadyToUse:      allReady,
	}

	log.Infof("CSI GroupController GetVolumeGroupSnapshot response: group_snapshot_id=%s, snapshots=%d, ready=%v",
		groupSnapshot.GroupSnapshotId, len(groupSnapshot.Snapshots), groupSnapshot.ReadyToUse)
	return &csi.GetVolumeGroupSnapshotResponse{GroupSnapshot: groupSnapshot}, nil
}

// parseGroupSnapshotID parses a composite group snapshot ID of format "systemID-cgID"
// into its component parts.
func parseGroupSnapshotID(groupSnapshotID string) (systemID string, cgID string, err error) {
	idx := strings.Index(groupSnapshotID, "-")
	if idx <= 0 || idx >= len(groupSnapshotID)-1 {
		return "", "", fmt.Errorf("expected format 'systemID-consistencyGroupID', got %q", groupSnapshotID)
	}
	return groupSnapshotID[:idx], groupSnapshotID[idx+1:], nil
}

// checkCSIIdempotency checks if a CreateVolumeGroupSnapshot request is idempotent by
// verifying that all expected snapshots already exist in the same consistency group.
func (gc *groupControllerService) checkCSIIdempotency(
	_ context.Context,
	snapshotDefs []*siotypes.SnapshotDef,
	systemID string,
	_ string,
) (*csi.VolumeGroupSnapshot, error) {
	s := gc.s
	adminClient := s.adminClients[systemID]

	// Track which snapshots already exist and their consistency groups
	existMap := make(map[string]bool) // snapName -> exists
	idMap := make(map[string]string)  // snapName -> snapID
	cgMap := make(map[string]string)  // snapName -> consistencyGroupID

	for _, sd := range snapshotDefs {
		existMap[sd.SnapshotName] = false
		existingSnaps, _ := adminClient.GetVolume("", "", sd.VolumeID, sd.SnapshotName, true)
		for _, es := range existingSnaps {
			if es.AncestorVolumeID == sd.VolumeID && es.Name == sd.SnapshotName {
				existMap[sd.SnapshotName] = true
				idMap[sd.SnapshotName] = es.ID
				cgMap[sd.SnapshotName] = es.ConsistencyGroupID
				break
			}
		}
	}

	// Check consistency: either all exist or none exist
	allExist := true
	noneExist := true
	for _, exists := range existMap {
		if exists {
			noneExist = false
		} else {
			allExist = false
		}
	}

	if noneExist {
		return nil, nil // Not idempotent — proceed with creation
	}
	if !allExist {
		return nil, status.Errorf(codes.Internal,
			"some snapshots exist on array while others do not; cannot create VolumeGroupSnapshot")
	}

	// All exist — verify they are in the same consistency group
	var cgID string
	for name, cg := range cgMap {
		if cgID == "" {
			cgID = cg
		} else if cg != cgID {
			return nil, status.Errorf(codes.Internal,
				"idempotent snapshots belong to different consistency groups: snap %s in CG %s vs expected %s", name, cg, cgID)
		}
	}

	// Verify the CG has no extra snapshots
	allVols, _ := adminClient.GetVolume("", "", "", "", true)
	cgCount := 0
	for _, vol := range allVols {
		if vol.ConsistencyGroupID == cgID {
			cgCount++
		}
	}
	if cgCount != len(idMap) {
		return nil, status.Errorf(codes.Internal,
			"consistency group %s contains %d snapshots but expected %d", cgID, cgCount, len(idMap))
	}

	log.Infof("CreateVolumeGroupSnapshot request is idempotent for CG %s", cgID)

	// Build VolumeGroupSnapshot from existing snapshots
	var snapshots []*csi.Snapshot
	var creationTime *timestamppb.Timestamp

	for snapName, snapID := range idMap {
		_ = snapName
		vol, err := s.getVolByID(snapID, systemID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get snapshot %s: %s", snapID, err.Error())
		}
		snap := s.getCSISnapshot(vol, systemID)
		snapshots = append(snapshots, snap)
		if creationTime == nil {
			creationTime = snap.CreationTime
		}
	}

	return &csi.VolumeGroupSnapshot{
		GroupSnapshotId: systemID + "-" + cgID,
		Snapshots:       snapshots,
		CreationTime:    creationTime,
		ReadyToUse:      true,
	}, nil
}

// buildCSIGroupSnapshot builds a VolumeGroupSnapshot response from the array's
// CreateSnapshotConsistencyGroup response.
func (gc *groupControllerService) buildCSIGroupSnapshot(
	_ context.Context,
	snapResponse *siotypes.SnapshotVolumesResp,
	systemID string,
) (*csi.VolumeGroupSnapshot, error) {
	s := gc.s
	adminClient := s.adminClients[systemID]
	var snapshots []*csi.Snapshot
	var creationTime *timestamppb.Timestamp
	allReady := true

	for index, snapID := range snapResponse.VolumeIDList {
		vol, err := s.getVolByID(snapID, systemID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get snapshot %s: %s", snapID, err.Error())
		}

		// Ensure the snapshot has a name; if not, assign one
		if vol.Name == "" {
			assignedName := snapID + "-snap-" + strconv.Itoa(index)
			tgtVol := goscaleio.NewVolume(adminClient)
			tgtVol.Volume = vol
			if err := tgtVol.SetVolumeName(assignedName); err != nil {
				log.Errorf("Error setting name of snapshot id=%s name=%s: %s", snapID, assignedName, err.Error())
			}
		}

		csiSnap := &csi.Snapshot{
			SizeBytes:      int64(vol.SizeInKb) * bytesInKiB,
			SnapshotId:     systemID + "-" + vol.ID,
			SourceVolumeId: systemID + "-" + vol.AncestorVolumeID,
			ReadyToUse:     true,
			CreationTime:   timestamppb.New(time.Unix(int64(vol.CreationTime), 0)),
		}
		snapshots = append(snapshots, csiSnap)

		if creationTime == nil {
			creationTime = csiSnap.CreationTime
		}
		if !csiSnap.ReadyToUse {
			allReady = false
		}
	}

	return &csi.VolumeGroupSnapshot{
		GroupSnapshotId: systemID + "-" + snapResponse.SnapshotGroupID,
		Snapshots:       snapshots,
		CreationTime:    creationTime,
		ReadyToUse:      allReady,
	}, nil
}

// truncateGroupSnapName ensures the snapshot name fits within PowerFlex's 31-character limit.
//
// When the requested name is short enough (baseName + "-" + index <= 31), it is used as-is.
// When the full name would exceed 31 characters, it truncates from the END of the baseName
// to preserve the unique suffix (typically the UUID portion), which prevents collisions:
//   - Long name: groupsnapshot-005f08b0-edda-4b29-9b56-bd37c6b7280c-0
//   - Truncated: ...b56-bd37c6b7280c-0 (keeps unique UUID suffix)
//
// This approach is simpler than hashing and works because Kubernetes-generated names
// have unique suffixes (UUIDs) that differ at the end.

func truncateGroupSnapName(baseName string, index int) string {
	suffix := "-" + strconv.Itoa(index)
	fullName := baseName + suffix

	// If the full name fits within PowerFlex's 31-char limit, use it as-is
	if len(fullName) <= maxPowerFlexNameLen {
		return fullName
	}

	// Name is too long - truncate from the END of baseName to preserve uniqueness
	// Calculate how many characters the suffix will take (including the hyphen)
	suffixLen := len(suffix)

	// Calculate maximum length for the base name
	maxBaseLen := maxPowerFlexNameLen - suffixLen

	// Ensure we have room for at least a minimal base
	if maxBaseLen < 1 {
		maxBaseLen = 1
	}

	// Take the LAST maxBaseLen characters from baseName (preserves UUID suffix)
	truncated := baseName
	if len(baseName) > maxBaseLen {
		// Truncate from the beginning, keeping the end
		truncated = baseName[len(baseName)-maxBaseLen:]
	}

	result := truncated + suffix

	log.Infof("Truncated group snapshot name from %q to %q (%d chars) to fit PowerFlex %d-char limit",
		fullName, result, len(result), maxPowerFlexNameLen)
	return result
}
