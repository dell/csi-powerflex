// Copyright © 2019-2022 Dell Inc. or its subsidiaries. All Rights Reserved.
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
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/goscaleio"
	siotypes "github.com/dell/goscaleio/types/v1"
	ptypes "github.com/golang/protobuf/ptypes"
	"github.com/sirupsen/logrus"
)

const (
	// KeyStoragePool is the key used to get the storagepool name from the
	// volume create parameters map
	KeyStoragePool = "storagepool"

	// KeyProtectionDomain is the key used to get the StoragePool's Protection Domain name from the
	// volume create parameters map. This parameter is optional.
	KeyProtectionDomain = "protectiondomain"

	// KeyBandwidthLimitInKbps is the key used to get the bandwidth limit from the volume
	// create parameters map
	KeyBandwidthLimitInKbps = "bandwidthLimitInKbps"

	// KeyIopsLimit is the key used to get the IOPS limit from the volume
	// create parameters map
	KeyIopsLimit = "iopsLimit"

	// KeySystemID is the key used to get the array ID from the volume
	// create parameters map
	KeySystemID = "systemID"

	// KeyMkfsFormatOption is the key used to get the file system option from the
	// volume create parameters map
	KeyMkfsFormatOption = "mkfsFormatOption"

	// DefaultVolumeSizeKiB is default volume sgolang/protobuf/blob/master/ptypesize
	// to create on a scaleIO cluster when no size is given, expressed in KiB
	DefaultVolumeSizeKiB = 16 * kiBytesInGiB

	// VolSizeMultipleGiB is the volume size that VxFlexOS creates volumes as
	// a multiple of, meaning that all volume sizes are a multiple of this
	// number
	VolSizeMultipleGiB = 8

	// bytesInKiB is the number of bytes in a kibibyte
	bytesInKiB = 1024

	// kiBytesInGiB is the number of kibibytes in a gibibyte
	kiBytesInGiB = 1024 * 1024

	// bytesInGiB is the number of bytes in a gibibyte
	bytesInGiB = kiBytesInGiB * bytesInKiB

	//VolumeIDList is the list of volume IDs
	VolumeIDList = "VolumeIDList"

	removeModeOnlyMe                    = "ONLY_ME"
	sioGatewayNotFound                  = "Not found"
	sioGatewayVolumeNotFound            = "Could not find the volume"
	sioVolumeRemovalOperationInProgress = "A volume removal operation is currently in progress"
	sioGatewayVolumeNameInUse           = "Volume name already in use. Please use a different name."
	errNoMultiMap                       = "volume not enabled for mapping to multiple hosts"
	errUnknownAccessType                = "unknown access type is not Block or Mount"
	errUnknownAccessMode                = "access mode cannot be UNKNOWN"
	errNoMultiNodeWriter                = "multi-node with writer(s) only supported for block access type"
	//TRUE means "true" (comment put in for lint check)
	TRUE = "TRUE"
	//FALSE means "false" (comment put in for lint check)
	FALSE = "FALSE"
)

// Extra metadata field names for propagating to goscaleio and beyond.
const (
	// These are available when enabling --extra-create-metadata for the external-provisioner.
	CSIPersistentVolumeName           = "csi.storage.k8s.io/pv/name"
	CSIPersistentVolumeClaimName      = "csi.storage.k8s.io/pvc/name"
	CSIPersistentVolumeClaimNamespace = "csi.storage.k8s.io/pvc/namespace"
	// These map to the above fields in the form of HTTP header names.
	HeaderPersistentVolumeName           = "x-csi-pv-name"
	HeaderPersistentVolumeClaimName      = "x-csi-pv-claimname"
	HeaderPersistentVolumeClaimNamespace = "x-csi-pv-namespace"
	// These help identify the system used as part of a request.
	HeaderSystemIdentifier    = "x-csi-system-id"
	HeaderCSIPluginIdentifier = "x-csi-plugin-id"
)

var (
	interestingParameters = [...]string{0: "FsType", 1: KeyMkfsFormatOption, 2: KeyBandwidthLimitInKbps, 3: KeyIopsLimit}
)

func (s *service) CreateVolume(
	ctx context.Context,
	req *csi.CreateVolumeRequest) (
	*csi.CreateVolumeResponse, error) {

	params := req.GetParameters()

	systemID, err := s.getSystemIDFromParameters(params)
	if err != nil {
		return nil, err
	}

	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	s.logStatistics()

	cr := req.GetCapacityRange()
	sizeInKiB, err := validateVolSize(cr)
	if err != nil {
		return nil, err
	}

	// validate AccessibleTopology
	accessibility := req.GetAccessibilityRequirements()
	if accessibility == nil {
		Log.Printf("Received CreateVolume request without accessibility keys")
	}

	var volumeTopology []*csi.Topology
	systemSegments := map[string]string{} // topology segments matching requested system for a volume
	if accessibility != nil && len(accessibility.GetPreferred()) > 0 {

		requestedSystem := ""
		sID := ""
		system := s.systems[systemID]
		if system != nil {
			sID = system.System.ID
		}

		//We need to get name of system, in case sc was set up to use name
		sName := system.System.Name

		segments := accessibility.GetPreferred()[0].GetSegments()
		for key := range segments {
			if strings.HasPrefix(key, Name) {
				tokens := strings.Split(key, "/")
				constraint := ""
				if len(tokens) > 1 {
					constraint = tokens[1]
				}
				Log.Printf("Found topology constraint: VxFlex OS system: %s", constraint)
				if constraint == sID || constraint == sName {
					if constraint == sID {
						requestedSystem = sID
					} else {
						requestedSystem = sName
					}
					//segment matches system ID/Name where volume will be created
					topologyKey := tokens[0] + "/" + sID
					systemSegments[topologyKey] = segments[key]
					Log.Printf("Added accessible topology segment for volume: %s, segment: %s = %s", req.GetName(),
						topologyKey, systemSegments[topologyKey])
				}
			}
		}

		// check that the required system id/name matched one of the system id/names from node topology
		if len(segments) > 0 && requestedSystem == "" {
			return nil, status.Errorf(codes.InvalidArgument,
				"Requested System %s is not accessible based on Preferred[0] accessibility data, sent by provisioner", systemID)
		}
		if len(systemSegments) > 0 {
			// add topology element containing segments matching required system to volume topology
			volumeTopology = append(volumeTopology, &csi.Topology{
				Segments: systemSegments,
			})
			Log.Printf("Accessible topology for volume: %s, segments: %#v", req.GetName(), systemSegments)
		}
	}

	params = mergeStringMaps(params, req.GetSecrets())

	// We require the storagePool name for creation
	sp, ok := params[KeyStoragePool]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument,
			"%s is a required parameter", KeyStoragePool)
	}

	pdID := ""
	pd, ok := params[KeyProtectionDomain]
	if !ok {
		Log.Printf("Protection Domain name not provided; there could be conflicts if two storage pools share a name")
	} else {
		pdID, err = s.getProtectionDomainIDFromName(systemID, pd)
		if err != nil {
			return nil, err
		}
	}

	volType := s.getVolProvisionType(params) // Thick or Thin

	name := req.GetName()
	if name == "" {
		return nil, status.Error(codes.InvalidArgument,
			"Name cannot be empty")
	}

	if len(name) > 31 {
		name = name[0:31]
		Log.Printf("Requested name %s longer than 31 character max, truncated to %s\n", req.Name, name)
		req.Name = name
	}

	contentSource := req.GetVolumeContentSource()
	if contentSource != nil {
		volumeSource := contentSource.GetVolume()
		if volumeSource != nil {
			Log.Printf("volume %s specified as volume content source", volumeSource.VolumeId)
			return s.Clone(req, volumeSource, name, sizeInKiB, sp)
		}
		snapshotSource := contentSource.GetSnapshot()
		if snapshotSource != nil {
			Log.Printf("snapshot %s specified as volume content source", snapshotSource.SnapshotId)
			return s.createVolumeFromSnapshot(req, snapshotSource, name, sizeInKiB, sp)
		}
	}

	// TODO handle Access mode in volume capability

	fields := map[string]interface{}{
		"name":                               name,
		"sizeInKiB":                          sizeInKiB,
		"storagePool":                        sp,
		"volType":                            volType,
		HeaderPersistentVolumeName:           params[CSIPersistentVolumeName],
		HeaderPersistentVolumeClaimName:      params[CSIPersistentVolumeClaimName],
		HeaderPersistentVolumeClaimNamespace: params[CSIPersistentVolumeClaimNamespace],
	}

	Log.WithFields(fields).Info("creating volume")

	volumeParam := &siotypes.VolumeParam{
		Name:           name,
		VolumeSizeInKb: fmt.Sprintf("%d", sizeInKiB),
		VolumeType:     volType,
	}

	// If the VolumeParam has a MetaData method, set the values accordingly.
	if t, ok := interface{}(volumeParam).(interface {
		MetaData() http.Header
	}); ok {
		t.MetaData().Set(HeaderPersistentVolumeName, params[CSIPersistentVolumeName])
		t.MetaData().Set(HeaderPersistentVolumeClaimName, params[CSIPersistentVolumeClaimName])
		t.MetaData().Set(HeaderPersistentVolumeClaimNamespace, params[CSIPersistentVolumeClaimNamespace])
		t.MetaData().Set(HeaderCSIPluginIdentifier, Name)
		t.MetaData().Set(HeaderSystemIdentifier, systemID)
	} else {
		Log.Println("warning: goscaleio.VolumeParam: no MetaData method exists, consider updating goscaleio library.")
	}

	createResp, err := s.adminClients[systemID].CreateVolume(volumeParam, sp, pdID)
	if err != nil {
		// handle case where volume already exists
		if !strings.EqualFold(err.Error(), sioGatewayVolumeNameInUse) {
			Log.Printf("error creating volume: %s pool %s error: %s", name, sp, err.Error())
			return nil, status.Errorf(codes.Internal,
				"error when creating volume %s storagepool %s: %s", name, sp, err.Error())
		}
	}

	var id string
	if createResp == nil {
		// volume already exists, look it up by name
		id, err = s.adminClients[systemID].FindVolumeID(name)
		if err != nil {
			return nil, status.Errorf(codes.Internal, err.Error())
		}
	} else {
		id = createResp.ID
	}

	vol, err := s.getVolByID(id, systemID)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable,
			"error retrieving volume details: %s", err.Error())
	}
	vi := s.getCSIVolume(vol, systemID)
	vi.AccessibleTopology = volumeTopology

	// since the volume could have already exists, double check that the
	// volume has the expected parameters
	spID, err := s.getStoragePoolID(sp, systemID, pdID)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable,
			"volume exists, but could not verify parameters: %s",
			err.Error())
	}
	if vol.StoragePoolID != spID {
		return nil, status.Errorf(codes.AlreadyExists,
			"volume exists in %s, but in different storage pool than requested %s", vol.StoragePoolID, spID)
	}

	if (vi.CapacityBytes / bytesInKiB) != sizeInKiB {
		return nil, status.Errorf(codes.AlreadyExists,
			"volume exists, but at different size than requested")
	}
	copyInterestingParameters(req.GetParameters(), vi.VolumeContext)

	Log.Printf("volume %s (%s) created %s\n", vi.VolumeContext["Name"], vi.VolumeId, vi.VolumeContext["CreationTime"])

	csiResp := &csi.CreateVolumeResponse{
		Volume: vi,
	}

	s.clearCache()

	volumeID := getVolumeIDFromCsiVolumeID(vi.VolumeId)
	vol, err = s.getVolByID(volumeID, systemID)

	counter := 0

	for err != nil && counter < 100 {
		time.Sleep(3 * time.Millisecond)
		vol, err = s.getVolByID(volumeID, systemID)
		counter = counter + 1
	}

	return csiResp, err
}

