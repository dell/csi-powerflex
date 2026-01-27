// Copyright © 2019-2024 Dell Inc. or its subsidiaries. All Rights Reserved.
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
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dell/csmlog"
	"github.com/dell/gofsutil"
	"github.com/dell/goscaleio"
	siotypes "github.com/dell/goscaleio/types/v1"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var (
	// slice of the connected PowerFlex systems
	connectedSystemID             = make([]string, 0)
	publishGetMappedVolMaxRetry   = 30
	unpublishGetMappedVolMaxRetry = 5
	getMappedVolDelay             = (1 * time.Second)

	// GetNodeLabels - Get the node labels
	GetNodeLabels = getNodelabels
	GetNodeUID    = getNodeUID
)

const (
	maxVxflexosVolumesPerNodeLabel = "max-vxflexos-volumes-per-node"
)

func (s *service) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	if !s.useNVME {
		// This stage path is a no-op for SDC nodes
		// Return OK to preserve idempotency semantics if upper layers still call Stage.
		return &csi.NodeStageVolumeResponse{}, nil
	}

	logFields := csmlog.ExtractFieldsFromContext(ctx)

	if req.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, "volume capability is required")
	}

	csiVolID := req.GetVolumeId()
	if csiVolID == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID is required")
	}

	stagingPath := req.GetStagingTargetPath()
	if stagingPath == "" {
		return nil, status.Error(codes.InvalidArgument, "staging target path is required")
	}

	volID := getVolumeIDFromCsiVolumeID(csiVolID)
	log.Infof("[NodeStageVolume] volumeID: %s", volID)

	systemID := s.getSystemIDFromCsiVolumeID(csiVolID)
	log.Infof("[NodeStageVolume] systemID: %s harvested from csiVolID: %s", systemID, csiVolID)
	if systemID == "" {
		systemID = s.opts.defaultSystemID
	}
	if systemID == "" {
		return nil, status.Error(codes.InvalidArgument, "systemID is not found in the request and there is no default system")
	}

	log.Infof("[NodeStageVolume] We are about to probe the system with systemID %s", systemID)
	// Probe the system to make sure it is managed by driver
	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	if err := s.discoverAndConnectNVMeTargets(s.systems[systemID]); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	var stager VolumeStager
	stager = &NVMeStager{
		useNVME:       s.useNVME,
		systemID:      systemID,
		nvmeConnector: s.nvmeConnector,
		targetNqn:     s.nvmeTargetNqn,
		adminClient:   s.adminClients[systemID],
	}
	response, err := stager.Stage(ctx, req, stagingPath, logFields, volID)
	return response, err
}

