// Copyright © 2019-2026 Dell Inc. or its subsidiaries. All Rights Reserved.
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

// Package service implements the CSI driver controller, node, and identity services for Dell PowerFlex.
package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Ecosystems/container-storage-modules/src/csi-vxflexos/v2/k8sutils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/yaml"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/Ecosystems/container-storage-modules/src/csmlog"
	"github.com/Ecosystems/container-storage-modules/src/goscaleio"
	siotypes "github.com/Ecosystems/container-storage-modules/src/goscaleio/types/v1"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/protobuf/types/known/timestamppb"
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

	// KeyNasName is the key used to get the NAS name from the
	// volume create parameters map
	KeyNasName = "nasName"

	// KeyFsType is the key used to get the filesystem type from the
	// volume create parameters map
	KeyFsType = "fsType"

	// NFSExportLocalPath is the local path for NFSExport
	NFSExportLocalPath = "/"

	// NFSExportNamePrefix is the prefix used for nfs exports created using
	// csi-powerflex driver
	NFSExportNamePrefix = "csishare-"

	// KeyPath is the key used to get path of the associated filesystem
	// from the volume create parameters map
	KeyPath = "path"

	// KeySoftLimit is the key used to get the soft limit of the filesystem
	// from the volume create parameters map
	KeySoftLimit = "softLimit"

	// KeyGracePeriod is the key used to get the grace period from the
	// volume create parameters map
	KeyGracePeriod = "gracePeriod"

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

	// minNfsSize is the minimum filesystem size for NFS
	minNfsSize = 3 * bytesInGiB

	// VolumeIDList is the list of volume IDs
	VolumeIDList = "VolumeIDList"

	removeModeOnlyMe                    = "ONLY_ME"
	sioGatewayNotFound                  = "Not found"
	sioGatewayVolumeNotFound            = "Could not find the volume"
	sioGatewayFileSystemNotFound        = "couldn't find filesystem by id"
	sioVolumeRemovalOperationInProgress = "A volume removal operation is currently in progress"
	sioGatewayVolumeNameInUse           = "Volume name already in use. Please use a different name."
	errNoMultiMap                       = "volume not enabled for mapping to multiple hosts"
	errUnknownAccessType                = "unknown access type is not Block or Mount"
	errUnknownAccessMode                = "access mode cannot be UNKNOWN"
	errNoMultiNodeWriter                = "multi-node with writer(s) only supported for block access type"
	sioReplicationGroupExists           = "The Replication Consistency Group already exists"
	sioReplicationPairExists            = "A Replication Pair for the specified local volume already exists"

	// DriverConfigParamsYaml is the name of the driver config params file.
	DriverConfigParamsYaml = "driver-config-params.yaml"

	// DefaultAPITimeout is the default timeout for PowerFlex API calls.
	DefaultAPITimeout = 10 * time.Second

	// MaxVolumeListEntries limits the page size for ListVolumes, since even a few hundred volumes
	// in the ListVolumeResponse will cause gRPC connection failure between the driver and CO.
	MaxVolumeListEntries = 100
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

var interestingParameters = [...]string{0: "FsType", 1: KeyMkfsFormatOption, 2: KeyBandwidthLimitInKbps, 3: KeyIopsLimit}

// ZoneContent represents the content of an availability zone mapping.
type ZoneContent struct {
	systemID         string
	protectionDomain ProtectionDomainName
	pool             PoolName
}

func (s *service) CreateVolume(
	ctx context.Context,
	req *csi.CreateVolumeRequest) (
	*csi.CreateVolumeResponse, error,
) {
	params := req.GetParameters()
	var systemID string
	var err error

	// zoneConfig is always read from the Secret so we can detect conflicts
	// between zone config and StorageClass PD/Pool parameters even when the
	// StorageClass also specifies an explicit systemID.
	zoneConfig := s.getZonesFromSecret()

	// Reject when zone config and StorageClass PD/Pool are both present,
	// but only if the targeted system actually has zones. If the SC specifies
	// a systemID whose array has no zone configuration, PD/pool in the SC is valid.
	conflictZones := zoneConfig
	if scSystemID, ok := params[KeySystemID]; ok && scSystemID != "" {
		conflictZones = filterZonesBySystem(zoneConfig, scSystemID)
	}
	if err := detectStorageClassZoneConflict(conflictZones, params); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s", err.Error())
	}

	// This is a map of zone to the arrayID and pool identifier; it is only
	// used for zone-based routing when no explicit systemID is requested.
	zoneTargetMap := make(map[ZoneName]ZoneContent)
	if _, ok := params[KeySystemID]; !ok {
		zoneTargetMap = zoneConfig
	}

	if len(zoneTargetMap) == 0 {
		sid, err := s.getSystemIDFromParameters(params)
		if err != nil {
			return nil, err
		}

		systemID = sid
	}

	if systemID != "" {
		if err := s.requireProbe(ctx, systemID); err != nil {
			return nil, err
		}
	}

	s.logStatistics()

	cr := req.GetCapacityRange()

	mutableParams := req.GetMutableParameters()
	if len(mutableParams) > 0 {
		if err := validateMutableParams(mutableParams); err != nil {
			return nil, err
		}
	}
	params = mergeStringMaps(params, mutableParams)

	// Check for filesystem type
	isNFS := false
	var fsType string
	if len(req.VolumeCapabilities) != 0 {
		fsType = req.VolumeCapabilities[0].GetMount().GetFsType()
		if fsType == "nfs" {
			isNFS = true
		}
	}

	// validate AccessibleTopology
	accessibility := req.GetAccessibilityRequirements()
	if accessibility == nil {
		csmlog.Info("Received CreateVolume request without accessibility keys")
	}

	// Look for zone topology
	zoneTopology := false
	var storagePool string
	var protectionDomain string
	var matchedZoneName string
	var volumeTopology []*csi.Topology
	systemSegments := map[string]string{} // topology segments matching requested system for a volume

	// Handle Zone topology, which happens when node is annotated with a matching zone label
	if len(zoneTargetMap) != 0 && accessibility != nil && len(accessibility.GetPreferred()) > 0 {
		contentSource := req.GetVolumeContentSource()
		var sourceSystemID string
		if contentSource != nil {
			csmlog.Infof("[CreateVolume] Zone volume has a content source - we are a snapshot or clone: %+v", contentSource)

			snapshotSource := contentSource.GetSnapshot()
			cloneSource := contentSource.GetVolume()

			if snapshotSource != nil {
				sourceSystemID = s.getSystemIDFromCsiVolumeID(snapshotSource.SnapshotId)
				csmlog.Infof("[CreateVolume] Zone snapshot source systemID: %s", sourceSystemID)
			} else if cloneSource != nil {
				sourceSystemID = s.getSystemIDFromCsiVolumeID(cloneSource.VolumeId)
				csmlog.Infof("[CreateVolume] Zone clone source systemID: %s", sourceSystemID)
			}
		}

		probeErr := false
		for _, topo := range accessibility.GetPreferred() {
			match := matchZoneFromTopology(s.opts.zoneLabelKey, zoneTargetMap, []*csi.Topology{topo}, sourceSystemID)
			if !match.Matched {
				continue
			}

			protectionDomain = match.ProtectionDomain
			storagePool = match.StoragePool
			systemID = match.SystemID
			matchedZoneName = match.ZoneName
			volumeTopology = match.Topology
			zoneTopology = true

			if err := s.requireProbe(ctx, systemID); err != nil {
				csmlog.Errorf("Failed to probe system: %v", systemID)
				// Reset all match state and try the next preferred topology
				probeErr = true
				zoneTopology = false
				matchedZoneName = ""
				volumeTopology = nil
				continue
			}

			csmlog.Infof("Preferred topology zone %s, systemID %s, protectionDomain %s, and storagePool %s", match.ZoneName, systemID, protectionDomain, storagePool)
			break
		}

		if !zoneTopology {
			if probeErr {
				return nil, status.Error(codes.Unavailable,
					"zone topology matched but all matching systems failed probe")
			}
			return nil, status.Error(codes.InvalidArgument,
				formatZoneMatchError(s.opts.zoneLabelKey, zoneTargetMap, accessibility.GetPreferred()))
		}
	}

	if !zoneTopology && accessibility != nil && len(accessibility.GetPreferred()) > 0 {
		requestedSystem := ""
		sID := ""
		system := s.systems[systemID]
		sName := ""
		if system != nil {
			sID = system.System.ID
			// We need to get name of system, in case sc was set up to use name
			sName = system.System.Name
		}

		segments := accessibility.GetPreferred()[0].GetSegments()
		for key := range segments {
			if strings.HasPrefix(key, Name) {
				tokens := strings.Split(key, "/")
				constraint := ""
				if len(tokens) > 1 {
					constraint = tokens[1]
				}
				csmlog.Infof("Found topology constraint: VxFlex OS system: %s", constraint)

				// Update constraint wrt to topology specified for NFS volume
				if isNFS {
					nfsTokens := strings.Split(constraint, "-")
					nfsLabel := ""
					if len(nfsTokens) > 1 {
						constraint = nfsTokens[0]
						nfsLabel = nfsTokens[1]
						if nfsLabel != "nfs" {
							return nil, status.Errorf(codes.InvalidArgument,
								"Invalid topology requested for NFS Volume. Please validate your storage class has nfs topology.")
						}
					}
				}

				// check if constraint is having nvmetcp type
				if strings.HasSuffix(constraint, "nvmetcp") {
					nvmeToken := strings.Split(constraint, "-")
					if len(nvmeToken) > 1 {
						// update constraint to system ID
						constraint = nvmeToken[0]
					}
				}

				if constraint == sID || constraint == sName {
					if constraint == sID {
						requestedSystem = sID
					} else {
						requestedSystem = sName
					}
					// segment matches system ID/Name where volume will be created
					topologyKey := tokens[0] + "/" + sID
					if strings.HasSuffix(key, "nvmetcp") {
						topologyKey = key
					}
					systemSegments[topologyKey] = segments[key]
					csmlog.Infof("Added accessible topology segment for volume: %s, segment: %s = %s", req.GetName(),
						topologyKey, systemSegments[topologyKey])
				}
			}
		}

		// check that the required system id/name matched one of the system id/names from node topology.
		// If the StorageClass explicitly requested a non-zoned system, allow the request even when
		// the accessibility segments only carry zone keys from a mixed-deployment cluster.
		if len(segments) > 0 && requestedSystem == "" {
			if _, explicit := params[KeySystemID]; explicit {
				requestedSystem = sID
				systemSegments[Name+"/"+sID] = ""
			} else {
				return nil, status.Errorf(codes.InvalidArgument,
					"Requested system %s is not accessible from the provided accessibility topology. "+
						"If zones are configured for some systems in the secret, all systems that share the same topology key must also have zones configured. "+
						"Mixing zoned and non-zoned systems on the same topology key is not supported.", systemID)
			}
		}
		if len(systemSegments) > 0 {
			// add topology element containing segments matching required system to volume topology
			volumeTopology = append(volumeTopology, &csi.Topology{
				Segments: systemSegments,
			})
			csmlog.Infof("Accessible topology for volume: %s, segments: %#v", req.GetName(), systemSegments)
		}
	}

	if len(req.VolumeCapabilities) != 0 {
		if req.VolumeCapabilities[0].GetBlock() != nil {
			// We need to check if user requests raw block access from nfs and prevent that
			fsType, ok := params[KeyFsType]
			// FsType can be empty
			if ok && fsType == "nfs" {
				return nil, status.Errorf(codes.InvalidArgument, "raw block requested from NFS Volume")
			}
		}
	}

	platformInfo, err := s.GetPlatformInfo(systemID)
	if err != nil {
		return nil, err
	}

	// abort immediately on unrecognised genType so a future array generation
	// cannot silently apply wrong rounding granularity.
	if !isKnownGenType(platformInfo.GenType) {
		csmlog.Warnf("Unrecognized genType value %q for array-id=%s; known identifiers: [EC]. Volume operation aborted.", platformInfo.GenType, systemID)
		s.granularityMetrics.IncDetectionError() // count detection errors
		return nil, status.Errorf(codes.Internal,
			"unrecognised array generation type %q for system %s; cannot determine volume size granularity",
			platformInfo.GenType, systemID)
	}

	if isNFS && platformInfo.GenType != "" && s.isGenTypeNotSupportsNfsAndReplication(platformInfo.GenType) {
		return nil, status.Errorf(codes.InvalidArgument, "NFS is not supported on the System %s GenType %s", systemID, platformInfo.GenType)
	}

	if isNFS && s.isNfsNotSupported(platformInfo.ArrayVersion) {
		return nil, status.Errorf(codes.InvalidArgument, "NFS is not supported on the System %s PowerFlex version %.1f", systemID, platformInfo.ArrayVersion)
	}

	remoteSystemID, ok := params[s.WithRP(KeyReplicationRemoteSystem)]
	if ok {
		isReplicationEnabledOnPlatform, err := s.IsReplicationEnabledOnPlatforms(systemID, remoteSystemID, platformInfo.GenType)
		if !isReplicationEnabledOnPlatform {
			return nil, err
		}
	}

	// fetch volume name
	name := req.GetName()
	if name == "" {
		return nil, status.Error(codes.InvalidArgument,
			"Name cannot be empty")
	}

	if len(name) > 31 {
		name = name[0:31]
		csmlog.Infof("Requested name %s longer than 31 character max, truncated to %s\n", req.Name, name)
		req.Name = name
	}

	var arr *ArrayConnectionData
	sysID := s.opts.defaultSystemID
	arr = s.opts.arrays[sysID]
	volName := name

	if isNFS {
		// fetch NAS server ID
		var nasName string
		if params[KeyNasName] != "" {
			nasName = params[KeyNasName] // Storage class takes precedence
		} else {
			csmlog.Info("nasName not present in storage class, value taken from secret")
			nasName = arr.NasName // Secret next
		}
		nasServerID, err := s.getNASServerIDFromName(systemID, nasName)
		if err != nil {
			return nil, err
		}

		// fetch storage pool ID
		pdID := ""
		pd, ok := params[KeyProtectionDomain]
		if !ok {
			csmlog.Info("Protection Domain name not provided; there could be conflicts if two storage pools share a name")
		} else {
			pdID, err = s.getProtectionDomainIDFromName(systemID, pd)
			if err != nil {
				return nil, err
			}

		}

		storagePoolName, ok := params[KeyStoragePool]
		if !ok {
			return nil, status.Errorf(codes.InvalidArgument,
				"%s is a required parameter", KeyStoragePool)
		}
		storagePoolID, err := s.getStoragePoolID(storagePoolName, systemID, pdID)
		if err != nil {
			return nil, err
		}

		// fetch volume size
		size := cr.GetRequiredBytes()
		// round off the size to the 3GB if less than 3GB
		if size < minNfsSize {
			csmlog.Infof("Size %d is less than 3GB, rounding to 3GB", size/bytesInGiB)
			size = minNfsSize
		}

		contentSource := req.GetVolumeContentSource()
		if contentSource != nil {
			snapshotSource := contentSource.GetSnapshot()
			if snapshotSource != nil {
				csmlog.Infof("snapshot %s specified as volume content source", snapshotSource.SnapshotId)
				return s.createVolumeFromSnapshot(req, snapshotSource, name, size, storagePoolName)
			}
		}
		// log all parameters used in CreateVolume call
		fields := map[string]interface{}{
			"Name":                               volName,
			"SizeInB":                            size,
			"StoragePoolID":                      storagePoolID,
			"NasServerID":                        nasServerID,
			HeaderPersistentVolumeName:           params[CSIPersistentVolumeName],
			HeaderPersistentVolumeClaimName:      params[CSIPersistentVolumeClaimName],
			HeaderPersistentVolumeClaimNamespace: params[CSIPersistentVolumeClaimNamespace],
		}
		// logctx = csmlog.WithContext(ctx)
		csmlog.WithFields(fields).Info("Executing CreateVolume with following fields")

		volumeParam := &siotypes.FsCreate{
			Name:          volName,
			SizeTotal:     int(size),
			StoragePoolID: storagePoolID,
			NasServerID:   nasServerID,
		}

		// Idempotency check
		system, err := s.adminClients[systemID].FindSystem(systemID, "", "")
		if err != nil {
			return nil, err
		}
		existingFS, err := system.GetFileSystemByIDName("", volName)

		if existingFS != nil {
			if existingFS.SizeTotal == int(size) {
				vi := s.getCSIVolumeFromFilesystem(existingFS, systemID)
				vi.VolumeContext[KeyNasName] = nasName
				vi.VolumeContext[KeyFsType] = fsType
				nfsTopology := s.GetNfsTopology(systemID)
				vi.AccessibleTopology = nfsTopology
				csiResp := &csi.CreateVolumeResponse{
					Volume: vi,
				}
				csmlog.Info("Volume exists in the requested state with same size")
				return csiResp, nil
			}
			csmlog.Info("'Volume name' already exists and size is different")
			return nil, status.Error(codes.AlreadyExists, "'Volume name' already exists and size is different.")
		}
		csmlog.Debug("Volume does not exist, proceeding to create new volume")
		fsResp, err := system.CreateFileSystem(volumeParam)
		if err != nil {
			csmlog.Debugf("Create volume response error:%v", err)
			return nil, status.Errorf(codes.Unknown, "Create Volume %s failed with error: %v", volName, err)
		}

		// set quota limits, if specified in NFS storage class
		isQuotaEnabled := s.opts.IsQuotaEnabled
		if isQuotaEnabled {
			// get filesystem (NFS volume), newly created
			fs, err := system.GetFileSystemByIDName(fsResp.ID, "")
			if err != nil {
				csmlog.Debugf("Find Volume response error: %v", err)
				return nil, status.Errorf(codes.Unknown, "Find Volume response error: %v", err)
			}
			path, ok := params[KeyPath]
			if !ok {
				return nil, status.Errorf(codes.InvalidArgument, "`%s` is a required parameter", KeyPath)
			}

			softLimit, ok := params[KeySoftLimit]
			if !ok {
				return nil, status.Errorf(codes.InvalidArgument, "`%s` is a required parameter", KeySoftLimit)
			}

			gracePeriod, ok := params[KeyGracePeriod]
			if !ok {
				return nil, status.Errorf(codes.InvalidArgument, "`%s` is a required parameter", KeyGracePeriod)
			}

			// create quota for the filesystem
			quotaID, err := s.createQuota(fsResp.ID, path, softLimit, gracePeriod, int(size), isQuotaEnabled, systemID)
			if err != nil {
				// roll back, delete the newly created volume
				if delErr := system.DeleteFileSystem(fs.Name); delErr != nil {
					return nil, status.Errorf(codes.Internal,
						"rollback (deleting volume '%s') failed with error : '%v'", fs.Name, delErr.Error())
				}
				csmlog.Errorf("failed to create quota for volume %s of size %d bytes: %v", fs.Name, size, err)
				csmlog.Debugf("Successfully rolled back by deleting the newly created volume: %s", fs.Name)
				return nil, err
			}
			csmlog.Infof("Tree quota set for: %d bytes on directory: '%s', quota ID: %s", size, path, quotaID)
		}

		newFs, err := system.GetFileSystemByIDName(fsResp.ID, "")
		if err != nil {
			csmlog.Debugf("Find Volume response error: %v", err)
			return nil, status.Errorf(codes.Unknown, "Find Volume response error: %v", err)
		}
		if newFs != nil {
			vi := s.getCSIVolumeFromFilesystem(newFs, systemID)
			vi.VolumeContext[KeyNasName] = nasName
			vi.VolumeContext[KeyFsType] = fsType
			nfsTopology := s.GetNfsTopology(systemID)
			vi.AccessibleTopology = nfsTopology
			csiResp := &csi.CreateVolumeResponse{
				Volume: vi,
			}
			return csiResp, nil
		}
	} else {
		size, err := validateVolSize(cr, platformInfo.GenType)
		if err != nil {
			return nil, err
		}

		// Determine the operation label used for logging, K8s events, and
		// Prometheus metrics (FR-7). Clone and restore share this validateVolSize
		// call but must be credited with their own operation labels per ER FR-4.2
		// ({operation} ∈ {create, expand, clone, restore}).
		volumeOpLabel := "create"
		if cs := req.GetVolumeContentSource(); cs != nil {
			if cs.GetVolume() != nil {
				volumeOpLabel = "clone"
			} else if cs.GetSnapshot() != nil {
				volumeOpLabel = "restore"
			}
		}

		// FR-6: INFO log when size is rounded; DEBUG otherwise.
		originalBytes := cr.GetRequiredBytes()
		roundedBytes := size * bytesInKiB
		if originalBytes != roundedBytes {
			csmlog.Infof("CreateVolume: size rounded from %d bytes to %d bytes (genType: %q, operation: %s)",
				originalBytes, roundedBytes, platformInfo.GenType, volumeOpLabel)
		}

		// FR-5: emit K8s event on the PVC when size was rounded up.
		if s.roundingEmitter != nil {
			s.roundingEmitter.EmitRounded(
				params[CSIPersistentVolumeClaimName],
				params[CSIPersistentVolumeClaimNamespace],
				originalBytes, roundedBytes, volumeOpLabel, platformInfo.GenType,
			)
		}

		// FR-7: increment Prometheus rounding metrics.
		s.granularityMetrics.IncRoundedMetrics(volumeOpLabel, originalBytes, roundedBytes)

		params = mergeStringMaps(params, req.GetSecrets())

		// We require the storagePool name for creation
		if storagePool == "" {
			sp, ok := params[KeyStoragePool]
			if !ok {
				return nil, status.Errorf(codes.InvalidArgument,
					"%s is a required parameter", KeyStoragePool)
			}

			storagePool = sp
		} else {
			csmlog.Infof("[CreateVolume] Multi-AZ Storage Pool Determined by Secret %s", storagePool)
		}

		var pdID string
		if protectionDomain == "" {
			pd, ok := params[KeyProtectionDomain]
			if !ok {
				csmlog.Info("Protection Domain name not provided; there could be conflicts if two storage pools share a name")
			} else {
				protectionDomain = pd
			}
		}

		pdID, err = s.getProtectionDomainIDFromName(systemID, protectionDomain)
		if err != nil {
			if matchedZoneName != "" {
				return nil, status.Error(codes.Internal,
					formatZonePDError(systemID, matchedZoneName, protectionDomain, err))
			}
			return nil, err
		}

		volType := s.getVolProvisionType(params) // Thick or Thin

		contentSource := req.GetVolumeContentSource()
		if contentSource != nil {
			volumeSource := contentSource.GetVolume()
			if volumeSource != nil {
				cloneResponse, err := s.Clone(req, volumeSource, name, size, storagePool)
				if err != nil {
					return nil, err
				}

				cloneResponse.Volume.AccessibleTopology = volumeTopology
				if zoneTopology && matchedZoneName != "" {
					cloneResponse.Volume.VolumeContext["zone"] = matchedZoneName
					cloneResponse.Volume.VolumeContext["protectionDomain"] = protectionDomain
				}

				return cloneResponse, nil
			}
			snapshotSource := contentSource.GetSnapshot()
			if snapshotSource != nil {
				csmlog.Infof("snapshot %s specified as volume content source", snapshotSource.SnapshotId)
				snapshotVolumeResponse, err := s.createVolumeFromSnapshot(req, snapshotSource, name, size, storagePool)
				if err != nil {
					return nil, err
				}

				snapshotVolumeResponse.Volume.AccessibleTopology = volumeTopology
				if zoneTopology && matchedZoneName != "" {
					snapshotVolumeResponse.Volume.VolumeContext["zone"] = matchedZoneName
					snapshotVolumeResponse.Volume.VolumeContext["protectionDomain"] = protectionDomain
				}

				return snapshotVolumeResponse, nil
			}
		}

		// TODO handle Access mode in volume capability

		fields := map[string]interface{}{
			"name":                               name,
			"sizeInKiB":                          size,
			"storagePool":                        storagePool,
			"volType":                            volType,
			HeaderPersistentVolumeName:           params[CSIPersistentVolumeName],
			HeaderPersistentVolumeClaimName:      params[CSIPersistentVolumeClaimName],
			HeaderPersistentVolumeClaimNamespace: params[CSIPersistentVolumeClaimNamespace],
		}
		// logctx = csmlog.WithContext(ctx)

		csmlog.WithFields(fields).Info("Executing CreateVolume with following fields")

		volumeParam := &siotypes.VolumeParam{
			Name:           name,
			VolumeSizeInKb: fmt.Sprintf("%d", size),
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
			csmlog.Warn("warning: goscaleio.VolumeParam: no MetaData method exists, consider updating goscaleio library.")
		}

		createResp, err := createVolumeFunc(s.adminClients[systemID], volumeParam, storagePool, pdID)
		if err != nil {
			// handle case where volume already exists
			if !strings.EqualFold(err.Error(), sioGatewayVolumeNameInUse) {
				csmlog.Infof("error creating volume: %s pool %s (requested size: %d KiB) error: %s", name, storagePool, size, err.Error())
				return nil, status.Errorf(codes.Internal,
					"error when creating volume %s (provisioned size %d KiB, genType %q) storagepool %s: %s",
					name, size, platformInfo.GenType, storagePool, err.Error())
			}
		}

		var id string
		if createResp == nil {
			// volume already exists, look it up by name
			id, err = s.adminClients[systemID].FindVolumeID(name)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "%s", err.Error())
			}
		} else {
			id = createResp.ID
		}

		vol, err := getVolByIDFunc(s, id, systemID)
		if err != nil {
			return nil, status.Errorf(codes.Unavailable,
				"error retrieving volume details: %s", err.Error())
		}
		vi := s.getCSIVolume(vol, systemID)
		vi.AccessibleTopology = volumeTopology

		// since the volume could have already exists, double check that the
		// volume has the expected parameters
		spID, err := s.getStoragePoolID(storagePool, systemID, pdID)
		if err != nil {
			return nil, status.Errorf(codes.Unavailable,
				"volume exists, but could not verify parameters: %s",
				err.Error())
		}
		if vol.StoragePoolID != spID {
			return nil, status.Errorf(codes.AlreadyExists,
				"volume exists in %s, but in different storage pool than requested %s", vol.StoragePoolID, spID)
		}

		if (vi.CapacityBytes / bytesInKiB) != size {
			return nil, status.Errorf(codes.AlreadyExists,
				"volume exists, but at different size than requested")
		}
		copyInterestingParameters(params, vi.VolumeContext)

		csmlog.Infof("volume %s (%s) created %s\n", vi.VolumeContext["Name"], vi.VolumeId, vi.VolumeContext["CreationTime"])

		// Add zone metadata to volume context for observability (AC-006)
		if zoneTopology && matchedZoneName != "" {
			vi.VolumeContext["zone"] = matchedZoneName
			vi.VolumeContext["protectionDomain"] = protectionDomain
		}

		vi.VolumeContext[KeyFsType] = fsType
		csiResp := &csi.CreateVolumeResponse{
			Volume: vi,
		}
		s.clearCache()

		volumeID := getVolumeIDFromCsiVolumeID(vi.VolumeId)
		vol, err = getVolByIDFunc(s, volumeID, systemID)

		counter := 0

		for err != nil && counter < 100 {
			time.Sleep(3 * time.Millisecond)
			vol, err = getVolByIDFunc(s, volumeID, systemID)
			counter = counter + 1
		}
		return csiResp, err
	}
	// return csiResp, err
	return nil, status.Errorf(codes.NotFound, "Volume not found after create. %v", err)
}

