package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/dell-csi-extensions/replication"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sio "github.com/dell/goscaleio"
	siotypes "github.com/dell/goscaleio/types/v1"
)

type ErrorCode int

const (
	ErrSuccess ErrorCode = 65
)

const (
	sioReplicationPairsDoesNotExist = "Error in get relationship ReplicationPair"
)

func (s *service) GetReplicationCapabilities(ctx context.Context, req *replication.GetReplicationCapabilityRequest) (*replication.GetReplicationCapabilityResponse, error) {
	var rep = new(replication.GetReplicationCapabilityResponse)
	rep.Capabilities = []*replication.ReplicationCapability{
		{
			Type: &replication.ReplicationCapability_Rpc{
				Rpc: &replication.ReplicationCapability_RPC{
					Type: replication.ReplicationCapability_RPC_CREATE_REMOTE_VOLUME,
				},
			},
		},
		{
			Type: &replication.ReplicationCapability_Rpc{
				Rpc: &replication.ReplicationCapability_RPC{
					Type: replication.ReplicationCapability_RPC_CREATE_PROTECTION_GROUP,
				},
			},
		},
		{
			Type: &replication.ReplicationCapability_Rpc{
				Rpc: &replication.ReplicationCapability_RPC{
					Type: replication.ReplicationCapability_RPC_DELETE_PROTECTION_GROUP,
				},
			},
		},
		{
			Type: &replication.ReplicationCapability_Rpc{
				Rpc: &replication.ReplicationCapability_RPC{
					Type: replication.ReplicationCapability_RPC_REPLICATION_ACTION_EXECUTION,
				},
			},
		},
		{
			Type: &replication.ReplicationCapability_Rpc{
				Rpc: &replication.ReplicationCapability_RPC{
					Type: replication.ReplicationCapability_RPC_MONITOR_PROTECTION_GROUP,
				},
			},
		},
	}
	rep.Actions = []*replication.SupportedActions{
		{
			Actions: &replication.SupportedActions_Type{
				Type: replication.ActionTypes_FAILOVER_REMOTE,
			},
		},
		{
			Actions: &replication.SupportedActions_Type{
				Type: replication.ActionTypes_UNPLANNED_FAILOVER_LOCAL,
			},
		},
		{
			Actions: &replication.SupportedActions_Type{
				Type: replication.ActionTypes_REPROTECT_LOCAL,
			},
		},
		{
			Actions: &replication.SupportedActions_Type{
				Type: replication.ActionTypes_SUSPEND,
			},
		},
		{
			Actions: &replication.SupportedActions_Type{
				Type: replication.ActionTypes_RESUME,
			},
		},
		{
			Actions: &replication.SupportedActions_Type{
				Type: replication.ActionTypes_SYNC,
			},
		},
		{
			Actions: &replication.SupportedActions_Type{
				Type: replication.ActionTypes_CREATE_SNAPSHOT,
			},
		},
		{
			Actions: &replication.SupportedActions_Type{
				Type: replication.ActionTypes_ABORT_SNAPSHOT,
			},
		},
	}
	return rep, nil
}