// NodeUnstageVolume will cleanup the staging path passed in the request.
// This will only be called by CSM-resliency (podmon), as the driver does not advertise support for STAGE_UNSTAGE_VOLUME in NodeGetCapabilities,
// therefore Kubernetes will not call it.
func (s *service) NodeUnstageVolume(
	ctx context.Context,
	req *csi.NodeUnstageVolumeRequest) (
	*csi.NodeUnstageVolumeResponse, error,
) {
	var reqID string
	headers, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if req, ok := headers["csi.requestid"]; ok && len(req) > 0 {
			reqID = req[0]
		}
	}

	stagingTargetPath := req.GetStagingTargetPath()
	if stagingTargetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "StagingTargetPath is required")
	}
	csiVolID := req.GetVolumeId()
	if csiVolID == "" {
		return nil, status.Error(codes.InvalidArgument, "Volume ID is required")
	}

	fields := map[string]interface{}{
		"CSI Request":         "NodeUnstageVolume",
		"CSI Volume ID":       csiVolID,
		"Staging Target Path": stagingTargetPath,
		"Request ID":          reqID,
	}

	// Skip ephemeral volumes. For ephemeral volumes, kubernetes gives us an internal ID, so we use the lockfile to find the Powerflex ID this is mapped to.
	lockFile := ephemeralStagingMountPath + csiVolID + "/id"
	if s.fileExist(lockFile) {
		log.WithFields(fields).Info("Skipping ephemeral volume")
		return &csi.NodeUnstageVolumeResponse{}, nil
	}

	// Calling NVMeStager to unstage volume
	if s.useNVME {
		var stager VolumeStager
		stager = &NVMeStager{
			useNVME:       s.useNVME,
			nvmeConnector: s.nvmeConnector,
		}
		response, err := stager.Unstage(ctx, stagingTargetPath, fields, csiVolID)
		return response, err
	}

	// Unmount the staging target path.
	log.WithFields(fields).Info("unmounting directory")
	if err := gofsutil.Unmount(ctx, stagingTargetPath); err != nil && !os.IsNotExist(err) {
		log.Errorf("Unable to Unmount staging target path: %s", err)
	}

	log.WithFields(fields).Info("removing directory")
	if err := os.Remove(stagingTargetPath); err != nil && !os.IsNotExist(err) {
		log.Errorf("Unable to remove staging target path: %v", err)
		err := fmt.Errorf("Unable to remove staging target path: %s error: %v", stagingTargetPath, err)
		return &csi.NodeUnstageVolumeResponse{}, err
	}

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (s *service) NodePublishVolume(
	ctx context.Context,
	req *csi.NodePublishVolumeRequest) (
	*csi.NodePublishVolumeResponse, error,
) {
	var reqID string
	headers, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if req, ok := headers["csi.requestid"]; ok && len(req) > 0 {
			reqID = req[0]
		}
	}
	s.logStatistics()
	volumeContext := req.GetVolumeContext()
	if volumeContext != nil {
		log.Info("VolumeContext:")
		for key, value := range volumeContext {
			log.WithFields(csmlog.Fields{key: value}).Info("found in VolumeContext")
		}
	}

	ephemeral, ok := req.VolumeContext["csi.storage.k8s.io/ephemeral"]
	if ok && strings.ToLower(ephemeral) == "true" {
		resp, err := s.ephemeralNodePublish(ctx, req)
		if err != nil {
			log.Errorf("ephemeralNodePublish returned error: %v", err)
		}
		return resp, err
	}

	csiVolID := req.GetVolumeId()
	if csiVolID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"volume ID is required")
	}
	log.Infof("[NodePublishVolume] csiVolID: %s", csiVolID)

	// Check for NFS protocol
	fsType := volumeContext[KeyFsType]
	isNFS := false
	if fsType == "nfs" {
		isNFS = true
	}

	volID := getVolumeIDFromCsiVolumeID(csiVolID)
	log.Infof("[NodePublishVolume] volumeID: %s", volID)

	systemID := s.getSystemIDFromCsiVolumeID(csiVolID)
	log.Infof("[NodePublishVolume] systemID: %s harvested from csiVolID: %s", systemID, csiVolID)
	if systemID == "" {
		// use default system
		systemID = s.opts.defaultSystemID
	}
	if systemID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"systemID is not found in the request and there is no default system")
	}

	log.Infof("[NodePublishVolume] We are about to probe the system with systemID %s", systemID)
	// Probe the system to make sure it is managed by driver
	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	// ensure no ambiguity if legacy vol
	err := s.checkVolumesMap(csiVolID)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"checkVolumesMap for id: %s failed : %s", csiVolID, err.Error())
	}
	// handle NFS nodePublish separately
	if isNFS {
		fsID := getFilesystemIDFromCsiVolumeID(csiVolID)

		fs, err := s.getFilesystemByID(fsID, systemID)
		if err != nil {
			if strings.EqualFold(err.Error(), sioGatewayFileSystemNotFound) || strings.Contains(err.Error(), "must be a hexadecimal number") {
				return nil, status.Error(codes.NotFound,
					"filesystem not found")
			}
		}

		client := s.adminClients[systemID]

		NFSExport, err := s.getNFSExport(fs, client)
		if err != nil {
			return nil, err
		}

		fileInterface, err := s.getFileInterface(systemID, fs, client)
		if err != nil {
			return nil, err
		}
		// Formulating nfsExportURl
		// NFSExportURL = "nas_server_ip:NFSExport_Path"
		// NFSExportURL = 10.1.1.1.1:/nfs-volume
		path := fmt.Sprintf("%s:%s", fileInterface.IPAddress, NFSExport.Path)

		if err := publishNFS(ctx, req, path); err != nil {
			return nil, err
		}

		return &csi.NodePublishVolumeResponse{}, nil
	}

	if s.useNVME {
		nguid, err := buildNGUID(volID, systemID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to build NGUID: %s", err.Error())
		}

		symlinkPath, _, err := gofsutil.WWNToDevicePathX(context.Background(), nguid)
		if err != nil || symlinkPath == "" {
			errmsg := fmt.Sprintf("device path not found for nguid %s: %s", nguid, err)
			log.Error(errmsg)
			return nil, status.Error(codes.NotFound, errmsg)
		}

		if err := publishNVMEVolume(req, reqID, symlinkPath); err != nil {
			return nil, err
		}
	} else {
		sdcMappedVol, err := s.getSDCMappedVol(volID, systemID, publishGetMappedVolMaxRetry)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}

		if err := publishVolume(req, s.privDir, sdcMappedVol.SdcDevice, reqID); err != nil {
			return nil, err
		}
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (s *service) NodeUnpublishVolume(
	ctx context.Context,
	req *csi.NodeUnpublishVolumeRequest) (
	*csi.NodeUnpublishVolumeResponse, error,
) {
	var reqID string
	headers, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if req, ok := headers["csi.requestid"]; ok && len(req) > 0 {
			reqID = req[0]
		}
	}

	targetPath := req.GetTargetPath()
	if targetPath == "" {
		return nil, status.Error(codes.InvalidArgument, "A target path argument is required")
	}

	s.logStatistics()

	csiVolID := req.GetVolumeId()
	if csiVolID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"volume ID is required")
	}

	isNFS := strings.Contains(csiVolID, "/")
	var ephemeralVolume bool
	// For ephemeral volumes, kubernetes gives us an internal ID, so we need to use the lockfile to find the Powerflex ID this is mapped to.

	lockFile := filepath.Clean(filepath.Join(ephemeralStagingMountPath, csiVolID, "id"))

	if s.fileExist(lockFile) {
		ephemeralVolume = true
		//while a file is being read from, it's a file determined by volID and is written by the driver
		/* #nosec G304 */
		idFromFile, err := os.ReadFile(lockFile)
		if err != nil && os.IsNotExist(err) {
			log.Errorf("NodeUnpublish with ephemeral volume. Was unable to read lockfile: %v", err)
			return nil, status.Error(codes.Internal, "NodeUnpublish with ephemeral volume. Was unable to read lockfile")
		}
		// Convert volume id from []byte to string format
		csiVolID = string(idFromFile)
		log.Infof("Read volume ID: %s from lockfile: %s ", csiVolID, lockFile)
	}

	if isNFS {
		fsID := getFilesystemIDFromCsiVolumeID(csiVolID)
		log.Infof("NodeUnpublishVolume fileSystemID: %s", fsID)

		systemID := s.getSystemIDFromCsiVolumeID(csiVolID)
		if systemID == "" {
			// use default system
			systemID = s.opts.defaultSystemID
		}
		log.Infof("NodeUnpublishVolume systemID: %s", systemID)
		if systemID == "" {
			return nil, status.Error(codes.InvalidArgument,
				"systemID is not found in the request and there is no default system")
		}

		fs, err := s.getFilesystemByID(fsID, systemID)
		if err != nil {
			if strings.EqualFold(err.Error(), sioGatewayFileSystemNotFound) || strings.Contains(err.Error(), "must be a hexadecimal number") {
				return nil, status.Error(codes.NotFound,
					"filesystem not found")
			}
		}

		// Probe the system to make sure it is managed by driver
		if err := s.requireProbe(ctx, systemID); err != nil {
			return nil, err
		}

		// ensure no ambiguity if legacy vol
		err = s.checkVolumesMap(csiVolID)
		if err != nil {
			return nil, status.Errorf(codes.Internal,
				"checkVolumesMap for id: %s failed : %s", csiVolID, err.Error())
		}

		if err := unpublishNFS(ctx, req, fs.Name); err != nil {
			return nil, err
		}

		return &csi.NodeUnpublishVolumeResponse{}, nil

	}

	volID := getVolumeIDFromCsiVolumeID(csiVolID)
	log.Infof("NodeUnpublishVolume volumeID: %s", volID)

	systemID := s.getSystemIDFromCsiVolumeID(csiVolID)
	if systemID == "" {
		// use default system
		systemID = s.opts.defaultSystemID
	}
	log.Infof("NodeUnpublishVolume systemID: %s", systemID)
	if systemID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"systemID is not found in the request and there is no default system")
	}

	// Probe the system to make sure it is managed by driver
	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	// ensure no ambiguity if legacy vol
	err := s.checkVolumesMap(csiVolID)
	if err != nil {
		return nil, status.Errorf(codes.Internal,
			"checkVolumesMap for id: %s failed : %s", csiVolID, err.Error())
	}

	if s.useNVME {
		log.Infof("NodeUnpublishVolume: NVME volume %s, doing mount cleanup", csiVolID)

		if ephemeralVolume {
			log.Info("Detected ephemeral")
			err := s.ephemeralNodeUnpublish(ctx, req)
			if err != nil {
				log.Errorf("ephemeralNodeUnpublish returned error: %v", err)
				return nil, err
			}
		}

		if err := unpublishNVMEVolume(csiVolID, targetPath, reqID); err != nil {
			return nil, err
		}

		// Idempotent need to return ok if not published
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	sdcMappedVol, err := s.getSDCMappedVol(volID, systemID, unpublishGetMappedVolMaxRetry)
	if err != nil {
		log.Infof("Error from getSDCMappedVol is: %#v", err)
		log.Infof("Error message from getSDCMappedVol is: %s", err.Error())
		// fix k8s 19 bug: ControllerUnpublishVolume is called before NodeUnpublishVolume
		// cleanup target from pod
		if err := gofsutil.Unmount(ctx, targetPath); err != nil {
			log.Errorf("cleanup target mount: %s", err.Error())
		}

		if err := removeWithRetry(targetPath); err != nil {
			log.Errorf("cleanup target path: %s", err.Error())
		}
		// dont cleanup pvtMount in case it is in use elsewhere on the node

		if ephemeralVolume {
			log.Info("Detected ephemeral")
			err := s.ephemeralNodeUnpublish(ctx, req)
			if err != nil {
				log.Errorf("ephemeralNodeUnpublish returned error: %s", err.Error())
				return nil, err
			}
		}

		// Idempotent need to return ok if not published
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	if err := unpublishVolume(csiVolID, req.GetTargetPath(), s.privDir, sdcMappedVol.SdcDevice, reqID); err != nil {
		return nil, err
	}

	if ephemeralVolume {
		log.Info("Detected ephemeral")
		err := s.ephemeralNodeUnpublish(ctx, req)
		if err != nil {
			log.Errorf("ephemeralNodeUnpublish returned error: %v", err)
			return nil, err
		}

	}

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// Get sdc mapped volume from the given volume ID/systemID
func (s *service) getSDCMappedVol(volumeID string, systemID string, maxRetry int) (*goscaleio.SdcMappedVolume, error) {
	// If not found immediately, give a little time for controller to
	// communicate with SDC that it has volume
	var sdcMappedVol *goscaleio.SdcMappedVolume
	var err error
	for i := 0; i < maxRetry; i++ {
		if id, ok := s.connectedSystemNameToID[systemID]; ok {
			log.Infof("Node publish getMappedVol name: %s id: %s", systemID, id)
			systemID = id
		}
		sdcMappedVol, err = getMappedVol(volumeID, systemID)
		if sdcMappedVol != nil {
			break
		}
		log.Infof("Node publish getMappedVol retry: %d", i)
		time.Sleep(getMappedVolDelay)
	}
	if err != nil {
		log.Infof("SDC returned volume %s on system %s not published to node", volumeID, systemID)
		return nil, err
	}
	return sdcMappedVol, err
}

// Get the volumes published to the SDC (given by SdcMappedVolume) and scan for requested vol id
func getMappedVol(volID string, systemID string) (*goscaleio.SdcMappedVolume, error) {
	// get source path of volume/device
	localVols, _ := goscaleio.GetLocalVolumeMap()
	var sdcMappedVol *goscaleio.SdcMappedVolume
	if len(localVols) == 0 {
		log.Infof("Length of localVols (goscaleio.GetLocalVolumeMap()) is 0 \n")
	}
	for _, v := range localVols {
		if v.VolumeID == volID && v.MdmID == systemID {
			sdcMappedVol = v
			log.Infof("Found matching SDC mapped volume %v", sdcMappedVol)
			break
		}
	}
	if sdcMappedVol == nil {
		return nil, status.Errorf(codes.Unavailable,
			"volume: %s on system: %s not published to node", volID, systemID)
	}
	return sdcMappedVol, nil
}

// getSystemName gets the system name for each system and append it to connectedSystemID variable
func (s *service) getSystemName(_ context.Context, systems []string) bool {
	for systemID := range s.opts.arrays {
		if id, ok := s.connectedSystemNameToID[systemID]; ok {
			for _, system := range systems {
				if id == system {
					log.Infof("nodeProbe found system Name: %s with id %s", systemID, id)
					connectedSystemID = append(connectedSystemID, systemID)
				}
			}
		}
	}
	return true
}

// nodeProbe fetchs the SDC GUID by drv_cfg and the systemIDs/names by getSystemName method.
// It also makes sure private directory(privDir) is created
func (s *service) nodeProbe(ctx context.Context) error {
	// skip SDC based probe if it is pure NVMe
	if s.useNVME == true {
		log.Info("skipping SDC based probe as the node is pure NVMe")
		return nil
	}

	// make sure the kernel module is loaded
	if kmodLoaded(s.opts) {
		// fetch the SDC GUID
		if s.opts.SdcGUID == "" {
			// try to query the SDC GUID
			guid, err := goscaleio.DrvCfgQueryGUID()
			if err != nil {
				return status.Errorf(codes.FailedPrecondition,
					"unable to get SDC GUID via config or automatically, error: %s", err.Error())
			}

			s.opts.SdcGUID = guid
			log.WithFields(csmlog.Fields{"guid": s.opts.SdcGUID}).Info("set SDC GUID")
		}

		// support for pre-approved guid
		if s.opts.IsApproveSDCEnabled {
			log.Infof("Approve SDC enabled")
			if err := s.approveSDC(s.opts); err != nil {
				return err
			}
		}

		var err error
		if len(connectedSystemID) == 0 {
			connectedSystemID, err = getSystemsKnownToSDC()
			if err != nil {
				return status.Errorf(codes.FailedPrecondition, "%s", err.Error())
			}
		}

		// 	rename SDC
		//	case1: if IsSdcRenameEnabled=true and prefix given then set the prefix+worker_node_name for sdc name.
		//	case2: if IsSdcRenameEnabled=true and prefix not given then set worker_node_name for sdc name.
		//
		if s.opts.IsSdcRenameEnabled {
			err = s.renameSDC(s.opts)
			if err != nil {
				return err
			}
		}

		// get all the system names and IDs.
		s.getSystemName(ctx, connectedSystemID)

		// make sure privDir is pre-created
		if _, err := mkdir(s.privDir); err != nil {
			return status.Errorf(codes.Internal,
				"plugin private dir: %s creation error: %s",
				s.privDir, err.Error())
		}
	} else {
		log.Infof("scini module not loaded, perhaps it was intentional")
	}

	return nil
}

func (s *service) approveSDC(opts Opts) error {
	for _, system := range s.systems {
		if system == nil {
			continue
		}

		var sdc *goscaleio.Sdc
		var sdcGUID string

		// Try to fetch SDC details, but handle case where it might not exist yet
		foundSdc, err := system.FindSdc("SdcGUID", opts.SdcGUID)
		if err != nil {
			// SDC not found in ApprovedIp mode, using the GUID from opts
			if system.System.RestrictedSdcMode == "ApprovedIp" {
				sdcGUID = opts.SdcGUID
			} else {
				return status.Errorf(codes.FailedPrecondition, "%s", err)
			}
		} else {
			sdc = foundSdc
			sdcGUID = foundSdc.Sdc.SdcGUID
		}

		// Check if SDC is already approved (only if SDC is found)
		if sdc != nil && sdc.Sdc.SdcApproved {
			log.Infof("SDC already approved, SDC GUID: %s", sdc.Sdc.SdcGUID)
			continue
		}

		mode := system.System.RestrictedSdcMode

		switch mode {
		case "None":
			log.Infof("Approval not required, RestrictedSdcMode is: %s", mode)
		case "Guid", "ApprovedIp":
			// Approve with SdcGUID (common for both modes)
			resp, err := system.ApproveSdc(&siotypes.ApproveSdcParam{
				SdcGUID: sdcGUID,
			})
			if err != nil {
				return status.Errorf(codes.FailedPrecondition, "%s", err)
			}
			log.Infof("SDC ID %s approved successfully using mode: %s", resp.SdcID, mode)

			// Additional step for ApprovedIp mode
			if mode == "ApprovedIp" {
				ipAddresses, err := s.getNodeIP()
				if err != nil {
					return status.Errorf(codes.FailedPrecondition, "failed to find network interface IPs: %s", err)
				}

				err = system.SetApprovedIps(resp.SdcID, ipAddresses)
				if err != nil {
					return status.Errorf(codes.FailedPrecondition, "failed to set approved IPs: %s", err)
				}
				log.Infof("Approved IPs added successfully for SDC ID: %s", resp.SdcID)
			}
		default:
			return status.Errorf(codes.InvalidArgument, "unsupported RestrictedSdcMode: %s", mode)
		}
	}
	return nil
}

func (s *service) getNodeIP() ([]string, error) {
	var ips []string

	// Get interface IPs from ConfigMap
	configInterfaceIPs, err := s.findNetworkInterfaceIPs()
	if err == nil && len(configInterfaceIPs) > 0 {
		return configInterfaceIPs, nil
	}

	// Fallback: Get all network interfaces
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, iface := range interfaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				if ipnet.IP.To4() != nil { // IPv4
					ips = append(ips, ipnet.IP.String())
				}
			}
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no valid IP addresses found")
	}
	return ips, nil
}

