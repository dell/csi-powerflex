// Copyright © 2019-2023 Dell Inc. or its subsidiaries. All Rights Reserved.
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
	"log"
	"net"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/cucumber/godog"
	"github.com/dell/dell-csi-extensions/podmon"
	"github.com/dell/dell-csi-extensions/replication"
	volGroupSnap "github.com/dell/dell-csi-extensions/volumeGroupSnapshot"
	"github.com/dell/gofsutil"
	"github.com/dell/goscaleio"
	types "github.com/dell/goscaleio/types/v1"
	"github.com/golang/protobuf/ptypes"
	"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
	v1 "k8s.io/api/core/v1"
	storage "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	arrayID                    = "14dbbf5617523654"
	arrayID2                   = "15dbbf5617523655"
	badVolumeID                = "Totally Fake ID"
	badCsiVolumeID             = "ffff-f250"
	goodVolumeID               = "111"
	badVolumeID2               = "9999"
	badVolumeID3               = "99"
	goodVolumeName             = "vol1"
	altVolumeID                = "222"
	goodNodeID                 = "9E56672F-2F4B-4A42-BFF4-88B6846FBFDA"
	goodArrayConfig            = "./features/array-config/config"
	goodDriverConfig           = "./features/driver-config/logConfig.yaml"
	altNodeID                  = "7E012974-3651-4DCB-9954-25975A3C3CDF"
	datafile                   = "test/tmp/datafile"
	datadir                    = "test/tmp/datadir"
	badtarget                  = "/nonexist/target"
	altdatadir                 = "test/tmp/altdatadir"
	altdatafile                = "test/tmp/altdatafile"
	sdcVolume1                 = "d0f055a700000000"
	sdcVolume2                 = "c0f055aa00000000"
	sdcVolume0                 = "0000000000000000"
	ephemVolumeSDC             = "6373692d64306630353561373030303030303030"
	mdmID                      = "14dbbf5617523654"
	mdmIDEphem                 = "14dbbf5617523654"
	mdmID1                     = "24dbbf5617523654"
	mdmID2                     = "34dbbf5617523654"
	badMdmID                   = "9999"
	nodePublishBlockDevicePath = "test/dev/scinia"
	nodePublishAltBlockDevPath = "test/dev/scinib"
	nodePublishEphemDevPath    = "test/dev/scinic"
	nodePublishSymlinkDir      = "test/dev/disk/by-id"
	goodSnapID                 = "444"
	altSnapID                  = "555"
)

var setupGetSystemIDtoFail bool

type feature struct {
	nGoRoutines                           int
	server                                *httptest.Server
	server2                               *httptest.Server
	service                               *service
	adminClient                           *goscaleio.Client
	system                                *goscaleio.System
	adminClient2                          *goscaleio.Client
	countOfArrays                         int
	system2                               *goscaleio.System
	err                                   error // return from the preceding call
	getPluginInfoResponse                 *csi.GetPluginInfoResponse
	getPluginCapabilitiesResponse         *csi.GetPluginCapabilitiesResponse
	probeResponse                         *csi.ProbeResponse
	createVolumeResponse                  *csi.CreateVolumeResponse
	publishVolumeResponse                 *csi.ControllerPublishVolumeResponse
	unpublishVolumeResponse               *csi.ControllerUnpublishVolumeResponse
	nodeGetInfoResponse                   *csi.NodeGetInfoResponse
	nodeGetCapabilitiesResponse           *csi.NodeGetCapabilitiesResponse
	nodeGetVolumeStatsResponse            *csi.NodeGetVolumeStatsResponse
	deleteVolumeResponse                  *csi.DeleteVolumeResponse
	getMappedVolResponse                  *goscaleio.SdcMappedVolume
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
	controllerGetVolumeRequest            *csi.ControllerGetVolumeRequest
	ControllerGetVolumeResponse           *csi.ControllerGetVolumeResponse
	validateVolumeHostConnectivityResp    *podmon.ValidateVolumeHostConnectivityResponse
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
	VolumeGroupSnapshot                   *volGroupSnap.CreateVolumeGroupSnapshotResponse
	replicationCapabilitiesResponse       *replication.GetReplicationCapabilityResponse
	clusterUID                            string
	createStorageProtectionGroupResponse  *replication.CreateStorageProtectionGroupResponse
	deleteStorageProtectionGroupResponse  *replication.DeleteStorageProtectionGroupResponse
	fileSystemID                          string
	systemID                              string
	context                               context.Context
	nodeLabels                            map[string]string
	maxVolSize                            int64
	nfsExport                             types.NFSExport
}

func (f *feature) checkGoRoutines(tag string) {
	goroutines := runtime.NumGoroutine()
	fmt.Printf("goroutines %s new %d old groutines %d\n", tag, goroutines, f.nGoRoutines)
	f.nGoRoutines = goroutines
}

func (f *feature) aVxFlexOSService() error {
	return f.aVxFlexOSServiceWithTimeoutMilliseconds(150)
}

func (f *feature) aVxFlexOSServiceWithTimeoutMilliseconds(millis int) error {
	f.checkGoRoutines("start aVxFlexOSService")
	goscaleio.ClientConnectTimeout = time.Duration(millis) * time.Millisecond

	// Save off the admin client and the system
	if f.service != nil {
		adminClient := f.service.adminClients[arrayID]
		adminClient2 := f.service.adminClients[arrayID2]
		if adminClient != nil {
			f.adminClient = adminClient
			f.adminClient.SetToken("xxxx")
		}
		if adminClient2 != nil {
			f.adminClient2 = adminClient2
			f.adminClient2.SetToken("xxxx")
		}

		system := f.service.systems[arrayID]
		if system != nil {
			f.system = system
		}
		system2 := f.service.systems[arrayID2]
		if system2 != nil {
			f.system2 = system2
		}

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
	f.validateVolumeHostConnectivityResp = nil
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
	f.getMappedVolResponse = nil
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
	f.maxVolSize = 0

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
	publishGetMappedVolMaxRetry = 2
	getMappedVolDelay = 10 * time.Millisecond

	// Get or reuse the cached service
	f.getService()

	goscaleio.SCINIMockMode = true

	// Get the httptest mock handler. Only set
	// a new server if there isn't one already.
	handler := getHandler()
	if handler != nil {
		if f.server == nil {
			f.server = httptest.NewServer(handler)
			f.server2 = httptest.NewServer(handler)
		}
		if f.service.opts.arrays != nil {
			f.service.opts.arrays[arrayID].Endpoint = f.server.URL
			if f.service.opts.arrays[arrayID2] != nil {
				f.service.opts.arrays[arrayID2].Endpoint = f.server2.URL
			}
		}
	} else {
		f.server = nil
	}

	inducedError = errors.New("")
	f.clusterUID = uuid.New().String()

	// Used to contain all contents of the systemArray.
	systemArrays = make(map[string]*systemArray)
	addr := f.server.Listener.Addr().String()
	addr2 := f.server2.Listener.Addr().String()

	systemArrays[addr] = &systemArray{ID: arrayID}
	systemArrays[addr2] = &systemArray{ID: arrayID2}

	systemArrays[addr].Init()
	systemArrays[addr2].Init()

	systemArrays[addr].Link(systemArrays[addr2])

	f.checkGoRoutines("end aVxFlexOSService")
	return nil
}

func (f *feature) getService() *service {
	testControllerHasNoConnection = false
	svc := new(service)

	svc.adminClients = make(map[string]*goscaleio.Client)
	svc.systems = make(map[string]*goscaleio.System)

	if f.adminClient != nil {
		svc.adminClients[arrayID] = f.adminClient
	}
	if f.adminClient2 != nil {
		svc.adminClients[arrayID2] = f.adminClient2
	}

	if f.system != nil {
		svc.systems[arrayID] = f.system
	}
	if f.system2 != nil {
		svc.systems[arrayID2] = f.system2
	}

	svc.storagePoolIDToName = map[string]string{}
	svc.volumePrefixToSystems = map[string][]string{}
	svc.connectedSystemNameToID = map[string]string{}
	svc.privDir = "./features"
	if ArrayConfigFile != goodArrayConfig {
		ArrayConfigFile = goodArrayConfig
	}
	if DriverConfigParamsFile != goodDriverConfig {
		DriverConfigParamsFile = goodDriverConfig
	}

	if f.service != nil {
		return f.service
	}
	var opts Opts
	ctx := new(context.Context)
	var err error
	opts.arrays, err = getArrayConfig(*ctx)
	if err != nil {
		log.Printf("Read arrays from config file failed: %s\n", err)
	}

	opts.AutoProbe = true
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
	opts.defaultSystemID = arrayID

	if setupGetSystemIDtoFail {
		opts.defaultSystemID = ""
		array := opts.arrays[arrayID]
		opts.arrays["addAnotherArray"] = array
	}

	opts.replicationPrefix = "replication.storage.dell.com"

	svc.opts = opts

	if f.system != nil {
		svc.systems[arrayID] = f.system
	}
	f.service = svc
	f.countOfArrays = len(svc.systems)
	svc.statisticsCounter = 99
	svc.logStatistics()
	if K8sClientset == nil {
		f.CreateCSINode()
		svc.ProcessMapSecretChange()
	}
	server := grpc.NewServer()
	svc.RegisterAdditionalServers(server)
	return svc
}

// CreateCSINode uses fakeclient to make csinode with topology key
func (f *feature) CreateCSINode() (*storage.CSINode, error) {

	K8sClientset = fake.NewSimpleClientset()

	//csiKubeClient := nim.volumeHost.GetKubeClient()
	nodeKind := v1.SchemeGroupVersion.WithKind("Node")

	fakeCSINode := &storage.CSINode{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node1",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: nodeKind.Version,
					Kind:       nodeKind.Kind,
					Name:       "node1",
				},
			},
		},
		Spec: storage.CSINodeSpec{
			Drivers: []storage.CSINodeDriver{
				{
					Name:         "csi-vxflexos.dellemc.com",
					NodeID:       "9E56672F-2F4B-4A42-BFF4-88B6846FBFDA",
					TopologyKeys: []string{"csi-vxflexos.dellemc.com/14dbbf5617523654"},
				},
			},
		},
	}
	return K8sClientset.StorageV1().CSINodes().Create(context.TODO(), fakeCSINode, metav1.CreateOptions{})
}

func (f *feature) aValidDynamicArrayChange() error {
	var count = 0
	for _, array := range f.service.opts.arrays {
		log.Printf("array after config change array details %#v", array.SystemID)
		count++
	}
	if count != 3 {
		return errors.New("Expected DynamicArrayChange to show 3 arrays ")
	}

	// put back array config
	backup := "features/config"
	_ = os.Rename(ArrayConfigFile, "features/array-config/config.2")
	_ = os.Rename(backup, ArrayConfigFile)
	return nil
}

func (f *feature) iCallDynamicArrayChange() error {
	f.countOfArrays = len(f.service.adminClients)
	log.Printf("before config change array count=%d", f.countOfArrays)
	for key := range f.service.adminClients {
		log.Printf("before config change array ID %s", key)
	}
	backup := "features/config"
	os.Rename(ArrayConfigFile, backup)
	err := os.Rename("features/array-config/config.2", ArrayConfigFile)
	log.Printf("wait for config change %s %#v", backup, err)
	time.Sleep(10 * time.Second)
	return nil
}

func (f *feature) iCallDynamicLogChange(file string) error {
	log.Printf("level before change: %s", Log.GetLevel())
	backup := "features/driver-config/logBackup.json"
	_ = os.Rename(DriverConfigParamsFile, backup)
	_ = os.Rename("features/driver-config/"+file, DriverConfigParamsFile)
	log.Printf("wait for config change %s", backup)
	time.Sleep(10 * time.Second)
	return nil

}

