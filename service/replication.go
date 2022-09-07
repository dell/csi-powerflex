package service

import (
	"context"
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
	Log.Printf("req GetReplicationCapabilities %+v", req)
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
	Log.Printf("rep GetReplicationCapabilities %+v", rep)
	return rep, nil
}

func (s *service) CreateStorageProtectionGroup(ctx context.Context, req *replication.CreateStorageProtectionGroupRequest) (*replication.CreateStorageProtectionGroupResponse, error) {
	Log.Printf("[CreateStorageProtectionGroup] - req %+v", req)
	Log.Printf("[CreateStorageProtectionGroup] - ctx %+v", ctx)

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

	Log.Printf("[CreateStorageProtectionGroup] - Volume ID: %s System ID: %s", volumeID, systemID)

	if volumeID == "" || systemID == "" {
		return nil, status.Error(codes.InvalidArgument, "failed to provide system ID or volume ID")
	}

	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	localSystem, err := s.getSystem(systemID)
	if err != nil {
		return nil, err
	}
	Log.Printf("[CreateStorageProtectionGroup] - Local System Content: %+v", localSystem)

	localProtectionDomain, err := s.getProtectionDomain(systemID, localSystem)
	if err != nil {
		return nil, err
	}
	Log.Printf("[CreateStorageProtectionGroup] - Local Protection Domain: %+v", localProtectionDomain)

	remoteSystem, err := s.getSystem(parameters["replication.storage.dell.com/remoteSystem"])
	if err != nil {
		return nil, err
	}
	Log.Printf("[CreateStorageProtectionGroup] - Remote System Content: %+v", remoteSystem)

	remoteProtectionDomain, err := s.getProtectionDomain(parameters["replication.storage.dell.com/remoteSystem"], remoteSystem)
	if err != nil {
		return nil, err
	}
	Log.Printf("[CreateStorageProtectionGroup] - Remote Protection Domain: %+v", remoteProtectionDomain[0])

	mdms, err := s.getPeerMdms(systemID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "can't query volume: %s", err.Error())
	}

	Log.Printf("MDMs: %+v", mdms[0])

	// Truncate ID to name to fit correctly
	consistencyGroupName := "rcg-" + systemID[:12] + "-" + remoteSystem.ID[:12]
	localRcg, err := s.CreateReplicationConsistencyGroup(systemID, consistencyGroupName,
		parameters["replication.storage.dell.com/rpo"], localProtectionDomain[0].ID,
		remoteProtectionDomain[0].ID, "", remoteSystem.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "invalid rcg response: %s", err.Error())
	}

	Log.Printf("[CreateStorageProtectionGroup] - RCGRESP %+v", localRcg)

	vol, err := s.getVolByID(volumeID, systemID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "can't query volume: %s", err.Error())
	}

	remoteVolumeName := "replicated-" + vol.Name

	// Probe the remote system
	if err := s.requireProbe(ctx, remoteSystem.ID); err != nil {
		return nil, err
	}

	adminClient := s.adminClients[remoteSystem.ID]
	if adminClient == nil {
		return nil, fmt.Errorf("can't find adminClient by id %s", systemID)
	}

	remoteVolumeID, err := adminClient.FindVolumeID(remoteVolumeName)
	if err != nil {
		return nil, fmt.Errorf("can't find volume by name %s", remoteVolumeName)
	}

	Log.Printf("[CreateStorageProtectionGroup] - vol.id %s, rmVolId %s, rcgId %s", vol.ID, remoteVolumeID, localRcg.ID)

	replicationPairName := "rp-" + vol.ID[:12] + "-" + remoteVolumeID[:12]
	rpResp, err := s.CreateReplicationPair(systemID, replicationPairName, vol.ID, remoteVolumeID, localRcg.ID)
	if err != nil {
		return nil, err
	}

	Log.Printf("[CreateStorageProtectionGroup] - rpResp %+v", rpResp)

	// Get Remote Content
	groups, err := adminClient.GetReplicationConsistencyGroups()
	if err != nil {
		return nil, err
	}

	var remoteGroupId string
	for _, rcg := range groups {
		if rcg.Name == consistencyGroupName {
			remoteGroupId = rcg.ID
		}
	}

	if remoteGroupId == "" {
		return nil, status.Errorf(codes.Internal, "remote replication consistency group not found")
	}

	pairs, err := adminClient.GetReplicationPairs(remoteGroupId)
	if err != nil {
		return nil, err
	}

	var remotePairId string
	for _, pair := range pairs {
		if pair.Name == replicationPairName {
			remotePairId = pair.ID
		}
	}

	// What is needed for the parameters?
	localParams := map[string]string{
		"replicationPairID": rpResp.ID,
		"systemName":        systemID,
	}

	remoteParams := map[string]string{
		"replicationPairID": remotePairId,
		"systemName":        remoteSystem.ID,
	}

	return &replication.CreateStorageProtectionGroupResponse{
		LocalProtectionGroupId:         localRcg.ID,
		LocalProtectionGroupAttributes: localParams,

		RemoteProtectionGroupId:         remoteGroupId,
		RemoteProtectionGroupAttributes: remoteParams,
	}, nil
}

