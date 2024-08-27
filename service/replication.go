package service

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/dell-csi-extensions/replication"
	"github.com/dell/goscaleio"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	siotypes "github.com/dell/goscaleio/types/v1"
)

const (
	// KeyReplicationRemoteSystem represents key for replication remote system
	KeyReplicationRemoteSystem = "remoteSystem"
	// KeyReplicationRemoteStoragePool represents key for replication remote storage pool
	KeyReplicationRemoteStoragePool = "remoteStoragePool"
	// KeyReplicationProtectionDomain represents key for replication protectionDomain
	KeyReplicationProtectionDomain = "protectionDomain"
	// KeyReplicationConsistencyGroupName represents key for replication consistency group name
	KeyReplicationConsistencyGroupName = "consistencyGroupName"
	// KeyReplicationRPO represents key for replication RPO
	KeyReplicationRPO = "rpo"
	// KeyReplicationClusterID represents key for replication remote cluster ID
	KeyReplicationClusterID = "remoteClusterID"
	// KeyReplicationVGPrefix represents key for replication vg prefix
	KeyReplicationVGPrefix = "volumeGroupPrefix"

	sioReplicationPairsDoesNotExist = "Error in get relationship ReplicationPair"
	sioReplicationGroupNotFound     = "The Replication Consistency Group was not found"
)

var (
	getRemoteSnapDelay = (1 * time.Second)
	snapshotMaxRetries = 10
)

func (s *service) GetReplicationCapabilities(_ context.Context, _ *replication.GetReplicationCapabilityRequest) (*replication.GetReplicationCapabilityResponse, error) {
	rep := new(replication.GetReplicationCapabilityResponse)
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

	vol, err := s.getVolByID(volumeID, systemID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "can't query volume: %s", err.Error())
	}

	_, err = s.getSystem(systemID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not get (local) system %s: %s", systemID, err.Error())
	}

	remoteSystemID, ok := parameters[s.WithRP(KeyReplicationRemoteSystem)]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "replication enabled but no remote system specified in storage class")
	}

	// Probe the remote system
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

	remoteStoragePool, ok := parameters[s.WithRP(KeyReplicationRemoteStoragePool)]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "replication enabled but no remote storage pool specified in storage class")
	}

	protectionDomain, ok := parameters[s.WithRP(KeyReplicationProtectionDomain)]
	if !ok {
		log.Printf("Remote protection domain not provided; there could be conflicts if two storage pools share a name")
	}

	name := "replicated-" + vol.Name
	volReq := createRemoteCreateVolumeRequest(name, remoteStoragePool, remoteSystem.ID, protectionDomain, int64(vol.SizeInKb))

	createVolumeResponse, err := s.CreateVolume(ctx, volReq)
	if err != nil {
		log.Printf("CreateVolume call failed: %s", err.Error())
		return nil, err
	}

	log.Printf("Potentially created a remote volume: %+v", createVolumeResponse)

	remoteParams := map[string]string{
		"storagePool":    remoteStoragePool,
		"remoteSystem":   remoteSystem.ID,
		"remoteVolumeID": createVolumeResponse.Volume.VolumeId,
	}

	remoteVolume := getRemoteCSIVolume(createVolumeResponse.GetVolume().VolumeId, vol.SizeInKb)
	remoteVolume.VolumeContext = remoteParams
	return &replication.CreateRemoteVolumeResponse{
		RemoteVolume: remoteVolume,
	}, nil
}

