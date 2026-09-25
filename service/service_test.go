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

package service

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	gobrick "github.com/Ecosystems/container-storage-modules/src/gobrick"
	sio "github.com/Ecosystems/container-storage-modules/src/goscaleio"
	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	siotypes "github.com/Ecosystems/container-storage-modules/src/goscaleio/types/v1"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Test wrappers over net package: InterfaceByName/Addrs
func TestService_InterfaceByName_Invalid(t *testing.T) {
	s := &service{}
	// Use an unlikely interface name to force an error path
	iface, err := s.InterfaceByName("__nonexistent_if__")
	assert.Nil(t, iface)
	assert.Error(t, err)
}

// Additional small tests for early-return/error paths
func TestGetVolByID_NoAdminClient(t *testing.T) {
	s := &service{adminClients: map[string]*sio.Client{}}
	v, err := s.getVolByID("vol-xyz", "sys-xyz")
	assert.Nil(t, v)
	assert.Error(t, err)
}

func TestGetFilesystemByID_NoAdminClient(t *testing.T) {
	s := &service{adminClients: map[string]*sio.Client{}}
	fs, err := s.getFilesystemByID("fs-xyz", "sys-xyz")
	assert.Nil(t, fs)
	assert.Error(t, err)
}

func TestGetSDCID_SystemNotFound(t *testing.T) {
	s := &service{systems: map[string]*sio.System{}}
	id, err := s.getSDCID("guid", "missing-sys")
	assert.Empty(t, id)
	assert.Error(t, err)
}

func TestGetPlatformVersion_NoClient(t *testing.T) {
	s := &service{adminClients: map[string]*sio.Client{}}
	ver, err := s.GetPlatformVersion("nosys")
	assert.NoError(t, err)
	assert.Equal(t, 0.0, ver)
}

func TestGetGenType_NoSystem(t *testing.T) {
	// FR-3: nil system must return an error, not ("", nil).
	s := &service{systems: map[string]*sio.System{}}
	_, err := s.GetGenType("nosys")
	assert.Error(t, err)
}

// Additional targeted unit tests consolidated from service_extra_test.go
func TestGetPodIDFromTargetPath(t *testing.T) {
	// Save and restore original prefix provider
	orig := getTargetPathPrefix
	t.Cleanup(func() { getTargetPathPrefix = orig })
	getTargetPathPrefix = func() string { return "/var/lib/kubelet/pods/" }

	// Valid UUID in path
	p := "/var/lib/kubelet/pods/123e4567-e89b-12d3-a456-426614174000/volumes/kubernetes.io~csi/target"
	assert.Equal(t, "123e4567-e89b-12d3-a456-426614174000", getPodIDFromTargetPath(p))

	// Missing UUID -> empty
	assert.Equal(t, "", getPodIDFromTargetPath("/var/lib/kubelet/pods/not-a-uuid/volumes/..."))

	// Different prefix -> empty
	assert.Equal(t, "", getPodIDFromTargetPath("/some/other/path"))
}

func TestGetMetric(t *testing.T) {
	metrics := []siotypes.Metric{{Name: "a", Values: []float64{1}}, {Name: "host_write_iops", Values: []float64{42}}}
	assert.Equal(t, 42.0, getMetric(metrics, "host_write_iops"))
	assert.Equal(t, 0.0, getMetric(metrics, "missing"))
	assert.Equal(t, 0.0, getMetric([]siotypes.Metric{{Name: "empty", Values: nil}}, "empty"))
}

type fakeNVMEConnector struct {
	nqn []string
	err error
}

func (f *fakeNVMEConnector) ConnectVolume(_ context.Context, _ gobrick.NVMeVolumeInfo, _ bool) (gobrick.Device, error) {
	return gobrick.Device{}, nil
}

func (f *fakeNVMEConnector) DisconnectVolumeByDeviceName(_ context.Context, _ string) error {
	return nil
}

func (f *fakeNVMEConnector) GetInitiatorName(_ context.Context) ([]string, error) {
	return f.nqn, f.err
}

func TestInitConnectorsAndGetInitiators(t *testing.T) {
	s := &service{opts: Opts{NodeChrootPath: "/"}}
	// Ensure lazy init
	s.initConnectors()
	assert.NotNil(t, s.nvmeConnector)
	assert.NotNil(t, s.nvmeLib)

	// Override connector for deterministic GetInitiators
	s.nvmeConnector = &fakeNVMEConnector{nqn: []string{"nqn.2014-08.org.nvmexpress:uuid"}}
	inits, err := s.getInitiators()
	assert.NoError(t, err)
	assert.Equal(t, []string{"nqn.2014-08.org.nvmexpress:uuid"}, inits)
}

