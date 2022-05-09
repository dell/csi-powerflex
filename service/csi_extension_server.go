package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	podmon "github.com/dell/dell-csi-extensions/podmon"
	volumeGroupSnapshot "github.com/dell/dell-csi-extensions/volumeGroupSnapshot"
	sio "github.com/dell/goscaleio"
	siotypes "github.com/dell/goscaleio/types/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	//ExistingGroupID group id on powerflex array
	ExistingGroupID = "existingSnapshotGroupID"
)

func (s *service) ValidateVolumeHostConnectivity(ctx context.Context, req *podmon.ValidateVolumeHostConnectivityRequest) (*podmon.ValidateVolumeHostConnectivityResponse, error) {
	Log.Infof("ValidateVolumeHostConnectivity called %+v", req)
	rep := &podmon.ValidateVolumeHostConnectivityResponse{
		Messages: make([]string, 0),
	}

	if (len(req.GetVolumeIds()) == 0 || len(req.GetVolumeIds()) == 0) && req.GetNodeId() == "" {
		// This is a nop call just testing the interface is present
		rep.Messages = append(rep.Messages, "ValidateVolumeHostConnectivity is implemented")
		return rep, nil
	}

	// The NodeID for the VxFlex OS is the SdcGUID field.
	if req.GetNodeId() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "The NodeID is a required field")
	}

	systemID := req.GetArrayId()
	if systemID == "" {
		if len(req.GetVolumeIds()) > 0 {
			systemID = s.getSystemIDFromCsiVolumeID(req.GetVolumeIds()[0])
		}
		if systemID == "" {
			systemID = s.opts.defaultSystemID
		}
	}

	// Do a probe of the requested system
	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	// First- check to see if the SDC is Connected or Disconnected.
	// Then retrieve the SDC and seet the connection state
	sdc, err := s.systems[systemID].FindSdc("SdcGUID", req.GetNodeId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "NodeID is invalid: %s - there is no corresponding SDC", req.GetNodeId())
	}
	connectionState := sdc.Sdc.MdmConnectionState
	rep.Messages = append(rep.Messages, fmt.Sprintf("SDC connection state: %s", connectionState))
	rep.Connected = (connectionState == "Connected")

	// Second- check to see if the Volumes have any I/O in the recent past.
	for _, volID := range req.GetVolumeIds() {
		// Probe system
		prevSystemID := systemID
		systemID = s.getSystemIDFromCsiVolumeID(volID)
		if systemID == "" {
			systemID = s.opts.defaultSystemID
		}
		if prevSystemID != systemID {
			if err := s.requireProbe(ctx, systemID); err != nil {
				rep.Messages = append(rep.Messages, fmt.Sprintf("Could not probe system: %s", volID))
				continue
			}
		}
		// Get the Volume
		vol, err := s.getVolByID(getVolumeIDFromCsiVolumeID(volID), systemID)
		if err != nil {
			rep.Messages = append(rep.Messages, fmt.Sprintf("Could not retrieve volume: %s", volID))
			continue
		}
		// Get the volume statistics
		volume := sio.NewVolume(s.adminClients[systemID])
		volume.Volume = vol
		stats, err := volume.GetVolumeStatistics()
		if err != nil {
			rep.Messages = append(rep.Messages, fmt.Sprintf("Could not retrieve volume statistics: %s", volID))
			continue
		}
		readCount := stats.UserDataReadBwc.NumOccured
		writeCount := stats.UserDataWriteBwc.NumOccured
		sampleSeconds := stats.UserDataWriteBwc.NumSeconds
		rep.Messages = append(rep.Messages, fmt.Sprintf("Volume %s writes %d reads %d for %d seconds",
			volID, writeCount, readCount, sampleSeconds))
		if (readCount + writeCount) > 0 {
			rep.IosInProgress = true
		}
	}

	Log.Infof("ValidateVolumeHostConnectivity reply %+v", rep)
	return rep, nil
}