// Copies the interesting parameters to the output map.
func copyInterestingParameters(parameters, out map[string]string) {
	for _, str := range interestingParameters {
		if parameters[str] != "" {
			out[str] = parameters[str]
		}
	}
}

// getSystemIDFromParameters gets the systemID from the given params, if not found get the default
// array
func (s *service) getSystemIDFromParameters(params map[string]string) (string, error) {
	if params == nil {
		return "", status.Errorf(codes.FailedPrecondition, "params map is nil")
	}

	systemID := ""
	for key, value := range params {
		if strings.EqualFold(key, KeySystemID) {
			systemID = value
			break
		}
	}

	// systemID not found in storage class params, use the default array
	if systemID == "" {
		if s.opts.defaultSystemID != "" {
			systemID = s.opts.defaultSystemID
		} else if len(s.opts.arrays) == 1 {
			for id := range s.opts.arrays { // use the only provided array
				systemID = id
			}
		} else {
			return "", status.Errorf(codes.FailedPrecondition, "No system ID is found in parameters or as default")
		}
	}
	Log.Printf("getSystemIDFromParameters system %s", systemID)

	// if name set for array.SystemID use id instead
	// names can change , id will remain unique
	if id, ok := s.connectedSystemNameToID[systemID]; ok {
		systemID = id
	}
	Log.Printf("Use systemID as %s", systemID)
	return systemID, nil
}

// Create a volume (which is actually a snapshot) from an existing snapshot.
// The snapshotSource gives the SnapshotId which is the volume to be replicated.
func (s *service) createVolumeFromSnapshot(req *csi.CreateVolumeRequest,
	snapshotSource *csi.VolumeContentSource_SnapshotSource,
	name string, sizeInKbytes int64, storagePool string) (*csi.CreateVolumeResponse, error) {

	// get systemID from snapshot source CSI id
	systemID := s.getSystemIDFromCsiVolumeID(snapshotSource.SnapshotId)
	if systemID == "" {
		// use default system
		systemID = s.opts.defaultSystemID
	}
	if systemID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"systemID is not found in snapshot source id and there is no default system")
	}

	// Look up the snapshot
	snapID := getVolumeIDFromCsiVolumeID(snapshotSource.SnapshotId)
	srcVol, err := s.getVolByID(snapID, systemID)
	if err != nil {
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "Snapshot not found: %s", snapshotSource.SnapshotId)
		}
	}
	// Validate the size is the same.
	if int64(srcVol.SizeInKb) != sizeInKbytes {
		return nil, status.Errorf(codes.InvalidArgument,
			"Snapshot %s has incompatible size %d kbytes with requested %d kbytes",
			snapshotSource.SnapshotId, srcVol.SizeInKb, sizeInKbytes)
	}

	adminClient := s.adminClients[systemID]
	system := s.systems[systemID]

	// Validate the storagePool is the same.
	snapStoragePool := s.getStoragePoolNameFromID(systemID, srcVol.StoragePoolID)
	if snapStoragePool != storagePool {
		return nil, status.Errorf(codes.InvalidArgument,
			"Snapshot storage pool %s is different than the requested storage pool %s", snapStoragePool, storagePool)
	}

	// Check for idempotent request
	existingVols, err := adminClient.GetVolume("", "", "", name, false)
	noVolErrString1 := "Error: problem finding volume: Volume not found"
	noVolErrString2 := "Error: problem finding volume: Could not find the volume"
	if (err != nil) && !(strings.Contains(err.Error(), noVolErrString1) || strings.Contains(err.Error(), noVolErrString2)) {
		Log.Printf("[createVolumeFromSnapshot] Idempotency check: GetVolume returned error: %s", err.Error())
		return nil, status.Errorf(codes.Internal, "Failed to create vol from snap -- GetVolume returned unexpected error: %s", err.Error())
	}

	for _, vol := range existingVols {
		if vol.Name == name && vol.StoragePoolID == srcVol.StoragePoolID {
			Log.Printf("Requested volume %s already exists", name)
			csiVolume := s.getCSIVolume(vol, systemID)
			csiVolume.ContentSource = req.GetVolumeContentSource()
			copyInterestingParameters(req.GetParameters(), csiVolume.VolumeContext)
			Log.Printf("Requested volume (from snap) already exists %s (%s) storage pool %s",
				csiVolume.VolumeContext["Name"], csiVolume.VolumeId, csiVolume.VolumeContext["StoragePoolName"])
			return &csi.CreateVolumeResponse{Volume: csiVolume}, nil
		}
	}

	// Snapshot the source snapshot
	snapshotDefs := make([]*siotypes.SnapshotDef, 0)
	snapDef := &siotypes.SnapshotDef{VolumeID: snapID, SnapshotName: name}
	snapshotDefs = append(snapshotDefs, snapDef)
	snapParam := &siotypes.SnapshotVolumesParam{SnapshotDefs: snapshotDefs}

	// Create snapshot
	snapResponse, err := system.CreateSnapshotConsistencyGroup(snapParam)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to create snapshot: %s", err.Error())
	}
	if len(snapResponse.VolumeIDList) != 1 {
		return nil, status.Errorf(codes.Internal, "Expected volume ID to be returned but it was not")
	}

	// Retrieve created destination volume
	dstID := snapResponse.VolumeIDList[0]
	dstVol, err := s.getVolByID(dstID, systemID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not retrieve created volume: %s", dstID)
	}
	// Create a volume response and return it
	s.clearCache()
	csiVolume := s.getCSIVolume(dstVol, systemID)
	csiVolume.ContentSource = req.GetVolumeContentSource()
	copyInterestingParameters(req.GetParameters(), csiVolume.VolumeContext)

	Log.Printf("Volume (from snap) %s (%s) storage pool %s",
		csiVolume.VolumeContext["Name"], csiVolume.VolumeId, csiVolume.VolumeContext["StoragePoolName"])
	return &csi.CreateVolumeResponse{Volume: csiVolume}, nil
}

func (s *service) CreateReplicationConsistencyGroup(systemID string, name string,
	rpo string, locatProtectionDomain string, remoteProtectionDomain string,
	peerMdmId string, remoteSystemId string) (*siotypes.ReplicationConsistencyGroupResp, error) {
	adminClient := s.adminClients[systemID]
	if adminClient == nil {
		return nil, fmt.Errorf("can't find adminClient by id %s", systemID)
	}

	// One or the other, not both
	if peerMdmId != "" && remoteSystemId != "" {
		return nil, fmt.Errorf("PeerMdmID and RemoteSystemID cannot be present. One or the other.")
	}

	rcgPayload := &siotypes.ReplicationConsistencyGroupCreatePayload{
		Name:                     name,
		RpoInSeconds:             rpo,
		ProtectionDomainId:       locatProtectionDomain,
		RemoteProtectionDomainId: remoteProtectionDomain,
		PeerMdmId:                peerMdmId,
		DestinationSystemId:      remoteSystemId,
	}

	rcgResp, err := adminClient.CreateReplicationConsistencyGroup(rcgPayload)
	if err != nil {
		return nil, err
	}

	return rcgResp, nil
}

func (s *service) clearCache() {
	s.volCacheRWL.Lock()
	defer s.volCacheRWL.Unlock()
	s.volCache = make([]*siotypes.Volume, 0)
	s.snapCacheRWL.Lock()
	defer s.snapCacheRWL.Unlock()
	s.snapCache = make([]*siotypes.Volume, 0)
}

