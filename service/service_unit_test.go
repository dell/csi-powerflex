// Copyright © 2019-2022 Dell Inc. or its subsidiaries. All Rights Reserved.
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
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/dell/csi-vxflexos/v2/k8sutils"
	sio "github.com/dell/goscaleio"
	siotypes "github.com/dell/goscaleio/types/v1"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/stretchr/testify/assert"
)

type mockService struct {
	service
}

func (s *mockService) InterfaceByName(interfaceName string) (*net.Interface, error) {
	if interfaceName == "" {
		return nil, fmt.Errorf("invalid interface name")
	} else if interfaceName != "eth0" {
		return nil, nil
	}
	return &net.Interface{
			Name: interfaceName,
		},
		nil
}

func (s *mockService) Addrs(interfaceObj *net.Interface) ([]net.Addr, error) {
	if interfaceObj == nil {
		return nil, fmt.Errorf("invalid interface object")
	}
	return []net.Addr{
		&net.IPNet{
			IP: net.IPv4(10, 0, 0, 1),
		},
	}, nil
}

func TestGetVolSize(t *testing.T) {
	tests := []struct {
		cr      *csi.CapacityRange
		sizeKiB int
	}{
		{
			// not requesting any range should result in a default size
			cr: &csi.CapacityRange{
				RequiredBytes: 0,
				LimitBytes:    0,
			},
			sizeKiB: DefaultVolumeSizeKiB,
		},
		{
			// requesting a size less than 1GiB should result in a minimal size
			cr: &csi.CapacityRange{
				RequiredBytes: 1,
				LimitBytes:    0,
			},
			sizeKiB: 8 * kiBytesInGiB,
		},
		{
			// not requesting a minimum but setting a limit below
			// the default size should result in an error
			cr: &csi.CapacityRange{
				RequiredBytes: 0,
				LimitBytes:    4 * bytesInGiB,
			},
			sizeKiB: 0,
		},
		{
			// requesting a size that is not evenly divisible by 8
			// should return a size rounded up to the next by 8
			cr: &csi.CapacityRange{
				RequiredBytes: 10 * bytesInGiB,
				LimitBytes:    0,
			},
			sizeKiB: 16 * kiBytesInGiB,
		},
		{
			// requesting a size that is not evenly divisible by 8
			// and is rounded up should return an error if max size
			// is in play
			cr: &csi.CapacityRange{
				RequiredBytes: 13 * bytesInGiB,
				LimitBytes:    14 * bytesInGiB,
			},
			sizeKiB: 0,
		},
		{
			// Requesting a size with decimal part that rounds up to next multiple of 8 GiB
			cr: &csi.CapacityRange{
				RequiredBytes: int64(9.5 * float64(bytesInGiB)),
				LimitBytes:    0,
			},
			sizeKiB: 16 * kiBytesInGiB,
		},
		{
			// Requesting a size of 8.5 GiB to test rounding up
			cr: &csi.CapacityRange{
				RequiredBytes: int64(48.5 * float64(bytesInGiB)),
				LimitBytes:    0,
			},
			sizeKiB: 56 * kiBytesInGiB,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run("", func(st *testing.T) {
			st.Parallel()
			size, err := validateVolSize(tt.cr)
			if tt.sizeKiB == 0 {
				// error is expected
				assert.Error(st, err)
			} else {
				assert.EqualValues(st, tt.sizeKiB, size)
			}
		})
	}
}

func TestGetProvisionType(t *testing.T) {
	tests := []struct {
		opts    Opts
		params  map[string]string
		volType string
	}{
		{
			// no opts and no params should default to thin
			opts:    Opts{},
			params:  make(map[string]string, 0),
			volType: thinProvisioned,
		},
		{
			// opts with thick and no params should be thin
			opts:    Opts{Thick: true},
			params:  make(map[string]string, 0),
			volType: thickProvisioned,
		},
		{
			// opts with thick and params to thin should be thin
			opts: Opts{Thick: true},
			params: map[string]string{
				KeyThickProvisioning: "false",
			},
			volType: thinProvisioned,
		},
		{
			// opts with thin and params to thick should be thick
			opts: Opts{Thick: false},
			params: map[string]string{
				KeyThickProvisioning: "true",
			},
			volType: thickProvisioned,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run("", func(st *testing.T) {
			st.Parallel()
			s := &service{opts: tt.opts}

			volType := s.getVolProvisionType(tt.params)
			assert.Equal(st, tt.volType, volType)
		})
	}
}

func TestVolumeCaps(t *testing.T) {
	tests := []struct {
		caps      []*csi.VolumeCapability
		vol       *siotypes.Volume
		supported bool
	}{
		// Unknown access mode is always unsupported
		{
			caps: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_UNKNOWN,
					},
				},
			},
			vol: &siotypes.Volume{
				MappingToAllSdcsEnabled: true,
			},
			supported: false,
		},
		{
			// Unknown access mode is always unsupported
			caps: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Block{
						Block: &csi.VolumeCapability_BlockVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_UNKNOWN,
					},
				},
			},
			vol: &siotypes.Volume{
				MappingToAllSdcsEnabled: true,
			},
			supported: false,
		},

		// SINGLE_NODE* is always supported
		{
			caps: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
			vol: &siotypes.Volume{
				MappingToAllSdcsEnabled: true,
			},
			supported: true,
		},
		{
			caps: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Block{
						Block: &csi.VolumeCapability_BlockVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER,
					},
				},
			},
			vol: &siotypes.Volume{
				MappingToAllSdcsEnabled: true,
			},
			supported: true,
		},
		{
			caps: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
					},
				},
			},
			vol: &siotypes.Volume{
				MappingToAllSdcsEnabled: true,
			},
			supported: true,
		},
		{
			caps: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Block{
						Block: &csi.VolumeCapability_BlockVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY,
					},
				},
			},
			vol: &siotypes.Volume{
				MappingToAllSdcsEnabled: true,
			},
			supported: true,
		},

		// MULTI_NODE_READER_ONLY supported when multi-map
		{
			caps: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
					},
				},
			},
			vol: &siotypes.Volume{
				MappingToAllSdcsEnabled: true,
			},
			supported: true,
		},
		{
			caps: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Block{
						Block: &csi.VolumeCapability_BlockVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
					},
				},
			},
			vol: &siotypes.Volume{
				MappingToAllSdcsEnabled: true,
			},
			supported: true,
		},
		{
			caps: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
					},
				},
			},
			vol: &siotypes.Volume{
				MappingToAllSdcsEnabled: false,
			},
			// removed dependence on MappingToAllSdcsEnabled TLW
			supported: true,
		},
		{
			caps: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Block{
						Block: &csi.VolumeCapability_BlockVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY,
					},
				},
			},
			vol: &siotypes.Volume{
				MappingToAllSdcsEnabled: false,
			},
			// removed dependence on MappingToAllSdcsEnabled TLW
			supported: true,
		},

		// MULTI_NODE_MULTI_WRITER always unsupported for mount
		{
			caps: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
					},
				},
			},
			vol: &siotypes.Volume{
				MappingToAllSdcsEnabled: false,
			},
			supported: false,
		},
		{
			caps: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
					},
				},
			},
			vol: &siotypes.Volume{
				MappingToAllSdcsEnabled: true,
			},
			supported: false,
		},

		// MULTI_NODE_MULTI_WRITER supported for block with multi-map
		{
			caps: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Block{
						Block: &csi.VolumeCapability_BlockVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
					},
				},
			},
			vol: &siotypes.Volume{
				MappingToAllSdcsEnabled: false,
			},
			// removed dependence on MappingToAllSdcsEnabled TLW
			supported: true,
		},
		{
			caps: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Block{
						Block: &csi.VolumeCapability_BlockVolume{},
					},
					AccessMode: &csi.VolumeCapability_AccessMode{
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER,
					},
				},
			},
			vol: &siotypes.Volume{
				MappingToAllSdcsEnabled: true,
			},
			supported: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run("", func(st *testing.T) {
			st.Parallel()
			s, _ := valVolumeCaps(tt.caps)

			assert.Equal(st, tt.supported, s)
		})
	}
}