func (s *service) CreateStorageProtectionGroup(ctx context.Context, req *replication.CreateStorageProtectionGroupRequest) (*replication.CreateStorageProtectionGroupResponse, error) {
	Log.Printf("[CreateStorageProtectionGroup] - req %+v", req)

	volHandleCtx := req.GetVolumeHandle()
	if volHandleCtx == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID is required")
	}

	parameters := req.GetParameters()
	if len(parameters) == 0 {
		return nil, status.Error(codes.InvalidArgument, "empty parameters list")
	}

	volumeID := getVolumeIDFromCsiVolumeID(volHandleCtx)
	systemID := s.getSystemIDFromCsiVolumeID(volHandleCtx)

	if volumeID == "" || systemID == "" {
		return nil, status.Error(codes.InvalidArgument, "failed to provide system ID or volume ID")
	}

	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	localSystem, err := s.getSystem(systemID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "couldn't getSystem (local): %s", err.Error())
	}

	localProtectionDomain, err := s.getProtectionDomain(systemID, localSystem)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "couldn't getProtectionDomain (local): %s", err.Error())
	}
	Log.Printf("[CreateStorageProtectionGroup] - Local Protection Domain: %+v", localProtectionDomain)

	remoteSystem, err := s.getSystem(parameters["replication.storage.dell.com/remoteSystem"])
	if err != nil {
		return nil, status.Errorf(codes.Internal, "couldn't getSystem (remote): %s", err.Error())
	}

	remoteProtectionDomain, err := s.getProtectionDomain(parameters["replication.storage.dell.com/remoteSystem"], remoteSystem)
	if err != nil {
		return nil, err
	}

	_, err = s.getPeerMdms(systemID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "can't query peer mdms: %s", err.Error())
	}

	var consistencyGroupName string
	if parameters["replication.storage.dell.com/consistencyGroupName"] != "" {
		consistencyGroupName = parameters["replication.storage.dell.com/consistencyGroupName"]
	} else {
		consistencyGroupName = "rcg-" + systemID[:12] + "-" + remoteSystem.ID[:12]
	}

	localRcg, err := s.CreateReplicationConsistencyGroup(systemID, consistencyGroupName,
		parameters["replication.storage.dell.com/rpo"], localProtectionDomain[0].ID,
		remoteProtectionDomain[0].ID, "", remoteSystem.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "invalid rcg response: %s", err.Error())
	}

	vol, err := s.getVolByID(volumeID, systemID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "can't query volume: %s", err.Error())
	}

	remoteVolumeName := "replicated-" + vol.Name

	if err := s.requireProbe(ctx, remoteSystem.ID); err != nil {
		return nil, status.Errorf(codes.Internal, "can't probe remote system: %s", err.Error())
	}

	adminClient := s.adminClients[remoteSystem.ID]
	if adminClient == nil {
		return nil, fmt.Errorf("can't find adminClient by id %s", systemID)
	}

	remoteVolumeID, err := adminClient.FindVolumeID(remoteVolumeName)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "can't find volume %s by name: %s", remoteVolumeName, err.Error())
	}

	replicationPairName := "rp-" + vol.ID[:12] + "-" + remoteVolumeID[:12]
	_, err = s.CreateReplicationPair(systemID, replicationPairName, vol.ID, remoteVolumeID, localRcg.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "can't createReplicationPair: %s", err.Error())
	}

	group, err := s.getReplicationConsistencyGroupById(systemID, localRcg.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "No replication consistency groups found: %s", err.Error())
	}

	localParams := map[string]string{
		"systemName":     localSystem.ID,
		"remoteSystemID": remoteSystem.ID,
	}

	remoteParams := map[string]string{
		"systemName":     remoteSystem.ID,
		"remoteSystemID": localSystem.ID,
	}

	Log.Printf("[CreateStorageProtectionGroup] - localRcg: %+s, group.ID: %s", localRcg.ID, group.ID)

	return &replication.CreateStorageProtectionGroupResponse{
		LocalProtectionGroupId:         group.ID,
		LocalProtectionGroupAttributes: localParams,

		RemoteProtectionGroupId:         group.RemoteID,
		RemoteProtectionGroupAttributes: remoteParams,
	}, nil
}