// validateVolSize uses the CapacityRange range params to determine what size
// volume to create, and returns an error if volume size would be greater than
// the given limit. Returned size is in KiB
func validateVolSize(cr *csi.CapacityRange) (int64, error) {

	minSize := cr.GetRequiredBytes()
	maxSize := cr.GetLimitBytes()

	if minSize < 0 || maxSize < 0 {
		return 0, status.Errorf(
			codes.OutOfRange,
			"bad capacity: volume size bytes %d and limit size bytes: %d must not be negative", minSize, maxSize)
	}

	if minSize == 0 {
		minSize = DefaultVolumeSizeKiB
	} else {
		minSize = minSize / bytesInKiB
	}

	var (
		sizeGiB int64
		sizeKiB int64
		sizeB   int64
	)
	// VxFlexOS creates volumes in multiples of 8GiB, rounding up.
	// Determine what actual size of volume will be, and check that
	// we do not exceed maxSize
	sizeGiB = minSize / kiBytesInGiB
	// if the requested size was less than 1GB, set the request to 1GB
	// so it can be rounded to a 8GiB boundary correctly
	if sizeGiB == 0 {
		sizeGiB = 1
	}
	mod := sizeGiB % VolSizeMultipleGiB
	if mod > 0 {
		sizeGiB = sizeGiB - mod + VolSizeMultipleGiB
	}
	sizeB = sizeGiB * bytesInGiB
	if maxSize != 0 {
		if sizeB > maxSize {
			return 0, status.Errorf(
				codes.OutOfRange,
				"bad capacity: volume size %d > limit_bytes: %d", sizeB, maxSize)
		}
	}

	sizeKiB = sizeGiB * kiBytesInGiB
	return sizeKiB, nil
}

func (s *service) DeleteVolume(
	ctx context.Context,
	req *csi.DeleteVolumeRequest) (
	*csi.DeleteVolumeResponse, error) {

	csiVolID := req.GetVolumeId()
	if csiVolID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"volume ID is required")
	}
	//ensure no ambiguity if legacy vol
	err := s.checkVolumesMap(csiVolID)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"checkVolumesMap for id: %s failed : %s", csiVolID, err.Error())

	}

	// get systemID from req
	systemID := s.getSystemIDFromCsiVolumeID(csiVolID)
	if systemID == "" {
		// use default system
		systemID = s.opts.defaultSystemID
	}

	if systemID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"systemID is not found in the request and there is no default system")
	}

	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	s.logStatistics()

	volID := getVolumeIDFromCsiVolumeID(csiVolID)
	vol, err := s.getVolByID(volID, systemID)

	if err != nil {
		if strings.EqualFold(err.Error(), sioGatewayVolumeNotFound) {
			Log.WithFields(logrus.Fields{"id": csiVolID}).Debug("volume is already deleted", csiVolID)
			return &csi.DeleteVolumeResponse{}, nil
		}
		if strings.Contains(err.Error(), sioVolumeRemovalOperationInProgress) {
			Log.WithFields(logrus.Fields{"id": csiVolID}).Debug("volume is currently being deleted", csiVolID)
			return &csi.DeleteVolumeResponse{}, nil
		}

		if strings.Contains(err.Error(), "must be a hexadecimal number") {

			Log.WithFields(logrus.Fields{"id": csiVolID}).Debug("volume id must be a hexadecimal number", csiVolID)
			return &csi.DeleteVolumeResponse{}, nil

		}

		return nil, status.Errorf(codes.Internal,
			"failure checking volume status before deletion: %s",
			err.Error())
	}

	if len(vol.MappedSdcInfo) > 0 {
		// Volume is in use
		return nil, status.Errorf(codes.FailedPrecondition,
			"volume in use by %s", vol.MappedSdcInfo[0].SdcID)
	}

	Log.WithFields(logrus.Fields{"name": vol.Name, "id": csiVolID}).Info("Deleting volume")
	tgtVol := goscaleio.NewVolume(s.adminClients[systemID])
	tgtVol.Volume = vol
	err = tgtVol.RemoveVolume(removeModeOnlyMe)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"error removing volume: %s", err.Error())
	}

	vol, err = s.getVolByID(volID, systemID)
	counter := 0

	for err != nil && strings.Contains(err.Error(), sioVolumeRemovalOperationInProgress) && counter < 100 {
		time.Sleep(3 * time.Millisecond)
		vol, err = s.getVolByID(volID, systemID)
		counter = counter + 1
	}

	s.clearCache()

	if err != nil && !strings.Contains(err.Error(), "Could not find the volume") {
		return nil, err
	}

	return &csi.DeleteVolumeResponse{}, nil
}

func (s *service) ControllerPublishVolume(
	ctx context.Context,
	req *csi.ControllerPublishVolumeRequest) (
	*csi.ControllerPublishVolumeResponse, error) {

	volumeContext := req.GetVolumeContext()
	if volumeContext != nil {
		Log.Printf("VolumeContext:")
		for key, value := range volumeContext {
			Log.Printf("    [%s]=%s", key, value)
		}
	}

	csiVolID := req.GetVolumeId()
	if csiVolID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"volume ID is required")
	}

	// get systemID from req
	systemID := s.getSystemIDFromCsiVolumeID(csiVolID)
	if systemID == "" {
		// use default system
		systemID = s.opts.defaultSystemID
	}
	if systemID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"systemID is not found in the request and there is no default system")
	}

	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}
	adminClient := s.adminClients[systemID]

	s.logStatistics()

	//ensure no ambiguity if legacy vol
	err := s.checkVolumesMap(csiVolID)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"checkVolumesMap for id: %s failed : %s", csiVolID, err.Error())

	}

	volID := getVolumeIDFromCsiVolumeID(csiVolID)
	vol, err := s.getVolByID(volID, systemID)

	if err != nil {
		if strings.EqualFold(err.Error(), sioGatewayVolumeNotFound) || strings.Contains(err.Error(), "must be a hexadecimal number") {
			return nil, status.Error(codes.NotFound,
				"volume not found")
		}
		return nil, status.Errorf(codes.Internal,
			"failure checking volume status before controller publish: %s",
			err.Error())
	}

	nodeID := req.GetNodeId()
	if nodeID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"node ID is required")
	}

	sdcID, err := s.getSDCID(nodeID, systemID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, err.Error())
	}

	vc := req.GetVolumeCapability()
	if vc == nil {
		return nil, status.Error(codes.InvalidArgument,
			"volume capability is required")
	}

	am := vc.GetAccessMode()
	if am == nil {
		return nil, status.Error(codes.InvalidArgument,
			"access mode is required")
	}

	if am.Mode == csi.VolumeCapability_AccessMode_UNKNOWN {
		return nil, status.Error(codes.InvalidArgument,
			errUnknownAccessMode)
	}
	// Check if volume is published to any node already
	allowMultipleMappings := "FALSE"
	vcs := []*csi.VolumeCapability{req.GetVolumeCapability()}
	isBlock := accTypeIsBlock(vcs)

	if len(vol.MappedSdcInfo) > 0 {
		for _, sdc := range vol.MappedSdcInfo {
			if sdc.SdcID == sdcID {
				// TODO check if published volume is compatible with this request
				// volume already mapped
				Log.Debug("volume already mapped")

				// check for QoS limits of mapped volume
				bandwidthLimit := volumeContext[KeyBandwidthLimitInKbps]
				iopsLimit := volumeContext[KeyIopsLimit]
				// validate requested QoS parameters
				if err := validateQoSParameters(bandwidthLimit, iopsLimit, vol.Name); err != nil {
					return nil, err
				}

				// check if volume QoS is same as requested QoS settings
				if len(bandwidthLimit) > 0 && strconv.Itoa(sdc.LimitBwInMbps*1024) != bandwidthLimit {
					return nil, status.Errorf(codes.InvalidArgument,
						"volume %s already published with bandwidth limit: %d, but does not match the requested bandwidth limit: %s", vol.Name, sdc.LimitBwInMbps*1024, bandwidthLimit)
				} else if len(iopsLimit) > 0 && strconv.Itoa(sdc.LimitIops) != iopsLimit {
					return nil, status.Errorf(codes.InvalidArgument,
						"volume %s already published with IOPS limit: %d, but does not match the requested IOPS limits: %s", vol.Name, sdc.LimitIops, iopsLimit)
				}

				return &csi.ControllerPublishVolumeResponse{}, nil
			}
		}

		// If volume has SINGLE_NODE cap, go no farther
		switch am.Mode {
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER,
			csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER,
			csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY:
			return nil, status.Errorf(codes.FailedPrecondition,
				"volume already published to SDC id: %s", vol.MappedSdcInfo[0].SdcID)
		}

		// All remaining cases are MULTI_NODE:
		// This original code precludes block multi-writers,
		// and is based on a faulty test that the Volume MappingToAllSdcsEnabled
		// attribute must be set to allow multiple writers, which is not true.
		// The proper way to control multiple mappings is with the allowMultipleMappings
		// attribute passed in the MapVolumeSdcParameter. Unfortunately you cannot
		// read this parameter back.

		allowMultipleMappings, err = shouldAllowMultipleMappings(isBlock, am)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, err.Error())
		}

		if err := validateAccessType(am, isBlock); err != nil {
			return nil, err
		}
	} else {
		allowMultipleMappings, err = shouldAllowMultipleMappings(isBlock, am)
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, err.Error())
		}
	}

	mapVolumeSdcParam := &siotypes.MapVolumeSdcParam{
		SdcID:                 sdcID,
		AllowMultipleMappings: allowMultipleMappings,
		AllSdcs:               "",
	}

	targetVolume := goscaleio.NewVolume(adminClient)
	targetVolume.Volume = &siotypes.Volume{ID: vol.ID}

	err = targetVolume.MapVolumeSdc(mapVolumeSdcParam)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"error mapping volume to node: %s", err.Error())
	}

	bandwidthLimit := volumeContext[KeyBandwidthLimitInKbps]
	iopsLimit := volumeContext[KeyIopsLimit]

	// validate requested QoS parameters
	if err := validateQoSParameters(bandwidthLimit, iopsLimit, vol.Name); err != nil {
		return nil, err
	}
	// check for atleast one of the QoS params should exist in storage class
	if len(bandwidthLimit) > 0 || len(iopsLimit) > 0 {
		if err = s.setQoSParameters(ctx, systemID, sdcID, bandwidthLimit, iopsLimit, vol.Name, csiVolID, nodeID); err != nil {
			return nil, err
		}
	}

	return &csi.ControllerPublishVolumeResponse{}, nil
}

