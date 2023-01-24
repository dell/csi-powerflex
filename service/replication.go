package service

import (
	"log"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/dell-csi-extensions/replication"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// KeyReplicationRemoteSystem represents key for replication remote system
	KeyReplicationRemoteSystem = "remoteSystem"
	// KeyReplicationRemoteStoragePool represents key for replication remote storage pool
	KeyReplicationRemoteStoragePool = "remoteStoragePool"
	// KeyReplicationProtectionDomain represents key for replication protectionDomain
	KeyReplicationProtectionDomain = "protectionDomain"
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
		return nil, status.Errorf(codes.InvalidArgument, "replication enabled but no remote protection domain specified in storage class")
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