func (s *service) renameSDC(opts Opts) error {
	// fetch hostname
	hostName, ok := os.LookupEnv("HOSTNAME")
	if !ok {
		return status.Errorf(codes.FailedPrecondition, "%s not set", "HOSTNAME")
	}

	// fetch SDC details
	for _, systemID := range connectedSystemID {
		if s.systems[systemID] == nil {
			continue
		}
		sdc, err := s.systems[systemID].FindSdc("SdcGUID", opts.SdcGUID)
		if err != nil {
			return status.Errorf(codes.FailedPrecondition, "%s", err)
		}
		sdcID := sdc.Sdc.ID

		var newName string
		if len(opts.SdcPrefix) > 0 {
			// case1: if IsSdcRenameEnabled=true and prefix given then set the prefix+worker_node_name for sdc name.
			newName = opts.SdcPrefix + "-" + hostName
		} else {
			// case2: if IsSdcRenameEnabled=true and prefix not given then set worker_node_name for sdc name.
			newName = hostName
		}
		if sdc.Sdc.Name == newName {
			log.Infof("SDC is already named: %s.", newName)
		} else {
			log.Infof("Assigning name: %s to SDC with GUID %s on system %s", newName, s.opts.SdcGUID,
				systemID)
			err = s.adminClients[systemID].RenameSdc(sdcID, newName)
			if err != nil {
				return status.Errorf(codes.FailedPrecondition, "Failed to rename SDC: %s", err)
			}
			err = s.getSDCName(opts.SdcGUID, systemID)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *service) getSDCName(sdcGUID string, systemID string) error {
	sdc, err := s.systems[systemID].FindSdc("SdcGUID", sdcGUID)
	if err != nil {
		return status.Errorf(codes.FailedPrecondition, "%s", err)
	}
	log.Infof("SDC name set to: %s.", sdc.Sdc.Name)
	return nil
}

func kmodLoaded(opts Opts) bool {
	// opts.Lsmod is introduced solely for unit testing.
	var out []byte
	var err error
	if opts.Lsmod == "" {
		out, err = exec.Command("lsmod").CombinedOutput()
		if err != nil {
			log.Errorf("error from lsmod: %v", err)
			return false
		}
	} else {
		out = []byte(opts.Lsmod)
	}

	r := bytes.NewReader(out)
	s := bufio.NewScanner(r)

	for s.Scan() {
		l := s.Text()
		words := strings.Split(l, " ")
		if words[0] == "scini" {
			return true
		}
	}

	return false
}

func getSystemsKnownToSDC() ([]string, error) {
	systems := make([]string, 0)

	discoveredSystems, err := goscaleio.DrvCfgQuerySystems()
	if err != nil {
		return systems, err
	}

	set := make(map[string]struct{}, len(*discoveredSystems))

	for _, s := range *discoveredSystems {
		_, ok := set[s.SystemID]
		// duplicate SDC ID found
		if ok {
			return nil, fmt.Errorf("duplicate systems found that are known to SDC: %s", s.SystemID)
		}
		set[s.SystemID] = struct{}{}

		systems = append(systems, s.SystemID)
		log.WithFields(csmlog.Fields{"ID": s.SystemID}).Info("Found connected system")

	}

	return systems, nil
}

func (s *service) NodeGetCapabilities(
	_ context.Context,
	_ *csi.NodeGetCapabilitiesRequest) (
	*csi.NodeGetCapabilitiesResponse, error,
) {
	// these capabilities deal with volume health monitoring, and are only advertised by driver when user sets
	// node.healthMonitor.enabled is set to true in values file
	healthMonitorCapabalities := []*csi.NodeServiceCapability{
		{
			// Required for health monitor, optional if Health monitor is disabled
			// Indicates driver can report on volume condition in node plugin
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_VOLUME_CONDITION,
				},
			},
		},
		{
			// Required for NodeGetVolumeStats, optional if health monitor is disabled
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
				},
			},
		},
	}

	nodeCapabalities := []*csi.NodeServiceCapability{
		{
			// Required for NodeExpandVolume
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
				},
			},
		},
		{
			// Indicates PowerFlex supports SINGLE_NODE_SINGLE_WRITER and/or SINGLE_NODE_MULTI_WRITER access modes
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_SINGLE_NODE_MULTI_WRITER,
				},
			},
		},
	}

	stageCapabilities := []*csi.NodeServiceCapability{
		{
			// Required for NodeStageVolume
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
				},
			},
		},
	}

	if s.opts.IsHealthMonitorEnabled {
		nodeCapabalities = append(nodeCapabalities, healthMonitorCapabalities...)
	}

	nodeCapabalities = append(nodeCapabalities, stageCapabilities...)

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: nodeCapabalities,
	}, nil
}