// validate the requested QoS parameters.
func validateQoSParameters(bandwidthLimit string, iopsLimit string, volumeName string) error {
	if len(bandwidthLimit) > 0 {
		_, err := strconv.ParseInt(bandwidthLimit, 10, 64)
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "requested Bandwidth limit: %s is not numeric for volume %s, error: %s", bandwidthLimit, volumeName, err.Error())
		}
	}

	if len(iopsLimit) > 0 {
		_, err := strconv.ParseInt(iopsLimit, 10, 64)
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "requested IOPS limit: %s is not numeric for volume %s, error: %s", iopsLimit, volumeName, err.Error())
		}
	}

	return nil
}

// setQoSParameters to set QoS parameters
func (s *service) setQoSParameters(
	ctx context.Context,
	systemID string, sdcID string, bandwidthLimit string,
	iopsLimit string, volumeName string, csiVolID string,
	nodeID string) error {

	Log.Infof("Setting QoS limits for volume %s, mapped to SDC %s", volumeName, sdcID)
	adminClient := s.adminClients[systemID]
	tgtVol := goscaleio.NewVolume(adminClient)
	volID := getVolumeIDFromCsiVolumeID(csiVolID)
	vol, err := s.getVolByID(volID, systemID)
	if err != nil {
		return status.Errorf(codes.NotFound, "volume %s was not found", volID)
	}
	tgtVol.Volume = vol
	settings := siotypes.SetMappedSdcLimitsParam{
		SdcID:                sdcID,
		BandwidthLimitInKbps: bandwidthLimit,
		IopsLimit:            iopsLimit,
	}
	err = tgtVol.SetMappedSdcLimits(&settings)
	if err != nil {
		// unpublish the volume
		Log.Errorf("unpublishing volume since error in setting QoS parameters for volume: %s, error: %s", volumeName, err.Error())

		_, newErr := s.ControllerUnpublishVolume(ctx, &csi.ControllerUnpublishVolumeRequest{
			VolumeId: csiVolID,
			NodeId:   nodeID,
		})
		if newErr != nil {
			return status.Errorf(codes.Internal,
				"controller unpublish failed, error: %s", newErr.Error())
		}
		return status.Errorf(codes.Internal,
			"error setting QoS parameters, error: %s", err.Error())
	}
	return nil
}

// Determine when the multiple mappings flag should be set when calling MapVolumeSdc
func shouldAllowMultipleMappings(isBlock bool, accessMode *csi.VolumeCapability_AccessMode) (string, error) {
	switch accessMode.Mode {
	case csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY:
		return TRUE, nil
	case csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER:
		if isBlock {
			return TRUE, nil
		}
		return FALSE, errors.New("Mount multinode multi-writer not allowed")
	case csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER:
		return FALSE, errors.New("Multinode single writer not supported")
	default:
		return FALSE, nil
	}
}

func validateAccessType(
	am *csi.VolumeCapability_AccessMode,
	isBlock bool) error {

	if isBlock {
		switch am.Mode {
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER,
			csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER,
			csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER:
			return nil
		default:
			return status.Errorf(codes.InvalidArgument,
				"Access mode: %v not compatible with access type", am.Mode)
		}
	} else {
		switch am.Mode {
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER,
			csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER,
			csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
			csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY:
			return nil
		default:
			return status.Errorf(codes.InvalidArgument,
				"Access mode: %v not compatible with access type", am.Mode)
		}
	}
}

func (s *service) ControllerUnpublishVolume(
	ctx context.Context,
	req *csi.ControllerUnpublishVolumeRequest) (
	*csi.ControllerUnpublishVolumeResponse, error) {

	// get systemID from req
	systemID := s.getSystemIDFromCsiVolumeID(req.GetVolumeId())
	if systemID == "" {
		// use default system
		systemID = s.opts.defaultSystemID
	}

	if systemID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"systemID is not found in the request and there is no default system")
	}

	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	s.logStatistics()

	csiVolID := req.GetVolumeId()
	if csiVolID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"volume ID is required")
	}
	//ensure no ambiguity if legacy vol
	err := s.checkVolumesMap(csiVolID)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"checkVolumesMap for id: %s failed : %s", csiVolID, err.Error())

	}

	volID := getVolumeIDFromCsiVolumeID(csiVolID)
	vol, err := s.getVolByID(volID, systemID)

	if err != nil {
		if strings.EqualFold(err.Error(), sioGatewayVolumeNotFound) {
			return nil, status.Error(codes.NotFound,
				"Volume not found")
		}
		return nil, status.Errorf(codes.Internal,
			"failure checking volume status before controller unpublish: %s",
			err.Error())
	}

	nodeID := req.GetNodeId()
	if nodeID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"Node ID is required")
	}

	sdcID, err := s.getSDCID(nodeID, systemID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, err.Error())
	}

	// check if volume is attached to node at all
	mappedToNode := false
	for _, mapping := range vol.MappedSdcInfo {
		if mapping.SdcID == sdcID {
			mappedToNode = true
			break
		}
	}

	if !mappedToNode {
		Log.Debug("volume already unpublished")
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}
	adminClient := s.adminClients[systemID]
	targetVolume := goscaleio.NewVolume(adminClient)
	targetVolume.Volume = vol

	unmapVolumeSdcParam := &siotypes.UnmapVolumeSdcParam{
		SdcID:   sdcID,
		AllSdcs: "",
	}

	if err = targetVolume.UnmapVolumeSdc(unmapVolumeSdcParam); err != nil {
		return nil, status.Errorf(codes.Internal,
			"Error unmapping volume from node: %s", err.Error())
	}

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (s *service) ValidateVolumeCapabilities(
	ctx context.Context,
	req *csi.ValidateVolumeCapabilitiesRequest) (
	*csi.ValidateVolumeCapabilitiesResponse, error) {

	csiVolID := req.GetVolumeId()
	if csiVolID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"volume ID is required")
	}
	//ensure no ambiguity if legacy vol
	err := s.checkVolumesMap(csiVolID)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"checkVolumesMap for id: %s failed : %s", csiVolID, err.Error())

	}

	// get systemID from req
	systemID := s.getSystemIDFromCsiVolumeID(csiVolID)
	if systemID == "" {
		// use default system
		systemID = s.opts.defaultSystemID
	}

	if systemID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"systemID is not found in the request and there is no default system")
	}

	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	volID := getVolumeIDFromCsiVolumeID(csiVolID)
	vol, err := s.getVolByID(volID, systemID)

	if err != nil {
		if strings.EqualFold(err.Error(), sioGatewayVolumeNotFound) || strings.Contains(err.Error(), "must be a hexadecimal number") {
			return nil, status.Error(codes.NotFound,
				"volume not found")
		}
		return nil, status.Errorf(codes.Internal,
			"failure checking volume status for capabilities: %s",
			err.Error())
	}

	vcs := req.GetVolumeCapabilities()
	supported, reason := valVolumeCaps(vcs, vol)

	resp := &csi.ValidateVolumeCapabilitiesResponse{}
	if supported {
		// The optional fields volume_context and parameters are not passed.
		confirmed := &csi.ValidateVolumeCapabilitiesResponse_Confirmed{}
		confirmed.VolumeCapabilities = vcs
		resp.Confirmed = confirmed
	} else {
		resp.Message = reason
	}

	return resp, nil
}

func accTypeIsBlock(vcs []*csi.VolumeCapability) bool {
	for _, vc := range vcs {
		if at := vc.GetBlock(); at != nil {
			return true
		}
	}
	return false
}

func checkValidAccessTypes(vcs []*csi.VolumeCapability) bool {
	for _, vc := range vcs {
		if vc == nil {
			continue
		}
		atblock := vc.GetBlock()
		if atblock != nil {
			continue
		}
		atmount := vc.GetMount()
		if atmount != nil {
			continue
		}
		// Unknown access type, we should reject it.
		return false
	}
	return true
}

func valVolumeCaps(
	vcs []*csi.VolumeCapability,
	vol *siotypes.Volume) (bool, string) {

	var (
		supported = true
		isBlock   = accTypeIsBlock(vcs)
		reason    string
	)
	// Check that all access types are valid
	if !checkValidAccessTypes(vcs) {
		return false, errUnknownAccessType
	}

	for _, vc := range vcs {
		am := vc.GetAccessMode()
		if am == nil {
			continue
		}
		switch am.Mode {
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER,
			csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER:
			break
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY:
			break
		case csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY:
			break
		case csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER:
			fallthrough
		case csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER:
			if !isBlock {
				supported = false
				reason = errNoMultiNodeWriter
			}
			break

		default:
			//This is to guard against new access modes not understood
			supported = false
			reason = errUnknownAccessMode
		}
	}

	return supported, reason
}