func (s *service) CreateRemoteVolume(ctx context.Context, req *replication.CreateRemoteVolumeRequest) (*replication.CreateRemoteVolumeResponse, error) {
	Log.Printf("[CreateRemoteVolume] - req %+v", req)

	volHandleCtx := req.GetVolumeHandle()
	parameters := req.GetParameters()
	if volHandleCtx == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID is required")
	}

	volumeID := getVolumeIDFromCsiVolumeID(volHandleCtx)
	systemID := s.getSystemIDFromCsiVolumeID(volHandleCtx)

	Log.Printf("Volume ID: %s System ID: %s", volumeID, systemID)

	if volumeID == "" || systemID == "" {
		return nil, status.Error(codes.InvalidArgument, "failed to provide system ID or volume ID")
	}

	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	/*
		Todo: PowerStore Flow:
			1. Gets the Volume Groups (vgs) via the volumeID.
			2. Gets the Replication Session by the Local Resource ID.
			3. Traverses the Storage Elements to get the remote volume ID.
	*/

	vol, err := s.getVolByID(volumeID, systemID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "can't query volume: %s", err.Error())
	}

	_, err = s.getSystem(systemID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not get (local) system %s: %s", systemID, err.Error())
	}

	// Probe the remote system
	remoteSystemID := parameters["replication.storage.dell.com/remoteSystem"]
	if err := s.requireProbe(ctx, remoteSystemID); err != nil {
		Log.Infof("Remote probe failed: %s", err)
		return nil, err
	}

	remoteSystem, err := s.getSystem(remoteSystemID)
	if err != nil {
		return nil, err
	}

	_, err = s.getPeerMdms(systemID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "can't getPeerMDMs: %s", err.Error())
	}

	name := "replicated-" + vol.Name
	volReq := createRemoteCreateVolumeRequest(name, parameters["replication.storage.dell.com/remoteStoragePool"], remoteSystem.ID)
	volReq.CapacityRange.RequiredBytes = int64(vol.SizeInKb)

	createVolumeResponse, err := s.CreateVolume(ctx, volReq)
	if err != nil {
		log.Printf("CreateVolume called failed: %s", err.Error())
		return nil, err
	} else {
		log.Printf("Potentially created a remote volume: %+v", createVolumeResponse)
	}

	remoteParams := map[string]string{
		"storagePool":    parameters["replication.storage.dell.com/remoteStoragePool"],
		"remoteSystem":   remoteSystem.ID,
		"remoteVolumeID": createVolumeResponse.Volume.VolumeId,
	}

	remoteVolume := getRemoteCSIVolume(createVolumeResponse.GetVolume().VolumeId, vol.SizeInKb)
	remoteVolume.VolumeContext = remoteParams
	return &replication.CreateRemoteVolumeResponse{
		RemoteVolume: remoteVolume,
	}, nil
}

func (s *service) DeleteStorageProtectionGroup(ctx context.Context, req *replication.DeleteStorageProtectionGroupRequest) (*replication.DeleteStorageProtectionGroupResponse, error) {
	Log.Printf("[DeleteStorageProtectionGroup] %+v", req)

	protectionGroupSystem := req.ProtectionGroupAttributes["systemName"]

	pairs, err := s.getReplicationPair(protectionGroupSystem, req.ProtectionGroupId)
	if err != nil {
		return nil, err
	}

	if len(pairs) != 0 {
		return nil, status.Errorf(codes.Internal, "unable to delete protection group, pairs exist")
	}

	err = s.DeleteReplicationConsistencyGroup(protectionGroupSystem, req.ProtectionGroupId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error deleting the replication consistency group: %s", err.Error())
	}

	return &replication.DeleteStorageProtectionGroupResponse{}, nil
}