func (s *service) CreateVolumeGroupSnapshot(ctx context.Context, req *volumeGroupSnapshot.CreateVolumeGroupSnapshotRequest) (*volumeGroupSnapshot.CreateVolumeGroupSnapshotResponse, error) {
	Log.Infof("CreateVolumeGroupSnapshot called with req: %v", req)

	err := validateCreateVGSreq(req)
	if err != nil {
		Log.Errorf("Error from CreateVolumeGroupSnapshot: %v ", err)
		return nil, err
	}

	//take first volume to calculate systemID. It is expected this systemID is consistent throughout
	systemID, err := s.getSystemID(req)
	if err != nil {
		Log.Errorf("Error from CreateVolumeGroupSnapshot: %v ", err)
		return nil, err
	}

	// Do a probe of the requested system
	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	Log.Infof("Creating Snapshot Consistency Group on system: %s", systemID)

	snapshotDefs, err := s.buildSnapshotDefs(req, systemID)

	if err != nil {
		Log.Errorf("Error from CreateVolumeGroupSnapshot: %v ", err)
		return nil, err
	}

	snapParam := &siotypes.SnapshotVolumesParam{SnapshotDefs: snapshotDefs}

	//check if req is Idempotent, return group found if yes
	existingGroup, err := s.checkIdempotency(ctx, snapParam, systemID, req.Parameters[ExistingGroupID])
	if err != nil {
		return nil, err
	}
	if existingGroup != nil {
		return existingGroup, nil
	}

	// Create snapshot(s), Idempotent requests will already be returned before this is called
	snapResponse, err := s.systems[systemID].CreateSnapshotConsistencyGroup(snapParam)
	if err != nil {
		var snapsThatFailed []string
		for _, snap := range snapshotDefs {
			snapsThatFailed = append(snapsThatFailed, snap.SnapshotName)
		}
		err = status.Errorf(codes.Internal, "Failed to create group with snapshots %s : %s", snapsThatFailed, err.Error())
		Log.Errorf("Error from CreateVolumeGroupSnapshot: %v ", err)
		return nil, err
	}
	Log.Infof("snapResponse is: %s", snapResponse)
	//populate response
	groupSnapshots, err := s.buildCreateVGSResponse(ctx, snapResponse, snapshotDefs, systemID)
	if err != nil {
		Log.Errorf("Error from CreateVolumeGroupSnapshot: %v ", err)
		return nil, err
	}

	//Check  Creation time, should be the same across all volumes
	err = checkCreationTime(groupSnapshots[0].CreationTime, groupSnapshots)
	if err != nil {
		return nil, err
	}

	resp := &volumeGroupSnapshot.CreateVolumeGroupSnapshotResponse{SnapshotGroupID: systemID + "-" + snapResponse.SnapshotGroupID, Snapshots: groupSnapshots, CreationTime: groupSnapshots[0].CreationTime}

	Log.Infof("CreateVolumeGroupSnapshot Response:  %#v", resp)
	return resp, nil
}

func checkCreationTime(time int64, snapshots []*volumeGroupSnapshot.Snapshot) error {
	Log.Infof("CheckCreationTime called with snapshots: %v", snapshots)
	for _, snap := range snapshots {
		if time != snap.CreationTime {
			err := status.Errorf(codes.Internal, "Creation time of snapshot %s, %d does not match with snapshot %s creation time %d. All snapshot creation times should be equal", snap.Name, snap.CreationTime, snapshots[0].Name, snapshots[0].CreationTime)
			Log.Errorf("Error from CheckCreationTime: %v ", err)
			return err
		}
		Log.Infof("CheckCreationTime: Creation time of %s is %d", snap.Name, time)

	}
	return nil
}

func (s *service) getSystemID(req *volumeGroupSnapshot.CreateVolumeGroupSnapshotRequest) (string, error) {
	//take first volume to calculate systemID. It is expected this systemID is consistent throughout
	systemID := s.getSystemIDFromCsiVolumeID(req.SourceVolumeIDs[0])
	if systemID == "" {
		// use default system
		systemID = s.opts.defaultSystemID
	}

	if systemID == "" {
		err := status.Error(codes.InvalidArgument, "systemID is not found in vol ID and there is no default system")
		Log.Errorf("Error from getSystemID: %v ", err)
		return systemID, err

	}

	return systemID, nil

}