func (s *service) CreateRemoteVolume(ctx context.Context, req *replication.CreateRemoteVolumeRequest) (*replication.CreateRemoteVolumeResponse, error) {
	Log.Printf("[CreateRemoteVolume] - req %+v", req)
	Log.Printf("[CreateRemoteVolume] - ctx %+v", ctx)

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
	Log.Printf("Volume Content: %+v", vol)

	localSystem, err := s.getSystem(systemID)
	if err != nil {
		return nil, err
	}
	Log.Printf("Local System Content: %+v", localSystem)

	// Probe the remote system
	remoteSystemID := parameters["replication.storage.dell.com/remoteSystem"]
	Log.Printf("Probing remote system...")
	if err := s.requireProbe(ctx, remoteSystemID); err != nil {
		Log.Infof("Remote probe failed: %s", err)
		return nil, err
	}

	Log.Printf("Getting remoteSystem %s", remoteSystemID)
	remoteSystem, err := s.getSystem(remoteSystemID)
	if err != nil {
		return nil, err
	}
	Log.Printf("Remote System Content: %+v", remoteSystem)

	mdms, err := s.getPeerMdms(systemID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "can't query volume: %s", err.Error())
	}

	Log.Printf("MDMs: %+v", mdms[0])

	// Create a volume on the remote system?
	name := "replicated-" + vol.Name
	Log.Printf("[CreateRemoteVolume] - Name: %s", name)

	volReq := createRemoteCreateVolumeRequest(name, parameters["replication.storage.dell.com/remoteStoragePool"], remoteSystem.ID)
	volReq.CapacityRange.RequiredBytes = int64(vol.SizeInKb)

	Log.Printf("[CreateRemoteVolume] - Remote volReq:%+v", volReq)

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
		Log.Printf("[DeleteStorageProtectionGroup] ERROR!!! %s", err.Error())
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

	switch action {
	case replication.ActionTypes_CREATE_SNAPSHOT.String():
		resp, err := s.CreateReplicationConsistencyGroupSnapshot(localSystem, req.GetProtectionGroupId())
		if err != nil {
			return nil, err
		}

		counter := 0

		// Needs a delay for the array to create the snap volumes.
		for len(actionAttributes) == 0 && counter < 10 {
			actionAttributes, err = s.getConsistencyGroupSnapshotContent(localSystem, remoteSystem, protectionGroupID, resp.SnapshotGroupID)
			if err != nil {
				return nil, err
			}
			time.Sleep(1 * time.Second)
			counter++
		}
	default:
		return nil, status.Errorf(codes.Unknown, "The requested action does not match with supported actions")
	}

	statusResp, err := s.GetStorageProtectionGroupStatus(ctx, &replication.GetStorageProtectionGroupStatusRequest{
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

	Log.Printf("[GetStorageProtectionGroupStatus] - Group Result: %+v", group)

	if ErrorCode(group.Error) != ErrSuccess {
		return &replication.GetStorageProtectionGroupStatusResponse{
			Status: &replication.StorageProtectionGroupStatus{
				State: replication.StorageProtectionGroupStatus_INVALID,
			},
		}, nil
	}

	var state replication.StorageProtectionGroupStatus_State
	switch group.CurrConsistMode {
	case sio.PARTIALLY_CONSISTENT:
		state = replication.StorageProtectionGroupStatus_SYNC_IN_PROGRESS
	case sio.CONSISTENT:
		state = replication.StorageProtectionGroupStatus_SYNCHRONIZED
	default:
		Log.Printf("The status (%s) does not match with known protection group states", group.CurrConsistMode)
		state = replication.StorageProtectionGroupStatus_UNKNOWN
	}

	return &replication.GetStorageProtectionGroupStatusResponse{
		Status: &replication.StorageProtectionGroupStatus{
			State: state,
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
		// If not found...
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
		VolumeContext: nil, // TODO: add values to volume context if needed
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
				// TODO: Change to local volume
				actionAttributes[remoteSystem+"-"+snap.AncestorVolumeID] = remoteSystem + "-" + snap.ID
			}
		}
	}

	return actionAttributes, nil
}