// DeleteLocalVolume deletes the backend volume on the storage array.
func (s *service) DeleteLocalVolume(ctx context.Context, req *replication.DeleteLocalVolumeRequest) (*replication.DeleteLocalVolumeResponse, error) {
	Log.Printf("[DeleteLocalVolume] - req %+v", req)

	volHandleCtx := req.GetVolumeHandle()

	if volHandleCtx == "" {
		return nil, status.Error(codes.InvalidArgument, "volume handle is required")
	}

	volumeID := getVolumeIDFromCsiVolumeID(volHandleCtx)
	systemID := s.getSystemIDFromCsiVolumeID(volHandleCtx)

	Log.Printf("Volume ID: %s System ID: %s", volumeID, systemID)

	if volumeID == "" || systemID == "" {
		return nil, status.Error(codes.InvalidArgument, "failed to provide system ID or volume ID")
	}

	vol, err := s.getVolByID(volumeID, systemID)
	if err != nil {
		if strings.EqualFold(err.Error(), sioGatewayVolumeNotFound) {
			log.Printf("[DeleteLocalVolume] - volume already deleted.")
			return &replication.DeleteLocalVolumeResponse{}, nil
		}

		return nil, status.Errorf(codes.Internal, "can't query volume: %s", err.Error())
	}

	if vol.VolumeReplicationState != "UnmarkedForReplication" {
		log.Printf("[DeleteLocalVolume] - target volume is marked for replication when deleting")
		return nil, status.Error(codes.InvalidArgument, "replication target volume marked for replication. Delete source volume.")
	}

	_, err = s.DeleteVolume(ctx, &csi.DeleteVolumeRequest{VolumeId: volHandleCtx})
	if err != nil {
		log.Printf("[DeleteLocalVolume] - call failed: %s", err.Error())
		return nil, err
	}

	return &replication.DeleteLocalVolumeResponse{}, nil
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

	localProtectionDomain, err := s.getProtectionDomain(systemID, parameters[KeyProtectionDomain])
	if err != nil {
		return nil, status.Errorf(codes.Internal, "couldn't getProtectionDomain (local): %s", err.Error())
	}

	Log.Printf("[CreateStorageProtectionGroup] - Local Protection Domain: %+v", localProtectionDomain)

	remoteSystemID, ok := parameters[s.WithRP(KeyReplicationRemoteSystem)]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "replication enabled but no remote system specified in storage class")
	}

	remoteSystem, err := s.getSystem(remoteSystemID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "couldn't getSystem (remote): %s", err.Error())
	}

	remoteProtectionDomain, err := s.getProtectionDomain(remoteSystemID, parameters[s.WithRP(KeyReplicationProtectionDomain)])
	if err != nil {
		return nil, err
	}

	_, err = s.getPeerMdms(systemID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "can't query peer mdms: %s", err.Error())
	}

	rpo, ok := parameters[s.WithRP(KeyReplicationRPO)]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "replication enabled but no RPO specified in storage class")
	}

	var consistencyGroupName string
	if name, ok := parameters[s.WithRP(KeyReplicationConsistencyGroupName)]; ok {
		consistencyGroupName = name
	}

	if consistencyGroupName == "" {
		remoteClusterID, ok := parameters[s.WithRP(KeyReplicationClusterID)]
		if !ok {
			return nil, status.Errorf(codes.InvalidArgument, "replication enabled but no remote cluster ID specified in storage class")
		}

		clusterUID, ok := parameters["clusterUID"]
		if !ok {
			Log.Warnf("[CreateStorageProtectionGroup] - source cluster UID not provided, using remote system ID in RCG name.")
			clusterUID = remoteSystemID
		}

		rcgPrefix, ok := parameters[s.WithRP(KeyReplicationVGPrefix)]
		if !ok || rcgPrefix == "" {
			Log.Warnf("[CreateStorageProtectionGroup] - RCG prefix not provided, using 'RCG' as prefix.")
			rcgPrefix = "rcg"
		}

		consistencyGroupName, err = s.createUniqueConsistencyGroupName(systemID, rpo,
			localProtectionDomain, remoteProtectionDomain, remoteClusterID, clusterUID, rcgPrefix)
		if err != nil {
			return nil, err
		}
		Log.Printf("[CreateStorageProtectionGroup] - consistencyGroupName: %+s", consistencyGroupName)
	}

	localRcg, err := s.CreateReplicationConsistencyGroup(systemID, consistencyGroupName,
		rpo, localProtectionDomain, remoteProtectionDomain, "", remoteSystem.ID)
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

	group, err := s.getReplicationConsistencyGroupByID(systemID, localRcg.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "No replication consistency groups found: %s", err.Error())
	}

	localParams := map[string]string{
		s.opts.replicationContextPrefix + "systemName":     localSystem.ID,
		s.opts.replicationContextPrefix + "remoteSystemID": remoteSystem.ID,
	}

	remoteParams := map[string]string{
		s.opts.replicationContextPrefix + "systemName":     remoteSystem.ID,
		s.opts.replicationContextPrefix + "remoteSystemID": localSystem.ID,
	}

	Log.Printf("[CreateStorageProtectionGroup] - localRcg: %+s, group.ID: %s", localRcg.ID, group.ID)

	return &replication.CreateStorageProtectionGroupResponse{
		LocalProtectionGroupId:         group.ID,
		LocalProtectionGroupAttributes: localParams,

		RemoteProtectionGroupId:         group.RemoteID,
		RemoteProtectionGroupAttributes: remoteParams,
	}, nil
}