func (s *service) ExecuteAction(ctx context.Context, req *replication.ExecuteActionRequest) (*replication.ExecuteActionResponse, error) {
	Log.Printf("[ExecuteAction] - req %+v", req)

	action := req.GetAction().GetActionTypes().String()
	protectionGroupID := req.GetProtectionGroupId()
	localParams := req.GetProtectionGroupAttributes()
	remoteParams := req.GetRemoteProtectionGroupAttributes()
	actionAttributes := make(map[string]string)
	remoteSystem := remoteParams["systemName"]
	localSystem := localParams["systemName"]

	statusResp, err := s.GetStorageProtectionGroupStatus(ctx, &replication.GetStorageProtectionGroupStatusRequest{
		ProtectionGroupId:         protectionGroupID,
		ProtectionGroupAttributes: localParams,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "unable to get storage protection group status: %s", err.Error())
	}

	switch action {
	case replication.ActionTypes_CREATE_SNAPSHOT.String():
		// TODO: Add delay for duplicate snapshots.
		resp, err := s.CreateReplicationConsistencyGroupSnapshot(localSystem, protectionGroupID)
		if err != nil {
			return nil, err
		}

		counter := 0

		for len(actionAttributes) == 0 && counter < 10 {
			actionAttributes, err = s.getConsistencyGroupSnapshotContent(localSystem, remoteSystem, protectionGroupID, resp.SnapshotGroupID)
			if err != nil {
				return nil, err
			}
			time.Sleep(1 * time.Second)
			counter++
		}
	case replication.ActionTypes_FAILOVER_REMOTE.String():
		if _, err := s.waitForConsistency(localSystem, protectionGroupID); err != nil {
			return nil, err
		}

		if err := s.ExecuteSwitchoverOnReplicationGroup(localSystem, protectionGroupID); err != nil {
			return nil, err
		}

	case replication.ActionTypes_UNPLANNED_FAILOVER_LOCAL.String():
		if _, err := s.waitForConsistency(localSystem, protectionGroupID); err != nil {
			return nil, err
		}

		if err := s.ExecuteFailoverOnReplicationGroup(localSystem, protectionGroupID); err != nil {
			return nil, err
		}

	case replication.ActionTypes_REPROTECT_LOCAL.String():
		if err := s.ensureFailover(localSystem, protectionGroupID); err != nil {
			return nil, err
		}

		if err := s.ExecuteReverseOnReplicationGroup(localSystem, protectionGroupID); err != nil {
			return nil, err
		}

	case replication.ActionTypes_RESUME.String():
		failover := statusResp.Status.State == replication.StorageProtectionGroupStatus_FAILEDOVER
		paused := statusResp.Status.State == replication.StorageProtectionGroupStatus_SUSPENDED
		if paused || failover {
			if err := s.ExecuteResumeOnReplicationGroup(localSystem, protectionGroupID, failover); err != nil {
				return nil, err
			}
		}
	case replication.ActionTypes_SUSPEND.String():
		paused := statusResp.Status.State == replication.StorageProtectionGroupStatus_SUSPENDED
		if !paused {
			if err := s.ExecutePauseOnReplicationGroup(localSystem, protectionGroupID); err != nil {
				return nil, err
			}
		}
	default:
		return nil, status.Errorf(codes.Unknown, "The requested action does not match with supported actions")
	}

	statusResp, err = s.GetStorageProtectionGroupStatus(ctx, &replication.GetStorageProtectionGroupStatusRequest{
		ProtectionGroupId:         protectionGroupID,
		ProtectionGroupAttributes: localParams,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "unable to get storage protection group status: %s", err.Error())
	}

	resp := &replication.ExecuteActionResponse{
		Success: true,
		ActionTypes: &replication.ExecuteActionResponse_Action{
			Action: req.GetAction(),
		},
		Status:           statusResp.Status,
		ActionAttributes: actionAttributes,
	}

	return resp, nil
}

func (s *service) GetStorageProtectionGroupStatus(ctx context.Context, req *replication.GetStorageProtectionGroupStatusRequest) (*replication.GetStorageProtectionGroupStatusResponse, error) {
	Log.Printf("[GetStorageProtectionGroupStatus] - req %+v", req)

	protectionGroupSystem := req.ProtectionGroupAttributes["systemName"]

	group, err := s.getReplicationConsistencyGroupById(protectionGroupSystem, req.ProtectionGroupId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "No replication consistency groups found: %s", err.Error())
	}

	pairs, err := s.getReplicationPair(protectionGroupSystem, req.ProtectionGroupId)
	if err != nil {
		return nil, err
	}

	if len(pairs) == 0 {
		return nil, status.Errorf(codes.Internal, "no replication pairs exist")
	}

	Log.Printf("[GetStorageProtectionGroupStatus] - group %+v", group)

	var state replication.StorageProtectionGroupStatus_State
	switch group.CurrConsistMode {
	case sio.PARTIALLY_CONSISTENT, sio.CONSISTENT_PENDING:
		state = replication.StorageProtectionGroupStatus_SYNC_IN_PROGRESS
	case sio.CONSISTENT:
		state = replication.StorageProtectionGroupStatus_SYNCHRONIZED
	default:
		Log.Printf("The status (%s) does not match with known protection group states", group.CurrConsistMode)
		state = replication.StorageProtectionGroupStatus_UNKNOWN
	}

	if group.AbstractState == "StoppedByUser" {
		if isFailover(group) {
			state = replication.StorageProtectionGroupStatus_FAILEDOVER
		} else if isPaused(group) {
			state = replication.StorageProtectionGroupStatus_SUSPENDED
		}
	}

	return &replication.GetStorageProtectionGroupStatusResponse{
		Status: &replication.StorageProtectionGroupStatus{
			State:    state,
			IsSource: group.ReplicationDirection == "LocalToRemote",
		},
	}, nil
}

func (s *service) getReplicationConsistencyGroupById(systemID string, groupId string) (*siotypes.ReplicationConsistencyGroup, error) {
	adminClient := s.adminClients[systemID]
	if adminClient == nil {
		return nil, fmt.Errorf("can't find adminClient by id %s", systemID)
	}

	group, err := adminClient.GetReplicationConsistencyGroupById(groupId)
	if err != nil {
		return nil, err
	}

	return group, nil
}