//validate if request has source volumes, a VGS name, and VGS name length < 27 chars
func validateCreateVGSreq(req *volumeGroupSnapshot.CreateVolumeGroupSnapshotRequest) error {
	if len(req.SourceVolumeIDs) == 0 {
		err := status.Errorf(codes.InvalidArgument, "SourceVolumeIDs cannot be empty")
		Log.Errorf("Error from validateCreateVGSreq: %v ", err)
		return err
	}

	if req.Name == "" {
		err := status.Error(codes.InvalidArgument, "CreateVolumeGroupSnapshotRequest Name is not  set")
		Log.Warnf("Warning from validateCreateVGSreq: %v ", err)
	}

	//name must be less than 28 chars, because we name snapshots with -<index>, and index can at most be 3 chars
	if len(req.Name) > 27 {
		err := status.Errorf(codes.InvalidArgument, "Requested name %s longer than 27 character max", req.Name)
		Log.Errorf("Error from validateCreateVGSreq: %v ", err)
		return err
	}

	return nil
}

func (s *service) buildSnapshotDefs(req *volumeGroupSnapshot.CreateVolumeGroupSnapshotRequest, systemID string) ([]*siotypes.SnapshotDef, error) {

	snapshotDefs := make([]*siotypes.SnapshotDef, 0)

	for _, id := range req.SourceVolumeIDs {
		snapSystemID := strings.TrimSpace(s.getSystemIDFromCsiVolumeID(id))
		if snapSystemID != "" && snapSystemID != systemID {
			err := status.Errorf(codes.Internal, "Source volumes for volume group snapshot should be on the same system but vol %s is not on system: %s", id, systemID)
			Log.Errorf("Error from buildSnapshotDefs: %v \n", err)
			return nil, err
		}

		//legacy vol check
		err := s.checkVolumesMap(id)
		if err != nil {
			err = status.Errorf(codes.Internal, "checkVolumesMap for id: %s failed : %s", id, err.Error())
			Log.Errorf("Error from buildSnapshotDefs: %v ", err)
			return nil, err
		}

		volID := getVolumeIDFromCsiVolumeID(id)

		_, err = s.getVolByID(volID, systemID)
		if err != nil {
			err = status.Errorf(codes.Internal, "failure checking source volume status: %s", err.Error())
			Log.Errorf("Error from buildSnapshotDefs: %v ", err)
			return nil, err
		}

		snapDef := siotypes.SnapshotDef{VolumeID: volID, SnapshotName: ""}
		snapshotDefs = append(snapshotDefs, &snapDef)
	}

	return snapshotDefs, nil

}