func (s *service) GetStorageProtectionGroupStatus(_ context.Context, req *replication.GetStorageProtectionGroupStatusRequest) (*replication.GetStorageProtectionGroupStatusResponse, error) {
	Log.Printf("[GetStorageProtectionGroupStatus] - req %+v", req)

	localParams := req.GetProtectionGroupAttributes()

	protectionGroupSystem, ok := localParams[s.opts.replicationContextPrefix+"systemName"]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "Error: can't find `systemName` in replication group")
	}

	group, err := s.getReplicationConsistencyGroupByID(protectionGroupSystem, req.ProtectionGroupId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "No replication consistency groups found: %s", err.Error())
	}

	pairs, err := s.getReplicationPairs(protectionGroupSystem, req.ProtectionGroupId)
	if err != nil {
		return nil, err
	}

	if len(pairs) == 0 {
		return nil, status.Errorf(codes.Internal, "no replication pairs exist")
	}

	Log.Printf("[GetStorageProtectionGroupStatus] - group %+v", group)

	var state replication.StorageProtectionGroupStatus_State
	switch group.CurrConsistMode {
	case goscaleio.PartiallyConsistent, goscaleio.ConsistentPending:
		state = replication.StorageProtectionGroupStatus_SYNC_IN_PROGRESS
	case goscaleio.Consistent:
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

func (s *service) DeleteStorageProtectionGroup(_ context.Context, req *replication.DeleteStorageProtectionGroupRequest) (*replication.DeleteStorageProtectionGroupResponse, error) {
	Log.Printf("[DeleteStorageProtectionGroup] %+v", req)
	localParams := req.GetProtectionGroupAttributes()

	protectionGroupSystem := localParams[s.opts.replicationContextPrefix+"systemName"]

	pairs, err := s.getReplicationPairs(protectionGroupSystem, req.ProtectionGroupId)
	if err != nil {
		// Handle the case where it doesn't exist. Already deleted.
		if strings.EqualFold(err.Error(), sioReplicationGroupNotFound) {
			return &replication.DeleteStorageProtectionGroupResponse{}, nil
		}
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

	remoteSystem := remoteParams[s.opts.replicationContextPrefix+"systemName"]
	localSystem := localParams[s.opts.replicationContextPrefix+"systemName"]

	client, err := s.verifySystem(localSystem)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "%s", err.Error())
	}

	group, err := s.getReplicationConsistencyGroupByID(localSystem, protectionGroupID)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "No replication consistency groups found: %s", err.Error())
	}

	statusResp, err := s.GetStorageProtectionGroupStatus(ctx, &replication.GetStorageProtectionGroupStatusRequest{
		ProtectionGroupId:         protectionGroupID,
		ProtectionGroupAttributes: localParams,
	})
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "unable to get storage protection group status: %s", err.Error())
	}

	switch action {
	case replication.ActionTypes_CREATE_SNAPSHOT.String():
		if statusResp.Status.State != replication.StorageProtectionGroupStatus_SYNCHRONIZED {
			return nil, status.Errorf(codes.FailedPrecondition, "rg is not synchronized, can't process snapshot")
		}

		resp, err := s.CreateReplicationConsistencyGroupSnapshot(client, group)
		if err != nil {
			return nil, status.Error(codes.Unknown, err.Error())
		}

		attempts := 0

		for len(actionAttributes) == 0 && attempts < snapshotMaxRetries {
			actionAttributes, err = s.getConsistencyGroupSnapshotContent(localSystem, remoteSystem, protectionGroupID, resp.SnapshotGroupID)
			if err != nil {
				return nil, status.Error(codes.Unknown, err.Error())
			}
			time.Sleep(getRemoteSnapDelay)
			attempts++
		}
	case replication.ActionTypes_FAILOVER_REMOTE.String():
		if err := s.ExecuteSwitchoverOnReplicationGroup(client, group); err != nil {
			return nil, status.Error(codes.Unknown, err.Error())
		}

	case replication.ActionTypes_UNPLANNED_FAILOVER_LOCAL.String():
		if err := s.ExecuteFailoverOnReplicationGroup(client, group); err != nil {
			return nil, status.Error(codes.Unknown, err.Error())
		}

	case replication.ActionTypes_REPROTECT_LOCAL.String():
		if err := s.ExecuteReverseOnReplicationGroup(client, group); err != nil {
			return nil, status.Error(codes.Unknown, err.Error())
		}

	case replication.ActionTypes_RESUME.String():
		failover := statusResp.Status.State == replication.StorageProtectionGroupStatus_FAILEDOVER
		paused := statusResp.Status.State == replication.StorageProtectionGroupStatus_SUSPENDED
		if paused || failover {
			if err := s.ExecuteResumeOnReplicationGroup(client, group, failover); err != nil {
				return nil, status.Error(codes.Unknown, err.Error())
			}
		}
	case replication.ActionTypes_SUSPEND.String():
		paused := statusResp.Status.State == replication.StorageProtectionGroupStatus_SUSPENDED
		if !paused {
			if err := s.ExecutePauseOnReplicationGroup(client, group); err != nil {
				return nil, status.Error(codes.Unknown, err.Error())
			}
		}
	case replication.ActionTypes_SYNC.String():
		if _, err := s.ExecuteSyncOnReplicationGroup(client, group); err != nil {
			return nil, status.Error(codes.Unknown, err.Error())
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

// WithRP appends Replication Prefix to provided string
func (s *service) WithRP(key string) string {
	return s.opts.replicationPrefix + "/" + key
}

func createRemoteCreateVolumeRequest(name, storagePool, systemID, protectionDomain string, sizeInKb int64) *csi.CreateVolumeRequest {
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	params[KeyStoragePool] = storagePool
	params[KeySystemID] = systemID
	params[KeyProtectionDomain] = protectionDomain
	req.Parameters = params
	req.Name = name
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = sizeInKb * 1024
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

func getRemoteCSIVolume(volumeID string, size int) *replication.Volume {
	volume := &replication.Volume{
		CapacityBytes: int64(size),
		VolumeId:      volumeID,

		VolumeContext: nil,
	}
	return volume
}

func (s *service) getReplicationConsistencyGroupByID(systemID string, groupID string) (*siotypes.ReplicationConsistencyGroup, error) {
	adminClient := s.adminClients[systemID]
	if adminClient == nil {
		return nil, fmt.Errorf("can't find adminClient by id %s", systemID)
	}

	group, err := adminClient.GetReplicationConsistencyGroupByID(groupID)
	if err != nil {
		return nil, err
	}

	return group, nil
}

func (s *service) createUniqueConsistencyGroupName(systemID, rpo, localPd, remotePd, remoteClusterID, clusterUID, rcgPrefix string) (string, error) {
	consistencyGroupName := rcgPrefix + "-"
	clusterUID = strings.Replace(clusterUID, "-", "", -1)
	remoteClusterID = strings.Replace(remoteClusterID, "-", "", -1)

	if remoteClusterID == "self" {
		consistencyGroupName += clusterUID[:6] + "-v"
	} else {
		remoteClusterID = strings.ReplaceAll(remoteClusterID, "cluster", "")

		if len(remoteClusterID) > 7 {
			consistencyGroupName += clusterUID[:6] + "-" + remoteClusterID[:6] + "-v"
		} else {
			consistencyGroupName += clusterUID[:6] + "-" + remoteClusterID + "-v"
		}
	}

	adminClient := s.adminClients[systemID]
	if adminClient == nil {
		return "", fmt.Errorf("can't find adminClient by id %s", systemID)
	}

	rcgs, err := adminClient.GetReplicationConsistencyGroups()
	if err != nil {
		return "", err
	}

	version := 1
	var found bool
	for _, rcg := range rcgs {
		if strings.Contains(rcg.Name, consistencyGroupName) {
			if rcg.ProtectionDomainID == localPd && rcg.RemoteProtectionDomainID == remotePd && strconv.Itoa(rcg.RpoInSeconds) == rpo {
				consistencyGroupName = rcg.Name
				found = true
				break
			}
			if rcg.Name[len(rcg.Name)-1:] == strconv.Itoa(version) {
				version++
			}
		}
	}

	if !found {
		consistencyGroupName += strconv.Itoa(version)
	}

	return consistencyGroupName, nil
}

func (s *service) getReplicationPairs(systemID string, groupID string) ([]*siotypes.ReplicationPair, error) {
	adminClient := s.adminClients[systemID]
	if adminClient == nil {
		return nil, fmt.Errorf("can't find adminClient by id %s", systemID)
	}

	group, err := adminClient.GetReplicationConsistencyGroupByID(groupID)
	if err != nil {
		return nil, err
	}

	rcg := goscaleio.NewReplicationConsistencyGroup(adminClient)
	rcg.ReplicationConsistencyGroup = group

	pairs, err := rcg.GetReplicationPairs()
	if err != nil {
		if !strings.EqualFold(err.Error(), sioReplicationPairsDoesNotExist) {
			Log.Printf("Error getting replication pairs: %s", err.Error())
			return nil, err
		}
	}

	return pairs, nil
}

func isFailover(group *siotypes.ReplicationConsistencyGroup) bool {
	return group.FailoverType != "None"
}

func isPaused(group *siotypes.ReplicationConsistencyGroup) bool {
	return group.PauseMode != "None"
}

func (s *service) getConsistencyGroupSnapshotContent(localSystem, remoteSystem, protectionGroup, snapshotGroup string) (map[string]string, error) {
	actionAttributes := make(map[string]string)

	pairs, err := s.getReplicationPairs(localSystem, protectionGroup)
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
