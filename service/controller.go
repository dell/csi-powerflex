package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/goscaleio"
	siotypes "github.com/dell/goscaleio/types/v1"
	ptypes "github.com/golang/protobuf/ptypes"
	log "github.com/sirupsen/logrus"
)

const (
	// KeyStoragePool is the key used to get the storagepool name from the
	// volume create parameters map
	KeyStoragePool = "storagepool"

	// DefaultVolumeSizeKiB is default volume sgolang/protobuf/blob/master/ptypesize to create on a scaleIO
	// cluster when no size is given, expressed in KiB
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

var (
	interestingParameters = [...]string{0: "FsType"}
)

func (s *service) CreateVolume(
	ctx context.Context,
	req *csi.CreateVolumeRequest) (
	*csi.CreateVolumeResponse, error) {

	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}

	s.logStatistics()

	cr := req.GetCapacityRange()
	sizeInKiB, err := validateVolSize(cr)
	if err != nil {
		return nil, err
	}

	// AccessibleTopology not currently supported
	accessibility := req.GetAccessibilityRequirements()
	if accessibility != nil {
		return nil, status.Errorf(codes.InvalidArgument, "Volume AccessibilityRequirements is not currently supported")
	}

	params := req.GetParameters()

	params = mergeStringMaps(params, req.GetSecrets())

	// We require the storagePool name for creation
	sp, ok := params[KeyStoragePool]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument,
			"%s is a required parameter", KeyStoragePool)
	}

	volType := s.getVolProvisionType(params) // Thick or Thin

	name := req.GetName()
	if name == "" {
		return nil, status.Error(codes.InvalidArgument,
			"Name cannot be empty")
	}

	if len(name) > 31 {
		name = name[0:31]
		fmt.Printf("Requested name %s longer than 31 character max, truncated to %s\n", req.Name, name)
		req.Name = name
	}

	// Volume content source support Snapshots only
	contentSource := req.GetVolumeContentSource()
	var snapshotSource *csi.VolumeContentSource_SnapshotSource
	if contentSource != nil {
		volumeSource := contentSource.GetVolume()
		if volumeSource != nil {
			return nil, status.Error(codes.InvalidArgument, "Volume as a VolumeContentSource is not supported (i.e. clone)")
		}
		snapshotSource = contentSource.GetSnapshot()
		if snapshotSource != nil {
			log.Printf("snapshot %s specified as volume content source", snapshotSource.SnapshotId)
			return s.createVolumeFromSnapshot(req, snapshotSource, name, sizeInKiB, sp)
		}
	}

	// TODO handle Access mode in volume capability

	fields := map[string]interface{}{
		"name":        name,
		"sizeInKiB":   sizeInKiB,
		"storagePool": sp,
		"volType":     volType,
	}

	log.WithFields(fields).Info("creating volume")

	volumeParam := &siotypes.VolumeParam{
		Name:           name,
		VolumeSizeInKb: fmt.Sprintf("%d", sizeInKiB),
		VolumeType:     volType,
	}
	createResp, err := s.adminClient.CreateVolume(volumeParam, sp)
	if err != nil {
		// handle case where volume already exists
		if !strings.EqualFold(err.Error(), sioGatewayVolumeNameInUse) {
			log.Printf("error creating volume: %s pool %s error: %s", name, sp, err.Error())
			return nil, status.Errorf(codes.Internal,
				"error when creating volume %s storagepool %s: %s", name, sp, err.Error())
		}
	}

	var id string
	if createResp == nil {
		// volume already exists, look it up by name
		id, err = s.adminClient.FindVolumeID(name)
		if err != nil {
			return nil, status.Errorf(codes.Internal, err.Error())
		}
	} else {
		id = createResp.ID
	}

	vol, err := s.getVolByID(id)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable,
			"error retrieving volume details: %s", err.Error())
	}
	vi := s.getCSIVolume(vol)

	// since the volume could have already exists, double check that the
	// volume has the expected parameters
	spID, err := s.getStoragePoolID(sp)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable,
			"volume exists, but could not verify parameters: %s",
			err.Error())
	}
	if vol.StoragePoolID != spID {
		return nil, status.Errorf(codes.AlreadyExists,
			"volume exists, but in different storage pool than requested")
	}

	if (vi.CapacityBytes / bytesInKiB) != sizeInKiB {
		return nil, status.Errorf(codes.AlreadyExists,
			"volume exists, but at different size than requested")
	}
	copyInterestingParameters(req.GetParameters(), vi.VolumeContext)

	log.Printf("volume %s (%s) created %s\n", vi.VolumeContext["Name"], vi.VolumeId, vi.VolumeContext["CreationTime"])

	csiResp := &csi.CreateVolumeResponse{
		Volume: vi,
	}

	s.clearCache()

	vol, err = s.getVolByID(vi.VolumeId)

	counter := 0

	for err != nil && counter < 100 {
		time.Sleep(3 * time.Millisecond)
		vol, err = s.getVolByID(vi.VolumeId)
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

// Create a volume (which is actually a snapshot) from an existing snapshot.
// The snapshotSource gives the SnapshotId which is the volume to be replicated.
func (s *service) createVolumeFromSnapshot(req *csi.CreateVolumeRequest,
	snapshotSource *csi.VolumeContentSource_SnapshotSource,
	name string, sizeInKbytes int64, storagePool string) (*csi.CreateVolumeResponse, error) {

	// Lookup the snapshot source volume.
	srcVol, err := s.getVolByID(snapshotSource.SnapshotId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "Snapshot not found: %s", snapshotSource.SnapshotId)
	}

	// Validate the size is the same.
	if int64(srcVol.SizeInKb) != sizeInKbytes {
		return nil, status.Errorf(codes.InvalidArgument,
			"Snapshot %s has incompatible size %d kbytes with requested %d kbytes",
			snapshotSource.SnapshotId, srcVol.SizeInKb, sizeInKbytes)
	}

	// Validate the storagePool is the same.
	snapStoragePool := s.getStoragePoolNameFromID(srcVol.StoragePoolID)
	if snapStoragePool != storagePool {
		return nil, status.Errorf(codes.InvalidArgument,
			"Snapshot storage pool %s is different than the requested storage pool %s", snapStoragePool, storagePool)
	}

	// Check for idempotent request
	existingVols, err := s.adminClient.GetVolume("", "", "", name, false)
	for _, vol := range existingVols {
		if vol.Name == name && vol.StoragePoolID == srcVol.StoragePoolID {
			log.Printf("Requested volume %s already exists", name)
			csiVolume := s.getCSIVolume(vol)
			log.Printf("Requested volume (from snap) already exists %s (%s) storage pool %s",
				csiVolume.VolumeContext["Name"], csiVolume.VolumeId, csiVolume.VolumeContext["StoragePoolName"])
			return &csi.CreateVolumeResponse{Volume: csiVolume}, nil
		}
	}

	// Snapshot the source snapshot
	snapshotDefs := make([]*siotypes.SnapshotDef, 0)
	snapDef := &siotypes.SnapshotDef{VolumeID: snapshotSource.SnapshotId, SnapshotName: name}
	snapshotDefs = append(snapshotDefs, snapDef)
	snapParam := &siotypes.SnapshotVolumesParam{SnapshotDefs: snapshotDefs}

	// Create snapshot
	snapResponse, err := s.system.CreateSnapshotConsistencyGroup(snapParam)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to create snapshot: %s", err.Error())
	}
	if len(snapResponse.VolumeIDList) != 1 {
		return nil, status.Errorf(codes.Internal, "Expected volume ID to be returned but it was not")
	}

	// Retrieve created destination volumevolume
	dstID := snapResponse.VolumeIDList[0]
	dstVol, err := s.getVolByID(dstID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not retrieve created volume: %s", dstID)
	}
	// Create a volume response and return it
	s.clearCache()
	csiVolume := s.getCSIVolume(dstVol)
	csiVolume.ContentSource = req.GetVolumeContentSource()
	copyInterestingParameters(req.GetParameters(), csiVolume.VolumeContext)

	log.Printf("Volume (from snap) %s (%s) storage pool %s",
		csiVolume.VolumeContext["Name"], csiVolume.VolumeId, csiVolume.VolumeContext["StoragePoolName"])
	return &csi.CreateVolumeResponse{Volume: csiVolume}, nil
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

	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}
	s.logStatistics()

	id := req.GetVolumeId()

	vol, err := s.getVolByID(id)
	if err != nil {
		if strings.EqualFold(err.Error(), sioGatewayVolumeNotFound) {
			log.WithFields(log.Fields{"id": id}).Debug("volume is already deleted", id)
			return &csi.DeleteVolumeResponse{}, nil
		}
		if strings.Contains(err.Error(), sioVolumeRemovalOperationInProgress) {
			log.WithFields(log.Fields{"id": id}).Debug("volume is currently being deleted", id)
			return &csi.DeleteVolumeResponse{}, nil
		}

		if strings.Contains(err.Error(), "must be a hexadecimal number") {

			log.WithFields(log.Fields{"id": id}).Debug("volume id must be a hexadecimal number", id)
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

	log.WithFields(log.Fields{"name": vol.Name, "id": id}).Info("Deleting volume")
	tgtVol := goscaleio.NewVolume(s.adminClient)
	tgtVol.Volume = vol
	err = tgtVol.RemoveVolume(removeModeOnlyMe)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"error removing volume: %s", err.Error())
	}

	vol, err = s.getVolByID(id)
	counter := 0

	for err != nil && strings.Contains(err.Error(), sioVolumeRemovalOperationInProgress) && counter < 100 {
		time.Sleep(3 * time.Millisecond)
		vol, err = s.getVolByID(id)
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
		log.Printf("VolumeContext:")
		for key, value := range volumeContext {
			log.Printf("    [%s]=%s", key, value)
		}
	}

	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}
	s.logStatistics()

	volID := req.GetVolumeId()
	if volID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"volume ID is required")
	}

	vol, err := s.getVolByID(volID)

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

	sdcID, err := s.getSDCID(nodeID)
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
				log.Debug("volume already mapped")
				return &csi.ControllerPublishVolumeResponse{}, nil
			}
		}

		// If volume has SINGLE_NODE cap, go no farther
		switch am.Mode {
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
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

	targetVolume := goscaleio.NewVolume(s.adminClient)
	targetVolume.Volume = &siotypes.Volume{ID: vol.ID}

	err = targetVolume.MapVolumeSdc(mapVolumeSdcParam)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"error mapping volume to node: %s", err.Error())
	}

	return &csi.ControllerPublishVolumeResponse{}, nil
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
			csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER:
			return nil
		default:
			return status.Errorf(codes.InvalidArgument,
				"Access mode: %v not compatible with access type", am.Mode)
		}
	} else {
		switch am.Mode {
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
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

	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}
	s.logStatistics()

	volID := req.GetVolumeId()
	if volID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"Volume ID is required")
	}

	vol, err := s.getVolByID(volID)
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

	sdcID, err := s.getSDCID(nodeID)
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
		log.Debug("volume already unpublished")
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	targetVolume := goscaleio.NewVolume(s.adminClient)
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

	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}

	volID := req.GetVolumeId()
	vol, err := s.getVolByID(volID)
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
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER:
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

	if err := s.requireProbe(ctx); err != nil {
		return nil, err
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
	source, nextToken, err := s.listVolumes(startToken, maxEntries, true, s.opts.EnableListVolumesSnapshots, "", "")
	if err != nil {
		return nil, err
	}

	// Process the source volumes and make CSI Volumes
	entries := make([]*csi.ListVolumesResponse_Entry, len(source))
	i := 0
	for _, vol := range source {
		entries[i] = &csi.ListVolumesResponse_Entry{
			Volume: s.getCSIVolume(vol),
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

	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}

	var (
		startToken int
		err        error
		maxEntries = int(req.MaxEntries)
		volumeID   string
		ancestorID string
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

	if req.SourceVolumeId != "" {
		ancestorID = req.SourceVolumeId
	}

	if req.SnapshotId != "" {
		volumeID = req.SnapshotId
		// Specifying the SnapshotId is more restrictive than the SourceVolumeId
		// so the latter is ignored.
		ancestorID = ""
	}

	// Call the common listVolumes code to list snapshots only.
	// If sourceVolumeID or snapshotID are provided, we list those use cases and do not use cache.
	source, nextToken, err := s.listVolumes(startToken, maxEntries, false, true, volumeID, ancestorID)

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
			Snapshot: s.getCSISnapshot(vol),
		}
		i = i + 1
	}

	return &csi.ListSnapshotsResponse{
		Entries:   entries,
		NextToken: nextToken,
	}, nil

}