func (s *service) createQuota(fsID, path, softLimit, gracePeriod string, size int, isQuotaEnabled bool, systemID string) (string, error) {
	system, err := s.adminClients[systemID].FindSystem(systemID, "", "")
	if err != nil {
		return "", err
	}

	// enabling quota on FS
	fs, err := system.GetFileSystemByIDName(fsID, "")
	if err != nil {
		csmlog.Debugf("Find Volume response error: %v", err)
		return "", status.Errorf(codes.Unknown, "Find Volume response error: %v", err)
	}

	// validate quota parameters
	softLimitPerc, gracePeriodInt, err := validateQuotaParameters(path, softLimit, gracePeriod, fsID)
	if err != nil {
		return "", err
	}

	// converting soft limit from percentage to value
	softLimitInt := (softLimitPerc * int64(size)) / 100

	// modify FS to set quota
	fsModify := &siotypes.FSModify{
		IsQuotaEnabled: isQuotaEnabled,
	}

	err = system.ModifyFileSystem(fsModify, fs.ID)
	if err != nil {
		csmlog.Debugf("Modify NFS volume failed with error: %v", err)
		return "", status.Errorf(codes.Unknown, "Modify NFS volume failed with error: %v", err)
	}

	fs, err = system.GetFileSystemByIDName(fsID, "")
	if err != nil {
		csmlog.Debugf("Find NFS volume response error: %v", err)
		return "", status.Errorf(codes.Unknown, "Find NFS volume response error: %v", err)
	}

	// need to set the quota based on the requested pv size
	// if a size isn't requested, skip creating the quota
	if size <= 0 {
		csmlog.Debugf("Quotas is enabled, but storage size is not requested, skip creating quotas for volume '%s'", fsID)
		return "", nil
	}

	// Check if softLimit less hardLimit (volume size)
	if int(softLimitInt) >= size {
		return "", status.Errorf(codes.InvalidArgument, "requested softLimit: %s perc is greater than volume size: %d for volume %s:", softLimit, size, fsID)
	}

	// Check if softLimit is unlimited, i.e. 0 bytes
	if softLimitInt == 0 {
		return "", status.Errorf(codes.InvalidArgument, "requested softLimit: %s perc, i.e. default value which is greater than hardlimit, i.e. volume size: %d for volume %s:", softLimit, size, fsID)
	}

	csmlog.Debugf("Begin to set quota for FS '%s', size '%d', quota enabled: '%t'", fsID, size, isQuotaEnabled)
	// log all parameters used in CreateTreeQuota call
	fields := map[string]interface{}{
		"FileSystemID": fsID,
		"Path":         path,
		"HardLimit":    size,
		"SoftLimit":    softLimitInt,
		"GracePeriod":  gracePeriodInt,
	}
	// logctx = csmlog.WithContext(ctx)
	csmlog.WithFields(fields).Info("Executing CreateTreeQuota with following fields")

	createQuotaParams := &siotypes.TreeQuotaCreate{
		FileSystemID: fsID,
		Path:         path,
		HardLimit:    size,
		SoftLimit:    int(softLimitInt),
		GracePeriod:  int(gracePeriodInt),
	}
	quota, err := system.CreateTreeQuota(createQuotaParams)
	if err != nil {
		csmlog.Debugf("Creating quota failed with error: %v", err)
		return "", status.Errorf(codes.Unknown, "Creating quota failed with error: %v", err)
	}
	return quota.ID, nil
}

// validate the requested quota parameters.
func validateQuotaParameters(path, softLimit, gracePeriod, fsID string) (int64, int64, error) {
	if path == "" {
		return 0, 0, status.Errorf(codes.InvalidArgument, "path not set for volume: %s,", fsID)
	}

	var err error
	var softLimitPerc int64
	if softLimit != "" {
		softLimitPerc, err = strconv.ParseInt(softLimit, 10, 64)
		if err != nil {
			return 0, 0, status.Errorf(codes.InvalidArgument, "requested softLimit: %s is not numeric for volume %s, error: %s", softLimit, fsID, err)
		}
	} else {
		return 0, 0, status.Errorf(codes.InvalidArgument, "softLimit not set for volume: %s,", fsID)
	}

	var gracePeriodInt int64
	if gracePeriod != "" {
		gracePeriodInt, err = strconv.ParseInt(gracePeriod, 10, 64)
		if err != nil {
			return 0, 0, status.Errorf(codes.InvalidArgument, "requested gracePeriod: %s is not numeric for volume %s, error: %s", gracePeriod, fsID, err)
		}
	} else {
		csmlog.Debugf("GracePeriod value set to default.")
		gracePeriodInt = 0
	}
	return softLimitPerc, gracePeriodInt, nil
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

	csmlog.Infof("getSystemIDFromParameters system %s", systemID)

	// if name set for array.SystemID use id instead
	// names can change , id will remain unique
	if id, ok := s.connectedSystemNameToID[systemID]; ok {
		systemID = id
	}
	csmlog.Infof("Use systemID as %s", systemID)
	return systemID, nil
}

// ZoneMatchResult holds the outcome of matching a CSI topology request against the zone target map.
type ZoneMatchResult struct {
	SystemID         string
	ProtectionDomain string
	StoragePool      string
	ZoneName         string
	Topology         []*csi.Topology
	Matched          bool
}