// NodeGetInfo returns Node information
// NodeId is the identifier of the node. If SDC is installed, SDC GUID will be appended to NodeId
// MaxVolumesPerNode (optional) is left as 0 which means unlimited
// AccessibleTopology will be set with the VxFlex OS SystemID
func (s *service) NodeGetInfo(
	ctx context.Context,
	_ *csi.NodeGetInfoRequest) (
	*csi.NodeGetInfoResponse, error,
) {
	// Fetch SDC GUID
	if s.opts.SdcGUID == "" {
		if err := s.nodeProbe(ctx); err != nil {
			log.Infof("failed to probe node: %s", err)
		}
	}

	// Fetch Node ID
	if len(connectedSystemID) == 0 {
		if err := s.nodeProbe(ctx); err != nil {
			log.Infof("failed to probe node: %s", err)
		}
	}

	labels, err := GetNodeLabels(ctx, s)
	if err != nil {
		return nil, err
	}

	var maxVxflexosVolumesPerNode int64
	if len(connectedSystemID) != 0 {
		// Check for node label 'max-vxflexos-volumes-per-node'. If present set 'MaxVolumesPerNode' to this value.
		// If node label is not present, set 'MaxVolumesPerNode' to default value i.e., 0
		if val, ok := labels[maxVxflexosVolumesPerNodeLabel]; ok {
			maxVxflexosVolumesPerNode, err = strconv.ParseInt(val, 10, 64)
			if err != nil {
				return nil, status.Error(codes.InvalidArgument, GetMessage("invalid value '%s' specified for 'max-vxflexos-volumes-per-node' node label", val))
			}
		} else {
			// As per the csi spec the plugin MUST NOT set negative values to
			// 'MaxVolumesPerNode' in the NodeGetInfoResponse response
			if s.opts.MaxVolumesPerNode < 0 {
				return nil, status.Error(codes.InvalidArgument, GetMessage("maxVxflexosVolumesPerNode MUST NOT be set to negative value"))
			}
			maxVxflexosVolumesPerNode = s.opts.MaxVolumesPerNode
		}
	}

	log.Debugf("MaxVolumesPerNode: %v\n", maxVxflexosVolumesPerNode)

	// Create the topology keys
	// csi-vxflexos.dellemc.com/<systemID>: <provisionerName>
	log.Infof("Arrays: %+v", s.opts.arrays)
	topology := map[string]string{}

	if zone, ok := labels[s.opts.zoneLabelKey]; ok {
		topology[s.opts.zoneLabelKey] = zone

		err = s.SetPodZoneLabel(ctx, topology)
		if err != nil {
			log.Warnf("Unable to set availability zone label '%s:%s' for this pod", topology[s.opts.zoneLabelKey], zone)
		}
	}

	nodeID, err := GetNodeUID(ctx, s)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, GetMessage("Could not fetch node UID"))
	}

	if s.opts.SdcGUID != "" {
		nodeID = s.opts.SdcGUID
	}

	if s.useNVME {
		nodeID = s.nodeID
	}

	for _, array := range s.opts.arrays {
		// Check if NFS protocol is enabled on the array
		isNFSEnabled, err := s.isNFSEnabled(ctx, array.SystemID)
		if err != nil {
			return nil, err
		}
		if isNFSEnabled {
			topology[Name+"/"+array.SystemID+"-nfs"] = "true"
		}
		if zone, ok := topology[s.opts.zoneLabelKey]; ok {
			if zone == string(array.AvailabilityZone.Name) {
				// Add only the secret values with the correct zone.
				log.Infof("Zone found for node ID: %s, adding system ID: %s to node topology", nodeID, array.SystemID)
				s.populateNodeTopology(topology, array.SystemID)
			}
		} else {
			log.Infof("No zoning found for node ID: %s, adding system ID: %s", nodeID, array.SystemID)
			s.populateNodeTopology(topology, array.SystemID)
		}
	}

	log.Debugf("NodeId: %v\n", nodeID)
	return &csi.NodeGetInfoResponse{
		NodeId: nodeID,
		AccessibleTopology: &csi.Topology{
			Segments: topology,
		},
		MaxVolumesPerNode: maxVxflexosVolumesPerNode,
	}, nil
}