func TestValidateQoSParameters(t *testing.T) {
	tests := []struct {
		bandwidthLimit string
		iopsLimit      string
		volumeName     string
		expectedError  error
	}{
		// requesting for valid values for both bandwidth and iops limit
		{
			bandwidthLimit: "10240",
			iopsLimit:      "12",
			volumeName:     "k8s-a031818af5",
			expectedError:  nil,
		},
		// requesting for invalid value bandwidth limit and valid value iops limit
		{
			bandwidthLimit: "10240kbps",
			iopsLimit:      "12",
			volumeName:     "k8s-a031818af5",
			expectedError:  errors.New("rpc error: code = InvalidArgument desc = requested Bandwidth limit: 10240kbps is not numeric for volume k8s-a031818af5, error: strconv.ParseInt: parsing \"10240kbps\": invalid syntax"),
		},
		// requesting for valid value bandwidth limit and invalid value iops limit
		{
			bandwidthLimit: "10240",
			iopsLimit:      "12iops",
			volumeName:     "k8s-a031818af5",
			expectedError:  errors.New("rpc error: code = InvalidArgument desc = requested IOPS limit: 12iops is not numeric for volume k8s-a031818af5, error: strconv.ParseInt: parsing \"12iops\": invalid syntax"),
		},
		// requesting for invalid values for both bandwidth and iops limit
		{
			bandwidthLimit: "10240kbps",
			iopsLimit:      "12iops",
			volumeName:     "k8s-a031818af5",
			expectedError:  errors.New("rpc error: code = InvalidArgument desc = requested Bandwidth limit: 10240kbps is not numeric for volume k8s-a031818af5, error: strconv.ParseInt: parsing \"10240kbps\": invalid syntax"),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run("", func(st *testing.T) {
			st.Parallel()
			err := validateQoSParameters(tt.bandwidthLimit, tt.iopsLimit, tt.volumeName)
			if err == tt.expectedError {
				fmt.Printf("Requested parameters are valid")
			} else if err != nil {
				if err.Error() != tt.expectedError.Error() {
					t.Errorf("Requested parameters are invalid, \n\tgot: %s \n\twant: %s",
						err, tt.expectedError)
				}
			}
		})
	}
}

func TestGetIPAddressByInterface(t *testing.T) {
	tests := []struct {
		name          string
		interfaceName string
		expectedIP    string
		expectedError error
	}{
		{
			name:          "Valid Interface Name",
			interfaceName: "eth0",
			expectedIP:    "10.0.0.1",
			expectedError: nil,
		},
		{
			name:          "Wrong Interface Name",
			interfaceName: "eth1",
			expectedIP:    "",
			expectedError: fmt.Errorf("invalid interface object"),
		},
		{
			name:          "Empty Interface Name",
			interfaceName: "",
			expectedIP:    "",
			expectedError: fmt.Errorf("invalid interface name"),
		},
	}

	for _, tt := range tests {
		s := &service{}
		t.Run(tt.name, func(t *testing.T) {
			interfaceIP, err := s.getIPAddressByInterface(tt.interfaceName, &mockService{})
			assert.Equal(t, err, tt.expectedError)
			assert.Equal(t, interfaceIP, tt.expectedIP)
		})
	}
}

func TestGetZoneKeyLabelFromSecret(t *testing.T) {
	tests := []struct {
		name          string
		arrays        map[string]*ArrayConnectionData
		expectedLabel string
		expectedErr   error
	}{
		{
			name:          "Empty array connection data",
			arrays:        map[string]*ArrayConnectionData{},
			expectedLabel: "",
			expectedErr:   nil,
		},
		{
			name: "Array connection data with same zone label keys",
			arrays: map[string]*ArrayConnectionData{
				"array1": {
					AvailabilityZone: &AvailabilityZone{
						Name:     "zone1",
						LabelKey: "custom-zone.io/area",
					},
				},
				"array2": {
					AvailabilityZone: &AvailabilityZone{
						Name:     "zone2",
						LabelKey: "custom-zone.io/area",
					},
				},
			},
			expectedLabel: "custom-zone.io/area",
			expectedErr:   nil,
		},
		{
			name: "Array connection data with different label keys",
			arrays: map[string]*ArrayConnectionData{
				"array1": {
					SystemID: "system-1",
					AvailabilityZone: &AvailabilityZone{
						Name:     "zone1",
						LabelKey: "custom-zone-1.io/area",
					},
				},
				"array2": {
					SystemID: "system-2",
					AvailabilityZone: &AvailabilityZone{
						Name:     "zone2",
						LabelKey: "custom-zone-2.io/area",
					},
				},
			},
			expectedLabel: "",
			expectedErr:   fmt.Errorf("array system-2 zone key custom-zone-2.io/area does not match custom-zone-1.io/area"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			label, err := getZoneKeyLabelFromSecret(tt.arrays)
			if tt.expectedErr == nil {
				assert.Nil(t, err)
			} else {
				assert.NotNil(t, err)
			}
			assert.Equal(t, label, tt.expectedLabel)
		})
	}
}

func TestFindNetworkInterfaceIPs(t *testing.T) {
	tests := []struct {
		name               string
		expectedError      string
		client             kubernetes.Interface
		createK8sClientSet func(kubeConfig ...string) error
		configMapData      map[string]string
		createConfigMap    func(map[string]string, kubernetes.Interface)
	}{
		{
			name:          "Error getting K8sClient",
			expectedError: "unable to load in-cluster configuration, KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT must be defined",
			createK8sClientSet: func(_ ...string) error {
				return fmt.Errorf("unable to load in-cluster configuration, KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT must be defined")
			},
			client:        nil,
			configMapData: nil,
			createConfigMap: func(map[string]string, kubernetes.Interface) {
			},
		},
		{
			name:          "Error getting ConfigMap",
			expectedError: "configmaps \"vxflexos-config-params\" not found",
			client:        fake.NewSimpleClientset(),
			configMapData: nil,
			createConfigMap: func(map[string]string, kubernetes.Interface) {
			},
		},
		{
			name:          "No Error",
			expectedError: "",
			client:        fake.NewSimpleClientset(),
			configMapData: map[string]string{
				"driver-config-params.yaml": `interfaceNames:
  worker1: 127.1.1.12`,
			},
			createConfigMap: func(data map[string]string, clientSet kubernetes.Interface) {
				configMap := &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      DriverConfigMap,
						Namespace: DriverNamespace,
					},
					Data: data,
				}
				// Create a ConfigMap using fake ClientSet
				_, err := clientSet.CoreV1().ConfigMaps(DriverNamespace).Create(context.TODO(), configMap, metav1.CreateOptions{})
				if err != nil {
					log.Fatalf("failed to create configMaps: %v", err)
				}
			},
		},
		{
			name:          "Error unmarshalling ConfigMap params",
			expectedError: "error converting YAML to JSON: yaml: line 1: did not find expected node content",
			client:        fake.NewSimpleClientset(),
			configMapData: map[string]string{
				"driver-config-params.yaml": `[interfaces:`,
			},
			createConfigMap: func(data map[string]string, clientSet kubernetes.Interface) {
				configMap := &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      DriverConfigMap,
						Namespace: DriverNamespace,
					},
					Data: data,
				}
				// Create a ConfigMap using fake ClientSet
				_, err := clientSet.CoreV1().ConfigMaps(DriverNamespace).Create(context.TODO(), configMap, metav1.CreateOptions{})
				if err != nil {
					log.Fatalf("failed to create configMaps: %v", err)
				}
			},
		},
		{
			name:          "Error getting the Network Interface IPs",
			expectedError: "failed to get the Network Interface IPs",
			client:        fake.NewSimpleClientset(),
			configMapData: map[string]string{
				"params-yaml": ``,
			},
			createConfigMap: func(data map[string]string, clientSet kubernetes.Interface) {
				configMap := &v1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      DriverConfigMap,
						Namespace: DriverNamespace,
					},
					Data: data,
				}
				// Create a ConfigMap using fake ClientSet
				_, err := clientSet.CoreV1().ConfigMaps(DriverNamespace).Create(context.TODO(), configMap, metav1.CreateOptions{})
				if err != nil {
					log.Fatalf("failed to create configMaps: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		s := &service{}
		t.Run(tt.name, func(t *testing.T) {
			defaultCreateKubeClientSet := CreateKubeClientSet
			if tt.createK8sClientSet != nil {
				CreateKubeClientSet = tt.createK8sClientSet
			}
			defer func() {
				if tt.createK8sClientSet != nil {
					CreateKubeClientSet = defaultCreateKubeClientSet
				}
			}()

			K8sClientset = tt.client
			tt.createConfigMap(tt.configMapData, tt.client)
			_, err := s.findNetworkInterfaceIPs()
			if tt.expectedError == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.expectedError)
			}
		})
	}
}

