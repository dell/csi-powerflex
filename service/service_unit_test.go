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
	"errors"
	"fmt"
	"net"
	"testing"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	siotypes "github.com/dell/goscaleio/types/v1"
	"github.com/stretchr/testify/assert"
)

type mockService struct{}

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
				RequiredBytes: 300 * bytesInKiB,
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

func TestFindNetworkInterfaceIPs(t *testing.T) {
	tests := []struct {
		name            string
		expectedError   error
		client          kubernetes.Interface
		configMapData   map[string]string
		createConfigMap func(map[string]string, kubernetes.Interface)
	}{
		{
			name:          "Error getting K8sClient",
			expectedError: fmt.Errorf("unable to load in-cluster configuration, KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT must be defined"),
			client:        nil,
			configMapData: nil,
			createConfigMap: func(map[string]string, kubernetes.Interface) {
			},
		},
		{
			name: "Error getting ConfigMap",
			expectedError: &k8serrors.StatusError{
				ErrStatus: metav1.Status{
					Status:  metav1.StatusFailure,
					Message: "configmaps \"vxflexos-config-params\" not found",
					Reason:  metav1.StatusReasonNotFound,
					Details: &metav1.StatusDetails{
						Name: "vxflexos-config-params",
						Kind: "configmaps",
					},
					Code: 404,
				},
			},
			client:        fake.NewSimpleClientset(),
			configMapData: nil,
			createConfigMap: func(map[string]string, kubernetes.Interface) {
			},
		},
		{
			name:          "No Error",
			expectedError: nil,
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
					Log.Fatalf("failed to create configMaps: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		s := &service{}
		t.Run(tt.name, func(t *testing.T) {
			K8sClientset = tt.client
			tt.createConfigMap(tt.configMapData, tt.client)
			_, err := s.findNetworkInterfaceIPs()
			assert.Equal(t, err, tt.expectedError)
		})
	}
}