// matchZoneFromTopology matches the preferred topologies from a CreateVolume request
// against the zone target map and returns the routing result. If sourceSystemID is
// non-empty (snapshot/clone), only zones on that system are considered.
func matchZoneFromTopology(
	zoneLabelKey string,
	zoneTargetMap map[ZoneName]ZoneContent,
	preferred []*csi.Topology,
	sourceSystemID string,
) ZoneMatchResult {
	for _, topo := range preferred {
		if topo == nil {
			continue
		}
		for topoLabel, zoneName := range topo.Segments {
			if topoLabel == zoneLabelKey {
				zoneTarget, ok := zoneTargetMap[ZoneName(zoneName)]
				if !ok {
					continue
				}
				if sourceSystemID != "" && zoneTarget.systemID != sourceSystemID {
					continue
				}
				return ZoneMatchResult{
					SystemID:         zoneTarget.systemID,
					ProtectionDomain: string(zoneTarget.protectionDomain),
					StoragePool:      string(zoneTarget.pool),
					ZoneName:         zoneName,
					Topology: []*csi.Topology{
						{Segments: map[string]string{zoneLabelKey: zoneName}},
					},
					Matched: true,
				}
			}
		}
	}
	return ZoneMatchResult{}
}

// filterZonesBySystem returns only the zone entries that belong to the given systemID.
// This allows the conflict check to be scoped to the targeted system: if the SC
// targets a non-zoned system, an empty map is returned and no conflict is raised.
func filterZonesBySystem(zones map[ZoneName]ZoneContent, systemID string) map[ZoneName]ZoneContent {
	filtered := make(map[ZoneName]ZoneContent)
	for name, content := range zones {
		if content.systemID == systemID {
			filtered[name] = content
		}
	}
	return filtered
}

// detectStorageClassZoneConflict rejects a CreateVolume request when zone
// config is present in the Secret AND StorageClass parameters specify
// protectionDomain or storagePool. These are conflicting configuration
// sources: zone config determines PD/pool, so StorageClass must not override them.
func detectStorageClassZoneConflict(zoneTargetMap map[ZoneName]ZoneContent, params map[string]string) error {
	if len(zoneTargetMap) == 0 {
		return nil
	}
	// StorageClass parameter keys may be camelCase or all lower case; normalize
	// by doing case-insensitive lookups.
	paramValue := func(key string) string {
		for k, v := range params {
			if strings.EqualFold(k, key) && v != "" {
				return v
			}
		}
		return ""
	}
	conflicting := make([]string, 0, 2)
	if paramValue(KeyProtectionDomain) != "" {
		conflicting = append(conflicting, "protectionDomain")
	}
	if paramValue(KeyStoragePool) != "" {
		conflicting = append(conflicting, "storagePool")
	}
	if len(conflicting) > 0 {
		return fmt.Errorf(
			"zone config in Secret conflicts with StorageClass parameters %s; "+
				"when zones are configured, StorageClass must not specify protectionDomain or storagePool",
			strings.Join(conflicting, ", "))
	}
	return nil
}

// formatZoneMatchError produces a descriptive error when no preferred topology
// matches any configured zone. It lists the requested zone names from the
// topology and the available zones from the secret.
func formatZoneMatchError(zoneLabelKey string, zoneTargetMap map[ZoneName]ZoneContent, preferred []*csi.Topology) string {
	requestedZones := make([]string, 0)
	for _, topo := range preferred {
		if topo == nil {
			continue
		}
		for k, v := range topo.GetSegments() {
			if strings.HasPrefix(k, zoneLabelKey) {
				requestedZones = append(requestedZones, v)
			}
		}
	}
	sort.Strings(requestedZones)

	availableZones := make([]string, 0, len(zoneTargetMap))
	for z, content := range zoneTargetMap {
		availableZones = append(availableZones, fmt.Sprintf("%s (system=%s, PD=%s)", z, content.systemID, content.protectionDomain))
	}
	sort.Strings(availableZones)

	return fmt.Sprintf("no zone topology found in accessibility requirements: "+
		"requested zones %v do not match any available zones [%s]",
		requestedZones, strings.Join(availableZones, "; "))
}

// formatZonePDError wraps a PD-related error with zone context so operators
// can quickly identify which zone and system are affected.
func formatZonePDError(systemID, zoneName, protectionDomain string, innerErr error) string {
	return fmt.Sprintf("failed to resolve protection domain %q for zone %q on system %s: %v",
		protectionDomain, zoneName, systemID, innerErr)
}

// getZonesFromSecret returns a map with zone names as keys to zone content
// with zone content consisting of the PowerFlex systemID, protection domain and pool.
// It iterates the Zones[] slice on each array (populated by normalizeZoneConfig at startup).
func (s *service) getZonesFromSecret() map[ZoneName]ZoneContent {
	zoneTargetMap := make(map[ZoneName]ZoneContent)

	for _, array := range s.opts.arrays {
		for _, z := range array.Zones {
			if len(z.ProtectionDomains) == 0 {
				continue
			}

			var pd ProtectionDomainName
			if z.ProtectionDomains[0].Name != "" {
				pd = z.ProtectionDomains[0].Name
			}

			var pool PoolName
			if len(z.ProtectionDomains[0].Pools) > 0 {
				pool = z.ProtectionDomains[0].Pools[0]
			}

			// getArrayConfig already rejects configs where the same zone name appears
			// on multiple systems, so a collision here is impossible at runtime.
			zoneTargetMap[z.Name] = ZoneContent{
				systemID:         array.SystemID,
				protectionDomain: pd,
				pool:             pool,
			}
		}
	}
	return zoneTargetMap
}

var getVolByIDFunc = func(s *service, id string, systemID string) (*siotypes.Volume, error) {
	return s.getVolByID(id, systemID)
}

var getStoragePoolNameFromIDFunc = func(s *service, systemID string, id string) string {
	return s.getStoragePoolNameFromID(systemID, id)
}

var getVolumeFunc = func(adminClient *goscaleio.Client, a, b, c, name string, e bool) ([]*siotypes.Volume, error) {
	return adminClient.GetVolume(a, b, c, name, e)
}

var createVolumeFunc = func(adminClient *goscaleio.Client, volumeParam *siotypes.VolumeParam, storagePoolName, protectionDomain string) (*siotypes.VolumeResp, error) {
	return adminClient.CreateVolume(volumeParam, storagePoolName, protectionDomain)
}

var createThinCloneFunc = func(system *goscaleio.System, snapParam *siotypes.CreateSnapshotParam) (*siotypes.SnapshotVolumesResp, error) {
	return system.CreateThinClone(snapParam)
}

