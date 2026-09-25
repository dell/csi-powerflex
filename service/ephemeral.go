// Copyright © 2021-2022 Dell Inc. or its subsidiaries. All Rights Reserved.
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
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	csmlog "github.com/Ecosystems/container-storage-modules/src/csmlog"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var ephemeralStagingMountPath = "/var/lib/kubelet/plugins/kubernetes.io/csi/pv/ephemeral/"

func (s *service) fileExist(filename string) bool {
	_, err := os.Stat(filename)
	csmlog.Debugf("Error stating file %s: %v", filename, err)
	if err != nil && os.IsNotExist(err) {
		return false
	}
	return true
}

func parseSize(size string) (int64, error) {
	pattern := `(\d*) ?Gi$`
	pathMetadata := regexp.MustCompile(pattern)

	matches := pathMetadata.FindStringSubmatch(size)
	for i, match := range matches {
		if i != 0 {
			bytes, err := strconv.ParseInt(match, 10, 64)
			if err != nil {
				return 0, errors.New("Failed to parse bytes")
			}
			return bytes * 1073741824, nil
		}
	}
	message := "failed to parse bytes for string: " + size
	return 0, errors.New(message)
}

// Call complete stack: systemProbe, CreateVolume, ControllerPublishVolume, and NodePublishVolume
func (s *service) ephemeralNodePublish(
	ctx context.Context,
	req *csi.NodePublishVolumeRequest,
) (*csi.NodePublishVolumeResponse, error) {
	_, err := os.Stat(ephemeralStagingMountPath)
	if err != nil {
		csmlog.WithContext(ctx).Warnf("Unable to stat staging path %s: %v", ephemeralStagingMountPath, err)
		if os.IsNotExist(err) {
			csmlog.Debug("path does not exist, will attempt to create it")
			err = os.MkdirAll(ephemeralStagingMountPath, 0o750)
			if err != nil {
				csmlog.Errorf("Unable to create dir %s: %v", ephemeralStagingMountPath, err)
				return nil, status.Error(codes.Internal, "Unable to create directory for mounting ephemeral volumes, error: "+err.Error())
			}
			csmlog.Debugf("dir created: %v", ephemeralStagingMountPath)
		}
	}

	volID := req.GetVolumeId()
	volName := req.VolumeContext["volumeName"]
	if len(volName) > 31 {
		csmlog.Errorf("Volume name: %s is over 32 characters, too long.", volName)
		return nil, status.Error(codes.Internal, "Volume name too long")
	}

	if volName == "" {
		csmlog.Errorf("Missing Parameter: volumeName must be specified in volume attributes section for ephemeral volumes")
		return nil, status.Error(codes.Internal, "Volume name not specified")
	}

	volSize, err := parseSize(req.VolumeContext["size"])
	if err != nil {
		csmlog.Errorf("Parse size failed %s", err.Error())
		return nil, status.Error(codes.Internal, "inline ephemeral parse size failed")
	}

	systemName := s.opts.defaultSystemID
	if req.VolumeContext["systemID"] != "" {
		csmlog.WithContext(ctx).Debug("Ignoring requested systemID for ephemeral volume; using configured default array")
	}

	array := s.opts.arrays[systemName]

	if array == nil {
		// to get inside this if block, req has name, but secret has ID, need to convert from name -> ID
		if id, ok := s.connectedSystemNameToID[systemName]; ok {
			// systemName was sent in req, but secret used ID. Change to ID.
			csmlog.Debugf("systemName set to id: %s", id)
			array = s.opts.arrays[id]
		} else {
			err = status.Errorf(codes.Internal, "systemID: %s not recgonized", systemName)
			csmlog.WithContext(ctx).Errorf("ephemeral publish failed: %v", err)
			return nil, err

		}
	}

	err = s.systemProbe(ctx, array)
	if err != nil {
		csmlog.WithContext(ctx).Errorf("ephemeral system probe failed: %v", err)
		return nil, status.Error(codes.Internal, "inline ephemeral system prob failed: "+err.Error())
	}

	sanitizedParams := sanitizeEphemeralCreateVolumeParams(req.VolumeContext)

	// Note: storagePool/storagepool are intentionally NOT sanitized because:
	// 1. storagepool is a required CreateVolume parameter
	// 2. ArrayConnectionData has no default pool field to inject
	// 3. systemID is locked to admin default, limiting pool selection
	//    to pools within the admin-configured default system only

	crvolresp, err := s.CreateVolume(ctx, &csi.CreateVolumeRequest{
		Name: volName,
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: volSize,
			LimitBytes:    0,
		},
		VolumeCapabilities: []*csi.VolumeCapability{req.VolumeCapability},
		Parameters:         sanitizedParams,
		Secrets:            req.Secrets,
	})
	if err != nil {
		csmlog.Errorf("CreateVolume Ephemeral %s", err.Error())
		return nil, status.Error(codes.Internal, "inline ephemeral create volume failed: "+err.Error())
	}

	csmlog.Infof("volume ID returned from CreateVolume is: %s ", crvolresp.Volume.VolumeId)
	volumeID := crvolresp.Volume.VolumeId

	// Create lockfile to map vol ID from request to volID returned by CreateVolume
	// will also be used to determine if volume is ephemeral in NodeUnpublish
	errLock := os.MkdirAll(filepath.Clean(filepath.Join(ephemeralStagingMountPath, volID)), 0o750)
	if errLock != nil {
		return nil, errLock
	}
	safePath := filepath.Join(ephemeralStagingMountPath, volID, "id")
	safePath = filepath.Clean(safePath)

	// in case systemName was not given with volume context
	systemName = s.getSystemIDFromCsiVolumeID(volumeID)

	if systemName == "" {
		csmlog.Errorf("getSystemIDFromCsiVolumeID was not able to determine systemName from VolumeID: %s", volumeID)
		return nil, status.Error(codes.Internal, "inline ephemeral getSystemIDFromCsiVolumeID failed ")
	}

	NodeID := s.opts.SdcGUID
	if s.useNVME {
		csmlog.Infof("SdcGUID is not set, using NodeID: %s", s.nodeID)
		NodeID = s.nodeID
	}

	cpubresp, err := s.ControllerPublishVolume(ctx, &csi.ControllerPublishVolumeRequest{
		NodeId:           NodeID,
		VolumeId:         volumeID,
		VolumeCapability: req.VolumeCapability,
		Readonly:         req.Readonly,
		Secrets:          req.Secrets,
		VolumeContext:    crvolresp.Volume.VolumeContext,
	})
	if err != nil {
		csmlog.Infof("Rolling back and calling unpublish ephemeral volumes with VolId %s", crvolresp.Volume.VolumeId)
		_, _ = s.NodeUnpublishVolume(ctx, &csi.NodeUnpublishVolumeRequest{
			VolumeId:   volID,
			TargetPath: req.TargetPath,
		})
		return nil, status.Error(codes.Internal, "inline ephemeral controller publish failed: "+err.Error())
	}
	if s.useNVME {
		csmlog.Debug("found NVME ephemeral volume")
		stageReq := &csi.NodeStageVolumeRequest{
			StagingTargetPath: filepath.Clean(filepath.Join(ephemeralStagingMountPath, volID)),
			VolumeId:          volumeID,
			VolumeCapability:  req.VolumeCapability,
			Secrets:           req.Secrets,
			VolumeContext:     crvolresp.Volume.VolumeContext,
		}
		_, err = s.NodeStageVolume(ctx, stageReq)
		if err != nil {
			_, _ = s.NodeUnpublishVolume(ctx, &csi.NodeUnpublishVolumeRequest{
				VolumeId:   volID,
				TargetPath: req.TargetPath,
			})
			return nil, status.Error(codes.Internal, "inline NVMe ephemeral node stage volume failed: "+err.Error())
		}
	}
	var f *os.File
	f, errLock = os.Create(safePath)
	if errLock != nil {
		return nil, errLock
	}
	csmlog.Debugf("Created lockfile during volume creation:%s", safePath)

	_, errLock = f.WriteString(volumeID)
	if errLock != nil {
		return nil, errLock
	}
	csmlog.Infof("lock-file contents written:%s", volumeID)

	defer func() {
		if err := f.Close(); err != nil {
			csmlog.Errorf("Error closing file %s: %v", safePath, err)
		}
	}()

	_, err = s.NodePublishVolume(ctx, &csi.NodePublishVolumeRequest{
		VolumeId:          volumeID,
		PublishContext:    cpubresp.PublishContext,
		StagingTargetPath: filepath.Clean(filepath.Join(ephemeralStagingMountPath, volID)),
		TargetPath:        req.TargetPath,
		VolumeCapability:  req.VolumeCapability,
		Readonly:          req.Readonly,
		Secrets:           req.Secrets,
		VolumeContext:     crvolresp.Volume.VolumeContext,
	})
	if err != nil {
		csmlog.Errorf("NodePublishErrEph %s", err.Error())
		_, _ = s.NodeUnpublishVolume(ctx, &csi.NodeUnpublishVolumeRequest{
			VolumeId:   volID,
			TargetPath: req.TargetPath,
		})
		return nil, status.Error(codes.Internal, "inline ephemeral node publish failed: "+err.Error())
	}
	return &csi.NodePublishVolumeResponse{}, nil
}

