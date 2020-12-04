package service

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/DATA-DOG/godog"
	"github.com/DATA-DOG/godog/gherkin"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/dell/gofsutil"
	"github.com/dell/goscaleio"
	types "github.com/dell/goscaleio/types/v1"
	ptypes "github.com/golang/protobuf/ptypes"
	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
)

const (
	badVolumeID                = "Totally Fake ID"
	goodVolumeID               = "111-111"
	goodVolumeName             = "vol1"
	altVolumeID                = "222-222"
	goodNodeID                 = "9E56672F-2F4B-4A42-BFF4-88B6846FBFDA"
	altNodeID                  = "7E012974-3651-4DCB-9954-25975A3C3CDF"
	datafile                   = "test/tmp/datafile"
	datadir                    = "test/tmp/datadir"
	badtarget                  = "/nonexist/target"
	altdatadir                 = "test/tmp/altdatadir"
	altdatafile                = "test/tmp/altdatafile"
	sdcVolume1                 = "d0f055a700000000"
	sdcVolume2                 = "d0f055aa00000001"
	sdcVolume0                 = "0000000000000000"
	mdmID                      = "0000"
	nodePublishBlockDevicePath = "test/dev/scinia"
	nodePublishAltBlockDevPath = "test/dev/scinib"
	nodePublishSymlinkDir      = "test/dev/disk/by-id"
	goodSnapID                 = "444-444"
	altSnapID                  = "555-555"
)

type feature struct {
	nGoRoutines                           int
	server                                *httptest.Server
	service                               *service
	adminClient                           *goscaleio.Client
	system                                *goscaleio.System
	err                                   error // return from the preceding call
	getPluginInfoResponse                 *csi.GetPluginInfoResponse
	getPluginCapabilitiesResponse         *csi.GetPluginCapabilitiesResponse
	probeResponse                         *csi.ProbeResponse
	createVolumeResponse                  *csi.CreateVolumeResponse
	publishVolumeResponse                 *csi.ControllerPublishVolumeResponse
	unpublishVolumeResponse               *csi.ControllerUnpublishVolumeResponse
	nodeGetInfoResponse                   *csi.NodeGetInfoResponse
	nodeGetCapabilitiesResponse           *csi.NodeGetCapabilitiesResponse
	deleteVolumeResponse                  *csi.DeleteVolumeResponse
	getCapacityResponse                   *csi.GetCapacityResponse
	controllerGetCapabilitiesResponse     *csi.ControllerGetCapabilitiesResponse
	validateVolumeCapabilitiesResponse    *csi.ValidateVolumeCapabilitiesResponse
	createSnapshotResponse                *csi.CreateSnapshotResponse
	createVolumeRequest                   *csi.CreateVolumeRequest
	publishVolumeRequest                  *csi.ControllerPublishVolumeRequest
	unpublishVolumeRequest                *csi.ControllerUnpublishVolumeRequest
	deleteVolumeRequest                   *csi.DeleteVolumeRequest
	listVolumesRequest                    *csi.ListVolumesRequest
	listVolumesResponse                   *csi.ListVolumesResponse
	listSnapshotsRequest                  *csi.ListSnapshotsRequest
	listSnapshotsResponse                 *csi.ListSnapshotsResponse
	listedVolumeIDs                       map[string]bool
	listVolumesNextTokenCache             string
	invalidVolumeID, noVolumeID, noNodeID bool
	omitAccessMode, omitVolumeCapability  bool
	wrongCapacity, wrongStoragePool       bool
	useAccessTypeMount                    bool
	capability                            *csi.VolumeCapability
	capabilities                          []*csi.VolumeCapability
	nodePublishVolumeRequest              *csi.NodePublishVolumeRequest
	createSnapshotRequest                 *csi.CreateSnapshotRequest
	volumeIDList                          []string
	snapshotIndex                         int
	volumeID                              string
}

func (f *feature) checkGoRoutines(tag string) {
	goroutines := runtime.NumGoroutine()
	fmt.Printf("goroutines %s new %d old groutines %d\n", tag, goroutines, f.nGoRoutines)
	f.nGoRoutines = goroutines
}

func (f *feature) aVxFlexOSService() error {
	f.checkGoRoutines("start aVxFlexOSService")
	// Save off the admin client and the system
	if f.service != nil && f.service.adminClient != nil {
		f.adminClient = f.service.adminClient
		f.system = f.service.system
	}
	f.err = nil
	f.getPluginInfoResponse = nil
	f.getPluginCapabilitiesResponse = nil
	f.probeResponse = nil
	f.createVolumeResponse = nil
	f.nodeGetInfoResponse = nil
	f.nodeGetCapabilitiesResponse = nil
	f.getCapacityResponse = nil
	f.controllerGetCapabilitiesResponse = nil
	f.validateVolumeCapabilitiesResponse = nil
	f.service = nil
	f.createVolumeRequest = nil
	f.publishVolumeRequest = nil
	f.unpublishVolumeRequest = nil
	f.invalidVolumeID = false
	f.noVolumeID = false
	f.noNodeID = false
	f.omitAccessMode = false
	f.omitVolumeCapability = false
	f.useAccessTypeMount = false
	f.wrongCapacity = false
	f.wrongStoragePool = false
	f.deleteVolumeRequest = nil
	f.deleteVolumeResponse = nil
	f.listVolumesRequest = nil
	f.listVolumesResponse = nil
	f.listVolumesNextTokenCache = ""
	f.listSnapshotsRequest = nil
	f.listSnapshotsResponse = nil
	f.listedVolumeIDs = make(map[string]bool)
	f.capability = nil
	f.capabilities = make([]*csi.VolumeCapability, 0)
	f.nodePublishVolumeRequest = nil
	f.createSnapshotRequest = nil
	f.createSnapshotResponse = nil
	f.volumeIDList = f.volumeIDList[:0]
	f.snapshotIndex = 0

	// configure gofsutil; we use a mock interface
	gofsutil.UseMockFS()
	gofsutil.GOFSMock.InduceBindMountError = false
	gofsutil.GOFSMock.InduceMountError = false
	gofsutil.GOFSMock.InduceGetMountsError = false
	gofsutil.GOFSMock.InduceDevMountsError = false
	gofsutil.GOFSMock.InduceUnmountError = false
	gofsutil.GOFSMock.InduceFormatError = false
	gofsutil.GOFSMock.InduceGetDiskFormatError = false
	gofsutil.GOFSMock.InduceGetDiskFormatType = ""
	gofsutil.GOFSMock.InduceFSTypeError = false
	gofsutil.GOFSMock.InduceResizeFSError = false
	gofsutil.GOFSMockMounts = gofsutil.GOFSMockMounts[:0]

	// configure variables in the driver
	getMappedVolMaxRetry = 1

	// Get or reuse the cached service
	f.getService()

	// Get the httptest mock handler. Only set
	// a new server if there isn't one already.
	handler := getHandler()
	if handler != nil {
		if f.server == nil {
			f.server = httptest.NewServer(handler)
		}
		f.service.opts.Endpoint = f.server.URL
		log.Printf("server url: %s\n", f.server.URL)
	} else {
		f.server = nil
	}
	f.checkGoRoutines("end aVxFlexOSService")
	return nil
}

func (f *feature) getService() *service {
	testControllerHasNoConnection = false
	svc := new(service)
	if f.adminClient != nil {
		svc.adminClient = f.adminClient
	}
	if f.system != nil {
		svc.system = f.system
	}
	svc.sdcMap = map[string]string{}
	svc.spCache = map[string]string{}
	svc.storagePoolIDToName = map[string]string{}
	svc.privDir = "./features"
	var opts Opts
	opts.User = "blah"
	opts.Password = "blah"
	opts.SystemName = "14dbbf5617523654"
	opts.Insecure = true
	opts.DisableCerts = true
	opts.EnableSnapshotCGDelete = true
	opts.EnableListVolumesSnapshots = true
	opts.SdcGUID = "9E56672F-2F4B-4A42-BFF4-88B6846FBFDA"
	opts.Lsmod = `
Module                  Size  Used by
vsock_diag             12610  0
scini                 799210  0
ip6t_rpfilter          12595  1
`
	opts.drvCfgQueryMDM = `
MDM-ID 14dbbf5617523654 SDC ID d0f33bd700000004 INSTALLATION ID 1c078b073d75512c IPs [0]-1.2.3.4 [1]-1.2.3.5
`
	svc.opts = opts
	f.service = svc
	svc.statisticsCounter = 99
	svc.logStatistics()
	return svc
}

// GetPluginInfo
func (f *feature) iCallGetPluginInfo() error {
	ctx := new(context.Context)
	req := new(csi.GetPluginInfoRequest)
	f.getPluginInfoResponse, f.err = f.service.GetPluginInfo(*ctx, req)
	if f.err != nil {
		return f.err
	}
	return nil
}
func (f *feature) aValidGetPlugInfoResponseIsReturned() error {
	rep := f.getPluginInfoResponse
	url := rep.GetManifest()["url"]
	if rep.GetName() == "" || rep.GetVendorVersion() == "" || url == "" {
		return errors.New("Expected GetPluginInfo to return name and version")
	}
	log.Printf("Name %s Version %s URL %s", rep.GetName(), rep.GetVendorVersion(), url)
	return nil
}

func (f *feature) iCallGetPluginCapabilities() error {
	ctx := new(context.Context)
	req := new(csi.GetPluginCapabilitiesRequest)
	f.getPluginCapabilitiesResponse, f.err = f.service.GetPluginCapabilities(*ctx, req)
	if f.err != nil {
		return f.err
	}
	return nil
}