// Subroutine to list volumes for both CSI operations ListVolumes and ListSnapshots.
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
func (s *service) listVolumes(startToken, maxEntries int, doVols, doSnaps bool, volumeID, ancestorID string) ([]*siotypes.Volume, string, error) {
	var (
		volumes  []*siotypes.Volume
		sioVols  []*siotypes.Volume
		sioSnaps []*siotypes.Volume
		err      error
	)

	// Handle exactly one volume or snapshot
	if volumeID != "" || ancestorID != "" {
		sioVols, err = s.adminClient.GetVolume("", volumeID, ancestorID, "", false)

		if err != nil {
			return nil, "", status.Errorf(codes.Internal,
				"Unable to list volumes ID %s AncestorID %s: %s", volumeID, ancestorID, err.Error())
		}
		// This disables the global list requests and the cache.
		doVols = false
		doSnaps = false
	}

	// Process volumes.
	if doVols {
		// Get the volumes from the cache if we can.
		if startToken != 0 && len(s.volCache) > 0 {
			log.Printf("volume cache hit: %d volumes", len(s.volCache))
			func() {
				s.volCacheRWL.Lock()
				defer s.volCacheRWL.Unlock()
				sioVols = make([]*siotypes.Volume, len(s.volCache))
				copy(sioVols, s.volCache)
			}()
		}
		if len(sioVols) == 0 {
			sioVols, err = s.adminClient.GetVolume("", "", "", "", false)
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
				}()
			}
		}
	}

	// Process snapshots.
	if doSnaps {
		if startToken != 0 && len(s.snapCache) > 0 {
			log.Printf("snap cache hit: %d snapshots", len(s.snapCache))
			func() {
				s.snapCacheRWL.Lock()
				defer s.snapCacheRWL.Unlock()
				sioSnaps = make([]*siotypes.Volume, len(s.snapCache))
				copy(sioSnaps, s.snapCache)
			}()
		}
		if len(sioSnaps) == 0 {
			sioSnaps, err = s.adminClient.GetVolume("", "", "", "", true)
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

func (s *service) GetCapacity(
	ctx context.Context,
	req *csi.GetCapacityRequest) (
	*csi.GetCapacityResponse, error) {

	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}

	var statsFunc func() (*siotypes.Statistics, error)

	// Default to get Capacity of system
	statsFunc = s.system.GetStatistics

	params := req.GetParameters()
	if len(params) > 0 {
		// if storage pool is given, get capacity of storage pool
		if spname, ok := params[KeyStoragePool]; ok {
			sp, err := s.adminClient.FindStoragePool("", spname, "")
			if err != nil {
				return nil, status.Errorf(codes.Internal,
					"unable to look up storage pool: %s, err: %s",
					spname, err.Error())
			}
			spc := goscaleio.NewStoragePoolEx(s.adminClient, sp)
			statsFunc = spc.GetStatistics
		}
	}
	stats, err := statsFunc()
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"unable to get system stats: %s", err.Error())
	}
	return &csi.GetCapacityResponse{
		AvailableCapacity: int64(stats.CapacityAvailableForVolumeAllocationInKb * bytesInKiB),
	}, nil
}

