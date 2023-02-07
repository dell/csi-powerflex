package service

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/dell-csi-extensions/replication"
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
		log.Printf("CreateVolume called failed: %s", err.Error())
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
			return nil, status.Errorf(codes.InvalidArgument, "replication enabled but no source cluster UID retrieved")
		}

		consistencyGroupName, err = s.createUniqueConsistencyGroupName(systemID, remoteSystemID, rpo,
			localProtectionDomain, remoteProtectionDomain, remoteClusterID, clusterUID)
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

func (s *service) createUniqueConsistencyGroupName(systemID, remoteSystemID, rpo, localPd, remotePd, remoteClusterID, clusterUID string) (string, error) {
	var consistencyGroupName string
	clusterUID = strings.Replace(clusterUID, "-", "", -1)
	remoteClusterID = strings.Replace(remoteClusterID, "-", "", -1)

	if remoteClusterID == "self" {
		consistencyGroupName += "rcg-"
		consistencyGroupName += clusterUID[:6] + "-v"
	} else {
		consistencyGroupName += "rcg-"
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
			} else {
				if rcg.Name[len(rcg.Name)-1:] == strconv.Itoa(version) {
					version++
				}
			}
		}
	}

	if !found {
		consistencyGroupName += strconv.Itoa(version)
	}

	return consistencyGroupName, nil
}