func (f *feature) aValidGetPluginCapabilitiesResponseIsReturned() error {
	rep := f.getPluginCapabilitiesResponse
	capabilities := rep.GetCapabilities()
	var foundController bool
	for _, capability := range capabilities {
		if capability.GetService().GetType() == csi.PluginCapability_Service_CONTROLLER_SERVICE {
			foundController = true
		}
	}
	if !foundController {
		return errors.New("Expected PlugiinCapabilitiesResponse to contain CONTROLLER_SERVICE")
	}
	return nil
}

func (f *feature) iCallProbe() error {
	ctx := new(context.Context)
	req := new(csi.ProbeRequest)
	f.checkGoRoutines("before probe")
	f.probeResponse, f.err = f.service.Probe(*ctx, req)
	f.checkGoRoutines("after probe")
	return nil
}

func (f *feature) aValidProbeResponseIsReturned() error {
	if f.probeResponse.GetReady().GetValue() != true {
		return errors.New("Probe returned Ready false")
	}
	return nil
}

func (f *feature) theErrorContains(arg1 string) error {
	f.checkGoRoutines("theErrorContains")
	// If arg1 is none, we expect no error, any error received is unexpected
	if arg1 == "none" {
		if f.err == nil {
			return nil
		}
		return fmt.Errorf("Unexpected error: %s", f.err)

	}
	// We expected an error...
	if f.err == nil {
		possibleMatches := strings.Split(arg1, "@@")
		for _, possibleMatch := range possibleMatches {
			if possibleMatch == "none" {
				return nil
			}
		}
		return fmt.Errorf("Expected error to contain %s but no error", arg1)
	}
	// Allow for multiple possible matches, separated by @@. This was necessary
	// because Windows and Linux sometimes return different error strings for
	// gofsutil operations. Note @@ was used instead of || because the Gherkin
	// parser is not smart enough to ignore vertical braces within a quoted string,
	// so if || is used it thinks the row's cell count is wrong.
	possibleMatches := strings.Split(arg1, "@@")
	for _, possibleMatch := range possibleMatches {
		if strings.Contains(f.err.Error(), possibleMatch) {
			return nil
		}
	}
	return fmt.Errorf("Expected error to contain %s but it was %s", arg1, f.err.Error())
}

func (f *feature) thePossibleErrorContains(arg1 string) error {
	if f.err == nil {
		return nil
	}
	return f.theErrorContains(arg1)
}

func (f *feature) theControllerHasNoConnection() error {
	testControllerHasNoConnection = true
	return nil
}

func (f *feature) thereIsANodeProbeLsmodError() error {
	f.service.opts.Lsmod = ""
	return nil
}

func (f *feature) thereIsANodeProbeSdcGUIDError() error {
	f.service.opts.SdcGUID = ""
	return nil
}

func (f *feature) thereIsANodeProbeDrvCfgError() error {
	f.service.opts.drvCfgQueryMDM = ""
	return nil
}

func getTypicalCreateVolumeRequest() *csi.CreateVolumeRequest {
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	params["storagepool"] = "viki_pool_HDD_20181031"
	req.Parameters = params
	req.Name = "volume1"
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

func (f *feature) iSpecifyCreateVolumeMountRequest(fstype string) error {
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	params["storagepool"] = "viki_pool_HDD_20181031"
	req.Parameters = params
	req.Name = "mount1"
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = 32 * 1024 * 1024 * 1024
	req.CapacityRange = capacityRange
	capability := new(csi.VolumeCapability)
	mountVolume := new(csi.VolumeCapability_MountVolume)
	mountVolume.FsType = fstype
	mountVolume.MountFlags = make([]string, 0)
	mount := new(csi.VolumeCapability_Mount)
	mount.Mount = mountVolume
	capability.AccessType = mount
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	capability.AccessMode = accessMode
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities
	f.createVolumeRequest = req
	return nil
}

func (f *feature) iCallCreateVolume(name string) error {
	ctx := new(context.Context)
	if f.createVolumeRequest == nil {
		req := getTypicalCreateVolumeRequest()
		f.createVolumeRequest = req
	}
	req := f.createVolumeRequest
	req.Name = name

	if stepHandlersErrors.NoAdminError {
		f.service.adminClient = nil
		f.service.opts.AutoProbe = true
	}

	f.createVolumeResponse, f.err = f.service.CreateVolume(*ctx, req)
	if f.err != nil {
		log.Printf("CreateVolume called failed: %s\n", f.err.Error())
	}

	if f.createVolumeResponse != nil {
		log.Printf("vol id %s\n", f.createVolumeResponse.GetVolume().VolumeId)
	}
	return nil
}

func (f *feature) aValidCreateVolumeResponseIsReturned() error {
	if f.err != nil {
		return f.err
	}
	f.volumeIDList = append(f.volumeIDList, f.createVolumeResponse.Volume.VolumeId)
	fmt.Printf("volume %s pool %s\n",
		f.createVolumeResponse.Volume.VolumeContext["Name"],
		f.createVolumeResponse.Volume.VolumeContext["StoragePoolName"])
	return nil
}

func (f *feature) iSpecifyAccessibilityRequirementsWithASystemIDOf(requestedSystem string) error {
	if requestedSystem == "f.service.opt.SystemName" {
		requestedSystem = f.service.opts.SystemName
	}
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	params["storagepool"] = "viki_pool_HDD_20181031"
	req.Parameters = params
	req.Name = "accessability"
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = 32 * 1024 * 1024 * 1024
	req.CapacityRange = capacityRange
	req.AccessibilityRequirements = new(csi.TopologyRequirement)
	top := new(csi.Topology)
	top.Segments = map[string]string{
		"csi-vxflexos.dellemc.com/" + requestedSystem: "powerflex.dellemc.com",
	}
	req.AccessibilityRequirements.Preferred = append(req.AccessibilityRequirements.Preferred, top)
	f.createVolumeRequest = req
	return nil
}

func (f *feature) iSpecifyVolumeContentSource() error {
	req := getTypicalCreateVolumeRequest()
	req.Name = "volume_content_source"
	req.VolumeContentSource = new(csi.VolumeContentSource)
	req.VolumeContentSource.Type = &csi.VolumeContentSource_Volume{Volume: &csi.VolumeContentSource_VolumeSource{}}
	f.createVolumeRequest = req
	return nil
}

func (f *feature) iSpecifyMULTINODEWRITER() error {
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	params["storagepool"] = "viki_pool_HDD_20181031"
	req.Parameters = params
	req.Name = "multinode_writer"
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = 32 * 1024 * 1024 * 1024
	req.CapacityRange = capacityRange
	block := new(csi.VolumeCapability_BlockVolume)
	capability := new(csi.VolumeCapability)
	accessType := new(csi.VolumeCapability_Block)
	accessType.Block = block
	capability.AccessType = new(csi.VolumeCapability_Block)
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
	capability.AccessMode = accessMode
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities
	f.createVolumeRequest = req
	return nil
}

func (f *feature) iSpecifyABadCapacity() error {
	req := getTypicalCreateVolumeRequest()
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = -32 * 1024 * 1024 * 1024
	req.CapacityRange = capacityRange
	req.Name = "bad capacity"
	f.createVolumeRequest = req
	return nil
}

func (f *feature) iSpecifyNoStoragePool() error {
	req := getTypicalCreateVolumeRequest()
	req.Parameters = nil
	req.Name = "no storage pool"
	f.createVolumeRequest = req
	return nil
}

func (f *feature) iCallCreateVolumeSize(name string, size int64) error {
	ctx := new(context.Context)
	req := getTypicalCreateVolumeRequest()
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = size * 1024 * 1024 * 1024
	req.CapacityRange = capacityRange
	req.Name = name
	f.createVolumeRequest = req

	f.createVolumeResponse, f.err = f.service.CreateVolume(*ctx, req)
	if f.err != nil {
		log.Printf("CreateVolumeSize called failed: %s\n", f.err.Error())
	}
	if f.createVolumeResponse != nil {
		log.Printf("vol id %s\n", f.createVolumeResponse.GetVolume().VolumeId)
	}

	return nil
}

func (f *feature) iChangeTheStoragePool(storagePoolName string) error {
	params := make(map[string]string)
	params["storagepool"] = storagePoolName
	f.createVolumeRequest.Parameters = params
	return nil
}

func (f *feature) iInduceError(errtype string) error {
	log.Printf("set induce error %s\n", errtype)
	switch errtype {
	case "WrongSysNameError":
		stepHandlersErrors.WrongSysNameError = true
	case "NoAdminError":
		stepHandlersErrors.NoAdminError = true
	case "NoUserError":
		stepHandlersErrors.NoUserError = true
	case "NoPasswordError":
		stepHandlersErrors.NoPasswordError = true
	case "NoSysNameError":
		stepHandlersErrors.NoSysNameError = true
	case "NoEndpointError":
		stepHandlersErrors.NoEndpointError = true
	case "WrongVolIDError":
		stepHandlersErrors.WrongVolIDError = true
	case "BadVolIDError":
		stepHandlersErrors.BadVolIDError = true
	case "FindVolumeIDError":
		stepHandlersErrors.FindVolumeIDError = true
	case "GetVolByIDError":
		stepHandlersErrors.GetVolByIDError = true
	case "GetStoragePoolsError":
		stepHandlersErrors.GetStoragePoolsError = true
	case "GetSdcInstancesError":
		stepHandlersErrors.GetSdcInstancesError = true
	case "MapSdcError":
		stepHandlersErrors.MapSdcError = true
	case "RemoveMappedSdcError":
		stepHandlersErrors.RemoveMappedSdcError = true
	case "require-probe":
		f.service.opts.SdcGUID = ""
	case "SIOGatewayVolumeNotFound":
		stepHandlersErrors.SIOGatewayVolumeNotFoundError = true
	case "GetStatisticsError":
		stepHandlersErrors.GetStatisticsError = true
	case "CreateSnapshotError":
		stepHandlersErrors.CreateSnapshotError = true
	case "RemoveVolumeError":
		stepHandlersErrors.RemoveVolumeError = true
	case "VolumeInstancesError":
		stepHandlersErrors.VolumeInstancesError = true
	case "NoVolumeIDError":
		stepHandlersErrors.NoVolumeIDError = true
	case "SetVolumeSizeError":
		stepHandlersErrors.SetVolumeSizeError = true
	case "NoSymlinkForNodePublish":
		cmd := exec.Command("rm", "-rf", nodePublishSymlinkDir)
		_, err := cmd.CombinedOutput()
		if err != nil {
			return err
		}
	case "NoBlockDevForNodePublish":
		unitTestEmulateBlockDevice = false
		cmd := exec.Command("rm", nodePublishBlockDevicePath)
		_, err := cmd.CombinedOutput()
		if err != nil {
			return nil
		}
	case "TargetNotCreatedForNodePublish":
		err := os.Remove(datafile)
		if err != nil {
			return nil
		}
		cmd := exec.Command("rm", "-rf", datadir)
		_, err = cmd.CombinedOutput()
		if err != nil {
			return err
		}
	case "PrivateDirectoryNotExistForNodePublish":
		f.service.privDir = "xxx/yyy"
	case "BlockMkfilePrivateDirectoryNodePublish":
		f.service.privDir = datafile
	case "NodePublishNoVolumeCapability":
		f.nodePublishVolumeRequest.VolumeCapability = nil
	case "NodePublishNoAccessMode":
		f.nodePublishVolumeRequest.VolumeCapability.AccessMode = nil
	case "NodePublishNoAccessType":
		f.nodePublishVolumeRequest.VolumeCapability.AccessType = nil
	case "NodePublishPrivateTargetAlreadyCreated":
		err := os.MkdirAll("features/"+sdcVolume1, 0777)
		if err != nil {
			fmt.Printf("Couldn't make: %s\n", datadir+"/"+sdcVolume1)
		}
	case "NodePublishPrivateTargetAlreadyMounted":
		cmd := exec.Command("mknod", nodePublishAltBlockDevPath, "b", "0", "0")
		_, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("Couldn't create block dev: %s\n", nodePublishAltBlockDevPath)
		}
		err = os.MkdirAll("features/"+sdcVolume1, 0777)
		if err != nil {
			fmt.Printf("Couldn't make: %s\n", datadir+"/"+sdcVolume1)
		}
		err = gofsutil.Mount(context.Background(), nodePublishAltBlockDevPath, "features\\"+sdcVolume1, "none")
		if err != nil {
			fmt.Printf("Couldn't mount: %s\n", "features\\"+sdcVolume1)
		}
	case "NodePublishNoTargetPath":
		f.nodePublishVolumeRequest.TargetPath = ""
	case "NodePublishBadTargetPath":
		f.nodePublishVolumeRequest.TargetPath = badtarget
	case "NodePublishBlockTargetNotFile":
		f.nodePublishVolumeRequest.TargetPath = datadir
	case "NodePublishFileTargetNotDir":
		f.nodePublishVolumeRequest.TargetPath = datafile
	case "NodePublishPathAltDataDir":
		if f.nodePublishVolumeRequest.TargetPath == datadir {
			err := os.MkdirAll(altdatadir, 0777)
			if err != nil {
				fmt.Printf("Couldn't make altdatadir: %s\n", altdatadir)
			}
			f.nodePublishVolumeRequest.TargetPath = altdatadir
		} else {
			_, err := os.Create(altdatafile)
			if err != nil {
				fmt.Printf("Couldn't make datafile: %s\n", altdatafile)
			}
			f.nodePublishVolumeRequest.TargetPath = altdatafile
		}
	case "GOFSMockBindMountError":
		gofsutil.GOFSMock.InduceBindMountError = true
	case "GOFSMockDevMountsError":
		gofsutil.GOFSMock.InduceDevMountsError = true
	case "GOFSMockMountError":
		gofsutil.GOFSMock.InduceMountError = true
	case "GOFSMockGetMountsError":
		gofsutil.GOFSMock.InduceGetMountsError = true
	case "GOFSMockUnmountError":
		gofsutil.GOFSMock.InduceUnmountError = true
	case "GOFSMockGetDiskFormatError":
		gofsutil.GOFSMock.InduceGetDiskFormatError = true
	case "GOFSMockGetDiskFormatType":
		gofsutil.GOFSMock.InduceGetDiskFormatType = "unknown-fs"
	case "GOFSMockFormatError":
		gofsutil.GOFSMock.InduceFormatError = true
	case "GOFSInduceFSTypeError":
		gofsutil.GOFSMock.InduceFSTypeError = true
	case "GOFSInduceResizeFSError":
		gofsutil.GOFSMock.InduceResizeFSError = true
	case "NodeUnpublishNoTargetPath":
		f.nodePublishVolumeRequest.TargetPath = ""
	case "NodeUnpublishBadVolume":
		f.nodePublishVolumeRequest.VolumeId = sdcVolume0
	case "none":
		return nil
	default:
		return fmt.Errorf("Don't know how to induce error %q", errtype)
	}
	return nil
}