func (s *service) ControllerGetCapabilities(
	ctx context.Context,
	req *csi.ControllerGetCapabilitiesRequest) (
	*csi.ControllerGetCapabilitiesResponse, error) {

	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: []*csi.ControllerServiceCapability{
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
						Type: csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
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
		},
	}, nil
}

func (s *service) controllerProbe(ctx context.Context) error {

	// Check that we have the details needed to login to the Gateway
	if s.opts.Endpoint == "" {
		return status.Error(codes.FailedPrecondition,
			"missing VxFlexOS Gateway endpoint")
	}
	if s.opts.User == "" {
		return status.Error(codes.FailedPrecondition,
			"missing VxFlexOS MDM user")
	}
	if s.opts.Password == "" {
		return status.Error(codes.FailedPrecondition,
			"missing VxFlexOS MDM password")
	}
	if s.opts.SystemName == "" {
		return status.Error(codes.FailedPrecondition,
			"missing VxFlexOS system name")
	}

	// Create our ScaleIO API client, if needed
	if s.adminClient == nil {
		c, err := goscaleio.NewClientWithArgs(
			s.opts.Endpoint, "", s.opts.Insecure, !s.opts.DisableCerts)
		if err != nil {
			return status.Errorf(codes.FailedPrecondition,
				"unable to create ScaleIO client: %s", err.Error())
		}
		s.adminClient = c
	}

	if s.adminClient.GetToken() == "" {
		_, err := s.adminClient.Authenticate(&goscaleio.ConfigConnect{
			Endpoint: s.opts.Endpoint,
			Username: s.opts.User,
			Password: s.opts.Password,
		})
		if err != nil {
			return status.Errorf(codes.FailedPrecondition,
				"unable to login to VxFlexOS Gateway: %s", err.Error())

		}
	}

	if s.system == nil {
		system, err := s.adminClient.FindSystem(
			s.opts.SystemName, s.opts.SystemName, "")
		if err != nil {
			return status.Errorf(codes.FailedPrecondition,
				"unable to find matching VxFlexOS system name: %s",
				err.Error())
		}
		s.system = system
	}

	return nil
}