func (s *service) getReplicationPair(systemID string, groupId string) ([]*siotypes.ReplicationPair, error) {
	adminClient := s.adminClients[systemID]
	if adminClient == nil {
		return nil, fmt.Errorf("can't find adminClient by id %s", systemID)
	}

	pairs, err := adminClient.GetReplicationPairs(groupId)
	if err != nil {
		if !strings.EqualFold(err.Error(), sioReplicationPairsDoesNotExist) {
			Log.Printf("Error getting replication pairs: %s", err.Error())
			return nil, err
		}
	}

	return pairs, nil
}

func getRemoteCSIVolume(volumeID string, size int) *replication.Volume {
	volume := &replication.Volume{
		CapacityBytes: int64(size),
		VolumeId:      volumeID,

		// TODO: add values to volume context if needed
		VolumeContext: nil,
	}
	return volume
}

func createRemoteCreateVolumeRequest(name string, storagePool string, systemID string) *csi.CreateVolumeRequest {
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	params["storagepool"] = storagePool
	params["systemID"] = systemID
	req.Parameters = params
	req.Name = name
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = 32 * 1024 * 1024 * 1024
	req.CapacityRange = capacityRange
	block := new(csi.VolumeCapability_BlockVolume)
	capability := new(csi.VolumeCapability)
	accessType := new(csi.VolumeCapability_Block)
	accessType.Block = block
	capability.AccessType = accessType
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	capability.AccessMode = accessMode
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities
	return req
}

func isFailover(group *siotypes.ReplicationConsistencyGroup) bool {
	// return group.CurrConsistMode == "Consistent" && group.FailoverType != "None"
	return group.FailoverType != "None"
}

func isPaused(group *siotypes.ReplicationConsistencyGroup) bool {
	// return group.CurrConsistMode == "Consistent" && group.PauseMode != "None"
	return group.PauseMode != "None"
}

func (s *service) getConsistencyGroupSnapshotContent(localSystem, remoteSystem, protectionGroup, snapshotGroup string) (map[string]string, error) {
	actionAttributes := make(map[string]string)

	pairs, err := s.getReplicationPair(localSystem, protectionGroup)
	if err != nil {
		return nil, err
	}

	for _, pair := range pairs {
		existingSnaps, _, err := s.listVolumes(remoteSystem, 0, 0, false, false, "", pair.RemoteVolumeID)
		if err != nil {
			return nil, err
		}

		for _, snap := range existingSnaps {
			if snapshotGroup == snap.ConsistencyGroupID {
				actionAttributes[localSystem+"-"+pair.LocalVolumeID] = remoteSystem + "-" + snap.ID
			}
		}
	}

	return actionAttributes, nil
}

func (s *service) ensureFailover(systemID string, replicationGroupID string) error {
	for i := 0; i < 30; i++ {
		group, err := s.getReplicationConsistencyGroupById(systemID, replicationGroupID)
		if err != nil {
			return status.Errorf(codes.Internal, "No replication consistency groups found: %s", err.Error())
		}

		Log.Printf("[ensureFailover] - %+v", group)

		if isFailover(group) && group.FailoverState == "Done" && group.DisasterRecoveryState == "Neutral" && group.RemoteDisasterRecoveryState == "Neutral" {
			Log.Printf("[ensureFailover] - Failover achieved...slight delay...")
			time.Sleep(5 * time.Second)
			return nil
		}

		time.Sleep(1 * time.Second)
	}

	return status.Errorf(codes.Internal, "unable to reach failover consistency")
}

func (s *service) waitForConsistency(systemID string, replicationGroupID string) (*siotypes.ReplicationConsistencyGroup, error) {
	for i := 0; i < 20; i++ {
		group, err := s.getReplicationConsistencyGroupById(systemID, replicationGroupID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "No replication consistency groups found: %s", err.Error())
		}

		if group.CurrConsistMode == "Consistent" {
			Log.Printf("Consistency Group %s - Reached Consistency.", group.Name)
			return group, nil
		}

		Log.Printf("[waitForConsistency] - Not consistent.")

		time.Sleep(3 * time.Second)
	}

	return nil, errors.New("consistency group did not reach consistency.")
}
