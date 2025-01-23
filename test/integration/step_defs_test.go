// Copyright © 2019-2025 Dell Inc. or its subsidiaries. All Rights Reserved.
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

//go:build integration
// +build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dell/csi-vxflexos/v2/k8sutils"

	"github.com/dell/csi-vxflexos/v2/service"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	apiv1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/cucumber/godog"
	csiext "github.com/dell/dell-csi-extensions/podmon"
	"github.com/dell/goscaleio"

	volGroupSnap "github.com/dell/dell-csi-extensions/volumeGroupSnapshot"
)

const (
	MaxRetries      = 10
	RetrySleepTime  = 10 * time.Second
	SleepTime       = 100 * time.Millisecond
	NodeName        = "node1"
	DriverConfigMap = "vxflexos-config-params"
	DriverNamespace = "vxflexos"
)

// ArrayConnectionData contains data required to connect to array
type ArrayConnectionData struct {
	SystemID       string `json:"systemID"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	Endpoint       string `json:"endpoint"`
	Insecure       bool   `json:"insecure,omitempty"`
	IsDefault      bool   `json:"isDefault,omitempty"`
	AllSystemNames string `json:"allSystemNames"`
	NasName        string `json:"nasname"`
}

type feature struct {
	errs                        []error
	anotherSystemID             string
	createVolumeRequest         *csi.CreateVolumeRequest
	publishVolumeRequest        *csi.ControllerPublishVolumeRequest
	nodePublishVolumeRequest    *csi.NodePublishVolumeRequest
	nodeGetInfoRequest          *csi.NodeGetInfoRequest
	nodeGetInfoResponse         *csi.NodeGetInfoResponse
	listVolumesResponse         *csi.ListVolumesResponse
	listSnapshotsResponse       *csi.ListSnapshotsResponse
	capability                  *csi.VolumeCapability
	capabilities                []*csi.VolumeCapability
	volID                       string
	snapshotID                  string
	volIDList                   []string
	volIDListShort              []string
	maxRetryCount               int
	expandVolumeResponse        *csi.ControllerExpandVolumeResponse
	nodeExpandVolumeResponse    *csi.NodeExpandVolumeResponse
	controllerGetVolumeResponse *csi.ControllerGetVolumeResponse
	nodeGetVolumeStatsResponse  *csi.NodeGetVolumeStatsResponse
	arrays                      map[string]*ArrayConnectionData
	VolumeGroupSnapshot         *volGroupSnap.CreateVolumeGroupSnapshotResponse
	VolumeGroupSnapshot2        *volGroupSnap.CreateVolumeGroupSnapshotResponse
}

func appendUnique(idList []string, newId string) (newList []string, added bool) {
	for _, id := range idList {
		if id == newId {
			return idList, false
		}
	}
	return append(idList, newId), true
}

func (f *feature) getGoscaleioClient() (client *goscaleio.Client, err error) {
	fmt.Println("f.arrays,len", f.arrays, f.arrays)

	if f.arrays == nil {
		fmt.Printf("Initialize ArrayConfig from %s:\n", configFile)
		var err error
		f.arrays, err = f.getArrayConfig(configFile)
		if err != nil {
			return nil, errors.New("Get multi array config failed " + err.Error())
		}
	}

	for _, a := range f.arrays {
		client, err := goscaleio.NewClientWithArgs(a.Endpoint, "", math.MaxInt64, true, false)
		if err != nil {
			log.Fatalf("err getting client: %v", err)
		}
		_, err = client.Authenticate(&goscaleio.ConfigConnect{
			Username: a.Username,
			Password: a.Password,
			Endpoint: a.Endpoint,
		})
		if err != nil {
			log.Fatalf("error authenticating: %v", err)
		}
		systems, err := client.GetSystems()
		if err != nil {
			log.Fatal(err)
		}
		system := systems[0]
		fmt.Println("systemid:", system.ID)

		return client, nil
	}
	return nil, err
}

// there is no way to call service.go methods from here
// hence copy same method over there , this is used to get all arrays and pick different
// systemID to test with see  method iSetAnotherSystemID
func (f *feature) getArrayConfig(filePath string) (map[string]*ArrayConnectionData, error) {
	arrays := make(map[string]*ArrayConnectionData)

	_, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("File %s does not exist", filePath)
		}
	}

	config, err := os.ReadFile(filepath.Clean(filePath))
	if err != nil {
		return nil, fmt.Errorf("File %s errors: %v", filePath, err)
	}

	if string(config) != "" {
		jsonCreds := make([]ArrayConnectionData, 0)
		err := json.Unmarshal(config, &jsonCreds)
		if err != nil {
			return nil, fmt.Errorf("Unable to parse the credentials: %v", err)
		}

		if len(jsonCreds) == 0 {
			return nil, fmt.Errorf("no arrays are provided in configFile %s", filePath)
		}

		noOfDefaultArray := 0
		for i, c := range jsonCreds {
			systemID := c.SystemID
			if _, ok := arrays[systemID]; ok {
				return nil, fmt.Errorf("duplicate system ID %s found at index %d", systemID, i)
			}
			if systemID == "" {
				return nil, fmt.Errorf("invalid value for system name at index %d", i)
			}
			if c.Username == "" {
				return nil, fmt.Errorf("invalid value for Username at index %d", i)
			}
			if c.Password == "" {
				return nil, fmt.Errorf("invalid value for Password at index %d", i)
			}
			if c.Endpoint == "" {
				return nil, fmt.Errorf("invalid value for Endpoint at index %d", i)
			}
			// ArrayConnectionData
			if c.AllSystemNames != "" {
				names := strings.Split(c.AllSystemNames, ",")
				fmt.Printf("For systemID %s configured System Names found %#v ", systemID, names)
			}

			// for PowerFlex v4.0
			if strings.TrimSpace(c.NasName) == "" {
				c.NasName = ""
			}

			fields := map[string]interface{}{
				"endpoint":       c.Endpoint,
				"user":           c.Username,
				"password":       "********",
				"insecure":       c.Insecure,
				"isDefault":      c.IsDefault,
				"systemID":       c.SystemID,
				"allSystemNames": c.AllSystemNames,
				"nasName":        c.NasName,
			}

			fmt.Printf("array found  %s %#v\n", c.SystemID, fields)

			if c.IsDefault {
				noOfDefaultArray++
			}

			if noOfDefaultArray > 1 {
				return nil, fmt.Errorf("'isDefault' parameter presents more than once in storage array list")
			}

			// copy in the arrayConnectionData to arrays
			copyOfCred := ArrayConnectionData{}
			copyOfCred = c
			arrays[c.SystemID] = &copyOfCred
		}
	} else {
		return nil, fmt.Errorf("arrays details are not provided in configFile %s", filePath)
	}
	return arrays, nil
}

func (f *feature) createConfigMap() error {
	var configYAMLContent strings.Builder

	for _, iface := range strings.Split(os.Getenv("NODE_INTERFACES"), ",") {

		interfaceData := strings.Split(strings.TrimSpace(iface), ":")
		interfaceIP, err := f.getIPAddressByInterface(interfaceData[1])
		if err != nil {
			fmt.Printf("Error while getting IP address for interface %s: %v\n", interfaceData[1], err)
			continue
		}
		configYAMLContent.WriteString(fmt.Sprintf(" %s: %s\n", interfaceData[0], interfaceIP))
	}

	configMapData := map[string]string{
		"driver-config-params.yaml": fmt.Sprintf(`interfaceNames:
%s`, configYAMLContent.String()),
	}

	configMap := &apiv1.ConfigMap{
		ObjectMeta: v1.ObjectMeta{
			Name:      DriverConfigMap,
			Namespace: DriverNamespace,
		},
		Data: configMapData,
	}

	_, err := service.K8sClientset.CoreV1().ConfigMaps(DriverNamespace).Create(context.TODO(), configMap, v1.CreateOptions{})
	if err != nil {
		fmt.Printf("Failed to create configMap: %v\n", err)
		return err
	}
	return nil
}

func (f *feature) setFakeNode() (*apiv1.Node, error) {
	fakeNode := &apiv1.Node{
		ObjectMeta: v1.ObjectMeta{
			Name:   NodeName,
			Labels: map[string]string{"label1": "value1", "label2": "value2"},
			UID:    "1aa4c285-d41b-4911-bf3e-621253bfbade",
		},
	}
	return service.K8sClientset.CoreV1().Nodes().Create(context.TODO(), fakeNode, v1.CreateOptions{})
}

func (f *feature) GetNodeUID() (string, error) {
	if service.K8sClientset == nil {
		err := k8sutils.CreateKubeClientSet()
		if err != nil {
			return "", fmt.Errorf("init client failed with error: %v", err)
		}
		service.K8sClientset = k8sutils.Clientset
	}

	// access the API to fetch node object
	node, err := service.K8sClientset.CoreV1().Nodes().Get(context.TODO(), NodeName, v1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("unable to fetch the node details. Error: %v", err)
	}
	return string(node.UID), nil
}

func (f *feature) getIPAddressByInterface(interfaceName string) (string, error) {
	interfaceObj, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return "", err
	}

	addrs, err := interfaceObj.Addrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		if ipNet.IP.To4() != nil {
			return ipNet.IP.String(), nil
		}
	}
	return "", fmt.Errorf("no IPv4 address found for interface %s", interfaceName)
}

func (f *feature) addError(err error) {
	f.errs = append(f.errs, err)
}

func (f *feature) aVxFlexOSService() error {
	f.errs = make([]error, 0)
	f.createVolumeRequest = nil
	f.publishVolumeRequest = nil
	f.nodeGetInfoRequest = nil
	f.nodeGetInfoResponse = nil
	f.listVolumesResponse = nil
	f.listSnapshotsResponse = nil
	f.capability = nil
	f.volID = ""
	f.snapshotID = ""
	f.volIDList = f.volIDList[:0]
	f.maxRetryCount = MaxRetries
	f.expandVolumeResponse = nil
	f.nodeExpandVolumeResponse = nil
	f.anotherSystemID = ""
	return nil
}

func (f *feature) aBasicBlockVolumeRequest(name string, size float64) error {
	req := new(csi.CreateVolumeRequest)
	storagePool := os.Getenv("STORAGE_POOL")
	params := make(map[string]string)
	params["storagepool"] = storagePool
	params["thickprovisioning"] = "false"
	if len(f.anotherSystemID) > 0 {
		params["systemID"] = f.anotherSystemID
	}
	req.Parameters = params
	makeAUniqueName(&name)
	req.Name = name
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = int64(math.Ceil(size * 1024 * 1024 * 1024))
	req.CapacityRange = capacityRange
	capability := new(csi.VolumeCapability)
	block := new(csi.VolumeCapability_BlockVolume)
	blockType := new(csi.VolumeCapability_Block)
	blockType.Block = block
	capability.AccessType = blockType
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	capability.AccessMode = accessMode
	f.capability = capability
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities
	f.createVolumeRequest = req
	return nil
}

func (f *feature) aBasicNfsVolumeRequest(name string, size int64) error {
	return f.nfsVolumeRequest(name, size, false, "/nfs-quota1", "20", "86400")
}

func (f *feature) aNfsVolumeRequestWithQuota(volname string, volsize int64, path string, softlimit string, graceperiod string) error {
	return f.nfsVolumeRequest(volname, volsize, true, path, softlimit, graceperiod)
}

func (f *feature) nfsVolumeRequest(volname string, volsize int64, withQuota bool, path string, softlimit string, graceperiod string) error {
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)

	ctx := context.Background()
	nfsPool := os.Getenv("NFS_STORAGE_POOL")

	if f.arrays == nil {
		fmt.Printf("Initialize ArrayConfig from %s\n", configFile)
		var err error
		f.arrays, err = f.getArrayConfig(configFile)
		if err != nil {
			return fmt.Errorf("get multi array config failed: %v", err)
		}
	}

	for _, a := range f.arrays {
		systemid := a.SystemID
		val, err := f.checkNFS(ctx, systemid)
		if err != nil {
			return err
		}

		if val {
			if a.NasName != "" {
				params["nasName"] = a.NasName
			}
			params["storagepool"] = nfsPool
			params["thickprovisioning"] = "false"
			if withQuota || os.Getenv("X_CSI_QUOTA_ENABLED") == "true" {
				params["isQuotaEnabled"] = "true"
				params["softLimit"] = softlimit
				params["path"] = path
				params["gracePeriod"] = graceperiod
			}
			if len(f.anotherSystemID) > 0 {
				params["systemID"] = f.anotherSystemID
			}
			req.Parameters = params
			makeAUniqueName(&volname)
			req.Name = volname
			capacityRange := new(csi.CapacityRange)
			capacityRange.RequiredBytes = volsize * 1024 * 1024 * 1024
			req.CapacityRange = capacityRange
			capability := new(csi.VolumeCapability)
			mount := new(csi.VolumeCapability_MountVolume)
			mount.FsType = "nfs"
			mountType := new(csi.VolumeCapability_Mount)
			mountType.Mount = mount
			capability.AccessType = mountType
			accessMode := new(csi.VolumeCapability_AccessMode)
			accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
			capability.AccessMode = accessMode
			f.capability = capability
			capabilities := make([]*csi.VolumeCapability, 0)
			capabilities = append(capabilities, capability)
			req.VolumeCapabilities = capabilities
			f.createVolumeRequest = req
			return nil
		}
		fmt.Printf("Array with SystemId %s does not support NFS. Skipping this step", systemid)
		return nil
	}
	return nil
}

func (f *feature) accessTypeIs(arg1 string) error {
	switch arg1 {
	case "multi-writer":
		f.createVolumeRequest.VolumeCapabilities[0].AccessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
	}
	return nil
}

func (f *feature) iCallCreateVolume() error {
	if f.createVolumeRequest == nil {
		return nil
	}
	volResp, err := f.createVolume(f.createVolumeRequest)
	if err != nil {
		fmt.Printf("CreateVolume %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("CreateVolume %s (%s) %s\n", volResp.GetVolume().VolumeContext["Name"],
			volResp.GetVolume().VolumeId, volResp.GetVolume().VolumeContext["CreationTime"])
		f.volID = volResp.GetVolume().VolumeId
		f.volIDList, _ = appendUnique(f.volIDList, volResp.GetVolume().VolumeId)
	}

	return nil
}

func (f *feature) createVolume(req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	var volResp *csi.CreateVolumeResponse
	var err error
	// Retry loop to deal with VxFlexOS API being overwhelmed
	for i := 0; i < f.maxRetryCount; i++ {
		volResp, err = client.CreateVolume(ctx, req)
		if err == nil || !strings.Contains(err.Error(), "Insufficient resources") {
			// no need for retry
			break
		}
		fmt.Printf("retry: %s\n", err.Error())
		time.Sleep(RetrySleepTime)
	}
	return volResp, err
}

func (f *feature) iCallDeleteVolume() error {
	if f.createVolumeRequest == nil {
		return nil
	}
	err := f.deleteVolume(f.volID)
	if err != nil {
		fmt.Printf("DeleteVolume %s failed: %v\n", f.volID, err)
		f.addError(err)
	} else {
		fmt.Printf("DeleteVolume %s completed successfully\n", f.volID)
	}
	return nil
}

func (f *feature) deleteVolume(id string) error {
	//if f.createVolumeRequest == nil {
	//	return nil
	//}
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	delVolReq := new(csi.DeleteVolumeRequest)
	delVolReq.VolumeId = id
	var err error
	// Retry loop to deal with VxFlexOS API being overwhelmed
	for i := 0; i < f.maxRetryCount; i++ {
		_, err = client.DeleteVolume(ctx, delVolReq)
		// "Insufficient resources" and "Aborted" API errors may be temporary, so we want to re-try
		if err == nil || (!strings.Contains(err.Error(), "Insufficient resources") && !strings.Contains(err.Error(), "Aborted")) {
			// no need for retry
			break
		}
		fmt.Printf("Will retry DeleteVolume due to error: %v\n", err)
		time.Sleep(RetrySleepTime)
	}
	return err
}

func (f *feature) thereAreNoErrors() error {
	if len(f.errs) == 0 {
		return nil
	}
	return f.errs[0]
}

func (f *feature) theErrorMessageShouldContain(expected string) error {
	// If arg1 is none, we expect no error, any error received is unexpected
	if f.createVolumeRequest == nil {
		return nil
	}
	if expected == "none" {
		if len(f.errs) == 0 {
			return nil
		}
		return fmt.Errorf("Unexpected error(s): %s", f.errs[0])
	}
	// We expect an error...
	if len(f.errs) == 0 {
		return errors.New("there were no errors but we expected: " + expected)
	}
	err0 := f.errs[0]
	if !strings.Contains(err0.Error(), expected) {
		return fmt.Errorf("Error %s does not contain the expected message: %s", err0.Error(), expected)
	}
	f.errs = nil
	return nil
}

func (f *feature) aMountVolumeRequest(name string) error {
	req := f.getMountVolumeRequest(name)
	f.createVolumeRequest = req
	return nil
}

func (f *feature) getMountVolumeRequest(name string) *csi.CreateVolumeRequest {
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)
	storagePool := os.Getenv("STORAGE_POOL")
	params["storagepool"] = storagePool
	// Use the default system, unless an alternative system is requested
	if len(f.anotherSystemID) > 0 {
		params["systemID"] = f.anotherSystemID
	}
	req.Parameters = params
	makeAUniqueName(&name)
	req.Name = name
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = 8 * 1024 * 1024 * 1024
	req.CapacityRange = capacityRange
	capability := new(csi.VolumeCapability)
	mountVolume := new(csi.VolumeCapability_MountVolume)
	mountVolume.FsType = "xfs"
	mountVolume.MountFlags = make([]string, 0)
	mount := new(csi.VolumeCapability_Mount)
	mount.Mount = mountVolume
	capability.AccessType = mount
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	capability.AccessMode = accessMode
	f.capability = capability
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities
	return req
}

func (f *feature) getControllerPublishVolumeRequest() *csi.ControllerPublishVolumeRequest {
	req := new(csi.ControllerPublishVolumeRequest)
	req.VolumeId = f.volID
	req.NodeId = os.Getenv("SDC_GUID")
	req.Readonly = false
	req.VolumeCapability = f.capability
	f.publishVolumeRequest = req
	return req
}

func (f *feature) whenICallPublishVolume(nodeIDEnvVar string) error {
	err := f.controllerPublishVolume(f.volID, nodeIDEnvVar)
	if err != nil {
		fmt.Printf("ControllerPublishVolume: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("ControllerPublishVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

func (f *feature) controllerPublishVolume(id string, nodeIDEnvVar string) error {
	req := f.getControllerPublishVolumeRequest()
	req.VolumeId = id
	req.NodeId = os.Getenv(nodeIDEnvVar)
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	_, err := client.ControllerPublishVolume(ctx, req)
	return err
}

func (f *feature) whenICallUnpublishVolume(nodeIDEnvVar string) error {
	err := f.controllerUnpublishVolume(f.publishVolumeRequest.VolumeId, nodeIDEnvVar)
	if err != nil {
		fmt.Printf("ControllerUnpublishVolume failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("ControllerUnpublishVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

func (f *feature) controllerUnpublishVolume(id string, nodeIDEnvVar string) error {
	req := new(csi.ControllerUnpublishVolumeRequest)
	req.VolumeId = id
	req.NodeId = os.Getenv(nodeIDEnvVar)
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	_, err := client.ControllerUnpublishVolume(ctx, req)
	return err
}

func (f *feature) maxRetries(arg1 int) error {
	f.maxRetryCount = arg1
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
	case "single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	case "multi-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
	case "multi-reader":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
	case "multi-node-single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER
	case "single-node-single-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER
	case "single-node-multi-writer":
		accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER
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
	storagePool := os.Getenv("STORAGE_POOL")
	params["storagepool"] = storagePool
	params["thickprovisioning"] = "true"
	if len(f.anotherSystemID) > 0 {
		params["systemID"] = f.anotherSystemID
	}
	// use new system name instead of previous name, only set if name has substring alt_system_id
	newName := os.Getenv("ALT_SYSTEM_ID")
	if len(newName) > 0 && strings.Contains(name, "alt_system_id") {
		fmt.Printf("Using %s as systemID for volume request \n", newName)
		params["systemID"] = newName
	} else {
		fmt.Printf("Env variable ALT_SYSTEM_ID not set, assuming system does not have a name \n")
	}
	req.Parameters = params
	makeAUniqueName(&name)
	req.Name = name
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = size * 1024 * 1024 * 1024
	req.CapacityRange = capacityRange
	req.VolumeCapabilities = f.capabilities
	f.createVolumeRequest = req
	return nil
}

func (f *feature) getNodePublishVolumeRequest() *csi.NodePublishVolumeRequest {
	req := new(csi.NodePublishVolumeRequest)
	req.VolumeId = f.volID
	req.Readonly = false

	if f.capability.AccessMode.Mode == csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY {
		req.Readonly = true
	}

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
	return req
}

//nolint:revive
func (f *feature) whenICallNodePublishVolumeWithPoint(arg1 string, arg2 string, mkfsFormatOption string) error {
	block := f.capability.GetBlock()
	if block == nil {
		_, err := os.Stat(arg2)
		if err != nil && os.IsNotExist(err) {
			err = os.Mkdir(arg2, 0o777)
			if err != nil {
				return err
			}

		}
	}
	err := f.nodePublishVolume(f.volID, arg2, mkfsFormatOption)
	if err != nil {
		fmt.Printf("NodePublishVolume failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("NodePublishVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

//nolint:revive
func (f *feature) whenICallNodePublishVolume(arg1 string) error {
	err := f.nodePublishVolume(f.volID, "", "")
	if err != nil {
		fmt.Printf("NodePublishVolume failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("NodePublishVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

func (f *feature) iCallEphemeralNodePublishVolume(id, size string) error {
	req := f.getNodePublishVolumeRequest()
	req.VolumeId = id
	f.volID = req.VolumeId
	req.VolumeContext = map[string]string{"csi.storage.k8s.io/ephemeral": "true", "volumeName": "int-ephemeral-vol", "size": size, "storagepool": "pool1"}

	ctx := context.Background()
	client := csi.NewNodeClient(grpcClient)
	_, err := client.NodePublishVolume(ctx, req)
	if err != nil {
		f.addError(err)
	}
	return nil
}

func (f *feature) nodePublishVolume(id string, path string, mkfsFormatOption string) error {
	req := f.getNodePublishVolumeRequest()
	if path != "" {
		block := f.capability.GetBlock()
		if block != nil {
			req.TargetPath = path
		}
		mount := f.capability.GetMount()
		if mount != nil {
			req.TargetPath = path
		}
	}
	req.VolumeId = id
	if len(mkfsFormatOption) > 0 {
		req.VolumeContext = map[string]string{"mkfsFormatOption": mkfsFormatOption}
	}
	ctx := context.Background()
	client := csi.NewNodeClient(grpcClient)
	_, err := client.NodePublishVolume(ctx, req)
	return err
}

//nolint:revive
func (f *feature) whenICallNodeUnpublishVolume(arg1 string) error {
	err := f.nodeUnpublishVolume(f.volID, f.nodePublishVolumeRequest.TargetPath)
	if err != nil {
		fmt.Printf("NodeUnpublishVolume failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("NodeUnpublishVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

//nolint:revive
func (f *feature) whenICallNodeUnpublishVolumeWithPoint(arg1, arg2 string) error {
	err := f.nodeUnpublishVolume(f.volID, arg2)
	if err != nil {
		fmt.Printf("NodeUnpublishVolume failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("NodeUnpublishVolume completed successfully\n")
	}
	time.Sleep(SleepTime)

	err = syscall.Unmount(arg2, 0)
	err = os.RemoveAll(arg2)
	return nil
}

func (f *feature) nodeUnpublishVolume(id string, path string) error {
	req := &csi.NodeUnpublishVolumeRequest{VolumeId: id, TargetPath: path}
	ctx := context.Background()
	client := csi.NewNodeClient(grpcClient)
	_, err := client.NodeUnpublishVolume(ctx, req)
	return err
}

//nolint:revive
func (f *feature) verifyPublishedVolumeWithVoltypeAccessFstype(voltype, access, fstype string) error {
	if len(f.errs) > 0 {
		fmt.Printf("Not verifying published volume because of previous error\n")
		return nil
	}
	var cmd *exec.Cmd
	if voltype == "mount" {
		cmd = exec.Command("/bin/sh", "-c", "mount | grep /tmp/datadir")
	} else if voltype == "block" {
		cmd = exec.Command("/bin/sh", "-c", "mount | grep /tmp/datafile")
	} else {
		return errors.New("unepected volume type")
	}
	stdout, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", stdout)
	if voltype == "mount" {
		// output: /dev/scinia on /tmp/datadir type xfs (rw,relatime,seclabel,attr2,inode64,noquota)
		if !strings.Contains(string(stdout), "/dev/scini") {
			return errors.New("Mount did not contain /dev/scini for scale-io")
		}
		if !strings.Contains(string(stdout), "/tmp/datadir") {
			return errors.New("Mount did not contain /tmp/datadir for type mount")
		}
		if !strings.Contains(string(stdout), fmt.Sprintf("type %s", fstype)) {
			return fmt.Errorf("Did not find expected fstype %s", fstype)
		}

	} else if voltype == "block" {
		// devtmpfs on /tmp/datafile type devtmpfs (rw,relatime,seclabel,size=8118448k,nr_inodes=2029612,mode=755)
		if !strings.Contains(string(stdout), "devtmpfs on /tmp/datafile") {
			return errors.New("Expected devtmpfs on /tmp/datafile for mounted block device")
		}
	}
	return nil
}

func (f *feature) iCallCreateSnapshot() error {
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	req := &csi.CreateSnapshotRequest{
		SourceVolumeId: f.volID,
		Name:           "snapshot-0eb5347a-0000-11e9-ab1c-005056a64ad3",
	}
	resp, err := client.CreateSnapshot(ctx, req)
	if err != nil {
		fmt.Printf("CreateSnapshot returned error: %s\n", err.Error())
		f.addError(err)
	} else {
		f.snapshotID = resp.Snapshot.SnapshotId
		fmt.Printf("createSnapshot: SnapshotId %s SourceVolumeId %s CreationTime %s\n",
			resp.Snapshot.SnapshotId, resp.Snapshot.SourceVolumeId, resp.Snapshot.CreationTime.AsTime().Format(time.RFC3339Nano))
	}
	time.Sleep(RetrySleepTime)
	return nil
}

func (f *feature) iCallDeleteSnapshot() error {
	//fmt.Printf("=== AB: sleeping 2 minutes before deleting snapshot %s\n", f.snapshotID)
	//time.Sleep(time.Minute * 3)

	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	req := &csi.DeleteSnapshotRequest{
		SnapshotId: f.snapshotID,
	}
	_, err := client.DeleteSnapshot(ctx, req)
	if err != nil {
		fmt.Printf("DeleteSnapshot returned error: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("DeleteSnapshot: SnapshotId %s\n", req.SnapshotId)
	}

	//fmt.Printf("=== AB: sleeping 3 minutes after deleting snapshot\n")
	//time.Sleep(time.Minute * 1)

	time.Sleep(RetrySleepTime)
	return nil
}

func (f *feature) iCallCreateSnapshotConsistencyGroup() error {
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	var volumeIDList string
	for i, v := range f.volIDList {
		switch i {
		case 0:
			continue
		case 1:
			volumeIDList = v
		default:
			volumeIDList = volumeIDList + "," + v
		}
	}
	req := &csi.CreateSnapshotRequest{
		SourceVolumeId: f.volIDList[0],
		Name:           "snaptest",
	}
	req.Parameters = make(map[string]string)
	req.Parameters["VolumeIDList"] = volumeIDList
	resp, err := client.CreateSnapshot(ctx, req)
	if err != nil {
		fmt.Printf("CreateSnapshot returned error: %s\n", err.Error())
		f.addError(err)
	} else {
		f.snapshotID = resp.Snapshot.SnapshotId
		fmt.Printf("createSnapshot: SnapshotId %s SourceVolumeId %s CreationTime %s\n",
			resp.Snapshot.SnapshotId, resp.Snapshot.SourceVolumeId, resp.Snapshot.CreationTime.AsTime().Format(time.RFC3339Nano))
	}
	time.Sleep(SleepTime)
	return nil
}

func (f *feature) iCallDeleteAllVolumes() error {
	for _, v := range f.volIDList {
		f.volID = v
		f.iCallDeleteVolume()
	}
	return nil
}

func (f *feature) iCallCreateVolumeFromSnapshot() error {
	req := f.createVolumeRequest
	req.Name = "volFromSnap-" + req.Name
	source := &csi.VolumeContentSource_SnapshotSource{SnapshotId: f.snapshotID}
	req.VolumeContentSource = new(csi.VolumeContentSource)
	req.VolumeContentSource.Type = &csi.VolumeContentSource_Snapshot{Snapshot: source}
	fmt.Printf("Calling CreateVolume %s with snapshot %s as source\n", req.Name, f.snapshotID)
	_ = f.createAVolume(req, "single CreateVolume from Snap")
	time.Sleep(SleepTime)
	return nil
}

func (f *feature) iCallCloneVolume() error {
	req := f.createVolumeRequest
	req.Name = "cloneVol-" + req.Name
	source := &csi.VolumeContentSource_VolumeSource{VolumeId: f.volID}
	req.VolumeContentSource = new(csi.VolumeContentSource)
	req.VolumeContentSource.Type = &csi.VolumeContentSource_Volume{Volume: source}
	fmt.Printf("Calling CreateVolume %s with volume %s as source\n", req.Name, f.volID)
	_ = f.createAVolume(req, "single CloneVolume")
	time.Sleep(SleepTime)
	return nil
}

func (f *feature) createAVolume(req *csi.CreateVolumeRequest, voltype string) error {
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	volResp, err := client.CreateVolume(ctx, req)
	if err != nil {
		fmt.Printf("CreateVolume (%s) request failed: %v\n", voltype, err)
		f.addError(err)
	} else {
		//fmt.Printf("CreateVolume succeded: %s (id: %s)\n",
		//	volResp.GetVolume().VolumeContext["Name"],
		//	volResp.GetVolume().VolumeId)
		f.volIDList, _ = appendUnique(f.volIDList, volResp.GetVolume().VolumeId)
	}
	return err
}

func (f *feature) iCallCreateManyVolumesFromSnapshot() error {
	fmt.Printf("Create volume from snapshot 130 times and expect it to fail " +
		"after the max number of volumes per snapshot is exceeded.\n")
	for i := 1; i <= 130; i++ {
		req := f.createVolumeRequest
		req.Name = fmt.Sprintf("volFromSnap%d", i)
		makeAUniqueName(&req.Name)
		source := &csi.VolumeContentSource_SnapshotSource{SnapshotId: f.snapshotID}
		req.VolumeContentSource = new(csi.VolumeContentSource)
		req.VolumeContentSource.Type = &csi.VolumeContentSource_Snapshot{Snapshot: source}
		fmt.Printf("Calling CreateVolume %s with snapshot %s as source\n", req.Name, f.snapshotID)
		err := f.createAVolume(req, "single CreateVolume from Snap")
		if err != nil {
			fmt.Printf("Got expected error on volume #%d\n", i)
			break
		}
	}
	return nil
}

func (f *feature) iCallCloneManyVolumes() error {
	fmt.Printf("Create volume by cloning 130 times and expect it to fail " +
		"after the max number of clones is exceeded.\n")
	for i := 1; i <= 130; i++ {
		req := f.createVolumeRequest
		req.Name = fmt.Sprintf("cloneVol%d", i)
		source := &csi.VolumeContentSource_VolumeSource{VolumeId: f.volID}
		req.VolumeContentSource = new(csi.VolumeContentSource)
		req.VolumeContentSource.Type = &csi.VolumeContentSource_Volume{Volume: source}
		fmt.Printf("Calling CreateVolume %s with volume %s as source\n", req.Name, f.volID)
		err := f.createAVolume(req, "single CloneVolume")
		if err != nil {
			fmt.Printf("Got expected error on volume #%d\n", i)
			break
		}
	}
	return nil
}

func (f *feature) iCallListVolume() error {
	if f.createVolumeRequest == nil {
		return nil
	}
	var err error
	ctx := context.Background()
	req := &csi.ListVolumesRequest{}
	client := csi.NewControllerClient(grpcClient)
	f.listVolumesResponse, err = client.ListVolumes(ctx, req)
	return err
}

func (f *feature) aValidListVolumeResponseIsReturned() error {
	if f.createVolumeRequest == nil {
		return nil
	}
	resp := f.listVolumesResponse
	entries := resp.GetEntries()
	if entries == nil {
		return errors.New("expected ListVolumeResponse.Entries but none")
	}
	for _, entry := range entries {
		vol := entry.GetVolume()
		if vol != nil {
			id := vol.VolumeId
			capacity := vol.CapacityBytes
			name := vol.VolumeContext["Name"]
			creation := vol.VolumeContext["CreationTime"]
			fmt.Printf("Volume ID: %s Name: %s Capacity: %d CreationTime: %s\n", id, name, capacity, creation)
		}
	}
	return nil
}

func (f *feature) iCallListSnapshotForSnap() error {
	var err error
	ctx := context.Background()
	req := &csi.ListSnapshotsRequest{SnapshotId: f.snapshotID}
	client := csi.NewControllerClient(grpcClient)
	fmt.Printf("ListSnapshots for snap id %s\n", f.snapshotID)
	f.listSnapshotsResponse, err = client.ListSnapshots(ctx, req)
	return err
}

func (f *feature) iCallListSnapshot() error {
	var err error
	ctx := context.Background()
	req := &csi.ListSnapshotsRequest{}
	client := csi.NewControllerClient(grpcClient)
	f.listSnapshotsResponse, err = client.ListSnapshots(ctx, req)
	return err
}

func (f *feature) expectErrorListSnapshotResponse() error {
	err := f.aValidListSnapshotResponseIsReturned()
	expected := "ListSnapshots does not contain snap id"
	// expect does not contain snap id
	if !strings.Contains(err.Error(), expected) {
		return fmt.Errorf("Error %s does not contain the expected message: %s", err.Error(), expected)
	}

	fmt.Printf("got expected error: %s", err.Error())

	return nil
}

func (f *feature) aValidListSnapshotResponseIsReturned() error {
	nextToken := f.listSnapshotsResponse.GetNextToken()
	if nextToken != "" {
		return errors.New("received NextToken on ListSnapshots but didn't expect one")
	}
	fmt.Printf("Looking for snap id %s\n", f.snapshotID)
	entries := f.listSnapshotsResponse.GetEntries()
	var foundSnapshot bool
	for j := 0; j < len(entries); j++ {
		entry := entries[j]
		id := entry.GetSnapshot().SnapshotId
		//ts := entry.GetSnapshot().CreationTime.AsTime().Format(time.RFC3339Nano)
		//fmt.Printf("snapshot ID %s source ID %s timestamp %s\n", id, entry.GetSnapshot().SourceVolumeId, ts)
		if f.snapshotID != "" && strings.Contains(id, f.snapshotID) {
			foundSnapshot = true
		}
	}
	if f.snapshotID != "" && !foundSnapshot {
		msg := "ListSnapshots does not contain snap id " + f.snapshotID
		fmt.Print(msg)
		return errors.New(msg)
	}
	return nil
}

func (f *feature) iSetAnotherSystemName(systemType string) error {
	if f.arrays == nil {
		fmt.Printf("Initialize ArrayConfig from %s:\n", configFile)
		var err error
		f.arrays, err = f.getArrayConfig(configFile)
		if err != nil {
			return errors.New("get multi array config failed " + err.Error())
		}
	}
	isNumeric := regexp.MustCompile(`^[0-9a-f]+$`).MatchString
	for _, a := range f.arrays {
		if systemType == "altSystem" && !a.IsDefault {
			if !isNumeric(a.SystemID) {
				f.anotherSystemID = a.SystemID
				break
			}
		}
		if systemType == "defaultSystem" && a.IsDefault {
			if !isNumeric(a.SystemID) {
				f.anotherSystemID = a.SystemID
				break
			}
		}
	}
	fmt.Printf("array selected for %s is %s\n", systemType, f.anotherSystemID)
	if f.anotherSystemID == "" {
		return errors.New("failed to get multi array config for " + systemType)
	}
	return nil
}

func (f *feature) iSetAnotherSystemID(systemType string) error {
	if f.arrays == nil {
		fmt.Printf("Initialize ArrayConfig from %s:\n", configFile)
		var err error
		f.arrays, err = f.getArrayConfig(configFile)
		if err != nil {
			return errors.New("Get multi array config failed " + err.Error())
		}
	}
	for _, a := range f.arrays {
		if systemType == "altSystem" && !a.IsDefault {
			f.anotherSystemID = a.SystemID
			break
		}
		if systemType == "defaultSystem" && a.IsDefault {
			f.anotherSystemID = a.SystemID
			break
		}
	}
	fmt.Printf("array selected for %s is %s\n", systemType, f.anotherSystemID)
	if f.anotherSystemID == "" {
		return errors.New("Failed to get  multi array config for " + systemType)
	}
	return nil
}

func (f *feature) iCreateVolumesInParallel(nVols int) error {
	fmt.Printf("Creating %d volumes in parallel...\n", nVols)

	idchan := make(chan string, nVols)
	errchan := make(chan error, nVols)
	t0 := time.Now()
	// Send requests
	for i := 0; i < nVols; i++ {
		name := fmt.Sprintf("scale%d", i)
		go func(name string, i int, idchan chan string, errchan chan error) {
			var resp *csi.CreateVolumeResponse
			var err error
			req := f.getMountVolumeRequest(name)
			if req != nil {
				if i%2 == 0 {
					req.Parameters["systemID"] = ""
				}
				resp, err = f.createVolume(req)
				if resp != nil && err == nil {
					idchan <- resp.GetVolume().VolumeId
				} else {
					idchan <- ""
				}
			}
			errchan <- err
		}(name, i, idchan, errchan)
	}
	// Wait on complete, collecting ids and errors
	nerrors := 0
	for i := 0; i < nVols; i++ {
		var id string
		var err error
		id = <-idchan
		if id != "" {
			f.volIDList, _ = appendUnique(f.volIDList, id)
		}
		err = <-errchan
		if err != nil {
			fmt.Printf("create volume received error: %s\n", err.Error())
			f.addError(err)
			nerrors++
		}
	}
	t1 := time.Now()
	if len(f.volIDList) > nVols {
		fmt.Printf("Found %d registered volumes while expected only %d, some of them will not be tested: %s\n",
			len(f.volIDList), nVols, strings.Join(f.volIDList, ","))
		//f.volIDList = f.volIDList[0:nVols]
	}
	fmt.Printf("Create volume time for %d volumes %.6fs, per volume %.6fs, errors %d\n",
		nVols, t1.Sub(t0).Seconds(), t1.Sub(t0).Seconds()/float64(nVols), nerrors)
	time.Sleep(SleepTime)
	return nil
}

func (f *feature) iPublishVolumesInParallel(nVols int) error {
	fmt.Printf("Publishing %d volumes in parallel...\n", nVols)

	nvols := len(f.volIDList)
	done := make(chan bool, nvols)
	errchan := make(chan error, nvols)

	// Send requests
	t0 := time.Now()
	for i := 0; i < nVols; i++ {
		id := f.volIDList[i]
		if id == "" {
			continue
		}
		go func(id string, done chan bool, errchan chan error) {
			err := f.controllerPublishVolume(id, "SDC_GUID")
			done <- true
			errchan <- err
		}(id, done, errchan)
	}

	// Wait for responses
	nerrors := 0
	for i := 0; i < nVols; i++ {
		if f.volIDList[i] == "" {
			continue
		}
		finished := <-done
		if !finished {
			return errors.New("premature completion")
		}
		err := <-errchan
		if err != nil {
			fmt.Printf("controller publish received error: %s\n", err.Error())
			f.addError(err)
			nerrors++
		}
	}
	t1 := time.Now()
	fmt.Printf("Controller publish volume time for %d volumes %.6fs, per volume %.6fs, errors %d\n", nVols, t1.Sub(t0).Seconds(), t1.Sub(t0).Seconds()/float64(nVols), nerrors)
	time.Sleep(4 * SleepTime)
	return nil
}

func (f *feature) iNodePublishVolumesInParallel(nVols int) error {
	fmt.Printf("Node Publishing %d volumes in parallel...\n", nVols)

	nvols := len(f.volIDList)
	// make a data directory for each
	for i := 0; i < nVols; i++ {
		dataDirName := fmt.Sprintf("/tmp/datadir%d", i)
		fmt.Printf("Checking %s\n", dataDirName)
		var fileMode os.FileMode = 0o777
		err := os.Mkdir(dataDirName, fileMode)
		if err != nil && !os.IsExist(err) {
			fmt.Printf("%s: %s\n", dataDirName, err)
		}
	}
	done := make(chan bool, nvols)
	errchan := make(chan error, nvols)

	// Send requests
	t0 := time.Now()
	for i := 0; i < nVols; i++ {
		id := f.volIDList[i]
		if id == "" {
			continue
		}
		dataDirName := fmt.Sprintf("/tmp/datadir%d", i)
		go func(id string, dataDirName string, done chan bool, errchan chan error) {
			err := f.nodePublishVolume(id, dataDirName, "")
			done <- true
			errchan <- err
		}(id, dataDirName, done, errchan)
	}

	// Wait for responses
	nerrors := 0
	for i := 0; i < nVols; i++ {
		if f.volIDList[i] == "" {
			continue
		}
		finished := <-done
		if !finished {
			return errors.New("premature completion")
		}
		err := <-errchan
		if err != nil {
			fmt.Printf("node publish received error: %s\n", err.Error())
			f.addError(err)
			nerrors++
		}
	}
	t1 := time.Now()
	fmt.Printf("Node publish volume time for %d volumes %.6fs, per volume %.6fs, errors %d\n", nVols, t1.Sub(t0).Seconds(), t1.Sub(t0).Seconds()/float64(nVols), nerrors)
	time.Sleep(2 * SleepTime)
	return nil
}

func (f *feature) iNodeUnpublishVolumesInParallel(nVols int) error {
	fmt.Printf("Node Unpublishing %d volumes in parallel...\n", nVols)

	nvols := len(f.volIDList)
	done := make(chan bool, nvols)
	errchan := make(chan error, nvols)

	// Send requests
	t0 := time.Now()
	for i := 0; i < nVols; i++ {
		id := f.volIDList[i]
		if id == "" {
			continue
		}
		dataDirName := fmt.Sprintf("/tmp/datadir%d", i)
		go func(id string, dataDirName string, done chan bool, errchan chan error) {
			err := f.nodeUnpublishVolume(id, dataDirName)
			done <- true
			errchan <- err
		}(id, dataDirName, done, errchan)
	}

	// Wait for responses
	nerrors := 0
	for i := 0; i < nVols; i++ {
		if f.volIDList[i] == "" {
			continue
		}
		finished := <-done
		if !finished {
			return errors.New("premature completion")
		}
		err := <-errchan
		if err != nil {
			fmt.Printf("node unpublish received error: %s\n", err.Error())
			f.addError(err)
			nerrors++
		}
	}
	t1 := time.Now()
	fmt.Printf("Node unpublish volume time for %d volumes %.6fs, per volume %.6fs, errors %d\n", nVols, t1.Sub(t0).Seconds(), t1.Sub(t0).Seconds()/float64(nVols), nerrors)
	time.Sleep(SleepTime)
	return nil
}

func (f *feature) iUnpublishVolumesInParallel(nVols int) error {
	fmt.Printf("Unpublishing %d volumes in parallel...\n", nVols)

	nvols := len(f.volIDList)
	done := make(chan bool, nvols)
	errchan := make(chan error, nvols)

	// Send request
	t0 := time.Now()
	for i := 0; i < nVols; i++ {
		id := f.volIDList[i]
		if id == "" {
			continue
		}
		go func(id string, done chan bool, errchan chan error) {
			err := f.controllerUnpublishVolume(id, "SDC_GUID")
			done <- true
			errchan <- err
		}(id, done, errchan)
	}

	// Wait for resonse
	nerrors := 0
	for i := 0; i < nVols; i++ {
		if f.volIDList[i] == "" {
			continue
		}
		finished := <-done
		if !finished {
			return errors.New("premature completion")
		}
		err := <-errchan
		if err != nil {
			fmt.Printf("controller unpublish received error: %s\n", err.Error())
			f.addError(err)
			nerrors++
		}
	}
	t1 := time.Now()
	fmt.Printf("Controller unpublish volume time for %d volumes %.6fs, per volume %.6fs, errors %d\n", nVols, t1.Sub(t0).Seconds(), t1.Sub(t0).Seconds()/float64(nVols), nerrors)
	time.Sleep(SleepTime)
	return nil
}

func (f *feature) whenIDeleteVolumesInParallel(nVols int) error {
	fmt.Printf("Deleting %d volumes in parallel...\n", nVols)

	if len(f.volIDList) > nVols {
		fmt.Printf("Found %d registered volumes vs %d expected\n", len(f.volIDList), nVols)
		return fmt.Errorf("unexpected number of created volumes")
	}

	done := make(chan bool, nVols)
	errchan := make(chan error, nVols)

	// Send requests
	t0 := time.Now()
	for i := 0; i < nVols; i++ {
		id := f.volIDList[i]
		if id == "" {
			continue
		}
		go func(id string, done chan bool, errchan chan error) {
			err := f.deleteVolume(id)
			done <- true
			errchan <- err
		}(f.volIDList[i], done, errchan)
	}

	// Wait on complete
	nerrors := 0
	for i := 0; i < nVols; i++ {
		var finished bool
		var err error
		name := fmt.Sprintf("scale%d", i)
		finished = <-done
		if !finished {
			return errors.New("premature completion")
		}
		err = <-errchan
		if err != nil {
			fmt.Printf("delete volume received error %s: %s\n", name, err.Error())
			f.addError(err)
			nerrors++
		}
	}
	t1 := time.Now()
	fmt.Printf("Delete volume time for %d volumes %.6fs, per volume %.6fs, errors %d\n", nVols, t1.Sub(t0).Seconds(), t1.Sub(t0).Seconds()/float64(nVols), nerrors)
	time.Sleep(RetrySleepTime)
	return nil
}

// Writes a fixed pattern of block data (0x57 bytes) in 1 MB chunks to raw block mounted at /tmp/datafile.
// Used to make sure the data has changed when taking a snapshot
func (f *feature) iWriteBlockData() error {
	buf := make([]byte, 1024*1024)
	for i := 0; i < 1024*1024; i++ {
		buf[i] = 0x57
	}
	fp, err := os.OpenFile("/tmp/datafile", os.O_RDWR, 0o666)
	if err != nil {
		return err
	}
	var nrecords int
	for err == nil {
		var n int
		n, err = fp.Write(buf)
		if n == len(buf) {
			nrecords++
		}
		if (nrecords % 256) == 0 {
			fmt.Printf("%d records\r", nrecords)
		}
	}
	fp.Close()
	fmt.Printf("\rWrote %d MB\n", nrecords)
	return nil
}

// Writes a fixed pattern of block data (0x57 bytes) in 1 MB chunks to raw block mounted at /tmp/datafile.
// Used to make sure the data has changed when taking a snapshot
func (f *feature) iReadWriteToVolume(folder string) error {
	buf := make([]byte, 1024)
	for i := 0; i < 1024; i++ {
		buf[i] = 0x57
	}
	// /tmp/podmondev1
	fmt.Printf("Read/Write block data wait..")
	err := f.iCallValidateVolumeHostConnectivity()
	if err == nil {
		fmt.Printf("Newly created Volume No IO expected \n")
	}
	// allow mount to stabilize
	time.Sleep(6 * time.Second)
	path := fmt.Sprintf("%s/%s", folder, "file")
	fp, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		files, err1 := os.ReadDir(path)
		if err1 != nil {
			fmt.Printf("Write block read dir  %s", err1.Error())
		}
		for _, file := range files {
			fmt.Println(file.Name())
		}
		fmt.Printf("Write block data %s", err.Error())
		return nil
	}
	var nrecords int
	for err == nil {
		var n int
		n, err = fp.Write(buf)
		if err != nil {
			fmt.Printf("Error write %s \n", err.Error())
		}
		if n == len(buf) {
			nrecords++
		}
		fmt.Printf("Write %d records\r", nrecords)
		if nrecords > 255 {
			break
		}
	}
	fmt.Printf("Write done %d \n", nrecords)
	fp.Close()
	// do read
	fp1, err := os.Open(path)
	buf = make([]byte, 1024)
	n, err := fp1.Read(buf)
	fmt.Printf("Read records %d  \r", n)
	if err != nil {
		fmt.Printf("Error %s \n", err.Error())
	}
	fp1.Close()
	fmt.Printf("Read done %d \n", nrecords)
	return nil
}

func (f *feature) iCallValidateVolumeHostConnectivity() error {
	ctx := context.Background()
	pclient := csiext.NewPodmonClient(grpcClient)

	sdcID := os.Getenv("SDC_GUID")
	sdcGUID := strings.ToUpper(sdcID)
	csiNodeID := sdcGUID

	volIDs := make([]string, 0)
	volIDs = append(volIDs, f.volID)

	req := &csiext.ValidateVolumeHostConnectivityRequest{
		NodeId:    csiNodeID,
		VolumeIds: volIDs,
	}
	connect, err := pclient.ValidateVolumeHostConnectivity(ctx, req)
	if err != nil {
		return fmt.Errorf("Error calling host connectivity %s", err.Error())
	}

	fmt.Printf("Volume %s IosInProgress=%t\n", f.volID, connect.IosInProgress)
	// connect = nil
	// req = nil
	// pclient = nil
	f.errs = make([]error, 0)
	if connect.IosInProgress || connect.Connected {
		return nil
	}
	err = fmt.Errorf("Unexpected error IO to volume: %t", connect.IosInProgress)
	f.addError(err)
	return nil
}

func (f *feature) iRemoveAVolumeFromVolumeGroupSnapshotRequest() error {
	// cut last volume off of list
	f.volIDList = f.volIDList[0 : len(f.volIDList)-1]
	return nil
}

func (f *feature) iCallCreateVolumeGroupSnapshot() error {
	ctx := context.Background()
	vgsClient := volGroupSnap.NewVolumeGroupSnapshotClient(grpcClient)
	params := make(map[string]string)
	if f.VolumeGroupSnapshot != nil {
		params["existingSnapshotGroupID"] = strings.Split(f.VolumeGroupSnapshot.SnapshotGroupID, "-")[1]
	}
	req := &volGroupSnap.CreateVolumeGroupSnapshotRequest{
		Name:            "apple",
		SourceVolumeIDs: f.volIDList,
		Parameters:      params,
	}
	group, err := vgsClient.CreateVolumeGroupSnapshot(ctx, req)
	if err != nil {
		f.addError(err)
	}
	fmt.Printf("Group returned is: %v \n", group)
	if group != nil {
		f.VolumeGroupSnapshot = group
	}
	return nil
}

// takes f.VolumeGroupSnapshot (assumes length >=2 ), and splits its snapshots into
// two VolumeGroupSnapshots, f.volumeGroupSnapshot and  f.volumeGroupSnapshot2
func (f *feature) iCallSplitVolumeGroupSnapshot() error {
	if f.VolumeGroupSnapshot == nil {
		fmt.Printf("No VolumeGroupSnapshot to split.\n")
		return nil
	}
	ctx := context.Background()
	vgsClient := volGroupSnap.NewVolumeGroupSnapshotClient(grpcClient)
	snapList := f.VolumeGroupSnapshot.Snapshots

	// delete first snap from VGS, and save corresponding VGS as f.volumeGroupSnapshot2
	f.VolumeGroupSnapshot.Snapshots = snapList[0:1]
	fmt.Printf("Snapshots in VGS to be deleted are: %v \n", f.VolumeGroupSnapshot.Snapshots)
	f.iCallDeleteVGS()
	f.VolumeGroupSnapshot.Snapshots = snapList[1:]
	f.VolumeGroupSnapshot2 = f.VolumeGroupSnapshot

	// adjust f.volIDList to only contain the first, unsnapped volume, and create another VGS for it. Save this one as  f.volumeGroupSnapshot
	f.volIDListShort = f.volIDList[0:1]
	req := &volGroupSnap.CreateVolumeGroupSnapshotRequest{
		Name:            "apple",
		SourceVolumeIDs: f.volIDListShort,
	}
	group, err := vgsClient.CreateVolumeGroupSnapshot(ctx, req)
	if err != nil {
		f.addError(err)
	}
	if group != nil {
		f.VolumeGroupSnapshot = group
	}

	fmt.Printf("group 1 is: %v \n", f.VolumeGroupSnapshot)
	fmt.Printf("group 2 is: %v \n", f.VolumeGroupSnapshot2)

	return nil
}

func (f *feature) iCallDeleteVGS() error {
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	if f.VolumeGroupSnapshot == nil && f.VolumeGroupSnapshot2 != nil {
		fmt.Printf("VolumeGroupSnapshot already deleted.\n")
		return nil
	}
	for _, snap := range f.VolumeGroupSnapshot.Snapshots {
		fmt.Printf("Deleting:  %v \n", snap.SnapId)
		req := &csi.DeleteSnapshotRequest{
			SnapshotId: snap.SnapId,
		}
		_, err := client.DeleteSnapshot(ctx, req)
		if err != nil {
			fmt.Printf("DeleteSnapshot returned error: %s\n", err.Error())
		}
	}

	if f.VolumeGroupSnapshot2 != nil {
		for _, snap := range f.VolumeGroupSnapshot2.Snapshots {
			fmt.Printf("Deleting:  %v \n", snap.SnapId)
			req := &csi.DeleteSnapshotRequest{
				SnapshotId: snap.SnapId,
			}
			_, err := client.DeleteSnapshot(ctx, req)
			if err != nil {
				fmt.Printf("DeleteSnapshot returned error: %s\n", err.Error())
			}
		}
	}
	return nil
}

func (f *feature) whenICallExpandVolumeTo(size int64) error {
	err := f.controllerExpandVolume(f.volID, size)
	if err != nil {
		fmt.Printf("ControllerExpandVolume %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("ControllerExpandVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

func (f *feature) controllerExpandVolume(volID string, size int64) error {
	const bytesInKiB = 1024
	var resp *csi.ControllerExpandVolumeResponse
	var err error
	req := &csi.ControllerExpandVolumeRequest{
		VolumeId:      volID,
		CapacityRange: &csi.CapacityRange{RequiredBytes: size * bytesInKiB * bytesInKiB * bytesInKiB},
	}
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	for i := 0; i < f.maxRetryCount; i++ {
		resp, err = client.ControllerExpandVolume(ctx, req)
		if err == nil {
			break
		}
		fmt.Printf("Controller ExpandVolume retry: %s\n", err.Error())
		time.Sleep(RetrySleepTime)
	}
	f.expandVolumeResponse = resp
	return err
}

func (f *feature) whenICallNodeExpandVolume() error {
	nodePublishReq := f.nodePublishVolumeRequest
	if nodePublishReq == nil {
		err := fmt.Errorf("Volume is not stage, nodePublishVolumeRequest not found")
		return err
	}
	err := f.nodeExpandVolume(f.volID, nodePublishReq.TargetPath)
	if err != nil {
		fmt.Printf("NodeExpandVolume %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("NodeExpandVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

func (f *feature) iCallNodeGetVolumeStats() error {
	ctx := context.Background()
	client := csi.NewNodeClient(grpcClient)
	var err error

	volID := f.volID
	vPath := "/tmp/datadir"

	req := &csi.NodeGetVolumeStatsRequest{VolumeId: volID, VolumePath: vPath}

	f.nodeGetVolumeStatsResponse, err = client.NodeGetVolumeStats(ctx, req)

	return err
}

func (f *feature) theVolumeConditionIs(condition string) error {
	fmt.Printf("f.nodeGetVolumeStatsResponse is %v\n", f.nodeGetVolumeStatsResponse)

	abnormal := false

	if condition == "abnormal" {
		abnormal = true
	}

	if f.nodeGetVolumeStatsResponse.VolumeCondition.Abnormal == abnormal {
		fmt.Printf("f.nodeGetVolumeStatsResponse check passed")
		return nil
	}
	fmt.Printf("abnormal should have been %v, but was %v instead", abnormal, f.nodeGetVolumeStatsResponse.VolumeCondition.Abnormal)
	return status.Errorf(codes.Internal, "Check NodeGetVolumeStatsResponse failed")
}

func (f *feature) nodeExpandVolume(volID, volPath string) error {
	var resp *csi.NodeExpandVolumeResponse
	var err error
	req := &csi.NodeExpandVolumeRequest{
		VolumeId:   volID,
		VolumePath: volPath,
	}
	ctx := context.Background()
	client := csi.NewNodeClient(grpcClient)
	// Retry loop to deal with API being overwhelmed
	for i := 0; i < f.maxRetryCount; i++ {
		resp, err = client.NodeExpandVolume(ctx, req)
		if err == nil {
			break
		}
		fmt.Printf("Node ExpandVolume retry: %s\n", err.Error())
		time.Sleep(RetrySleepTime)
	}
	f.nodeExpandVolumeResponse = resp
	return err
}

func (f *feature) iCallControllerGetVolume() error {
	var err error
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	req := &csi.ControllerGetVolumeRequest{
		VolumeId: f.volID,
	}
	f.controllerGetVolumeResponse, err = client.ControllerGetVolume(ctx, req)
	if err != nil {
		f.addError(err)
	}

	return nil
}

func (f *feature) theVolumeconditionIs(health string) error {
	abnormal := false

	if health == "healthy" {
		abnormal = false
	}

	if health == "unhealthy" {
		abnormal = true
	}

	if f.controllerGetVolumeResponse.Status.VolumeCondition.Abnormal == abnormal {
		fmt.Printf("the Volume is in a good condition")
		return nil
	}

	if f.controllerGetVolumeResponse.Status.VolumeCondition.Abnormal == abnormal {
		fmt.Printf("the Volume is not found")
		return nil
	}
	return nil
}

// add given suffix to name or use time as suffix and set to max of 30 characters
func makeAUniqueName(name *string) {
	if name == nil {
		temp := "tmp"
		name = &temp
	}
	suffix := os.Getenv("VOL_NAME_SUFFIX")
	if len(suffix) == 0 {
		now := time.Now()
		suffix = fmt.Sprintf("%02d%02d%02d", now.Hour(), now.Minute(), now.Second())
		*name += "_" + suffix
	} else {
		*name += "_" + suffix
	}
	tmp := *name
	if len(tmp) > 30 {
		*name = tmp[len(tmp)-30:]
	}
}

func (f *feature) iSetBadAllSystemNames() error {
	name := os.Getenv("ALT_SYSTEM_ID")
	for _, a := range f.arrays {
		if strings.Contains(a.AllSystemNames, name) {
			a.AllSystemNames = "badname"
			fmt.Printf("set bad allSystemNames for %s done \n", name)
			return nil
		}
	}
	return fmt.Errorf("Error during set bad secret allSystemNames for %s", name)
}

// And Set System Name As  "id-some-name" or "id_some_name"
func (f *feature) iSetSystemName(name string) error {
	parts := strings.Split(name, "-")
	id := ""
	if len(parts) > 1 {
		id = parts[0]
	} else {
		parts = strings.Split(name, "_")
		if len(parts) > 1 {
			id = parts[0]
		}
	}
	isNumeric := regexp.MustCompile(`^[0-9a-f]+$`).MatchString
	if !isNumeric(id) {
		return fmt.Errorf("Error during set name on pflex %s is not id of system", id)
	}
	endpoint := ""
	var array *ArrayConnectionData
	for _, a := range f.arrays {
		if strings.Contains(a.SystemID, id) || strings.Contains(a.SystemID, "pflex") {
			endpoint = a.Endpoint
			array = a
		}
	}
	if array == nil {
		return fmt.Errorf("Error during set name on pflex %s not found in secret", name)
	}
	if endpoint != "" {
		cred := array.Username + ":" + array.Password
		url := endpoint + "/api/login"
		fmt.Printf("call url %s\n", url)
		token, err := f.restCallToSetName(cred, url, "")
		if err != nil {
			fmt.Printf("name changed error %s", err.Error())
			return err
		}
		if len(token) > 1 {
			auth := array.Username + ":" + token
			urlsys := endpoint + "/api/instances/System::" + id + "/action/setSystemName"
			fmt.Printf("call urlsys %s\n", urlsys)
			fmt.Printf("call name %s\n", name)
			_, err := f.restCallToSetName(auth, urlsys, name)
			if err != nil {
				return fmt.Errorf("Error during set name on pflex %s", err.Error())
			}
			os.Setenv("ALT_SYSTEM_ID", name)
			return nil
		}
	}
	return fmt.Errorf("Error during set name on pflex %s", name)
}

func (f *feature) restCallToSetName(auth string, url string, name string) (string, error) {
	var req *http.Request
	var err error
	if name != "" {
		type Payload struct {
			NewName string `json:"newName"`
		}
		data := Payload{
			NewName: name,
		}
		payloadBytes, err := json.Marshal(data)
		if err != nil {
			// handle err
			fmt.Printf("name change rest payload error %s", err.Error())
			return "", err
		}
		body := bytes.NewReader(payloadBytes)
		req, err = http.NewRequest("POST", url, body)
		fmt.Printf("name change body  %#v\n", data)
	} else {
		req, err = http.NewRequest("GET", url, nil)
	}
	if err != nil {
		fmt.Printf("name change rest error %s", err.Error())
		return "", err
	}

	tokens := strings.Split(auth, ":")
	req.SetBasicAuth(tokens[0], tokens[1])
	req.Header.Set("Content-Type", "application/json")

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402
	}
	hc := &http.Client{Timeout: 2 * time.Second, Transport: tr}

	resp, err := hc.Do(req)
	if err != nil {
		// handle err
		fmt.Printf("name change rest error %s", err.Error())
		return "", err
	}
	if name == "" {
		responseData, _ := io.ReadAll(resp.Body)
		token := regexp.MustCompile(`^"(.*)"$`).ReplaceAllString(string(responseData), `$1`)
		fmt.Printf("name change token %s\n", token)
		return token, nil
	}
	responseData, _ := io.ReadAll(resp.Body)
	fmt.Printf("name change response %s\n", responseData)
	defer resp.Body.Close()
	return "", nil
}

func (f *feature) aBasicNfsVolumeRequestWithWrongNasName(name string, size int64) error {
	req := new(csi.CreateVolumeRequest)
	params := make(map[string]string)

	ctx := context.Background()
	nfsPool := os.Getenv("NFS_STORAGE_POOL")

	fmt.Println("f.arrays,len", f.arrays, f.arrays)

	if f.arrays == nil {
		fmt.Printf("Initialize ArrayConfig from %s:\n", configFile)
		var err error
		f.arrays, err = f.getArrayConfig(configFile)
		if err != nil {
			return errors.New("Get multi array config failed " + err.Error())
		}
	}

	wrongNasName := "wrongnas"

	for _, a := range f.arrays {
		systemid := a.SystemID
		val, err := f.checkNFS(ctx, systemid)
		if err != nil {
			return err
		}

		if val {
			if a.NasName != "" {
				params["nasName"] = wrongNasName
			}

			params["storagepool"] = nfsPool
			params["thickprovisioning"] = "false"
			if len(f.anotherSystemID) > 0 {
				params["systemID"] = f.anotherSystemID
			}
			req.Parameters = params
			makeAUniqueName(&name)
			req.Name = name
			capacityRange := new(csi.CapacityRange)
			capacityRange.RequiredBytes = size * 1024 * 1024 * 1024
			req.CapacityRange = capacityRange
			capability := new(csi.VolumeCapability)
			mount := new(csi.VolumeCapability_MountVolume)
			mount.FsType = "nfs"
			mountType := new(csi.VolumeCapability_Mount)
			mountType.Mount = mount
			capability.AccessType = mountType
			accessMode := new(csi.VolumeCapability_AccessMode)
			accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
			capability.AccessMode = accessMode
			f.capability = capability
			capabilities := make([]*csi.VolumeCapability, 0)
			capabilities = append(capabilities, capability)
			req.VolumeCapabilities = capabilities
			f.createVolumeRequest = req
			return nil
		}
		fmt.Printf("Array with SystemId %s does not support NFS. Skipping this step", systemid)
		return nil
	}
	return nil
}

func (f *feature) aNfsCapabilityWithVoltypeAccessFstype(voltype, access, fstype string) error {
	// Construct the volume capabilities
	ctx := context.Background()

	fmt.Println("f.arrays,len", f.arrays, f.arrays)

	if f.arrays == nil {
		fmt.Printf("Initialize ArrayConfig from %s:\n", configFile)
		var err error
		f.arrays, err = f.getArrayConfig(configFile)
		if err != nil {
			return errors.New("Get multi array config failed " + err.Error())
		}
	}

	for _, a := range f.arrays {
		systemid := a.SystemID
		val, err := f.checkNFS(ctx, systemid)
		if err != nil {
			return err
		}

		if val {
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
			case "single-writer":
				accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
			case "multi-writer":
				accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER
			case "multi-reader":
				accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY
			case "multi-node-single-writer":
				accessMode.Mode = csi.VolumeCapability_AccessMode_MULTI_NODE_SINGLE_WRITER
			case "single-node-single-writer":
				accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_SINGLE_WRITER
			case "single-node-multi-writer":
				accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_MULTI_WRITER
			}
			capability.AccessMode = accessMode
			f.capabilities = make([]*csi.VolumeCapability, 0)
			f.capabilities = append(f.capabilities, capability)
			f.capability = capability
			return nil
		}
		fmt.Printf("Array with SystemId %s does not support NFS. Skipping this step", systemid)
		return nil
	}
	return nil
}

func (f *feature) aNfsVolumeRequest(name string, size int64) error {
	ctx := context.Background()
	nfsPool := os.Getenv("NFS_STORAGE_POOL")

	fmt.Println("f.arrays,len", f.arrays, f.arrays)

	if f.arrays == nil {
		fmt.Printf("Initialize ArrayConfig from %s:\n", configFile)
		var err error
		f.arrays, err = f.getArrayConfig(configFile)
		if err != nil {
			return errors.New("Get multi array config failed " + err.Error())
		}
	}

	for _, a := range f.arrays {
		systemid := a.SystemID
		val, err := f.checkNFS(ctx, systemid)
		if err != nil {
			return err
		}

		if val {
			req := new(csi.CreateVolumeRequest)
			params := make(map[string]string)
			if a.NasName != "" {
				params["nasName"] = a.NasName
			}
			params["storagepool"] = nfsPool
			params["thickprovisioning"] = "false"
			if os.Getenv("X_CSI_QUOTA_ENABLED") == "true" {
				params["isQuotaEnabled"] = "true"
				params["softLimit"] = "20"
				params["path"] = "/nfs-quota1"
				params["gracePeriod"] = "86400"
			}
			if len(f.anotherSystemID) > 0 {
				params["systemID"] = f.anotherSystemID
			}
			req.Parameters = params
			makeAUniqueName(&name)
			req.Name = name
			capacityRange := new(csi.CapacityRange)
			capacityRange.RequiredBytes = size * 1024 * 1024 * 1024
			req.CapacityRange = capacityRange
			req.VolumeCapabilities = f.capabilities
			f.createVolumeRequest = req
			return nil
		}
		fmt.Printf("Array with SystemId %s does not support NFS. Skipping this step", systemid)
		return nil
	}
	return nil
}

func (f *feature) whenICallPublishVolumeForNfsWithoutSDC() error {
	if f.createVolumeRequest == nil {
		return nil
	}
	err := f.controllerPublishVolumeForNfsWithoutSDC(f.volID)
	if err != nil {
		fmt.Printf("ControllerPublishVolume %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("ControllerPublishVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

func (f *feature) whenICallPublishVolumeForNfs(nodeIDEnvVar string) error {
	if f.createVolumeRequest == nil {
		return nil
	}
	err := f.controllerPublishVolumeForNfs(f.volID, nodeIDEnvVar)
	if err != nil {
		fmt.Printf("ControllerPublishVolume %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("ControllerPublishVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

func (f *feature) controllerPublishVolumeForNfsWithoutSDC(id string) error {
	if f.createVolumeRequest == nil {
		return nil
	}

	clientSet := fake.NewSimpleClientset()
	service.K8sClientset = clientSet
	_, err := f.setFakeNode()
	if err != nil {
		return fmt.Errorf("setFakeNode failed with error: %v", err)
	}

	req := f.getControllerPublishVolumeRequest()
	req.VolumeId = id
	req.NodeId, _ = f.GetNodeUID()

	err = f.createConfigMap()
	if err != nil {
		return fmt.Errorf("createConfigMap failed with error: %v", err)
	}

	if f.arrays == nil {
		fmt.Printf("Initialize ArrayConfig from %s:\n", configFile)
		var err error
		f.arrays, err = f.getArrayConfig(configFile)
		if err != nil {
			return errors.New("Get multi array config failed " + err.Error())
		}
	}

	for _, a := range f.arrays {
		req.VolumeContext = make(map[string]string)
		req.VolumeContext["nasName"] = a.NasName
		req.VolumeContext["fsType"] = "nfs"
		ctx := context.Background()
		client := csi.NewControllerClient(grpcClient)
		_, err := client.ControllerPublishVolume(ctx, req)
		return err
	}

	return nil
}

func (f *feature) controllerPublishVolumeForNfs(id string, nodeIDEnvVar string) error {
	if f.createVolumeRequest == nil {
		return nil
	}
	req := f.getControllerPublishVolumeRequest()
	req.VolumeId = id
	req.NodeId = os.Getenv(nodeIDEnvVar)

	if f.arrays == nil {
		fmt.Printf("Initialize ArrayConfig from %s:\n", configFile)
		var err error
		f.arrays, err = f.getArrayConfig(configFile)
		if err != nil {
			return errors.New("Get multi array config failed " + err.Error())
		}
	}

	for _, a := range f.arrays {
		req.VolumeContext = make(map[string]string)
		req.VolumeContext["nasName"] = a.NasName
		req.VolumeContext["fsType"] = "nfs"
		ctx := context.Background()
		client := csi.NewControllerClient(grpcClient)
		_, err := client.ControllerPublishVolume(ctx, req)
		return err
	}

	return nil
}

func (f *feature) getNodePublishVolumeRequestForNfs() *csi.NodePublishVolumeRequest {
	if f.createVolumeRequest == nil {
		return nil
	}
	req := new(csi.NodePublishVolumeRequest)
	req.VolumeId = f.volID
	req.Readonly = false
	req.VolumeContext = make(map[string]string)
	req.VolumeContext["fsType"] = "nfs"

	if f.capability.AccessMode.Mode == csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY {
		req.Readonly = true
	}

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
	return req
}

func (f *feature) whenICallNodePublishVolumeForNfsWithoutSDC() error {
	if f.createVolumeRequest == nil {
		return nil
	}
	err := f.nodePublishVolumeForNfs(f.volID, "")
	if err != nil {
		fmt.Printf("NodePublishVolume failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("NodePublishVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

//nolint:revive
func (f *feature) whenICallNodePublishVolumeForNfs(arg1 string) error {
	if f.createVolumeRequest == nil {
		return nil
	}
	err := f.nodePublishVolumeForNfs(f.volID, "")
	if err != nil {
		fmt.Printf("NodePublishVolume failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("NodePublishVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

func (f *feature) nodePublishVolumeForNfs(id string, path string) error {
	if f.createVolumeRequest == nil {
		return nil
	}
	req := f.getNodePublishVolumeRequestForNfs()
	if path != "" {
		block := f.capability.GetBlock()
		if block != nil {
			req.TargetPath = path
		}
		mount := f.capability.GetMount()
		if mount != nil {
			req.TargetPath = path
		}
	}
	req.VolumeId = id
	ctx := context.Background()
	client := csi.NewNodeClient(grpcClient)
	_, err := client.NodePublishVolume(ctx, req)
	return err
}

func (f *feature) whenICallNodeUnpublishVolumeForNfsWithoutSDC() error {
	if f.createVolumeRequest == nil {
		return nil
	}
	err := f.nodeUnpublishVolumeForNfs(f.volID, f.nodePublishVolumeRequest.TargetPath)
	if err != nil {
		fmt.Printf("NodeUnpublishVolume failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("NodeUnpublishVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

//nolint:revive
func (f *feature) whenICallNodeUnpublishVolumeForNfs(arg1 string) error {
	if f.createVolumeRequest == nil {
		return nil
	}
	err := f.nodeUnpublishVolumeForNfs(f.volID, f.nodePublishVolumeRequest.TargetPath)
	if err != nil {
		fmt.Printf("NodeUnpublishVolume failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("NodeUnpublishVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

func (f *feature) nodeUnpublishVolumeForNfs(id string, path string) error {
	if f.createVolumeRequest == nil {
		return nil
	}
	req := &csi.NodeUnpublishVolumeRequest{VolumeId: id, TargetPath: path}
	ctx := context.Background()
	client := csi.NewNodeClient(grpcClient)
	_, err := client.NodeUnpublishVolume(ctx, req)
	return err
}

func (f *feature) whenICallUnpublishVolumeForNfsWithoutSDC() error {
	if f.createVolumeRequest == nil {
		return nil
	}
	err := f.controllerUnpublishVolumeForNfsWithoutSDC(f.publishVolumeRequest.VolumeId)
	if err != nil {
		fmt.Printf("ControllerUnpublishVolume failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("ControllerUnpublishVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

func (f *feature) whenICallUnpublishVolumeForNfs(nodeIDEnvVar string) error {
	if f.createVolumeRequest == nil {
		return nil
	}
	err := f.controllerUnpublishVolumeForNfs(f.publishVolumeRequest.VolumeId, nodeIDEnvVar)
	if err != nil {
		fmt.Printf("ControllerUnpublishVolume failed: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("ControllerUnpublishVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

func (f *feature) controllerUnpublishVolumeForNfsWithoutSDC(id string) error {
	if f.createVolumeRequest == nil {
		return nil
	}

	clientSet := fake.NewSimpleClientset()
	service.K8sClientset = clientSet
	_, err := f.setFakeNode()
	if err != nil {
		return fmt.Errorf("setFakeNode failed with error: %v", err)
	}

	req := new(csi.ControllerUnpublishVolumeRequest)
	req.VolumeId = id
	req.NodeId, _ = f.GetNodeUID()
	err = f.createConfigMap()
	if err != nil {
		return fmt.Errorf("createConfigMap failed with error: %v", err)
	}

	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	_, err = client.ControllerUnpublishVolume(ctx, req)
	return err
}

func (f *feature) controllerUnpublishVolumeForNfs(id string, nodeIDEnvVar string) error {
	if f.createVolumeRequest == nil {
		return nil
	}
	req := new(csi.ControllerUnpublishVolumeRequest)
	req.VolumeId = id
	req.NodeId = os.Getenv(nodeIDEnvVar)
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	_, err := client.ControllerUnpublishVolume(ctx, req)
	return err
}

func (f *feature) whenICallNfsExpandVolumeTo(size int64) error {
	if f.createVolumeRequest == nil {
		return nil
	}
	err := f.controllerExpandVolumeForNfs(f.volID, size)
	if err != nil {
		fmt.Printf("ControllerExpandVolume %s:\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("ControllerExpandVolume completed successfully\n")
	}
	time.Sleep(SleepTime)
	return nil
}

func (f *feature) controllerExpandVolumeForNfs(volID string, size int64) error {
	const bytesInKiB = 1024
	var resp *csi.ControllerExpandVolumeResponse
	var err error
	req := &csi.ControllerExpandVolumeRequest{
		VolumeId:      volID,
		CapacityRange: &csi.CapacityRange{RequiredBytes: size * bytesInKiB * bytesInKiB * bytesInKiB},
	}
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	for i := 0; i < f.maxRetryCount; i++ {
		resp, err = client.ControllerExpandVolume(ctx, req)
		if err == nil {
			break
		}
		fmt.Printf("Controller ExpandVolume retry: %s\n", err.Error())
		time.Sleep(RetrySleepTime)
	}
	f.expandVolumeResponse = resp
	return err
}

func (f *feature) ICallListFileSystemSnapshot() error {
	ctx := context.Background()
	if f.arrays == nil {
		fmt.Printf("Initialize ArrayConfig from %s:\n", configFile)
		var err error
		f.arrays, err = f.getArrayConfig(configFile)
		if err != nil {
			return errors.New("Get multi array config failed " + err.Error())
		}
	}

	for _, a := range f.arrays {
		systemid := a.SystemID
		val, err := f.checkNFS(ctx, systemid)
		if err != nil {
			return err
		}

		if val {
			c, err := f.getGoscaleioClient()
			if err != nil {
				return errors.New("Geting goscaleio client failed " + err.Error())
			}
			system, err := c.FindSystem(systemid, "", "")
			if err != nil {
				return err
			}

			FileSystems, err := system.GetAllFileSystems()
			if err != nil {
				return err
			}
			var foundSnapshot bool
			for j := 0; j < len(FileSystems); j++ {
				fs := FileSystems[j]
				fsID := fs.ID

				if f.snapshotID != "" && strings.Contains(f.snapshotID, fsID) {
					foundSnapshot = true
					fmt.Printf("found_snapshot changed to %v", foundSnapshot)

				}
			}
			if f.snapshotID != "" && !foundSnapshot {
				msg := "ListFileSystemSnapshot does not contain snap id " + f.snapshotID
				fmt.Print(msg)
				return errors.New(msg)
			}
			return nil
		}
		fmt.Printf("Array with SystemId %s does not support NFS. Skipping this step", systemid)
		return nil
	}
	return nil
}

func (f *feature) iCallCreateSnapshotForFS() error {
	if f.createVolumeRequest == nil {
		return nil
	}
	var err error
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	req := &csi.CreateSnapshotRequest{
		SourceVolumeId: f.volID,
		Name:           "snapshot-9x89727a-0000-11e9-ab1c-005056a64ad3",
	}
	resp, err := client.CreateSnapshot(ctx, req)
	if err != nil {
		fmt.Printf("CreateSnapshot returned error: %s\n", err.Error())
		f.addError(err)
	} else {
		f.snapshotID = resp.Snapshot.SnapshotId
		//fmt.Printf("createSnapshot: SnapshotId %s SourceVolumeId %s CreationTime %s\n",
		//	resp.Snapshot.SnapshotId, resp.Snapshot.SourceVolumeId, resp.Snapshot.CreationTime.AsTime().Format(time.RFC3339))
	}
	time.Sleep(RetrySleepTime)
	return nil
}

func (f *feature) iCallDeleteSnapshotForFS() error {
	if f.createVolumeRequest == nil {
		return nil
	}
	ctx := context.Background()
	client := csi.NewControllerClient(grpcClient)
	req := &csi.DeleteSnapshotRequest{
		SnapshotId: f.snapshotID,
	}
	_, err := client.DeleteSnapshot(ctx, req)
	if err != nil {
		fmt.Printf("DeleteSnapshot returned error: %s\n", err.Error())
		f.addError(err)
	} else {
		fmt.Printf("DeleteSnapshot: SnapshotId %s\n", req.SnapshotId)
	}
	time.Sleep(RetrySleepTime)
	return nil
}

func (f *feature) checkNFS(_ context.Context, systemID string) (bool, error) {
	c, err := f.getGoscaleioClient()
	if err != nil {
		return false, errors.New("Geting goscaleio client failed " + err.Error())
	}
	if c == nil {
		return false, nil
	}
	version, err := c.GetVersion()
	if err != nil {
		return false, err
	}
	ver, err := strconv.ParseFloat(version, 64)
	if err != nil {
		return false, err
	}
	if ver >= 4.0 {
		arrayConData, err := f.getArrayConfig(configFile)
		if err != nil {
			return false, err
		}
		array := arrayConData[systemID]
		if array.NasName == "" {
			fmt.Println("nasName value not found in secret, it is mandatory parameter for NFS volume operations")
		}
		return true, nil
	}
	return false, nil
}

func (f *feature) createFakeNodeLabels(zoneLabelKey string) error {
	if zoneLabelKey == "" {
		zoneLabelKey = os.Getenv("ZONE_LABEL_KEY")
	}
	nodeName := os.Getenv("X_CSI_POWERFLEX_KUBE_NODE_NAME")

	node := &apiv1.Node{
		ObjectMeta: v1.ObjectMeta{
			Name: nodeName,
			Labels: map[string]string{
				zoneLabelKey:             "zoneA",
				"kubernetes.io/hostname": nodeName,
			},
		},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{
					Type:   apiv1.NodeReady,
					Status: apiv1.ConditionTrue,
				},
			},
		},
	}
	_, err := service.K8sClientset.CoreV1().Nodes().Create(context.TODO(), node, v1.CreateOptions{})
	if err != nil {
		fmt.Printf("CreateNode returned error: %s\n", err.Error())
		return err
	}
	return nil
}

func (f *feature) iCallNodeGetInfo(zoneLabelKey string) error {
	fmt.Println("[iCallNodeGetInfo] Calling NodeGetInfo...")
	_, err := f.nodeGetInfo(f.nodeGetInfoRequest, zoneLabelKey)
	if err != nil {
		fmt.Printf("NodeGetInfo returned error: %s\n", err.Error())
		f.addError(err)
	}
	return nil
}

func (f *feature) nodeGetInfo(req *csi.NodeGetInfoRequest, zoneLabelKey string) (*csi.NodeGetInfoResponse, error) {
	fmt.Println("[nodeGetInfo] Calling NodeGetInfo...")

	ctx := context.Background()
	client := csi.NewNodeClient(grpcClient)
	var nodeResp *csi.NodeGetInfoResponse

	clientSet := fake.NewSimpleClientset()
	service.K8sClientset = clientSet

	err := f.createFakeNodeLabels(zoneLabelKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create fake node labels: %v", err)
	}

	nodeResp, err = client.NodeGetInfo(ctx, req)
	f.nodeGetInfoResponse = nodeResp
	return nodeResp, err
}

func (f *feature) aNodeGetInfoIsReturnedWithZoneTopology() error {
	accessibility := f.nodeGetInfoResponse.GetAccessibleTopology()
	log.Printf("Node Accessibility %+v", accessibility)
	if _, ok := accessibility.Segments[os.Getenv("ZONE_LABEL_KEY")]; !ok {
		return fmt.Errorf("zone not found")
	}
	return nil
}

func (f *feature) aNodeGetInfoIsReturnedWithoutZoneTopology(zoneLabelKey string) error {
	accessibility := f.nodeGetInfoResponse.GetAccessibleTopology()
	log.Printf("Node Accessibility %+v", accessibility)
	if _, ok := accessibility.Segments[zoneLabelKey]; ok {
		return fmt.Errorf("zone found")
	}
	return nil
}

func (f *feature) aNodeGetInfoIsReturnedWithSystemTopology() error {
	accessibility := f.nodeGetInfoResponse.GetAccessibleTopology()
	log.Printf("Node Accessibility %+v", accessibility)

	var err error
	f.arrays, err = f.getArrayConfig(zoneConfigFile)
	if err != nil {
		return fmt.Errorf("failed to get array config: %v", err)
	}

	labelAdded := false
	for _, array := range f.arrays {
		log.Printf("array systemID %+v", array.SystemID)
		if _, ok := accessibility.Segments[service.Name+"/"+array.SystemID]; ok {
			labelAdded = true
		}
	}

	if !labelAdded {
		return fmt.Errorf("topology with zone label not found")
	}
	return nil
}

func (f *feature) iCreateZoneRequest(name string) error {
	req := f.createGenericZoneRequest(name)
	req.AccessibilityRequirements = new(csi.TopologyRequirement)
	req.AccessibilityRequirements.Preferred = []*csi.Topology{
		{
			Segments: map[string]string{
				"zone.csi-vxflexos.dellemc.com": "zoneA",
			},
		},
		{
			Segments: map[string]string{
				"zone.csi-vxflexos.dellemc.com": "zoneB",
			},
		},
	}

	f.createVolumeRequest = req

	return nil
}

func (f *feature) iCreateInvalidZoneRequest() error {
	req := f.createGenericZoneRequest("invalid-zone-volume")
	req.AccessibilityRequirements = new(csi.TopologyRequirement)
	topologies := []*csi.Topology{
		{
			Segments: map[string]string{
				"zone.csi-vxflexos.dellemc.com": "invalidZoneInfo",
			},
		},
	}
	req.AccessibilityRequirements.Preferred = topologies
	f.createVolumeRequest = req

	return nil
}

func (f *feature) createGenericZoneRequest(name string) *csi.CreateVolumeRequest {
	req := new(csi.CreateVolumeRequest)
	storagePool := os.Getenv("STORAGE_POOL")
	params := make(map[string]string)
	params["storagepool"] = storagePool
	params["thickprovisioning"] = "false"
	if len(f.anotherSystemID) > 0 {
		params["systemID"] = f.anotherSystemID
	}
	req.Parameters = params
	makeAUniqueName(&name)
	req.Name = name
	capacityRange := new(csi.CapacityRange)
	capacityRange.RequiredBytes = 8 * 1024 * 1024 * 1024
	req.CapacityRange = capacityRange

	capability := new(csi.VolumeCapability)
	block := new(csi.VolumeCapability_BlockVolume)
	blockType := new(csi.VolumeCapability_Block)
	blockType.Block = block
	capability.AccessType = blockType
	accessMode := new(csi.VolumeCapability_AccessMode)
	accessMode.Mode = csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER
	capability.AccessMode = accessMode
	f.capability = capability
	capabilities := make([]*csi.VolumeCapability, 0)
	capabilities = append(capabilities, capability)
	req.VolumeCapabilities = capabilities

	return req
}

func FeatureContext(s *godog.ScenarioContext) {
	f := &feature{}
	s.Step(`^a VxFlexOS service$`, f.aVxFlexOSService)
	s.Step(`^a basic block volume request "([^"]*)" "(\d+(\.\d+)?)"$`, f.aBasicBlockVolumeRequest)
	s.Step(`^Set System Name As "([^"]*)"$`, f.iSetSystemName)
	s.Step(`^Set Bad AllSystemNames$`, f.iSetBadAllSystemNames)
	s.Step(`^I call CreateVolume$`, f.iCallCreateVolume)
	s.Step(`^I call DeleteVolume$`, f.iCallDeleteVolume)
	s.Step(`^there are no errors$`, f.thereAreNoErrors)
	s.Step(`^the error message should contain "([^"]*)"$`, f.theErrorMessageShouldContain)
	s.Step(`^a mount volume request "([^"]*)"$`, f.aMountVolumeRequest)
	s.Step(`^when I call PublishVolume$`, f.whenICallPublishVolume)
	s.Step(`^when I call UnpublishVolume$`, f.whenICallUnpublishVolume)
	s.Step(`^when I call PublishVolume "([^"]*)"$`, f.whenICallPublishVolume)
	s.Step(`^when I call UnpublishVolume "([^"]*)"$`, f.whenICallUnpublishVolume)
	s.Step(`^access type is "([^"]*)"$`, f.accessTypeIs)
	s.Step(`^max retries (\d+)$`, f.maxRetries)
	s.Step(`^a capability with voltype "([^"]*)" access "([^"]*)" fstype "([^"]*)"$`, f.aCapabilityWithVoltypeAccessFstype)
	s.Step(`^a volume request "([^"]*)" "(\d+)"$`, f.aVolumeRequest)
	s.Step(`^when I call NodePublishVolume "([^"]*)"$`, f.whenICallNodePublishVolume)
	s.Step(`^when I call NodePublishVolumeWithPoint "([^"]*)" "([^"]*)" "([^"]*)"$`, f.whenICallNodePublishVolumeWithPoint)
	s.Step(`^when I call NodeUnpublishVolume "([^"]*)"$`, f.whenICallNodeUnpublishVolume)
	s.Step(`^when I call NodeUnpublishVolumeWithPoint "([^"]*)" "([^"]*)"$`, f.whenICallNodeUnpublishVolumeWithPoint)
	s.Step(`^verify published volume with voltype "([^"]*)" access "([^"]*)" fstype "([^"]*)"$`, f.verifyPublishedVolumeWithVoltypeAccessFstype)
	s.Step(`^I call CreateSnapshot$`, f.iCallCreateSnapshot)
	s.Step(`^I call CreateSnapshotConsistencyGroup$`, f.iCallCreateSnapshotConsistencyGroup)
	s.Step(`^I call DeleteAllVolumes$`, f.iCallDeleteAllVolumes)
	s.Step(`^I call DeleteSnapshot$`, f.iCallDeleteSnapshot)
	s.Step(`^I call CreateVolumeFromSnapshot$`, f.iCallCreateVolumeFromSnapshot)
	s.Step(`^I call CreateManyVolumesFromSnapshot$`, f.iCallCreateManyVolumesFromSnapshot)
	s.Step(`^I call ListVolume$`, f.iCallListVolume)
	s.Step(`^a valid ListVolumeResponse is returned$`, f.aValidListVolumeResponseIsReturned)
	s.Step(`^I call ListSnapshot$`, f.iCallListSnapshot)
	s.Step(`^I call ListSnapshot For Snap$`, f.iCallListSnapshotForSnap)
	s.Step(`^a valid ListSnapshotResponse is returned$`, f.aValidListSnapshotResponseIsReturned)
	s.Step(`^expect Error ListSnapshotResponse$`, f.expectErrorListSnapshotResponse)
	s.Step(`^I create (\d+) volumes in parallel$`, f.iCreateVolumesInParallel)
	s.Step(`^I publish (\d+) volumes in parallel$`, f.iPublishVolumesInParallel)
	s.Step(`^I set another systemID "([^"]*)"$`, f.iSetAnotherSystemID)
	s.Step(`^I set another systemName "([^"]*)"$`, f.iSetAnotherSystemName)
	s.Step(`^I node publish (\d+) volumes in parallel$`, f.iNodePublishVolumesInParallel)
	s.Step(`^I node unpublish (\d+) volumes in parallel$`, f.iNodeUnpublishVolumesInParallel)
	s.Step(`^I unpublish (\d+) volumes in parallel$`, f.iUnpublishVolumesInParallel)
	s.Step(`^when I delete (\d+) volumes in parallel$`, f.whenIDeleteVolumesInParallel)
	s.Step(`^I write block data$`, f.iWriteBlockData)
	s.Step(`^I read write data to volume "([^"]*)"$`, f.iReadWriteToVolume)
	s.Step(`^when I call Validate Volume Host connectivity$`, f.iCallValidateVolumeHostConnectivity)
	s.Step(`^I call CreateVolumeGroupSnapshot$`, f.iCallCreateVolumeGroupSnapshot)
	s.Step(`^when I call ExpandVolume to "([^"]*)"$`, f.whenICallExpandVolumeTo)
	s.Step(`^when I call NodeExpandVolume$`, f.whenICallNodeExpandVolume)
	s.Step(`^I call CloneVolume$`, f.iCallCloneVolume)
	s.Step(`^I call CloneManyVolumes$`, f.iCallCloneManyVolumes)
	s.Step(`^I call EthemeralNodePublishVolume with ID "([^"]*)" and size "([^"]*)"$`, f.iCallEphemeralNodePublishVolume)
	s.Step(`^I call DeleteVGS$`, f.iCallDeleteVGS)
	s.Step(`^remove a volume from VolumeGroupSnapshotRequest$`, f.iRemoveAVolumeFromVolumeGroupSnapshotRequest)
	s.Step(`^I call split VolumeGroupSnapshot$`, f.iCallSplitVolumeGroupSnapshot)
	s.Step(`^I call ControllerGetVolume$`, f.iCallControllerGetVolume)
	s.Step(`^the volumecondition is "([^"]*)"$`, f.theVolumeconditionIs)
	s.Step(`^I call NodeGetVolumeStats$`, f.iCallNodeGetVolumeStats)
	s.Step(`^the VolumeCondition is "([^"]*)"$`, f.theVolumeConditionIs)
	s.Step(`^a basic nfs volume request with wrong nasname "([^"]*)" "(\d+)"$`, f.aBasicNfsVolumeRequestWithWrongNasName)
	s.Step(`^a basic nfs volume request "([^"]*)" "(\d+)"$`, f.aBasicNfsVolumeRequest)
	s.Step(`^a basic nfs volume request with quota enabled volname "([^"]*)" volsize "(\d+)" path "([^"]*)" softlimit "([^"]*)" graceperiod "([^"]*)"$`, f.aNfsVolumeRequestWithQuota)
	s.Step(`^a nfs capability with voltype "([^"]*)" access "([^"]*)" fstype "([^"]*)"$`, f.aNfsCapabilityWithVoltypeAccessFstype)
	s.Step(`^a nfs volume request "([^"]*)" "(\d+)"$`, f.aNfsVolumeRequest)
	s.Step(`^when I call PublishVolume for nfs "([^"]*)"$`, f.whenICallPublishVolumeForNfs)
	s.Step(`^when I call NodePublishVolume for nfs "([^"]*)"$`, f.whenICallNodePublishVolumeForNfs)
	s.Step(`^when I call NodeUnpublishVolume for nfs "([^"]*)"$`, f.whenICallNodeUnpublishVolumeForNfs)
	s.Step(`^when I call UnpublishVolume for nfs "([^"]*)"$`, f.whenICallUnpublishVolumeForNfs)
	s.Step(`^when I call PublishVolume for nfs$`, f.whenICallPublishVolumeForNfsWithoutSDC)
	s.Step(`^when I call NodePublishVolume for nfs$`, f.whenICallNodePublishVolumeForNfsWithoutSDC)
	s.Step(`^when I call NodeUnpublishVolume for nfs$`, f.whenICallNodeUnpublishVolumeForNfsWithoutSDC)
	s.Step(`^when I call UnpublishVolume for nfs$`, f.whenICallUnpublishVolumeForNfsWithoutSDC)
	s.Step(`^when I call NfsExpandVolume to "([^"]*)"$`, f.whenICallNfsExpandVolumeTo)
	s.Step(`^I call ListFileSystemSnapshot$`, f.ICallListFileSystemSnapshot)
	s.Step(`^I call CreateSnapshotForFS$`, f.iCallCreateSnapshotForFS)
	s.Step(`^I call DeleteSnapshotForFS$`, f.iCallDeleteSnapshotForFS)
	s.Step(`^I create a zone volume request "([^"]*)"$`, f.iCreateZoneRequest)
	s.Step(`^I create an invalid zone volume request$`, f.iCreateInvalidZoneRequest)
	s.Step(`^I call NodeGetInfo with "([^"]*)"$`, f.iCallNodeGetInfo)
	s.Step(`^a NodeGetInfo is returned with zone topology$`, f.aNodeGetInfoIsReturnedWithZoneTopology)
	s.Step(`^a NodeGetInfo is returned without zone topology "([^"]*)"$`, f.aNodeGetInfoIsReturnedWithoutZoneTopology)
	s.Step(`^a NodeGetInfo is returned with system topology$`, f.aNodeGetInfoIsReturnedWithSystemTopology)
}
