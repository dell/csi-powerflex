/*
 *
 * Copyright © 2025 Dell Inc. or its subsidiaries. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

package service

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dell/csmlog"
	"github.com/dell/gobrick"
	"github.com/dell/gofsutil"
	"github.com/dell/goscaleio"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	procMountsPath    = "/proc/self/mountinfo"
	procMountsRetries = 15
	defaultDirPerm    = 0o700
)

// StageStatus represents the staging readiness of the volume at stagingPath.
type StageStatus int

const (
	StageNotFound    StageStatus = iota // no mount at stagingPath
	StageReady                          // mounted and publish-ready
	StageDeletedLink                    // mount source points to a deleted path
	StageMpathMember                    // mounted device is a multipath member path
	StageProbeError                     // failed to probe mounts or device format
)

func (s StageStatus) String() string {
	switch s {
	case StageNotFound:
		return "not_found"
	case StageReady:
		return "ready"
	case StageDeletedLink:
		return "deleted_link"
	case StageMpathMember:
		return "mpath_member"
	case StageProbeError:
		return "probe_error"
	default:
		return "unknown"
	}
}

type deviceInfo struct {
	nguid       string
	nvmeTargets []gobrick.NVMeTargetInfo
}

// VolumeStager allows node staging of a volume.
type VolumeStager interface {
	Stage(ctx context.Context, req *csi.NodeStageVolumeRequest, stagingPath string, logFields csmlog.Fields, volID string) (*csi.NodeStageVolumeResponse, error)
	Unstage(ctx context.Context, stagingPath string, logFields csmlog.Fields, volID string) (*csi.NodeUnstageVolumeResponse, error)
}

// NVMeStager implementation for staging volumes to NVMe hosts.
type NVMeStager struct {
	useNVME       bool
	nvmeConnector NVMEConnector
	systemID      string
	adminClient   *goscaleio.Client
	targetNqn     map[string]string
}

// Stage stages volume by connecting it through NVMe/TCP and creating bind mount to staging path.
func (n *NVMeStager) Stage(ctx context.Context, req *csi.NodeStageVolumeRequest, stagingPath string, logFields csmlog.Fields, volID string) (*csi.NodeStageVolumeResponse, error) {
	log := log.WithContext(ctx)
	logFields["VolumeID"] = volID
	logFields["StagingPath"] = stagingPath

	// Validate volume capability
	isBlock, mount, accMode, _, err := validateVolumeCapability(req.GetVolumeCapability(), false)
	if err != nil {
		return nil, err
	}

	// Validate volume existence
	_, err = n.adminClient.GetVolume("", strings.TrimSpace(volID), "", "", false)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "volume %s not found: %s", volID, err.Error())
	}

	// Build NGUID
	nguid, err := buildNGUID(volID, n.systemID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to build NGUID: %s", err.Error())
	}

	// Get system and NVMe targets
	system, err := n.adminClient.FindSystem(n.systemID, "", "")
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "system %s not found: %s", n.systemID, err.Error())
	}

	targetPortals, err := getNVMETCPTargetsInfoFromStorage(system)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "unable to get NVMe/TCP targets: %s", err.Error())
	}

	nvmeTargets := buildNVMeTargetInfo(n.targetNqn, targetPortals)

	deviceInfo := deviceInfo{
		nguid:       nguid,
		nvmeTargets: nvmeTargets,
	}

	logFields["Targets"] = nvmeTargets
	logFields["WWN"] = nguid
	ctx = csmlog.SetLogFields(ctx, logFields)

	// Ensure staging directory exists
	if err := os.MkdirAll(stagingPath, defaultDirPerm); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create staging path %s: %s", stagingPath, err.Error())
	}
	log.WithFields(logFields).Info("staging path created")

	// Check staging status
	stageStatus, err := isAlreadyStaged(ctx, stagingPath)
	log.WithFields(logFields).Debugf("staging status detected: %s", stageStatus.String())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to probe staging state: %v", err)
	}

	switch stageStatus {
	case StageReady:
		log.WithFields(logFields).Info("device already staged")
		return &csi.NodeStageVolumeResponse{}, nil
	case StageDeletedLink, StageMpathMember:
		log.WithFields(logFields).Warnf("unsafe staging state (%s); performing cleanup", stageStatus)
		if _, err := n.Unstage(ctx, stagingPath, logFields, volID); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to cleanup staging path: %v", err)
		}
	case StageNotFound:
		log.WithFields(logFields).Info("device not staged; proceeding with staging")
	default:
		return nil, status.Errorf(codes.Internal, "unknown stage status: %v", stageStatus)
	}

	// Connect device
	devicePath, err := n.connectDevice(ctx, deviceInfo)
	if err != nil {
		return nil, err
	}
	logFields["DevicePath"] = devicePath

	// Validate block device
	sysDevice, err := GetDevice(devicePath)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "error getting block device for volume %s: %s", volID, err.Error())
	}

	// Mount if filesystem volume
	if !isBlock {
		fs := mount.GetFsType()
		mntFlags := mount.GetMountFlags()
		fsFormatOption := req.GetVolumeContext()[KeyMkfsFormatOption]
		if fs == "xfs" {
			mntFlags = append(mntFlags, "nouuid")
		}
		if err := handlePrivFSMount(ctx, accMode, sysDevice, mntFlags, fs, stagingPath, fsFormatOption); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to mount disk %s to staging path: %s", devicePath, err.Error())
		}
	}

	log.WithFields(logFields).Info("stage complete")
	return &csi.NodeStageVolumeResponse{}, nil
}

func (n *NVMeStager) Unstage(ctx context.Context, stagingPath string, logFields csmlog.Fields, volID string) (*csi.NodeUnstageVolumeResponse, error) {
	log := log.WithContext(ctx)

	mounts, err := getPathMounts(ctx, stagingPath)
	if err != nil {
		log.Errorf("NodeUnstageVolume: failed to get mounts for staging path: %v", err)
		return &csi.NodeUnstageVolumeResponse{}, nil
	}

	var devicePath string
	for _, m := range mounts {
		if m.Path == stagingPath {
			devicePath = m.Device
			break
		}
	}

	// Unmount the staging target path.
	log.WithFields(logFields).Info("unmounting directory")
	if err := gofsutil.Unmount(ctx, stagingPath); err != nil && !os.IsNotExist(err) {
		log.Errorf("Unable to Unmount staging target path: %s", err)
	}

	log.WithFields(logFields).Info("removing directory")
	if err := os.Remove(stagingPath); err != nil && !os.IsNotExist(err) {
		log.Errorf("Unable to remove staging target path: %v", err)
	}

	// If we found a backing device and it looks like NVME, disconnect it.
	if devicePath != "" {
		log.Infof("NodeUnsatgeVolume: disconnecting NVME device %s for volumeID= %s", devicePath, volID)
		if err := n.disconnectNVMEDevice(ctx, devicePath); err != nil {
			log.Errorf("Node Unstage volume: failed to disconnect NVMEdevice %s: %v", devicePath, err)
			return nil, status.Errorf(codes.Internal, "Failed to disconnect NVME device %s: %v", devicePath, err)
		}
	} else {
		log.Infof("NodeUnsatgeVolume: no backing device found for stagingPath=%s; skipping NVME disconnect", stagingPath)
	}

	log.WithFields(logFields).Info("unstage complete")
	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (n *NVMeStager) connectDevice(ctx context.Context, data deviceInfo) (string, error) {
	log := log.WithContext(ctx)
	if !n.useNVME {
		log.Warn("invalid operation: node is not NVMe-enabled")
		return "", status.Errorf(codes.FailedPrecondition, "node does not support NVMe")
	}

	device, err := n.connectNVMEDevice(ctx, data)
	if err != nil {
		log.Errorf("unable to find device after multiple discovery attempts: %s", err.Error())
		return "", status.Errorf(codes.Internal, "unable to find device: %s", err.Error())
	}

	return filepath.Join("/dev", device.Name), nil
}

func (n *NVMeStager) connectNVMEDevice(ctx context.Context, data deviceInfo) (gobrick.Device, error) {
	logFields := csmlog.ExtractFieldsFromContext(ctx)
	var targets []gobrick.NVMeTargetInfo
	for _, t := range data.nvmeTargets {
		targets = append(targets, gobrick.NVMeTargetInfo{Target: t.Target, Portal: t.Portal})
	}

	// separate context to prevent 15 seconds cancel from kubernetes
	connectorCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	connectorCtx = csmlog.SetLogFields(connectorCtx, logFields)

	return n.nvmeConnector.ConnectVolume(connectorCtx, gobrick.NVMeVolumeInfo{
		Targets: targets,
		WWN:     data.nguid,
	}, false)
}

func (n *NVMeStager) disconnectNVMEDevice(ctx context.Context, devicePath string) error {
	devName := strings.TrimPrefix(devicePath, "/dev/")
	if devName == "" {
		return fmt.Errorf("empty device name for NVME disconnect")
	}

	if !strings.HasPrefix(devName, "nvme") {
		log.Infof("disconnectNVMEdevice: device %s does not look like nvme skipping", devName)
		return nil
	}

	log.Infof("disconnectNVMEDevices: disconnecting NVME device %s via connector", devName)

	if err := n.nvmeConnector.DisconnectVolumeByDeviceName(ctx, devName); err != nil {
		return fmt.Errorf("disconnectNVMEDevice: connector failed for %s: %w", devName, err)
	}

	log.Infof("disconnectNVMEDevice: successfully disconnect NVME device %s", devName)
	return nil
}

// isAlreadyStaged checks if stagingPath is mounted in a publish-ready way.
func isAlreadyStaged(ctx context.Context, stagingPath string) (StageStatus, error) {
	mnts, err := getPathMounts(ctx, stagingPath)
	if err != nil {
		return StageProbeError, fmt.Errorf("get mounts: %w", err)
	}
	if len(mnts) == 0 {
		log.Debug("isAlreadyStaged: no mounts found at stagingPath")
		return StageNotFound, nil
	}

	for _, m := range mnts {
		if strings.HasSuffix(m.Source, "deleted") {
			log.Warnf("isAlreadyStaged: mount source is deleted: src=%s", m.Source)
			return StageDeletedLink, nil
		}
	}

	devFS, err := gofsutil.GetDiskFormat(ctx, stagingPath)
	if err != nil {
		return StageProbeError, fmt.Errorf("disk format probe: %w", err)
	}
	if devFS == "mpath_member" {
		log.Warn("isAlreadyStaged: device is a multipath member (not the DM map)")
		return StageMpathMember, nil
	}

	return StageReady, nil
}

func buildNVMeTargetInfo(targetNqn map[string]string, nvmetcpTargetPortals []string) []gobrick.NVMeTargetInfo {
	nvmetcpTargets := []gobrick.NVMeTargetInfo{}
	for _, portal := range nvmetcpTargetPortals {
		ip, _, _ := net.SplitHostPort(portal)
		nvmetcpTargets = append(nvmetcpTargets, gobrick.NVMeTargetInfo{
			Target: targetNqn[ip],
			Portal: portal,
		})
	}
	return nvmetcpTargets
}