func (f *feature) getControllerPublishVolumeRequest(accessType string) *csi.ControllerPublishVolumeRequest {
	capability := new(csi.VolumeCapability)
	block := new(csi.VolumeCapability_Block)
	block.Block = new(csi.VolumeCapability_BlockVolume)
	if f.useAccessTypeMount {
		mountVolume := new(csi.VolumeCapability_MountVolume)
		mountVolume.FsType = "xfs"
		mountVolume.MountFlags = make([]string, 0)
		mount := new(csi.VolumeCapability_Mount)
		mount.Mount = mountVolume
		capability.AccessType = mount
	} else {
		capability.AccessType = block
	}
	accessMode := new(csi.VolumeCapability_AccessMode)
	switch accessType {
	case "multi-single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER
		break
	case "single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
		break
	case "multiple-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
		break
	case "multiple-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
		break
	case "unknown":
		accessMode.Mode = csi.VolumeCapability_AccessMode_UNKNOWN
		break
	}
	if !f.omitAccessMode {
		capability.AccessMode = accessMode
	}
	fmt.Printf("capability.AccessType %v\n", capability.AccessType)
	fmt.Printf("capability.AccessMode %v\n", capability.AccessMode)
	req := new(csi.ControllerPublishVolumeRequest)
	if !f.noVolumeID {
		if f.invalidVolumeID {
			req.VolumeId = "9999-9999"
		} else {
			req.VolumeId = goodVolumeID
		}
	}
	if !f.noNodeID {
		req.NodeId = goodNodeID
	}
	req.Readonly = false
	if !f.omitVolumeCapability {
		req.VolumeCapability = capability
	}
	return req
}

func (f *feature) getControllerListVolumesRequest(maxEntries int32, startingToken string) *csi.ListVolumesRequest {
	return &csi.ListVolumesRequest{
		MaxEntries:    maxEntries,
		StartingToken: startingToken,
	}
}

func (f *feature) getControllerDeleteVolumeRequest(accessType string) *csi.DeleteVolumeRequest {
	capability := new(csi.VolumeCapability)
	block := new(csi.VolumeCapability_Block)
	block.Block = new(csi.VolumeCapability_BlockVolume)
	if f.useAccessTypeMount {
		mountVolume := new(csi.VolumeCapability_MountVolume)
		mountVolume.FsType = "xfs"
		mountVolume.MountFlags = make([]string, 0)
		mount := new(csi.VolumeCapability_Mount)
		mount.Mount = mountVolume
		capability.AccessType = mount
	} else {
		capability.AccessType = block
	}
	accessMode := new(csi.VolumeCapability_AccessMode)
	switch accessType {
	case "single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
		break
	case "multiple-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
		break
	case "multiple-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
		break
	case "unknown":
		accessMode.Mode = csi.VolumeCapability_AccessMode_UNKNOWN
		break
	}
	if !f.omitAccessMode {
		capability.AccessMode = accessMode
	}
	fmt.Printf("capability.AccessType %v\n", capability.AccessType)
	fmt.Printf("capability.AccessMode %v\n", capability.AccessMode)
	req := new(csi.DeleteVolumeRequest)
	if !f.noVolumeID {
		if f.invalidVolumeID {
			req.VolumeId = "9999-9999"
		} else {
			req.VolumeId = goodVolumeID
		}
	}
	return req
}

func (f *feature) iCallPublishVolumeWith(arg1 string) error {
	ctx := new(context.Context)
	req := f.publishVolumeRequest
	if f.publishVolumeRequest == nil {
		req = f.getControllerPublishVolumeRequest(arg1)
		f.publishVolumeRequest = req
	}
	log.Printf("Calling controllerPublishVolume")
	f.publishVolumeResponse, f.err = f.service.ControllerPublishVolume(*ctx, req)
	if f.err != nil {
		log.Printf("PublishVolume call failed: %s\n", f.err.Error())
	}
	return nil
}