func (s *service) requireProbe(ctx context.Context) error {
	if s.adminClient == nil {
		if !s.opts.AutoProbe {
			return status.Error(codes.FailedPrecondition,
				"Controller Service has not been probed")

		}
		log.Debug("probing controller service automatically")
		if err := s.controllerProbe(ctx); err != nil {
			return status.Errorf(codes.FailedPrecondition,
				"failed to probe/init plugin: %s", err.Error())
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

	// Requires probe
	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}

	// Validate requested name is not to long, if supplied. If so, truncate to 31 characters.
	if req.Name != "" && len(req.Name) > 31 {
		name := req.Name
		name = strings.Replace(name, "snapshot-", "sn-", 1)
		name = name[0:31]
		fmt.Printf("Requested name %s longer than 31 character max, truncated to %s\n", req.Name, name)
		req.Name = name
	}

	if req.Name == "" {

		return nil, status.Errorf(codes.InvalidArgument, "snapshot name cannot be Nil")

	}

	// Validate snapshot volume
	id := req.GetSourceVolumeId()
	if id == "" {
		return nil, status.Errorf(codes.InvalidArgument, "volume ID to be snapped is required")
	}

	// Check for idempotent request, i.e. the snapshot has been already created, by looking up the name.
	existingVols, err := s.adminClient.GetVolume("", "", "", req.Name, false)
	for _, vol := range existingVols {
		ancestor := vol.AncestorVolumeID
		fmt.Printf("idempotent Name %s Name %s Ancestor %s id %s VTree %s pool %s\n",
			vol.Name, req.Name, ancestor, id, vol.VTreeID, vol.StoragePoolID)
		if vol.Name == req.Name && vol.AncestorVolumeID == id {
			fmt.Printf("Idempotent request, snapshot %s ancestor %s already exists\n", req.Name, id)
			snapshot := &csi.Snapshot{SizeBytes: int64(vol.SizeInKb) * bytesInKiB,
				SnapshotId:     vol.ID,
				SourceVolumeId: id, ReadyToUse: true,
				CreationTime: ptypes.TimestampNow()}
			resp := &csi.CreateSnapshotResponse{Snapshot: snapshot}
			return resp, nil
		}
	}

	// Validate volume
	vol, err := s.getVolByID(id)
	if err != nil {
		if strings.EqualFold(err.Error(), sioGatewayVolumeNotFound) {
			return nil, status.Errorf(codes.NotFound, "volume %s was not found", id)
		}
		return nil, status.Errorf(codes.Internal,
			"failure checking volume status: %s", err.Error())
	}
	vtreeID := vol.VTreeID
	log.Printf("vtree ID: %s\n", vtreeID)

	// Build list of volumes to be snapshotted.
	snapshotDefs := make([]*siotypes.SnapshotDef, 0)
	snapName := generateSnapName(vol.Name)
	if req.Name != "" {
		snapName = req.Name
	}
	snapDef := siotypes.SnapshotDef{VolumeID: id, SnapshotName: snapName}
	snapshotDefs = append(snapshotDefs, &snapDef)

	// Determine if we want to add additional volumes to a consistency group
	volIDList := req.Parameters[VolumeIDList]
	if volIDList != "" {
		volIDs := strings.Split(volIDList, ",")
		for _, v := range volIDs {
			volID := strings.Replace(v, " ", "", -1)
			if volID == id {
				// Don't list the original volume again
				continue
			}
			volx, err := s.getVolByID(volID)
			if err != nil {
				return nil, status.Errorf(codes.NotFound, "volume %s was not found", volID)
			}
			snapName = generateSnapName(volx.Name)
			snapshotDefX := siotypes.SnapshotDef{VolumeID: volID, SnapshotName: snapName}
			snapshotDefs = append(snapshotDefs, &snapshotDefX)
		}
	}
	snapParam := &siotypes.SnapshotVolumesParam{SnapshotDefs: snapshotDefs}

	// Create snapshot(s)
	snapResponse, err := s.system.CreateSnapshotConsistencyGroup(snapParam)
	if err != nil {
		return nil, status.Errorf(codes.AlreadyExists, "Failed to create snapshot: %s", err.Error())
	}

	// populate response structure
	snapshot := &csi.Snapshot{SizeBytes: int64(vol.SizeInKb) * bytesInKiB,
		SnapshotId:     snapResponse.VolumeIDList[0],
		SourceVolumeId: id, ReadyToUse: true,
		CreationTime: ptypes.TimestampNow()}
	resp := &csi.CreateSnapshotResponse{Snapshot: snapshot}
	s.clearCache()

	log.Printf("createSnapshot: SnapshotId %s SourceVolumeId %s CreationTime %s",
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
		log.Printf("secret: %s = %s", k, v)
	}

	// Requires probe
	if err := s.requireProbe(ctx); err != nil {
		return nil, err
	}

	// Validate snapshot volume
	id := req.GetSnapshotId()
	if id == "" {
		return nil, status.Errorf(codes.InvalidArgument, "snapshot ID to be deleted is required")
	}
	vol, err := s.getVolByID(id)
	if err != nil {
		if strings.Contains(err.Error(), "Could not find the volume") || strings.Contains(err.Error(), "must be a hexadecimal number") {
			log.Printf("Snapshot %s already deleted\n", id)
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

	// Check for consistency group delete, and it must be globally enabled as startup option,
	// otherwise only single snap is deleted
	if vol.ConsistencyGroupID != "" && s.opts.EnableSnapshotCGDelete {
		return s.DeleteSnapshotConsistencyGroup(ctx, vol, req)
	}

	// Delete snapshot
	tgtVol := goscaleio.NewVolume(s.adminClient)
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
	req *csi.DeleteSnapshotRequest) (
	*csi.DeleteSnapshotResponse, error) {

	cgVols := make([]*siotypes.Volume, 0)
	exposedVols := make([]string, 0)
	cgID := snapVol.ConsistencyGroupID
	log.Printf("Called DeleteSnapshotConsistencyGroup id: cg %s\n", cgID)

	// make call to cluster to get all volumes
	// Collect a list of the volumes in the same consistency group (cgVols)
	// Collect the names of volumes that are exposed.
	sioVols, err := s.adminClient.GetVolume("", "", "", "", true)
	for _, vol := range sioVols {
		if vol.ConsistencyGroupID == cgID {
			log.Printf("Name %s CG %s ID %s", vol.Name, vol.ConsistencyGroupID, vol.ID)
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
		log.Printf("Name %s CG %s ID %s", snapVol.Name, snapVol.ConsistencyGroupID, snapVol.ID)
		cgVols = append(cgVols, snapVol)
	}
	log.Printf("CG Snapshots to be deleted: %v\n", cgVols)

	// Otherwise let's delete them all. If there is an error we fail immediately.
	s.clearCache()
	for _, vol := range cgVols {
		// Delete snapshot
		tgtVol := goscaleio.NewVolume(s.adminClient)
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
	return nil, status.Error(codes.Unimplemented, "")
}

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