//A VolumeGroupSnapshot request is idempotent if the following criteria is met:
//1. For each snapshot we intend to make, there is a snapshot with the same name and ancestor ID on array
//2. Each snapshot that we find to satisfy criteria 1 all belong to the same consitency group
//3. The consistency group that satisfies criteria 2 contain no other snapshots
func (s *service) checkIdempotency(ctx context.Context, snapshotsToMake *siotypes.SnapshotVolumesParam, systemID string, snapGrpID string) (*volumeGroupSnapshot.CreateVolumeGroupSnapshotResponse, error) {
	Log.Infof("CheckIdempotency called")

	//We use maps to keep track of info, to ensure criterias 1-3 are met

	//Maps snapshots we intend to create (from snapshotsToMake)  -> boolean (boolean is true if snapshot already exists on aray, false otherwise)
	idempotencyMap := make(map[string]bool)

	//Maps idempotent snapshots -> consistency group ID
	consistencyGroupMap := make(map[string]string)

	//A list of snapshots found within the consistency group on the array. This is expensive to compute, so it's
	//filled in last
	var consistencyGroupOnArray []string

	//Maps snapshot name -> snapshot ID
	IDsForResponse := make(map[string]string)

	//go through the existing vols, and update maps as needed
	//check that all idempotent snapshots belong to the same consistency group.
	//this check verifies criteria #2
	consitencyGroupValue := snapGrpID
	var idempotencyValue bool
	for _, snap := range snapshotsToMake.SnapshotDefs {
		//snapshots will always have a  consistency group ID, so setting it to "", means no snapshot was found
		existingSnaps, _ := s.adminClients[systemID].GetVolume("", "", snap.VolumeID, "", true)
		idempotencyMap[snap.VolumeID] = false
		for _, existingSnap := range existingSnaps {
			consistencyGroupMap[existingSnap.Name] = ""
			//a snapshot in snapshotsToMake already exists in array, update maps
			foundGrpID := systemID + "-" + existingSnap.ConsistencyGroupID
			if snap.VolumeID == existingSnap.AncestorVolumeID && systemID+"-"+snapGrpID == foundGrpID {
				Log.Infof("Snapshot for %s exists on array for group id %s", snap.VolumeID, foundGrpID)
				idempotencyMap[snap.VolumeID] = true
				idempotencyValue = true
				consistencyGroupMap[existingSnap.Name] = foundGrpID
				IDsForResponse[existingSnap.Name] = existingSnap.ID
			} else {
				delete(consistencyGroupMap, existingSnap.Name)
			}
		}
	}

	//check Idempotency map. Either all snapshots can be idempodent, or none can be idempotent. A mixture is not allowed
	//this check verifies criteria #1
	for snap := range idempotencyMap {
		if idempotencyMap[snap] != idempotencyValue {
			err := status.Error(codes.Internal, "Some snapshots exist on array, while others need to be created. Cannot create VolumeGroupSnapshot")
			Log.Errorf("Error from checkIdempotency: %v ", err)
			return nil, err
		} else if idempotencyValue {
			Log.Debugf("snap: %s already exists on array", snap)
		} else {
			Log.Debugf("snap: %s does not already exist on array", snap)
		}
	}
	//since we know all values in idempotencyMap match idempotencyValue, we can return now
	if idempotencyValue == false {
		return nil, nil

	}

	//now we need to check that the consistency group contains no extra snaps. This is done last.
	existingVols, _ := s.adminClients[systemID].GetVolume("", "", "", "", true)
	for _, vol := range existingVols {
		grpID := systemID + "-" + vol.ConsistencyGroupID
		if grpID == systemID+"-"+consitencyGroupValue {
			Log.Infof("Checking  %s: Snapshot %s found in consistency group.", consitencyGroupValue, vol.Name)
			consistencyGroupOnArray = append(consistencyGroupOnArray, vol.Name)
		}
	}

	//we know from criteria #2 that all idempotent snaps are in the same consistency group, so now we need to ensure that they're
	//the only snaps in the consistency group.
	//this check verifies criteria #3
	if len(consistencyGroupOnArray) != len(IDsForResponse) {
		err := status.Errorf(codes.Internal, "CG: %s contains more snapshots than requested. Cannot create VolumeGroupSnapshot", consitencyGroupValue)
		Log.Errorf("Error from checkIdempotency: %v ", err)
		return nil, err
	}

	Log.Infof("Request is idempotent")

	//with all 3 criteria met, we need to return a CreateVolumeGroupSnapshotResponse with the VGS that satisfied the criteria
	var groupSnapshots []*volumeGroupSnapshot.Snapshot
	for snap := range IDsForResponse {
		id := IDsForResponse[snap]
		idToQuery := systemID + "-" + id
		req := &csi.ListSnapshotsRequest{SnapshotId: idToQuery}
		existingSnap, err := s.ListSnapshots(ctx, req)
		if err != nil {
			Log.Errorf("Failed to list snaps")
		}
		creationTime := existingSnap.Entries[0].Snapshot.CreationTime.GetSeconds()*1000000000 + int64(existingSnap.Entries[0].Snapshot.CreationTime.GetNanos())
		fmt.Printf("Creation time is: %d\n", creationTime)
		snap := volumeGroupSnapshot.Snapshot{
			Name:          snap,
			CapacityBytes: existingSnap.Entries[0].Snapshot.SizeBytes,
			SnapId:        existingSnap.Entries[0].Snapshot.SnapshotId,
			SourceId:      systemID + "-" + existingSnap.Entries[0].Snapshot.SourceVolumeId,
			ReadyToUse:    existingSnap.Entries[0].Snapshot.ReadyToUse,
			CreationTime:  creationTime,
		}
		groupSnapshots = append(groupSnapshots, &snap)
	}
	resp := &volumeGroupSnapshot.CreateVolumeGroupSnapshotResponse{SnapshotGroupID: systemID + "-" + consitencyGroupValue, Snapshots: groupSnapshots, CreationTime: groupSnapshots[0].CreationTime}
	Log.Infof("Returning Idempotent response: %v", resp)
	return resp, nil
}