func (f *feature) aValidPublishVolumeResponseIsReturned() error {
	if f.err != nil {
		return errors.New("PublishVolume returned error: " + f.err.Error())
	}
	if f.publishVolumeResponse == nil {
		return errors.New("No PublishVolumeResponse returned")
	}
	for key, value := range f.publishVolumeResponse.PublishContext {
		fmt.Printf("PublishContext %s: %s", key, value)
	}
	return nil
}

func (f *feature) aValidVolume() error {
	volumeIDToName[goodVolumeID] = goodVolumeName
	volumeNameToID[goodVolumeName] = goodVolumeID
	return nil
}

func (f *feature) anInvalidVolume() error {
	f.invalidVolumeID = true
	return nil
}

func (f *feature) noVolume() error {
	f.noVolumeID = true
	return nil
}

func (f *feature) noNode() error {
	f.noNodeID = true
	return nil
}

func (f *feature) noVolumeCapability() error {
	f.omitVolumeCapability = true
	return nil
}

func (f *feature) noAccessMode() error {
	f.omitAccessMode = true
	return nil
}

func (f *feature) thenIUseADifferentNodeID() error {
	f.publishVolumeRequest.NodeId = altNodeID
	if f.unpublishVolumeRequest != nil {
		f.unpublishVolumeRequest.NodeId = altNodeID
	}
	return nil
}

func (f *feature) iUseAccessTypeMount() error {
	f.useAccessTypeMount = true
	return nil
}

func (f *feature) noErrorWasReceived() error {
	if f.err != nil {
		return f.err
	}
	return nil
}

func (f *feature) getControllerUnpublishVolumeRequest() *csi.ControllerUnpublishVolumeRequest {
	req := new(csi.ControllerUnpublishVolumeRequest)
	if !f.noVolumeID {
		if f.invalidVolumeID {
			req.VolumeId = "9999-9999"
		} else {
			req.VolumeId = goodVolumeID
		}
	}
	if !f.noNodeID {
		req.NodeId = goodNodeID
	}
	return req
}

func (f *feature) iCallUnpublishVolume() error {
	ctx := new(context.Context)
	req := f.unpublishVolumeRequest
	if f.unpublishVolumeRequest == nil {
		req = f.getControllerUnpublishVolumeRequest()
		f.unpublishVolumeRequest = req
	}
	log.Printf("Calling controllerUnpublishVolume: %s", req.VolumeId)
	f.unpublishVolumeResponse, f.err = f.service.ControllerUnpublishVolume(*ctx, req)
	if f.err != nil {
		log.Printf("UnpublishVolume call failed: %s\n", f.err.Error())
	}
	return nil
}

func (f *feature) aValidUnpublishVolumeResponseIsReturned() error {
	if f.unpublishVolumeResponse == nil {
		return errors.New("expected unpublishVolumeResponse (with no contents)but did not get one")
	}
	return nil
}

func (f *feature) theNumberOfSDCMappingsIs(arg1 int) error {
	if len(sdcMappings) != arg1 {
		return fmt.Errorf("expected %d SDC mappings but there were %d", arg1, len(sdcMappings))
	}
	return nil
}

func (f *feature) iCallNodeGetInfo() error {
	ctx := new(context.Context)
	req := new(csi.NodeGetInfoRequest)
	f.nodeGetInfoResponse, f.err = f.service.NodeGetInfo(*ctx, req)
	return nil
}

func (f *feature) aValidNodeGetInfoResponseIsReturned() error {
	if f.err != nil {
		return f.err
	}
	fmt.Printf("node: %s", f.nodeGetInfoResponse)
	if f.nodeGetInfoResponse.NodeId == "" {
		return errors.New("expected NodeGetInfoResponse to contain NodeID but it was null")
	}
	if f.nodeGetInfoResponse.MaxVolumesPerNode != 0 {
		return errors.New("expected NodeGetInfoResponse MaxVolumesPerNode to be 0")
	}
	fmt.Printf("NodeID %s\n", f.nodeGetInfoResponse.NodeId)
	return nil
}

func (f *feature) iCallDeleteVolumeWith(arg1 string) error {
	ctx := new(context.Context)
	req := f.deleteVolumeRequest
	if f.deleteVolumeRequest == nil {
		req = f.getControllerDeleteVolumeRequest(arg1)
		f.deleteVolumeRequest = req
	}
	log.Printf("Calling DeleteVolume")
	f.deleteVolumeResponse, f.err = f.service.DeleteVolume(*ctx, req)
	if f.err != nil {
		log.Printf("DeleteVolume called failed: %s\n", f.err.Error())
	}
	return nil
}

func (f *feature) aValidDeleteVolumeResponseIsReturned() error {
	if f.deleteVolumeResponse == nil {
		return errors.New("expected deleteVolumeResponse (with no contents)but did not get one")
	}
	return nil
}

func (f *feature) aValidListVolumesResponseIsReturned() error {
	if f.listVolumesResponse == nil {
		return errors.New("expected a non-nil listVolumesResponse, but it was nil")
	}
	return nil
}

func (f *feature) theVolumeIsAlreadyMappedToAnSDC() error {
	sdcMappings = append(sdcMappings, types.MappedSdcInfo{SdcIP: "1.1.1.1"})
	sdcMappings = append(sdcMappings, types.MappedSdcInfo{SdcIP: "2.2.2.2"})
	return nil
}

func (f *feature) iCallGetCapacityWithStoragePool(arg1 string) error {
	ctx := new(context.Context)
	req := new(csi.GetCapacityRequest)
	if arg1 != "" {
		parameters := make(map[string]string)
		parameters[KeyStoragePool] = arg1
		req.Parameters = parameters
	}
	log.Printf("Calling GetCapacity")
	f.getCapacityResponse, f.err = f.service.GetCapacity(*ctx, req)
	if f.err != nil {
		log.Printf("GetCapacity call failed: %s\n", f.err.Error())
		return nil
	}
	return nil
}

func (f *feature) aValidGetCapacityResponseIsReturned() error {
	if f.err != nil {
		return f.err
	}
	if f.getCapacityResponse.AvailableCapacity <= 0 {
		return errors.New("Expected AvailableCapacity to be positive")
	}
	fmt.Printf("Available capacity: %d\n", f.getCapacityResponse.AvailableCapacity)
	return nil
}

func (f *feature) iCallControllerGetCapabilities() error {
	ctx := new(context.Context)
	req := new(csi.ControllerGetCapabilitiesRequest)
	log.Printf("Calling ControllerGetCapabilities")
	f.controllerGetCapabilitiesResponse, f.err = f.service.ControllerGetCapabilities(*ctx, req)
	if f.err != nil {
		log.Printf("ControllerGetCapabilities call failed: %s\n", f.err.Error())
		return f.err
	}
	return nil
}

// parseListVolumesTable parses the given DataTable and ensures that it follows the
// format:
// | max_entries | starting_token |
// | <number>    | <string>       |
func parseListVolumesTable(dt *gherkin.DataTable) (int32, string, error) {
	if c := len(dt.Rows); c != 2 {
		return 0, "", fmt.Errorf("expected table with header row and single value row, got %d row(s)", c)
	}

	var (
		maxEntries    int32
		startingToken string
	)
	for i, v := range dt.Rows[0].Cells {
		switch h := v.Value; h {
		case "max_entries":
			str := dt.Rows[1].Cells[i].Value
			n, err := strconv.Atoi(str)
			if err != nil {
				return 0, "", fmt.Errorf("expected a valid number for max_entries, got %v", err)
			}
			maxEntries = int32(n)

		case "starting_token":
			startingToken = dt.Rows[1].Cells[i].Value
		default:
			return 0, "", fmt.Errorf(`want headers ["max_entries", "starting_token"], got %q`, h)
		}
	}

	return maxEntries, startingToken, nil
}

// iCallListVolumesAgainWith nils out the previous request before delegating
// to iCallListVolumesWith with the same table data.  This simulates multiple
// calls to ListVolume for the purpose of testing the pagination token.
func (f *feature) iCallListVolumesAgainWith(dt *gherkin.DataTable) error {
	f.listVolumesRequest = nil
	return f.iCallListVolumesWith(dt)
}

func (f *feature) iCallListVolumesWith(dt *gherkin.DataTable) error {
	maxEntries, startingToken, err := parseListVolumesTable(dt)
	if err != nil {
		return err
	}

	ctx := new(context.Context)
	req := f.listVolumesRequest
	if f.listVolumesRequest == nil {
		switch st := startingToken; st {
		case "none":
			startingToken = ""
		case "next":
			startingToken = f.listVolumesNextTokenCache
		case "invalid":
			startingToken = "invalid-token"
		case "larger":
			startingToken = "9999"
		default:
			return fmt.Errorf(`want start token of "next", "none", "invalid", "larger", got %q`, st)
		}
		req = f.getControllerListVolumesRequest(maxEntries, startingToken)
		f.listVolumesRequest = req
	}
	log.Printf("Calling ListVolumes with req=%+v", f.listVolumesRequest)
	f.listVolumesResponse, f.err = f.service.ListVolumes(*ctx, req)
	if f.err != nil {
		log.Printf("ListVolume called failed: %s\n", f.err.Error())
	} else {
		f.listVolumesNextTokenCache = f.listVolumesResponse.NextToken
	}
	return nil
}