func (s *service) populateNodeTopology(topology map[string]string, systemID string) {
	// Check if NVMe protocol is enabled on the array
	if s.useNVME {
		system := s.systems[systemID]
		_, err := s.discoverNVMeTargets(system)
		if err != nil {
			log.Infof("Failed to connect to NVMe targets: %s", err)
		} else {
			topology[Name+"/"+systemID+"-nvmetcp"] = "true"
		}
	} else {
		topology[Name+"/"+systemID] = SystemTopologySystemValue
	}
}

// NodeGetVolumeStats will check the status of a volume given its ID and path
// if volume is healthy, stats on volume usage will be returned
// if volume is unhealthy, a message will be returned detailing the issue
// To determine if volume is healthy, this method checks: volume known to array, volume known to SDC, volume path readable, and volume path mounted
// Note: kubelet only calls this method when feature gate: CSIVolumeHealth=true
func (s *service) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	csiVolID := req.GetVolumeId()
	volPath := req.GetVolumePath()
	mounted := false
	healthy := true
	message := ""

	// validate params first, make sure neither field is empty
	if len(volPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "no volume Path provided")
	}

	if len(csiVolID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "no volume ID  provided")
	}

	// check if volume exists

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

	// make sure systemID we get is managed by the driver
	if err := s.requireProbe(ctx, systemID); err != nil {
		log := log.WithContext(ctx)
		log.Infof("System: %s is not managed by driver; volume stats will not be collected", systemID)
		return nil, err
	}

	_, err := s.getSDCMappedVol(volID, systemID, 30)
	if err != nil {
		// volume not known to SDC, next check if it exists at all
		_, _, err := s.listVolumes(systemID, 0, 0, false, false, volID, "")
		if err != nil && strings.Contains(err.Error(), sioGatewayVolumeNotFound) {
			message = fmt.Sprintf("Volume is not found by node driver at %s", time.Now().Format("2006-01-02 15:04:05"))
		} else if err != nil {
			// error was returned, but had nothing to do with the volume not being on the array (may be env related)
			return nil, err
		}
		// volume was found, but was not known to SDC. This is abnormal.
		healthy = false
		if message == "" {
			message = fmt.Sprintf("volume: %s was not mapped to host: %v", volID, err)
		}

	}

	// check if volume path is accessible
	if healthy {
		_, err = os.ReadDir(volPath)
		if err != nil && healthy {
			healthy = false
			message = fmt.Sprintf("volume path: %s is not accessible: %v", volPath, err)
		}
	}

	if healthy {

		// check if path is mounted on node
		mounts, err := getPathMounts(ctx, volPath)
		if len(mounts) > 0 {
			for _, m := range mounts {
				if m.Path == volPath {
					log.Infof("volPath: %s is mounted", volPath)
					mounted = true
				}
			}
		}
		if len(mounts) == 0 || !mounted || err != nil {
			healthy = false
			message = fmt.Sprintf("volPath: %s is not mounted: %v", volPath, err)
		}

	}

	if healthy {

		availableBytes, totalBytes, usedBytes, totalInodes, freeInodes, usedInodes, err := gofsutil.FsInfo(ctx, volPath)
		if err != nil {
			return &csi.NodeGetVolumeStatsResponse{
				Usage: []*csi.VolumeUsage{
					{
						Available: 0,
						Total:     0,
						Used:      0,
						Unit:      csi.VolumeUsage_UNKNOWN,
					},
				},
				VolumeCondition: &csi.VolumeCondition{
					Abnormal: true,
					Message:  fmt.Sprintf("failed to get metrics for volume with error: %v", err),
				},
			}, nil
		}
		return &csi.NodeGetVolumeStatsResponse{
			Usage: []*csi.VolumeUsage{
				{
					Available: availableBytes,
					Total:     totalBytes,
					Used:      usedBytes,
					Unit:      csi.VolumeUsage_BYTES,
				},
				{
					Available: freeInodes,
					Total:     totalInodes,
					Used:      usedInodes,
					Unit:      csi.VolumeUsage_INODES,
				},
			},
			VolumeCondition: &csi.VolumeCondition{
				Abnormal: !healthy,
				Message:  message,
			},
		}, nil

	}

	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{
				Available: 0,
				Total:     0,
				Used:      0,
				Unit:      csi.VolumeUsage_UNKNOWN,
			},
		},
		VolumeCondition: &csi.VolumeCondition{
			Abnormal: !healthy,
			Message:  message,
		},
	}, nil
}