func TestConfigureAutoBlockProtocol(t *testing.T) {
	tests := []struct {
		name            string
		version         float64
		nvmeInitiators  int
		sdcGUID         string
		nodeProbeErr    error
		expectedUseSDC  bool
		expectedUseNVME bool
	}{
		{
			name:           "Both SDC and NVMeTCP available",
			version:        4.5,
			nvmeInitiators: 1,
			sdcGUID:        "some-guid",
			expectedUseSDC: true,
		},
		{
			name:            "Only NVMeTCP available",
			version:         4.5,
			nvmeInitiators:  1,
			sdcGUID:         "",
			expectedUseNVME: true,
		},
		{
			name:           "Only SDC available",
			version:        3.9,
			nvmeInitiators: 0,
			sdcGUID:        "some-guid",
			expectedUseSDC: true,
		},
		{
			name:           "Neither SDC nor NVMeTCP available",
			version:        3.9,
			nvmeInitiators: 0,
			sdcGUID:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockService{
				service: service{
					opts: Opts{
						SdcGUID: tt.sdcGUID,
					},
				},
			}

			svc.configureAutoBlockProtocol(context.Background(), tt.version, tt.nvmeInitiators)

			if svc.useSDC != tt.expectedUseSDC {
				t.Errorf("expected useSDC=%v, got %v", tt.expectedUseSDC, svc.useSDC)
			}
			if svc.useNVME != tt.expectedUseNVME {
				t.Errorf("expected useNVME=%v, got %v", tt.expectedUseNVME, svc.useNVME)
			}
		})
	}
}