func (s *service) ListVolumes(
	ctx context.Context,
	req *csi.ListVolumesRequest) (
	*csi.ListVolumesResponse, error) {

	// TODO: Implement this method to get volumes from all systems. Currently we get volumes only from default system
	systemID := s.opts.defaultSystemID
	if systemID != "" {
		if err := s.requireProbe(ctx, systemID); err != nil {
			Log.Printf("Could not probe system: %s", systemID)
			return nil, err
		}
	} else {
		// Default system is not set: not supported
		Log.Printf("Default system is not set")
		return nil, status.Error(codes.InvalidArgument, "There is no default system in controller to list volumes.")
	}

	var (
		startToken int
		err        error
		maxEntries = int(req.MaxEntries)
	)

	if v := req.StartingToken; v != "" {
		i, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			return nil, status.Errorf(
				codes.Aborted,
				"Unable to parse StartingToken: %v into uint32",
				req.StartingToken)
		}
		startToken = int(i)
	}

	// Call the common listVolumes code
	source, nextToken, err := s.listVolumes(systemID, startToken, maxEntries, true, s.opts.EnableListVolumesSnapshots, "", "")
	if err != nil {
		return nil, err
	}

	// Process the source volumes and make CSI Volumes
	entries := make([]*csi.ListVolumesResponse_Entry, len(source))
	i := 0
	for _, vol := range source {
		entries[i] = &csi.ListVolumesResponse_Entry{
			Volume: s.getCSIVolume(vol, systemID),
		}
		i = i + 1
	}

	return &csi.ListVolumesResponse{
		Entries:   entries,
		NextToken: nextToken,
	}, nil
}

func (s *service) ListSnapshots(
	ctx context.Context,
	req *csi.ListSnapshotsRequest) (
	*csi.ListSnapshotsResponse, error) {

	var (
		startToken int
		err        error
		maxEntries = int(req.MaxEntries)
		volumeID   string
		ancestorID string
	)
	// TODO: Currently, when there is no SourceVolumeID or SnapshotId in request, we get volumes only from default system
	if v := req.StartingToken; v != "" {
		i, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			return nil, status.Errorf(
				codes.Aborted,
				"Unable to parse StartingToken: %v into uint32",
				req.StartingToken)
		}
		startToken = int(i)
	}

	// csiSourceID is either source volume ID or snapshot ID
	csiSourceID := ""
	csiVolID := req.SourceVolumeId
	if csiVolID != "" {
		ancestorID = getVolumeIDFromCsiVolumeID(csiVolID)
		csiSourceID = csiVolID
	}

	csiSnapshotID := req.SnapshotId
	if csiSnapshotID != "" {
		volumeID = getVolumeIDFromCsiVolumeID(csiSnapshotID)
		csiSourceID = csiSnapshotID
		// Specifying the SnapshotId is more restrictive than the SourceVolumeId
		// so the latter is ignored.
		ancestorID = ""
	}

	// Use systemID from csiSourceID if available, otherwise default systemID is used
	systemID := s.opts.defaultSystemID
	if csiSourceID != "" {
		systemID = s.getSystemIDFromCsiVolumeID(csiSourceID)
		if systemID == "" {
			// use default system
			systemID = s.opts.defaultSystemID
		}

		if systemID == "" {
			return nil, status.Errorf(codes.InvalidArgument,
				"systemID is not found in SourceVolumeID (%s) or SnapshotID (%s) "+
					"and there is no default system in controller to list snapshots",
				req.SourceVolumeId, req.SnapshotId)
		}
	}

	if err := s.requireProbe(ctx, systemID); err != nil {
		Log.Printf("Could not probe system: %s", systemID)
		return nil, err
	}

	// Call the common listVolumes code to list snapshots only.
	// If sourceVolumeID or snapshotID are provided, we list those use cases and do not use cache.
	source, nextToken, err := s.listVolumes(systemID, startToken, maxEntries, false, true, volumeID, ancestorID)

	if err != nil && strings.Contains(err.Error(), "must be a hexadecimal number") {
		return &csi.ListSnapshotsResponse{}, nil
	}

	if err != nil {
		return nil, err
	}

	// Process the source volumes and make CSI Volumes
	entries := make([]*csi.ListSnapshotsResponse_Entry, len(source))
	i := 0
	for _, vol := range source {
		entries[i] = &csi.ListSnapshotsResponse_Entry{
			Snapshot: s.getCSISnapshot(vol, systemID),
		}
		i = i + 1
	}

	return &csi.ListSnapshotsResponse{
		Entries:   entries,
		NextToken: nextToken,
	}, nil

}

// Subroutine to list volumes for both CSI operations ListVolumes and ListSnapshots.
// systemID:  systemID to get volumes/snapshots from
// startToken: integer offset in volumes to list (if both vols and snaps returned, indexes into overall list)
// maxEntries: maximum number of entries to be returned
// doVols: return volume entries
// doSnaps: return snapshot entries
// volumeID: If present, restricts output to a particular volume
// ancstorID: If present, restricts output to volumes having the given ancestor ID (i.e. snap source)
// Returns:
// array of Volume pointers to be returned
// next starting token (string)
// error
func (s *service) listVolumes(systemID string, startToken int, maxEntries int, doVols, doSnaps bool, volumeID, ancestorID string) (
	[]*siotypes.Volume, string, error) {
	var (
		volumes  []*siotypes.Volume
		sioVols  []*siotypes.Volume
		sioSnaps []*siotypes.Volume
		err      error
	)

	adminClient := s.adminClients[systemID]

	// Handle exactly one volume or snapshot
	if volumeID != "" || ancestorID != "" {
		sioVols, err = adminClient.GetVolume("", volumeID, ancestorID, "", false)

		if err != nil {
			return nil, "", status.Errorf(codes.Internal,
				"Unable to list volumes for volume ID %s ancestor ID %s: %s", volumeID, ancestorID, err.Error())
		}
		// This disables the global list requests and the cache.
		doVols = false
		doSnaps = false
	}

	// If neither ancestorID, nor volumeID provided, process volumes with volume cache
	if doVols {
		// Get the volumes from the cache if we can.
		if startToken != 0 && len(s.volCache) > 0 {
			Log.Printf("volume cache hit: %d volumes", len(s.volCache))
			func() {
				s.volCacheRWL.Lock()
				defer s.volCacheRWL.Unlock()
				sioVols = make([]*siotypes.Volume, len(s.volCache))
				// Check if cache has volumes for the required systemID
				if s.volCacheSystemID == systemID {
					copy(sioVols, s.volCache)
				}
			}()
		}

		if len(sioVols) == 0 {
			sioVols, err = adminClient.GetVolume("", "", "", "", false)
			if err != nil {
				return nil, "", status.Errorf(
					codes.Internal,
					"Unable to list volumes: %s", err.Error())
			}
			// We want to cache this volume list so that we don't
			// have to get all the volumes again on the next call
			if len(sioVols) > 0 {
				func() {
					s.volCacheRWL.Lock()
					defer s.volCacheRWL.Unlock()
					s.volCache = make([]*siotypes.Volume, len(sioVols))
					copy(s.volCache, sioVols)
					s.volCacheSystemID = systemID
				}()
			}
		}
	}

	// Process snapshots.
	if doSnaps {
		if startToken != 0 && len(s.snapCache) > 0 {
			Log.Printf("snap cache hit: %d snapshots", len(s.snapCache))
			func() {
				s.snapCacheRWL.Lock()
				defer s.snapCacheRWL.Unlock()
				sioSnaps = make([]*siotypes.Volume, len(s.snapCache))
				// Check if cache has snapshots for the required systemID
				if s.snapCacheSystemID == systemID {
					copy(sioSnaps, s.snapCache)
				}
			}()
		}
		if len(sioSnaps) == 0 {
			sioSnaps, err = adminClient.GetVolume("", "", "", "", true)
			if err != nil {
				return nil, "", status.Errorf(
					codes.Internal,
					"Unable to list snapshots: %s", err.Error())
			}
			if len(sioSnaps) > 0 {
				func() {
					s.snapCacheRWL.Lock()
					defer s.snapCacheRWL.Unlock()
					s.snapCache = make([]*siotypes.Volume, len(sioSnaps))
					copy(s.snapCache, sioSnaps)
					s.snapCacheSystemID = systemID
				}()
			}
		}
	}

	// Make aggregate volumes slice containing both
	volumes = make([]*siotypes.Volume, len(sioVols)+len(sioSnaps))
	if len(sioVols) > 0 {
		copy(volumes[0:], sioVols)
	}
	if len(sioSnaps) > 0 {
		copy(volumes[len(sioVols):], sioSnaps)
	}

	if startToken > len(volumes) {
		return nil, "", status.Errorf(
			codes.Aborted,
			"startingToken=%d > len(volumes)=%d",
			startToken, len(volumes))
	}

	// Discern the number of remaining entries.
	rem := len(volumes) - startToken

	// If maxEntries is 0 or greater than the number of remaining entries then
	// set nentries to the number of remaining entries.
	if maxEntries == 0 || maxEntries > rem {
		maxEntries = rem
	}

	// Compute the next starting point; if at end reset
	nextToken := startToken + maxEntries
	nextTokenStr := ""
	if nextToken < (startToken + rem) {
		nextTokenStr = fmt.Sprintf("%d", nextToken)
	}

	return volumes[startToken : startToken+maxEntries], nextTokenStr, nil
}

// Gets capacity of a given storage system. When storage pool name is provided, gets capcity of this storage pool only.
func (s *service) getSystemCapacity(ctx context.Context, systemID, protectionDomain string, spName ...string) (int64, error) {

	Log.Infof("Get capacity for system: %s, pool %s", systemID, spName)

	if err := s.requireProbe(ctx, systemID); err != nil {
		return 0, err
	}

	adminClient := s.adminClients[systemID]
	system := s.systems[systemID]

	var statsFunc func() (*siotypes.Statistics, error)

	// Default to get Capacity of system
	statsFunc = system.GetStatistics

	if len(spName) > 0 {
		// if storage pool is given, get capacity of storage pool
		pdID, err := s.getProtectionDomainIDFromName(systemID, protectionDomain)
		if err != nil {
			return 0, err
		}
		sp, err := adminClient.FindStoragePool("", spName[0], "", pdID)
		if err != nil {
			return 0, status.Errorf(codes.Internal,
				"unable to look up storage pool: %s on system: %s, err: %s",
				spName, systemID, err.Error())
		}
		spc := goscaleio.NewStoragePoolEx(adminClient, sp)
		statsFunc = spc.GetStatistics
	}

	stats, err := statsFunc()
	if err != nil {
		return 0, status.Errorf(codes.Internal,
			"unable to get system stats for system: %s, err: %s", systemID, err.Error())
	}
	return int64(stats.CapacityAvailableForVolumeAllocationInKb * bytesInKiB), nil
}