func TestGetNodeIPByCSINodeID(t *testing.T) {
	// Fake cluster with one node and one CSINode
	K8sClientset = fake.NewSimpleClientset(
		&v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "kube-node-1"},
			Status:     v1.NodeStatus{Addresses: []v1.NodeAddress{{Type: v1.NodeInternalIP, Address: "10.0.0.5"}}},
		},
		&storagev1.CSINode{
			ObjectMeta: metav1.ObjectMeta{Name: "kube-node-1"},
			Spec:       storagev1.CSINodeSpec{Drivers: []storagev1.CSINodeDriver{{Name: Name, NodeID: "node-123"}}},
		},
	)

	s := &service{}
	ip := s.GetNodeIPByCSINodeID("node-123")
	assert.Equal(t, "10.0.0.5", ip)
}

func TestService_Addrs_Smoke(t *testing.T) {
	s := &service{}
	ifaces, _ := net.Interfaces()
	if len(ifaces) == 0 {
		t.Skip("no network interfaces present")
	}
	_, err := s.Addrs(&ifaces[0])
	assert.NoError(t, err)
}

// Test isPaused helper in replication.go
func TestIsPaused(t *testing.T) {
	grp := &siotypes.ReplicationConsistencyGroup{PauseMode: "None"}
	assert.False(t, isPaused(grp))

	grp.PauseMode = "Paused"
	assert.True(t, isPaused(grp))
}

// Parsing helpers for CSI volume IDs
func TestGetVolumeIDFromCsiVolumeID(t *testing.T) {
	// Empty input
	assert.Equal(t, "", getVolumeIDFromCsiVolumeID(""))
	// No hyphen – return as-is
	assert.Equal(t, "abcd", getVolumeIDFromCsiVolumeID("abcd"))
	// Hyphenated – last token is the volume ID
	assert.Equal(t, "vol123", getVolumeIDFromCsiVolumeID("sys-az-vol123"))
}

func TestGetSystemIDFromCsiVolumeID(t *testing.T) {
	s := &service{connectedSystemNameToID: map[string]string{"sysName": "sysID"}}

	// Hyphenated block volume format: systemID-volID
	assert.Equal(t, "sysA", s.getSystemIDFromCsiVolumeID("sysA-vol1"))
	// Slash (NFS) format: systemID/filesystemID
	assert.Equal(t, "sysB", s.getSystemIDFromCsiVolumeID("sysB/fs1"))
	// Name mapping via connectedSystemNameToID
	assert.Equal(t, "sysID", s.getSystemIDFromCsiVolumeID("sysName-vol2"))
	assert.Equal(t, "sysID", s.getSystemIDFromCsiVolumeID("sysName/fs2"))
	// Missing system part returns empty
	assert.Equal(t, "", s.getSystemIDFromCsiVolumeID("justvol"))
}

var (
	testStatus    int
	testStartTime time.Time
)

func TestMain(m *testing.M) {
	testStatus = 0
	testStartTime = time.Now()

	if st := m.Run(); st > testStatus {
		testStatus = st
	}

	fmt.Printf("status %d\n", testStatus)

	os.Exit(testStatus)
}

func TestFeatures(t *testing.T) {
	defaultGetTargetPathPrefix := getTargetPathPrefix
	defaultEphemeralStagingMountPath := ephemeralStagingMountPath
	unitTestEmulateBlockDevice = true
	defer func() {
		getTargetPathPrefix = defaultGetTargetPathPrefix
		ephemeralStagingMountPath = defaultEphemeralStagingMountPath
		unitTestEmulateBlockDevice = false
	}()

	tempTargetPathPrefix := t.TempDir()
	getTargetPathPrefix = func() string {
		return tempTargetPathPrefix
	}

	nodePublishBlockDevicePath = filepath.Join(tempTargetPathPrefix, nodePublishBlockDevicePath)
	nodePublishAltBlockDevPath = filepath.Join(tempTargetPathPrefix, nodePublishAltBlockDevPath)
	nodePublishEphemDevPath = filepath.Join(tempTargetPathPrefix, nodePublishEphemDevPath)
	ephemeralStagingMountPath = tempTargetPathPrefix

	server := &http.Server{
		Addr:              "localhost:6060",
		ReadHeaderTimeout: 60 * time.Second,
	}

	go server.ListenAndServe()
	fmt.Printf("starting godog...\n")

	opts := godog.Options{
		Format: "pretty",
		Paths:  []string{"features"},
		Tags:   "~@gateway_monitoring",
	}

	status := godog.TestSuite{
		Name:                "godog",
		ScenarioInitializer: FeatureContext,
		Options:             &opts,
	}.Run()

	if status > 0 {
		t.Error("godog tests failed")
	}
}