func (f *feature) aValidControllerGetCapabilitiesResponseIsReturned() error {
	rep := f.controllerGetCapabilitiesResponse
	if rep != nil {
		if rep.Capabilities == nil {
			return errors.New("no capabilities returned in ControllerGetCapabilitiesResponse")
		}
		count := 0
		for _, cap := range rep.Capabilities {
			typex := cap.GetRpc().Type
			switch typex {
			case csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME:
				count = count + 1
			case csi.ControllerServiceCapability_RPC_PUBLISH_UNPUBLISH_VOLUME:
				count = count + 1
			case csi.ControllerServiceCapability_RPC_LIST_VOLUMES:
				count = count + 1
			case csi.ControllerServiceCapability_RPC_GET_CAPACITY:
				count = count + 1
			case csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT:
				count = count + 1
			case csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS:
				count = count + 1
			case csi.ControllerServiceCapability_RPC_EXPAND_VOLUME:
				count = count + 1
			case csi.ControllerServiceCapability_RPC_CLONE_VOLUME:
				count = count + 1
			default:
				return fmt.Errorf("received unexpected capability: %v", typex)
			}
		}
		if count != 8 {
			return errors.New("Did not retrieve all the expected capabilities")
		}
		return nil
	}

	return errors.New("expected ControllerGetCapabilitiesResponse but didn't get one")

}

func (f *feature) iCallCloneVolume() error {
	ctx := new(context.Context)
	req := getTypicalCreateVolumeRequest()
	req.Name = "clone"

	if f.wrongCapacity {
		req.CapacityRange.RequiredBytes = 64 * 1024 * 1024 * 1024
	}

	if f.wrongStoragePool {
		req.Parameters["storagepool"] = "bad storage pool"
	}

	source := &csi.VolumeContentSource_VolumeSource{VolumeId: goodVolumeID}
	req.VolumeContentSource = new(csi.VolumeContentSource)
	req.VolumeContentSource.Type = &csi.VolumeContentSource_Volume{Volume: source}
	f.createVolumeResponse, f.err = f.service.CreateVolume(*ctx, req)
	if f.err != nil {
		fmt.Printf("Error on CreateVolume from volume: %s\n", f.err.Error())
	}

	return nil
}

func (f *feature) iCallValidateVolumeCapabilitiesWithVoltypeAccessFstype(voltype, access, fstype string) error {
	ctx := new(context.Context)
	req := new(csi.ValidateVolumeCapabilitiesRequest)
	if f.invalidVolumeID || f.createVolumeResponse == nil {
		req.VolumeId = "000-000"
	} else {
		req.VolumeId = f.createVolumeResponse.GetVolume().VolumeId
	}
	// Construct the volume capabilities
	capability := new(csi.VolumeCapability)
	switch voltype {
	case "block":
		block := new(csi.VolumeCapability_BlockVolume)
		accessType := new(csi.VolumeCapability_Block)
		accessType.Block = block
		capability.AccessType = accessType
	case "mount":
		mount := new(csi.VolumeCapability_MountVolume)
		accessType := new(csi.VolumeCapability_Mount)
		accessType.Mount = mount
		capability.AccessType = accessType
	}
	accessMode := new(csi.VolumeCapability_AccessMode)
	switch access {
	case "single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	case "multi-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
	case "multi-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
	case "multi-node-single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER
	}
	capability.AccessMode = accessMode
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities
	log.Printf("Calling ValidateVolumeCapabilities")
	f.validateVolumeCapabilitiesResponse, f.err = f.service.ValidateVolumeCapabilities(*ctx, req)
	if f.err != nil {
		return nil
	}
	if f.validateVolumeCapabilitiesResponse.Message != "" {
		f.err = errors.New(f.validateVolumeCapabilitiesResponse.Message)
	} else {
		// Validate we get a Confirmed structure with VolumeCapabilities
		if f.validateVolumeCapabilitiesResponse.Confirmed == nil {
			return errors.New("Expected ValidateVolumeCapabilities to have a Confirmed structure but it did not")
		}
		confirmed := f.validateVolumeCapabilitiesResponse.Confirmed
		if len(confirmed.VolumeCapabilities) <= 0 {
			return errors.New("Expected ValidateVolumeCapabilities to return the confirmed VolumeCapabilities but it did not")
		}
	}
	return nil
}

// thereAreValidVolumes creates the requested number of volumes
// for the test scenario, using a suffix.
func (f *feature) thereAreValidVolumes(n int) error {
	idTemplate := "111-11%d"
	nameTemplate := "vol%d"
	for i := 0; i < n; i++ {
		name := fmt.Sprintf(nameTemplate, i)
		id := fmt.Sprintf(idTemplate, i)
		volumeIDToName[id] = id
		volumeNameToID[name] = name
	}
	return nil
}

func (f *feature) volumesAreListed(expected int) error {
	if f.listVolumesResponse == nil {
		return fmt.Errorf("expected a non-nil list volume response, but got nil")
	}

	if actual := len(f.listVolumesResponse.Entries); actual != expected {
		return fmt.Errorf("expected %d volumes to have been listed, got %d", expected, actual)
	}
	return nil
}

func (f *feature) anInvalidListVolumesResponseIsReturned() error {
	if f.err == nil {
		return fmt.Errorf("expected error response, but couldn't find it")
	}
	return nil
}

func (f *feature) aCapabilityWithVoltypeAccessFstype(voltype, access, fstype string) error {
	// Construct the volume capabilities
	capability := new(csi.VolumeCapability)
	switch voltype {
	case "block":
		blockVolume := new(csi.VolumeCapability_BlockVolume)
		block := new(csi.VolumeCapability_Block)
		block.Block = blockVolume
		capability.AccessType = block
	case "mount":
		mountVolume := new(csi.VolumeCapability_MountVolume)
		mountVolume.FsType = fstype
		mountVolume.MountFlags = make([]string, 0)
		mount := new(csi.VolumeCapability_Mount)
		mount.Mount = mountVolume
		capability.AccessType = mount
	}
	accessMode := new(csi.VolumeCapability_AccessMode)
	switch access {
	case "single-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY
	case "single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	case "multiple-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
	case "multiple-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
	case "multiple-node-single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER
	}
	capability.AccessMode = accessMode
	f.capabilities = make([]*csi.VolumeCapability, 0)
	f.capabilities = append(f.capabilities, capability)
	f.capability = capability
	return nil
}

func (f *feature) aVolumeRequest(name string, size int64) error {
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	params["storagepool"] = "viki_pool_HDD_20181031"
	params["thickprovisioning"] = "true"
	req.Parameters = params
	req.Name = name
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = size * 1024 * 1024 * 1024
	req.CapacityRange = capacityRange
	req.VolumeCapabilities = f.capabilities
	f.createVolumeRequest = req
	return nil
}

func (f *feature) aControllerPublishedVolume() error {
	fmt.Printf("setting up dev directory, block device, and symlink\n")
	// Make the directories; on Windows these show up in C:/dev/...
	_, err := os.Stat(nodePublishSymlinkDir)
	if err != nil {
		err = os.MkdirAll(nodePublishSymlinkDir, 0777)
		if err != nil {
			fmt.Printf("by-id: " + err.Error())
		}
	}

	// Remove the private staging directory directory
	cmd := exec.Command("rm", "-rf", "features/"+sdcVolume1)
	_, err = cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("error removing private staging directory\n")
	} else {
		fmt.Printf("removed private staging directory\n")
	}

	// Make the block device
	_, err = os.Stat(nodePublishBlockDevicePath)
	if err != nil {
		cmd := exec.Command("mknod", nodePublishBlockDevicePath, "b", "0", "0")
		output, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("scinia: %s\n", err.Error())
		}
		fmt.Printf("mknod output: %s\n", output)

		// Make the symlink
		cmdstring := fmt.Sprintf("cd %s; ln -s ../../scinia emc-vol-%s-%s", nodePublishSymlinkDir, mdmID, sdcVolume1)
		cmd = exec.Command("sh", "-c", cmdstring)
		output, err = cmd.CombinedOutput()
		fmt.Printf("symlink output: %s\n", output)
		if err != nil {
			fmt.Printf("link: %s\n", err.Error())
			err = nil
		}
	}

	// Make the target directory if required
	_, err = os.Stat(datadir)
	if err != nil {
		err = os.MkdirAll(datadir, 0777)
		if err != nil {
			fmt.Printf("Couldn't make datadir: %s\n", datadir)
		}
	}

	// Make the target file if required
	_, err = os.Stat(datafile)
	if err != nil {
		file, err := os.Create(datafile)
		if err != nil {
			fmt.Printf("Couldn't make datafile: %s\n", datafile)
		} else {
			file.Close()
		}
	}

	// Set variable in goscaleio that dev is in a different place.
	goscaleio.FSDevDirectoryPrefix = "test"
	// Empty WindowsMounts in gofsutil
	gofsutil.GOFSMockMounts = gofsutil.GOFSMockMounts[:0]
	// Set variables in mount for unit testing
	unitTestEmulateBlockDevice = true
	return nil
}

func (f *feature) getNodePublishVolumeRequest() error {
	req := new(csi.NodePublishVolumeRequest)
	req.VolumeId = sdcVolume1
	req.Readonly = false
	req.VolumeCapability = f.capability
	block := f.capability.GetBlock()
	if block != nil {
		req.TargetPath = datafile
	}
	mount := f.capability.GetMount()
	if mount != nil {
		req.TargetPath = datadir
	}

	f.nodePublishVolumeRequest = req
	return nil
}

func (f *feature) iGiveRequestVolumeContext() error {

	volContext := map[string]string{
		"id2USE": f.nodePublishVolumeRequest.VolumeId}

	f.nodePublishVolumeRequest.VolumeContext = volContext
	return nil
}