func sanitizeEphemeralCreateVolumeParams(params map[string]string) map[string]string {
	safeParams := make(map[string]string, len(params))
	for k, v := range params {
		switch k {
		case "systemID":
			continue
		default:
			safeParams[k] = v
		}
	}
	return safeParams
}

// Call stack: ControllerUnpublishVolume, DeleteVolume (NodeUnpublish will be already called by the time this method is called)
// remove lockfile
func (s *service) ephemeralNodeUnpublish(
	ctx context.Context,
	req *csi.NodeUnpublishVolumeRequest,
) error {
	csmlog.Infof("Called ephemeral Node unpublish")

	volID := req.GetVolumeId()
	if volID == "" {
		return status.Error(codes.InvalidArgument, "volume ID is required")
	}
	stagingPath := filepath.Clean(filepath.Join(ephemeralStagingMountPath, volID))
	lockFile := filepath.Clean(filepath.Join(ephemeralStagingMountPath, volID, "id"))
	csmlog.Debugf("Lock-file path:%s", lockFile)

	//while a file is being read from, it's a file determined by volID and is written by the driver
	/* #nosec G304 */
	dat, err := os.ReadFile(lockFile)
	if err != nil && os.IsNotExist(err) {
		return status.Error(codes.Internal, "Inline ephemeral. Was unable to read lockfile")
	}

	goodVolid := string(dat)
	NodeID := s.opts.SdcGUID
	if s.useNVME {
		csmlog.Infof("SdcGUID is not set, using NodeID: %s", s.nodeID)
		NodeID = s.nodeID
	}
	csmlog.WithContext(ctx).Infof("Read volume and array ID from file: %s", goodVolid)

	if s.useNVME {
		csmlog.Debug("Unstaging NVME ephemeral volume")
		unStageReq := &csi.NodeUnstageVolumeRequest{
			StagingTargetPath: stagingPath,
			VolumeId:          goodVolid,
		}
		_, err = s.NodeUnstageVolume(ctx, unStageReq)
		if err != nil {
			return err
		}
	}
	_, err = s.ControllerUnpublishVolume(ctx, &csi.ControllerUnpublishVolumeRequest{
		VolumeId: goodVolid,
		NodeId:   NodeID,
	})
	if err != nil {
		return fmt.Errorf("Inline ephemeral controller unpublish failed: %v: ", err)
	}

	_, err = s.DeleteVolume(ctx, &csi.DeleteVolumeRequest{
		VolumeId: goodVolid,
	})
	if err != nil {
		return err
	}
	fileToRemove := filepath.Clean(filepath.Join(ephemeralStagingMountPath, volID))
	csmlog.Debugf("lock-file to delete:%s", fileToRemove)

	err = os.RemoveAll(fileToRemove)
	if err != nil {
		return fmt.Errorf("failed to cleanup lock files: %v", err)
	}
	return nil
}