// Gets capacity for all systems known to controller.
// When storage pool name is provided, gets capacity of this storage pool name from all systems
func (s *service) getCapacityForAllSystems(ctx context.Context, protectionDomain string, spName ...string) (int64, error) {

	var capacity int64

	for _, array := range s.opts.arrays {
		var systemCapacity int64
		var err error

		if len(spName) > 0 {
			systemCapacity, err = s.getSystemCapacity(ctx, array.SystemID, protectionDomain, spName[0])
		} else {
			systemCapacity, err = s.getSystemCapacity(ctx, array.SystemID, "")
		}

		if err != nil {
			return 0, status.Errorf(codes.Internal,
				"Unable to get capacity for system: %s, err: %s", array.SystemID, err.Error())
		}

		capacity += systemCapacity
	}

	return capacity, nil
}

func (s *service) GetCapacity(
	ctx context.Context,
	req *csi.GetCapacityRequest) (
	*csi.GetCapacityResponse, error) {

	var (
		capacity int64
		err      error
	)

	params := req.GetParameters()
	if params == nil || len(params) == 0 {
		// Get capacity of all systems
		capacity, err = s.getCapacityForAllSystems(ctx, "")
	} else {
		spname := params[KeyStoragePool]
		pd, ok := params[KeyProtectionDomain]
		if !ok {
			Log.Printf("Protection Domain name not provided; there could be conflicts if two storage pools share a name")
		}
		systemID := ""
		for key, value := range params {
			if strings.EqualFold(key, KeySystemID) {
				systemID = value
				break
			}
		}

		if systemID == "" {
			// Get capacity of storage pool spname in all systems, return total capacity
			capacity, err = s.getCapacityForAllSystems(ctx, "", spname)
		} else {
			capacity, err = s.getSystemCapacity(ctx, systemID, pd, spname)
		}
	}

	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"Unable to get capacity: %s", err.Error())
	}

	return &csi.GetCapacityResponse{
		AvailableCapacity: capacity,
	}, nil
}

func (s *service) ControllerGetCapabilities(
	ctx context.Context,
	req *csi.ControllerGetCapabilitiesRequest) (
	*csi.ControllerGetCapabilitiesResponse, error) {

	capabilities := []*csi.ControllerServiceCapability{
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
				},
			},
		},
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
				},
			},
		},
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_GET_CAPACITY,
				},
			},
		},
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
				},
			},
		},
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS,
				},
			},
		},
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
				},
			},
		},
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_CLONE_VOLUME,
				},
			},
		},
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_SINGLE_NODE_MULTI_WRITER,
				},
			},
		},
	}

	healthMonitorCapabilities := []*csi.ControllerServiceCapability{
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_VOLUME_CONDITION,
				},
			},
		},
		{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_GET_VOLUME,
				},
			},
		},
	}

	if s.opts.IsHealthMonitorEnabled {
		capabilities = append(capabilities, healthMonitorCapabilities...)
	}

	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: capabilities,
	}, nil
}

// systemProbeAll will iterate through all arrays in service.opts.arrays and probe them. If failed, it logs
// the failed system name
func (s *service) systemProbeAll(ctx context.Context) error {
	// probe all arrays
	Log.Infof("Probing all arrays. Number of arrays: %d", len(s.opts.arrays))
	allArrayFail := true
	errMap := make(map[string]error)

	for _, array := range s.opts.arrays {
		err := s.systemProbe(ctx, array)
		systemID := array.SystemID
		if err == nil {
			Log.Infof("array %s probed successfully", systemID)
			allArrayFail = false
		} else {
			errMap[systemID] = err
			Log.Errorf("array %s probe failed: %v", array.SystemID, err)
		}
	}

	if allArrayFail {
		return status.Error(codes.FailedPrecondition,
			fmt.Sprintf("All arrays are not working. Could not proceed further: %v", errMap))
	}

	return nil
}

// systemProbe will probe the given array
func (s *service) systemProbe(ctx context.Context, array *ArrayConnectionData) error {

	// Check that we have the details needed to login to the Gateway
	if array.Endpoint == "" {
		return status.Error(codes.FailedPrecondition,
			"missing VxFlexOS Gateway endpoint")
	}
	if array.Username == "" {
		return status.Error(codes.FailedPrecondition,
			"missing VxFlexOS MDM user")
	}
	if array.Password == "" {
		return status.Error(codes.FailedPrecondition,
			"missing VxFlexOS MDM password")
	}
	if array.SystemID == "" {
		return status.Error(codes.FailedPrecondition,
			"missing VxFlexOS system name")
	}
	var altSystemNames []string
	if array.AllSystemNames != "" {
		altSystemNames = strings.Split(array.AllSystemNames, ",")
	}

	systemID := array.SystemID

	// Create ScaleIO API client if needed
	if s.adminClients[systemID] == nil {
		skipCertificateValidation := array.SkipCertificateValidation || array.Insecure
		c, err := goscaleio.NewClientWithArgs(array.Endpoint, "", skipCertificateValidation, !s.opts.DisableCerts)
		if err != nil {
			return status.Errorf(codes.FailedPrecondition,
				"unable to create ScaleIO client: %s", err.Error())
		}
		s.adminClients[systemID] = c
		for _, name := range altSystemNames {
			s.adminClients[name] = c
		}
	}

	if s.adminClients[systemID].GetToken() == "" {
		_, err := s.adminClients[systemID].Authenticate(&goscaleio.ConfigConnect{
			Endpoint: array.Endpoint,
			Username: array.Username,
			Password: array.Password,
		})
		if err != nil {
			return status.Errorf(codes.FailedPrecondition,
				"unable to login to VxFlexOS Gateway: %s", err.Error())

		}
	}

	// initialize system if needed
	if s.systems[systemID] == nil {
		system, err := s.adminClients[systemID].FindSystem(
			array.SystemID, array.SystemID, "")
		if err != nil {
			return status.Errorf(codes.FailedPrecondition,
				"unable to find matching VxFlexOS system name: %s",
				err.Error())
		}
		s.systems[systemID] = system
		if system.System != nil && system.System.Name != "" {
			Log.Printf("Found Name for system=%s with ID=%s", system.System.Name, system.System.ID)
			s.connectedSystemNameToID[system.System.Name] = system.System.ID
			s.systems[system.System.ID] = system
			s.adminClients[system.System.ID] = s.adminClients[systemID]
		}
		// associate alternate system name to systemID
		for _, name := range altSystemNames {
			s.systems[name] = system
			s.adminClients[name] = s.adminClients[systemID]
			s.connectedSystemNameToID[name] = system.System.ID
		}
	}

	sysID := systemID
	if id, ok := s.connectedSystemNameToID[systemID]; ok {
		Log.Printf("System with name %s found id: %s", systemID, id)
		sysID = id
		s.opts.arrays[sysID] = array
	}
	if array.IsDefault == true {
		Log.Infof("default array is set to array ID: %s", sysID)
		s.opts.defaultSystemID = sysID
		Log.Printf("%s is the default array, skipping VolumePrefixToSystems map update. \n", sysID)
	} else {
		err := s.UpdateVolumePrefixToSystemsMap(sysID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *service) requireProbe(ctx context.Context, systemID string) error {
	if s.adminClients[systemID] == nil {
		Log.Debugf("probing system %s automatically", systemID)
		array, ok := s.opts.arrays[systemID]
		if ok {
			if err := s.systemProbe(ctx, array); err != nil {
				return status.Errorf(codes.FailedPrecondition,
					"failed to probe system: %s, error: %s", systemID, err.Error())
			}
		} else {
			return status.Errorf(codes.InvalidArgument,
				"system %s is not configured in the driver", systemID)
		}
	}

	return nil
}

// CreateSnapshot creates a snapshot.
// If Parameters["VolumeIDList"] has a comma separated list of additional volumes, they will be
// snapshotted in a consistency group with the primary volume in CreateSnapshotRequest.SourceVolumeId.
func (s *service) CreateSnapshot(
	ctx context.Context,
	req *csi.CreateSnapshotRequest) (
	*csi.CreateSnapshotResponse, error) {

	// Validate snapshot volume
	csiVolID := req.GetSourceVolumeId()
	if csiVolID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "CSI volume ID to be snapped is required")
	}
	//ensure no ambiguity if legacy vol
	err := s.checkVolumesMap(csiVolID)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"checkVolumesMap for id: %s failed : %s", csiVolID, err.Error())

	}

	volID := getVolumeIDFromCsiVolumeID(csiVolID)
	systemID := s.getSystemIDFromCsiVolumeID(csiVolID)

	if systemID == "" {
		// use default system
		systemID = s.opts.defaultSystemID
	}

	if systemID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"systemID is not found in the request and there is no default system")
	}

	// Requires probe
	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	// Validate requested name is not to long, if supplied. If so, truncate to 31 characters.
	if req.Name != "" && len(req.Name) > 31 {
		name := req.Name
		name = strings.Replace(name, "snapshot-", "sn-", 1)
		length := int(math.Min(float64(len(name)), 31))
		name = name[0:length]
		Log.Printf("Requested name %s longer than 31 character max, truncated to %s\n", req.Name, name)
		req.Name = name
	}

	if req.Name == "" {
		return nil, status.Errorf(codes.InvalidArgument, "snapshot name cannot be Nil")
	}

	// Check for idempotent request, i.e. the snapshot has been already created, by looking up the name.
	existingVols, err := s.adminClients[systemID].GetVolume("", "", "", req.Name, false)
	noVolErrString1 := "Error: problem finding volume: Volume not found"
	noVolErrString2 := "Error: problem finding volume: Could not find the volume"
	if (err != nil) && !(strings.Contains(err.Error(), noVolErrString1) || strings.Contains(err.Error(), noVolErrString2)) {
		Log.Printf("[CreateSnapshot] Idempotency check: GetVolume returned error: %s", err.Error())
		return nil, status.Errorf(codes.Internal, "Failed to create snapshot -- GetVolume returned unexpected error: %s", err.Error())
	}

	for _, vol := range existingVols {
		ancestor := vol.AncestorVolumeID
		Log.Printf("idempotent Name %s Name %s Ancestor %s id %s VTree %s pool %s\n",
			vol.Name, req.Name, ancestor, volID, vol.VTreeID, vol.StoragePoolID)
		if vol.Name == req.Name && vol.AncestorVolumeID == volID {
			// populate response structure
			Log.Printf("Idempotent request, snapshot id %s for source vol %s in system %s already exists\n", vol.ID, vol.AncestorVolumeID, systemID)
			snapshot := s.getCSISnapshot(vol, systemID)
			resp := &csi.CreateSnapshotResponse{Snapshot: snapshot}
			return resp, nil
		}
	}

	// Validate volume
	vol, err := s.getVolByID(volID, systemID)
	if err != nil {
		if strings.EqualFold(err.Error(), sioGatewayVolumeNotFound) {
			return nil, status.Errorf(codes.NotFound, "volume %s was not found", volID)
		}
		return nil, status.Errorf(codes.Internal,
			"failure checking volume status: %s", err.Error())
	}
	vtreeID := vol.VTreeID
	Log.Printf("vtree ID: %s\n", vtreeID)

	// Build list of volumes to be snapshotted.
	snapshotDefs := make([]*siotypes.SnapshotDef, 0)
	snapName := generateSnapName(vol.Name)
	if req.Name != "" {
		snapName = req.Name
	}
	snapDef := siotypes.SnapshotDef{VolumeID: volID, SnapshotName: snapName}
	snapshotDefs = append(snapshotDefs, &snapDef)

	// Determine if we want to add additional volumes to a consistency group
	// volIDList should be in PowerFlex format, or CSI format
	volIDList := req.Parameters[VolumeIDList]
	if volIDList != "" {
		volIDs := strings.Split(volIDList, ",")
		for _, v := range volIDs {
			//neeed to trim space in case there are spaces inside VolumeIDList
			consistencyGroupSystem := strings.TrimSpace(s.getSystemIDFromCsiVolumeID(v))
			if consistencyGroupSystem != "" && consistencyGroupSystem != systemID {
				//system needs to be the same throughout snapshot consistency group, this is an error
				err = status.Errorf(codes.Internal, "Consistency group needs to be on the same system but vol %s is not on system: %s ", v, systemID)
				Log.Errorf("Consistency group needs to be on the same system but vol %s is not on system: %s ", v, systemID)
				return nil, err
			}
			v = getVolumeIDFromCsiVolumeID(v)
			vID := strings.Replace(v, " ", "", -1)
			if vID == volID {
				// Don't list the original volume again
				continue
			}
			volx, err := s.getVolByID(vID, systemID)
			if err != nil {
				return nil, status.Errorf(codes.NotFound, "volume %s was not found", vID)
			}
			snapName = generateSnapName(volx.Name)
			snapshotDefX := siotypes.SnapshotDef{VolumeID: vID, SnapshotName: snapName}
			snapshotDefs = append(snapshotDefs, &snapshotDefX)
		}
	}
	snapParam := &siotypes.SnapshotVolumesParam{SnapshotDefs: snapshotDefs}

	// Create snapshot(s)
	snapResponse, err := s.systems[systemID].CreateSnapshotConsistencyGroup(snapParam)
	if err != nil {
		return nil, status.Errorf(codes.AlreadyExists, "Failed to create snapshot: %s", err.Error())
	}

	// populate response structure
	vol, err = s.getVolByID(volID, systemID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "volume %s was not found", volID)
	}
	creationTimeUnix := time.Unix(int64(vol.CreationTime), 0)
	creationTimeStamp, _ := ptypes.TimestampProto(creationTimeUnix)
	dash := "-"
	csiSnapshotID := systemID + dash + snapResponse.VolumeIDList[0]
	snapshot := &csi.Snapshot{SizeBytes: int64(vol.SizeInKb) * bytesInKiB,
		SnapshotId:     csiSnapshotID,
		SourceVolumeId: csiVolID, ReadyToUse: true,
		CreationTime: creationTimeStamp}
	resp := &csi.CreateSnapshotResponse{Snapshot: snapshot}
	s.clearCache()

	Log.Printf("createSnapshot: SnapshotId %s SourceVolumeId %s CreationTime %s",
		snapshot.SnapshotId, snapshot.SourceVolumeId, ptypes.TimestampString(snapshot.CreationTime))
	return resp, nil
}