func (f *feature) iMarkRequestReadOnly() error {
	f.nodePublishVolumeRequest.Readonly = true
	return nil
}

func (f *feature) iCallNodePublishVolume(arg1 string) error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := f.nodePublishVolumeRequest
	if req == nil {
		_ = f.getNodePublishVolumeRequest()
		req = f.nodePublishVolumeRequest
	}
	fmt.Printf("Calling NodePublishVolume\n")
	_, err := f.service.NodePublishVolume(ctx, req)
	if err != nil {
		fmt.Printf("NodePublishVolume failed: %s\n", err.Error())
		if f.err == nil {
			f.err = err
		}
	} else {
		fmt.Printf("NodePublishVolume completed successfully\n")
	}
	return nil
}

func (f *feature) iCallNodeUnpublishVolume(arg1 string) error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := new(csi.NodeUnpublishVolumeRequest)
	req.VolumeId = f.nodePublishVolumeRequest.VolumeId
	req.TargetPath = f.nodePublishVolumeRequest.TargetPath
	fmt.Printf("Calling NodeUnpublishVolume\n")
	_, err := f.service.NodeUnpublishVolume(ctx, req)
	if err != nil {
		fmt.Printf("NodeUnpublishVolume failed: %s\n", err.Error())
		if f.err == nil {
			f.err = err
		}
	} else {
		fmt.Printf("NodeUnpublishVolume completed successfully\n")
	}
	return nil
}

func (f *feature) thereAreNoRemainingMounts() error {
	if len(gofsutil.GOFSMockMounts) > 0 {
		return errors.New("expected all mounts to be removed but one or more remained")
	}
	return nil
}

func (f *feature) iCallBeforeServe() error {
	ctxOSEnviron := interface{}("os.Environ")
	stringSlice := make([]string, 0)
	stringSlice = append(stringSlice, EnvEndpoint+"=unix_sock")
	stringSlice = append(stringSlice, EnvUser+"=admin")
	stringSlice = append(stringSlice, EnvPassword+"=password")
	stringSlice = append(stringSlice, EnvSystemName+"=unknown")
	stringSlice = append(stringSlice, "X_CSI_PRIVATE_MOUNT_DIR=/csi")
	stringSlice = append(stringSlice, "X_CSI_VXFLEXOS_ENABLESNAPSHOTCGDELETE=true")
	stringSlice = append(stringSlice, "X_CSI_VXFLEXOS_ENABLELISTVOLUMESNAPSHOTS=true")
	ctx := context.WithValue(context.Background(), ctxOSEnviron, stringSlice)
	listener, err := net.Listen("tcp", "127.0.0.1:65000")
	if err != nil {
		return err
	}
	f.err = f.service.BeforeServe(ctx, nil, listener)
	listener.Close()
	return nil
}

func (f *feature) iCallNodeStageVolume() error {
	ctx := new(context.Context)
	req := new(csi.NodeStageVolumeRequest)
	_, f.err = f.service.NodeStageVolume(*ctx, req)
	return nil
}

func (f *feature) iCallControllerExpandVolume(size int64) error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	f.volumeID = f.createVolumeResponse.GetVolume().VolumeId
	req := &csi.ControllerExpandVolumeRequest{
		VolumeId:      f.volumeID,
		CapacityRange: &csi.CapacityRange{RequiredBytes: size * bytesInKiB * bytesInKiB * bytesInKiB},
	}
	if stepHandlersErrors.NoVolumeIDError {
		req.VolumeId = ""
	}
	_, f.err = f.service.ControllerExpandVolume(ctx, req)
	return nil
}

func (f *feature) iCallNodeExpandVolume(volPath string) error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	f.volumeID = f.createVolumeResponse.Volume.VolumeId
	req := &csi.NodeExpandVolumeRequest{
		VolumeId:   sdcVolume1,
		VolumePath: volPath,
	}
	if stepHandlersErrors.NoVolumeIDError {
		req.VolumeId = ""
	}
	_, f.err = f.service.NodeExpandVolume(ctx, req)
	return nil
}

func (f *feature) iCallNodeGetVolumeStats() error {
	ctx := new(context.Context)
	req := new(csi.NodeGetVolumeStatsRequest)
	_, f.err = f.service.NodeGetVolumeStats(*ctx, req)
	return nil
}

func (f *feature) iCallNodeUnstageVolume() error {
	ctx := new(context.Context)
	req := new(csi.NodeUnstageVolumeRequest)
	_, f.err = f.service.NodeUnstageVolume(*ctx, req)
	return nil
}

func (f *feature) iCallNodeGetCapabilities() error {
	ctx := new(context.Context)
	req := new(csi.NodeGetCapabilitiesRequest)
	f.nodeGetCapabilitiesResponse, f.err = f.service.NodeGetCapabilities(*ctx, req)
	return nil
}

func (f *feature) aValidNodeGetCapabilitiesResponseIsReturned() error {
	if f.err != nil {
		return f.err
	}
	rep := f.nodeGetCapabilitiesResponse
	if rep != nil {
		if rep.Capabilities == nil {
			return errors.New("no capabilities returned in NodeGetCapabilitiesResponse")
		}
		count := 0
		for _, cap := range rep.Capabilities {
			typex := cap.GetRpc().Type
			switch typex {
			case csi.NodeServiceCapability_RPC_EXPAND_VOLUME:
				count = count + 1
			default:
				return fmt.Errorf("received unxexpcted capability: %v", typex)
			}
		}
		if count != 1 {
			return errors.New("Did not retrieve all the expected capabilities")
		}
		return nil
	}
	return errors.New("expected NodeGetCapabilitiesResponse but didn't get one")
}

func (f *feature) iCallCreateSnapshot(snapName string) error {
	ctx := new(context.Context)

	if len(f.volumeIDList) == 0 {
		f.volumeIDList = append(f.volumeIDList, "00000000")
	}
	req := &csi.CreateSnapshotRequest{
		SourceVolumeId: f.volumeIDList[0],
		Name:           snapName,
	}

	if stepHandlersErrors.WrongVolIDError {
		req.SourceVolumeId = f.volumeIDList[1]
	}

	if f.invalidVolumeID {
		req.SourceVolumeId = "00000000"
	} else if f.noVolumeID {
		req.SourceVolumeId = ""
	} else if len(f.volumeIDList) > 1 {
		req.Parameters = make(map[string]string)
		stringList := ""
		for _, v := range f.volumeIDList {
			if stringList == "" {
				stringList = v
			} else {
				stringList = stringList + "," + v
			}
		}
		req.Parameters[VolumeIDList] = stringList
	}

	fmt.Println("snapName is: ", snapName)
	fmt.Println("ctx: ", *ctx)
	fmt.Println("req: ", req)

	f.createSnapshotResponse, f.err = f.service.CreateSnapshot(*ctx, req)
	return nil
}

func (f *feature) aValidCreateSnapshotResponseIsReturned() error {
	if f.err != nil {
		return f.err
	}
	if f.createSnapshotResponse == nil {
		return errors.New("Expected CreateSnapshotResponse to be returned")
	}
	return nil
}

func (f *feature) aValidSnapshot() error {
	volumeIDToName[goodSnapID] = "snap4"
	volumeNameToID["snap4"] = goodSnapID
	volumeIDToAncestorID[goodSnapID] = goodVolumeID
	return nil
}

func (f *feature) iCallDeleteSnapshot() error {
	ctx := new(context.Context)
	req := &csi.DeleteSnapshotRequest{SnapshotId: goodSnapID, Secrets: make(map[string]string)}
	req.Secrets["x"] = "y"
	if f.invalidVolumeID {
		req.SnapshotId = "00000000"
	} else if f.noVolumeID {
		req.SnapshotId = ""
	}
	_, f.err = f.service.DeleteSnapshot(*ctx, req)
	return nil
}

func (f *feature) aValidSnapshotConsistencyGroup() error {
	// first snapshot in CG
	volumeIDToName[goodSnapID] = "snap4"
	volumeNameToID["snap4"] = goodSnapID
	volumeIDToAncestorID[goodSnapID] = goodVolumeID
	volumeIDToConsistencyGroupID[goodSnapID] = goodVolumeID

	// second snapshot in CG; this looks weird, but we give same ID to snap
	// as it's ancestor so that we can publish the volume
	volumeIDToName[altSnapID] = "snap5"
	volumeNameToID["snap5"] = altSnapID
	volumeIDToAncestorID[altSnapID] = altVolumeID
	volumeIDToConsistencyGroupID[altSnapID] = goodVolumeID

	// only return the SDC mappings on the altSnapID
	req := f.getControllerPublishVolumeRequest("single-writer")
	req.VolumeId = altSnapID
	f.publishVolumeRequest = req
	sdcMappingsID = altSnapID
	return nil
}

func (f *feature) iCallCreateVolumeFromSnapshot() error {
	ctx := new(context.Context)
	req := getTypicalCreateVolumeRequest()
	req.Name = "volumeFromSnap"
	if f.wrongCapacity {
		req.CapacityRange.RequiredBytes = 64 * 1024 * 1024 * 1024
	}
	if f.wrongStoragePool {
		req.Parameters["storagepool"] = "bad storage pool"
	}
	source := &csi.VolumeContentSource_SnapshotSource{SnapshotId: goodSnapID}
	req.VolumeContentSource = new(csi.VolumeContentSource)
	req.VolumeContentSource.Type = &csi.VolumeContentSource_Snapshot{Snapshot: source}
	f.createVolumeResponse, f.err = f.service.CreateVolume(*ctx, req)
	if f.err != nil {
		fmt.Printf("Error on CreateVolume from snap: %s\n", f.err.Error())
	}
	return nil
}

