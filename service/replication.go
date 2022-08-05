package service

import (
	"context"
	"log"

	//"fmt"
	//"strings"

	//common "github.com/dell/dell-csi-extensions/common"
	//csi "github.com/container-storage-interface/spec/lib/go/csi"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/dell-csi-extensions/replication"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	//sio "github.com/dell/goscaleio"
	//siotypes "github.com/dell/goscaleio/types/v1"
	//"google.golang.org/grpc/codes"
	//"google.golang.org/grpc/status"
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
	}
	Log.Printf("rep GetReplicationCapabilities %+v", rep)
	return rep, nil
}

func (s *service) CreateStorageProtectionGroup(ctx context.Context, req *replication.CreateStorageProtectionGroupRequest) (*replication.CreateStorageProtectionGroupResponse, error) {
	Log.Printf("rep CreateStorageProtectionGroup %+v", req)
	return nil, nil
}

// CreateRemoteVolume creates replica of volume in remote cluster
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
	Log.Printf("Local System Content: %+v", localSystem[0])

	remoteSystem, err := s.getSystem(parameters["replication.storage.dell.com/remoteSystem"])
	if err != nil {
		return nil, err
	}
	Log.Printf("Remote System Content: %+v", remoteSystem[0])

	mdms, err := s.getPeerMdms(systemID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "can't query volume: %s", err.Error())
	}

	Log.Printf("MDMs: %+v", mdms)

	// Create a volume on the remote system?
	// Generate a UUID for the name?
	// id := uuid.New()
	name := "replica-" + volHandleCtx
	Log.Printf("[CreateRemoteVolume] - Name: %s", name)

	volReq := createRemoteCreateVolumeRequest(name, parameters["replication.storage.dell.com/remoteStoragePool"], remoteSystem[0].ID)
	volReq.CapacityRange.RequiredBytes = int64(vol.SizeInKb)

	Log.Printf("[CreateRemoteVolume] - Remote volReq:%+v", volReq)

	// crvolresp, err := s.CreateVolume(ctx, &csi.CreateVolumeRequest{
	// 	Name: name,
	// 	CapacityRange: &csi.CapacityRange{
	// 		RequiredBytes: int64(vol.SizeInKb),
	// 		LimitBytes:    0,
	// 	},
	// 	VolumeCapabilities: []*csi.VolumeCapability{req.VolumeCapability},
	// 	Parameters:         req.VolumeContext,
	// 	Secrets:            req.Secrets,
	// })

	// Try to create?
	createVolumeResponse, err := s.CreateVolume(ctx, volReq)
	if err != nil {
		log.Printf("CreateVolume called failed: %s", err.Error())
	} else {
		log.Printf("Potentially created a remote volume: %+v", createVolumeResponse)
	}

	remoteParams := map[string]string{
		"storagePool":  parameters["replication.storage.dell.com/remoteStoragePool"],
		"remoteSystem": remoteSystem[0].ID,
	}
	remoteVolume := getRemoteCSIVolume(createVolumeResponse.GetVolume().VolumeId, vol.SizeInKb)
	remoteVolume.VolumeContext = remoteParams
	return &replication.CreateRemoteVolumeResponse{
		RemoteVolume: remoteVolume,
	}, nil
}

func (s *service) DeleteStorageProtectionGroup(ctx context.Context, req *replication.DeleteStorageProtectionGroupRequest) (*replication.DeleteStorageProtectionGroupResponse, error) {
	Log.Printf("rep DeleteStorageProtectionGroup %+v", req)
	return nil, nil
}

func (s *service) ExecuteAction(ctx context.Context, req *replication.ExecuteActionRequest) (*replication.ExecuteActionResponse, error) {
	Log.Printf("rep ExecuteAction %+v", req)
	return nil, nil
}

func (s *service) GetStorageProtectionGroupStatus(ctx context.Context, req *replication.GetStorageProtectionGroupStatusRequest) (*replication.GetStorageProtectionGroupStatusResponse, error) {
	Log.Printf("rep GetStorageProtectionGroupStatus %+v", req)
	return nil, nil
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