func (f *feature) aValidDynamicLogChange(file, expectedLevel string) error {
	log.Printf("level after change: %s", Log.GetLevel())
	backup := "features/driver-config/logBackup.json"
	if Log.GetLevel().String() != expectedLevel {
		err := fmt.Errorf("level was expected to be %s, but was %s instead", expectedLevel, Log.GetLevel().String())
		return err
	}
	log.Printf("Reverting log changes made")
	_ = os.Rename(DriverConfigParamsFile, "features/driver-config/"+file)
	_ = os.Rename(backup, DriverConfigParamsFile)
	return nil

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

func (f *feature) iCallcheckVolumesMap(id string) error {
	f.err = f.service.checkVolumesMap(id)

	return nil

}

func (f *feature) iCallgetProtectionDomainIDFromName(systemID, protectionDomainName string) error {
	id := ""
	id, f.err = f.service.getProtectionDomainIDFromName(systemID, protectionDomainName)
	fmt.Printf("Protection Domain ID is: %s\n", id)
	return nil
}

func (f *feature) iCallgetMappedVolsWithVolIDAndSysID(volID, sysID string) error {
	f.getMappedVolResponse, f.err = getMappedVol(volID, sysID)

	return nil
}

func (f *feature) theVolumeIsFromTheCorrectSystem(volID, sysID string) error {
	if f.getMappedVolResponse.VolumeID == volID {
		if f.getMappedVolResponse.MdmID == sysID {
			return nil
		}
		errString := fmt.Sprintf("correct volume ID %s returned from wrong system %s", volID, sysID)
		return errors.New(errString)
	}
	errString := fmt.Sprintf("incorrect volume ID %s returned", volID)
	return errors.New(errString)
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
	f.service.opts.AutoProbe = true
	f.service.mode = "controller"
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
	f.service.opts.Lsmod = "junk"
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

func getTypicalNFSCreateVolumeRequest() *csi.CreateVolumeRequest {
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	params["storagepool"] = "viki_pool_HDD_20181031"
	params["path"] = "/fs"
	params["softLimit"] = "0"
	params["gracePeriod"] = "0"
	req.Parameters = params
	req.Name = "mount1"
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = 32 * 1024 * 1024 * 1024
	req.CapacityRange = capacityRange
	capability := new(csi.VolumeCapability)
	mountVolume := new(csi.VolumeCapability_MountVolume)
	mountVolume.FsType = "nfs"
	if mountVolume.FsType == "nfs" {
		req.Parameters["nasName"] = "dummy-nas-server"
	}
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
	if mountVolume.FsType == "nfs" {
		req.Parameters["nasName"] = "dummy-nas-server"
	}
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
		fmt.Println("I am in Noadmin error.....")
		f.service.adminClients[arrayID] = nil
	}

	fmt.Println("I am in iCallCreateVolume fn.....")

	f.createVolumeResponse, f.err = f.service.CreateVolume(*ctx, req)
	if f.err != nil {
		log.Printf("CreateVolume called failed: %s\n", f.err.Error())
	}

	if f.createVolumeResponse != nil {
		log.Printf("vol id %s\n", f.createVolumeResponse.GetVolume().VolumeId)
	}
	return nil
}

func (f *feature) iCallValidateVolumeHostConnectivity() error {

	ctx := new(context.Context)

	sdcID := f.service.opts.SdcGUID
	sdcGUID := strings.ToUpper(sdcID)
	csiNodeID := sdcGUID

	volIDs := make([]string, 0)

	if stepHandlersErrors.PodmonNoVolumeNoNodeIDError == true {
		csiNodeID = ""
	} else if stepHandlersErrors.PodmonNoNodeIDError == true {
		csiNodeID = ""
		volid := f.createVolumeResponse.GetVolume().VolumeId
		volIDs = volIDs[:0]
		volIDs = append(volIDs, volid)
	} else if stepHandlersErrors.PodmonControllerProbeError == true {
		f.service.mode = "controller"
	} else if stepHandlersErrors.PodmonNodeProbeError == true {
		f.service.mode = "node"
	} else if stepHandlersErrors.PodmonVolumeError == true {
		volid := badVolumeID2
		volIDs = append(volIDs, volid)
	} else if stepHandlersErrors.PodmonNoSystemError == true {
		f.service.mode = "node"
		f.system = nil
		f.service.opts.arrays[arrayID].SystemID = "WrongSystemName"
	} else {
		volid := f.createVolumeResponse.GetVolume().VolumeId
		volIDs = volIDs[:0]
		volIDs = append(volIDs, volid)
	}

	req := &podmon.ValidateVolumeHostConnectivityRequest{
		NodeId:    csiNodeID,
		VolumeIds: volIDs,
	}

	connect, err := f.service.ValidateVolumeHostConnectivity(*ctx, req)
	if err != nil {
		f.err = errors.New(err.Error())
		return nil
	}
	f.validateVolumeHostConnectivityResp = connect
	if len(connect.Messages) > 0 {
		for i, msg := range connect.Messages {
			fmt.Printf("messages %d: %s\n", i, msg)
			if stepHandlersErrors.PodmonVolumeStatisticsError == true ||
				stepHandlersErrors.PodmonVolumeError == true {
				if strings.Contains(msg, "volume") {
					fmt.Printf("found %d: %s\n", i, msg)
					f.err = errors.New(connect.Messages[i])
					return nil
				}
			}
		}
		if stepHandlersErrors.PodmonVolumeStatisticsError == true {
			f.err = errors.New(connect.Messages[0])
			return nil
		}
	}

	if connect.IosInProgress {
		return nil
	}
	err = fmt.Errorf("Unexpected error IO to volume: %t", connect.IosInProgress)
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
		requestedSystem = f.service.opts.defaultSystemID
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

func (f *feature) iSpecifyAccessibilityRequirementsNFSWithASystemIDOf(requestedSystem string) error {
	if requestedSystem == "f.service.opt.SystemName" {
		requestedSystem = f.service.opts.defaultSystemID
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
		"csi-vxflexos.dellemc.com/" + requestedSystem + "-nfs": "powerflex.dellemc.com",
	}
	req.AccessibilityRequirements.Preferred = append(req.AccessibilityRequirements.Preferred, top)
	req.AccessibilityRequirements.Preferred = append(req.AccessibilityRequirements.Preferred, top)
	capability := new(csi.VolumeCapability)
	mountVolume := new(csi.VolumeCapability_MountVolume)
	mountVolume.FsType = "nfs"
	if mountVolume.FsType == "nfs" {
		req.Parameters["nasName"] = "dummy-nas-server"
	}
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

func (f *feature) iSpecifyBadAccessibilityRequirementsNFSWithASystemIDOf(requestedSystem string) error {
	if requestedSystem == "f.service.opt.SystemName" {
		requestedSystem = f.service.opts.defaultSystemID
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
		"csi-vxflexos.dellemc.com/" + requestedSystem + "-abc": "powerflex.dellemc.com",
	}
	req.AccessibilityRequirements.Preferred = append(req.AccessibilityRequirements.Preferred, top)
	capability := new(csi.VolumeCapability)
	mountVolume := new(csi.VolumeCapability_MountVolume)
	mountVolume.FsType = "nfs"
	if mountVolume.FsType == "nfs" {
		req.Parameters["nasName"] = "dummy-nas-server"
	}
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

func (f *feature) aValidCreateVolumeResponseWithTopologyIsReturned() error {
	if f.err != nil {
		return f.err
	}

	isNFS := false
	var fsType string
	if len(f.createVolumeRequest.VolumeCapabilities) != 0 {
		fsType = f.createVolumeRequest.VolumeCapabilities[0].GetMount().GetFsType()
		if fsType == "nfs" {
			isNFS = true
		}
	}

	f.volumeIDList = append(f.volumeIDList, f.createVolumeResponse.Volume.VolumeId)
	topology := f.createVolumeResponse.Volume.AccessibleTopology
	if len(topology) != 1 {
		fmt.Printf("Volume topology should have one element. Found %d elements.", len(topology))
		return errors.New("wrong topology data in volume create response")
	}

	topology1 := topology[0]
	segments := topology1.Segments
	fmt.Printf("Volume topology segments %#v . \n", segments)
	if len(segments) != 1 {
		fmt.Printf("Volume topology should have one segement. Found %d.", len(segments))
		return errors.New("wrong topology data in volume create response")
	}

	requestedSystem := f.service.opts.defaultSystemID
	for key := range segments {
		if strings.HasPrefix(key, Name) {
			tokens := strings.Split(key, "/")
			constraint := ""
			if len(tokens) > 1 {
				constraint = tokens[1]
			}

			log.Printf("Found topology constraint: VxFlex OS system: %s", constraint)
			if isNFS {
				nfsTokens := strings.Split(constraint, "-")
				nfsLabel := ""
				if len(nfsTokens) > 1 {
					constraint = nfsTokens[0]
					nfsLabel = nfsTokens[1]
					if nfsLabel != "nfs" {
						return status.Errorf(codes.InvalidArgument,
							"Invalid topology requested for NFS Volume. Please validate your storage class has nfs topology.")
					}
				}
			}
			if constraint != requestedSystem {
				fmt.Printf("Volume topology segement should have system %s. Found %s.", requestedSystem, constraint)
				return errors.New("wrong systemID in AccessibleTopology")
			}
		} else {
			return errors.New("wrong prefix in AccessibleTopology")
		}
	}

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
	var req *csi.CreateVolumeRequest
	if f.createVolumeRequest == nil {
		req = getTypicalCreateVolumeRequest()
	} else {
		req = f.createVolumeRequest
	}

	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = -32 * 1024 * 1024 * 1024
	req.CapacityRange = capacityRange
	req.Name = "bad capacity"
	f.createVolumeRequest = req
	return nil
}

func (f *feature) iSpecifyNoStoragePool() error {
	req := getTypicalCreateVolumeRequest()
	req.Parameters = make(map[string]string)
	req.Name = "no storage pool"
	f.createVolumeRequest = req
	return nil
}

func (f *feature) iCallCreateVolumeSize(name string, size int64) error {
	ctx := new(context.Context)
	var req *csi.CreateVolumeRequest
	if f.createVolumeRequest == nil {
		req = getTypicalCreateVolumeRequest()
	} else {
		req = f.createVolumeRequest
	}

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

func (f *feature) iCallCreateVolumeSizeNFS(name string, size int64) error {
	ctx := new(context.Context)
	var req *csi.CreateVolumeRequest
	if f.createVolumeRequest == nil {
		req = getTypicalNFSCreateVolumeRequest()
	} else {
		req = f.createVolumeRequest
	}

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
	// params := make(map[string]string)
	// params["storagepool"] = storagePoolName
	f.createVolumeRequest.Parameters["storagepool"] = storagePoolName
	return nil
}

func (f *feature) iInduceError(errtype string) error {
	log.Printf("set induce error %s\n", errtype)
	inducedError = errors.New(errtype)
	switch errtype {
	case "WrongSysNameError":
		stepHandlersErrors.WrongSysNameError = true
	case "BadCapacityError":
		stepHandlersErrors.BadCapacityError = true
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
	case "WrongFileSystemIDError":
		stepHandlersErrors.WrongFileSystemIDError = true
	case "NoFileSystemIDError":
		stepHandlersErrors.NoFileSystemIDError = true
	case "WrongSystemError":
		stepHandlersErrors.WrongSystemError = true
	case "NFSExportsInstancesError":
		stepHandlersErrors.NFSExportInstancesError = true
	case "BadVolIDError":
		stepHandlersErrors.BadVolIDError = true
	case "NoCsiVolIDError":
		f.nodePublishVolumeRequest.VolumeId = ""
	case "EmptyConnectedSystemIDErrorNoAutoProbe":
		connectedSystemID = nil
	case "NoSysIDError":
		f.nodePublishVolumeRequest.VolumeId = badVolumeID2
	case "WrongSysIDError":
		f.nodePublishVolumeRequest.VolumeId = badVolumeID2
	case "RequireProbeFailError":
		f.nodePublishVolumeRequest.VolumeId = "50"
	case "VolumeIDTooShortErrorInNodeExpand":
		stepHandlersErrors.VolumeIDTooShortError = true
	case "TooManyDashesVolIDInNodeExpand":
		stepHandlersErrors.TooManyDashesVolIDError = true
	case "CorrectFormatBadCsiVolIDInNodeExpand":
		stepHandlersErrors.CorrectFormatBadCsiVolID = true
	case "EmptySysIDInNodeExpand":
		stepHandlersErrors.EmptySysID = true
	case "WrongVolIDErrorInNodeExpand":
		stepHandlersErrors.BadVolIDError = true
	case "EmptyEphemeralID":
		f.nodePublishVolumeRequest.VolumeId = mdmID
		stepHandlersErrors.EmptyEphemeralID = true
	case "IncorrectEphemeralID":
		f.nodePublishVolumeRequest.VolumeId = mdmID
		stepHandlersErrors.IncorrectEphemeralID = true
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
	case "SDCLimitsError":
		stepHandlersErrors.SDCLimitsError = true
	case "require-probe":
		f.service.opts.SdcGUID = ""
		f.service.opts.arrays = make(map[string]*ArrayConnectionData)
	case "no-sdc":
		stepHandlersErrors.PodmonFindSdcError = true
	case "no-system":
		stepHandlersErrors.PodmonNoSystemError = true
	case "controller-probe":
		stepHandlersErrors.PodmonControllerProbeError = true
	case "node-probe":
		stepHandlersErrors.PodmonNodeProbeError = true
	case "volume-error":
		stepHandlersErrors.PodmonVolumeError = true
	case "no-nodeId":
		stepHandlersErrors.PodmonVolumeStatisticsError = true
		stepHandlersErrors.PodmonNoNodeIDError = true
		f.service.opts.SdcGUID = ""
	case "no-volume-no-nodeId":
		stepHandlersErrors.PodmonVolumeStatisticsError = true
		stepHandlersErrors.PodmonNoVolumeNoNodeIDError = true
		f.volumeID = "0"
		f.service.opts.SdcGUID = ""
	case "no-volume-statistics":
		stepHandlersErrors.PodmonVolumeStatisticsError = true
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
	case "FileSystemInstancesError":
		stepHandlersErrors.FileSystemInstancesError = true
	case "GetFileSystemsByIdError":
		stepHandlersErrors.GetFileSystemsByIDError = true
	case "NasNotFoundError":
		stepHandlersErrors.NasServerNotFoundError = true
	case "fileInterfaceNotFoundError":
		stepHandlersErrors.FileInterfaceNotFoundError = true
	case "NoVolumeIDError":
		stepHandlersErrors.NoVolumeIDError = true
	case "BadVolIDJSON":
		stepHandlersErrors.BadVolIDJSON = true
	case "BadMountPathError":
		stepHandlersErrors.BadMountPathError = true
	case "NoMountPathError":
		stepHandlersErrors.NoMountPathError = true
	case "NoVolIDError":
		stepHandlersErrors.NoVolIDError = true
	case "NoVolIDSDCError":
		stepHandlersErrors.NoVolIDSDCError = true
	case "SetSdcNameError":
		stepHandlersErrors.SetSdcNameError = true
	case "ApproveSdcError":
		stepHandlersErrors.ApproveSdcError = true
	case "NoVolError":
		stepHandlersErrors.NoVolError = true
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
		err = gofsutil.Mount(context.Background(), nodePublishAltBlockDevPath, "features/"+sdcVolume1, "none")
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
	case "LegacyVolumeConflictError":
		stepHandlersErrors.LegacyVolumeConflictError = true
	case "VolumeIDTooShortError":
		stepHandlersErrors.VolumeIDTooShortError = true
	case "VolIDListEmptyError":
		stepHandlersErrors.VolIDListEmptyError = true
	case "CreateVGSNoNameError":
		stepHandlersErrors.CreateVGSNoNameError = true
	case "CreateVGSNameTooLongError":
		stepHandlersErrors.CreateVGSNameTooLongError = true
	case "CreateVGSLegacyVol":
		stepHandlersErrors.CreateVGSLegacyVol = true
	case "CreateVGSAcrossTwoArrays":
		stepHandlersErrors.CreateVGSAcrossTwoArrays = true
	case "CreateVGSBadTimeError":
		stepHandlersErrors.CreateVGSBadTimeError = true
	case "CreateSplitVGSError":
		stepHandlersErrors.CreateSplitVGSError = true
	case "ProbePrimaryError":
		f.service.adminClients[arrayID] = nil
		f.service.systems[arrayID] = nil
		stepHandlersErrors.PodmonControllerProbeError = true
	case "ProbeSecondaryError":
		f.service.adminClients[arrayID2] = nil
		f.service.systems[arrayID2] = nil
		stepHandlersErrors.PodmonControllerProbeError = true
	default:
		fmt.Println("Ensure that the error is handled in the handlers section.")
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
	case "single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	case "multiple-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
	case "multiple-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
	case "single-node-single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER
	case "single-node-multi-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER
	case "unknown":
		accessMode.Mode = csi.VolumeCapability_AccessMode_UNKNOWN
	}
	if !f.omitAccessMode {
		capability.AccessMode = accessMode
	}
	fmt.Printf("capability.AccessType %v\n", capability.AccessType)
	fmt.Printf("capability.AccessMode %v\n", capability.AccessMode)
	req := new(csi.ControllerPublishVolumeRequest)
	if !f.noVolumeID {
		if f.invalidVolumeID {
			req.VolumeId = badVolumeID2
		} else {
			req.VolumeId = goodVolumeID
		}
	}

	if stepHandlersErrors.VolumeIDTooShortError {
		req.VolumeId = badVolumeID3
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

func (f *feature) getControllerPublishVolumeRequestNFS(accessType string) *csi.ControllerPublishVolumeRequest {
	capability := new(csi.VolumeCapability)
	block := new(csi.VolumeCapability_Block)
	block.Block = new(csi.VolumeCapability_BlockVolume)
	if f.useAccessTypeMount {
		mountVolume := new(csi.VolumeCapability_MountVolume)
		mountVolume.FsType = "nfs"
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
	case "single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	case "multiple-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
	case "multiple-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
	case "single-node-single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER
	case "single-node-multi-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER
	case "unknown":
		accessMode.Mode = csi.VolumeCapability_AccessMode_UNKNOWN
	}
	if !f.omitAccessMode {
		capability.AccessMode = accessMode
	}
	fmt.Printf("capability.AccessType %v\n", capability.AccessType)
	fmt.Printf("capability.AccessMode %v\n", capability.AccessMode)
	req := new(csi.ControllerPublishVolumeRequest)
	if !f.noVolumeID {
		if f.invalidVolumeID {
			req.VolumeId = badVolumeID2
		} else {
			req.VolumeId = "14dbbf5617523654" + "/" + fileSystemNameToID["volume1"]
		}
	}

	if stepHandlersErrors.VolumeIDTooShortError {
		req.VolumeId = badVolumeID3
	}

	if !f.noNodeID {
		req.NodeId = goodNodeID
	}
	req.Readonly = false
	if !f.omitVolumeCapability {
		req.VolumeCapability = capability
	}
	req.VolumeContext = make(map[string]string)
	req.VolumeContext[KeyFsType] = "nfs"
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
	case "multiple-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
	case "multiple-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
	case "unknown":
		accessMode.Mode = csi.VolumeCapability_AccessMode_UNKNOWN
	}
	if !f.omitAccessMode {
		capability.AccessMode = accessMode
	}
	fmt.Printf("capability.AccessType %v\n", capability.AccessType)
	fmt.Printf("capability.AccessMode %v\n", capability.AccessMode)
	req := new(csi.DeleteVolumeRequest)
	if !f.noVolumeID {
		if f.invalidVolumeID {
			req.VolumeId = badVolumeID2
		} else {
			req.VolumeId = goodVolumeID
		}
	}
	return req
}

func (f *feature) getControllerDeleteVolumeRequestBad(accessType string) *csi.DeleteVolumeRequest {
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
	case "multiple-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
	case "multiple-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
	case "unknown":
		accessMode.Mode = csi.VolumeCapability_AccessMode_UNKNOWN
	}
	if !f.omitAccessMode {
		capability.AccessMode = accessMode
	}
	fmt.Printf("capability.AccessType %v\n", capability.AccessType)
	fmt.Printf("capability.AccessMode %v\n", capability.AccessMode)
	req := new(csi.DeleteVolumeRequest)
	if !f.noVolumeID {
		if f.invalidVolumeID {
			req.VolumeId = badVolumeID2
		} else {
			req.VolumeId = ""
		}
	}
	return req
}

func (f *feature) getControllerDeleteVolumeRequestNFS(accessType string) *csi.DeleteVolumeRequest {
	capability := new(csi.VolumeCapability)

	mountVolume := new(csi.VolumeCapability_MountVolume)
	mountVolume.FsType = "nfs"
	mountVolume.MountFlags = make([]string, 0)
	mount := new(csi.VolumeCapability_Mount)
	mount.Mount = mountVolume
	capability.AccessType = mount

	accessMode := new(csi.VolumeCapability_AccessMode)
	switch accessType {
	case "single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	case "multiple-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
	case "multiple-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
	case "unknown":
		accessMode.Mode = csi.VolumeCapability_AccessMode_UNKNOWN
	}
	if !f.omitAccessMode {
		capability.AccessMode = accessMode
	}
	fmt.Printf("capability.AccessType %v\n", capability.AccessType)
	fmt.Printf("capability.AccessMode %v\n", capability.AccessMode)
	req := new(csi.DeleteVolumeRequest)
	if !f.noVolumeID {
		if f.invalidVolumeID {
			req.VolumeId = badVolumeID2
		} else {
			req.VolumeId = "14dbbf5617523654" + "/" + fileSystemNameToID["volume1"]
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

func (f *feature) iCallPublishVolumeWithNFS(arg1 string) error {
	ctx := new(context.Context)
	req := f.publishVolumeRequest
	if f.publishVolumeRequest == nil {
		req = f.getControllerPublishVolumeRequestNFS(arg1)
		f.publishVolumeRequest = req
	} else {
		capability := new(csi.VolumeCapability)
		accessMode := new(csi.VolumeCapability_AccessMode)
		switch arg1 {
		case "multi-single-writer":
			accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER
		case "single-writer":
			accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
		case "multiple-reader":
			accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
		case "multiple-writer":
			accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
		case "single-node-single-writer":
			accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER
		case "single-node-multi-writer":
			accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER
		case "unknown":
			accessMode.Mode = csi.VolumeCapability_AccessMode_UNKNOWN
		}
		if !f.omitAccessMode {
			capability.AccessMode = accessMode
		}
		req.VolumeCapability.AccessMode = accessMode
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
	//this prevents the step handler from returning the volume '111' as found in the non default array
	volIDtoUse := "1234"
	if stepHandlersErrors.LegacyVolumeConflictError {
		volIDtoUse = goodVolumeID
	}
	volumeIDToName[volIDtoUse] = goodVolumeName
	volumeNameToID[goodVolumeName] = volIDtoUse
	volumeIDToSizeInKB[volIDtoUse] = defaultVolumeSize
	volumeIDToReplicationState[volIDtoUse] = unmarkedForReplication
	return nil
}

func (f *feature) aBadFileSystem() error {
	for key := range fileSystemIDName {
		fileSystemIDName[key] = ""
	}
	return nil
}

func (f *feature) aBadNFSExport() error {
	for key := range nfsExportIDName {
		nfsExportIDName[key] = ""
	}
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
			req.VolumeId = badVolumeID2
		} else {
			req.VolumeId = goodVolumeID
		}
	}
	if !f.noNodeID {
		req.NodeId = goodNodeID
	}
	return req
}

func (f *feature) getControllerUnpublishVolumeRequestNFS() *csi.ControllerUnpublishVolumeRequest {
	req := new(csi.ControllerUnpublishVolumeRequest)
	if !f.noVolumeID {
		if f.invalidVolumeID {
			req.VolumeId = badVolumeID2
		} else {
			req.VolumeId = "14dbbf5617523654" + "/" + fileSystemNameToID["volume1"]
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

func (f *feature) iCallUnpublishVolumeNFS() error {
	ctx := new(context.Context)
	req := f.unpublishVolumeRequest
	if f.unpublishVolumeRequest == nil {
		req = f.getControllerUnpublishVolumeRequestNFS()
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
	f.service.opts.SdcGUID = "9E56672F-2F4B-4A42-BFF4-88B6846FBFDA"
	GetNodeLabels = mockGetNodeLabels
	f.nodeGetInfoResponse, f.err = f.service.NodeGetInfo(*ctx, req)
	return nil
}

func (f *feature) iCallNodeGetInfoWithValidVolumeLimitNodeLabels() error {
	ctx := new(context.Context)
	req := new(csi.NodeGetInfoRequest)
	f.service.opts.SdcGUID = "9E56672F-2F4B-4A42-BFF4-88B6846FBFDA"
	GetNodeLabels = mockGetNodeLabelsWithVolumeLimits
	f.nodeGetInfoResponse, f.err = f.service.NodeGetInfo(*ctx, req)
	fmt.Printf("MaxVolumesPerNode: %v", f.nodeGetInfoResponse.MaxVolumesPerNode)
	return nil
}

func (f *feature) iCallNodeGetInfoWithInvalidVolumeLimitNodeLabels() error {
	ctx := new(context.Context)
	req := new(csi.NodeGetInfoRequest)
	f.service.opts.SdcGUID = "9E56672F-2F4B-4A42-BFF4-88B6846FBFDA"
	GetNodeLabels = mockGetNodeLabelsWithInvalidVolumeLimits
	f.nodeGetInfoResponse, f.err = f.service.NodeGetInfo(*ctx, req)
	return nil
}

func mockGetNodeLabels(ctx context.Context, s *service) (map[string]string, error) {
	labels := map[string]string{"csi-vxflexos.dellemc.com/05d539c3cdc5280f-nfs": "true", "csi-vxflexos.dellemc.com/0e7a082862fedf0f": "csi-vxflexos.dellemc.com"}
	return labels, nil
}

func mockGetNodeLabelsWithVolumeLimits(ctx context.Context, s *service) (map[string]string, error) {
	labels := map[string]string{"max-vxflexos-volumes-per-node": "2"}
	return labels, nil
}

func mockGetNodeLabelsWithInvalidVolumeLimits(ctx context.Context, s *service) (map[string]string, error) {
	labels := map[string]string{"max-vxflexos-volumes-per-node": "invalid-vol-limit"}
	return labels, nil
}

func (f *feature) setFakeNode() (*v1.Node, error) {
	os.Setenv("HOSTNAME", "node1")
	fakeNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "node1",
			Labels: map[string]string{"label1": "value1", "label2": "value2"},
		},
	}
	return K8sClientset.CoreV1().Nodes().Create(context.TODO(), fakeNode, metav1.CreateOptions{})
}

func (f *feature) iCallGetNodeLabels() error {
	f.setFakeNode()
	labels, err := f.service.GetNodeLabels(f.context)
	fmt.Printf("Node labels: %v", labels)
	if err != nil {
		return err
	}
	f.nodeLabels = labels
	return nil
}
func (f *feature) aValidLabelIsReturned() error {
	if f.nodeLabels == nil {
		return errors.New("Unable to fetch the node labels")
	}
	fmt.Printf("Node labels: %v", f.nodeLabels)
	return nil
}

func (f *feature) iSetInvalidEnvMaxVolumesPerNode() error {
	LookupEnv = mockLookupEnv
	os.Setenv("X_CSI_MAX_VOLUMES_PER_NODE", "invalid_value")
	_, f.err = ParseInt64FromContext(f.context, EnvMaxVolumesPerNode)
	return nil
}

func mockLookupEnv(ctx context.Context, key string) (string, bool) {
	return "invalid_value", true
}

func (f *feature) iCallGetNodeLabelsWithInvalidNode() error {
	os.Setenv("HOSTNAME", "node2")
	_, f.err = f.service.GetNodeLabels(f.context)
	return nil
}

func (f *feature) iCallGetNodeLabelsWithUnsetKubernetesClient() error {
	K8sClientset = nil
	ctx := new(context.Context)
	f.nodeLabels, f.err = f.service.GetNodeLabels(*ctx)
	return nil
}

func (f *feature) iCallNodeProbe() error {
	ctx := new(context.Context)
	req := new(csi.ProbeRequest)
	f.checkGoRoutines("before probe")
	f.service.mode = "node"
	f.probeResponse, f.err = f.service.Probe(*ctx, req)
	f.checkGoRoutines("after probe")
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

func (f *feature) theVolumeLimitIsSet() error {
	if f.err != nil {
		return f.err
	}
	fmt.Printf("node: %s", f.nodeGetInfoResponse)
	if f.nodeGetInfoResponse.NodeId == "" {
		return errors.New("expected NodeGetInfoResponse to contain NodeID but it was null")
	}
	if f.nodeGetInfoResponse.MaxVolumesPerNode != 2 {
		return errors.New("expected NodeGetInfoResponse MaxVolumesPerNode to be 2")
	}
	fmt.Printf("MaxVolumesPerNode: %d\n", f.nodeGetInfoResponse.MaxVolumesPerNode)
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

func (f *feature) iCallDeleteVolumeWithBad(arg1 string) error {
	ctx := new(context.Context)
	req := f.deleteVolumeRequest
	if f.deleteVolumeRequest == nil {
		req = f.getControllerDeleteVolumeRequestBad(arg1)
		f.deleteVolumeRequest = req
	}
	log.Printf("Calling DeleteVolume")
	f.deleteVolumeResponse, f.err = f.service.DeleteVolume(*ctx, req)
	if f.err != nil {
		log.Printf("DeleteVolume called failed: %s\n", f.err.Error())
	}
	return nil
}

func (f *feature) iCallDeleteVolumeNFSWith(arg1 string) error {
	ctx := new(context.Context)
	req := f.deleteVolumeRequest
	if f.deleteVolumeRequest == nil {
		req = f.getControllerDeleteVolumeRequestNFS(arg1)
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

func (f *feature) iCallGetMaximumVolumeSize(arg1 string) {

	systemid := arg1
	f.maxVolSize, f.err = f.service.getMaximumVolumeSize(systemid)
	if f.err != nil {
		log.Printf("err while getting max vol size: %s\n", f.err.Error())
	}

}

func (f *feature) aValidGetCapacityResponsewithmaxvolsizeIsReturned() error {
	if f.err != nil {
		return f.err
	}
	if f.maxVolSize >= 0 {
		f.getCapacityResponse.MaximumVolumeSize = wrapperspb.Int64(f.maxVolSize)
	}
	if f.getCapacityResponse.AvailableCapacity <= 0 {
		return errors.New("Expected AvailableCapacity to be positive")
	}

	if f.maxVolSize >= 0 && f.getCapacityResponse.AvailableCapacity > 0 {

		fmt.Printf("Available capacity: and Max volume size: %d %v\n", f.getCapacityResponse.AvailableCapacity, f.getCapacityResponse.MaximumVolumeSize)
	}

	fmt.Printf("Available capacity: %d\n", f.getCapacityResponse.AvailableCapacity)
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

func (f *feature) iCallControllerGetCapabilities(isHealthMonitorEnabled string) error {

	if isHealthMonitorEnabled == "true" {
		f.service.opts.IsHealthMonitorEnabled = true
	}
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
/* func parseListVolumesTable(dt *gherkin.DataTable) (int32, string, error) {
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
} */

// iCallListVolumesAgainWith nils out the previous request before delegating
// to iCallListVolumesWith with the same table data.  This simulates multiple
// calls to ListVolume for the purpose of testing the pagination token.
func (f *feature) iCallListVolumesAgainWith(maxEntriesString, startingToken string) error {
	f.listVolumesRequest = nil
	return f.iCallListVolumesWith(maxEntriesString, startingToken)
}

func (f *feature) iCallListVolumesWith(maxEntriesString, startingToken string) error {
	maxEntries, err := strconv.Atoi(maxEntriesString)
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
		req = f.getControllerListVolumesRequest(int32(maxEntries), startingToken)
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
			case csi.ControllerServiceCapability_RPC_SINGLE_NODE_MULTI_WRITER:
				count = count + 1
			case csi.ControllerServiceCapability_RPC_GET_VOLUME:
				count = count + 1
			case csi.ControllerServiceCapability_RPC_VOLUME_CONDITION:
				count = count + 1
			default:
				return fmt.Errorf("received unexpected capability: %v", typex)
			}
		}

		if f.service.opts.IsHealthMonitorEnabled && count != 10 {
			// Set default value
			f.service.opts.IsHealthMonitorEnabled = false
			return errors.New("Did not retrieve all the expected capabilities")
		} else if !f.service.opts.IsHealthMonitorEnabled && count != 8 {
			return errors.New("Did not retrieve all the expected capabilities")
		}

		// Set default value
		f.service.opts.IsHealthMonitorEnabled = false
		return nil
	}

	return errors.New("expected ControllerGetCapabilitiesResponse but didn't get one")

}

func (f *feature) iCallCloneVolume() error {
	ctx := new(context.Context)
	req := getTypicalCreateVolumeRequest()
	req.Name = "clone"
	if f.invalidVolumeID {
		req.Name = "invalid-clone"
	}
	if f.wrongCapacity {
		req.CapacityRange.RequiredBytes = 64 * 1024 * 1024 * 1024
	}

	if f.wrongStoragePool {
		req.Parameters["storagepool"] = "bad storage pool"
	}
	source := &csi.VolumeContentSource_VolumeSource{VolumeId: goodVolumeID}
	req.VolumeContentSource = new(csi.VolumeContentSource)
	req.VolumeContentSource.Type = &csi.VolumeContentSource_Volume{Volume: source}
	req.AccessibilityRequirements = new(csi.TopologyRequirement)
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
		req.VolumeId = badVolumeID2
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
	case "single-node-single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER
	case "single-node-multi-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER
	case "multi-node-single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER
	}
	capability.AccessMode = accessMode
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities
	log.Printf("Calling ValidateVolumeCapabilities %#v", accessMode)
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
		volumeIDToSizeInKB[id] = defaultVolumeSize
		volumeIDToReplicationState[id] = unmarkedForReplication
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
	case "single-node-single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER
	case "single-node-multi-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER
	case "multi-pod-rw":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
		fmt.Printf("debug ALLOW_RWO_MULTI_POD set")
		os.Setenv("ALLOW_RWO_MULTI_POD", "true")
		f.service.opts.AllowRWOMultiPodAccess = true
		mountAllowRWOMultiPodAccess = true
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

func (f *feature) aControllerPublishedEphemeralVolume() error {
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
	cmd := exec.Command("rm", "-rf", "features/"+mdmIDEphem+"-"+ephemVolumeSDC)
	_, err = cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("error removing private staging directory\n")
	} else {
		fmt.Printf("removed private staging directory\n")
	}

	// Make the block device
	_, err = os.Stat(nodePublishEphemDevPath)
	_, err2 := os.Stat(nodePublishSymlinkDir + "/emc-vol" + "-" + mdmIDEphem + "-" + ephemVolumeSDC)
	if err != nil || err2 != nil {
		cmd := exec.Command("mknod", nodePublishEphemDevPath, "b", "0", "0")
		output, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("scinic: %s\n", err.Error())
		}
		fmt.Printf("mknod output: %s\n", output)

		// Make the symlink
		cmdstring := fmt.Sprintf("cd %s; ln -s ../../scinic emc-vol-%s-%s", nodePublishSymlinkDir, mdmIDEphem, ephemVolumeSDC)
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

func (f *feature) twoIdenticalVolumesOnTwoDifferentSystems() error {
	// Make two directories, one for each volume/system
	for _, mdmID := range [2]string{mdmID1, mdmID2} {
		volDirectory := "twoVolTest/dev/disk/by-id/emc-vol-" + mdmID + "-" + sdcVolume2
		_, err := os.Stat(volDirectory)
		if err != nil {
			cmdstring := "mkdir -p " + volDirectory
			cmd := exec.Command("sh", "-c", cmdstring)
			output, err := cmd.CombinedOutput()
			fmt.Printf("mkdir output: %s\n", output)
			if err != nil {
				fmt.Printf("mkdir: %s\n", err.Error())
				err = nil
			}
		}
	}

	// Set variable in goscaleio that dev is in a different place.
	goscaleio.FSDevDirectoryPrefix = "twoVolTest"

	return nil
}

func (f *feature) iCreateFalseEphemeralID() error {
	fakeEphemeralIDFolder := ephemeralStagingMountPath + mdmIDEphem

	_, err := os.Stat(fakeEphemeralIDFolder)
	if err != nil {
		cmdstring := "mkdir -p " + fakeEphemeralIDFolder
		cmd := exec.Command("sh", "-c", cmdstring)
		output, err := cmd.CombinedOutput()
		fmt.Printf("mkdir output: %s\n", output)
		if err != nil {
			fmt.Printf("mkdir: %s\n", err.Error())
			err = nil
		}
	}

	file, err := os.Create(fakeEphemeralIDFolder + "/id")
	if err != nil {
		fmt.Printf("mkdir: %s\n", err.Error())
		// return error
	}

	if stepHandlersErrors.IncorrectEphemeralID {
		id, err := file.WriteString("1996")
		fmt.Printf("%d written to file %s\n", id, fakeEphemeralIDFolder+"/id")
		if err != nil {
			// print error
			file.Close()
			// return error
		}

		// print `line` written successfully
		err = file.Close()
		if err != nil {
			// How to print error??
			return nil
		}
		return nil
	} else if stepHandlersErrors.EmptyEphemeralID {
		return nil
	}

	return nil
}

func (f *feature) getNodeEphemeralVolumePublishRequest(name, size, sp, systemName string) error {
	req := new(csi.NodePublishVolumeRequest)
	req.VolumeId = sdcVolume1
	req.Readonly = false
	req.VolumeCapability = f.capability
	f.service.opts.defaultSystemID = ""
	req.VolumeContext = map[string]string{"csi.storage.k8s.io/ephemeral": "true", "volumeName": name, "size": size, "storagepool": sp, "systemID": systemName}

	//remove ephemeral mounting path before starting test
	os.RemoveAll("/var/lib/kubelet/plugins/kubernetes.io/csi/pv/ephemeral/")

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

func (f *feature) getNodePublishVolumeRequestNFS() error {
	req := new(csi.NodePublishVolumeRequest)
	req.VolumeId = "14dbbf5617523654" + "/" + fileSystemNameToID["volume1"]
	req.Readonly = false
	req.VolumeCapability = f.capability

	mount := f.capability.GetMount()
	if mount != nil {
		req.TargetPath = datadir
	}
	req.VolumeContext = make(map[string]string)
	req.VolumeContext[KeyFsType] = "nfs"
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

func (f *feature) iCallMountPublishVolume() error {
	req := new(csi.NodePublishVolumeRequest)
	req.TargetPath = "/badpath"
	capability := new(csi.VolumeCapability)
	accessType := new(csi.VolumeCapability_Block)
	capability.AccessType = accessType
	accessMode := new(csi.VolumeCapability_AccessMode)
	capability.AccessMode = accessMode
	req.VolumeCapability = capability
	err := publishVolume(req, "", "/bad/device", "1")
	if err != nil {
		fmt.Printf("nodePublishVolume bad targetPath: %s\n", err.Error())
		f.err = errors.New("error in publishVolume")
	}
	return nil
}

func (f *feature) iCallMountUnpublishVolume() error {
	req := new(csi.NodeUnpublishVolumeRequest)
	req.TargetPath = ""
	err := unpublishVolume(req, "", "/bad/device", "1")
	if err != nil {
		fmt.Printf("NodeUnpublishVolume bad targetPath: %s\n", err.Error())
		f.err = errors.New("error in unpublishVolume")
	}
	req.TargetPath = "/badpath"
	err = unpublishVolume(req, "", "/bad/device", "1")
	if err != nil {
		fmt.Printf("NodeUnpublishVolume bad device : %s\n", err.Error())
		f.err = errors.New("error in unpublishVolume")
	}
	return nil
}

func (f *feature) iCallNodePublishVolume(arg1 string) error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := f.nodePublishVolumeRequest
	if req == nil {
		fmt.Printf("Request was Nil \n")
		_ = f.getNodePublishVolumeRequest()
		req = f.nodePublishVolumeRequest
	}
	fmt.Printf("Calling NodePublishVolume\n")
	fmt.Printf("nodePV req is: %v \n", req)
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

func (f *feature) iCallNodePublishVolumeNFS(arg1 string) error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := f.nodePublishVolumeRequest
	if req == nil {
		fmt.Printf("Request was Nil \n")
		_ = f.getNodePublishVolumeRequestNFS()
		req = f.nodePublishVolumeRequest
	}
	fmt.Printf("Calling NodePublishVolume\n")
	fmt.Printf("nodePV req is: %v \n", req)
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

func (f *feature) iCallUnmountPrivMount() error {
	gofsutil.GOFSMock.InduceGetMountsError = true
	ctx := new(context.Context)
	err := unmountPrivMount(*ctx, nil, "/foo/bar")
	fmt.Printf("unmountPrivMount getMounts error: %s\n", err.Error())
	//  getMounts induced error
	if err != nil {
		msg := err.Error()
		if msg == "getMounts induced error" {
			f.err = errors.New("error in unmountPrivMount")
		}
	}
	/*
		//  needs a mounted ok device to unmount
		_ = handlePrivFSMount(context.TODO(), accessMode, sysDevice, nil, "", "", "")

		target := "/tmp/foo"
		flags := make([]string, 0)
		flags = append(flags, "rw")
		_ = mountBlock(sysDevice, target, flags, false)

		gofsutil.GOFSMock.InduceGetMountsError = false
		gofsutil.GOFSMock.InduceUnmountError = true
		err = unmountPrivMount(*ctx, nil, target)
		fmt.Printf("unmountPrivMount unmount error: %s\n", err)
		if err != nil {
			f.err = errors.New("error in unmountPrivMount")
			f.theErrorContains(err.Error())
		}
	*/
	return nil
}

func (f *feature) iCallGetPathMounts() error {
	gofsutil.GOFSMock.InduceGetMountsError = true
	_, err := getPathMounts("/foo/bar")
	fmt.Printf(" getPathMounts error : %s\n", err)
	// getMounts induced err
	if err != nil {
		f.err = errors.New("error in GetPathMounts")

	}
	return nil
}

func (f *feature) iCallHandlePrivFSMount() error {

	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY
	gofsutil.GOFSMock.InduceMountError = true
	device := "/dev/scinia"
	sysDevice := &Device{
		Name:     device,
		FullPath: device,
		RealDev:  device,
	}
	fmt.Printf("debug input param sysDevice %#v\n", sysDevice)
	err := handlePrivFSMount(context.TODO(), accessMode, sysDevice, nil, "", "", "")
	msg := "mount induced error"
	fmt.Printf("expected handlePrivFSMount error msg = %s\n", err.Error())
	if err != nil && strings.Contains(err.Error(), msg) {
		f.err = errors.New("error in handlePrivFSMount")
	}
	accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	gofsutil.GOFSMock.InduceMountError = false
	gofsutil.GOFSMock.InduceBindMountError = true
	err = handlePrivFSMount(context.TODO(), accessMode, sysDevice, nil, "", "", "rw")
	msg = "bindMount induced error"
	fmt.Printf("expected handlePrivFSMount error msg= %s\n", err.Error())
	if err != nil && strings.Contains(err.Error(), msg) {
		f.err = errors.New("error in handlePrivFSMount")
	}

	accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
	gofsutil.GOFSMock.InduceMountError = false
	gofsutil.GOFSMock.InduceBindMountError = false
	err = handlePrivFSMount(context.TODO(), accessMode, sysDevice, nil, "", "", "")
	msg = "Invalid access mode"
	fmt.Printf("expected handlePrivFSMount error msg= %s\n", err.Error())
	if err != nil && strings.Contains(err.Error(), msg) {
		f.err = errors.New("error in handlePrivFSMount")
	}
	return nil
}

func (f *feature) iCallEvalSymlinks() error {
	err := evalSymlinks("/foo/bar")
	if err == "/foo/bar" {
		fmt.Printf("evalSymlinks failed: %s\n", err)
		if f.err == nil {
			f.err = errors.New("error in evalSymlinks")
		}
	}
	return nil
}

func (f *feature) iCallRemoveWithRetry() error {
	err := removeWithRetry("/sys/fs/cgroup")
	if err != nil {
		fmt.Printf("removeWithRetry failed: %s\n", err.Error())
		if f.err == nil {
			f.err = err
		}
	}
	return nil
}

func (f *feature) iCallBlockValidateVolCapabilities() error {

	block := new(csi.VolumeCapability_BlockVolume)
	capability := new(csi.VolumeCapability)
	accessType := new(csi.VolumeCapability_Block)
	accessType.Block = block
	capability.AccessType = accessType
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_UNKNOWN
	capability.AccessMode = accessMode

	//isBlock, mntVol, accMode, multiAccessFlag, err := validateVolumeCapability(volCap, ro)

	_, _, _, _, err := validateVolumeCapability(capability, false)
	if err != nil {
		fmt.Printf("error in validateVolCapabilities : %s\n", err.Error())
		if f.err == nil {
			f.err = err
		}
	}
	return nil

}

func (f *feature) iCallMountValidateVolCapabilities() error {

	capability := new(csi.VolumeCapability)
	mountVolume := new(csi.VolumeCapability_MountVolume)
	mountVolume.FsType = "xfs"
	mountVolume.MountFlags = make([]string, 0)
	mount := new(csi.VolumeCapability_Mount)
	mount.Mount = mountVolume
	capability.AccessType = mount
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_UNKNOWN
	capability.AccessMode = accessMode

	_, _, _, _, err := validateVolumeCapability(capability, false)
	if err != nil {
		fmt.Printf("error in validateVolCapabilities : %s\n", err.Error())
		if f.err == nil {
			f.err = err
		}
	}
	return nil

}

func (f *feature) iCallCleanupPrivateTargetForErrors() error {
	gofsutil.GOFSMock.InduceDevMountsError = true
	sysdevice, _ := GetDevice("test/dev/scinia")
	err := cleanupPrivateTarget(sysdevice, "1", "features/d0f055a700000000")
	if err != nil {
		fmt.Printf("Cleanupprivatetarget getDevice error : %s\n", err.Error())
		if f.err == nil {
			f.err = errors.New("error in CleanupPrivateTarget")
		}
	}
	gofsutil.GOFSMock.InduceDevMountsError = false
	gofsutil.GOFSMock.InduceUnmountError = true
	err = cleanupPrivateTarget(sysdevice, "1", "features/d0f055a700000000")
	if err != nil {
		fmt.Printf("Cleanupprivatetarget unmount error: %s\n", err.Error())
		if f.err == nil {
			f.err = errors.New("error in CleanupPrivateTarget")
		}
	}
	gofsutil.GOFSMock.InduceUnmountError = false
	sysdevice, _ = GetDevice("/dev/scinizz")
	err = cleanupPrivateTarget(sysdevice, "1", "/sys/fs/cgroup")
	if err != nil {
		fmt.Printf("Cleanupprivatetarget /tmp : %s\n", err.Error())
		if f.err == nil {
			f.err = errors.New("error in CleanupPrivateTarget")
		}
	}
	return nil
}

func (f *feature) iCallCleanupPrivateTarget() error {
	sysdevice, terr := GetDevice("test/dev/scinia")
	if terr != nil {
		return terr
	}
	err := cleanupPrivateTarget(sysdevice, "1", "features/d0f055a700000000")
	if err != nil {
		fmt.Printf("Cleanupprivatetarget failed: %s\n", err.Error())
		if f.err == nil {
			f.err = err
		}
	}
	return nil
}

func (f *feature) iCallUnmountAndDeleteTarget() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	targetPath := f.nodePublishVolumeRequest.TargetPath
	if err := gofsutil.Unmount(ctx, targetPath); err != nil {
		return err
	}
	if err := os.Remove(targetPath); err != nil {
		fmt.Printf("Unable to remove directory: %v", err)
	}
	return nil
}

func (f *feature) iCallEphemeralNodePublish() error {
	save := ephemeralStagingMountPath
	ephemeralStagingMountPath = "/tmp"
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := new(csi.NodePublishVolumeRequest)
	systemName := "bad-system-config"
	req.VolumeContext = map[string]string{"csi.storage.k8s.io/ephemeral": "true", "volumeName": "xxxx", "size": "8Gi", "storagepool": "pool1", "systemID": systemName}
	_, err := f.service.ephemeralNodePublish(ctx, req)
	if err != nil {
		fmt.Printf("ephemeralNodePublish 1 failed: %s\n", err.Error())
		if f.err == nil {
			f.err = err
		}
	}
	ephemeralStagingMountPath = save
	return nil
}

func (f *feature) iCallEphemeralNodeUnpublish() error {
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := new(csi.NodeUnpublishVolumeRequest)
	req.VolumeId = goodVolumeID

	if stepHandlersErrors.NoVolumeIDError {
		req.VolumeId = ""
	}

	fmt.Printf("Calling ephemeralNodeUnpublishiVolume\n")
	err := f.service.ephemeralNodeUnpublish(ctx, req)
	if err != nil {
		fmt.Printf("ephemeralNodeUnpublishVolume failed: %s\n", err.Error())
		if f.err == nil {
			f.err = err
		}
	} else {
		fmt.Printf("ephemeralNodeUnpublishVolume completed successfully\n")
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
	stringSlice = append(stringSlice, "X_CSI_PRIVATE_MOUNT_DIR=/csi")
	stringSlice = append(stringSlice, "X_CSI_VXFLEXOS_ENABLESNAPSHOTCGDELETE=true")
	stringSlice = append(stringSlice, "X_CSI_VXFLEXOS_ENABLELISTVOLUMESNAPSHOTS=true")
	stringSlice = append(stringSlice, "X_CSI_HEALTH_MONITOR_ENABLED=true")
	stringSlice = append(stringSlice, "X_CSI_RENAME_SDC_ENABLED=true")
	stringSlice = append(stringSlice, "X_CSI_RENAME_SDC_PREFIX=test")
	stringSlice = append(stringSlice, "X_CSI_APPROVE_SDC_ENABLED=true")
	stringSlice = append(stringSlice, "X_CSI_REPLICATION_CONTEXT_PREFIX=test")
	stringSlice = append(stringSlice, "X_CSI_REPLICATION_PREFIX=test")
	stringSlice = append(stringSlice, "X_CSI_POWERFLEX_EXTERNAL_ACCESS=test")
	stringSlice = append(stringSlice, "X_CSI_VXFLEXOS_THICKPROVISIONING=dummy")

	if os.Getenv("ALLOW_RWO_MULTI_POD") == "true" {
		fmt.Printf("debug set ALLOW_RWO_MULTI_POD\n")
		stringSlice = append(stringSlice, "X_CSI_ALLOW_RWO_MULTI_POD_ACCESS=true")
	}
	ctx := context.WithValue(context.Background(), ctxOSEnviron, stringSlice)
	listener, err := net.Listen("tcp", "127.0.0.1:65000")
	if err != nil {
		return err
	}
	os.Setenv(EnvAutoProbe, "true")
	presp, perr := f.service.ProbeController(ctx, nil)
	fmt.Printf("ProbeController resp %#v \n", presp)
	if perr != nil {
		f.err = perr
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
	if f.createVolumeResponse == nil || f.createVolumeResponse.Volume == nil {
		f.volumeID = badVolumeID2
	} else {
		f.volumeID = f.createVolumeResponse.Volume.VolumeId
	}
	req := &csi.NodeExpandVolumeRequest{
		VolumeId:   sdcVolume1,
		VolumePath: volPath,
	}
	if stepHandlersErrors.NoVolumeIDError {
		req.VolumeId = ""
	} else if stepHandlersErrors.VolumeIDTooShortError {
		req.VolumeId = "50"
	} else if stepHandlersErrors.TooManyDashesVolIDError {
		req.VolumeId = "abcd-3"
	} else if stepHandlersErrors.CorrectFormatBadCsiVolID {
		req.VolumeId = badCsiVolumeID
	} else if stepHandlersErrors.EmptySysID {
		req.VolumeId = goodVolumeID
	} else if stepHandlersErrors.BadVolIDError {
		req.VolumeId = badVolumeID
	}
	_, f.err = f.service.NodeExpandVolume(ctx, req)
	return nil
}

func (f *feature) iCallNodeGetVolumeStats() error {
	ctx := new(context.Context)

	VolumeID := sdcVolume1
	VolumePath := datadir

	if stepHandlersErrors.BadVolIDError {
		VolumeID = badVolumeID
	}
	if stepHandlersErrors.NoVolIDError {
		VolumeID = ""
	}
	if stepHandlersErrors.BadMountPathError {
		VolumePath = "there/is/nothing/mounted/here"
	}
	if stepHandlersErrors.NoMountPathError {
		VolumePath = ""
	}
	if stepHandlersErrors.NoVolIDSDCError {
		VolumeID = goodVolumeID
	}
	if stepHandlersErrors.NoVolError {
		VolumeID = "435645643"
		stepHandlersErrors.SIOGatewayVolumeNotFoundError = true
	}
	if stepHandlersErrors.NoSysNameError {
		f.service.opts.defaultSystemID = ""
	}

	req := &csi.NodeGetVolumeStatsRequest{VolumeId: VolumeID, VolumePath: VolumePath}

	f.nodeGetVolumeStatsResponse, f.err = f.service.NodeGetVolumeStats(*ctx, req)

	return nil
}

func (f *feature) aCorrectNodeGetVolumeStatsResponse() error {
	if stepHandlersErrors.NoVolIDError || stepHandlersErrors.NoMountPathError || stepHandlersErrors.BadVolIDError || stepHandlersErrors.NoSysNameError {
		//errors and no responses should be returned in these instances
		if f.nodeGetVolumeStatsResponse == nil {
			fmt.Printf("Response check passed\n")
			return nil
		}

		fmt.Printf("Expected NodeGetVolumeStatsResponse to be nil, but instead, it was %v", f.nodeGetVolumeStatsResponse)
		return status.Errorf(codes.Internal, "Check NodeGetVolumeStatsResponse failed")
	}

	//assume no errors induced, so response should be okay, these values will change below if errors were induced
	abnormal := false
	message := ""
	usage := true

	fmt.Printf("response from NodeGetVolumeStats is: %v\n", f.nodeGetVolumeStatsResponse)

	if stepHandlersErrors.BadMountPathError {
		abnormal = true
		message = "not accessible"
		usage = false
	}

	if gofsutil.GOFSMock.InduceGetMountsError {
		abnormal = true
		message = "not mounted"
		usage = false
	}

	if stepHandlersErrors.NoVolIDSDCError {
		abnormal = true
		message = "not mapped to host"
		usage = false

	}

	if stepHandlersErrors.NoVolError {
		abnormal = true
		message = "Volume is not found"
		usage = false
	}

	//check message and abnormal state returned in NodeGetVolumeStatsResponse.VolumeCondition
	if f.nodeGetVolumeStatsResponse.VolumeCondition.Abnormal == abnormal && strings.Contains(f.nodeGetVolumeStatsResponse.VolumeCondition.Message, message) {
		fmt.Printf("NodeGetVolumeStats Response VolumeCondition check passed\n")
	} else {
		fmt.Printf("Expected nodeGetVolumeStatsResponse.Abnormal to be %v, and message to contain: %s, but instead, abnormal was: %v and message was: %s", abnormal, message, f.nodeGetVolumeStatsResponse.VolumeCondition.Abnormal, f.nodeGetVolumeStatsResponse.VolumeCondition.Message)
		return status.Errorf(codes.Internal, "Check NodeGetVolumeStatsResponse failed")
	}

	//check Usage returned in NodeGetVolumeStatsResponse
	if usage {
		if f.nodeGetVolumeStatsResponse.Usage != nil {
			fmt.Printf("NodeGetVolumeStats Response Usage check passed\n")
			return nil
		}
		fmt.Printf("Expected NodeGetVolumeStats Response to have Usage, but Usage was nil")
		return status.Errorf(codes.Internal, "Check NodeGetVolumeStatsResponse failed")
	}

	return nil
}

func (f *feature) iCallNodeUnstageVolumeWith(error string) error {
	// Save the ephemeralStagingMountPath to restore below
	ephemeralPath := ephemeralStagingMountPath
	header := metadata.New(map[string]string{"csi.requestid": "1"})
	if error == "NoRequestID" {
		header = metadata.New(map[string]string{"csi.requestid": ""})
	}
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := new(csi.NodeUnstageVolumeRequest)
	req.VolumeId = goodVolumeID
	if error == "NoVolumeID" {
		req.VolumeId = ""
	}
	req.StagingTargetPath = datadir
	if error == "NoStagingTarget" {
		req.StagingTargetPath = ""
	}
	if error == "UnmountError" {
		req.StagingTargetPath = "/tmp"
		gofsutil.GOFSMock.InduceUnmountError = true
	}
	if error == "EphemeralVolume" {
		// Create an ephemeral volume id
		ephemeralStagingMountPath = "test/"
		err := os.MkdirAll("test"+"/"+goodVolumeID+"/id", 0777)
		if err != nil {
			return err
		}
	}
	_, f.err = f.service.NodeUnstageVolume(ctx, req)
	ephemeralStagingMountPath = ephemeralPath
	os.Remove("test" + "/" + goodVolumeID + "/id")
	return nil
}

func (f *feature) iCallNodeGetCapabilities(isHealthMonitorEnabled string) error {
	ctx := new(context.Context)
	if isHealthMonitorEnabled == "true" {
		f.service.opts.IsHealthMonitorEnabled = true
	}
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
			case csi.NodeServiceCapability_RPC_SINGLE_NODE_MULTI_WRITER:
				count = count + 1
			case csi.NodeServiceCapability_RPC_VOLUME_CONDITION:
				count = count + 1
			case csi.NodeServiceCapability_RPC_GET_VOLUME_STATS:
				count = count + 1
			default:
				return fmt.Errorf("received unxexpcted capability: %v", typex)
			}
		}

		if f.service.opts.IsHealthMonitorEnabled && count != 4 {
			// Set default value
			f.service.opts.IsHealthMonitorEnabled = false
			return errors.New("Did not retrieve all the expected capabilities")
		} else if !f.service.opts.IsHealthMonitorEnabled && count != 2 {
			return errors.New("Did not retrieve all the expected capabilities")
		}
		// Set default value
		f.service.opts.IsHealthMonitorEnabled = false
		return nil
	}
	return errors.New("expected NodeGetCapabilitiesResponse but didn't get one")
}

func (f *feature) iCallCreateVolumeGroupSnapshot() error {
	ctx := context.Background()
	name := "apple"

	if stepHandlersErrors.NoSysNameError {
		f.service.opts.defaultSystemID = ""
		f.volumeIDList = []string{"1235"}
	}
	if stepHandlersErrors.CreateVGSNoNameError {
		name = ""
	}

	if stepHandlersErrors.CreateVGSNameTooLongError {
		name = "ThisNameIsOverThe27CharacterLimit"
	}
	if stepHandlersErrors.VolIDListEmptyError {
		f.volumeIDList = nil
	}
	if stepHandlersErrors.CreateVGSAcrossTwoArrays {
		f.volumeIDList = []string{"14dbbf5617523654-12235531", "15dbbf5617523655-12345986", "14dbbf5617523654-12456777"}
	}

	if stepHandlersErrors.LegacyVolumeConflictError {
		//need a legacy vol so check map executes
		f.volumeIDList = []string{"1234"}

	}
	if stepHandlersErrors.CreateVGSLegacyVol {
		//make sure legacy vol works
		tokens := strings.Split(f.volumeIDList[0], "-")
		f.volumeIDList[0] = tokens[1]
	}

	req := &volGroupSnap.CreateVolumeGroupSnapshotRequest{
		Name:            name,
		SourceVolumeIDs: f.volumeIDList,
		Parameters:      make(map[string]string),
	}
	if f.VolumeGroupSnapshot != nil {
		// idempotency test
		req.Parameters["existingSnapshotGroupID"] = strings.Split(f.VolumeGroupSnapshot.SnapshotGroupID, "-")[1]
	}

	group, err := f.service.CreateVolumeGroupSnapshot(ctx, req)
	if err != nil {
		f.err = err
	}
	if group != nil {
		f.VolumeGroupSnapshot = group
	}
	return nil
}

func (f *feature) iRemoveAVolumeFromVolumeGroupSnapshotRequest() error {
	//cut last volume off of list
	f.volumeIDList = f.volumeIDList[0 : len(f.volumeIDList)-1]
	return nil
}

func (f *feature) iCallCheckCreationTime() error {
	if f.VolumeGroupSnapshot == nil || f.err != nil {
		return nil
	}
	//add a bad snap so creation time will not match
	if stepHandlersErrors.CreateVGSBadTimeError {

		snap := volGroupSnap.Snapshot{
			Name:          "unit-test-1",
			CapacityBytes: 34359738368,
			SnapId:        "test-1",
			SourceId:      "source-1",
			ReadyToUse:    true,
			CreationTime:  123,
		}
		f.VolumeGroupSnapshot.Snapshots = append(f.VolumeGroupSnapshot.Snapshots, &snap)

	}

	err := checkCreationTime(f.VolumeGroupSnapshot.Snapshots[0].CreationTime, f.VolumeGroupSnapshot.Snapshots)
	if err != nil {
		f.err = err
	}
	return nil
}

func (f *feature) iCallControllerGetVolume() error {

	header := metadata.New(map[string]string{"csi.requestid": "1"})
	ctx := metadata.NewIncomingContext(context.Background(), header)
	req := &csi.ControllerGetVolumeRequest{
		VolumeId: sdcVolume1,
	}
	if stepHandlersErrors.NoVolumeIDError {
		req.VolumeId = ""
	}
	f.ControllerGetVolumeResponse, f.err = f.service.ControllerGetVolume(ctx, req)

	if f.err != nil {
		log.Printf("Controller GetVolume call failed: %s\n", f.err.Error())
	}
	fmt.Printf("Response from ControllerGetVolume is %v", f.ControllerGetVolumeResponse)
	return nil
}

func (f *feature) aValidControllerGetVolumeResponseIsReturned() error {
	if f.err != nil {
		return f.err
	}

	fmt.Printf("volume is %v\n", f.ControllerGetVolumeResponse.Volume)
	fmt.Printf("volume condition is '%s'\n", f.ControllerGetVolumeResponse.Status)

	return nil

}

func (f *feature) aValidCreateVolumeSnapshotGroupResponse() error {
	//only check resp. if CreateVolumeGroupSnapshot returns okay
	if f.VolumeGroupSnapshot == nil || f.err != nil {
		return nil
	}
	err := status.Errorf(codes.Internal, "Bad VolumeSnapshotGroupResponse")
	gID := f.VolumeGroupSnapshot.SnapshotGroupID
	tokens := strings.Split(gID, "-")
	if len(tokens) == 0 {
		fmt.Printf("Error: VolumeSnapshotGroupResponse SnapshotGroupID: %s does not contain systemID \n", gID)
		return err
	}
	for _, snap := range f.VolumeGroupSnapshot.Snapshots {
		snapID := snap.SnapId
		srcID := snap.SourceId
		tokens = strings.Split(snapID, "-")
		if len(tokens) == 0 {
			fmt.Printf("Error: VolumeSnapshotGroupResponse SnapId: %s does not contain systemID \n", snapID)
			return err
		}
		fmt.Printf("SnapId: %s contains systemID: %s \n", snapID, tokens[0])
		tokens = strings.Split(srcID, "-")
		if len(tokens) == 0 {
			fmt.Printf("Error: VolumeSnapshotGroupResponse SourceId: %s does not contain systemID \n", srcID)
			return err
		}
		fmt.Printf("SourceId: %s contains systemID: %s \n", srcID, tokens[0])
	}

	fmt.Printf("VolumeSnapshotGroupResponse looks OK \n")
	return nil
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
		req.SourceVolumeId = badVolumeID2
	} else if f.noVolumeID {
		req.SourceVolumeId = ""
	} else if len(f.volumeIDList) > 1 {
		req.Parameters = make(map[string]string)
		stringList := ""
		for _, v := range f.volumeIDList {
			if stepHandlersErrors.WrongSystemError {
				v = "12345678910-766f6c31"
			}
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

func (f *feature) iCallCreateSnapshotNFS(snapName string) error {
	ctx := new(context.Context)

	req := &csi.CreateSnapshotRequest{
		SourceVolumeId: "14dbbf5617523654" + "/" + fileSystemNameToID["volume1"],
		Name:           snapName,
	}

	if stepHandlersErrors.WrongFileSystemIDError {
		req.SourceVolumeId = "14dbbf5617523654" + "/" + fileSystemNameToID["A Different Volume"]
	}

	if stepHandlersErrors.NoFileSystemIDError {
		req.SourceVolumeId = "14dbbf5617523654" + "/" + fileSystemNameToID["volume2"]
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
	volumeIDToSizeInKB[goodSnapID] = defaultVolumeSize
	volumeIDToReplicationState[goodSnapID] = unmarkedForReplication
	return nil
}

func (f *feature) iCallDeleteSnapshot() error {
	ctx := new(context.Context)
	req := &csi.DeleteSnapshotRequest{SnapshotId: goodSnapID, Secrets: make(map[string]string)}
	req.Secrets["x"] = "y"
	if f.invalidVolumeID {
		req.SnapshotId = badVolumeID2
	} else if f.noVolumeID {
		req.SnapshotId = ""
	}
	_, f.err = f.service.DeleteSnapshot(*ctx, req)
	return nil
}

func (f *feature) iCallDeleteSnapshotNFS() error {
	ctx := new(context.Context)
	var req *csi.DeleteSnapshotRequest = new(csi.DeleteSnapshotRequest)
	if fileSystemNameToID["snap1"] == "" {
		req = &csi.DeleteSnapshotRequest{SnapshotId: "14dbbf5617523654" + "/" + "1111111", Secrets: make(map[string]string)}
	} else {
		req = &csi.DeleteSnapshotRequest{SnapshotId: "14dbbf5617523654" + "/" + fileSystemNameToID["snap1"], Secrets: make(map[string]string)}
	}

	req.Secrets["x"] = "y"
	_, f.err = f.service.DeleteSnapshot(*ctx, req)
	return nil
}

func (f *feature) aValidSnapshotConsistencyGroup() error {
	// first snapshot in CG
	volumeIDToName[goodSnapID] = "snap4"
	volumeNameToID["snap4"] = goodSnapID
	volumeIDToAncestorID[goodSnapID] = goodVolumeID
	volumeIDToConsistencyGroupID[goodSnapID] = goodVolumeID
	volumeIDToSizeInKB[goodSnapID] = "8192"
	volumeIDToReplicationState[goodSnapID] = unmarkedForReplication

	// second snapshot in CG; this looks weird, but we give same ID to snap
	// as it's ancestor so that we can publish the volume
	volumeIDToName[altSnapID] = "snap5"
	volumeNameToID["snap5"] = altSnapID
	volumeIDToAncestorID[altSnapID] = altVolumeID
	volumeIDToConsistencyGroupID[altSnapID] = goodVolumeID
	volumeIDToSizeInKB[altSnapID] = "8192"
	volumeIDToReplicationState[altSnapID] = unmarkedForReplication

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

func (f *feature) iCallCreateVolumeFromSnapshotNFS() error {
	ctx := new(context.Context)
	req := getTypicalNFSCreateVolumeRequest()
	req.Name = "volumeFromSnap"
	if f.wrongCapacity {
		req.CapacityRange.RequiredBytes = 64 * 1024 * 1024 * 1024
	}
	if f.wrongStoragePool {
		req.Parameters["storagepool"] = "other_storage_pool"
	}
	source := &csi.VolumeContentSource_SnapshotSource{SnapshotId: "14dbbf5617523654" + "/" + fileSystemNameToID["snap1"]}
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
		id := fmt.Sprintf(arrayID+"-%d", f.snapshotIndex)
		volumeIDToName[id] = name
		volumeNameToID[name] = id
		volumeIDToAncestorID[id] = volumeID
		volumeIDToSizeInKB[id] = "8192"
		volumeIDToReplicationState[id] = unmarkedForReplication
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
	req.StartingToken = "0"
	req.MaxEntries = 100

	if stepHandlersErrors.BadVolIDError {
		req.SourceVolumeId = "Not at all valid"
		req.SnapshotId = "14dbbf5617523654-11111111"
	}

	f.listSnapshotsRequest = req
	log.Printf("Calling ListSnapshots with req=%+v", f.listSnapshotsRequest)
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
		f.service.opts.arrays[arrayID].Endpoint = ""
		f.service.opts.arrays[arrayID2].Endpoint = ""
		f.service.opts.AutoProbe = true
	} else if stepHandlersErrors.NoUserError {
		f.service.opts.arrays[arrayID].Username = ""
		f.service.opts.arrays[arrayID2].Username = ""
	} else if stepHandlersErrors.NoPasswordError {
		f.service.opts.arrays[arrayID].Password = ""
		f.service.opts.arrays[arrayID2].Password = ""
	} else if stepHandlersErrors.NoSysNameError {
		f.service.opts.arrays[arrayID].SystemID = ""
		f.service.opts.arrays[arrayID2].SystemID = ""
	} else if stepHandlersErrors.WrongSysNameError {
		f.service.opts.arrays[arrayID].SystemID = "WrongSystemName"
		f.service.opts.arrays[arrayID2].SystemID = "WrongSystemName"
	} else if testControllerHasNoConnection {
		f.service.adminClients[arrayID] = nil
		f.service.adminClients[arrayID2] = nil
	}

	return nil
}

func (f *feature) iCallupdateVolumesMap(systemID string) error {

	f.service.volumePrefixToSystems["123"] = []string{"123456789"}
	f.err = f.service.UpdateVolumePrefixToSystemsMap(systemID)
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

func (f *feature) undoSetupGetSystemIDtoFail() error {
	setupGetSystemIDtoFail = false
	return nil
}

func (f *feature) setupGetSystemIDtoFail() error {
	setupGetSystemIDtoFail = true
	return nil
}

func (f *feature) iCallGetVolumeIDFromCsiVolumeID(csiVolID string) error {
	v := getVolumeIDFromCsiVolumeID(csiVolID)
	fmt.Printf("DEBUG getVol %s\n", v)
	out := fmt.Sprintf("Got %s\n", v)
	f.err = errors.New(out)
	return nil
}

func (f *feature) iCallGetFileSystemIDFromCsiVolumeID(csiVolID string) error {
	fmt.Println("csiVolID", csiVolID)
	v := getFilesystemIDFromCsiVolumeID(csiVolID)
	fmt.Println("i got", v)
	f.fileSystemID = v
	return nil
}

func (f *feature) theFileSystemIDIs(fsID string) error {
	if fsID == f.fileSystemID {
		return nil
	}
	return fmt.Errorf("expected %s but got %s", fsID, f.fileSystemID)
}

func (f *feature) iCallGetSystemIDFromCsiVolumeID(csiVolID string) error {
	s := f.service.getSystemIDFromCsiVolumeID(csiVolID)
	fmt.Printf("DEBUG getSystem %s\n", s)
	out := fmt.Sprintf("Got %s\n", s)
	f.err = errors.New(out)
	return nil
}

func (f *feature) iCallGetSystemIDFromCsiVolumeIDNfs(csiVolID string) error {
	s := f.service.getSystemIDFromCsiVolumeID(csiVolID)
	f.systemID = s
	return nil
}

func (f *feature) theSystemIDIs(systemID string) error {
	if systemID == f.systemID {
		return nil
	}
	return fmt.Errorf("expected %s but got %s", systemID, f.fileSystemID)
}

func (f *feature) iCallGetSystemIDFromParameters(option string) error {
	params := make(map[string]string)
	saveID := f.service.opts.defaultSystemID
	saveArrays := f.service.opts.arrays

	if option == "NoSystemIDKey" {
		params["NoID"] = "xxx"
		f.service.opts.arrays = nil
	} else if option == "good" {
		params["SystemID"] = arrayID
	} else if option == "NilParams" {
		params = nil
	}
	_, err := f.service.getSystemIDFromParameters(params)
	if err != nil {
		f.service.opts.defaultSystemID = saveID
		f.service.opts.arrays = saveArrays
		f.err = err
		return nil
	}

	return nil
}

func (f *feature) iCallGetStoragePoolnameByID(id string) error {
	f.service.storagePoolIDToName[id] = ""
	res := f.service.getStoragePoolNameFromID(arrayID, id)
	if res == "" {
		f.err = errors.New("cannot find storage pool")
	}
	return nil
}

func (f *feature) iCallGetSystemNameMatchingError() error {
	systems := make([]string, 0)
	id := "9999999999999999"
	systems = append(systems, id)
	systems = append(systems, "14dbbf5617523654")
	stepHandlersErrors.systemNameMatchingError = true
	f.service.getSystemName(context.TODO(), systems)
	return nil
}

func (f *feature) iCallGetSystemNameError() error {
	id := "9999999999999999"
	// this works with wrong id
	badarray := f.service.opts.arrays[arrayID]
	badarray.SystemID = ""

	f.service.opts.arrays = make(map[string]*ArrayConnectionData)
	f.service.opts.arrays[id] = badarray

	stepHandlersErrors.PodmonNodeProbeError = true
	//  Unable to probe system with ID:
	ctx := new(context.Context)
	f.err = f.service.systemProbe(*ctx, badarray)
	return nil

}

func (f *feature) iCallGetSystemName() error {
	systems := make([]string, 0)
	systems = append(systems, arrayID)
	f.service.getSystemName(context.TODO(), systems)
	return nil

}

func (f *feature) iCallNodeGetAllSystems() error {
	// lookup the system names for a couple of systems
	// This should not generate an error as systems without names are supported
	ctx := new(context.Context)
	badarray := f.service.opts.arrays[arrayID]
	f.err = f.service.systemProbe(*ctx, badarray)
	return nil
}

func (f *feature) iDoNotHaveAGatewayConnection() error {
	f.service.adminClients[arrayID] = nil
	f.service.adminClients["mocksystem"] = nil
	return nil
}

func (f *feature) iDoNotHaveAValidGatewayEndpoint() error {
	f.service.opts.arrays[arrayID].Endpoint = ""
	return nil
}

func (f *feature) iDoNotHaveAValidGatewayPassword() error {
	f.service.opts.arrays[arrayID].Password = ""
	return nil
}

func (f *feature) theValidateConnectivityResponseMessageContains(expected string) error {
	resp := f.validateVolumeHostConnectivityResp
	if resp != nil {
		for _, m := range resp.Messages {
			if strings.Contains(m, expected) {
				return nil
			}
		}
	}
	return fmt.Errorf("Expected %s message in ValidateVolumeHostConnectivityResp but it wasn't there", expected)
}

func (f *feature) anInvalidConfig(config string) error {
	ArrayConfigFile = config
	return nil
}

func (f *feature) anInvalidMaxVolumesPerNode() error {
	f.service.opts.MaxVolumesPerNode = -1
	return nil
}

func (f *feature) iCallGetArrayConfig() error {
	ctx := new(context.Context)
	_, err := getArrayConfig(*ctx)
	if err != nil {
		f.err = err
	}
	return nil
}

func (f *feature) iCallgetArrayInstallationID(systemID string) error {
	id := ""
	id, f.err = f.service.getArrayInstallationID(systemID)
	fmt.Printf("Installation ID is: %s\n", id)
	return nil
}

func (f *feature) iCallSetQoSParameters(systemID string, sdcID string, bandwidthLimit string, iopsLimit string, volumeName string, csiVolID string, nodeID string) error {
	ctx := new(context.Context)
	f.err = f.service.setQoSParameters(*ctx, systemID, sdcID, bandwidthLimit, iopsLimit, volumeName, csiVolID, nodeID)
	if f.err != nil {
		fmt.Printf("error in setting QoS parameters for volume %s : %s\n", volumeName, f.err.Error())
	}
	return nil
}

func (f *feature) iUseConfig(filename string) error {
	ArrayConfigFile = "./features/array-config/" + filename
	var err error
	f.service.opts.arrays, err = getArrayConfig(context.Background())
	if err != nil {
		return fmt.Errorf("invalid array config: %s", err.Error())
	}
	if f.service.opts.arrays != nil {
		f.service.opts.arrays[arrayID].Endpoint = f.server.URL
		if f.service.opts.arrays[arrayID2] != nil {
			f.service.opts.arrays[arrayID2].Endpoint = f.server2.URL
		}
	}

	fmt.Printf("****************************************************** s.opts.arrays %v\n", f.service.opts.arrays)
	f.service.systemProbeAll(context.Background())
	f.adminClient = f.service.adminClients[arrayID]
	f.adminClient2 = f.service.adminClients[arrayID2]
	if f.adminClient == nil {
		return fmt.Errorf("adminClient nil")
	}
	if f.adminClient2 == nil {
		return fmt.Errorf("adminClient2 nil")
	}
	return nil
}

func (f *feature) iCallGetReplicationCapabilities() error {
	req := &replication.GetReplicationCapabilityRequest{}
	ctx := new(context.Context)
	f.replicationCapabilitiesResponse, f.err = f.service.GetReplicationCapabilities(*ctx, req)
	log.Printf("GetReplicationCapabilities returned %+v", f.replicationCapabilitiesResponse)
	return nil
}

func (f *feature) aReplicationCapabilitiesStructureIsReturned(arg1 string) error {
	if f.err != nil {
		return f.err
	}
	var createRemoteVolume, createProtectionGroup, deleteProtectionGroup, monitorProtectionGroup, replicationActionExecution bool
	for _, cap := range f.replicationCapabilitiesResponse.GetCapabilities() {
		if cap == nil {
			continue
		}
		rpc := cap.GetRpc()
		if rpc == nil {
			continue
		}
		ty := rpc.GetType()
		switch ty {
		case replication.ReplicationCapability_RPC_CREATE_REMOTE_VOLUME:
			createRemoteVolume = true
		case replication.ReplicationCapability_RPC_CREATE_PROTECTION_GROUP:
			createProtectionGroup = true
		case replication.ReplicationCapability_RPC_DELETE_PROTECTION_GROUP:
			deleteProtectionGroup = true
		case replication.ReplicationCapability_RPC_MONITOR_PROTECTION_GROUP:
			monitorProtectionGroup = true
		case replication.ReplicationCapability_RPC_REPLICATION_ACTION_EXECUTION:
			replicationActionExecution = true
		}
	}
	var failoverRemote, unplannedFailoverLocal, reprotectLocal, suspend, resume, sync bool
	for _, act := range f.replicationCapabilitiesResponse.GetActions() {
		if act == nil {
			continue
		}
		ty := act.GetType()
		switch ty {
		case replication.ActionTypes_FAILOVER_REMOTE:
			failoverRemote = true
		case replication.ActionTypes_UNPLANNED_FAILOVER_LOCAL:
			unplannedFailoverLocal = true
		case replication.ActionTypes_REPROTECT_LOCAL:
			reprotectLocal = true
		case replication.ActionTypes_SUSPEND:
			suspend = true
		case replication.ActionTypes_RESUME:
			resume = true
		case replication.ActionTypes_SYNC:
			sync = true
		}
	}
	if !createRemoteVolume || !createProtectionGroup || !deleteProtectionGroup || !replicationActionExecution || !monitorProtectionGroup {
		return fmt.Errorf("Not all expected ReplicationCapability_RPC capabilities were returned")
	}
	if !failoverRemote || !unplannedFailoverLocal || !reprotectLocal || !suspend || !resume || !sync {
		return fmt.Errorf("Not all expected ReplicationCapbility_RPC actions were returned")
	}
	return nil
}

func (f *feature) iSetRenameSdcEnabledWithPrefix(renameEnabled string, prefix string) error {
	if renameEnabled == "true" {
		f.service.opts.IsSdcRenameEnabled = true
	}
	f.service.opts.SdcPrefix = prefix
	return nil
}

func (f *feature) iSetApproveSdcEnabled(approveSDCEnabled string) error {
	if approveSDCEnabled == "true" {
		f.service.opts.IsApproveSDCEnabled = true
	}
	return nil
}

func (f *feature) iCallCreateRemoteVolume() error {
	ctx := new(context.Context)
	req := &replication.CreateRemoteVolumeRequest{}
	if f.createVolumeResponse == nil {
		return errors.New("iCallCreateRemoteVolume: f.createVolumeResponse is nil")
	}
	req.VolumeHandle = f.createVolumeResponse.Volume.VolumeId
	if stepHandlersErrors.NoVolIDError {
		req.VolumeHandle = ""
	}
	if stepHandlersErrors.BadVolIDError {
		req.VolumeHandle = "/"
	}
	req.Parameters = map[string]string{
		f.service.WithRP(KeyReplicationRemoteStoragePool): "viki_pool_HDD_20181031",
		f.service.WithRP(KeyReplicationRemoteSystem):      "15dbbf5617523655",
	}
	_, f.err = f.service.CreateRemoteVolume(*ctx, req)
	if f.err != nil {
		fmt.Printf("CreateRemoteVolumeRequest returned error: %s", f.err)
	}
	return nil
}

func (f *feature) iCallDeleteLocalVolume(name string) error {
	ctx := new(context.Context)

	replicatedVolName := "replicated-" + name
	volumeHandle := arrayID2 + "-" + volumeNameToID[replicatedVolName]

	if inducedError.Error() == "BadVolumeHandleError" {
		volumeHandle = ""
	}
	if stepHandlersErrors.BadVolIDError {
		volumeHandle = "/"
	}

	log.Printf("iCallDeleteLocalVolume name %s to ID %s", name, volumeHandle)

	req := &replication.DeleteLocalVolumeRequest{
		VolumeHandle: volumeHandle,
	}

	_, f.err = f.service.DeleteLocalVolume(*ctx, req)
	if f.err != nil {
		fmt.Printf("DeleteLocalVolume returned error: %s", f.err)
	}

	return nil
}

func (f *feature) iCallCreateStorageProtectionGroup() error {
	ctx := new(context.Context)
	parameters := make(map[string]string)

	// Must be repeatable.
	clusterUID := f.clusterUID

	if inducedError.Error() != "EmptyParametersListError" {
		parameters[f.service.WithRP(KeyReplicationRemoteSystem)] = arrayID2
		parameters[f.service.WithRP(KeyReplicationRPO)] = "60"
		parameters[f.service.WithRP(KeyReplicationClusterID)] = "cluster-k212"
		parameters["clusterUID"] = clusterUID
	}

	if inducedError.Error() == "BadRemoteSystem" {
		parameters[f.service.WithRP(KeyReplicationRemoteSystem)] = "xxx"
	}

	if inducedError.Error() == "NoRemoteSystem" {
		delete(parameters, f.service.WithRP(KeyReplicationRemoteSystem))
	}

	if inducedError.Error() == "NoRPOSpecified" {
		delete(parameters, f.service.WithRP(KeyReplicationRPO))
	}

	if inducedError.Error() == "NoRemoteClusterID" {
		delete(parameters, f.service.WithRP(KeyReplicationClusterID))
	}

	req := &replication.CreateStorageProtectionGroupRequest{
		VolumeHandle: f.createVolumeResponse.GetVolume().VolumeId,
		Parameters:   parameters,
	}
	fmt.Printf("CreateStorageProtectionGroupRequest %+v\n", req)
	fmt.Printf("StorageProtectionGroupRequest volumeHandle %s\n", req.VolumeHandle)
	if stepHandlersErrors.NoVolIDError {
		req.VolumeHandle = ""
	}
	if stepHandlersErrors.BadVolIDError {
		req.VolumeHandle = "0%0"
	}
	f.createStorageProtectionGroupResponse, f.err = f.service.CreateStorageProtectionGroup(*ctx, req)
	return nil
}

func (f *feature) iCallCreateStorageProtectionGroupWith(arg1, arg2, arg3 string) error {
	ctx := new(context.Context)
	parameters := make(map[string]string)

	// Must be repeatable.
	clusterUID := f.clusterUID

	parameters["clusterUID"] = clusterUID
	parameters[f.service.WithRP(KeyReplicationRemoteSystem)] = arrayID2
	parameters[f.service.WithRP(KeyReplicationRPO)] = arg3
	parameters[f.service.WithRP(KeyReplicationConsistencyGroupName)] = arg1
	parameters[f.service.WithRP(KeyReplicationClusterID)] = arg2

	req := &replication.CreateStorageProtectionGroupRequest{
		VolumeHandle: f.createVolumeResponse.GetVolume().VolumeId,
		Parameters:   parameters,
	}

	f.createStorageProtectionGroupResponse, f.err = f.service.CreateStorageProtectionGroup(*ctx, req)
	return nil
}

func (f *feature) iCallGetStorageProtectionGroupStatus() error {
	ctx := new(context.Context)
	attributes := make(map[string]string)

	replicationGroupConsistMode = defaultConsistencyMode

	attributes[f.service.opts.replicationContextPrefix+"systemName"] = arrayID
	req := &replication.GetStorageProtectionGroupStatusRequest{
		ProtectionGroupId:         f.createStorageProtectionGroupResponse.LocalProtectionGroupId,
		ProtectionGroupAttributes: attributes,
	}
	_, f.err = f.service.GetStorageProtectionGroupStatus(*ctx, req)

	return nil
}

func (f *feature) iCallGetStorageProtectionGroupStatusWithStateAndMode(arg1, arg2 string) error {
	ctx := new(context.Context)
	attributes := make(map[string]string)

	replicationGroupState = arg1
	replicationGroupConsistMode = arg2

	attributes[f.service.opts.replicationContextPrefix+"systemName"] = arrayID
	req := &replication.GetStorageProtectionGroupStatusRequest{
		ProtectionGroupId:         f.createStorageProtectionGroupResponse.LocalProtectionGroupId,
		ProtectionGroupAttributes: attributes,
	}
	_, f.err = f.service.GetStorageProtectionGroupStatus(*ctx, req)

	return nil
}

func (f *feature) iCallDeleteVolume(name string) error {
	for id, name := range volumeIDToName {
		fmt.Printf("volIDToName id %s name %s\n", id, name)
	}
	for name, id := range volumeNameToID {
		fmt.Printf("volNameToID name %s id %s\n", name, id)
	}
	ctx := new(context.Context)
	req := f.getControllerDeleteVolumeRequest("single-writer")
	id := arrayID + "-" + volumeNameToID[name]
	log.Printf("iCallDeleteVolume name %s to ID %s", name, id)
	req.VolumeId = id
	f.deleteVolumeResponse, f.err = f.service.DeleteVolume(*ctx, req)
	if f.err != nil {
		fmt.Printf("DeleteVolume error: %s", f.err)
	}
	return nil
}

func (f *feature) iCallDeleteStorageProtectionGroup() error {
	ctx := new(context.Context)
	attributes := make(map[string]string)
	attributes[f.service.opts.replicationContextPrefix+"systemName"] = arrayID
	req := &replication.DeleteStorageProtectionGroupRequest{
		ProtectionGroupId:         f.createStorageProtectionGroupResponse.LocalProtectionGroupId,
		ProtectionGroupAttributes: attributes,
	}
	f.deleteStorageProtectionGroupResponse, f.err = f.service.DeleteStorageProtectionGroup(*ctx, req)
	return nil
}

func (f *feature) iCallExecuteAction(arg1 string) error {
	ctx := new(context.Context)
	attributes := make(map[string]string)
	remoteAttributes := make(map[string]string)

	var action replication.ExecuteActionRequest_Action
	var act replication.Action

	switch arg1 {
	case "CreateSnapshot":
		act.ActionTypes = replication.ActionTypes_CREATE_SNAPSHOT
		getRemoteSnapDelay = 10 * time.Millisecond
	case "FailoverRemote":
		act.ActionTypes = replication.ActionTypes_FAILOVER_REMOTE
	case "UnplannedFailover":
		act.ActionTypes = replication.ActionTypes_UNPLANNED_FAILOVER_LOCAL
	case "ReprotectLocal":
		act.ActionTypes = replication.ActionTypes_REPROTECT_LOCAL
	case "Resume":
		act.ActionTypes = replication.ActionTypes_RESUME
	case "Suspend":
		act.ActionTypes = replication.ActionTypes_SUSPEND
	case "Sync":
		act.ActionTypes = replication.ActionTypes_SYNC
	default:
		act.ActionTypes = replication.ActionTypes_UNKNOWN_ACTION
	}

	action.Action = &act

	attributes[f.service.opts.replicationContextPrefix+"systemName"] = arrayID
	remoteAttributes[f.service.opts.replicationContextPrefix+"systemName"] = arrayID2
	req := &replication.ExecuteActionRequest{
		ProtectionGroupId:               f.createStorageProtectionGroupResponse.LocalProtectionGroupId,
		ProtectionGroupAttributes:       attributes,
		RemoteProtectionGroupId:         f.createStorageProtectionGroupResponse.RemoteProtectionGroupId,
		RemoteProtectionGroupAttributes: remoteAttributes,
		ActionTypes:                     &action,
	}

	_, f.err = f.service.ExecuteAction(*ctx, req)
	return nil
}

func (f *feature) iCallEnableFSQuota() error {
	f.service.opts.IsQuotaEnabled = true
	isQuotaEnabled = true
	return nil
}

func (f *feature) iCallDisableFSQuota() error {
	f.service.opts.IsQuotaEnabled = false
	isQuotaEnabled = false
	return nil
}

func (f *feature) iCallSetQuotaParams(path, softlimit, graceperiod string) error {
	if f.createVolumeRequest == nil {
		req := getTypicalNFSCreateVolumeRequest()
		f.createVolumeRequest = req
	}
	f.createVolumeRequest.Parameters["path"] = path
	f.createVolumeRequest.Parameters["softLimit"] = softlimit
	f.createVolumeRequest.Parameters["gracePeriod"] = graceperiod
	return nil
}

func (f *feature) iSpecifyNoPath() error {
	if f.createVolumeRequest == nil {
		req := getTypicalNFSCreateVolumeRequest()
		f.createVolumeRequest = req
	}
	delete(f.createVolumeRequest.Parameters, "path")
	return nil
}

func (f *feature) iSpecifyNoSoftLimit() error {
	if f.createVolumeRequest == nil {
		req := getTypicalNFSCreateVolumeRequest()
		f.createVolumeRequest = req
	}
	delete(f.createVolumeRequest.Parameters, "softLimit")
	return nil
}

func (f *feature) iSpecifyNoGracePeriod() error {
	if f.createVolumeRequest == nil {
		req := getTypicalNFSCreateVolumeRequest()
		f.createVolumeRequest = req
	}
	delete(f.createVolumeRequest.Parameters, "gracePeriod")
	return nil
}

func (f *feature) iCallParseCIDR(ip string) error {
	_, err := ParseCIDR(ip)
	f.err = err
	return nil
}

func (f *feature) iCallGetIPListWithMaskFromString(ip string) error {
	_, err := GetIPListWithMaskFromString(ip)
	f.err = err
	return nil
}

func (f *feature) iCallparseMask(ip string) error {
	_, err := parseMask(ip)
	f.err = err
	return nil
}

func (f *feature) iCallGivenNFSExport(nfsexporthost string) error {
	f.nfsExport = types.NFSExport{
		ReadWriteHosts: []string{nfsexporthost},
	}
	return nil
}

func (f *feature) iCallexternalAccessAlreadyAdded(externalAccess string) error {
	export := f.nfsExport
	resp := externalAccessAlreadyAdded(&export, externalAccess)
	if resp {
		f.err = errors.New("external access exists")
	} else {
		f.err = errors.New("external access does not exist")
	}
	return nil
}

func FeatureContext(s *godog.ScenarioContext) {
	f := &feature{}
	s.Step(`^a VxFlexOS service$`, f.aVxFlexOSService)
	s.Step(`^a VxFlexOS service with timeout (\d+) milliseconds$`, f.aVxFlexOSServiceWithTimeoutMilliseconds)
	s.Step(`^I call GetPluginInfo$`, f.iCallGetPluginInfo)
	s.Step(`^I call DynamicArrayChange$`, f.iCallDynamicArrayChange)
	s.Step(`^a valid DynamicArrayChange occurs$`, f.aValidDynamicArrayChange)
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
	s.Step(`^the fileSystemID is "([^"]*)"$`, f.theFileSystemIDIs)
	s.Step(`^the systemID is "([^"]*)"$`, f.theSystemIDIs)
	s.Step(`^the possible error contains "([^"]*)"$`, f.thePossibleErrorContains)
	s.Step(`^the Controller has no connection$`, f.theControllerHasNoConnection)
	s.Step(`^there is a Node Probe Lsmod error$`, f.thereIsANodeProbeLsmodError)
	s.Step(`^there is a Node Probe SdcGUID error$`, f.thereIsANodeProbeSdcGUIDError)
	s.Step(`^there is a Node Probe drvCfg error$`, f.thereIsANodeProbeDrvCfgError)
	s.Step(`^I call CreateVolume "([^"]*)"$`, f.iCallCreateVolume)
	s.Step(`^I call ValidateConnectivity$`, f.iCallValidateVolumeHostConnectivity)
	s.Step(`^a valid CreateVolumeResponse is returned$`, f.aValidCreateVolumeResponseIsReturned)
	s.Step(`^I specify AccessibilityRequirements with a SystemID of "([^"]*)"$`, f.iSpecifyAccessibilityRequirementsWithASystemIDOf)
	s.Step(`^I specify NFS AccessibilityRequirements with a SystemID of "([^"]*)"$`, f.iSpecifyAccessibilityRequirementsNFSWithASystemIDOf)
	s.Step(`^I specify bad NFS AccessibilityRequirements with a SystemID of "([^"]*)"$`, f.iSpecifyBadAccessibilityRequirementsNFSWithASystemIDOf)
	s.Step(`^a valid CreateVolumeResponse with topology is returned$`, f.aValidCreateVolumeResponseWithTopologyIsReturned)
	s.Step(`^I specify MULTINODE_WRITER$`, f.iSpecifyMULTINODEWRITER)
	s.Step(`^I specify a BadCapacity$`, f.iSpecifyABadCapacity)
	s.Step(`^I specify NoStoragePool$`, f.iSpecifyNoStoragePool)
	s.Step(`^I call CreateVolumeSize "([^"]*)" "(\d+)"$`, f.iCallCreateVolumeSize)
	s.Step(`^I call CreateVolumeSize nfs "([^"]*)" "(\d+)"$`, f.iCallCreateVolumeSizeNFS)

	s.Step(`^I change the StoragePool "([^"]*)"$`, f.iChangeTheStoragePool)
	s.Step(`^I induce error "([^"]*)"$`, f.iInduceError)
	s.Step(`^I specify VolumeContentSource$`, f.iSpecifyVolumeContentSource)
	s.Step(`^I specify CreateVolumeMountRequest "([^"]*)"$`, f.iSpecifyCreateVolumeMountRequest)
	s.Step(`^I call PublishVolume with "([^"]*)"$`, f.iCallPublishVolumeWith)
	s.Step(`^I call NFS PublishVolume with "([^"]*)"$`, f.iCallPublishVolumeWithNFS)
	s.Step(`^a valid PublishVolumeResponse is returned$`, f.aValidPublishVolumeResponseIsReturned)
	s.Step(`^a valid volume$`, f.aValidVolume)
	s.Step(`^an invalid volume$`, f.anInvalidVolume)
	s.Step(`^I set bad FileSystem Id`, f.aBadFileSystem)
	s.Step(`^I set bad NFSExport Id`, f.aBadNFSExport)
	s.Step(`^no volume$`, f.noVolume)
	s.Step(`^no node$`, f.noNode)
	s.Step(`^no volume capability$`, f.noVolumeCapability)
	s.Step(`^no access mode$`, f.noAccessMode)
	s.Step(`^then I use a different nodeID$`, f.thenIUseADifferentNodeID)
	s.Step(`^I use AccessType Mount$`, f.iUseAccessTypeMount)
	s.Step(`^no error was received$`, f.noErrorWasReceived)
	s.Step(`^I call UnpublishVolume$`, f.iCallUnpublishVolume)
	s.Step(`^I call UnpublishVolume nfs`, f.iCallUnpublishVolumeNFS)
	s.Step(`^a valid UnpublishVolumeResponse is returned$`, f.aValidUnpublishVolumeResponseIsReturned)
	s.Step(`^the number of SDC mappings is (\d+)$`, f.theNumberOfSDCMappingsIs)
	s.Step(`^I call NodeGetInfo$`, f.iCallNodeGetInfo)
	s.Step(`^I call Node Probe$`, f.iCallNodeProbe)
	s.Step(`^a valid NodeGetInfoResponse is returned$`, f.aValidNodeGetInfoResponseIsReturned)
	s.Step(`^the Volume limit is set$`, f.theVolumeLimitIsSet)
	s.Step(`^an invalid MaxVolumesPerNode$`, f.anInvalidMaxVolumesPerNode)
	s.Step(`^I call GetNodeLabels$`, f.iCallGetNodeLabels)
	s.Step(`^a valid label is returned$`, f.aValidLabelIsReturned)
	s.Step(`^I set invalid EnvMaxVolumesPerNode$`, f.iSetInvalidEnvMaxVolumesPerNode)
	s.Step(`^I call GetNodeLabels with invalid node$`, f.iCallGetNodeLabelsWithInvalidNode)
	s.Step(`^I call NodeGetInfo with valid volume limit node labels$`, f.iCallNodeGetInfoWithValidVolumeLimitNodeLabels)
	s.Step(`^I call NodeGetInfo with invalid volume limit node labels$`, f.iCallNodeGetInfoWithInvalidVolumeLimitNodeLabels)
	s.Step(`^I call GetNodeLabels with unset KubernetesClient$`, f.iCallGetNodeLabelsWithUnsetKubernetesClient)
	s.Step(`^I call DeleteVolume with "([^"]*)"$`, f.iCallDeleteVolumeWith)
	s.Step(`^I call DeleteVolume with Bad "([^"]*)"$`, f.iCallDeleteVolumeWithBad)
	s.Step(`^I call DeleteVolume nfs with "([^"]*)"$`, f.iCallDeleteVolumeNFSWith)
	s.Step(`^a valid DeleteVolumeResponse is returned$`, f.aValidDeleteVolumeResponseIsReturned)
	s.Step(`^the volume is already mapped to an SDC$`, f.theVolumeIsAlreadyMappedToAnSDC)
	s.Step(`^I call GetCapacity with storage pool "([^"]*)"$`, f.iCallGetCapacityWithStoragePool)
	s.Step(`^a valid GetCapacityResponse is returned$`, f.aValidGetCapacityResponseIsReturned)
	s.Step(`^a valid GetCapacityResponse1 is returned$`, f.aValidGetCapacityResponsewithmaxvolsizeIsReturned)
	s.Step(`^I call get GetMaximumVolumeSize with systemid "([^"]*)"$`, f.iCallGetMaximumVolumeSize)
	s.Step(`^I call ControllerGetCapabilities "([^"]*)"$`, f.iCallControllerGetCapabilities)
	s.Step(`^a valid ControllerGetCapabilitiesResponse is returned$`, f.aValidControllerGetCapabilitiesResponseIsReturned)
	s.Step(`^I call ValidateVolumeCapabilities with voltype "([^"]*)" access "([^"]*)" fstype "([^"]*)"$`, f.iCallValidateVolumeCapabilitiesWithVoltypeAccessFstype)
	s.Step(`^a valid ListVolumesResponse is returned$`, f.aValidListVolumesResponseIsReturned)
	s.Step(`^I call ListVolumes with max_entries "([^"]*)" and starting_token "([^"]*)"$`, f.iCallListVolumesWith)
	s.Step(`^I call ListVolumes again with max_entries "([^"]*)" and starting_token "([^"]*)"$`, f.iCallListVolumesAgainWith)
	s.Step(`^there (?:are|is) (\d+) valid volumes?$`, f.thereAreValidVolumes)
	s.Step(`^(\d+) volume(?:s)? (?:are|is) listed$`, f.volumesAreListed)
	s.Step(`^an invalid ListVolumesResponse is returned$`, f.anInvalidListVolumesResponseIsReturned)
	s.Step(`^setup Get SystemID to fail$`, f.setupGetSystemIDtoFail)
	s.Step(`^undo setup Get SystemID to fail$`, f.undoSetupGetSystemIDtoFail)
	s.Step(`^a capability with voltype "([^"]*)" access "([^"]*)" fstype "([^"]*)"$`, f.aCapabilityWithVoltypeAccessFstype)
	s.Step(`^a controller published volume$`, f.aControllerPublishedVolume)
	s.Step(`^I call NodePublishVolume "([^"]*)"$`, f.iCallNodePublishVolume)
	s.Step(`^I call NodePublishVolume NFS "([^"]*)"$`, f.iCallNodePublishVolumeNFS)
	s.Step(`^I call CleanupPrivateTarget$`, f.iCallCleanupPrivateTarget)
	s.Step(`^I call removeWithRetry$`, f.iCallRemoveWithRetry)
	s.Step(`^I call evalSymlinks$`, f.iCallEvalSymlinks)
	s.Step(`^I call unmountPrivMount$`, f.iCallUnmountPrivMount)
	s.Step(`^I call CleanupPrivateTarget for errors$`, f.iCallCleanupPrivateTargetForErrors)

	s.Step(`^I call getPathMounts$`, f.iCallGetPathMounts)
	s.Step(`^I call mountValidateBlockVolCapabilities$`, f.iCallMountValidateVolCapabilities)
	s.Step(`^I call blockValidateMountVolCapabilities$`, f.iCallBlockValidateVolCapabilities)
	s.Step(`^I call UnmountAndDeleteTarget$`, f.iCallUnmountAndDeleteTarget)
	s.Step(`^get Node Publish Volume Request$`, f.getNodePublishVolumeRequest)
	s.Step(`^get Node Publish Volume Request NFS$`, f.getNodePublishVolumeRequestNFS)
	s.Step(`^get Node Publish Ephemeral Volume Request with name "([^"]*)" size "([^"]*)" storagepool "([^"]*)" and systemName "([^"]*)"$`, f.getNodeEphemeralVolumePublishRequest)
	s.Step(`^I mark request read only$`, f.iMarkRequestReadOnly)
	s.Step(`^I call NodeUnpublishVolume "([^"]*)"$`, f.iCallNodeUnpublishVolume)
	s.Step(`^there are no remaining mounts$`, f.thereAreNoRemainingMounts)
	s.Step(`^I call BeforeServe$`, f.iCallBeforeServe)
	s.Step(`^I call NodeStageVolume$`, f.iCallNodeStageVolume)
	s.Step(`^I call NodeUnstageVolume with "([^"]*)"$`, f.iCallNodeUnstageVolumeWith)
	s.Step(`^I call NodeGetCapabilities "([^"]*)"$`, f.iCallNodeGetCapabilities)
	s.Step(`^a valid NodeGetCapabilitiesResponse is returned$`, f.aValidNodeGetCapabilitiesResponseIsReturned)
	s.Step(`^I call CreateSnapshot "([^"]*)"$`, f.iCallCreateSnapshot)
	s.Step(`^I call CreateSnapshot NFS "([^"]*)"$`, f.iCallCreateSnapshotNFS)
	s.Step(`^a valid CreateSnapshotResponse is returned$`, f.aValidCreateSnapshotResponseIsReturned)
	s.Step(`^a valid snapshot$`, f.aValidSnapshot)
	s.Step(`^I call DeleteSnapshot$`, f.iCallDeleteSnapshot)
	s.Step(`^I call DeleteSnapshot NFS$`, f.iCallDeleteSnapshotNFS)
	s.Step(`^a valid snapshot consistency group$`, f.aValidSnapshotConsistencyGroup)
	s.Step(`^I call Create Volume from Snapshot$`, f.iCallCreateVolumeFromSnapshot)
	s.Step(`^I call Create Volume from SnapshotNFS$`, f.iCallCreateVolumeFromSnapshotNFS)
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
	s.Step(`^I call ControllerExpandVolume set to "([^"]*)"$`, f.iCallControllerExpandVolume)
	s.Step(`^I call NodeExpandVolume with volumePath as "([^"]*)"$`, f.iCallNodeExpandVolume)
	s.Step(`^I call NodeGetVolumeStats$`, f.iCallNodeGetVolumeStats)
	s.Step(`^a correct NodeGetVolumeStats Response is returned$`, f.aCorrectNodeGetVolumeStatsResponse)
	s.Step(`^I give request volume context$`, f.iGiveRequestVolumeContext)
	s.Step(`^I call GetDevice "([^"]*)"$`, f.iCallGetDevice)
	s.Step(`^I call NewService$`, f.iCallNewService)
	s.Step(`^a new service is returned$`, f.aNewServiceIsReturned)
	s.Step(`^I call getVolProvisionType with bad params$`, f.iCallGetVolProvisionTypeWithBadParams)
	s.Step(`^i Call getStoragePoolnameByID "([^"]*)"$`, f.iCallGetStoragePoolnameByID)
	s.Step(`^I call evalsymlink "([^"]*)"$`, f.iCallEvalsymlink)
	s.Step(`^I call handlePrivFSMount$`, f.iCallHandlePrivFSMount)
	s.Step(`^I Call nodeGetAllSystems$`, f.iCallNodeGetAllSystems)
	s.Step(`^I do not have a gateway connection$`, f.iDoNotHaveAGatewayConnection)
	s.Step(`^I do not have a valid gateway endpoint$`, f.iDoNotHaveAValidGatewayEndpoint)
	s.Step(`^I do not have a valid gateway password$`, f.iDoNotHaveAValidGatewayPassword)
	s.Step(`^I call Clone volume$`, f.iCallCloneVolume)
	s.Step(`^I call CreateVolumeSnapshotGroup$`, f.iCallCreateVolumeGroupSnapshot)
	s.Step(`^the ValidateConnectivity response message contains "([^"]*)"$`, f.theValidateConnectivityResponseMessageContains)
	s.Step(`^I create false ephemeral ID$`, f.iCreateFalseEphemeralID)
	s.Step(`^I call EphemeralNodeUnpublish$`, f.iCallEphemeralNodeUnpublish)
	s.Step(`^I call EphemeralNodePublish$`, f.iCallEphemeralNodePublish)
	s.Step(`^I call getVolumeIDFromCsiVolumeID "([^"]*)"$`, f.iCallGetVolumeIDFromCsiVolumeID)
	s.Step(`^I call getFilesystemIDFromCsiVolumeID "([^"]*)"$`, f.iCallGetFileSystemIDFromCsiVolumeID)
	s.Step(`^I call getSystemIDFromCsiVolumeID "([^"]*)"$`, f.iCallGetSystemIDFromCsiVolumeID)
	s.Step(`^I call getSystemIDFromCsiVolumeIDNfs "([^"]*)"$`, f.iCallGetSystemIDFromCsiVolumeIDNfs)
	s.Step(`^I call GetSystemIDFromParameters with bad params "([^"]*)"$`, f.iCallGetSystemIDFromParameters)
	s.Step(`^I call getSystemName$`, f.iCallGetSystemName)
	s.Step(`^I call mount publishVolume$`, f.iCallMountPublishVolume)
	s.Step(`^I call mount unpublishVolume$`, f.iCallMountUnpublishVolume)
	s.Step(`^I call getSystemNameError$`, f.iCallGetSystemNameError)
	s.Step(`^I call getSystemNameMatchingError$`, f.iCallGetSystemNameMatchingError)
	s.Step(`^an invalid config "([^"]*)"$`, f.anInvalidConfig)
	s.Step(`^I call getArrayConfig$`, f.iCallGetArrayConfig)
	s.Step(`^a controller published ephemeral volume$`, f.aControllerPublishedEphemeralVolume)
	s.Step(`^I call UpdateVolumePrefixToSystemsMap "([^"]*)"$`, f.iCallupdateVolumesMap)
	s.Step(`^I call checkVolumesMap "([^"]*)"$`, f.iCallcheckVolumesMap)
	s.Step(`^two identical volumes on two different systems$`, f.twoIdenticalVolumesOnTwoDifferentSystems)
	s.Step(`^I call getMappedVols with volID "([^"]*)" and sysID "([^"]*)"$`, f.iCallgetMappedVolsWithVolIDAndSysID)
	s.Step(`the volume "([^"]*)" is from the correct system "([^"]*)"$`, f.theVolumeIsFromTheCorrectSystem)
	s.Step(`^a valid CreateVolumeSnapshotGroup response is returned$`, f.aValidCreateVolumeSnapshotGroupResponse)
	s.Step(`^I call CheckCreationTime$`, f.iCallCheckCreationTime)
	s.Step(`^I call ControllerGetVolume$`, f.iCallControllerGetVolume)
	s.Step(`^a valid ControllerGetVolumeResponse is returned$`, f.aValidControllerGetVolumeResponseIsReturned)
	s.Step(`^remove a volume from VolumeGroupSnapshotRequest$`, f.iRemoveAVolumeFromVolumeGroupSnapshotRequest)
	s.Step(`^I call DynamicLogChange "([^"]*)"$`, f.iCallDynamicLogChange)
	s.Step(`^a valid DynamicLogChange occurs "([^"]*)" "([^"]*)"$`, f.aValidDynamicLogChange)
	s.Step(`^I call getProtectionDomainIDFromName "([^"]*)" "([^"]*)"$`, f.iCallgetProtectionDomainIDFromName)
	s.Step(`^I call getArrayInstallationID "([^"]*)"$`, f.iCallgetArrayInstallationID)
	s.Step(`^I call setQoSParameters with systemID "([^"]*)" sdcID "([^"]*)" bandwidthLimit "([^"]*)" iopsLimit "([^"]*)" volumeName "([^"]*)" csiVolID "([^"]*)" nodeID "([^"]*)"$`, f.iCallSetQoSParameters)
	s.Step(`^I use config "([^"]*)"$`, f.iUseConfig)
	s.Step(`^I call GetReplicationCapabilities$`, f.iCallGetReplicationCapabilities)
	s.Step(`^a "([^"]*)" replication capabilities structure is returned$`, f.aReplicationCapabilitiesStructureIsReturned)
	s.Step(`^I set renameSDC with renameEnabled "([^"]*)" prefix "([^"]*)"$`, f.iSetRenameSdcEnabledWithPrefix)
	s.Step(`^I set approveSDC with approveSDCEnabled "([^"]*)"`, f.iSetApproveSdcEnabled)
	s.Step(`^I call CreateRemoteVolume$`, f.iCallCreateRemoteVolume)
	s.Step(`^I call DeleteLocalVolume "([^"]*)"$`, f.iCallDeleteLocalVolume)
	s.Step(`^I call CreateStorageProtectionGroup$`, f.iCallCreateStorageProtectionGroup)
	s.Step(`^I call CreateStorageProtectionGroup with "([^"]*)", "([^"]*)", "([^"]*)"$`, f.iCallCreateStorageProtectionGroupWith)
	s.Step(`^I call GetStorageProtectionGroupStatus$`, f.iCallGetStorageProtectionGroupStatus)
	s.Step(`^I call GetStorageProtectionGroupStatus with state "([^"]*)" and mode "([^"]*)"$`, f.iCallGetStorageProtectionGroupStatusWithStateAndMode)
	s.Step(`^I call DeleteVolume "([^"]*)"$`, f.iCallDeleteVolume)
	s.Step(`^I call DeleteStorageProtectionGroup$`, f.iCallDeleteStorageProtectionGroup)
	s.Step(`^I call ExecuteAction "([^"]*)"$`, f.iCallExecuteAction)
	s.Step(`^I enable quota for filesystem$`, f.iCallEnableFSQuota)
	s.Step(`^I disable quota for filesystem$`, f.iCallDisableFSQuota)
	s.Step(`^I set quota with path "([^"]*)" softLimit "([^"]*)" graceperiod "([^"]*)"$`, f.iCallSetQuotaParams)
	s.Step(`^I specify NoPath$`, f.iSpecifyNoPath)
	s.Step(`^I specify NoSoftLimit`, f.iSpecifyNoSoftLimit)
	s.Step(`^I specify NoGracePeriod`, f.iSpecifyNoGracePeriod)
	s.Step(`^I call ParseCIDR with ip "([^"]*)"`, f.iCallParseCIDR)
	s.Step(`^I call GetIPListWithMaskFromString with ip "([^"]*)"`, f.iCallGetIPListWithMaskFromString)
	s.Step(`^I call parseMask with ip "([^"]*)"`, f.iCallparseMask)
	s.Step(`^I call externalAccessAlreadyAdded with externalAccess "([^"]*)"`, f.iCallexternalAccessAlreadyAdded)
	s.Step(`^an NFSExport instance with nfsexporthost "([^"]*)"`, f.iCallGivenNFSExport)

	s.After(func(ctx context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		if f.server != nil {
			f.server.Close()
		}
		return ctx, nil
	})
}