//build the response for CreateVGS to return
func (s *service) buildCreateVGSResponse(ctx context.Context, snapResponse *siotypes.SnapshotVolumesResp, snapshotDefs []*siotypes.SnapshotDef, systemID string) ([]*volumeGroupSnapshot.Snapshot, error) {
	var groupSnapshots []*volumeGroupSnapshot.Snapshot
	for index, id := range snapResponse.VolumeIDList {
		idToQuery := systemID + "-" + id
		req := &csi.ListSnapshotsRequest{SnapshotId: idToQuery}
		lResponse, err := s.ListSnapshots(ctx, req)
		if err != nil {
			err = status.Errorf(codes.Internal, "Failed to get snapshot: %s", err.Error())
			Log.Errorf("Error from buildCreateVGSResponse: %v ", err)
			return nil, err
		}
		var arraySnapName string
		// ancestorvolumeid
		existingSnap, _ := s.adminClients[systemID].GetVolume("", id, lResponse.Entries[0].Snapshot.SourceVolumeId, "", true)
		for _, e := range existingSnap {
			if e.ID == id && e.ConsistencyGroupID == snapResponse.SnapshotGroupID {
				if e.Name == "" {
					Log.Infof("debug set snap name for [%s]", e.ID)
					arraySnapName = e.ID + "-snap-" + strconv.Itoa(index)
					tgtVol := sio.NewVolume(s.adminClients[systemID])
					tgtVol.Volume = e
					err := tgtVol.SetVolumeName(arraySnapName)
					if err != nil {
						Log.Errorf("Error setting name of snapshot id=%s name=%s %s", e.ID, arraySnapName, err.Error())
					}
				} else {
					Log.Infof("debug found snap name %s for %s", e.Name, e.ID)
					arraySnapName = e.Name
				}
			}
		}

		Log.Infof("Snapshot Name created for: %s is %s", lResponse.Entries[0].Snapshot.SnapshotId, arraySnapName)
		//need to convert time from seconds and nanoseconds to int64 nano seconds
		creationTime := lResponse.Entries[0].Snapshot.CreationTime.GetSeconds()*1000000000 + int64(lResponse.Entries[0].Snapshot.CreationTime.GetNanos())
		Log.Infof("Creation time is: %d\n", creationTime)
		snap := volumeGroupSnapshot.Snapshot{
			Name:          arraySnapName,
			CapacityBytes: lResponse.Entries[0].Snapshot.SizeBytes,
			SnapId:        lResponse.Entries[0].Snapshot.SnapshotId,
			SourceId:      systemID + "-" + lResponse.Entries[0].Snapshot.SourceVolumeId,
			ReadyToUse:    lResponse.Entries[0].Snapshot.ReadyToUse,
			CreationTime:  creationTime,
		}
		groupSnapshots = append(groupSnapshots, &snap)
	}

	return groupSnapshots, nil

}