// Generate a snapshot name with a timestamp.
// Limited to 31 characters. User can alternately supply a snapshot name.
func generateSnapName(volumeName string) string {
	now := time.Now().String()
	vs := strings.Split(now, ".")
	timestamp := strings.Replace(vs[0], " ", "_", -1)
	name := strings.Replace(volumeName+"_"+timestamp, "-", "", -1)
	name = strings.Replace(name, ":", "", -1)
	namebytes := []byte(name)
	name = string(namebytes[0:31])
	return name
}

func (s *service) DeleteSnapshot(
	ctx context.Context,
	req *csi.DeleteSnapshotRequest) (
	*csi.DeleteSnapshotResponse, error) {

	// Display any secrets passed in
	secrets := req.GetSecrets()
	for k, v := range secrets {
		Log.Printf("secret: %s = %s", k, v)
	}

	// Validate snapshot volume
	csiSnapID := req.GetSnapshotId()
	if csiSnapID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "snapshot ID to be deleted is required")
	}

	systemID := s.getSystemIDFromCsiVolumeID(csiSnapID)
	if systemID == "" {
		// use default system
		systemID = s.opts.defaultSystemID
	}

	if systemID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"systemID is not found in the request and there is no default system")
	}

	// Requires probe
	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	snapID := getVolumeIDFromCsiVolumeID(csiSnapID)
	vol, err := s.getVolByID(snapID, systemID)
	if err != nil {
		if strings.Contains(err.Error(), "Could not find the volume") || strings.Contains(err.Error(), "must be a hexadecimal number") {
			Log.Printf("Snapshot %s already deleted on system %s \n", snapID, systemID)
			return &csi.DeleteSnapshotResponse{}, nil
		}
		return nil, status.Errorf(codes.Internal, "Failed to retrieve snapshot: %s", err.Error())
	}

	// Check volume not exposed
	if len(vol.MappedSdcInfo) > 0 {
		ips := ""
		for i, sdc := range vol.MappedSdcInfo {
			if i > 0 {
				ips = ips + ", "
			}
			ips = ips + sdc.SdcIP
		}
		return nil, status.Errorf(codes.FailedPrecondition, "snapshot is in use by the following SDC IP addresses: %s", ips)
	}

	adminClient := s.adminClients[systemID]

	// Check for consistency group delete, and it must be globally enabled as startup option,
	// otherwise only single snap is deleted
	if vol.ConsistencyGroupID != "" && s.opts.EnableSnapshotCGDelete {
		return s.DeleteSnapshotConsistencyGroup(ctx, vol, req, adminClient)
	}

	// Delete snapshot
	tgtVol := goscaleio.NewVolume(adminClient)
	tgtVol.Volume = vol
	err = tgtVol.RemoveVolume(removeModeOnlyMe)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error removing snapshot: %s", err.Error())
	}
	s.clearCache()

	return &csi.DeleteSnapshotResponse{}, nil
}

// DeleteSnapshotConsistencyGroup is called when we wish to delete an entire CG
// of snapshots. We retrieve all the volumes and determine if any are in use.
func (s *service) DeleteSnapshotConsistencyGroup(
	ctx context.Context, snapVol *siotypes.Volume,
	req *csi.DeleteSnapshotRequest, adminClient *goscaleio.Client) (
	*csi.DeleteSnapshotResponse, error) {

	cgVols := make([]*siotypes.Volume, 0)
	exposedVols := make([]string, 0)
	cgID := snapVol.ConsistencyGroupID
	Log.Printf("Called DeleteSnapshotConsistencyGroup id: cg %s\n", cgID)

	// make call to cluster to get all volumes
	// Collect a list of the volumes in the same consistency group (cgVols)
	// Collect the names of volumes that are exposed.
	sioVols, err := adminClient.GetVolume("", "", "", "", true)
	for _, vol := range sioVols {
		if vol.ConsistencyGroupID == cgID {
			Log.Printf("Name %s CG %s ID %s", vol.Name, vol.ConsistencyGroupID, vol.ID)
			cgVols = append(cgVols, vol)
			if len(vol.MappedSdcInfo) > 0 {
				exposedVols = append(exposedVols, fmt.Sprintf("%s (%s) ", vol.Name, vol.ID))
			}
		}
	}

	// If there are any volumes in the consistency group that are exposed,
	// this operation is a non-starter as the volume may be in use.
	if len(exposedVols) > 0 {
		return nil, status.Errorf(codes.FailedPrecondition, "One or more consistency group volumes are exposed and may be in use: %v", exposedVols)
	}
	// If there are no volumes, at least add the original one passed in.
	if len(cgVols) == 0 {
		Log.Printf("Name %s CG %s ID %s", snapVol.Name, snapVol.ConsistencyGroupID, snapVol.ID)
		cgVols = append(cgVols, snapVol)
	}
	Log.Printf("CG Snapshots to be deleted: %v\n", cgVols)

	// Otherwise let's delete them all. If there is an error we fail immediately.
	s.clearCache()
	for _, vol := range cgVols {
		// Delete snapshot
		tgtVol := goscaleio.NewVolume(adminClient)
		tgtVol.Volume = vol
		err = tgtVol.RemoveVolume(removeModeOnlyMe)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "error removing snapshot: %s", err.Error())
		}
	}

	// All good if got here.
	return &csi.DeleteSnapshotResponse{}, nil
}