func (f *feature) theWrongCapacity() error {
	f.wrongCapacity = true
	return nil
}

func (f *feature) theWrongStoragePool() error {
	f.wrongStoragePool = true
	return nil
}

// Every increasing int used to generate unique snapshot indexes

func (f *feature) thereAreValidSnapshotsOfVolume(nsnapshots int, volume string) error {
	volumeID := goodVolumeID
	if volume == "alt" {
		volumeID = altVolumeID
	}
	end := f.snapshotIndex + nsnapshots
	for ; f.snapshotIndex < end; f.snapshotIndex++ {
		name := fmt.Sprintf("snap%d", f.snapshotIndex)
		id := fmt.Sprintf("0000-%d", f.snapshotIndex)
		volumeIDToName[id] = name
		volumeNameToID[name] = id
		volumeIDToAncestorID[id] = volumeID
	}
	return nil
}

func (f *feature) iCallListSnapshotsWithMaxentriesAndStartingtoken(maxEntriesString, startingTokenString string) error {
	maxEntries, err := strconv.Atoi(maxEntriesString)
	if err != nil {
		return nil
	}
	ctx := new(context.Context)
	req := &csi.ListSnapshotsRequest{MaxEntries: int32(maxEntries), StartingToken: startingTokenString}
	f.listSnapshotsRequest = req
	log.Printf("Calling ListSnapshots with req=%+v", f.listVolumesRequest)
	f.listSnapshotsResponse, f.err = f.service.ListSnapshots(*ctx, req)
	if f.err != nil {
		log.Printf("ListSnapshots called failed: %s\n", f.err.Error())
	}
	return nil
}

func (f *feature) iCallListSnapshotsForVolume(arg1 string) error {
	sourceVolumeID := goodVolumeID
	if arg1 == "alt" {
		sourceVolumeID = altVolumeID
	}

	ctx := new(context.Context)
	req := &csi.ListSnapshotsRequest{SourceVolumeId: sourceVolumeID}

	if stepHandlersErrors.BadVolIDError {
		req.SourceVolumeId = "Not at all valid"
		req.SnapshotId = "111-111"
	}

	f.listSnapshotsRequest = req
	log.Printf("Calling ListSnapshots with req=%+v", f.listVolumesRequest)
	f.listSnapshotsResponse, f.err = f.service.ListSnapshots(*ctx, req)
	if f.err != nil {
		log.Printf("ListSnapshots called failed: %s\n", f.err.Error())
	}
	return nil
}

func (f *feature) iCallListSnapshotsForSnapshot(arg1 string) error {
	ctx := new(context.Context)
	req := &csi.ListSnapshotsRequest{SnapshotId: arg1}
	f.listSnapshotsRequest = req
	log.Printf("Calling ListSnapshots with req=%+v", f.listVolumesRequest)
	f.listSnapshotsResponse, f.err = f.service.ListSnapshots(*ctx, req)
	if f.err != nil {
		log.Printf("ListSnapshots called failed: %s\n", f.err.Error())
	}
	return nil
}

func (f *feature) theSnapshotIDIs(arg1 string) error {
	if len(f.listedVolumeIDs) != 1 {
		return errors.New("Expected only 1 volume to be listed")
	}
	if f.listedVolumeIDs[arg1] == false {
		return errors.New("Expected volume was not found")
	}
	return nil
}

func (f *feature) aValidListSnapshotsResponseIsReturnedWithListedAndNexttoken(listed, nextTokenString string) error {
	if f.err != nil {
		return f.err
	}
	nextToken := f.listSnapshotsResponse.GetNextToken()
	if nextToken != nextTokenString {
		return fmt.Errorf("Expected nextToken %s got %s", nextTokenString, nextToken)
	}
	entries := f.listSnapshotsResponse.GetEntries()
	expectedEntries, err := strconv.Atoi(listed)
	if err != nil {
		return err
	}
	if entries == nil || len(entries) != expectedEntries {
		return fmt.Errorf("Expected %d List SnapshotResponse entries but got %d", expectedEntries, len(entries))
	}
	for j := 0; j < expectedEntries; j++ {
		entry := entries[j]
		id := entry.GetSnapshot().SnapshotId
		if expectedEntries <= 10 {
			ts := ptypes.TimestampString(entry.GetSnapshot().CreationTime)
			fmt.Printf("snapshot ID %s source ID %s timestamp %s\n", id, entry.GetSnapshot().SourceVolumeId, ts)
		}
		if f.listedVolumeIDs[id] {
			return errors.New("Got duplicate snapshot ID: " + id)
		}
		f.listedVolumeIDs[id] = true
	}
	fmt.Printf("Total snapshots received: %d\n", len(f.listedVolumeIDs))
	return nil
}

func (f *feature) theTotalSnapshotsListedIs(arg1 string) error {
	expectedSnapshots, err := strconv.Atoi(arg1)
	if err != nil {
		return err
	}
	if len(f.listedVolumeIDs) != expectedSnapshots {
		return fmt.Errorf("expected %d snapshots to be listed but got %d", expectedSnapshots, len(f.listedVolumeIDs))
	}
	return nil
}

func (f *feature) iInvalidateTheProbeCache() error {

	if stepHandlersErrors.NoEndpointError {
		f.service.opts.Endpoint = ""
		f.service.opts.AutoProbe = true
		return nil
	} else if stepHandlersErrors.NoUserError {
		f.service.opts.User = ""
	} else if stepHandlersErrors.NoPasswordError {
		f.service.opts.Password = ""
	} else if stepHandlersErrors.NoSysNameError {
		f.service.opts.SystemName = ""
	} else if stepHandlersErrors.WrongSysNameError {
		f.service.opts.SystemName = "WrongSystemName"
	} else {
		f.service.adminClient = nil
		f.service.system = nil
	}

	return nil
}

func (f *feature) iCallEvalsymlink(path string) error {

	d := evalSymlinks(path)
	if d == path {
		f.err = errors.New("Could not evaluate symlinks for path")
	}
	return nil
}

func (f *feature) iCallGetDevice(Path string) error {
	device, err := GetDevice(Path)
	if device == nil && err != nil {
		f.err = errors.New("invalid path error")
	}
	return nil
}

func (f *feature) iCallNewService() error {
	return nil
}

func (f *feature) aNewServiceIsReturned() error {
	svc, ok := New().(Service)
	if !ok || svc == nil {
		return errors.New("Service New does not return properly")
	}

	return nil
}

func (f *feature) iCallGetVolProvisionTypeWithBadParams() error {
	params := map[string]string{KeyThickProvisioning: "notBoolean"}

	if tp, ok := params[KeyThickProvisioning]; ok {
		_, err := strconv.ParseBool(tp)
		if err != nil {
			f.err = errors.New("getVolProvisionType - invalid boolean received")
		}
	}

	f.service.getVolProvisionType(params)

	return nil
}

func (f *feature) iCallGetStoragePoolnameByID(id string) error {
	f.service.storagePoolIDToName[id] = ""
	res := f.service.getStoragePoolNameFromID(id)
	if res == "" {
		f.err = errors.New("cannot find storage pool")
	}
	return nil
}

func (f *feature) iCallNodeGetAllSystems() error {
	// lookup the system names for a couple of systems
	// This should not generate an error as systems without names are supported
	systems := make([]string, 0)
	systems = append(systems, "14dbbf5617523654")
	systems = append(systems, "9999999999999999")
	f.err = f.service.getAllSystems(context.TODO(), systems)
	return nil
}

func (f *feature) iDoNotHaveAGatewayConnection() error {
	f.service.adminClient = nil
	return nil
}

func (f *feature) iDoNotHaveAValidGatewayEndpoint() error {
	f.service.opts.Endpoint = ""
	return nil
}

func (f *feature) iDoNotHaveAValidGatewayPassword() error {
	f.service.opts.Password = ""
	return nil
}