// helper to build a server returning the given status and code
func newStatusServer(status ArrayConnectivityStatus, httpCode int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(httpCode)
		if httpCode == http.StatusOK {
			_ = json.NewEncoder(w).Encode(status)
		} else {
			_, _ = w.Write([]byte(`{"error":"error message"}`))
		}
	}))
}

func TestQueryArrayStatus_AllScenarios(t *testing.T) {
	ctx := context.Background()
	os.Setenv(EnvPodmonArrayConnectivityPollRate, "60")
	defer os.Unsetenv(EnvPodmonArrayConnectivityPollRate)

	tol := SetPollingFrequency(ctx)
	type tc struct {
		name     string
		makeURL  func(t *testing.T) string
		wantConn bool
		wantErr  bool
	}

	now := time.Now().Unix()

	cases := []tc{
		{
			name: "Connected_timeDiff<=tolerance+2",
			makeURL: func(t *testing.T) string {
				// timeDiff = LastAttempt - LastSuccess = 0 => connected
				resp := ArrayConnectivityStatus{
					LastAttempt: now,
					LastSuccess: now,
				}
				srv := newStatusServer(resp, http.StatusOK)
				t.Cleanup(srv.Close)
				return srv.URL
			},
			wantConn: true,
			wantErr:  false,
		},
		{
			name: "NotConnected_timeDiff>tolerance+2",
			makeURL: func(t *testing.T) string {
				// Make timeDiff strictly greater than tolerance+2
				// timeDiff = (now-1) - ((now-1) - (tol+3)) = tol+3
				resp := ArrayConnectivityStatus{
					LastAttempt: now - 1,
					LastSuccess: (now - 1) - (tol + 3),
				}
				srv := newStatusServer(resp, http.StatusOK)
				t.Cleanup(srv.Close)
				return srv.URL
			},
			wantConn: false,
			wantErr:  false,
		},
		{
			name: "Stale_currTime-LastAttempt>tolerance*2",
			makeURL: func(t *testing.T) string {
				// Stale branch: (currTime - LastAttempt) > 2*tol
				resp := ArrayConnectivityStatus{
					LastAttempt: now - (2*tol + 1),
					LastSuccess: now - 100, // arbitrary older success
				}
				srv := newStatusServer(resp, http.StatusOK)
				t.Cleanup(srv.Close)
				return srv.URL
			},
			wantConn: false,
			wantErr:  false,
		},
		{
			name: "HTTPNon200_returns_error",
			makeURL: func(t *testing.T) string {
				resp := ArrayConnectivityStatus{
					LastAttempt: now,
					LastSuccess: now,
				}
				srv := newStatusServer(resp, http.StatusInternalServerError)
				t.Cleanup(srv.Close)
				return srv.URL
			},
			wantConn: false,
			wantErr:  true,
		},
		{
			name: "BadJSON_returns_error",
			makeURL: func(t *testing.T) string {
				// 200 OK but invalid JSON to exercise unmarshal error path
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{invalid json`))
				}))
				t.Cleanup(srv.Close)
				return srv.URL
			},
			wantConn: false,
			wantErr:  true,
		},
		{
			name: "HTTPClientError_connection_refused",
			makeURL: func(_ *testing.T) string {
				// Unreachable port typically triggers client.Get error
				return "http://127.0.0.1:1"
			},
			wantConn: false,
			wantErr:  true,
		},
		{
			name: "PanicRecovery_server_panics",
			makeURL: func(t *testing.T) string {
				// The server panics; your function has a defer recover() that logs.
				// Behavior should result in an error (no crash).
				srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
					panic("test panic")
				}))
				t.Cleanup(srv.Close)
				return srv.URL
			},
			wantConn: false,
			wantErr:  true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &service{} // same package => can instantiate unexported type

			url := c.makeURL(t)
			connected, err := s.QueryArrayStatus(ctx, url)

			if c.wantErr && err == nil {
				t.Fatalf("expected error; got nil (connected=%v)", connected)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if connected != c.wantConn {
				t.Fatalf("connected: got %v, want %v (err=%v)", connected, c.wantConn, err)
			}
		})
	}
}

func TestStartGatewayMonitoring(t *testing.T) {
	t.Run("starts successfully with valid config", func(t *testing.T) {
		svc := &service{
			adminClients: map[string]*sio.Client{},
			opts: Opts{
				GatewayMonitoringInterval: 5 * time.Second,
				MetricsPort:               "0",
			},
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		svc.startGatewayMonitoring(ctx)

		assert.NotNil(t, svc.metricsServer, "metricsServer should be set after startGatewayMonitoring")
		assert.NotNil(t, svc.gatewayMonitor, "gatewayMonitor should be set after startGatewayMonitoring")
	})

	t.Run("uses default poll interval when interval is zero", func(t *testing.T) {
		svc := &service{
			adminClients: map[string]*sio.Client{},
			opts: Opts{
				GatewayMonitoringInterval: 0,
				MetricsPort:               "0",
			},
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		svc.startGatewayMonitoring(ctx)

		assert.NotNil(t, svc.metricsServer)
		assert.NotNil(t, svc.gatewayMonitor)
	})

	t.Run("does not panic when metrics server port is already in use", func(t *testing.T) {
		// Bind a listener to claim a port so that the second start fails.
		ln, listenErr := net.Listen("tcp", ":0")
		assert.NoError(t, listenErr)
		defer ln.Close()

		addr := ln.Addr().String()
		// Extract just the port number (without colon) since Start() prepends one.
		_, port, splitErr := net.SplitHostPort(addr)
		assert.NoError(t, splitErr)

		svc := &service{
			adminClients: map[string]*sio.Client{},
			opts: Opts{
				MetricsPort: port,
			},
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Should log an error but not panic.
		assert.NotPanics(t, func() {
			svc.startGatewayMonitoring(ctx)
		})
		assert.Nil(t, svc.metricsServer, "metricsServer should not be set when port binding fails")
	})
}

func TestStartMetricsServer(t *testing.T) {
	t.Run("starts successfully and sets metricsServer", func(t *testing.T) {
		svc := &service{
			opts: Opts{MetricsPort: "0"},
		}
		svc.startMetricsServer()
		assert.NotNil(t, svc.metricsServer, "metricsServer should be set after startMetricsServer")
	})

	t.Run("does not panic when port is already in use", func(t *testing.T) {
		ln, err := net.Listen("tcp", ":0")
		assert.NoError(t, err)
		defer ln.Close()

		_, port, err := net.SplitHostPort(ln.Addr().String())
		assert.NoError(t, err)

		svc := &service{opts: Opts{MetricsPort: port}}
		assert.NotPanics(t, func() { svc.startMetricsServer() })
		assert.Nil(t, svc.metricsServer, "metricsServer should remain nil when binding fails")
	})
}

func TestStartGatewayMonitor(t *testing.T) {
	t.Run("does not start when metricsServer is nil", func(t *testing.T) {
		svc := &service{
			adminClients: map[string]*sio.Client{},
			opts:         Opts{GatewayMonitoringInterval: 5 * time.Second},
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		assert.NotPanics(t, func() { svc.startGatewayMonitor(ctx) })
		assert.Nil(t, svc.gatewayMonitor, "gatewayMonitor should remain nil when metricsServer is nil")
	})

	t.Run("starts gateway monitor when metricsServer is running", func(t *testing.T) {
		svc := &service{
			adminClients: map[string]*sio.Client{},
			opts: Opts{
				MetricsPort:               "0",
				GatewayMonitoringInterval: 5 * time.Second,
			},
		}
		svc.startMetricsServer()
		assert.NotNil(t, svc.metricsServer)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		svc.startGatewayMonitor(ctx)
		assert.NotNil(t, svc.gatewayMonitor, "gatewayMonitor should be set when metricsServer is running")
	})
}

func TestStartGatewayMonitoringWithLeaderElection(t *testing.T) {
	origLEFunc := k8sutils.LeaderElectionFunc
	origInClusterFunc := k8sutils.InClusterConfigFunc
	origNewForConfigFunc := k8sutils.NewForConfigFunc
	origClientset := K8sClientset
	defer func() {
		k8sutils.LeaderElectionFunc = origLEFunc
		k8sutils.InClusterConfigFunc = origInClusterFunc
		k8sutils.NewForConfigFunc = origNewForConfigFunc
		K8sClientset = origClientset
	}()

	t.Run("calls LeaderElectionFunc with correct lock name", func(t *testing.T) {
		K8sClientset = fake.NewSimpleClientset()
		leCalled := make(chan string, 1)

		k8sutils.LeaderElectionFunc = func(_ *kubernetes.Interface, lockName string, _ string, _ func(context.Context)) error {
			leCalled <- lockName
			return nil
		}

		svc := &service{
			adminClients: map[string]*sio.Client{},
			opts: Opts{
				MetricsPort:               "0",
				GatewayMonitoringInterval: 5 * time.Second,
			},
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		svc.startGatewayMonitoringWithLeaderElection(ctx)

		select {
		case name := <-leCalled:
			assert.Equal(t, "gateway-monitor-csi-vxflexos-dellemc-com", name)
		case <-ctx.Done():
			t.Fatal("LeaderElectionFunc was not called within timeout")
		}
	})

	t.Run("creates k8s clientset when K8sClientset is nil", func(t *testing.T) {
		K8sClientset = nil
		k8sutils.InClusterConfigFunc = func() (*rest.Config, error) {
			return &rest.Config{}, nil
		}
		k8sutils.NewForConfigFunc = func(_ *rest.Config) (kubernetes.Interface, error) {
			return fake.NewSimpleClientset(), nil
		}
		leCalled := make(chan bool, 1)
		k8sutils.LeaderElectionFunc = func(_ *kubernetes.Interface, _ string, _ string, _ func(context.Context)) error {
			leCalled <- true
			return nil
		}

		svc := &service{
			adminClients: map[string]*sio.Client{},
			opts:         Opts{MetricsPort: "0"},
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		svc.startGatewayMonitoringWithLeaderElection(ctx)

		select {
		case <-leCalled:
			// success: LE was reached after creating the clientset
		case <-ctx.Done():
			t.Fatal("LeaderElectionFunc was not called within timeout")
		}
	})

	t.Run("logs error when k8s clientset creation fails", func(t *testing.T) {
		K8sClientset = nil
		k8sutils.InClusterConfigFunc = func() (*rest.Config, error) {
			return nil, errors.New("no in-cluster config")
		}

		svc := &service{
			adminClients: map[string]*sio.Client{},
			opts:         Opts{MetricsPort: "0"},
		}

		// Should not panic even when clientset creation fails.
		assert.NotPanics(t, func() {
			svc.startGatewayMonitoringWithLeaderElection(context.Background())
		})
	})

	t.Run("logs error when LeaderElectionFunc returns error", func(t *testing.T) {
		K8sClientset = fake.NewSimpleClientset()
		k8sutils.LeaderElectionFunc = func(_ *kubernetes.Interface, _ string, _ string, _ func(context.Context)) error {
			return errors.New("injected leader election failure")
		}

		svc := &service{
			adminClients: map[string]*sio.Client{},
			opts:         Opts{MetricsPort: "0"},
		}

		assert.NotPanics(t, func() {
			svc.startGatewayMonitoringWithLeaderElection(context.Background())
		})
	})

	t.Run("runFunc stops monitoring when context is cancelled", func(t *testing.T) {
		K8sClientset = fake.NewSimpleClientset()

		// Capture the runFunc provided to LeaderElectionFunc and invoke it directly
		// so we can test it without needing a real k8s cluster.
		var capturedRunFunc func(context.Context)
		k8sutils.LeaderElectionFunc = func(_ *kubernetes.Interface, _ string, _ string, fn func(context.Context)) error {
			capturedRunFunc = fn
			return nil
		}

		svc := &service{
			adminClients: map[string]*sio.Client{},
			opts: Opts{
				MetricsPort:               "0",
				GatewayMonitoringInterval: 5 * time.Second,
			},
		}
		parentCtx, parentCancel := context.WithCancel(context.Background())
		defer parentCancel()

		svc.startGatewayMonitoringWithLeaderElection(parentCtx)
		assert.NotNil(t, capturedRunFunc, "LeaderElectionFunc should have been called")

		// Pre-start the metrics server so that startGatewayMonitor can proceed.
		svc.startMetricsServer()
		assert.NotNil(t, svc.metricsServer, "metricsServer should be running before runFunc is invoked")

		// Run the captured func with a short-lived context to simulate lease expiry.
		leCtx, leCancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			capturedRunFunc(leCtx)
			close(done)
		}()

		// Give monitoring a moment to start, then cancel the lease context.
		time.Sleep(50 * time.Millisecond)
		leCancel()

		select {
		case <-done:
			// runFunc returned after context cancellation — success.
		case <-time.After(5 * time.Second):
			t.Fatal("runFunc did not return within timeout after context cancellation")
		}

		// The metrics server must still be running after the monitor is stopped.
		assert.NotNil(t, svc.metricsServer, "metricsServer should still be running after lease is released")
		assert.Nil(t, svc.gatewayMonitor, "gatewayMonitor should be nil after lease is released")
	})
}
