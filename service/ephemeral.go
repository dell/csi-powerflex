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
	"os"
	"regexp"
	"strconv"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	ephemeralStagingMountPath = "/var/lib/kubelet/plugins/kubernetes.io/csi/pv/ephemeral/"
)

func (s *service) fileExist(filename string) bool {
	_, err := os.Stat(filename)
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
	req *csi.NodePublishVolumeRequest) (
	*csi.NodePublishVolumeResponse, error) {

	_, err := os.Stat(ephemeralStagingMountPath)
	if err != nil {
		if os.IsNotExist(err) {
			Log.Debug("path does not exist, will attempt to create it")
			err = os.MkdirAll(ephemeralStagingMountPath, 0750)
			if err != nil {
				Log.Errorf("ephemeralNodePublish: %s", err.Error())
				return nil, status.Error(codes.Internal, "Unable to create directory for mounting ephemeral volumes")
			}
		}
		Log.Errorf("Unable to check stat of file: %s with error: %v", ephemeralStagingMountPath, err.Error())
	}

	volID := req.GetVolumeId()
	volName := req.VolumeContext["volumeName"]
	if len(volName) > 31 {
		Log.Errorf("Volume name: %s is over 32 characters, too long.", volName)
		return nil, status.Error(codes.Internal, "Volume name too long")

	}

	if volName == "" {
		Log.Errorf("Missing Parameter: volumeName must be specified in volume attributes section for ephemeral volumes")
		return nil, status.Error(codes.Internal, "Volume name not specified")
	}

	volSize, err := parseSize(req.VolumeContext["size"])
	if err != nil {
		Log.Errorf("Parse size failed %s", err.Error())
		return nil, status.Error(codes.Internal, "inline ephemeral parse size failed")
	}

	systemName := req.VolumeContext["systemID"]

	if systemName == "" {
		Log.Debug("systemName not specified, using default array")
		systemName = s.opts.defaultSystemID
	}

	array := s.opts.arrays[systemName]

	if array == nil {
		//to get inside this if block, req has name, but secret has ID, need to convert from name -> ID
		if id, ok := s.connectedSystemNameToID[systemName]; ok {
			//systemName was sent in req, but secret used ID. Change to ID.
			Log.Debug("systemName set to id")
			array = s.opts.arrays[id]
		} else {
			err = status.Errorf(codes.Internal, "systemID: %s not recgonized", systemName)
			Log.Errorf("Error from ephemeralNodePublish: %v ", err)
			return nil, err

		}
	}

	err = s.systemProbe(ctx, array)

	if err != nil {
		Log.Errorf("systemProb  Ephemeral %s", err.Error())
		return nil, status.Error(codes.Internal, "inline ephemeral system prob failed: "+err.Error())
	}

	crvolresp, err := s.CreateVolume(ctx, &csi.CreateVolumeRequest{
		Name: volName,
		CapacityRange: &csi.CapacityRange{
			RequiredBytes: volSize,
			LimitBytes:    0,
		},
		VolumeCapabilities: []*csi.VolumeCapability{req.VolumeCapability},
		Parameters:         req.VolumeContext,
		Secrets:            req.Secrets,
	})

	if err != nil {
		Log.Errorf("CreateVolume Ephemeral %s", err.Error())
		return nil, status.Error(codes.Internal, "inline ephemeral create volume failed: "+err.Error())
	}

	Log.Infof("volume ID returned from CreateVolume is: %s ", crvolresp.Volume.VolumeId)

	//Create lockfile to map vol ID from request to volID returned by CreateVolume
	// will also be used to determine if volume is ephemeral in NodeUnpublish
	errLock := os.MkdirAll(ephemeralStagingMountPath+volID, 0750)
	if errLock != nil {
		return nil, errLock
	}
	f, errLock := os.Create(ephemeralStagingMountPath + volID + "/id")
	if errLock != nil {
		return nil, errLock
	}
	defer f.Close() //#nosec
	_, errLock = f.WriteString(crvolresp.Volume.VolumeId)
	if errLock != nil {
		return nil, errLock
	}

	volumeID := crvolresp.Volume.VolumeId

	//in case systemName was not given with volume context
	systemName = s.getSystemIDFromCsiVolumeID(volumeID)

	if systemName == "" {

		Log.Errorf("getSystemIDFromCsiVolumeID was not able to determine systemName from volID: %s", volumeID)
		return nil, status.Error(codes.Internal, "inline ephemeral getSystemIDFromCsiVolumeID failed ")
	}

	NodeID := s.opts.SdcGUID

	cpubresp, err := s.ControllerPublishVolume(ctx, &csi.ControllerPublishVolumeRequest{
		NodeId:           NodeID,
		VolumeId:         volumeID,
		VolumeCapability: req.VolumeCapability,
		Readonly:         req.Readonly,
		Secrets:          req.Secrets,
		VolumeContext:    crvolresp.Volume.VolumeContext,
	})

	if err != nil {
		Log.Infof("Rolling back and calling unpublish ephemeral volumes with VolId %s", crvolresp.Volume.VolumeId)
		_, _ = s.NodeUnpublishVolume(ctx, &csi.NodeUnpublishVolumeRequest{
			VolumeId:   volID,
			TargetPath: req.TargetPath,
		})
		return nil, status.Error(codes.Internal, "inline ephemeral controller publish failed: "+err.Error())
	}

	_, err = s.NodePublishVolume(ctx, &csi.NodePublishVolumeRequest{
		VolumeId:          volumeID,
		PublishContext:    cpubresp.PublishContext,
		StagingTargetPath: ephemeralStagingMountPath,
		TargetPath:        req.TargetPath,
		VolumeCapability:  req.VolumeCapability,
		Readonly:          req.Readonly,
		Secrets:           req.Secrets,
		VolumeContext:     crvolresp.Volume.VolumeContext,
	})
	if err != nil {
		Log.Errorf("NodePublishErrEph %s", err.Error())
		_, _ = s.NodeUnpublishVolume(ctx, &csi.NodeUnpublishVolumeRequest{
			VolumeId:   volID,
			TargetPath: req.TargetPath,
		})
		return nil, status.Error(codes.Internal, "inline ephemeral node publish failed: "+err.Error())
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

// Call stack: ControllerUnpublishVolume, DeleteVolume (NodeUnpublish will be already called by the time this method is called)
// remove lockfile
func (s *service) ephemeralNodeUnpublish(
	ctx context.Context,
	req *csi.NodeUnpublishVolumeRequest) error {

	Log.Infof("Called ephemeral Node unpublish")

	volID := req.GetVolumeId()
	if volID == "" {
		return status.Error(codes.InvalidArgument, "volume ID is required")
	}

	lockFile := ephemeralStagingMountPath + volID + "/id"

	//while a file is being read from, it's a file determined by volID and is written by the driver
	/* #nosec G304 */
	dat, err := os.ReadFile(lockFile)
	if err != nil && os.IsNotExist(err) {
		return status.Error(codes.Internal, "Inline ephemeral. Was unable to read lockfile")
	}

	goodVolid := string(dat)
	NodeID := s.opts.SdcGUID

	_, err = s.ControllerUnpublishVolume(ctx, &csi.ControllerUnpublishVolumeRequest{
		VolumeId: goodVolid,
		NodeId:   NodeID,
	})
	if err != nil {

		return errors.New("Inline ephemeral controller unpublish failed")
	}

	_, err = s.DeleteVolume(ctx, &csi.DeleteVolumeRequest{
		VolumeId: goodVolid,
	})
	if err != nil {

		return err
	}
	err = os.RemoveAll(ephemeralStagingMountPath + volID)
	if err != nil {
		return errors.New("failed to cleanup lock files")
	}

	return nil

}