func FeatureContext(s *godog.Suite) {
	f := &feature{}
	s.Step(`^a VxFlexOS service$`, f.aVxFlexOSService)
	s.Step(`^I call GetPluginInfo$`, f.iCallGetPluginInfo)
	s.Step(`^a valid GetPlugInfoResponse is returned$`, f.aValidGetPlugInfoResponseIsReturned)
	s.Step(`^I call GetPluginCapabilities$`, f.iCallGetPluginCapabilities)
	s.Step(`^a valid GetPluginCapabilitiesResponse is returned$`, f.aValidGetPluginCapabilitiesResponseIsReturned)
	s.Step(`^a (?:VxFlexOS|VxFlex OS) service$`, f.aVxFlexOSService)
	s.Step(`^I call GetPluginInfo$`, f.iCallGetPluginInfo)
	s.Step(`^a valid GetPlugInfoResponse is returned$`, f.aValidGetPlugInfoResponseIsReturned)
	s.Step(`^I call GetPluginCapabilities$`, f.iCallGetPluginCapabilities)
	s.Step(`^a valid GetPluginCapabilitiesResponse is returned$`, f.aValidGetPluginCapabilitiesResponseIsReturned)
	s.Step(`^I call Probe$`, f.iCallProbe)
	s.Step(`^a valid ProbeResponse is returned$`, f.aValidProbeResponseIsReturned)
	s.Step(`^the error contains "([^"]*)"$`, f.theErrorContains)
	s.Step(`^the possible error contains "([^"]*)"$`, f.thePossibleErrorContains)
	s.Step(`^the Controller has no connection$`, f.theControllerHasNoConnection)
	s.Step(`^there is a Node Probe Lsmod error$`, f.thereIsANodeProbeLsmodError)
	s.Step(`^there is a Node Probe SdcGUID error$`, f.thereIsANodeProbeSdcGUIDError)
	s.Step(`^there is a Node Probe drvCfg error$`, f.thereIsANodeProbeDrvCfgError)
	s.Step(`^I call CreateVolume "([^"]*)"$`, f.iCallCreateVolume)
	s.Step(`^a valid CreateVolumeResponse is returned$`, f.aValidCreateVolumeResponseIsReturned)
	s.Step(`^I specify AccessibilityRequirements with a SystemID of "([^"]*)"$`, f.iSpecifyAccessibilityRequirementsWithASystemIDOf)
	s.Step(`^I specify MULTINODE_WRITER$`, f.iSpecifyMULTINODEWRITER)
	s.Step(`^I specify a BadCapacity$`, f.iSpecifyABadCapacity)
	s.Step(`^I specify NoStoragePool$`, f.iSpecifyNoStoragePool)
	s.Step(`^I call CreateVolumeSize "([^"]*)" "(\d+)"$`, f.iCallCreateVolumeSize)
	s.Step(`^I change the StoragePool "([^"]*)"$`, f.iChangeTheStoragePool)
	s.Step(`^I induce error "([^"]*)"$`, f.iInduceError)
	s.Step(`^I specify VolumeContentSource$`, f.iSpecifyVolumeContentSource)
	s.Step(`^I specify CreateVolumeMountRequest "([^"]*)"$`, f.iSpecifyCreateVolumeMountRequest)
	s.Step(`^I call PublishVolume with "([^"]*)"$`, f.iCallPublishVolumeWith)
	s.Step(`^a valid PublishVolumeResponse is returned$`, f.aValidPublishVolumeResponseIsReturned)
	s.Step(`^a valid volume$`, f.aValidVolume)
	s.Step(`^an invalid volume$`, f.anInvalidVolume)
	s.Step(`^no volume$`, f.noVolume)
	s.Step(`^no node$`, f.noNode)
	s.Step(`^no volume capability$`, f.noVolumeCapability)
	s.Step(`^no access mode$`, f.noAccessMode)
	s.Step(`^then I use a different nodeID$`, f.thenIUseADifferentNodeID)
	s.Step(`^I use AccessType Mount$`, f.iUseAccessTypeMount)
	s.Step(`^no error was received$`, f.noErrorWasReceived)
	s.Step(`^I call UnpublishVolume$`, f.iCallUnpublishVolume)
	s.Step(`^a valid UnpublishVolumeResponse is returned$`, f.aValidUnpublishVolumeResponseIsReturned)
	s.Step(`^the number of SDC mappings is (\d+)$`, f.theNumberOfSDCMappingsIs)
	s.Step(`^I call NodeGetInfo$`, f.iCallNodeGetInfo)
	s.Step(`^a valid NodeGetInfoResponse is returned$`, f.aValidNodeGetInfoResponseIsReturned)
	s.Step(`^I call DeleteVolume with "([^"]*)"$`, f.iCallDeleteVolumeWith)
	s.Step(`^a valid DeleteVolumeResponse is returned$`, f.aValidDeleteVolumeResponseIsReturned)
	s.Step(`^the volume is already mapped to an SDC$`, f.theVolumeIsAlreadyMappedToAnSDC)
	s.Step(`^I call GetCapacity with storage pool "([^"]*)"$`, f.iCallGetCapacityWithStoragePool)
	s.Step(`^a valid GetCapacityResponse is returned$`, f.aValidGetCapacityResponseIsReturned)
	s.Step(`^I call ControllerGetCapabilities$`, f.iCallControllerGetCapabilities)
	s.Step(`^a valid ControllerGetCapabilitiesResponse is returned$`, f.aValidControllerGetCapabilitiesResponseIsReturned)
	s.Step(`^I call ValidateVolumeCapabilities with voltype "([^"]*)" access "([^"]*)" fstype "([^"]*)"$`, f.iCallValidateVolumeCapabilitiesWithVoltypeAccessFstype)
	s.Step(`^a valid ListVolumesResponse is returned$`, f.aValidListVolumesResponseIsReturned)
	s.Step(`^I call(?:ed)? ListVolumes with$`, f.iCallListVolumesWith)
	s.Step(`^I call(?:ed)? ListVolumes again with$`, f.iCallListVolumesAgainWith)
	s.Step(`^there (?:are|is) (\d+) valid volumes?$`, f.thereAreValidVolumes)
	s.Step(`^(\d+) volume(?:s)? (?:are|is) listed$`, f.volumesAreListed)
	s.Step(`^an invalid ListVolumesResponse is returned$`, f.anInvalidListVolumesResponseIsReturned)
	s.Step(`^a capability with voltype "([^"]*)" access "([^"]*)" fstype "([^"]*)"$`, f.aCapabilityWithVoltypeAccessFstype)
	s.Step(`^a controller published volume$`, f.aControllerPublishedVolume)
	s.Step(`^I call NodePublishVolume "([^"]*)"$`, f.iCallNodePublishVolume)
	s.Step(`^get Node Publish Volume Request$`, f.getNodePublishVolumeRequest)
	s.Step(`^I mark request read only$`, f.iMarkRequestReadOnly)
	s.Step(`^I call NodeUnpublishVolume "([^"]*)"$`, f.iCallNodeUnpublishVolume)
	s.Step(`^there are no remaining mounts$`, f.thereAreNoRemainingMounts)
	s.Step(`^I call BeforeServe$`, f.iCallBeforeServe)
	s.Step(`^I call NodeStageVolume$`, f.iCallNodeStageVolume)
	s.Step(`^I call NodeUnstageVolume$`, f.iCallNodeUnstageVolume)
	s.Step(`^I call NodeGetCapabilities$`, f.iCallNodeGetCapabilities)
	s.Step(`^a valid NodeGetCapabilitiesResponse is returned$`, f.aValidNodeGetCapabilitiesResponseIsReturned)
	s.Step(`^I call CreateSnapshot "([^"]*)"$`, f.iCallCreateSnapshot)
	s.Step(`^a valid CreateSnapshotResponse is returned$`, f.aValidCreateSnapshotResponseIsReturned)
	s.Step(`^a valid snapshot$`, f.aValidSnapshot)
	s.Step(`^I call DeleteSnapshot$`, f.iCallDeleteSnapshot)
	s.Step(`^a valid snapshot consistency group$`, f.aValidSnapshotConsistencyGroup)
	s.Step(`^I call Create Volume from Snapshot$`, f.iCallCreateVolumeFromSnapshot)
	s.Step(`^the wrong capacity$`, f.theWrongCapacity)
	s.Step(`^the wrong storage pool$`, f.theWrongStoragePool)
	s.Step(`^there are (\d+) valid snapshots of "([^"]*)" volume$`, f.thereAreValidSnapshotsOfVolume)
	s.Step(`^I call ListSnapshots with max_entries "([^"]*)" and starting_token "([^"]*)"$`, f.iCallListSnapshotsWithMaxentriesAndStartingtoken)
	s.Step(`^a valid ListSnapshotsResponse is returned with listed "([^"]*)" and next_token "([^"]*)"$`, f.aValidListSnapshotsResponseIsReturnedWithListedAndNexttoken)
	s.Step(`^the total snapshots listed is "([^"]*)"$`, f.theTotalSnapshotsListedIs)
	s.Step(`^I call ListSnapshots for volume "([^"]*)"$`, f.iCallListSnapshotsForVolume)
	s.Step(`^I call ListSnapshots for snapshot "([^"]*)"$`, f.iCallListSnapshotsForSnapshot)
	s.Step(`^the snapshot ID is "([^"]*)"$`, f.theSnapshotIDIs)
	s.Step(`^I invalidate the Probe cache$`, f.iInvalidateTheProbeCache)
	s.Step(`^I call ControllerExpandVolume set to (\d+)$`, f.iCallControllerExpandVolume)
	s.Step(`^I call NodeExpandVolume with volumePath as "([^"]*)"$`, f.iCallNodeExpandVolume)
	s.Step(`^I call NodeGetVolumeStats$`, f.iCallNodeGetVolumeStats)
	s.Step(`^I give request volume context$`, f.iGiveRequestVolumeContext)
	s.Step(`^I call GetDevice "([^"]*)"$`, f.iCallGetDevice)
	s.Step(`^I call NewService$`, f.iCallNewService)
	s.Step(`^a new service is returned$`, f.aNewServiceIsReturned)
	s.Step(`^I call getVolProvisionType with bad params$`, f.iCallGetVolProvisionTypeWithBadParams)
	s.Step(`^i Call getStoragePoolnameByID "([^"]*)"$`, f.iCallGetStoragePoolnameByID)
	s.Step(`^I call evalsymlink "([^"]*)"$`, f.iCallEvalsymlink)
	s.Step(`^I Call nodeGetAllSystems$`, f.iCallNodeGetAllSystems)
	s.Step(`^I do not have a gateway connection$`, f.iDoNotHaveAGatewayConnection)
	s.Step(`^I do not have a valid gateway endpoint$`, f.iDoNotHaveAValidGatewayEndpoint)
	s.Step(`^I do not have a valid gateway password$`, f.iDoNotHaveAValidGatewayPassword)
	s.Step(`^I call Clone volume$`, f.iCallCloneVolume)
}