func Test_service_SetPodZoneLabel(t *testing.T) {
	type fields struct {
		opts                    Opts
		adminClients            map[string]*sio.Client
		systems                 map[string]*sio.System
		mode                    string
		volCache                []*siotypes.Volume
		volCacheSystemID        string
		snapCache               []*siotypes.Volume
		snapCacheSystemID       string
		privDir                 string
		storagePoolIDToName     map[string]string
		statisticsCounter       int
		volumePrefixToSystems   map[string][]string
		connectedSystemNameToID map[string]string
	}

	type args struct {
		ctx       context.Context
		zoneLabel map[string]string
	}

	const validZoneName = "zoneA"
	const validZoneLabelKey = "topology.kubernetes.io/zone"
	const validAppName = "test-node-pod"
	const validAppLabelKey = "app"
	const validNodeName = "kube-node-name"
	validAppLabels := map[string]string{validAppLabelKey: validAppName}

	tests := map[string]struct {
		fields   fields
		args     args
		initTest func(s *service)
		wantErr  bool
	}{
		"successfully add zone labels to a pod": {
			// happy path test
			wantErr: false,
			args: args{
				ctx: context.Background(),
				zoneLabel: map[string]string{
					validZoneLabelKey: validZoneName,
				},
			},
			fields: fields{
				opts: Opts{
					KubeNodeName: validNodeName,
				},
			},
			initTest: func(s *service) {
				// setup fake k8s client and create a pod to perform tests against
				K8sClientset = fake.NewSimpleClientset()
				podClient := K8sClientset.CoreV1().Pods(DriverNamespace)

				// create test pod
				_, err := podClient.Create(context.Background(), &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:   validAppName,
						Labels: validAppLabels,
					},
					Spec: v1.PodSpec{
						NodeName: s.opts.KubeNodeName,
					},
				}, metav1.CreateOptions{})
				if err != nil {
					t.Errorf("error creating test pod error = %v", err)
				}
			},
		},
		"when 'list pods' k8s client request fails": {
			// Attempt to set pod labels when the k8s client cannot get pods
			wantErr: true,
			args: args{
				ctx: context.Background(),
				zoneLabel: map[string]string{
					validZoneLabelKey: validZoneName,
				},
			},
			fields: fields{
				opts: Opts{
					KubeNodeName: validNodeName,
				},
			},
			initTest: func(_ *service) {
				// create a client, but do not create any pods so the request
				// to list pods fails
				K8sClientset = fake.NewSimpleClientset()
			},
		},
		"clientset is nil and fails to create one": {
			wantErr: true,
			args: args{
				ctx: context.Background(),
				zoneLabel: map[string]string{
					validZoneLabelKey: validZoneName,
				},
			},
			fields: fields{
				opts: Opts{
					KubeNodeName: validNodeName,
				},
			},
			initTest: func(_ *service) {
				// setup clientset to nil to force creation
				// Creation should fail because tests are not run in a cluster
				K8sClientset = nil
			},
		},
	}

	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			s := &service{
				opts:                    tt.fields.opts,
				adminClients:            tt.fields.adminClients,
				systems:                 tt.fields.systems,
				mode:                    tt.fields.mode,
				volCache:                tt.fields.volCache,
				volCacheRWL:             sync.RWMutex{},
				volCacheSystemID:        tt.fields.volCacheSystemID,
				snapCache:               tt.fields.snapCache,
				snapCacheRWL:            sync.RWMutex{},
				snapCacheSystemID:       tt.fields.snapCacheSystemID,
				privDir:                 tt.fields.privDir,
				storagePoolIDToName:     tt.fields.storagePoolIDToName,
				statisticsCounter:       tt.fields.statisticsCounter,
				volumePrefixToSystems:   tt.fields.volumePrefixToSystems,
				connectedSystemNameToID: tt.fields.connectedSystemNameToID,
			}

			tt.initTest(s)
			err := s.SetPodZoneLabel(tt.args.ctx, tt.args.zoneLabel)
			if (err != nil) != tt.wantErr {
				t.Errorf("service.SetPodZoneLabel() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestArrayConnectionData_isInZone(t *testing.T) {
	type fields struct {
		SystemID                  string
		Username                  string
		Password                  string
		Endpoint                  string
		SkipCertificateValidation bool
		Insecure                  bool
		IsDefault                 bool
		AllSystemNames            string
		NasName                   string
		Zones                     []AvailabilityZone
	}
	type args struct {
		zoneName string
	}
	tests := map[string]struct {
		fields fields
		args   args
		want   bool
	}{
		"success": {
			want: true,
			fields: fields{
				Zones: []AvailabilityZone{
					{
						LabelKey: "topology.kubernetes.io/zone",
						Name:     "zoneA",
					},
				},
			},
			args: args{
				zoneName: "zoneA",
			},
		},
		"availability zone is not used": {
			want:   false,
			fields: fields{},
			args: args{
				zoneName: "zoneA",
			},
		},
		"zone names do not match": {
			want: false,
			fields: fields{
				Zones: []AvailabilityZone{
					{
						LabelKey: "topology.kubernetes.io/zone",
						Name:     "zoneA",
					},
				},
			},
			args: args{
				zoneName: "zoneB",
			},
		},
	}
	for testName, tt := range tests {
		t.Run(testName, func(t *testing.T) {
			array := &ArrayConnectionData{
				SystemID:                  tt.fields.SystemID,
				Username:                  tt.fields.Username,
				Password:                  tt.fields.Password,
				Endpoint:                  tt.fields.Endpoint,
				SkipCertificateValidation: tt.fields.SkipCertificateValidation,
				Insecure:                  tt.fields.Insecure,
				IsDefault:                 tt.fields.IsDefault,
				AllSystemNames:            tt.fields.AllSystemNames,
				NasName:                   tt.fields.NasName,
				Zones:                     tt.fields.Zones,
			}
			if got := array.isInZone(tt.args.zoneName); got != tt.want {
				t.Errorf("ArrayConnectionData.isInZone() = %v, want %v", got, tt.want)
			}
		})
	}
}

// QoS validation and comparison tests for validateAndCompareQoS
func TestValidateAndCompareQoS_NoParams_ReturnsNil(t *testing.T) {
	vc := map[string]string{}
	sdc := &siotypes.MappedSdcInfo{LimitBwInMbps: 0, LimitIops: 0}
	err := validateAndCompareQoS(vc, "volA", sdc)
	assert.NoError(t, err)
}

func TestValidateAndCompareQoS_BandwidthMismatch_ReturnsInvalidArgument(t *testing.T) {
	// sdc has 2 Mbps -> 2048 kbps; request 4096 => mismatch
	vc := map[string]string{KeyBandwidthLimitInKbps: "4096"}
	sdc := &siotypes.MappedSdcInfo{LimitBwInMbps: 2, LimitIops: 0}
	err := validateAndCompareQoS(vc, "volB", sdc)
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestValidateAndCompareQoS_IOPSMismatch_ReturnsInvalidArgument(t *testing.T) {
	vc := map[string]string{KeyIopsLimit: "3000"}
	sdc := &siotypes.MappedSdcInfo{LimitBwInMbps: 0, LimitIops: 2000}
	err := validateAndCompareQoS(vc, "volC", sdc)
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestValidateAndCompareQoS_MatchingValues_ReturnsNil(t *testing.T) {
	// sdc has 4 Mbps -> 4096 kbps; iops 5000; request matches
	vc := map[string]string{KeyBandwidthLimitInKbps: "4096", KeyIopsLimit: "5000"}
	sdc := &siotypes.MappedSdcInfo{LimitBwInMbps: 4, LimitIops: 5000}
	err := validateAndCompareQoS(vc, "volD", sdc)
	assert.NoError(t, err)
}

func TestValidateAndCompareQoS_NonNumericParam_ReturnsValidationError(t *testing.T) {
	vc := map[string]string{KeyBandwidthLimitInKbps: "not-a-number"}
	sdc := &siotypes.MappedSdcInfo{}
	err := validateAndCompareQoS(vc, "volE", sdc)
	assert.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}