func (s *service) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {

	var reqID string
	var err error
	headers, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if req, ok := headers["csi.requestid"]; ok && len(req) > 0 {
			reqID = req[0]
		}
	}

	csiVolID := req.GetVolumeId()
	if csiVolID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"volume ID is required")
	}
	//ensure no ambiguity if legacy vol
	err = s.checkVolumesMap(csiVolID)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"checkVolumesMap for id: %s failed : %s", csiVolID, err.Error())

	}

	volID := getVolumeIDFromCsiVolumeID(csiVolID)
	systemID := s.getSystemIDFromCsiVolumeID(csiVolID)
	if systemID == "" {
		// use default system
		systemID = s.opts.defaultSystemID
	}

	if systemID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"systemID is not found in the request and there is no default system")
	}

	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	vol, err := s.getVolByID(volID, systemID)

	if err != nil {
		if strings.EqualFold(err.Error(), sioGatewayVolumeNotFound) || strings.Contains(err.Error(), "must be a hexadecimal number") {
			return nil, status.Error(codes.NotFound, "volume not found")
		}
		return nil, status.Errorf(codes.Internal, "failure to load volume: %s", err.Error())
	}

	volName := vol.Name
	cr := req.GetCapacityRange()
	Log.Printf("cr:%d", cr)
	requestedSize, err := validateVolSize(cr)
	if err != nil {
		return nil, err
	}
	Log.Printf("req.size:%d", requestedSize)
	fields := map[string]interface{}{
		"RequestID":     reqID,
		"VolumeName":    volName,
		"RequestedSize": requestedSize,
	}
	Log.WithFields(fields).Info("Executing ExpandVolume with following fields")
	allocatedSize := int64(vol.SizeInKb)
	Log.Printf("allocatedsize:%d", allocatedSize)

	if requestedSize < allocatedSize {
		return &csi.ControllerExpandVolumeResponse{}, nil
	}

	if requestedSize == allocatedSize {
		Log.Infof("Idempotent call detected for volume (%s) with requested size (%d) SizeInKb and allocated size (%d) SizeInKb",
			volName, requestedSize, allocatedSize)
		return &csi.ControllerExpandVolumeResponse{
			CapacityBytes:         requestedSize * bytesInKiB,
			NodeExpansionRequired: true}, nil
	}

	reqSize := requestedSize / kiBytesInGiB
	tgtVol := goscaleio.NewVolume(s.adminClients[systemID])
	tgtVol.Volume = vol
	err = tgtVol.SetVolumeSize(strconv.Itoa(int(reqSize)))
	if err != nil {
		Log.Errorf("Failed to execute ExpandVolume() with error (%s)", err.Error())
		return nil, status.Error(codes.Internal, err.Error())
	}

	//return the response with NodeExpansionRequired = true, so that CO could call
	// NodeExpandVolume subsequently
	csiResp := &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         requestedSize * bytesInKiB,
		NodeExpansionRequired: true,
	}
	return csiResp, nil
}

// mergeStringMaps adds two string to string maps together
func mergeStringMaps(base map[string]string, additional map[string]string) map[string]string {
	result := make(map[string]string)
	if base != nil {
		for k, v := range base {
			result[k] = v
		}
	}
	if additional != nil {
		for k, v := range additional {
			result[k] = v
		}
	}
	return result

}

func (s *service) Clone(req *csi.CreateVolumeRequest,
	volumeSource *csi.VolumeContentSource_VolumeSource, name string, sizeInKbytes int64, storagePool string) (*csi.CreateVolumeResponse, error) {

	// get systemID from volume source CSI id
	systemID := s.getSystemIDFromCsiVolumeID(volumeSource.VolumeId)
	if systemID == "" {
		// use default system
		systemID = s.opts.defaultSystemID
	}
	if systemID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"systemID is not found in source volume id and there is no default system")
	}

	// Look up the source volume
	sourceVolID := getVolumeIDFromCsiVolumeID(volumeSource.VolumeId)
	srcVol, err := s.getVolByID(sourceVolID, systemID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "Volume not found: %s", volumeSource.VolumeId)
	}

	// Validate the size is the same
	if int64(srcVol.SizeInKb) != sizeInKbytes {
		return nil, status.Errorf(codes.InvalidArgument,
			"Volume %s has incompatible size %d kbytes with requested %d kbytes",
			volumeSource.VolumeId, srcVol.SizeInKb, sizeInKbytes)
	}

	adminClient := s.adminClients[systemID]
	// Validate the storage pool is the same
	volStoragePool := s.getStoragePoolNameFromID(systemID, srcVol.StoragePoolID)
	if volStoragePool != storagePool {
		return nil, status.Errorf(codes.InvalidArgument,
			"Volume storage pool %s is different from the requested storage pool %s", volStoragePool, storagePool)
	}

	// Check for idempotent request
	existingVols, err := adminClient.GetVolume("", "", "", name, false)
	noVolErrString1 := "Error: problem finding volume: Volume not found"
	noVolErrString2 := "Error: problem finding volume: Could not find the volume"
	if (err != nil) && !(strings.Contains(err.Error(), noVolErrString1) || strings.Contains(err.Error(), noVolErrString2)) {
		Log.Printf("[Clone] Idempotency check: GetVolume returned error: %s", err.Error())
		return nil, status.Errorf(codes.Internal, "Failed to create clone -- GetVolume returned unexpected error: %s", err.Error())
	}

	for _, vol := range existingVols {
		if vol.Name == name && vol.StoragePoolID == srcVol.StoragePoolID {
			Log.Printf("Requested volume %s already exists", name)
			csiVolume := s.getCSIVolume(vol, systemID)
			csiVolume.ContentSource = req.GetVolumeContentSource()
			copyInterestingParameters(req.GetParameters(), csiVolume.VolumeContext)
			Log.Printf("Requested volume (from clone) already exists %s (%s) storage pool %s",
				csiVolume.VolumeContext["Name"], csiVolume.VolumeId, csiVolume.VolumeContext["StoragePoolName"])
			return &csi.CreateVolumeResponse{Volume: csiVolume}, nil

		}
	}

	// Snapshot the source volumes
	snapshotDefs := make([]*siotypes.SnapshotDef, 0)
	snapDef := &siotypes.SnapshotDef{VolumeID: sourceVolID, SnapshotName: name}
	snapshotDefs = append(snapshotDefs, snapDef)
	snapParam := &siotypes.SnapshotVolumesParam{SnapshotDefs: snapshotDefs}

	// Create snapshot
	system := s.systems[systemID]
	snapResponse, err := system.CreateSnapshotConsistencyGroup(snapParam)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to call CreateSnapshotConsistencyGroup to clone volume: %s", err.Error())
	}

	if len(snapResponse.VolumeIDList) != 1 {
		return nil, status.Errorf(codes.Internal, "Expected volume ID to be returned but it was not")
	}

	// Retrieve created destination volume
	destID := snapResponse.VolumeIDList[0]
	destVol, err := s.getVolByID(destID, systemID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not retrieve created volume: %s", destID)
	}

	// Create a volume response and return it
	s.clearCache()
	csiVolume := s.getCSIVolume(destVol, systemID)
	csiVolume.ContentSource = req.GetVolumeContentSource()
	copyInterestingParameters(req.GetParameters(), csiVolume.VolumeContext)

	Log.Printf("Volume (from volume clone) %s (%s) storage pool %s",
		csiVolume.VolumeContext["Name"], csiVolume.VolumeId, csiVolume.VolumeContext["storagePoolName"])

	return &csi.CreateVolumeResponse{Volume: csiVolume}, nil

}

// ControllerGetVolume fetch current information about a volume
// returns volume condition if found else returns not found
func (s *service) ControllerGetVolume(ctx context.Context, req *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {

	abnormal := false
	csiVolID := req.GetVolumeId()
	if csiVolID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"volume ID is required")
	}
	volID := getVolumeIDFromCsiVolumeID(csiVolID)
	systemID := s.getSystemIDFromCsiVolumeID(csiVolID)
	if systemID == "" {
		// use default system
		systemID = s.opts.defaultSystemID
	}
	if systemID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"systemID is not found in the request and there is no default system")
	}

	vol, err := s.getVolByID(volID, systemID)
	if err != nil {
		if strings.EqualFold(err.Error(), sioGatewayVolumeNotFound) {
			message := fmt.Sprintf("Volume is not found by controller at %s", time.Now().Format("2006-01-02 15:04:05"))
			return &csi.ControllerGetVolumeResponse{
				Volume: nil,
				Status: &csi.ControllerGetVolumeResponse_VolumeStatus{
					VolumeCondition: &csi.VolumeCondition{
						Abnormal: true,
						Message:  message,
					},
				},
			}, nil
		}
		return nil, status.Errorf(codes.Internal,
			"Volume status could not be determined: %s",
			err.Error())
	}

	csiResp := &csi.ControllerGetVolumeResponse{
		Volume: s.getCSIVolume(vol, systemID),
		Status: &csi.ControllerGetVolumeResponse_VolumeStatus{
			VolumeCondition: &csi.VolumeCondition{
				Abnormal: abnormal,
				Message:  "Volume is in good condition",
			},
		},
	}

	return csiResp, nil
}