func (s *service) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	var reqID string
	var err error
	var devName string
	headers, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if req, ok := headers["csi.requestid"]; ok && len(req) > 0 {
			reqID = req[0]
		}
	}

	err = s.nodeProbe(ctx)
	if err != nil {
		log.Error("nodeProbe failed with error :" + err.Error())
	}

	volumePath := req.GetVolumePath()
	if volumePath == "" {
		log.Error("Volume path required")
		return nil, status.Error(codes.InvalidArgument,
			"Volume path required")
	}

	// Check if volume path is a directory.
	// Mount type volumes are always mounted on a directory.
	// If not a directory, assume it's a raw block device mount and return ok.
	volumePathInfo, err := os.Lstat(volumePath)
	if err != nil {
		return nil, status.Error(codes.NotFound, "Could not stat volume path: "+volumePath)
	}
	if s.useSDC && !volumePathInfo.Mode().IsDir() {
		log.Infof("Volume path %s is not a directory- assuming a raw block device mount", volumePath)
		return &csi.NodeExpandVolumeResponse{}, nil
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

	volumeID := getVolumeIDFromCsiVolumeID(csiVolID)
	log.Infof("NodeExpandVolume volumeID: %s", volumeID)

	if volumeID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"volume ID is required")
	}

	systemID := s.getSystemIDFromCsiVolumeID(csiVolID)
	if systemID == "" {
		// use default system
		systemID = s.opts.defaultSystemID
	}
	log.Infof("NodeExpandVolume systemID: %s", systemID)
	if systemID == "" {
		return nil, status.Error(codes.InvalidArgument,
			"systemID is not found in the request and there is no default system")
	}

	// Probe the system to make sure it is managed by driver
	if err := s.requireProbe(ctx, systemID); err != nil {
		return nil, err
	}

	vol, err := s.getVolByID(volumeID, systemID)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable,
			"error retrieving volume details: %s", err.Error())
	}
	volname := vol.Name

	if s.useNVME {

		nguid, err := buildNGUID(volumeID, systemID)
		log.Infof("printing nguidddd %s", nguid)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to build NGUID: %s", err.Error())
		}

		devMnt, err := gofsutil.GetMountInfoFromDevice(ctx, volname)
		if err != nil {
			deviceNames, _ := gofsutil.GetSysBlockDevicesForVolumeWWN(context.Background(), nguid)
			for _, deviceName := range deviceNames {
				log.Infof("printing devicenames %s", deviceName)
			}

			if len(deviceNames) > 0 {
				for _, deviceName := range deviceNames {
					if strings.HasPrefix(deviceName, "nvme") {
						nvmeControllerDevice, err := gofsutil.GetNVMeController(deviceName)
						if err != nil {
							log.Errorf("Failed to rescan device (%s) with error (%s)", deviceName, err.Error())
							return nil, status.Error(codes.Internal, err.Error())
						}
						if nvmeControllerDevice != "" {
							devicePath := "/dev/" + nvmeControllerDevice
							log.Infof("Rescanning unmounted (raw block) device %s to expand size", devicePath)
							err = s.nvmeLib.DeviceRescan(devicePath)
							if err != nil {
								log.Errorf("Failed to rescan device (%s) with error (%s)", devicePath, err.Error())
								return nil, status.Error(codes.Internal, err.Error())
							}
						}
					} else {
						devicePath := "/sys/block" + "/" + deviceName
						log.Infof("Rescanning unmounted (raw block) device %s to expand size", deviceName)
						err = gofsutil.DeviceRescan(context.Background(), devicePath)
						if err != nil {
							log.Errorf("Failed to rescan device (%s) with error (%s)", devicePath, err.Error())
							return nil, status.Error(codes.Internal, err.Error())
						}
					}
					devName = deviceName
				}

				mpathDev, err := gofsutil.GetMpathNameFromDevice(ctx, devName)
				if err != nil {
					log.Errorf("Failed to fetch mpath name for device (%s) with error (%s)", devName, err.Error())
					return nil, status.Error(codes.Internal, err.Error())
				}
				if mpathDev != "" {
					err = gofsutil.ResizeMultipath(context.Background(), mpathDev)
					if err != nil {
						log.Errorf("Failed to resize filesystem: device  (%s) with error (%s)", mpathDev, err.Error())
						return nil, status.Error(codes.Internal, err.Error())
					}
				}

				return &csi.NodeExpandVolumeResponse{}, nil
			}
			log.Errorf("Failed to find mount info for (%s) with error (%s)", volname, err.Error())
			return nil, status.Error(codes.Internal,
				fmt.Sprintf("Failed to find mount info for (%s) with error (%s)", volname, err.Error()))
		}
		log.Infof("Mount info for volume %s: %+v", volname, devMnt)

		// Expand the filesystem with the actual expanded volume size.
		if devMnt.MPathName != "" {
			err = gofsutil.ResizeMultipath(context.Background(), devMnt.MPathName)
			if err != nil {
				log.Errorf("Failed to resize filesystem: device  (%s) with error (%s)", devMnt.MountPoint, err.Error())
				return nil, status.Error(codes.Internal, err.Error())
			}
		}
		// For a regular device, get the device path (devMnt.DeviceNames[1]) where the filesystem is mounted
		// PublishVolume creates devMnt.DeviceNames[0] but is left unused for regular devices
		var devicePath string
		if len(devMnt.DeviceNames) > 1 {
			devicePath = "/dev/" + devMnt.DeviceNames[1]
		} else {
			devicePath = "/dev/" + devMnt.DeviceNames[0]
		}

		// Determine file system type
		fsType, err := gofsutil.FindFSType(context.Background(), devMnt.MountPoint)
		if err != nil {
			log.Errorf("Failed to fetch filesystem for volume  (%s) with error (%s)", devMnt.MountPoint, err.Error())
			return nil, status.Error(codes.Internal, err.Error())
		}
		log.Infof("Found %s filesystem mounted on volume %s", fsType, devMnt.MountPoint)

		// Resize the filesystem
		err = gofsutil.ResizeFS(context.Background(), devMnt.MountPoint, devicePath, devMnt.PPathName, devMnt.MPathName, fsType)
		if err != nil {
			log.Errorf("Failed to resize filesystem: mountpoint (%s) device (%s) with error (%s)",
				devMnt.MountPoint, devicePath, err.Error())
			return nil, status.Error(codes.Internal, err.Error())
		}

		return &csi.NodeExpandVolumeResponse{}, nil
	}

	sdcMappedVolume, err := s.getSDCMappedVol(volumeID, systemID, publishGetMappedVolMaxRetry)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	log.Infof("sdcMappedVolume %+v", sdcMappedVolume)
	sdcDevice := strings.Replace(sdcMappedVolume.SdcDevice, "/dev/", "", 1)
	log.Infof("sdcDevice %s", sdcDevice)
	devicePath := sdcMappedVolume.SdcDevice
	log.Infof("devicePath %s", devicePath)
	size := req.GetCapacityRange().GetRequiredBytes()

	f := csmlog.Fields{
		"CSIRequestID": reqID,
		"DevicePath":   devicePath,
		"VolumeID":     csiVolID,
		"VolumePath":   volumePath,
		"Size":         size,
	}
	log.WithFields(f).Info("resizing volume")

	rc, err := goscaleio.DrvCfgQueryRescan()
	log.Infof("Rescan all SDC devices")
	if err != nil {
		log.Errorf("Rescan failed with ioctl error code %s with error %s, Run rescan manually on Powerflex host", rc, err.Error())
	}

	fsType, err := gofsutil.FindFSType(context.Background(), volumePath)
	if err != nil {
		log.Errorf("Failed to fetch filesystem type for mount (%s) with error (%s)", volumePath, err.Error())
		return nil, status.Error(codes.Internal, err.Error())
	}
	log.Infof("Found %s filesystem mounted on volume %s", fsType, volumePath)

	// Resize the filesystem
	err = gofsutil.ResizeFS(context.Background(), volumePath, devicePath, "", "", fsType)
	if err != nil {
		log.Errorf("Failed to resize filesystem: mountpoint (%s) device (%s) with error (%s)",
			volumePath, devicePath, err.Error())
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &csi.NodeExpandVolumeResponse{}, nil
}

func getNodelabels(ctx context.Context, s *service) (map[string]string, error) {
	return s.GetNodeLabels(ctx)
}

func getNodeUID(ctx context.Context, s *service) (string, error) {
	return s.GetNodeUID(ctx)
}