// Create a volume (which is actually a snapshot) from an existing snapshot.
// The snapshotSource gives the SnapshotId which is the volume to be replicated.
func (s *service) createVolumeFromSnapshot(req *csi.CreateVolumeRequest,
	snapshotSource *csi.VolumeContentSource_SnapshotSource,
	name string, sizeInKbytes int64, storagePool string,
) (*csi.CreateVolumeResponse, error) {
	params := mergeStringMaps(req.GetParameters(), req.GetMutableParameters())
	isNFS := false
	var fsType string
	if len(req.VolumeCapabilities) != 0 {
		fsType = req.VolumeCapabilities[0].GetMount().GetFsType()
		if fsType == "nfs" {
			isNFS = true
		}
	}

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

	if isNFS {
		// Look up the snapshot
		fmt.Println("snapshotSource.SnapshotId", snapshotSource.SnapshotId)
		snapID := getFilesystemIDFromCsiVolumeID(snapshotSource.SnapshotId)
		srcVol, err := s.getFilesystemByID(snapID, systemID)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "Snapshot not found: %s, error: %s", snapshotSource.SnapshotId, err.Error())
		}

		// Validate the size is the same.
		if int64(srcVol.SizeTotal) != sizeInKbytes {
			return nil, status.Errorf(codes.InvalidArgument,
				"Snapshot %s has incompatible size %d bytes with requested %d bytes",
				snapshotSource.SnapshotId, srcVol.SizeTotal, sizeInKbytes)
		}

		system := s.systems[systemID]

		// Validate the storagePool is the same.
		snapStoragePool := s.getStoragePoolNameFromID(systemID, srcVol.StoragePoolID)
		if snapStoragePool != storagePool {
			return nil, status.Errorf(codes.InvalidArgument,
				"Snapshot storage pool %s is different than the requested storage pool %s", snapStoragePool, storagePool)
		}

		_, err = system.RestoreFileSystemFromSnapshot(&siotypes.RestoreFsSnapParam{
			SnapshotID: snapID,
		}, srcVol.ParentID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "error during fs creation from snapshot: %s, error: %s", snapshotSource.SnapshotId, err.Error())
		}

		restoreFs, err := system.GetFileSystemByIDName(srcVol.ParentID, "")
		if err != nil {
			if strings.Contains(err.Error(), sioGatewayFileSystemNotFound) {
				return nil, status.Errorf(codes.NotFound, "NFS volume not found: %s, error: %s", srcVol.ID, err.Error())
			}
		}

		csiVolume := s.getCSIVolumeFromFilesystem(restoreFs, systemID)

		csiVolume.ContentSource = req.GetVolumeContentSource()
		copyInterestingParameters(params, csiVolume.VolumeContext)

		csmlog.Infof("Volume (from snap) %s (%s) storage pool %s",
			csiVolume.VolumeContext["Name"], csiVolume.VolumeId, csiVolume.VolumeContext["StoragePoolName"])
		return &csi.CreateVolumeResponse{Volume: csiVolume}, nil

	}

	// Look up the snapshot
	snapID := getVolumeIDFromCsiVolumeID(snapshotSource.SnapshotId)
	srcVol, err := getVolByIDFunc(s, snapID, systemID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "Snapshot not found: %s, error: %s", snapshotSource.SnapshotId, err.Error())
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
	snapStoragePool := getStoragePoolNameFromIDFunc(s, systemID, srcVol.StoragePoolID)
	if snapStoragePool != storagePool {
		return nil, status.Errorf(codes.InvalidArgument,
			"Snapshot storage pool %s is different than the requested storage pool %s", snapStoragePool, storagePool)
	}

	// Check for idempotent request
	existingVols, err := getVolumeFunc(adminClient, "", "", "", name, false)
	noVolErrString1 := "Error: problem finding volume: Volume not found"
	noVolErrString2 := "Error: problem finding volume: Could not find the volume"
	if (err != nil) && !(strings.Contains(err.Error(), noVolErrString1) || strings.Contains(err.Error(), noVolErrString2)) {
		csmlog.Infof("[createVolumeFromSnapshot] Idempotency check: GetVolume returned error: %s", err.Error())
		return nil, status.Errorf(codes.Internal, "Failed to create vol from snap -- GetVolume returned unexpected error: %s", err.Error())
	}

	for _, vol := range existingVols {
		if vol.Name == name && vol.StoragePoolID == srcVol.StoragePoolID {
			csmlog.Infof("Requested volume %s already exists", name)
			csiVolume := s.getCSIVolume(vol, systemID)
			csiVolume.ContentSource = req.GetVolumeContentSource()
			copyInterestingParameters(params, csiVolume.VolumeContext)
			csmlog.Infof("Requested volume (from snap) already exists %s (%s) storage pool %s",
				csiVolume.VolumeContext["Name"], csiVolume.VolumeId, csiVolume.VolumeContext["StoragePoolName"])
			return &csi.CreateVolumeResponse{Volume: csiVolume}, nil
		}
	}

	// Snapshot the source snapshot
	snapshotDefs := make([]*siotypes.SnapshotDef, 0)
	snapDef := &siotypes.SnapshotDef{VolumeID: snapID, SnapshotName: name}
	snapshotDefs = append(snapshotDefs, snapDef)
	snapResponse := &siotypes.SnapshotVolumesResp{}

	// Create clone or snapshot
	if srcVol.GenType == "EC" {
		snapParam := &siotypes.CreateSnapshotParam{SnapshotDefs: snapshotDefs}
		snapResponse, err = createThinCloneFunc(system, snapParam)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to call CreateThinClone to create volume from snapshot: %s", err.Error())
		}
	} else {
		snapParam := &siotypes.SnapshotVolumesParam{SnapshotDefs: snapshotDefs, AccessMode: "ReadWrite"}
		snapResponse, err = system.CreateSnapshotConsistencyGroup(snapParam)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to call CreateSnapshotConsistencyGroup to create volume from snapshot: %s", err.Error())
		}
	}

	if len(snapResponse.VolumeIDList) != 1 {
		return nil, status.Errorf(codes.Internal, "Expected volume ID to be returned but it was not")
	}

	// Retrieve created destination volume
	dstID := snapResponse.VolumeIDList[0]
	dstVol, err := s.getVolByID(dstID, systemID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not retrieve created volume: %s, error: %s", dstID, err.Error())
	}
	// Create a volume response and return it
	s.clearCache()
	csiVolume := s.getCSIVolume(dstVol, systemID)
	csiVolume.ContentSource = req.GetVolumeContentSource()
	copyInterestingParameters(params, csiVolume.VolumeContext)

	csmlog.Infof("Volume (from snap) %s (%s) storage pool %s",
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
// the given limit. Returned size is in KiB.
//
// genType controls the rounding granularity:
//   - "EC" (Gen2/EC arrays) → 1 GiB ceiling rounding (FR-1)
//   - ""   (Gen1 arrays)    → 8 GiB multiple rounding (backward-compatible)
//
// CALLERS MUST validate genType with isKnownGenType before invoking this
// function. Any non-EC value (including unrecognised future identifiers) is
// treated as Gen1 by this function WITHOUT validation — the caller is
// responsible for rejecting unknown values before they reach here.
func validateVolSize(cr *csi.CapacityRange, genType string) (int64, error) {
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
		sizeGiBFloat float64
		sizeGiB      int64
		sizeKiB      int64
		sizeB        int64
	)

	// Calculate size in GiB using float for precision.
	sizeGiBFloat = float64(minSize) / float64(kiBytesInGiB)

	// Use math.Ceil to round up to the nearest whole GiB.
	sizeGiB = int64(math.Ceil(sizeGiBFloat))

	// Enforce minimum of 1 GiB.
	if sizeGiB < 1 {
		sizeGiB = 1
	}

	// Apply genType-aware granularity:
	//   Gen2/EC ("EC") → 1 GiB ceiling — already done by math.Ceil above.
	//   Gen1 ("")      → round up to next multiple of 8 GiB (original behaviour).
	if genType != "EC" {
		// VxFlexOS Gen1 creates volumes in multiples of 8 GiB, rounding up.
		mod := sizeGiB % VolSizeMultipleGiB
		if mod > 0 {
			sizeGiB = sizeGiB - mod + VolSizeMultipleGiB
		}
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

// isKnownGenType reports whether genType is one of the recognised values that
// the CSI driver understands:
//   - ""   — Gen1 arrays (protection domains have no genType field set)
//   - "EC" — Gen2/EC arrays (protection domains return genType "EC")
//
// Any other value indicates an unexpected PFMP response and should be treated
// as an error by callers (FR-3).
func isKnownGenType(genType string) bool {
	return genType == "" || genType == "EC"
}

func (s *service) DeleteVolume(
	ctx context.Context,
	req *csi.DeleteVolumeRequest) (
	*csi.DeleteVolumeResponse, error,
) {
	csiVolID := req.GetVolumeId()
	if csiVolID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"volume ID is required")
	}

	isNFS := strings.Contains(csiVolID, "/")
	// ensure no ambiguity if legacy vol
	err := s.checkVolumesMap(csiVolID)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"checkVolumesMap for id: %s failed : %s", csiVolID, err.Error())
	}

	if isNFS {
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
		system, err := s.adminClients[systemID].FindSystem(systemID, "", "")
		if err != nil {
			return nil, err
		}
		fsID := getFilesystemIDFromCsiVolumeID(csiVolID)
		toBeDeletedFS, err := system.GetFileSystemByIDName(fsID, "")
		if err != nil {
			if strings.Contains(err.Error(), sioGatewayFileSystemNotFound) {
				csmlog.WithFields(csmlog.Fields{"id": fsID}).Debug("NFS volume does not exist")
				return &csi.DeleteVolumeResponse{}, nil
			}
		}

		listSnaps, err := system.GetFsSnapshotsByVolumeID(fsID)
		if err != nil {
			return nil, status.Errorf(codes.Unknown, "failure getting snapshot: %s", err.Error())
		}

		if len(listSnaps) > 0 {
			return nil, status.Errorf(codes.FailedPrecondition,
				"unable to delete NFS volume -- snapshots based on this volume still exist: %v",
				listSnaps)
		}

		fsName := toBeDeletedFS.Name

		// Check if nfs export exists for the File system
		client := s.adminClients[systemID]

		nfsExport, err := s.getNFSExport(toBeDeletedFS, client)
		if err != nil {
			if !strings.Contains(err.Error(), "not found") {
				return nil, status.Errorf(codes.Internal,
					"error getting the NFS Export for the fs: %s", err.Error())
			}
		}

		if nfsExport != nil &&
			(len(nfsExport.ReadOnlyHosts) > 0 ||
				len(nfsExport.ReadOnlyRootHosts) > 0 ||
				len(nfsExport.ReadWriteHosts) > 0 ||
				len(nfsExport.ReadWriteRootHosts) > 0) {
			// if one entry is there for RWRootHosts or RWHosts, check if this is the same externalAccess defined in value.yaml
			// if yes modifyNFSExport and remove externalAccess from the HostAcceesList on the array
			if (len(nfsExport.ReadWriteRootHosts) == 1 || len(nfsExport.ReadWriteHosts) == 1) && s.opts.ExternalAccess != "" {
				externalAccess := s.opts.ExternalAccess
				modifyNFSExport := false
				// we need to construct the payload dynamically otherwise 400 error will be thrown
				var modifyParam *siotypes.NFSExportModify = &siotypes.NFSExportModify{}
				// Removing externalAccess from RWHosts as well as RWRootHosts
				if len(nfsExport.ReadWriteRootHosts) == 1 && externalAccess == nfsExport.ReadWriteRootHosts[0] {
					csmlog.Debugf("Trying to remove externalAccess IP with mask having RWRootHosts access while deleting the volume: %v ", externalAccess)
					modifyNFSExport = true
					modifyParam.RemoveReadWriteRootHosts = []string{externalAccess}
				}
				if len(nfsExport.ReadWriteHosts) == 1 && externalAccess == nfsExport.ReadWriteHosts[0] {
					csmlog.Debugf("Trying to remove externalAccess IP with mask having RWHosts access while deleting the volume: %v", externalAccess)
					modifyNFSExport = true
					modifyParam.RemoveReadWriteHosts = []string{externalAccess}
				}
				// call ModifyNFSExport API only when modifyParam payload is not empty i.e. something is there to modify
				if modifyNFSExport {
					err = client.ModifyNFSExport(modifyParam, fsID)
					if err != nil {
						csmlog.Warnf("failure when removing externalAccess from nfs export: %v", err)
					}
				} else {
					// either of RWRootHosts or RWHosts has one entry but it is not externalAccess
					return nil, status.Errorf(codes.FailedPrecondition,
						"filesystem %s can not be deleted as it has associated NFS shares.",
						fsID)
				}
			} else {
				return nil, status.Errorf(codes.FailedPrecondition,
					"filesystem %s can not be deleted as it has associated NFS shares.",
					fsID)
			}
		}

		csmlog.WithFields(csmlog.Fields{"name": fsName, "id": fsID}).Info("Deleting NFS volume")
		err = system.DeleteFileSystem(fsName)
		if err != nil {
			if strings.Contains(err.Error(), sioGatewayFileSystemNotFound) {
				return &csi.DeleteVolumeResponse{}, nil
			}
			return nil, status.Errorf(codes.Internal,
				"error deleting NFS volume: %s", err.Error())
		}
		return &csi.DeleteVolumeResponse{}, nil
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
			csmlog.Debugf("volume is already deleted : %v", csiVolID)
			return &csi.DeleteVolumeResponse{}, nil
		}
		if strings.Contains(err.Error(), sioVolumeRemovalOperationInProgress) {
			csmlog.Debugf("volume is currently being deleted : %v", csiVolID)
			return &csi.DeleteVolumeResponse{}, nil
		}

		if strings.Contains(err.Error(), "must be a hexadecimal number") {

			csmlog.Debugf("volume id must be a hexadecimal number : %v", csiVolID)
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

	// If volume is marked for replication, remove the replication pair first.
	if vol.VolumeReplicationState != "UnmarkedForReplication" {
		csmlog.Infof("[DeleteVolume] - vol: %+v", vol)
		pair, err := s.removeVolumeFromReplicationPair(systemID, volID)
		if err != nil {
			return nil, status.Errorf(codes.Internal,
				"error removing replication pair: %s", err.Error())
		}
		csmlog.Infof("[DeleteVolume] - Removed Pair: %+v", pair)
	}

	csmlog.WithFields(csmlog.Fields{"name": vol.Name, "id": csiVolID}).Info("Deleting volume")
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

// CreateKubeClientSet creates a Kubernetes client set.
var CreateKubeClientSet = k8sutils.CreateKubeClientSet

func (s *service) findNetworkInterfaceIPs() ([]string, error) {
	if K8sClientset == nil {
		err := CreateKubeClientSet()
		if err != nil {
			csmlog.Errorf("Failed to create Kubernetes clientset: %v", err)
			return []string{}, err
		}
		K8sClientset = k8sutils.Clientset
	}

	// Get the ConfigMap
	configMap, err := K8sClientset.CoreV1().ConfigMaps(DriverNamespace).Get(context.TODO(), DriverConfigMap, metav1.GetOptions{})
	if err != nil {
		csmlog.Errorf("Failed to get the ConfigMap: %v", err)
		return []string{}, err
	}

	var configData map[string]interface{}
	var allNetworkInterfaceIPs []string

	if configParamsYaml, ok := configMap.Data[DriverConfigParamsYaml]; ok {
		err := yaml.Unmarshal([]byte(configParamsYaml), &configData)
		if err != nil {
			csmlog.Errorf("Failed to unmarshal the ConfigMap params: %v", err)
			return []string{}, err
		}

		if interfaceNames, ok := configData["interfaceNames"].(map[string]interface{}); ok {
			for _, ipAddressList := range interfaceNames {

				ipAddresses := strings.Split(ipAddressList.(string), ",")
				allNetworkInterfaceIPs = append(allNetworkInterfaceIPs, ipAddresses...)
			}
			return allNetworkInterfaceIPs, nil
		}
	}
	return []string{}, fmt.Errorf("failed to get the Network Interface IPs")
}

func (s *service) ControllerPublishVolume(
	ctx context.Context,
	req *csi.ControllerPublishVolumeRequest) (
	*csi.ControllerPublishVolumeResponse, error,
) {
	volumeContext := req.GetVolumeContext()
	if volumeContext != nil {
		csmlog.Infof("VolumeContext:")
		for key, value := range volumeContext {
			csmlog.Infof("    [%s]=%s", key, value)
		}
	}

	if req.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, "volume capability is required")
	}

	am := req.GetVolumeCapability().GetAccessMode()
	if am == nil {
		return nil, status.Error(codes.InvalidArgument, "access mode is required")
	}

	if am.Mode == csi.VolumeCapability_AccessMode_UNKNOWN {
		return nil, status.Error(codes.InvalidArgument, errUnknownAccessMode)
	}

	nodeID := req.GetNodeId()
	if nodeID == "" {
		return nil, status.Error(codes.InvalidArgument, "node ID is required")
	}

	// create publish context
	publishContext := make(map[string]string)
	publishContext[KeyNasName] = volumeContext[KeyNasName]

	csiVolID := req.GetVolumeId()
	publishContext["volumeContextId"] = csiVolID

	if csiVolID == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID is required")
	}

	// get systemID from req
	systemID := s.getSystemIDFromCsiVolumeID(csiVolID)
	if systemID == "" {
		// use default system
		systemID = s.opts.defaultSystemID
	}
	if systemID == "" {
		return nil, status.Error(codes.InvalidArgument, "systemID is not found in the request and there is no default system")
	}

	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}
	adminClient := s.adminClients[systemID]

	s.logStatistics()

	// ensure no ambiguity if legacy vol
	err := s.checkVolumesMap(csiVolID)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"checkVolumesMap for id: %s failed : %s", csiVolID, err.Error())
	}

	// Check for NVMe type
	isNVME := false
	_, hostType, err := s.getHostIDAndType(systemID, nodeID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound,
			"error getting host ID and type for nodeID %s: %s", nodeID, err.Error())
	}
	if hostType == NVMeTCP {
		isNVME = true
	}

	// Check for NFS protocol
	fsType := volumeContext[KeyFsType]
	isNFS := false
	if fsType == "nfs" {
		isNFS = true
	}
	if isNFS {
		fsID := getFilesystemIDFromCsiVolumeID(csiVolID)
		fs, err := s.getFilesystemByID(fsID, systemID)
		if err != nil {
			if strings.EqualFold(err.Error(), sioGatewayFileSystemNotFound) || strings.Contains(err.Error(), "must be a hexadecimal number") {
				return nil, status.Error(codes.NotFound, "volume not found")
			}
			return nil, status.Errorf(codes.Internal, "failure checking volume status before controller publish: %s", err.Error())
		}

		var ipAddresses []string

		ipAddresses, err = s.findNetworkInterfaceIPs()
		if err != nil || len(ipAddresses) == 0 {

			csmlog.Infof("ControllerPublish - No network interfaces found, trying to get SDC IPs")
			// get SDC IPs if Network Interface IPs not found
			ipAddresses, err = s.getSDCIPs(nodeID, systemID)
			if err != nil {
				return nil, status.Errorf(codes.NotFound, "%s", err.Error())
			} else if len(ipAddresses) == 0 {
				return nil, status.Errorf(codes.NotFound, "%s", "received empty sdcIPs")
			}
		}
		csmlog.Infof("ControllerPublish - ipAddresses %v", ipAddresses)

		externalAccess := s.opts.ExternalAccess
		publishContext["host"] = ipAddresses[0]

		// Export for NFS
		resp, err := s.exportFilesystem(ctx, req, adminClient, fs, ipAddresses, externalAccess, nodeID, publishContext, am)
		return resp, err
	}
	volID := getVolumeIDFromCsiVolumeID(csiVolID)
	vol, err := s.getVolByID(volID, systemID)
	if err != nil {
		if strings.EqualFold(err.Error(), sioGatewayVolumeNotFound) || strings.Contains(err.Error(), "must be a hexadecimal number") ||
			strings.Contains(err.Error(), "Invalid volume") {
			return nil, status.Error(codes.NotFound, "volume not found")
		}
		return nil, status.Errorf(codes.Internal, "failure checking volume status before controller publish: %s", err.Error())
	}
	csmlog.Infof("Found volume: %s with volume ID: %s", vol.Name, vol.ID)

	var publisher VolumePublisher
	if isNVME {
		publisher = &NVMePublisher{
			svc: s,
			vol: vol,
		}
	} else {
		publisher = &SDCPublisher{
			svc: s,
			vol: vol,
		}
	}

	return publisher.Publish(ctx, req, adminClient, systemID, csiVolID)
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
	nodeID string,
) error {
	csmlog.Infof("Setting QoS limits for volume %s, mapped to SDC %s", volumeName, sdcID)
	adminClient := s.adminClients[systemID]
	tgtVol := goscaleio.NewVolume(adminClient)
	volID := getVolumeIDFromCsiVolumeID(csiVolID)
	vol, err := s.getVolByID(volID, systemID)
	if err != nil {
		return status.Errorf(codes.NotFound, "volume %s was not found, error: %s", volID, err.Error())
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
		csmlog.Errorf("unpublishing volume since error in setting QoS parameters for volume: %s, error: %s", volumeName, err.Error())

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
func shouldAllowMultipleMappings(isBlock bool, accessMode *csi.VolumeCapability_AccessMode) (bool, error) {
	switch accessMode.Mode {
	case csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY:
		return true, nil
	case csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER:
		if isBlock {
			return true, nil
		}
		return false, errors.New("mount multinode multi-writer not allowed")
	case csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER:
		return false, errors.New("multinode single writer not supported")
	default:
		return false, nil
	}
}

func validateAccessType(
	am *csi.VolumeCapability_AccessMode,
	isBlock bool,
) error {
	if isBlock {
		switch am.Mode {
		case csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
			csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER,
			csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER,
			csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
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
	req *csi.ControllerUnpublishVolumeRequest,
) (*csi.ControllerUnpublishVolumeResponse, error) {
	// Validate and Extract IDs
	csiVolID := req.GetVolumeId()
	if csiVolID == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID is required")
	}

	systemID := s.getSystemIDFromCsiVolumeID(csiVolID)
	if systemID == "" {
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

	if err := s.checkVolumesMap(csiVolID); err != nil {
		return nil, status.Errorf(codes.Internal,
			"checkVolumesMap for id: %s failed: %s", csiVolID, err.Error())
	}

	nodeID := req.GetNodeId()
	if nodeID == "" {
		return nil, status.Error(codes.InvalidArgument, "Node ID is required")
	}

	csmlog.Infof("ControllerUnpublishVolume called for nodeID: %s", nodeID)

	adminClient := s.adminClients[systemID]
	isNFS := strings.Contains(csiVolID, "/")

	// Handle NFS Volumes
	if isNFS {
		fsID := getFilesystemIDFromCsiVolumeID(csiVolID)
		fs, err := s.getFilesystemByID(fsID, systemID)
		if err != nil {
			if strings.EqualFold(err.Error(), sioGatewayFileSystemNotFound) ||
				strings.Contains(err.Error(), "must be a hexadecimal number") {
				return nil, status.Error(codes.NotFound, "volume not found")
			}
			return nil, status.Errorf(codes.Internal,
				"failure checking volume status before controller unpublish: %s", err.Error())
		}

		ipAddresses, err := s.findNetworkInterfaceIPs()
		if err != nil || len(ipAddresses) == 0 {
			csmlog.Infof("No network interfaces found, trying to get SDC IPs")
			ipAddresses, err = s.getSDCIPs(nodeID, systemID)
			if err != nil {
				return nil, status.Errorf(codes.NotFound, "%s", err.Error())
			} else if len(ipAddresses) == 0 {
				return nil, status.Errorf(codes.NotFound, "received empty SDC IPs")
			}
		}

		csmlog.Infof("SDC IP addresses: %v", ipAddresses)

		if err := s.unexportFilesystem(ctx, req, adminClient, fs, csiVolID, ipAddresses, nodeID); err != nil {
			return nil, err
		}
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	// Handle Block Volumes
	volID := getVolumeIDFromCsiVolumeID(csiVolID)
	vol, err := s.getVolByID(volID, systemID)
	if err != nil {
		if strings.EqualFold(err.Error(), sioGatewayVolumeNotFound) {
			csmlog.Debugf("volume %s is already deleted", volID)
			return &csi.ControllerUnpublishVolumeResponse{}, nil
		}
		return nil, status.Errorf(codes.Internal,
			"failure checking volume status before controller unpublish: %s", err.Error())
	}

	// Check Mapping and Protocol
	var (
		protocol     string
		mappedToNode bool
	)

	hostID, hostType, err := s.getHostIDAndType(systemID, nodeID)
	if err != nil || hostID == "" {
		return nil, status.Errorf(codes.Internal,
			"error getting host ID for nodeID %s: %s", nodeID, err.Error())
	}

	for _, mapping := range vol.MappedSdcInfo {
		csmlog.Debugf("Checking mapping for nodeID: %s", nodeID)
		csmlog.Debugf("Mapping HostType: %s, Host ID: %s, Host Name: %s", mapping.HostType, mapping.SdcID, mapping.SdcName)

		if mapping.HostType == "SdcHost" {
			if mapping.SdcID == hostID {
				protocol = SDC
				mappedToNode = true
				break
			}
		} else if mapping.HostType == "NVMeHost" {
			if mapping.SdcID == hostID {
				protocol = NVMeTCP
				mappedToNode = true
				break
			}
		} else if mapping.HostType == "" {
			// PowerFlex 3.6 does not populate HostType in MappedSdcInfo.
			// Fall back to the host type determined by getHostIDAndType.
			if mapping.SdcID == hostID {
				csmlog.Debugf("Mapping has empty HostType for host ID %s; using hostType %s from getHostIDAndType", hostID, hostType)
				protocol = hostType
				mappedToNode = true
				break
			}
		}
	}

	if !mappedToNode {
		csmlog.Debug("Volume already unpublished")
		return &csi.ControllerUnpublishVolumeResponse{}, nil
	}

	// Unmap Volume
	targetVolume := goscaleio.NewVolume(adminClient)
	targetVolume.Volume = vol

	switch protocol {
	case SDC:
		unmapVolumeSdcParam := &siotypes.UnmapVolumeSdcParam{
			SdcID:   hostID,
			AllSdcs: "",
		}
		if err := targetVolume.UnmapVolumeSdc(unmapVolumeSdcParam); err != nil {
			return nil, status.Errorf(codes.Internal,
				"Error unmapping volume from SDC node: %s", err.Error())
		}
	case NVMeTCP:
		unmapVolumeNVMeParam := &siotypes.UnmapVolumeNVMeParam{
			HostID:   hostID,
			AllHosts: "",
		}
		if err := targetVolume.RemoveMappedHost(unmapVolumeNVMeParam); err != nil {
			return nil, status.Errorf(codes.Internal,
				"Error unmapping volume from NVMe host: %s", err.Error())
		}
	}

	return &csi.ControllerUnpublishVolumeResponse{}, nil
}

func (s *service) ValidateVolumeCapabilities(
	ctx context.Context,
	req *csi.ValidateVolumeCapabilitiesRequest) (
	*csi.ValidateVolumeCapabilitiesResponse, error,
) {
	csiVolID := req.GetVolumeId()
	if csiVolID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"volume ID is required")
	}
	// ensure no ambiguity if legacy vol
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
	_, err = s.getVolByID(volID, systemID)
	if err != nil {
		if strings.EqualFold(err.Error(), sioGatewayVolumeNotFound) || strings.Contains(err.Error(), "must be a hexadecimal number") ||
			strings.Contains(err.Error(), "Invalid volume") {
			return nil, status.Error(codes.NotFound,
				"volume not found")
		}
		return nil, status.Errorf(codes.Internal,
			"failure checking volume status for capabilities: %s",
			err.Error())
	}

	vcs := req.GetVolumeCapabilities()
	supported, reason := valVolumeCaps(vcs)

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
) (bool, string) {
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
			// This is to guard against new access modes not understood
			supported = false
			reason = errUnknownAccessMode
		}
	}

	return supported, reason
}

func (s *service) ListVolumes(
	ctx context.Context,
	req *csi.ListVolumesRequest) (
	*csi.ListVolumesResponse, error,
) {
	csmlog.Infof("ListVolumes called")

	// Validate and normalize request pagination before fetching any array data.
	var (
		startToken int
		maxEntries = int(req.MaxEntries)
	)
	if v := req.StartingToken; v != "" {
		i, err := strconv.ParseInt(v, 10, 32)
		if err != nil {
			return nil, status.Errorf(
				codes.Aborted,
				"Unable to parse StartingToken: %v into int32, err: %v",
				req.StartingToken, err)
		}
		startToken = int(i)
		if startToken < 0 {
			return nil, status.Errorf(
				codes.Aborted,
				"StartingToken cannot be negative: %d",
				startToken)
		}
	}
	if maxEntries < 0 {
		return nil, status.Error(
			codes.InvalidArgument,
			"MaxEntries cannot be negative")
	}
	if maxEntries == 0 || maxEntries > MaxVolumeListEntries {
		maxEntries = MaxVolumeListEntries
	}

	// Collect all volumes from every configured system so the response
	// contains entries from every array instead of only the last one.
	var allEntries []*csi.ListVolumesResponse_Entry

	// Stable iteration order keeps pagination tokens valid across calls.
	systemIDs := make([]string, 0, len(s.opts.arrays))
	for id := range s.opts.arrays {
		systemIDs = append(systemIDs, id)
	}
	sort.Strings(systemIDs)

	for _, id := range systemIDs {
		arr := s.opts.arrays[id]
		systemID := arr.SystemID

		if systemID == "" {
			csmlog.Infof("SystemID is empty in controller array configuration")
			return nil, status.Error(codes.InvalidArgument, "There is no SystemID in controller array configuration")
		}

		if err := s.requireProbe(ctx, systemID); err != nil {
			csmlog.Warnf("Could not probe system: %s", systemID)
			continue
		}

		// Retrieve every volume/snapshot for this system. We call listVolumes
		// with startToken=0 so the result contains all entries for the system;
		// global pagination is applied after the loop. The per-system cache is
		// intentionally not used here: it only holds one system at a time and
		// would be unsafe to reuse across multiple arrays in this loop.
		source, _, err := s.listVolumes(systemID, 0, 0, true, s.opts.EnableListVolumesSnapshots, "", "")
		if err != nil {
			return nil, err
		}

		for _, vol := range source {
			if vol == nil {
				csmlog.Infof("Volume is nil in ListVolumeResponse from system %s", systemID)
				continue
			}
			allEntries = append(allEntries, &csi.ListVolumesResponse_Entry{
				Volume: s.getCSIVolume(vol, systemID),
			})
		}
	}

	// Apply the request pagination against the combined list.
	if startToken > len(allEntries) {
		return nil, status.Errorf(
			codes.Aborted,
			"startingToken=%d > len(entries)=%d",
			startToken, len(allEntries))
	}

	rem := len(allEntries) - startToken
	if maxEntries > rem {
		maxEntries = rem
	}

	nextToken := startToken + maxEntries
	nextTokenStr := ""
	if nextToken < len(allEntries) {
		nextTokenStr = fmt.Sprintf("%d", nextToken)
	}

	return &csi.ListVolumesResponse{
		Entries:   allEntries[startToken : startToken+maxEntries],
		NextToken: nextTokenStr,
	}, nil
}

func (s *service) ListSnapshots(
	ctx context.Context,
	req *csi.ListSnapshotsRequest) (
	*csi.ListSnapshotsResponse, error,
) {
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
				"Unable to parse StartingToken: %v into int32, err: %v",
				req.StartingToken, err)
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
		csmlog.Infof("Could not probe system: %s", systemID)
		code := status.Code(err)
		if code == codes.NotFound {
			return &csi.ListSnapshotsResponse{}, nil
		}

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

	// Process the source volumes and make CSI Snapshots
	entries := make([]*csi.ListSnapshotsResponse_Entry, 0, len(source))
	for _, vol := range source {
		if vol == nil {
			csmlog.Infof("Volume is nil in ListSnapshotsResponse from system %s", systemID)
			continue
		}
		entries = append(entries, &csi.ListSnapshotsResponse_Entry{
			Snapshot: s.getCSISnapshot(vol, systemID),
		})
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
	[]*siotypes.Volume, string, error,
) {
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
			csmlog.Infof("volume cache hit: %d volumes", len(s.volCache))
			func() {
				s.volCacheRWL.Lock()
				defer s.volCacheRWL.Unlock()
				// Check if cache has volumes for the required systemID
				if s.volCacheSystemID == systemID {
					sioVols = make([]*siotypes.Volume, len(s.volCache))
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
			csmlog.Infof("snap cache hit: %d snapshots", len(s.snapCache))
			func() {
				s.snapCacheRWL.Lock()
				defer s.snapCacheRWL.Unlock()
				// Check if cache has snapshots for the required systemID
				if s.snapCacheSystemID == systemID {
					sioSnaps = make([]*siotypes.Volume, len(s.snapCache))
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

// Gets capacity for all systems known to controller.
// When storage pool name is provided, gets capacity of this storage pool name from all systems
func (s *service) getCapacityForAllSystems(ctx context.Context, protectionDomain string, spName ...string) (int64, error) {
	var capacity int64

	for _, array := range s.opts.arrays {
		var systemCapacity int64
		var err error

		if len(spName) > 0 && spName[0] != "" {
			systemCapacity, err = s.getSystemCapacity(ctx, array.SystemID, protectionDomain, spName[0])
		} else {
			systemCapacity, err = s.getSystemCapacity(ctx, array.SystemID, "")
		}

		if err != nil {
			return 0, status.Errorf(codes.Internal,
				"unable to get system stats for system: %s, err: %s", array.SystemID, err.Error())
		}

		capacity += systemCapacity
	}

	return capacity, nil
}

// maxVolumesSizeForArray - store the maxVolumesSizeForArray
var maxVolumesSizeForArray = make(map[string]int64)

var mutex = &sync.Mutex{}

func (s *service) GetCapacity(
	ctx context.Context,
	req *csi.GetCapacityRequest) (
	*csi.GetCapacityResponse, error,
) {
	var (
		capacity int64
		err      error
	)

	systemID := ""
	params := req.GetParameters()
	if len(params) == 0 {
		// Get capacity of all systems
		capacity, err = s.getCapacityForAllSystems(ctx, "")
	} else {
		spname := params[KeyStoragePool]
		pd, ok := params[KeyProtectionDomain]
		if !ok {
			csmlog.Infof("Protection Domain name not provided; there could be conflicts if two storage pools share a name")
		}
		for key, value := range params {
			if strings.EqualFold(key, KeySystemID) {
				systemID = value
				break
			}
		}

		// If using availability zones, get capacity for the system in the zone
		// using accessible topology parameter from k8s.
		if s.opts.zoneLabelKey != "" {
			systemID, err = s.getSystemIDFromZoneLabelKey(req)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "%s", err.Error())
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

	if systemID == "" && s.opts.defaultSystemID != "" {
		systemID = s.opts.defaultSystemID
	}

	if systemID == "" {
		return &csi.GetCapacityResponse{
			AvailableCapacity: capacity,
		}, nil
	}

	maxVolSize, err := s.getMaximumVolumeSize(systemID)
	if err != nil {
		csmlog.Debugf("GetMaxVolumeSize returning error: %v", err)
	}

	if maxVolSize < 0 {
		return &csi.GetCapacityResponse{
			AvailableCapacity: capacity,
		}, nil
	}

	maxVolSizeinBytes := maxVolSize * bytesInGiB
	maxVol := wrapperspb.Int64(maxVolSizeinBytes)
	return &csi.GetCapacityResponse{
		AvailableCapacity: capacity,
		MaximumVolumeSize: maxVol,
	}, nil
}

// getSystemIDFromZoneLabelKey returns the system ID associated with the zoneLabelKey if zoneLabelKey is set and
// contains an associated zone name. Returns an empty string otherwise.
func (s *service) getSystemIDFromZoneLabelKey(req *csi.GetCapacityRequest) (systemID string, err error) {
	if req.AccessibleTopology == nil {
		return "", nil
	}
	zoneName, ok := req.AccessibleTopology.Segments[s.opts.zoneLabelKey]
	if !ok {
		csmlog.Infof("could not get availability zone from accessible topology. Getting capacity for all systems")
		return "", nil
	}

	// find the systemID with the matching zone name
	for _, array := range s.opts.arrays {
		if array.isInZone(zoneName) {
			systemID = array.SystemID
			break
		}
	}
	if systemID == "" {
		return "", fmt.Errorf("could not find an array assigned to zone '%s'; "+
			"if zones are configured on some systems, all systems must have zones configured — "+
			"mixing zoned and non-zoned systems on the same topology key is not supported", zoneName)
	}
	return systemID, nil
}

func (s *service) getMaximumVolumeSize(systemID string) (int64, error) {
	valueInCache, found := getCachedMaximumVolumeSize(systemID)
	if !found || valueInCache < 0 {
		adminClient := s.adminClients[systemID]
		if adminClient == nil {
			return 0, status.Errorf(codes.InvalidArgument, "can't find adminClient by id %s", systemID)
		}

		vol1, err := adminClient.GetMaxVol()
		if err != nil {
			csmlog.Debugf("GetMaxVolumeSize returning error: %v ", err)
			return 0, err
		}

		value, err := strconv.ParseInt(vol1, 10, 64)
		if err != nil {
			csmlog.Debugf("error converting str to int: %v ", err)
			return 0, err

		}

		cacheMaximumVolumeSize(systemID, value)
		valueInCache = value

	}
	return valueInCache, nil
}

func getCachedMaximumVolumeSize(key string) (int64, bool) {
	mutex.Lock()
	defer mutex.Unlock()

	value, found := maxVolumesSizeForArray[key]
	return value, found
}

func cacheMaximumVolumeSize(key string, value int64) {
	mutex.Lock()
	defer mutex.Unlock()

	maxVolumesSizeForArray[key] = value
}

func (s *service) ControllerGetCapabilities(
	_ context.Context,
	_ *csi.ControllerGetCapabilitiesRequest) (
	*csi.ControllerGetCapabilitiesResponse, error,
) {
	capabilities := []*csi.ControllerServiceCapability{
		{ // Required for Create/Delete Volume
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
				},
			},
		},
		{ // Required for ControllerPublish and ControllerUnpublish Volume
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME,
				},
			},
		},
		{ // Required for GetCapacity
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_GET_CAPACITY,
				},
			},
		},
		{ // Required for CreateSnapshot and DeleteSnapshot
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
				},
			},
		},
		{ // Required for ListSnapshots
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS,
				},
			},
		},
		{ // Required for ControllerExpandVolume
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
				},
			},
		},
		{ // Required for Clone
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_CLONE_VOLUME,
				},
			},
		},
		{ // Indicates PowerFlex supports SINGLE_NODE_SINGLE_WRITER and/or SINGLE_NODE_MULTI_WRITER access modes
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_SINGLE_NODE_MULTI_WRITER,
				},
			},
		},
		{ // Required for ControllerModifyVolume (CSI 1.12 VolumeAttributesClass)
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_MODIFY_VOLUME,
				},
			},
		},
	}

	healthMonitorCapabilities := []*csi.ControllerServiceCapability{
		{
			// Required for health monitor, optional if Health monitor is disabled
			// Indicates driver can report on volume condition in controller plugin
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_VOLUME_CONDITION,
				},
			},
		},
		{
			// Required for ControllerGetVolume which is only required if health monitor is enabled
			// Optional if ListVolumes capabilty is also being returned
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_GET_VOLUME,
				},
			},
		},
		{
			// Required for ListVolumes
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
				},
			},
		},
	}
	ListVolumesCapability := csi.ControllerServiceCapability{
		// Optional capability only returned when health monitor is not enabled. This is so health monitor will use GetVolume call instead of ListVolumes
		Type: &csi.ControllerServiceCapability_Rpc{
			Rpc: &csi.ControllerServiceCapability_RPC{
				Type: csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
			},
		},
	}

	if s.opts.IsHealthMonitorEnabled {
		capabilities = append(capabilities, healthMonitorCapabilities...)
	} else {
		capabilities = append(capabilities, &ListVolumesCapability)
	}

	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: capabilities,
	}, nil
}

func (s *service) getZoneFromZoneLabelKey(ctx context.Context, zoneLabelKey string) (zone string, err error) {
	// get labels for this service, s
	labels, err := GetNodeLabels(ctx, s)
	if err != nil {
		return "", err
	}

	csmlog.WithContext(ctx).Infof("Listing labels: %v", labels)

	// get the zone name from the labels
	if val, ok := labels[zoneLabelKey]; ok {
		return val, nil
	}

	return "", fmt.Errorf("label %s not found", zoneLabelKey)
}

// systemProbeAll will iterate through all arrays in service.opts.arrays and probe them. If failed, it logs
// the failed system name
func (s *service) systemProbeAll(ctx context.Context) error {
	// probe all arrays
	csmlog.WithContext(ctx).Infof("Probing all associated arrays")
	allArrayFail := true
	errMap := make(map[string]error)
	zoneName := ""
	usingZones := s.opts.zoneLabelKey != "" && s.isNodeMode()

	if usingZones {
		var err error
		zoneName, err = s.getZoneFromZoneLabelKey(ctx, s.opts.zoneLabelKey)
		if err != nil {
			// Node has no zone label — only non-zoned arrays will be probed
			csmlog.WithContext(ctx).Infof("node has no zone label (%v); will only probe arrays without zone configuration", err)
			zoneName = ""
		} else {
			csmlog.WithContext(ctx).Infof("probing zoneLabel '%s', zone value: '%s'", s.opts.zoneLabelKey, zoneName)
		}
	}

	newCtx, cancel := s.createProbeContextWithDeadline(ctx)
	defer cancel()

	for _, array := range s.opts.arrays {
		// If zone information is available, use it to probe the array
		if usingZones {
			if zoneName == "" {
				// Node has no zone label — skip arrays that have zone configuration
				if array.hasZoneConfig() {
					configuredZones := array.configuredZoneNames()
					csmlog.WithContext(ctx).Infof("array %s has zone config %v but node has no zone label, skipping", array.SystemID, configuredZones)
					errMap[array.SystemID] = fmt.Errorf("array %s has zone config %v but node has no zone label", array.SystemID, configuredZones)
					continue
				}
			} else if !array.isInZone(zoneName) {
				// Driver node containers should not probe arrays that exist outside their assigned zone
				// Driver controller container should probe all arrays
				configuredZones := array.configuredZoneNames()
				csmlog.WithContext(ctx).Infof("array %s zones %v does not match %s, not pinging this array", array.SystemID, configuredZones, zoneName)
				errMap[array.SystemID] = fmt.Errorf("array %s zones %v does not match %s, not pinging this array", array.SystemID, configuredZones, zoneName)
				continue
			}
		}

		err := s.systemProbe(newCtx, array)
		systemID := array.SystemID
		if err != nil {
			errMap[systemID] = err
			csmlog.WithContext(ctx).Errorf("array %s probe failed: %v", array.SystemID, err)
		} else {
			allArrayFail = false
			csmlog.WithContext(ctx).Infof("array %s probed successfully", systemID)
		}
	}

	csmlog.WithContext(ctx).Infof("[SystemProbeAll] Number of failed probes: %d", len(errMap))

	if allArrayFail {
		return status.Error(codes.FailedPrecondition,
			fmt.Sprintf("All arrays are not working. Could not proceed further: %v", errMap))
	}

	return nil
}

// ExtractIP extracts the IP address from the provided endpoint URL.
func ExtractIP(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}

	host := u.Hostname() // strips port if present
	ip := net.ParseIP(host)
	if ip == nil {
		return "", fmt.Errorf("not a valid IP: %s", host)
	}

	return ip.String(), nil
}

// ExtractHost returns the hostname or IP address from endpoint, stripping any port.
// Unlike ExtractIP, it accepts both IP addresses and hostnames.
func ExtractHost(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("no host in endpoint: %s", endpoint)
	}
	return host, nil
}

func oidcPrechecks(array *ArrayConnectionData) error {
	if array.OidcClientID == "" {
		return status.Error(codes.FailedPrecondition, "missing OidcClientID")
	}
	if array.OidcClientSecret == "" {
		return status.Error(codes.FailedPrecondition, "missing OidcClientSecret")
	}
	if array.CiamClientID == "" {
		return status.Error(codes.FailedPrecondition, "missing CiamClientID")
	}
	if array.CiamClientSecret == "" {
		return status.Error(codes.FailedPrecondition, "missing CiamClientSecret")
	}
	if array.Issuer == "" {
		return status.Error(codes.FailedPrecondition, "missing Issuer")
	}

	return nil
}

// ParseScopes parses a comma-separated string of scopes into a slice of unique scope strings.
func ParseScopes(scopesCSV string) []string {
	csv := strings.TrimSpace(scopesCSV)
	if csv == "" {
		return nil
	}

	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{})

	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

// systemProbe will probe the given array
func (s *service) systemProbe(ctx context.Context, array *ArrayConnectionData) error {
	lock := s.getProbeLock(array.SystemID)
	csmlog.WithContext(ctx).Debugf("[systemProbe] Waiting for lock for systemID=%s", array.SystemID)
	lock.Lock()
	defer lock.Unlock()
	csmlog.WithContext(ctx).Debugf("[systemProbe] Acquired lock for systemID=%s, starting probe", array.SystemID)

	// Check that we have the details needed to login to the Gateway
	if array.Endpoint == "" {
		return status.Error(codes.FailedPrecondition,
			"missing PowerFlex Gateway endpoint")
	}
	if array.Username == "" {
		return status.Error(codes.FailedPrecondition,
			"missing PowerFlex MDM user")
	}
	if array.Password == "" {
		return status.Error(codes.FailedPrecondition,
			"missing PowerFlex MDM password")
	}
	if array.SystemID == "" {
		return status.Error(codes.FailedPrecondition,
			"missing PowerFlex system name")
	}
	var altSystemNames []string
	if array.AllSystemNames != "" {
		altSystemNames = strings.Split(array.AllSystemNames, ",")
	}

	systemID := array.SystemID

	// Create ScaleIO API client if needed
	if s.adminClients[systemID] == nil {
		skipCertificateValidation := array.SkipCertificateValidation || array.Insecure
		client, err := goscaleio.NewClientWithArgs(array.Endpoint, "", math.MaxInt64, skipCertificateValidation, !s.opts.DisableCerts, "")
		if err != nil {
			return status.Errorf(codes.FailedPrecondition,
				"unable to create ScaleIO client: %s", err.Error())
		}

		client.SetCustomHTTPHeaders(http.Header{
			"Application-Type": {fmt.Sprintf("%s/%s", VerboseName, ManifestSemver)},
		})

		s.adminClients[systemID] = client
		for _, name := range altSystemNames {
			s.adminClients[name] = client
		}
	}

	csmlog.WithContext(ctx).Infof("Login to PowerFlex Gateway, system=%s, endpoint=%s, user=%s\n", systemID, array.Endpoint, array.Username)

	client := s.adminClients[systemID]
	if client.GetToken() == "" {
		if s.opts.AuthType == "OIDC" {
			csmlog.WithContext(ctx).Debugf("Authentication via OIDC")

			err := oidcPrechecks(array)
			if err != nil {
				return status.Errorf(codes.FailedPrecondition,
					"OIDC prechecks failed: %s", err.Error())
			}

			pfmpIP, err := ExtractIP(array.Endpoint)
			if err != nil {
				return status.Errorf(codes.FailedPrecondition,
					"unable to extract endpoint IP: %s", err.Error())
			}

			_, err = client.WithContext(ctx).Authenticate(&goscaleio.ConfigConnect{
				Endpoint:         array.Endpoint,
				AuthType:         s.opts.AuthType,
				PfmpIP:           pfmpIP,
				CiamClientID:     array.CiamClientID,
				CiamClientSecret: array.CiamClientSecret,
				OidcClientID:     array.OidcClientID,
				OidcClientSecret: array.OidcClientSecret,
				Issuer:           array.Issuer,
				Insecure:         array.SkipCertificateValidation,
				Scopes:           ParseScopes(array.Scopes),
			})
			if err != nil {
				return status.Errorf(codes.FailedPrecondition,
					"unable to login to PowerFlex Gateway: %s", err.Error())
			}
		} else {
			csmlog.WithContext(ctx).Debugf("Basic Authentication")
			_, err := client.WithContext(ctx).Authenticate(&goscaleio.ConfigConnect{
				Endpoint: array.Endpoint,
				Username: array.Username,
				Password: array.Password,
				Insecure: array.SkipCertificateValidation,
			})
			if err != nil {
				return status.Errorf(codes.FailedPrecondition,
					"unable to login to PowerFlex Gateway: %s", err.Error())
			}
		}
	}

	// initialize system if needed
	if s.systems[systemID] == nil {
		system, err := client.WithContext(ctx).FindSystem(array.SystemID, array.SystemID, "")
		if err != nil {
			return status.Errorf(codes.FailedPrecondition,
				"unable to find matching PowerFlex system name: %s",
				err.Error())
		}

		s.systems[systemID] = system
		if system.System != nil && system.System.Name != "" {
			csmlog.WithContext(ctx).Infof("Found Name for system=%s with ID=%s", system.System.Name, system.System.ID)
			s.connectedSystemNameToID[system.System.Name] = system.System.ID
			s.systems[system.System.ID] = system
			s.adminClients[system.System.ID] = client
		}
		// associate alternate system name to systemID
		for _, name := range altSystemNames {
			s.systems[name] = system
			s.adminClients[name] = client
			s.connectedSystemNameToID[name] = system.System.ID
		}
	}

	sysID := systemID
	if id, ok := s.connectedSystemNameToID[systemID]; ok {
		csmlog.WithContext(ctx).Infof("System with name %s found id: %s", systemID, id)
		sysID = id
		s.opts.arrays[sysID] = array
	}

	if array.IsDefault {
		csmlog.WithContext(ctx).Infof("default array is set to array ID: %s", sysID)
		s.opts.defaultSystemID = sysID
		csmlog.WithContext(ctx).Infof("%s is the default array, skipping VolumePrefixToSystems map update. \n", sysID)
	} else {
		err := s.UpdateVolumePrefixToSystemsMap(sysID)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *service) getProbeLock(systemID string) *sync.Mutex {
	actual, loaded := s.probeLocks.LoadOrStore(systemID, &sync.Mutex{})
	if loaded {
		csmlog.Debugf("[probeLock] Reusing existing lock for systemID=%s", systemID)
	} else {
		csmlog.Debugf("[probeLock] Created new lock for systemID=%s", systemID)
	}
	return actual.(*sync.Mutex)
}

func (s *service) requireProbe(ctx context.Context, systemID string) error {
	if s.adminClients[systemID] == nil || s.systems[systemID] == nil {
		mx.Lock()
		defer mx.Unlock()
		csmlog.WithContext(ctx).Debugf("probing system %s automatically", systemID)
		array, ok := s.opts.arrays[systemID]
		if ok {
			if err := s.systemProbe(ctx, array); err != nil {
				return status.Errorf(codes.FailedPrecondition,
					"failed to probe system: %s, error: %s", systemID, err.Error())
			}
		} else {
			return status.Errorf(codes.NotFound,
				"system %s is not configured in the driver", systemID)
		}
	}

	return nil
}

var createSnapshotFunc = func(system *goscaleio.System, snapParam *siotypes.CreateSnapshotParam) (*siotypes.SnapshotVolumesResp, error) {
	return system.CreateSnapshot(snapParam)
}

// CreateSnapshot creates a snapshot.
// If Parameters["VolumeIDList"] has a comma separated list of additional volumes, they will be
// snapshotted in a consistency group with the primary volume in CreateSnapshotRequest.SourceVolumeId.
func (s *service) CreateSnapshot(
	ctx context.Context,
	req *csi.CreateSnapshotRequest) (
	*csi.CreateSnapshotResponse, error,
) {
	// Validate snapshot volume
	csiVolID := req.GetSourceVolumeId()
	if csiVolID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "CSI volume ID to be snapped is required")
	}
	// ensure no ambiguity if legacy vol
	err := s.checkVolumesMap(csiVolID)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"checkVolumesMap for id: %s failed : %s", csiVolID, err.Error())
	}

	isNFS := strings.Contains(csiVolID, "/")

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
		csmlog.Infof("Requested name %s longer than 31 character max, truncated to %s\n", req.Name, name)
		req.Name = name
	}

	if req.Name == "" {
		return nil, status.Errorf(codes.InvalidArgument, "snapshot name cannot be Nil")
	}

	if isNFS {
		fileSystemID := getFilesystemIDFromCsiVolumeID(csiVolID)
		_, err := s.getFilesystemByID(fileSystemID, systemID)
		if err != nil {
			if strings.EqualFold(err.Error(), sioGatewayFileSystemNotFound) {
				return nil, status.Errorf(codes.NotFound, "NFS volume %s not found", fileSystemID)
			}
		}

		system, err := s.adminClients[systemID].FindSystem(systemID, "", "")
		if err != nil {
			return nil, err
		}

		existingSnap, err := system.GetFileSystemByIDName("", req.Name)

		if err == nil {
			if existingSnap.ParentID != fileSystemID {
				return nil, status.Errorf(codes.AlreadyExists,
					"snapshot with name '%s' exists, but SourceVolumeId %s doesn't match", req.Name, fileSystemID)
			}
			snapResponse := s.getCSISnapshotFromFileSystem(existingSnap, systemID)

			return &csi.CreateSnapshotResponse{Snapshot: snapResponse}, nil
		}

		resp, err := system.CreateFileSystemSnapshot(&siotypes.CreateFileSystemSnapshotParam{
			Name: req.Name,
		}, fileSystemID)
		if err != nil {
			return nil, status.Errorf(codes.Internal,
				"error creating snapshot with name %s for Volume ID %s", req.Name, fileSystemID)
		}

		newSnap, err := system.GetFileSystemByIDName(resp.ID, "")
		if err != nil {
			if strings.EqualFold(err.Error(), sioGatewayFileSystemNotFound) {
				return nil, status.Errorf(codes.NotFound, "snapshot with ID %s was not found", resp.ID)
			}
		}

		creationTime, _ := strconv.Atoi(newSnap.CreationTimestamp)

		creationTimeUnix := time.Unix(int64(creationTime), 0)
		creationTimeStamp := timestamppb.New(creationTimeUnix)
		slash := "/"
		csiSnapshotID := systemID + slash + newSnap.ID
		snapshot := &csi.Snapshot{
			SizeBytes:      int64(newSnap.SizeTotal),
			SnapshotId:     csiSnapshotID,
			SourceVolumeId: csiVolID, ReadyToUse: true,
			CreationTime: creationTimeStamp,
		}
		csiSnapResponse := &csi.CreateSnapshotResponse{Snapshot: snapshot}
		s.clearCache()

		csmlog.Infof("createSnapshot: SnapshotId %s SourceVolumeId %s CreationTime %s",
			snapshot.SnapshotId, snapshot.SourceVolumeId, snapshot.CreationTime.AsTime().Format(time.RFC3339Nano))
		return csiSnapResponse, nil

	}

	volID := getVolumeIDFromCsiVolumeID(csiVolID)

	// Check for idempotent request, i.e. the snapshot has been already created, by looking up the name.
	adminClient := s.adminClients[systemID]
	existingVols, err := getVolumeFunc(adminClient, "", "", "", req.Name, false)
	noVolErrString1 := "Error: problem finding volume: Volume not found"
	noVolErrString2 := "Error: problem finding volume: Could not find the volume"
	if (err != nil) && !(strings.Contains(err.Error(), noVolErrString1) || strings.Contains(err.Error(), noVolErrString2)) {
		csmlog.Infof("[CreateSnapshot] Idempotency check: GetVolume returned error: %s", err.Error())
		return nil, status.Errorf(codes.Internal, "Failed to create snapshot -- GetVolume returned unexpected error: %s", err.Error())
	}

	for _, vol := range existingVols {
		ancestor := vol.AncestorVolumeID
		csmlog.Infof("idempotent Name %s Name %s Ancestor %s id %s VTree %s pool %s\n",
			vol.Name, req.Name, ancestor, volID, vol.VTreeID, vol.StoragePoolID)
		if vol.Name == req.Name && vol.AncestorVolumeID == volID {
			// populate response structure
			csmlog.Infof("Idempotent request, snapshot id %s for source vol %s in system %s already exists\n", vol.ID, vol.AncestorVolumeID, systemID)
			snapshot := s.getCSISnapshot(vol, systemID)
			resp := &csi.CreateSnapshotResponse{Snapshot: snapshot}
			return resp, nil
		}
	}

	// Validate volume
	vol, err := getVolByIDFunc(s, volID, systemID)
	if err != nil {
		if strings.EqualFold(err.Error(), sioGatewayVolumeNotFound) {
			return nil, status.Errorf(codes.NotFound, "volume %s was not found", volID)
		}
		return nil, status.Errorf(codes.Internal,
			"failure checking volume status: %s", err.Error())
	}
	vtreeID := vol.VTreeID
	csmlog.Infof("vtree ID: %s\n", vtreeID)

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
			// neeed to trim space in case there are spaces inside VolumeIDList
			consistencyGroupSystem := strings.TrimSpace(s.getSystemIDFromCsiVolumeID(v))
			if consistencyGroupSystem != "" && consistencyGroupSystem != systemID {
				// system needs to be the same throughout snapshot consistency group, this is an error
				err = status.Errorf(codes.Internal, "Consistency group needs to be on the same system but vol %s is not on system: %s ", v, systemID)
				csmlog.Errorf("Consistency group needs to be on the same system but vol %s is not on system: %s ", v, systemID)
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

	// Create snapshot(s)
	snapResponse := &siotypes.SnapshotVolumesResp{}
	if vol.GenType == "EC" {
		snapParam := &siotypes.CreateSnapshotParam{SnapshotDefs: snapshotDefs}
		system := s.systems[systemID]
		snapResponse, err = createSnapshotFunc(system, snapParam)
	} else {
		snapParam := &siotypes.SnapshotVolumesParam{SnapshotDefs: snapshotDefs, AccessMode: "ReadOnly"}
		snapResponse, err = s.systems[systemID].CreateSnapshotConsistencyGroup(snapParam)
	}
	if err != nil {
		return nil, status.Errorf(codes.AlreadyExists, "Failed to create snapshot: %s", err.Error())
	}

	// populate response structure
	vol, err = s.getVolByID(volID, systemID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "volume %s was not found, error: %s", volID, err.Error())
	}
	creationTimeUnix := time.Unix(int64(vol.CreationTime), 0)
	creationTimeStamp := timestamppb.New(creationTimeUnix)
	dash := "-"
	csiSnapshotID := systemID + dash + snapResponse.VolumeIDList[0]
	snapshot := &csi.Snapshot{
		SizeBytes:      int64(vol.SizeInKb) * bytesInKiB,
		SnapshotId:     csiSnapshotID,
		SourceVolumeId: csiVolID, ReadyToUse: true,
		CreationTime: creationTimeStamp,
	}
	resp := &csi.CreateSnapshotResponse{Snapshot: snapshot}
	s.clearCache()

	csmlog.Infof("createSnapshot: SnapshotId %s SourceVolumeId %s CreationTime %s",
		snapshot.SnapshotId, snapshot.SourceVolumeId, snapshot.CreationTime.AsTime().Format(time.RFC3339Nano))
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
	if len(namebytes) > 31 {
		name = string(namebytes[0:31])
		csmlog.Infof("Requested name %s longer than 31 character max, truncated to %s\n", string(namebytes), name)
	}
	return name
}

func (s *service) DeleteSnapshot(
	ctx context.Context,
	req *csi.DeleteSnapshotRequest) (
	*csi.DeleteSnapshotResponse, error,
) {
	// Validate snapshot volume
	csiSnapID := req.GetSnapshotId()
	if csiSnapID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "snapshot ID to be deleted is required")
	}

	isNFS := strings.Contains(csiSnapID, "/")

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

	if isNFS {
		snapID := getFilesystemIDFromCsiVolumeID(csiSnapID)
		system, err := s.adminClients[systemID].FindSystem(systemID, "", "")
		if err != nil {
			return nil, fmt.Errorf("can't find system by id %s, error: %s", systemID, err.Error())
		}
		snap, err := s.getFilesystemByID(snapID, systemID)
		if err == nil {
			err = system.DeleteFileSystem(snap.Name)

			if err == nil {
				return &csi.DeleteSnapshotResponse{}, nil
			}
			if err != nil {
				if strings.Contains(err.Error(), sioGatewayFileSystemNotFound) || strings.Contains(err.Error(), "must be a hexadecimal number") {
					csmlog.Infof("Snapshot %s already deleted on system %s \n", snapID, systemID)
					return &csi.DeleteSnapshotResponse{}, nil
				}
				return nil, err
			}
		}
		if err != nil {
			if strings.Contains(err.Error(), sioGatewayFileSystemNotFound) || strings.Contains(err.Error(), "must be a hexadecimal number") {
				csmlog.Infof("Snapshot %s already deleted on system %s \n", snapID, systemID)
				return &csi.DeleteSnapshotResponse{}, nil
			}
			return nil, err
		}
	}

	snapID := getVolumeIDFromCsiVolumeID(csiSnapID)
	vol, err := s.getVolByID(snapID, systemID)
	if err != nil {
		if strings.Contains(err.Error(), "Could not find the volume") || strings.Contains(err.Error(), "must be a hexadecimal number") {
			csmlog.Infof("Snapshot %s already deleted on system %s \n", snapID, systemID)
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
	_ context.Context, snapVol *siotypes.Volume,
	_ *csi.DeleteSnapshotRequest, adminClient *goscaleio.Client) (
	*csi.DeleteSnapshotResponse, error,
) {
	cgVols := make([]*siotypes.Volume, 0)
	exposedVols := make([]string, 0)
	cgID := snapVol.ConsistencyGroupID
	//
	csmlog.Infof("Called DeleteSnapshotConsistencyGroup id: cg %s\n", cgID)

	// make call to cluster to get all volumes
	// Collect a list of the volumes in the same consistency group (cgVols)
	// Collect the names of volumes that are exposed.
	sioVols, err := adminClient.GetVolume("", "", "", "", true)
	for _, vol := range sioVols {
		if vol.ConsistencyGroupID == cgID {
			csmlog.Infof("Name %s CG %s ID %s", vol.Name, vol.ConsistencyGroupID, vol.ID)
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
		csmlog.Infof("Name %s CG %s ID %s", snapVol.Name, snapVol.ConsistencyGroupID, snapVol.ID)
		cgVols = append(cgVols, snapVol)
	}
	csmlog.Infof("CG Snapshots to be deleted: %v\n", cgVols)

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
	csmlog.WithContext(ctx).Infof("[ControllerExpandVolume] req: %+v", req)

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
	// ensure no ambiguity if legacy vol
	err = s.checkVolumesMap(csiVolID)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"checkVolumesMap for id: %s failed : %s", csiVolID, err.Error())
	}

	isNFS := strings.Contains(csiVolID, "/")

	if isNFS {
		fsID := getFilesystemIDFromCsiVolumeID(csiVolID)
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
		fs, err := s.getFilesystemByID(fsID, systemID)
		if err != nil {
			if strings.EqualFold(err.Error(), sioGatewayFileSystemNotFound) || strings.Contains(err.Error(), "must be a hexadecimal number") {
				return nil, status.Error(codes.NotFound,
					"volume not found")
			}
			return nil, status.Errorf(codes.Internal, "failure to load volume: %s", err.Error())
		}

		fsName := fs.Name
		cr := req.GetCapacityRange()
		csmlog.WithContext(ctx).Infof("cr:%+v", cr)
		requestedSize := int(cr.GetRequiredBytes())
		csmlog.WithContext(ctx).Infof("req.size:%d", requestedSize)
		csmlog.WithContext(ctx).Infof("Executing ExpandVolume: reqID=%s, fsName=%s, requestedSize=%d", reqID, fsName, requestedSize)

		allocatedSize := fs.SizeTotal
		csmlog.WithContext(ctx).Infof("allocatedsize:%d", allocatedSize)

		// nil response returned if volume shrink operation is tried
		if requestedSize < allocatedSize {
			csmlog.WithContext(ctx).Infof("volume shrink tried")
			return &csi.ControllerExpandVolumeResponse{}, nil
		}

		// idempotency check
		if requestedSize == allocatedSize {
			csmlog.WithContext(ctx).Infof("Idempotent call detected for volume (%s) with requested size (%d) SizeInKb and allocated size (%d) SizeInKb",
				fsName, requestedSize, allocatedSize)
			return &csi.ControllerExpandVolumeResponse{
				CapacityBytes:         int64(requestedSize),
				NodeExpansionRequired: false,
			}, nil
		}

		system, err := s.adminClients[systemID].FindSystem(systemID, "", "")
		if err != nil {
			return nil, err
		}

		if err := system.ModifyFileSystem(&siotypes.FSModify{Size: requestedSize}, fsID); err != nil {
			csmlog.WithContext(ctx).Errorf("NFS volume expansion failed with error: %s", err.Error())
			return nil, status.Error(codes.Internal, err.Error())
		}

		// update tree quota hard limit and soft limit if pvc size has changed

		isQuotaEnabled := s.opts.IsQuotaEnabled
		if isQuotaEnabled && fs.IsQuotaEnabled {
			treeQuota, err := system.GetTreeQuotaByFSID(fsID)
			if err != nil {
				csmlog.WithContext(ctx).Errorf("Fetching tree quota for NFS volume failed, error: %s", err.Error())
				return nil, status.Error(codes.Internal, err.Error())
			}

			// Modify Tree Quota
			updatedSoftLimit := treeQuota.SoftLimit * (requestedSize / treeQuota.HardLimit)
			treeQuotaID := treeQuota.ID
			csmlog.WithContext(ctx).Infof("Modifying tree quota ID %s for NFS volume ID: %s", treeQuotaID, fsID)
			quotaModify := &siotypes.TreeQuotaModify{
				HardLimit: requestedSize,
				SoftLimit: updatedSoftLimit,
			}

			err = system.ModifyTreeQuota(quotaModify, treeQuotaID)
			if err != nil {
				csmlog.WithContext(ctx).Errorf("Modifying tree quota for NFS volume failed, error: %s", err.Error())
				return nil, status.Error(codes.Internal, err.Error())
			}
			csmlog.WithContext(ctx).Infof("Tree quota modified successfully.")
		}

		csiResp := &csi.ControllerExpandVolumeResponse{
			CapacityBytes:         int64(requestedSize),
			NodeExpansionRequired: false,
		}
		return csiResp, nil
	}

	// Determine if node expansion is required based on volume capability.
	// Per CSI spec, if the volume is used as a raw block device, the SP MAY set
	// node_expansion_required to false to skip NodeExpandVolume on the node.
	nodeExpansionRequired := true
	if volCap := req.GetVolumeCapability(); volCap != nil && volCap.GetBlock() != nil {
		csmlog.WithContext(ctx).Info("Volume capability is raw block; setting NodeExpansionRequired to false")
		nodeExpansionRequired = false
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

	// Fetch genType from cached PlatformInfo to apply correct granularity (FR-2).
	platformInfo, err := s.GetPlatformInfo(systemID)
	if err != nil {
		return nil, err
	}

	// abort immediately on unrecognised genType so a future array generation
	// cannot silently apply wrong rounding granularity.
	if !isKnownGenType(platformInfo.GenType) {
		csmlog.WithContext(ctx).Warnf("Unrecognized genType value %q for array-id=%s; known identifiers: [EC]. Volume operation aborted.", platformInfo.GenType, systemID)
		s.granularityMetrics.IncDetectionError()
		return nil, status.Errorf(codes.Internal,
			"unrecognised array generation type %q for system %s; cannot determine volume size granularity",
			platformInfo.GenType, systemID)
	}

	volName := vol.Name
	cr := req.GetCapacityRange()
	csmlog.WithContext(ctx).Infof("cr:%+v", cr)
	requestedSize, err := validateVolSize(cr, platformInfo.GenType)
	if err != nil {
		return nil, err
	}

	// FR-6: INFO log when size is rounded; DEBUG otherwise.
	expandOrigBytes := cr.GetRequiredBytes()
	expandRoundedBytes := requestedSize * bytesInKiB
	if expandOrigBytes != expandRoundedBytes {
		csmlog.WithContext(ctx).Infof("ControllerExpandVolume: size rounded from %d bytes to %d bytes (genType: %q, operation: expand)",
			expandOrigBytes, expandRoundedBytes, platformInfo.GenType)
	}

	// FR-5: emit K8s event when size was rounded up.
	// ControllerExpandVolumeRequest carries no PVC metadata, so we use the
	// PV/volume name with an empty namespace — the event is visible on the PV.
	if s.roundingEmitter != nil {
		s.roundingEmitter.EmitRounded(
			volName, "", /* namespace unavailable in ExpandVolume */
			expandOrigBytes, expandRoundedBytes, "expand", platformInfo.GenType)
	}

	// FR-7: increment Prometheus rounding metrics.
	s.granularityMetrics.IncRoundedMetrics("expand", expandOrigBytes, expandRoundedBytes)

	csmlog.WithContext(ctx).Infof("req.size:%d", requestedSize)
	fields := map[string]interface{}{
		"RequestID":     reqID,
		"VolumeName":    volName,
		"RequestedSize": requestedSize,
	}
	csmlog.WithContext(ctx).WithFields(fields).Info("Executing ControllerExpandVolume")
	allocatedSize := int64(vol.SizeInKb)
	csmlog.WithContext(ctx).Infof("allocatedsize:%d", allocatedSize)

	if requestedSize < allocatedSize {
		return &csi.ControllerExpandVolumeResponse{}, nil
	}

	if requestedSize == allocatedSize {
		csmlog.WithContext(ctx).Infof("Idempotent call detected for volume (%s) with requested size (%d) SizeInKb and allocated size (%d) SizeInKb",
			volName, requestedSize, allocatedSize)
		return &csi.ControllerExpandVolumeResponse{
			CapacityBytes:         expandRoundedBytes,
			NodeExpansionRequired: nodeExpansionRequired,
		}, nil
	}

	reqSize := requestedSize / kiBytesInGiB
	tgtVol := goscaleio.NewVolume(s.adminClients[systemID])
	tgtVol.Volume = vol
	err = tgtVol.SetVolumeSize(strconv.Itoa(int(reqSize)))
	if err != nil {
		csmlog.WithContext(ctx).Errorf("Failed to execute ExpandVolume() for volume %s (requested size: %d KiB, genType: %q) with error (%s)",
			volName, requestedSize, platformInfo.GenType, err.Error())
		return nil, status.Errorf(codes.Internal,
			"expand volume %s to %d KiB (genType %q) failed: %s",
			volName, requestedSize, platformInfo.GenType, err.Error())
	}

	// If volume is marked for replication, remove the replication pair first.
	if vol.VolumeReplicationState != "UnmarkedForReplication" {
		csmlog.WithContext(ctx).Infof("[ControllerExpandVolume] - vol: %+v", vol)
		err := s.expandReplicationPair(ctx, req, systemID, volID)
		if err != nil {
			return nil, status.Errorf(codes.Internal,
				"error expanding replication pair: %s", err.Error())
		}
	}

	// return the response with NodeExpansionRequired set based on volume capability;
	// raw block volumes do not need node expansion, filesystem volumes do
	csiResp := &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         expandRoundedBytes,
		NodeExpansionRequired: nodeExpansionRequired,
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
	volumeSource *csi.VolumeContentSource_VolumeSource, name string, sizeInKbytes int64, storagePool string,
) (*csi.CreateVolumeResponse, error) {
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
		return nil, status.Errorf(codes.NotFound, "Volume not found: %s, error: %s", volumeSource.VolumeId, err.Error())
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
		csmlog.Infof("[Clone] Idempotency check: GetVolume returned error: %s", err.Error())
		return nil, status.Errorf(codes.Internal, "Failed to create clone -- GetVolume returned unexpected error: %s", err.Error())
	}

	mergedParams := mergeStringMaps(req.GetParameters(), req.GetMutableParameters())
	for _, vol := range existingVols {
		if vol.Name == name && vol.StoragePoolID == srcVol.StoragePoolID {
			csmlog.Infof("Requested volume %s already exists", name)
			csiVolume := s.getCSIVolume(vol, systemID)
			csiVolume.ContentSource = req.GetVolumeContentSource()
			copyInterestingParameters(mergedParams, csiVolume.VolumeContext)
			csmlog.Infof("Requested volume (from clone) already exists %s (%s) storage pool %s",
				csiVolume.VolumeContext["Name"], csiVolume.VolumeId, csiVolume.VolumeContext["StoragePoolName"])
			return &csi.CreateVolumeResponse{Volume: csiVolume}, nil

		}
	}

	// Snapshot the source volumes
	snapshotDefs := make([]*siotypes.SnapshotDef, 0)
	snapDef := &siotypes.SnapshotDef{VolumeID: sourceVolID, SnapshotName: name}
	snapshotDefs = append(snapshotDefs, snapDef)

	snapResponse := &siotypes.SnapshotVolumesResp{}
	// Create snapshot
	system := s.systems[systemID]
	if srcVol.GenType == "EC" {
		snapParam := &siotypes.CreateSnapshotParam{SnapshotDefs: snapshotDefs}
		snapResponse, err = system.CreateThinClone(snapParam)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to call CreateThinClone to clone volume: %s", err.Error())
		}
	} else {
		snapParam := &siotypes.SnapshotVolumesParam{SnapshotDefs: snapshotDefs, AccessMode: "ReadWrite"}
		snapResponse, err = system.CreateSnapshotConsistencyGroup(snapParam)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to call CreateSnapshotConsistencyGroup to clone volume: %s", err.Error())
		}
	}

	if len(snapResponse.VolumeIDList) != 1 {
		return nil, status.Errorf(codes.Internal, "Expected volume ID to be returned but it was not")
	}

	// Retrieve created destination volume
	destID := snapResponse.VolumeIDList[0]
	destVol, err := s.getVolByID(destID, systemID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Could not retrieve created volume: %s, error: %s", destID, err.Error())
	}

	// Create a volume response and return it
	s.clearCache()
	csiVolume := s.getCSIVolume(destVol, systemID)
	csiVolume.ContentSource = req.GetVolumeContentSource()
	copyInterestingParameters(mergedParams, csiVolume.VolumeContext)

	csmlog.Infof("Volume (from volume clone) %s (%s) storage pool %s",
		csiVolume.VolumeContext["Name"], csiVolume.VolumeId, csiVolume.VolumeContext["storagePoolName"])

	return &csi.CreateVolumeResponse{Volume: csiVolume}, nil
}

// ControllerGetVolume fetch current information about a volume
// returns volume condition if found else returns not found
func (s *service) ControllerGetVolume(_ context.Context, req *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
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

func (s *service) CreateReplicationConsistencyGroup(systemID string, name string,
	rpo string, locatProtectionDomain string, remoteProtectionDomain string,
	peerMdmID string, remoteSystemID string,
) (*siotypes.ReplicationConsistencyGroupResp, error) {
	adminClient := s.adminClients[systemID]
	if adminClient == nil {
		return nil, fmt.Errorf("can't find adminClient by id %s", systemID)
	}

	if peerMdmID != "" && remoteSystemID != "" {
		return nil, fmt.Errorf("peerMdmID and remoteSystemID cannot both be present")
	}

	rcgPayload := &siotypes.ReplicationConsistencyGroupCreatePayload{
		Name:                     name,
		RpoInSeconds:             rpo,
		ProtectionDomainID:       locatProtectionDomain,
		RemoteProtectionDomainID: remoteProtectionDomain,
		PeerMdmID:                peerMdmID,
		DestinationSystemID:      remoteSystemID,
	}

	rcgResp, err := adminClient.CreateReplicationConsistencyGroup(rcgPayload)
	if err != nil {
		// Handle the case where it already exists.
		if !strings.EqualFold(err.Error(), sioReplicationGroupExists) {
			csmlog.Infof("Replication Creation Error: %s", err.Error())
			return nil, err
		}
	}

	var id string
	if rcgResp == nil {
		rcgs, err := adminClient.GetReplicationConsistencyGroups()
		if err != nil {
			return nil, err
		}

		// RCG already exists, find it on the array.
		for _, rcg := range rcgs {
			if rcg.Name == name && rcg.ProtectionDomainID == locatProtectionDomain && rcg.RemoteProtectionDomainID == remoteProtectionDomain {
				csmlog.Infof("Replication Group Found: %s, %s", rcg.ID, rcg.RemoteID)
				id = rcg.ID
				break
			}
		}

		if id == "" {
			return nil, status.Errorf(codes.Internal, "couldn't find replication consistency group")
		}
	} else {
		id = rcgResp.ID
	}

	return &siotypes.ReplicationConsistencyGroupResp{
		ID: id,
	}, nil
}

func (s *service) CreateReplicationPair(systemID string, name string,
	localVolumeID string, remoteVolumeID string, replicationGroupID string,
) (*siotypes.ReplicationPair, error) {
	adminClient := s.adminClients[systemID]
	if adminClient == nil {
		return nil, fmt.Errorf("can't find adminClient by id %s", systemID)
	}

	payload := &siotypes.QueryReplicationPair{
		Name:                          name,
		SourceVolumeID:                localVolumeID,
		DestinationVolumeID:           remoteVolumeID,
		ReplicationConsistencyGroupID: replicationGroupID,
		CopyType:                      "OnlineCopy",
	}

	response, err := adminClient.CreateReplicationPair(payload)
	if err != nil {
		// Handle the case where it already exists.
		if !strings.EqualFold(err.Error(), sioReplicationPairExists) {
			csmlog.Infof("Replication Pair Creation Error: %s", err.Error())
			return nil, err
		}
	}

	if response == nil {
		pairs, err := adminClient.GetAllReplicationPairs()
		if err != nil {
			return nil, err
		}

		for _, pair := range pairs {
			if pair.Name == name {
				csmlog.Infof("Replication Pair Found: %+v", pair)
				response = pair
				break
			}
		}

		if response == nil {
			return nil, status.Errorf(codes.Internal, "couldn't find replication pair")
		}
	}

	return response, nil
}

func (s *service) DeleteReplicationConsistencyGroup(systemID string, groupID string) error {
	adminClient := s.adminClients[systemID]
	if adminClient == nil {
		return status.Errorf(codes.InvalidArgument, "can't find adminClient by id %s", systemID)
	}

	if groupID == "" {
		return status.Errorf(codes.InvalidArgument, "group id wasn't provided")
	}

	group, err := adminClient.GetReplicationConsistencyGroupByID(groupID)
	if err != nil {
		csmlog.Infof("Replication Deletion Error: %s", err.Error())
		return err
	}

	rcg := goscaleio.NewReplicationConsistencyGroup(adminClient)
	rcg.ReplicationConsistencyGroup = group

	err = rcg.RemoveReplicationConsistencyGroup(false)

	return err
}

func (s *service) CreateReplicationConsistencyGroupSnapshot(client *goscaleio.Client, group *siotypes.ReplicationConsistencyGroup) (*siotypes.CreateReplicationConsistencyGroupSnapshotResp, error) {
	rcg := goscaleio.NewReplicationConsistencyGroup(client)
	rcg.ReplicationConsistencyGroup = group

	response, err := rcg.CreateReplicationConsistencyGroupSnapshot()
	if err != nil {
		return nil, err
	}

	return response, nil
}

func (s *service) ExecuteFailoverOnReplicationGroup(client *goscaleio.Client, group *siotypes.ReplicationConsistencyGroup) error {
	rcg := goscaleio.NewReplicationConsistencyGroup(client)
	rcg.ReplicationConsistencyGroup = group

	csmlog.Infof("[ExecuteFailoverOnReplicationGroup]: Executing Failover command")

	return rcg.ExecuteFailoverOnReplicationGroup()
}

func (s *service) ExecuteSwitchoverOnReplicationGroup(client *goscaleio.Client, group *siotypes.ReplicationConsistencyGroup) error {
	rcg := goscaleio.NewReplicationConsistencyGroup(client)
	rcg.ReplicationConsistencyGroup = group

	csmlog.Infof("[ExecuteSwitchoverOnReplicationGroup]: Executing Switchover (Unplanned Failover)")

	return rcg.ExecuteSwitchoverOnReplicationGroup(false)
}

func (s *service) ExecuteReverseOnReplicationGroup(client *goscaleio.Client, group *siotypes.ReplicationConsistencyGroup) error {
	rcg := goscaleio.NewReplicationConsistencyGroup(client)
	rcg.ReplicationConsistencyGroup = group

	csmlog.Infof("[ExecuteReverseOnReplicationGroup]: Executing Reverse (Reprotect Local)")

	return rcg.ExecuteReverseOnReplicationGroup()
}

func (s *service) ExecuteResumeOnReplicationGroup(client *goscaleio.Client, group *siotypes.ReplicationConsistencyGroup, failover bool) error {
	rcg := goscaleio.NewReplicationConsistencyGroup(client)
	rcg.ReplicationConsistencyGroup = group

	csmlog.Infof("[ExecuteReverseOnReplicationGroup]: Resuming Replication Group")

	if failover {
		csmlog.Infof("[ExecuteReverseOnReplicationGroup]: In Failover, Restoring...")
		return rcg.ExecuteRestoreOnReplicationGroup()
	}

	return rcg.ExecuteResumeOnReplicationGroup()
}

func (s *service) ExecutePauseOnReplicationGroup(client *goscaleio.Client, group *siotypes.ReplicationConsistencyGroup) error {
	rcg := goscaleio.NewReplicationConsistencyGroup(client)
	rcg.ReplicationConsistencyGroup = group

	csmlog.Infof("[ExecutePauseOnReplicationGroup]: Pause Replication Group")

	return rcg.ExecutePauseOnReplicationGroup()
}

func (s *service) ExecuteSyncOnReplicationGroup(client *goscaleio.Client, group *siotypes.ReplicationConsistencyGroup) (*siotypes.SynchronizationResponse, error) {
	rcg := goscaleio.NewReplicationConsistencyGroup(client)
	rcg.ReplicationConsistencyGroup = group

	csmlog.Infof("[ExecuteSyncOnReplicationGroup]: Executing SyncNow")

	return rcg.ExecuteSyncOnReplicationGroup()
}

func (s *service) verifySystem(systemID string) (*goscaleio.Client, error) {
	adminClient := s.adminClients[systemID]
	if adminClient == nil {
		return nil, fmt.Errorf("can't find adminClient by id %s", systemID)
	}

	return adminClient, nil
}

func (s *service) createProbeContextWithDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	defaultProbeDeadline := time.Now().Add(s.opts.probeTimeout)
	probeDeadline, ok := ctx.Deadline()
	if !ok {
		csmlog.WithContext(ctx).Infof("Probe deadline not in context, using default")
		probeDeadline = time.Now().Add(s.opts.probeTimeout)
	}

	// Set the deadline to be the lowest of the two times.
	if probeDeadline.After(defaultProbeDeadline) {
		csmlog.WithContext(ctx).Infof("Original Probe Deadline %s is greater than defaultProbeDeadline %s, setting to default", probeDeadline, defaultProbeDeadline)
		probeDeadline = defaultProbeDeadline
	}

	// Calculate the new deadline by subtracting the desired duration
	return context.WithDeadline(ctx, probeDeadline)
}

// supportedMutableParams defines the set of mutable parameters accepted by ControllerModifyVolume.
var supportedMutableParams = map[string]bool{
	"bandwidthLimitInKbps": true,
	"iopsLimit":            true,
}

// validateMutableParams checks that all mutable parameter keys are supported
// and their values are valid non-negative integers.
func validateMutableParams(params map[string]string) error {
	for key, val := range params {
		if !supportedMutableParams[key] {
			return status.Errorf(codes.InvalidArgument, "unsupported mutable parameter: %s. Supported keys: %v", key, supportedMutableParams)
		}
		if val == "" {
			return status.Errorf(codes.InvalidArgument, "invalid value for %s: value must not be empty", key)
		}
		v, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "invalid value for %s: %s is not a valid integer", key, val)
		}
		if v < 0 {
			return status.Errorf(codes.InvalidArgument, "invalid value for %s: %s must be a non-negative integer", key, val)
		}
	}
	return nil
}

func isBackendUnavailableError(err error) bool {
	if err == nil {
		return false
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	errMsg := strings.ToLower(err.Error())
	for _, indicator := range []string{
		"connection refused",
		"connection reset",
		"no such host",
		"timeout awaiting headers",
		"i/o timeout",
		"service unavailable",
		"bad gateway",
		"gateway timeout",
		"503",
		"504",
	} {
		if strings.Contains(errMsg, indicator) {
			return true
		}
	}

	return false
}

func mapControllerModifyVolumeBackendError(systemID, csiVolID string, err error) error {
	if isBackendUnavailableError(err) {
		return status.Errorf(codes.Unavailable,
			"PowerFlex API unreachable for system %s: %s. Retry is safe.",
			systemID, err.Error())
	}

	return status.Errorf(codes.Internal,
		"PowerFlex API error modifying volume %s: %s",
		csiVolID, err.Error())
}

// buildSdcLimitsParam builds a SetMappedSdcLimitsParam for the given SDC, merging
// requested values with current values for QoS preservation. Returns nil if
// the target values already match (idempotent).
func buildSdcLimitsParam(sdcInfo *siotypes.MappedSdcInfo, mutableParams map[string]string) *siotypes.SetMappedSdcLimitsParam {
	bwKbps := fmt.Sprintf("%d", sdcInfo.LimitBwInMbps*1024)
	if v, ok := mutableParams["bandwidthLimitInKbps"]; ok {
		bwKbps = v
	}
	iops := fmt.Sprintf("%d", sdcInfo.LimitIops)
	if v, ok := mutableParams["iopsLimit"]; ok {
		iops = v
	}
	// Idempotency check
	if bwKbps == fmt.Sprintf("%d", sdcInfo.LimitBwInMbps*1024) && iops == fmt.Sprintf("%d", sdcInfo.LimitIops) {
		return nil
	}
	return &siotypes.SetMappedSdcLimitsParam{
		SdcID:                sdcInfo.SdcID,
		BandwidthLimitInKbps: bwKbps,
		IopsLimit:            iops,
	}
}

// ControllerModifyVolume modifies mutable parameters (QoS: bandwidthLimitInKbps, iopsLimit)
// on an existing PowerFlex block volume. It validates the request, queries the volume's
// current MappedSdcInfo, preserves unspecified QoS fields, and applies limits to all
// mapped SDCs via the PowerFlex setMappedSdcLimits API.
func (s *service) ControllerModifyVolume(
	ctx context.Context,
	req *csi.ControllerModifyVolumeRequest,
) (*csi.ControllerModifyVolumeResponse, error) {
	csiVolID := req.GetVolumeId()
	if csiVolID == "" {
		return nil, status.Error(codes.InvalidArgument, "volume_id is required")
	}

	// Check NFS format early (cheap string operation) but reject after volume existence check
	isNFSVolume := strings.Contains(csiVolID, "/")

	// ensure no ambiguity if legacy vol
	if err := s.checkVolumesMap(csiVolID); err != nil {
		return nil, status.Errorf(codes.Internal,
			"checkVolumesMap for id: %s failed : %s", csiVolID, err.Error())
	}

	// Check if volume exists BEFORE validating mutable_parameters
	// This ensures we return NotFound for non-existent volumes
	volID := getVolumeIDFromCsiVolumeID(csiVolID)
	systemID := s.getSystemIDFromCsiVolumeID(csiVolID)
	if systemID == "" {
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
		if strings.EqualFold(err.Error(), sioGatewayVolumeNotFound) ||
			strings.Contains(err.Error(), "must be a hexadecimal number") {
			return nil, status.Errorf(codes.NotFound, "ControllerModifyVolume volume %s not found", csiVolID)
		}
		return nil, mapControllerModifyVolumeBackendError(systemID, csiVolID, err)
	}

	// Reject NFS volumes after confirming volume exists
	if isNFSVolume {
		return nil, status.Error(codes.InvalidArgument, "ControllerModifyVolume is not supported for NFS volumes")
	}

	// NOW validate mutable_parameters as the volume exists
	mutableParams := req.GetMutableParameters()
	// If no mutable parameters provided, return success (no modification needed)
	// This aligns with CSI spec where mutable_parameters is optional
	if len(mutableParams) == 0 {
		csmlog.Debugf("ControllerModifyVolume: no mutable parameters provided for volume %s, returning success", csiVolID)
		return &csi.ControllerModifyVolumeResponse{}, nil
	}

	if err := validateMutableParams(mutableParams); err != nil {
		return nil, err
	}

	if len(vol.MappedSdcInfo) == 0 {
		return nil, status.Errorf(codes.FailedPrecondition,
			"ControllerModifyVolume: volume %s has no mapped SDC attachments", csiVolID)
	}

	tgtVol := goscaleio.NewVolume(s.adminClients[systemID])
	tgtVol.Volume = vol

	for _, sdcInfo := range vol.MappedSdcInfo {
		params := buildSdcLimitsParam(sdcInfo, mutableParams)
		if params == nil {
			csmlog.Infof("ControllerModifyVolume: SDC %s already at target QoS, skipping", sdcInfo.SdcID)
			continue
		}
		csmlog.Infof("ControllerModifyVolume: setting QoS for volume %s SDC %s: bw=%s iops=%s",
			csiVolID, sdcInfo.SdcID, params.BandwidthLimitInKbps, params.IopsLimit)
		if err := tgtVol.SetMappedSdcLimits(params); err != nil {
			return nil, mapControllerModifyVolumeBackendError(systemID, csiVolID, err)
		}
	}

	csmlog.Infof("ControllerModifyVolume: successfully modified volume %s with parameters %v", csiVolID, mutableParams)
	return &csi.ControllerModifyVolumeResponse{}, nil
}

func (s *service) IsReplicationEnabledOnPlatforms(sourceSystemID, remoteSystemID, sourceGenType string) (bool, error) {
	// check replication supports on source and Target
	if remoteSystemID != "" {
		csmlog.Infof("Checking if Replication is Enabled on the Platform level. SourceSystemId: %s RemoteSystemId: %s", sourceSystemID, remoteSystemID)

		if s.isReplicationNotSupported(sourceGenType) {
			csmlog.Infof("Replication is not supported on this System %s with GenType: %s", sourceSystemID, sourceGenType)
			return false, status.Errorf(codes.InvalidArgument, "Replication is not supported on this System %s with GenType: %s", sourceSystemID, sourceGenType)
		}

		platformInfo, err := s.GetPlatformInfo(remoteSystemID)
		if err != nil {
			return false, err
		}

		if s.isReplicationNotSupported(platformInfo.GenType) {
			csmlog.Infof("Replication is not supported on this System %s with GenType: %s", remoteSystemID, platformInfo.GenType)
			return false, status.Errorf(codes.InvalidArgument, "Replication is not supported on this System %s with GenType: %s", remoteSystemID, platformInfo.GenType)
		}

		csmlog.Infof("Checked: Replication is Enabled on the Platform level. SourceSystemId: %s RemoteSystemId: %s", sourceSystemID, remoteSystemID)
	}

	return true, nil
}
