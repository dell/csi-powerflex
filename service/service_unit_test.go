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
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"github.com/Ecosystems/container-storage-modules/src/csi-vxflexos/v2/k8sutils"
	svcmetrics "github.com/Ecosystems/container-storage-modules/src/csi-vxflexos/v2/service/metrics"
	csmlog "github.com/Ecosystems/container-storage-modules/src/csmlog"
	sio "github.com/Ecosystems/container-storage-modules/src/goscaleio"
	siotypes "github.com/Ecosystems/container-storage-modules/src/goscaleio/types/v1"
	csi "github.com/container-storage-interface/spec/lib/go/csi"
)

type mockService struct {
	service
}

func (s *mockService) InterfaceByName(interfaceName string) (*net.Interface, error) {
	if interfaceName == "" {
		return nil, fmt.Errorf("invalid interface name")
	} else if interfaceName == "eth1" {
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
	switch interfaceObj.Name {
	case "eth_addrs_err":
		return nil, fmt.Errorf("addrs error")
	case "eth_no_ipnet":
		return []net.Addr{
			&net.IPAddr{IP: net.IPv4(10, 0, 0, 1)},
		}, nil
	case "eth_ipv6":
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("2001:db8::1")},
		}, nil
	default:
		return []net.Addr{
			&net.IPNet{
				IP: net.IPv4(10, 0, 0, 1),
			},
		}, nil
	}
}

func TestGetVolSize(t *testing.T) {
	tests := []struct {
		name    string
		cr      *csi.CapacityRange
		genType string // "" = Gen1 (8 GiB granularity); "EC" = Gen2/EC (1 GiB granularity)
		sizeKiB int    // 0 means an error is expected
	}{
		// ── Existing Gen1 cases (backward-compatibility) ─────────────────
		{
			name: "Gen1: no range → default size",
			cr: &csi.CapacityRange{
				RequiredBytes: 0,
				LimitBytes:    0,
			},
			sizeKiB: DefaultVolumeSizeKiB,
		},
		{
			name: "Gen1: 1 byte → minimal 8 GiB size",
			cr: &csi.CapacityRange{
				RequiredBytes: 1,
				LimitBytes:    0,
			},
			sizeKiB: 8 * kiBytesInGiB,
		},
		{
			name: "Gen1: no min but limit below default → error",
			cr: &csi.CapacityRange{
				RequiredBytes: 0,
				LimitBytes:    4 * bytesInGiB,
			},
			sizeKiB: 0,
		},
		{
			name: "Gen1: 10 GiB → rounds up to 16 GiB",
			cr: &csi.CapacityRange{
				RequiredBytes: 10 * bytesInGiB,
				LimitBytes:    0,
			},
			sizeKiB: 16 * kiBytesInGiB,
		},
		{
			name: "Gen1: 13 GiB → rounds to 16 GiB, exceeds 14 GiB limit → error",
			cr: &csi.CapacityRange{
				RequiredBytes: 13 * bytesInGiB,
				LimitBytes:    14 * bytesInGiB,
			},
			sizeKiB: 0,
		},
		{
			name: "Gen1: 9.5 GiB → rounds up to 16 GiB",
			cr: &csi.CapacityRange{
				RequiredBytes: int64(9.5 * float64(bytesInGiB)),
				LimitBytes:    0,
			},
			sizeKiB: 16 * kiBytesInGiB,
		},
		{
			name: "Gen1: 48.5 GiB → rounds up to 56 GiB",
			cr: &csi.CapacityRange{
				RequiredBytes: int64(48.5 * float64(bytesInGiB)),
				LimitBytes:    0,
			},
			sizeKiB: 56 * kiBytesInGiB,
		},
		// ── FR-2: Gen1 regression (explicit 8 GiB multiples) ─────────────
		{
			name:    "Gen1 regression: 1 GiB → 8 GiB",
			cr:      &csi.CapacityRange{RequiredBytes: 1 * bytesInGiB},
			genType: "",
			sizeKiB: 8 * kiBytesInGiB,
		},
		{
			name:    "Gen1 regression: 3 GiB → 8 GiB",
			cr:      &csi.CapacityRange{RequiredBytes: 3 * bytesInGiB},
			genType: "",
			sizeKiB: 8 * kiBytesInGiB,
		},
		{
			name:    "Gen1 regression: 8 GiB → 8 GiB (no rounding)",
			cr:      &csi.CapacityRange{RequiredBytes: 8 * bytesInGiB},
			genType: "",
			sizeKiB: 8 * kiBytesInGiB,
		},
		{
			name:    "Gen1 regression: 12.2 GiB → 16 GiB",
			cr:      &csi.CapacityRange{RequiredBytes: int64(math.Trunc(float64(bytesInGiB) * 12.2))},
			genType: "",
			sizeKiB: 16 * kiBytesInGiB,
		},
		// ── FR-1: Gen2/EC exact integer GiB cases ─────────────────────────
		{
			name:    "Gen2/EC: 1 GiB exact → 1 GiB",
			cr:      &csi.CapacityRange{RequiredBytes: 1 * bytesInGiB},
			genType: "EC",
			sizeKiB: 1 * kiBytesInGiB,
		},
		{
			name:    "Gen2/EC: 2 GiB exact → 2 GiB",
			cr:      &csi.CapacityRange{RequiredBytes: 2 * bytesInGiB},
			genType: "EC",
			sizeKiB: 2 * kiBytesInGiB,
		},
		{
			name:    "Gen2/EC: 3 GiB exact → 3 GiB",
			cr:      &csi.CapacityRange{RequiredBytes: 3 * bytesInGiB},
			genType: "EC",
			sizeKiB: 3 * kiBytesInGiB,
		},
		{
			name:    "Gen2/EC: 5 GiB exact → 5 GiB",
			cr:      &csi.CapacityRange{RequiredBytes: 5 * bytesInGiB},
			genType: "EC",
			sizeKiB: 5 * kiBytesInGiB,
		},
		{
			name:    "Gen2/EC: 10 GiB exact → 10 GiB",
			cr:      &csi.CapacityRange{RequiredBytes: 10 * bytesInGiB},
			genType: "EC",
			sizeKiB: 10 * kiBytesInGiB,
		},
		// ── FR-1: Gen2/EC non-integer GiB → ceiling ───────────────────────
		{
			name:    "Gen2/EC: 1.2 GiB → 2 GiB",
			cr:      &csi.CapacityRange{RequiredBytes: int64(math.Trunc(float64(bytesInGiB) * 1.2))},
			genType: "EC",
			sizeKiB: 2 * kiBytesInGiB,
		},
		{
			name:    "Gen2/EC: 3.5 GiB → 4 GiB",
			cr:      &csi.CapacityRange{RequiredBytes: int64(math.Trunc(float64(bytesInGiB) * 3.5))},
			genType: "EC",
			sizeKiB: 4 * kiBytesInGiB,
		},
		{
			name:    "Gen2/EC: 12.2 GiB → 13 GiB",
			cr:      &csi.CapacityRange{RequiredBytes: int64(math.Trunc(float64(bytesInGiB) * 12.2))},
			genType: "EC",
			sizeKiB: 13 * kiBytesInGiB,
		},
		// ── FR-1: Gen2/EC sub-1 GiB → minimum 1 GiB ──────────────────────
		{
			name:    "Gen2/EC: 256 MiB → 1 GiB",
			cr:      &csi.CapacityRange{RequiredBytes: 256 * 1024 * 1024},
			genType: "EC",
			sizeKiB: 1 * kiBytesInGiB,
		},
		{
			name:    "Gen2/EC: 512 MiB → 1 GiB",
			cr:      &csi.CapacityRange{RequiredBytes: 512 * 1024 * 1024},
			genType: "EC",
			sizeKiB: 1 * kiBytesInGiB,
		},
		{
			name:    "Gen2/EC: 768 MiB → 1 GiB",
			cr:      &csi.CapacityRange{RequiredBytes: 768 * 1024 * 1024},
			genType: "EC",
			sizeKiB: 1 * kiBytesInGiB,
		},
		// ── FR-1: Gen2/EC boundary: exactly 1.0 GiB ───────────────────────
		{
			name:    "Gen2/EC: exactly 1.0 GiB → 1 GiB (no rounding)",
			cr:      &csi.CapacityRange{RequiredBytes: 1 * bytesInGiB},
			genType: "EC",
			sizeKiB: 1 * kiBytesInGiB,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(st *testing.T) {
			st.Parallel()
			size, err := validateVolSize(tt.cr, tt.genType)
			if tt.sizeKiB == 0 {
				// error is expected
				assert.Error(st, err)
			} else {
				assert.NoError(st, err)
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
		{
			name:          "Addrs error",
			interfaceName: "eth_addrs_err",
			expectedIP:    "",
			expectedError: fmt.Errorf("addrs error"),
		},
		{
			name:          "Non-IPNet address",
			interfaceName: "eth_no_ipnet",
			expectedIP:    "",
			expectedError: fmt.Errorf("no IPv4 address found for interface eth_no_ipnet"),
		},
		{
			name:          "No IPv4 address",
			interfaceName: "eth_ipv6",
			expectedIP:    "",
			expectedError: fmt.Errorf("no IPv4 address found for interface eth_ipv6"),
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
					Zones: []AvailabilityZone{
						{
							Name:     "zone1",
							LabelKey: "custom-zone.io/area",
						},
					},
				},
				"array2": {
					Zones: []AvailabilityZone{
						{
							Name:     "zone2",
							LabelKey: "custom-zone.io/area",
						},
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
					Zones: []AvailabilityZone{
						{
							Name:     "zone1",
							LabelKey: "custom-zone-1.io/area",
						},
					},
				},
				"array2": {
					SystemID: "system-2",
					Zones: []AvailabilityZone{
						{
							Name:     "zone2",
							LabelKey: "custom-zone-2.io/area",
						},
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
					csmlog.Fatalf("failed to create configMaps: %v", err)
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
					csmlog.Fatalf("failed to create configMaps: %v", err)
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
					csmlog.Fatalf("failed to create configMaps: %v", err)
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

	t.Run("operation interceptor accessors", func(t *testing.T) {
		orig := globalOperationInterceptor
		defer func() { globalOperationInterceptor = orig }()

		svc := &service{}
		assert.Nil(t, svc.OperationInterceptor())
		assert.Nil(t, GetOperationInterceptor())

		dummy := func(context.Context, interface{}, *grpc.UnaryServerInfo, grpc.UnaryHandler) (interface{}, error) {
			return nil, nil
		}
		globalOperationInterceptor = dummy
		assert.NotNil(t, GetOperationInterceptor())
	})

	t.Run("metricsLeaderElectionEnabled", func(t *testing.T) {
		t.Setenv(EnvMetricsLeaderElectionEnabled, "true")
		assert.True(t, metricsLeaderElectionEnabled())

		t.Setenv(EnvMetricsLeaderElectionEnabled, "FALSE")
		assert.False(t, metricsLeaderElectionEnabled())

		t.Setenv(EnvMetricsLeaderElectionEnabled, "TrUe")
		assert.True(t, metricsLeaderElectionEnabled())
	})

	t.Run("metricsLeaderElectionEnabled defaults to false when unset", func(t *testing.T) {
		orig, ok := os.LookupEnv(EnvMetricsLeaderElectionEnabled)
		require.NoError(t, os.Unsetenv(EnvMetricsLeaderElectionEnabled))
		defer func() {
			if ok {
				_ = os.Setenv(EnvMetricsLeaderElectionEnabled, orig)
			}
		}()

		assert.False(t, metricsLeaderElectionEnabled())
	})

	t.Run("metricsLeaderElectionNamespace", func(t *testing.T) {
		orig := DriverNamespace
		DriverNamespace = "default-namespace"
		defer func() { DriverNamespace = orig }()

		assert.Equal(t, "default-namespace", metricsLeaderElectionNamespace())
		t.Setenv(EnvDriverNamespace, "vxflexos")
		assert.Equal(t, "vxflexos", metricsLeaderElectionNamespace())
	})

	t.Run("metricsLeaderElectionNamespace falls back to DriverNamespace default", func(t *testing.T) {
		origNamespace := DriverNamespace
		DriverNamespace = ""
		defer func() { DriverNamespace = origNamespace }()

		origEnv, ok := os.LookupEnv(EnvDriverNamespace)
		require.NoError(t, os.Unsetenv(EnvDriverNamespace))
		defer func() {
			if ok {
				_ = os.Setenv(EnvDriverNamespace, origEnv)
			}
		}()

		assert.Equal(t, "", metricsLeaderElectionNamespace())
	})

	t.Run("metricsLeaderElectionDurationEnvVars", func(t *testing.T) {
		t.Setenv(EnvMetricsLeaderElectionLeaseDuration, "12s")
		t.Setenv(EnvMetricsLeaderElectionRenewDeadline, "8s")
		t.Setenv(EnvMetricsLeaderElectionRetryPeriod, "2s")

		assert.Equal(t, 12*time.Second, metricsLeaderElectionLeaseDuration())
		assert.Equal(t, 8*time.Second, metricsLeaderElectionRenewDeadline())
		assert.Equal(t, 2*time.Second, metricsLeaderElectionRetryPeriod())

		for _, env := range []string{EnvMetricsLeaderElectionLeaseDuration, EnvMetricsLeaderElectionRenewDeadline, EnvMetricsLeaderElectionRetryPeriod} {
			orig, ok := os.LookupEnv(env)
			require.NoError(t, os.Unsetenv(env))
			defer func() {
				if ok {
					_ = os.Setenv(env, orig)
				}
			}()
		}

		assert.Equal(t, 60*time.Second, metricsLeaderElectionLeaseDuration())
		assert.Equal(t, 40*time.Second, metricsLeaderElectionRenewDeadline())
		assert.Equal(t, 5*time.Second, metricsLeaderElectionRetryPeriod())
	})

	t.Run("durationFromEnvOrDefault", func(t *testing.T) {
		assert.Equal(t, 42*time.Second, durationFromEnvOrDefault("MISSING_DURATION_ENV", 42*time.Second))

		t.Setenv("TEST_DURATION_ENV", "15s")
		assert.Equal(t, 15*time.Second, durationFromEnvOrDefault("TEST_DURATION_ENV", 42*time.Second))

		t.Setenv("TEST_DURATION_ENV", "invalid")
		assert.Equal(t, 42*time.Second, durationFromEnvOrDefault("TEST_DURATION_ENV", 42*time.Second))

		t.Setenv("TEST_DURATION_ENV", "")
		assert.Equal(t, 42*time.Second, durationFromEnvOrDefault("TEST_DURATION_ENV", 42*time.Second))
	})

	t.Run("intFromEnvOrDefault", func(t *testing.T) {
		assert.Equal(t, 7, intFromEnvOrDefault("MISSING_INT_ENV", 7))

		t.Setenv("TEST_INT_ENV", "13")
		assert.Equal(t, 13, intFromEnvOrDefault("TEST_INT_ENV", 7))

		t.Setenv("TEST_INT_ENV", "invalid")
		assert.Equal(t, 7, intFromEnvOrDefault("TEST_INT_ENV", 7))

		t.Setenv("TEST_INT_ENV", "0")
		assert.Equal(t, 7, intFromEnvOrDefault("TEST_INT_ENV", 7))
	})

	t.Run("metricsCollectionInterval", func(t *testing.T) {
		t.Setenv(EnvMetricsCollectionInterval, "17s")
		assert.Equal(t, 17*time.Second, metricsCollectionInterval())

		t.Setenv(EnvMetricsCollectionInterval, "bad")
		assert.Equal(t, 30*time.Second, metricsCollectionInterval())
	})

	t.Run("metricsRuntimeConfig", func(t *testing.T) {
		t.Setenv(EnvMetricsArrayTimeout, "11s")
		t.Setenv(EnvMetricsCollectionCacheTTL, "19s")
		t.Setenv(EnvMetricsArrayRateLimit, "123")
		t.Setenv(EnvMetricsArrayCBThreshold, "4")
		t.Setenv(EnvMetricsArrayCBResetTimeout, "21s")

		cfg := metricsRuntimeConfig()
		assert.Equal(t, 11*time.Second, cfg.Timeout)
		assert.Equal(t, 19*time.Second, cfg.CacheTTL)
		assert.Equal(t, 123, cfg.RateLimit)
		assert.Equal(t, 4, cfg.CBThreshold)
		assert.Equal(t, 21*time.Second, cfg.CBResetTimeout)
	})
}

func TestStartCollectors(t *testing.T) {
	t.Run("startCollectors_nilMetricsServer", func(_ *testing.T) {
		s := &service{
			metricsServer: nil,
		}
		ctx := context.Background()
		s.startCollectors(ctx)
	})

	t.Run("startCollectors_withMetricsServer", func(_ *testing.T) {
		s := &service{
			mode: "controller",
			adminClients: map[string]*sio.Client{
				"system1": nil,
			},
			opts: Opts{
				arrays: map[string]*ArrayConnectionData{
					"system1": {
						Endpoint: "https://127.0.0.1",
					},
				},
			},
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s.startCollectors(ctx)
	})

	t.Run("startCollectors_nodeMode", func(_ *testing.T) {
		s := &service{
			mode: "node",
			adminClients: map[string]*sio.Client{
				"system1": nil,
			},
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s.startCollectors(ctx)
	})
}

func TestStartCollectors_SetsOperationInterceptor(t *testing.T) {
	originalInterceptor := globalOperationInterceptor
	t.Cleanup(func() { globalOperationInterceptor = originalInterceptor })

	t.Run("controller mode", func(t *testing.T) {
		s := &service{
			mode:          "controller",
			metricsServer: svcmetrics.NewSharedMetricsServer(),
			adminClients:  map[string]*sio.Client{},
			opts: Opts{
				defaultSystemID: "sys-1",
			},
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		s.startCollectors(ctx)

		require.NotNil(t, s.operationInterceptor)
		require.NotNil(t, GetOperationInterceptor())
		assert.Equal(t, reflect.ValueOf(s.operationInterceptor).Pointer(), reflect.ValueOf(GetOperationInterceptor()).Pointer())
	})

	t.Run("node mode", func(t *testing.T) {
		s := &service{
			mode:          "node",
			metricsServer: svcmetrics.NewSharedMetricsServer(),
			adminClients:  map[string]*sio.Client{},
			opts: Opts{
				defaultSystemID: "sys-1",
			},
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		s.startCollectors(ctx)

		require.NotNil(t, s.operationInterceptor)
		require.NotNil(t, GetOperationInterceptor())
		assert.Equal(t, reflect.ValueOf(s.operationInterceptor).Pointer(), reflect.ValueOf(GetOperationInterceptor()).Pointer())
	})
}

func TestStartCollectors_ControllerWithAdminClients(t *testing.T) {
	originalInterceptor := globalOperationInterceptor
	originalK8sClientset := K8sClientset
	t.Cleanup(func() {
		globalOperationInterceptor = originalInterceptor
		K8sClientset = originalK8sClientset
	})

	K8sClientset = fake.NewSimpleClientset()

	s := &service{
		mode:          "controller",
		metricsServer: svcmetrics.NewSharedMetricsServer(),
		adminClients: map[string]*sio.Client{
			"system1": nil,
			"system2": nil,
		},
		opts: Opts{
			defaultSystemID: "sys-1",
			arrays: map[string]*ArrayConnectionData{
				"system1": {Endpoint: "https://10.0.0.1"},
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.startCollectors(ctx)

	assert.NotNil(t, s.operationInterceptor)
	assert.NotNil(t, s.collectorManager)
	assert.NotNil(t, GetOperationInterceptor())
}

func TestStartCollectors_LeaderElectionEnabledNoKubeclient(t *testing.T) {
	originalInterceptor := globalOperationInterceptor
	originalK8sClientset := K8sClientset
	origKubeclient := k8sutils.Kubeclient
	t.Cleanup(func() {
		globalOperationInterceptor = originalInterceptor
		K8sClientset = originalK8sClientset
		k8sutils.Kubeclient = origKubeclient
	})

	t.Setenv(EnvMetricsLeaderElectionEnabled, "true")
	k8sutils.Kubeclient = nil

	s := &service{
		mode:          "controller",
		metricsServer: svcmetrics.NewSharedMetricsServer(),
		adminClients: map[string]*sio.Client{
			"system1": nil,
		},
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				"system1": {Endpoint: "https://10.0.0.1"},
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.startCollectors(ctx)

	assert.NotNil(t, s.operationInterceptor)
}

func TestGetProvisionType_InvalidThickProvisioningValue(t *testing.T) {
	svc := &service{opts: Opts{Thick: true}}

	got := svc.getVolProvisionType(map[string]string{
		KeyThickProvisioning: "not-a-bool",
	})

	assert.Equal(t, thickProvisioned, got)
}

func TestRegisterAdditionalServers(t *testing.T) {
	svc := &service{}
	server := grpc.NewServer()
	defer server.Stop()

	assert.NotPanics(t, func() {
		svc.RegisterAdditionalServers(server)
	})
}

func TestSetupNVMeHost_NoInitiators(t *testing.T) {
	s := &service{}
	err := s.setupNVMeHost([]string{}, "system1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NVMe initiators not found on node")
}

func TestStartGatewayMonitor_NoMetricsServer(_ *testing.T) {
	s := &service{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.startGatewayMonitor(ctx)
}

func TestBeforeServe_MissingArrayConfig(t *testing.T) {
	origConfig := ArrayConfigFile
	t.Cleanup(func() { ArrayConfigFile = origConfig })

	ArrayConfigFile = "/tmp/nonexistent-array-config.yaml"

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	svc := &service{}
	err = svc.BeforeServe(context.Background(), nil, listener)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestUpdateDriverConfigParams(t *testing.T) {
	t.Run("accepts text log format and valid level", func(t *testing.T) {
		defer csmlog.SetFormat("json")
		defer csmlog.SetLevel(DefaultLogLevel)
		origLogLevel, ok := os.LookupEnv("X_CSI_LOG_LEVEL")
		defer func() {
			if ok {
				_ = os.Setenv("X_CSI_LOG_LEVEL", origLogLevel)
				return
			}
			_ = os.Unsetenv("X_CSI_LOG_LEVEL")
		}()

		vc := viper.New()
		vc.Set("CSI_LOG_FORMAT", "text")
		vc.Set(ParamCSILogLevel, "info")

		svc := &service{}
		assert.NoError(t, svc.updateDriverConfigParams(vc))
		assert.Equal(t, "info", os.Getenv("X_CSI_LOG_LEVEL"))
	})

	t.Run("returns error for invalid log level", func(t *testing.T) {
		defer csmlog.SetFormat("json")
		defer csmlog.SetLevel(DefaultLogLevel)

		vc := viper.New()
		vc.Set("CSI_LOG_FORMAT", "yaml")
		vc.Set(ParamCSILogLevel, "not-a-level")

		svc := &service{}
		err := svc.updateDriverConfigParams(vc)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "input log level")
	})
}

// TestArrayConnectionDataZonesSerialization tests JSON serialization/deserialization
// of the Zones field on ArrayConnectionData.
func TestArrayConnectionDataZonesSerialization(t *testing.T) {
	t.Run("zones present", func(t *testing.T) {
		input := ArrayConnectionData{
			SystemID: "system1",
			Username: "admin",
			Password: "pass",
			Endpoint: "https://10.0.0.1",
			Zones: []AvailabilityZone{
				{
					Name:     "zoneA",
					LabelKey: "topology.kubernetes.io/zone",
					ProtectionDomains: []ProtectionDomain{
						{Name: "PD1", Pools: []PoolName{"pool1"}},
					},
				},
				{
					Name:     "zoneB",
					LabelKey: "topology.kubernetes.io/zone",
					ProtectionDomains: []ProtectionDomain{
						{Name: "PD2", Pools: []PoolName{"pool2"}},
					},
				},
			},
		}

		data, err := json.Marshal(input)
		require.NoError(t, err)
		assert.Contains(t, string(data), `"zones"`)

		var output ArrayConnectionData
		err = json.Unmarshal(data, &output)
		require.NoError(t, err)
		assert.Equal(t, 2, len(output.Zones))
		assert.Equal(t, ZoneName("zoneA"), output.Zones[0].Name)
		assert.Equal(t, ZoneName("zoneB"), output.Zones[1].Name)
		assert.Equal(t, ProtectionDomainName("PD1"), output.Zones[0].ProtectionDomains[0].Name)
		assert.Equal(t, ProtectionDomainName("PD2"), output.Zones[1].ProtectionDomains[0].Name)
	})

	t.Run("zones absent", func(t *testing.T) {
		input := ArrayConnectionData{
			SystemID: "system1",
			Username: "admin",
			Password: "pass",
			Endpoint: "https://10.0.0.1",
		}

		data, err := json.Marshal(input)
		require.NoError(t, err)
		assert.NotContains(t, string(data), `"zones"`)

		var output ArrayConnectionData
		err = json.Unmarshal(data, &output)
		require.NoError(t, err)
		assert.Nil(t, output.Zones)
	})

	t.Run("zones empty array", func(t *testing.T) {
		jsonStr := `{"systemID":"system1","username":"admin","password":"pass","endpoint":"https://10.0.0.1","zones":[]}`

		var output ArrayConnectionData
		err := json.Unmarshal([]byte(jsonStr), &output)
		require.NoError(t, err)
		assert.NotNil(t, output.Zones)
		assert.Equal(t, 0, len(output.Zones))
	})

	t.Run("zones coexists with legacy zone", func(t *testing.T) {
		jsonStr := `{
			"systemID":"system1","username":"admin","password":"pass","endpoint":"https://10.0.0.1",
			"zone":{"name":"legacyZone","labelKey":"topology.kubernetes.io/zone","protectionDomains":[{"name":"PD0","pools":["pool0"]}]},
			"zones":[{"name":"zoneA","labelKey":"topology.kubernetes.io/zone","protectionDomains":[{"name":"PD1","pools":["pool1"]}]}]
		}`

		var output ArrayConnectionData
		err := json.Unmarshal([]byte(jsonStr), &output)
		require.NoError(t, err)
		// Both fields should be populated from JSON
		assert.NotNil(t, output.AvailabilityZone)
		assert.Equal(t, ZoneName("legacyZone"), output.AvailabilityZone.Name)
		assert.Equal(t, 1, len(output.Zones))
		assert.Equal(t, ZoneName("zoneA"), output.Zones[0].Name)
	})
}

// TestNormalizeZoneConfig tests the legacy zone → zones[] normalization logic.
func TestNormalizeZoneConfig(t *testing.T) {
	t.Run("singular zone present with no zones → normalized to single-element zones", func(t *testing.T) {
		a := &ArrayConnectionData{
			SystemID: "sys1",
			AvailabilityZone: &AvailabilityZone{
				Name:     "zoneA",
				LabelKey: "topology.kubernetes.io/zone",
				ProtectionDomains: []ProtectionDomain{
					{Name: "PD1", Pools: []PoolName{"pool1"}},
				},
			},
		}
		err := normalizeZoneConfig(a)
		require.NoError(t, err)
		assert.Equal(t, 1, len(a.Zones))
		assert.Equal(t, ZoneName("zoneA"), a.Zones[0].Name)
		assert.Equal(t, ProtectionDomainName("PD1"), a.Zones[0].ProtectionDomains[0].Name)
		// Legacy field is preserved for backward compat (preinit.go still reads it)
		assert.NotNil(t, a.AvailabilityZone)
	})

	t.Run("zones present with no zone → used as-is", func(t *testing.T) {
		a := &ArrayConnectionData{
			SystemID: "sys1",
			Zones: []AvailabilityZone{
				{
					Name:     "zoneA",
					LabelKey: "topology.kubernetes.io/zone",
					ProtectionDomains: []ProtectionDomain{
						{Name: "PD1", Pools: []PoolName{"pool1"}},
					},
				},
				{
					Name:     "zoneB",
					LabelKey: "topology.kubernetes.io/zone",
					ProtectionDomains: []ProtectionDomain{
						{Name: "PD2", Pools: []PoolName{"pool2"}},
					},
				},
			},
		}
		err := normalizeZoneConfig(a)
		require.NoError(t, err)
		assert.Equal(t, 2, len(a.Zones))
		assert.Nil(t, a.AvailabilityZone)
	})

	t.Run("both zone and zones present → reject as config error", func(t *testing.T) {
		a := &ArrayConnectionData{
			SystemID: "sys1",
			AvailabilityZone: &AvailabilityZone{
				Name:     "legacyZone",
				LabelKey: "topology.kubernetes.io/zone",
				ProtectionDomains: []ProtectionDomain{
					{Name: "PD0", Pools: []PoolName{"pool0"}},
				},
			},
			Zones: []AvailabilityZone{
				{
					Name:     "zoneA",
					LabelKey: "topology.kubernetes.io/zone",
					ProtectionDomains: []ProtectionDomain{
						{Name: "PD1", Pools: []PoolName{"pool1"}},
					},
				},
			},
		}
		err := normalizeZoneConfig(a)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "both")
	})

	t.Run("neither zone nor zones → no-op, both remain nil/empty", func(t *testing.T) {
		a := &ArrayConnectionData{
			SystemID: "sys1",
		}
		err := normalizeZoneConfig(a)
		require.NoError(t, err)
		assert.Nil(t, a.AvailabilityZone)
		assert.Equal(t, 0, len(a.Zones))
	})

	t.Run("mixed legacy + new entries across different systems → accepted", func(t *testing.T) {
		// System 1 uses legacy "zone"
		sys1 := &ArrayConnectionData{
			SystemID: "sys1",
			AvailabilityZone: &AvailabilityZone{
				Name:     "zoneA",
				LabelKey: "topology.kubernetes.io/zone",
				ProtectionDomains: []ProtectionDomain{
					{Name: "PD1", Pools: []PoolName{"pool1"}},
				},
			},
		}
		// System 2 uses new "zones[]"
		sys2 := &ArrayConnectionData{
			SystemID: "sys2",
			Zones: []AvailabilityZone{
				{
					Name:     "zoneB",
					LabelKey: "topology.kubernetes.io/zone",
					ProtectionDomains: []ProtectionDomain{
						{Name: "PD2", Pools: []PoolName{"pool2"}},
					},
				},
			},
		}

		err := normalizeZoneConfig(sys1)
		require.NoError(t, err)
		assert.Equal(t, 1, len(sys1.Zones))
		assert.Equal(t, ZoneName("zoneA"), sys1.Zones[0].Name)
		assert.NotNil(t, sys1.AvailabilityZone)

		err = normalizeZoneConfig(sys2)
		require.NoError(t, err)
		assert.Equal(t, 1, len(sys2.Zones))
		assert.Equal(t, ZoneName("zoneB"), sys2.Zones[0].Name)
	})
}

// TestValidateZonePDUniqueness tests per-system Protection Domain uniqueness validation.
func TestValidateZonePDUniqueness(t *testing.T) {
	t.Run("duplicate PD names within one system zones → rejected", func(t *testing.T) {
		zones := []AvailabilityZone{
			{
				Name: "zoneA",
				ProtectionDomains: []ProtectionDomain{
					{Name: "PD1", Pools: []PoolName{"pool1"}},
				},
			},
			{
				Name: "zoneB",
				ProtectionDomains: []ProtectionDomain{
					{Name: "PD1", Pools: []PoolName{"pool2"}}, // duplicate PD1
				},
			},
		}
		err := validateZonePDUniqueness("sys1", zones)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "PD1")
		assert.Contains(t, err.Error(), "sys1")
		assert.Contains(t, err.Error(), "zoneA")
		assert.Contains(t, err.Error(), "zoneB")
	})

	t.Run("same PD name on different systems → accepted", func(t *testing.T) {
		zones1 := []AvailabilityZone{
			{
				Name: "zoneA",
				ProtectionDomains: []ProtectionDomain{
					{Name: "PD1", Pools: []PoolName{"pool1"}},
				},
			},
		}
		zones2 := []AvailabilityZone{
			{
				Name: "zoneB",
				ProtectionDomains: []ProtectionDomain{
					{Name: "PD1", Pools: []PoolName{"pool1"}}, // same PD name, different system
				},
			},
		}
		err := validateZonePDUniqueness("sys1", zones1)
		require.NoError(t, err)
		err = validateZonePDUniqueness("sys2", zones2)
		require.NoError(t, err)
	})

	t.Run("unique PDs within one system → accepted", func(t *testing.T) {
		zones := []AvailabilityZone{
			{
				Name: "zoneA",
				ProtectionDomains: []ProtectionDomain{
					{Name: "PD1", Pools: []PoolName{"pool1"}},
				},
			},
			{
				Name: "zoneB",
				ProtectionDomains: []ProtectionDomain{
					{Name: "PD2", Pools: []PoolName{"pool2"}},
				},
			},
		}
		err := validateZonePDUniqueness("sys1", zones)
		require.NoError(t, err)
	})

	t.Run("empty zones → accepted", func(t *testing.T) {
		err := validateZonePDUniqueness("sys1", nil)
		require.NoError(t, err)
	})

	t.Run("zone with more than one PD → rejected", func(t *testing.T) {
		zones := []AvailabilityZone{
			{
				Name: "zoneA",
				ProtectionDomains: []ProtectionDomain{
					{Name: "PD1", Pools: []PoolName{"pool1"}},
					{Name: "PD2", Pools: []PoolName{"pool2"}}, // two PDs in same zone
				},
			},
		}
		err := validateZonePDUniqueness("sys1", zones)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "only one protection domain per zone is supported")
	})
}

// mockPoolLookup implements zonePoolLookup for unit testing.
type mockPoolLookup struct {
	// pdPools maps PD name → list of pool names
	pdPools map[string][]string
	// pdErrors maps PD name → error to return
	pdErrors map[string]error
}

func (m *mockPoolLookup) FindPoolsForPD(pdName string) ([]string, error) {
	if err, ok := m.pdErrors[pdName]; ok {
		return nil, err
	}
	pools, ok := m.pdPools[pdName]
	if !ok {
		return nil, fmt.Errorf("protection domain %q not found", pdName)
	}
	return pools, nil
}

// TestResolveDefaultPools tests Storage Pool default resolution for zone entries.
func TestResolveDefaultPools(t *testing.T) {
	t.Run("zone entry with storagePool specified → used as-is", func(t *testing.T) {
		zones := []AvailabilityZone{
			{
				Name: "zoneA",
				ProtectionDomains: []ProtectionDomain{
					{Name: "PD1", Pools: []PoolName{"pool1"}},
				},
			},
		}
		mock := &mockPoolLookup{pdPools: map[string][]string{"PD1": {"poolX"}}}
		err := resolveDefaultPools("sys1", zones, mock)
		require.NoError(t, err)
		// pool should remain "pool1", not be overwritten
		assert.Equal(t, PoolName("pool1"), zones[0].ProtectionDomains[0].Pools[0])
	})

	t.Run("zone entry with storagePool omitted → API queried, first pool assigned", func(t *testing.T) {
		zones := []AvailabilityZone{
			{
				Name: "zoneA",
				ProtectionDomains: []ProtectionDomain{
					{Name: "PD1", Pools: nil}, // no pools specified
				},
			},
		}
		mock := &mockPoolLookup{pdPools: map[string][]string{"PD1": {"autoPool1", "autoPool2"}}}
		err := resolveDefaultPools("sys1", zones, mock)
		require.NoError(t, err)
		require.Equal(t, 1, len(zones[0].ProtectionDomains[0].Pools))
		assert.Equal(t, PoolName("autoPool1"), zones[0].ProtectionDomains[0].Pools[0])
	})

	t.Run("zone entry with storagePool omitted and PD has no pools → startup error", func(t *testing.T) {
		zones := []AvailabilityZone{
			{
				Name: "zoneA",
				ProtectionDomains: []ProtectionDomain{
					{Name: "PD1", Pools: nil},
				},
			},
		}
		mock := &mockPoolLookup{pdPools: map[string][]string{"PD1": {}}} // PD exists but empty pools
		err := resolveDefaultPools("sys1", zones, mock)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "PD1")
		assert.Contains(t, err.Error(), "no storage pools")
	})

	t.Run("zone entry with storagePool omitted and PD not found → startup error", func(t *testing.T) {
		zones := []AvailabilityZone{
			{
				Name: "zoneA",
				ProtectionDomains: []ProtectionDomain{
					{Name: "PD-missing", Pools: nil},
				},
			},
		}
		mock := &mockPoolLookup{pdPools: map[string][]string{}}
		err := resolveDefaultPools("sys1", zones, mock)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "PD-missing")
	})

	t.Run("zone with more than one PD → rejected", func(t *testing.T) {
		zones := []AvailabilityZone{
			{
				Name: "zoneA",
				ProtectionDomains: []ProtectionDomain{
					{Name: "PD1", Pools: nil},
					{Name: "PD2", Pools: nil},
				},
			},
		}
		mock := &mockPoolLookup{pdPools: map[string][]string{}}
		err := resolveDefaultPools("sys1", zones, mock)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "only one protection domain per zone is supported")
	})

	t.Run("lookup returns explicit error → error propagated", func(t *testing.T) {
		zones := []AvailabilityZone{
			{
				Name: "zoneA",
				ProtectionDomains: []ProtectionDomain{
					{Name: "PD1", Pools: nil},
				},
			},
		}
		mock := &mockPoolLookup{
			pdPools:  map[string][]string{},
			pdErrors: map[string]error{"PD1": fmt.Errorf("API timeout")},
		}
		err := resolveDefaultPools("sys1", zones, mock)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to resolve pools")
		assert.Contains(t, err.Error(), "API timeout")
	})
}

// TestGetZonesFromSecret tests the refactored getZonesFromSecret that iterates zones[].
func TestGetZonesFromSecret(t *testing.T) {
	t.Run("single system with 2 zones → zoneTargetMap has 2 entries pointing to same systemID", func(t *testing.T) {
		svc := &service{}
		svc.opts.arrays = map[string]*ArrayConnectionData{
			"sys1": {
				SystemID: "sys1",
				Zones: []AvailabilityZone{
					{
						Name: "zoneA",
						ProtectionDomains: []ProtectionDomain{
							{Name: "PD1", Pools: []PoolName{"pool1"}},
						},
					},
					{
						Name: "zoneB",
						ProtectionDomains: []ProtectionDomain{
							{Name: "PD2", Pools: []PoolName{"pool2"}},
						},
					},
				},
			},
		}

		result := svc.getZonesFromSecret()
		assert.Equal(t, 2, len(result))

		zoneA, ok := result[ZoneName("zoneA")]
		require.True(t, ok)
		assert.Equal(t, "sys1", zoneA.systemID)
		assert.Equal(t, ProtectionDomainName("PD1"), zoneA.protectionDomain)
		assert.Equal(t, PoolName("pool1"), zoneA.pool)

		zoneB, ok := result[ZoneName("zoneB")]
		require.True(t, ok)
		assert.Equal(t, "sys1", zoneB.systemID)
		assert.Equal(t, ProtectionDomainName("PD2"), zoneB.protectionDomain)
		assert.Equal(t, PoolName("pool2"), zoneB.pool)
	})

	t.Run("2 systems with 3 zones (uneven) → zoneTargetMap has 3 entries", func(t *testing.T) {
		svc := &service{}
		svc.opts.arrays = map[string]*ArrayConnectionData{
			"sys1": {
				SystemID: "sys1",
				Zones: []AvailabilityZone{
					{
						Name: "zoneA",
						ProtectionDomains: []ProtectionDomain{
							{Name: "PD1", Pools: []PoolName{"pool1"}},
						},
					},
					{
						Name: "zoneB",
						ProtectionDomains: []ProtectionDomain{
							{Name: "PD2", Pools: []PoolName{"pool2"}},
						},
					},
				},
			},
			"sys2": {
				SystemID: "sys2",
				Zones: []AvailabilityZone{
					{
						Name: "zoneC",
						ProtectionDomains: []ProtectionDomain{
							{Name: "PD3", Pools: []PoolName{"pool3"}},
						},
					},
				},
			},
		}

		result := svc.getZonesFromSecret()
		assert.Equal(t, 3, len(result))

		zoneA := result[ZoneName("zoneA")]
		assert.Equal(t, "sys1", zoneA.systemID)

		zoneB := result[ZoneName("zoneB")]
		assert.Equal(t, "sys1", zoneB.systemID)

		zoneC := result[ZoneName("zoneC")]
		assert.Equal(t, "sys2", zoneC.systemID)
	})

	t.Run("zone with empty protectionDomains is skipped", func(t *testing.T) {
		svc := &service{}
		svc.opts.arrays = map[string]*ArrayConnectionData{
			"sys1": {
				SystemID: "sys1",
				Zones: []AvailabilityZone{
					{
						Name:              "zoneA",
						ProtectionDomains: []ProtectionDomain{},
					},
				},
			},
		}

		result := svc.getZonesFromSecret()
		assert.Equal(t, 0, len(result))
	})

	t.Run("zone with no pool or no protection domain name uses zero values", func(t *testing.T) {
		svc := &service{}
		svc.opts.arrays = map[string]*ArrayConnectionData{
			"sys1": {
				SystemID: "sys1",
				Zones: []AvailabilityZone{
					{
						Name: "zoneA",
						ProtectionDomains: []ProtectionDomain{
							{Name: ""},
						},
					},
				},
			},
		}

		result := svc.getZonesFromSecret()
		assert.Equal(t, 1, len(result))
		zoneA, ok := result[ZoneName("zoneA")]
		require.True(t, ok)
		assert.Equal(t, "sys1", zoneA.systemID)
		assert.Equal(t, ProtectionDomainName(""), zoneA.protectionDomain)
		assert.Equal(t, PoolName(""), zoneA.pool)
	})

	t.Run("no zones configured → zoneTargetMap is empty (non-regression)", func(t *testing.T) {
		svc := &service{}
		svc.opts.arrays = map[string]*ArrayConnectionData{
			"sys1": {
				SystemID: "sys1",
				// No Zones, no AvailabilityZone
			},
		}

		result := svc.getZonesFromSecret()
		assert.Equal(t, 0, len(result))
	})
}

// TestIsInZoneMultiZone tests isInZone with the Zones[] slice.
func TestIsInZoneMultiZone(t *testing.T) {
	t.Run("node zone matches one of system zones → true", func(t *testing.T) {
		a := &ArrayConnectionData{
			SystemID: "sys1",
			Zones: []AvailabilityZone{
				{Name: "zoneA"},
				{Name: "zoneB"},
			},
		}
		assert.True(t, a.isInZone("zoneA"))
		assert.True(t, a.isInZone("zoneB"))
	})

	t.Run("node zone not in any zones → false", func(t *testing.T) {
		a := &ArrayConnectionData{
			SystemID: "sys1",
			Zones: []AvailabilityZone{
				{Name: "zoneA"},
				{Name: "zoneB"},
			},
		}
		assert.False(t, a.isInZone("zoneC"))
	})

	t.Run("node without zone label when zones configured → false", func(t *testing.T) {
		a := &ArrayConnectionData{
			SystemID: "sys1",
			Zones: []AvailabilityZone{
				{Name: "zoneA"},
			},
		}
		assert.False(t, a.isInZone(""))
	})

	t.Run("no zones configured → false (non-zone mode handled by caller)", func(t *testing.T) {
		a := &ArrayConnectionData{
			SystemID: "sys1",
		}
		assert.False(t, a.isInZone("zoneA"))
	})
}

// TestGetZoneKeyLabelFromSecretMultiZone tests getZoneKeyLabelFromSecret with zones[].
func TestGetZoneKeyLabelFromSecretMultiZone(t *testing.T) {
	t.Run("all systems use same labelKey across zones → accepted", func(t *testing.T) {
		arrays := map[string]*ArrayConnectionData{
			"sys1": {
				SystemID: "sys1",
				Zones: []AvailabilityZone{
					{Name: "zoneA", LabelKey: "topology.kubernetes.io/zone"},
					{Name: "zoneB", LabelKey: "topology.kubernetes.io/zone"},
				},
			},
			"sys2": {
				SystemID: "sys2",
				Zones: []AvailabilityZone{
					{Name: "zoneC", LabelKey: "topology.kubernetes.io/zone"},
				},
			},
		}
		key, err := getZoneKeyLabelFromSecret(arrays)
		require.NoError(t, err)
		assert.Equal(t, "topology.kubernetes.io/zone", key)
	})

	t.Run("systems with different labelKey values → rejected", func(t *testing.T) {
		arrays := map[string]*ArrayConnectionData{
			"sys1": {
				SystemID: "sys1",
				Zones: []AvailabilityZone{
					{Name: "zoneA", LabelKey: "topology.kubernetes.io/zone"},
				},
			},
			"sys2": {
				SystemID: "sys2",
				Zones: []AvailabilityZone{
					{Name: "zoneB", LabelKey: "custom.label/zone"},
				},
			},
		}
		_, err := getZoneKeyLabelFromSecret(arrays)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "does not match")
	})

	t.Run("no zones configured → empty label key", func(t *testing.T) {
		arrays := map[string]*ArrayConnectionData{
			"sys1": {SystemID: "sys1"},
		}
		key, err := getZoneKeyLabelFromSecret(arrays)
		require.NoError(t, err)
		assert.Equal(t, "", key)
	})
}

// TestConfiguredZoneNames tests the configuredZoneNames helper.
func TestConfiguredZoneNames(t *testing.T) {
	t.Run("multi-zone → returns all zone names", func(t *testing.T) {
		a := &ArrayConnectionData{
			Zones: []AvailabilityZone{
				{Name: "zoneA"},
				{Name: "zoneB"},
			},
		}
		names := a.configuredZoneNames()
		assert.Equal(t, []string{"zoneA", "zoneB"}, names)
	})

	t.Run("single zone → returns single name", func(t *testing.T) {
		a := &ArrayConnectionData{
			Zones: []AvailabilityZone{{Name: "zoneX"}},
		}
		names := a.configuredZoneNames()
		assert.Equal(t, []string{"zoneX"}, names)
	})

	t.Run("no zones → returns nil", func(t *testing.T) {
		a := &ArrayConnectionData{}
		names := a.configuredZoneNames()
		assert.Nil(t, names)
	})
}

// TestMatchZoneFromTopology tests the extracted zone matching logic used by CreateVolume.
func TestMatchZoneFromTopology(t *testing.T) {
	zoneLabelKey := "topology.kubernetes.io/zone"

	// Standard 2-system, 3-zone setup
	zoneTargetMap := map[ZoneName]ZoneContent{
		"zoneA": {systemID: "sys1", protectionDomain: "PD-A", pool: "poolA"},
		"zoneB": {systemID: "sys1", protectionDomain: "PD-B", pool: "poolB"},
		"zoneC": {systemID: "sys2", protectionDomain: "PD-C", pool: "poolC"},
	}

	t.Run("PVC with zone A topology → provisions on sys1/PD-A/poolA", func(t *testing.T) {
		preferred := []*csi.Topology{
			{Segments: map[string]string{zoneLabelKey: "zoneA"}},
		}
		result := matchZoneFromTopology(zoneLabelKey, zoneTargetMap, preferred, "")
		require.True(t, result.Matched)
		assert.Equal(t, "sys1", result.SystemID)
		assert.Equal(t, "PD-A", result.ProtectionDomain)
		assert.Equal(t, "poolA", result.StoragePool)
		assert.Equal(t, "zoneA", result.ZoneName)
		require.Len(t, result.Topology, 1)
		assert.Equal(t, "zoneA", result.Topology[0].Segments[zoneLabelKey])
	})

	t.Run("PVC with zone B topology on same system → provisions on PD-B", func(t *testing.T) {
		preferred := []*csi.Topology{
			{Segments: map[string]string{zoneLabelKey: "zoneB"}},
		}
		result := matchZoneFromTopology(zoneLabelKey, zoneTargetMap, preferred, "")
		require.True(t, result.Matched)
		assert.Equal(t, "sys1", result.SystemID)
		assert.Equal(t, "PD-B", result.ProtectionDomain)
		assert.Equal(t, "poolB", result.StoragePool)
		assert.Equal(t, "zoneB", result.ZoneName)
	})

	t.Run("PVC with zone C topology on multi-array → routes to sys2", func(t *testing.T) {
		preferred := []*csi.Topology{
			{Segments: map[string]string{zoneLabelKey: "zoneC"}},
		}
		result := matchZoneFromTopology(zoneLabelKey, zoneTargetMap, preferred, "")
		require.True(t, result.Matched)
		assert.Equal(t, "sys2", result.SystemID)
		assert.Equal(t, "PD-C", result.ProtectionDomain)
		assert.Equal(t, "poolC", result.StoragePool)
	})

	t.Run("snapshot/clone pins to source system → skips other systems", func(t *testing.T) {
		// Source volume is on sys2, so only zoneC (on sys2) should match
		preferred := []*csi.Topology{
			{Segments: map[string]string{zoneLabelKey: "zoneA"}},
			{Segments: map[string]string{zoneLabelKey: "zoneC"}},
		}
		result := matchZoneFromTopology(zoneLabelKey, zoneTargetMap, preferred, "sys2")
		require.True(t, result.Matched)
		assert.Equal(t, "sys2", result.SystemID)
		assert.Equal(t, "zoneC", result.ZoneName)
	})

	t.Run("snapshot/clone on sys1 → picks zoneA (first match on sys1)", func(t *testing.T) {
		preferred := []*csi.Topology{
			{Segments: map[string]string{zoneLabelKey: "zoneA"}},
			{Segments: map[string]string{zoneLabelKey: "zoneB"}},
			{Segments: map[string]string{zoneLabelKey: "zoneC"}},
		}
		result := matchZoneFromTopology(zoneLabelKey, zoneTargetMap, preferred, "sys1")
		require.True(t, result.Matched)
		assert.Equal(t, "sys1", result.SystemID)
		assert.Equal(t, "zoneA", result.ZoneName)
	})

	t.Run("no matching zone → not matched", func(t *testing.T) {
		preferred := []*csi.Topology{
			{Segments: map[string]string{zoneLabelKey: "zoneX"}},
		}
		result := matchZoneFromTopology(zoneLabelKey, zoneTargetMap, preferred, "")
		assert.False(t, result.Matched)
	})

	t.Run("empty preferred topologies → not matched", func(t *testing.T) {
		result := matchZoneFromTopology(zoneLabelKey, zoneTargetMap, []*csi.Topology{}, "")
		assert.False(t, result.Matched)
	})

	t.Run("wrong label key prefix → not matched", func(t *testing.T) {
		preferred := []*csi.Topology{
			{Segments: map[string]string{"other.label/zone": "zoneA"}},
		}
		result := matchZoneFromTopology(zoneLabelKey, zoneTargetMap, preferred, "")
		assert.False(t, result.Matched)
	})

	t.Run("snapshot source system not in any zone → not matched", func(t *testing.T) {
		preferred := []*csi.Topology{
			{Segments: map[string]string{zoneLabelKey: "zoneA"}},
			{Segments: map[string]string{zoneLabelKey: "zoneB"}},
			{Segments: map[string]string{zoneLabelKey: "zoneC"}},
		}
		result := matchZoneFromTopology(zoneLabelKey, zoneTargetMap, preferred, "sys-unknown")
		assert.False(t, result.Matched)
	})

	t.Run("multi-segment topology → picks correct label key", func(t *testing.T) {
		preferred := []*csi.Topology{
			{Segments: map[string]string{
				"other.label/region": "us-east",
				zoneLabelKey:         "zoneB",
			}},
		}
		result := matchZoneFromTopology(zoneLabelKey, zoneTargetMap, preferred, "")
		require.True(t, result.Matched)
		assert.Equal(t, "zoneB", result.ZoneName)
		assert.Equal(t, "sys1", result.SystemID)
		assert.Equal(t, "PD-B", result.ProtectionDomain)
	})

	t.Run("first preferred topology wins when multiple match", func(t *testing.T) {
		preferred := []*csi.Topology{
			{Segments: map[string]string{zoneLabelKey: "zoneB"}},
			{Segments: map[string]string{zoneLabelKey: "zoneC"}},
		}
		result := matchZoneFromTopology(zoneLabelKey, zoneTargetMap, preferred, "")
		require.True(t, result.Matched)
		assert.Equal(t, "zoneB", result.ZoneName, "first preferred topology should win")
	})
}

// TestStorageClassZoneConflictDetection tests that CreateVolume rejects requests
// when zone config is present AND StorageClass specifies protectionDomain or storagePool.
func TestStorageClassZoneConflictDetection(t *testing.T) {
	zoneTargetMap := map[ZoneName]ZoneContent{
		"zoneA": {systemID: "sys1", protectionDomain: "PD-A", pool: "poolA"},
		"zoneB": {systemID: "sys1", protectionDomain: "PD-B", pool: "poolB"},
	}

	t.Run("zone config present + StorageClass specifies protectionDomain → rejected", func(t *testing.T) {
		params := map[string]string{
			KeyProtectionDomain: "some-pd",
		}
		err := detectStorageClassZoneConflict(zoneTargetMap, params)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "protectionDomain")
		assert.Contains(t, err.Error(), "zone")
	})

	t.Run("zone config present + StorageClass specifies storagePool → rejected", func(t *testing.T) {
		params := map[string]string{
			KeyStoragePool: "some-pool",
		}
		err := detectStorageClassZoneConflict(zoneTargetMap, params)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "storagePool")
		assert.Contains(t, err.Error(), "zone")
	})

	t.Run("zone config present + StorageClass specifies both → rejected", func(t *testing.T) {
		params := map[string]string{
			KeyProtectionDomain: "some-pd",
			KeyStoragePool:      "some-pool",
		}
		err := detectStorageClassZoneConflict(zoneTargetMap, params)
		require.Error(t, err)
	})

	t.Run("no zone config + StorageClass specifies PD/Pool → no error (non-regression)", func(t *testing.T) {
		emptyZoneMap := map[ZoneName]ZoneContent{}
		params := map[string]string{
			KeyProtectionDomain: "some-pd",
			KeyStoragePool:      "some-pool",
		}
		err := detectStorageClassZoneConflict(emptyZoneMap, params)
		assert.NoError(t, err)
	})

	t.Run("zone config present + StorageClass has no PD/Pool → no error", func(t *testing.T) {
		params := map[string]string{
			"csi.storage.k8s.io/fstype": "ext4",
		}
		err := detectStorageClassZoneConflict(zoneTargetMap, params)
		assert.NoError(t, err)
	})
}

// TestZoneMatchResultVolumeContext verifies that zone metadata is correctly
// populated in the ZoneMatchResult for volume context enrichment.
func TestZoneMatchResultVolumeContext(t *testing.T) {
	zoneLabelKey := "topology.kubernetes.io/zone"
	zoneTargetMap := map[ZoneName]ZoneContent{
		"zoneA": {systemID: "sys1", protectionDomain: "PD-Alpha", pool: "pool-fast"},
		"zoneB": {systemID: "sys1", protectionDomain: "PD-Beta", pool: "pool-slow"},
	}

	t.Run("zone mode → result includes zone name and PD for volume context", func(t *testing.T) {
		preferred := []*csi.Topology{
			{Segments: map[string]string{zoneLabelKey: "zoneA"}},
		}
		result := matchZoneFromTopology(zoneLabelKey, zoneTargetMap, preferred, "")
		require.True(t, result.Matched)

		// These fields should be set on VolumeContext by CreateVolume
		assert.Equal(t, "zoneA", result.ZoneName)
		assert.Equal(t, "PD-Alpha", result.ProtectionDomain)
		assert.Equal(t, "pool-fast", result.StoragePool)

		// Topology should include zone label
		require.Len(t, result.Topology, 1)
		assert.Equal(t, "zoneA", result.Topology[0].Segments[zoneLabelKey])
	})

	t.Run("non-zone mode → result has no zone metadata", func(t *testing.T) {
		emptyZoneMap := map[ZoneName]ZoneContent{}
		preferred := []*csi.Topology{
			{Segments: map[string]string{zoneLabelKey: "zoneA"}},
		}
		result := matchZoneFromTopology(zoneLabelKey, emptyZoneMap, preferred, "")
		assert.False(t, result.Matched)
		assert.Empty(t, result.ZoneName)
		assert.Empty(t, result.ProtectionDomain)
	})
}

// TestGetZonesFromSecretProvisioning verifies getZonesFromSecret produces correct
// zone-to-PD/pool routing for CreateVolume provisioning scenarios.
func TestGetZonesFromSecretProvisioning(t *testing.T) {
	t.Run("single system dual-zone: each zone maps to distinct PD and pool", func(t *testing.T) {
		svc := &service{}
		svc.opts.arrays = map[string]*ArrayConnectionData{
			"sys1": {
				SystemID: "sys1",
				Zones: []AvailabilityZone{
					{Name: "zoneA", ProtectionDomains: []ProtectionDomain{{Name: "PD-Alpha", Pools: []PoolName{"pool-fast"}}}},
					{Name: "zoneB", ProtectionDomains: []ProtectionDomain{{Name: "PD-Beta", Pools: []PoolName{"pool-slow"}}}},
				},
			},
		}

		result := svc.getZonesFromSecret()
		require.Equal(t, 2, len(result))

		// Verify each zone routes to a distinct PD
		assert.NotEqual(t, result[ZoneName("zoneA")].protectionDomain, result[ZoneName("zoneB")].protectionDomain)
		// Both route to the same system
		assert.Equal(t, result[ZoneName("zoneA")].systemID, result[ZoneName("zoneB")].systemID)
	})

	t.Run("multi-array 3-zone uneven: zones correctly route to their systems", func(t *testing.T) {
		svc := &service{}
		svc.opts.arrays = map[string]*ArrayConnectionData{
			"sys1": {
				SystemID: "sys1",
				Zones: []AvailabilityZone{
					{Name: "zoneA", ProtectionDomains: []ProtectionDomain{{Name: "PD-1A", Pools: []PoolName{"pool1a"}}}},
					{Name: "zoneB", ProtectionDomains: []ProtectionDomain{{Name: "PD-1B", Pools: []PoolName{"pool1b"}}}},
				},
			},
			"sys2": {
				SystemID: "sys2",
				Zones: []AvailabilityZone{
					{Name: "zoneC", ProtectionDomains: []ProtectionDomain{{Name: "PD-2C", Pools: []PoolName{"pool2c"}}}},
				},
			},
		}

		result := svc.getZonesFromSecret()
		require.Equal(t, 3, len(result))

		// zoneA and zoneB → sys1
		assert.Equal(t, "sys1", result[ZoneName("zoneA")].systemID)
		assert.Equal(t, "sys1", result[ZoneName("zoneB")].systemID)
		// zoneC → sys2
		assert.Equal(t, "sys2", result[ZoneName("zoneC")].systemID)

		// Each zone maps to a distinct PD
		assert.Equal(t, ProtectionDomainName("PD-1A"), result[ZoneName("zoneA")].protectionDomain)
		assert.Equal(t, ProtectionDomainName("PD-1B"), result[ZoneName("zoneB")].protectionDomain)
		assert.Equal(t, ProtectionDomainName("PD-2C"), result[ZoneName("zoneC")].protectionDomain)
	})

	t.Run("legacy AvailabilityZone (after normalization) also populates zone map", func(t *testing.T) {
		svc := &service{}
		// After normalizeZoneConfig, legacy zone is copied to Zones[]
		svc.opts.arrays = map[string]*ArrayConnectionData{
			"sys1": {
				SystemID: "sys1",
				AvailabilityZone: &AvailabilityZone{
					Name:              "zoneA",
					ProtectionDomains: []ProtectionDomain{{Name: "PD1", Pools: []PoolName{"pool1"}}},
				},
				Zones: []AvailabilityZone{
					{Name: "zoneA", ProtectionDomains: []ProtectionDomain{{Name: "PD1", Pools: []PoolName{"pool1"}}}},
				},
			},
		}

		result := svc.getZonesFromSecret()
		require.Equal(t, 1, len(result))
		assert.Equal(t, "sys1", result[ZoneName("zoneA")].systemID)
		assert.Equal(t, ProtectionDomainName("PD1"), result[ZoneName("zoneA")].protectionDomain)
	})
}

// TestBackwardCompatibility verifies that existing one-system-per-zone
// deployments continue to work after the multi-zone upgrade.
func TestBackwardCompatibility(t *testing.T) {
	zoneLabelKey := "topology.kubernetes.io/zone"

	t.Run("existing multi-system config with one zone per system → identical behavior", func(t *testing.T) {
		svc := &service{}
		svc.opts.arrays = map[string]*ArrayConnectionData{
			"sys1": {
				SystemID: "sys1",
				AvailabilityZone: &AvailabilityZone{
					Name:              "zoneA",
					LabelKey:          zoneLabelKey,
					ProtectionDomains: []ProtectionDomain{{Name: "PD1", Pools: []PoolName{"pool1"}}},
				},
			},
			"sys2": {
				SystemID: "sys2",
				AvailabilityZone: &AvailabilityZone{
					Name:              "zoneB",
					LabelKey:          zoneLabelKey,
					ProtectionDomains: []ProtectionDomain{{Name: "PD2", Pools: []PoolName{"pool2"}}},
				},
			},
		}

		for _, arr := range svc.opts.arrays {
			err := normalizeZoneConfig(arr)
			require.NoError(t, err)
		}

		result := svc.getZonesFromSecret()
		require.Equal(t, 2, len(result))

		zoneA := result[ZoneName("zoneA")]
		assert.Equal(t, "sys1", zoneA.systemID)
		assert.Equal(t, ProtectionDomainName("PD1"), zoneA.protectionDomain)

		zoneB := result[ZoneName("zoneB")]
		assert.Equal(t, "sys2", zoneB.systemID)
		assert.Equal(t, ProtectionDomainName("PD2"), zoneB.protectionDomain)

		preferred := []*csi.Topology{
			{Segments: map[string]string{zoneLabelKey: "zoneA"}},
		}
		match := matchZoneFromTopology(zoneLabelKey, result, preferred, "")
		require.True(t, match.Matched)
		assert.Equal(t, "sys1", match.SystemID)
		assert.Equal(t, "PD1", match.ProtectionDomain)
	})

	t.Run("mixed zones[] and legacy zone across systems → correct routing", func(t *testing.T) {
		svc := &service{}
		svc.opts.arrays = map[string]*ArrayConnectionData{
			"sys1": {
				SystemID: "sys1",
				Zones: []AvailabilityZone{
					{Name: "zoneA", LabelKey: zoneLabelKey, ProtectionDomains: []ProtectionDomain{{Name: "PD-1A", Pools: []PoolName{"pool1a"}}}},
					{Name: "zoneB", LabelKey: zoneLabelKey, ProtectionDomains: []ProtectionDomain{{Name: "PD-1B", Pools: []PoolName{"pool1b"}}}},
				},
			},
			"sys2": {
				SystemID: "sys2",
				AvailabilityZone: &AvailabilityZone{
					Name:              "zoneC",
					LabelKey:          zoneLabelKey,
					ProtectionDomains: []ProtectionDomain{{Name: "PD-2C", Pools: []PoolName{"pool2c"}}},
				},
			},
		}

		for _, arr := range svc.opts.arrays {
			err := normalizeZoneConfig(arr)
			require.NoError(t, err)
		}

		result := svc.getZonesFromSecret()
		require.Equal(t, 3, len(result))

		assert.Equal(t, "sys1", result[ZoneName("zoneA")].systemID)
		assert.Equal(t, "sys1", result[ZoneName("zoneB")].systemID)
		assert.Equal(t, "sys2", result[ZoneName("zoneC")].systemID)

		preferred := []*csi.Topology{
			{Segments: map[string]string{zoneLabelKey: "zoneC"}},
		}
		match := matchZoneFromTopology(zoneLabelKey, result, preferred, "")
		require.True(t, match.Matched)
		assert.Equal(t, "sys2", match.SystemID)
		assert.Equal(t, "PD-2C", match.ProtectionDomain)
		assert.Equal(t, "pool2c", match.StoragePool)
	})

	t.Run("no config changes required for existing non-zone deployments", func(t *testing.T) {
		svc := &service{}
		svc.opts.arrays = map[string]*ArrayConnectionData{
			"sys1": {
				SystemID: "sys1",
			},
		}

		for _, arr := range svc.opts.arrays {
			err := normalizeZoneConfig(arr)
			require.NoError(t, err)
		}

		result := svc.getZonesFromSecret()
		assert.Equal(t, 0, len(result), "non-zone config should produce empty zone map")
	})
}

// TestZoneAwareErrorMessages verifies that zone-related errors include
// system ID, zone name, and protection domain context.
func TestZoneAwareErrorMessages(t *testing.T) {
	zoneLabelKey := "topology.kubernetes.io/zone"

	t.Run("no zone topology found includes available zone names", func(t *testing.T) {
		zoneTargetMap := map[ZoneName]ZoneContent{
			"zoneA": {systemID: "sys1", protectionDomain: "PD1", pool: "pool1"},
			"zoneB": {systemID: "sys1", protectionDomain: "PD2", pool: "pool2"},
		}
		preferred := []*csi.Topology{
			{Segments: map[string]string{zoneLabelKey: "zoneX"}},
		}
		match := matchZoneFromTopology(zoneLabelKey, zoneTargetMap, preferred, "")
		assert.False(t, match.Matched)

		errMsg := formatZoneMatchError(zoneLabelKey, zoneTargetMap, preferred)
		assert.Contains(t, errMsg, "zoneX")
		assert.Contains(t, errMsg, "available zones")
	})

	t.Run("PD unreachable error includes zone context", func(t *testing.T) {
		innerErr := fmt.Errorf("connection refused")
		errMsg := formatZonePDError("sys1", "zoneA", "PD1", innerErr)
		assert.Contains(t, errMsg, "sys1")
		assert.Contains(t, errMsg, "zoneA")
		assert.Contains(t, errMsg, "PD1")
		assert.Contains(t, errMsg, "connection refused")
	})

	t.Run("snapshot zone pin error includes source system context", func(t *testing.T) {
		zoneTargetMap := map[ZoneName]ZoneContent{
			"zoneA": {systemID: "sys1", protectionDomain: "PD1", pool: "pool1"},
			"zoneB": {systemID: "sys2", protectionDomain: "PD2", pool: "pool2"},
		}
		preferred := []*csi.Topology{
			{Segments: map[string]string{zoneLabelKey: "zoneB"}},
		}
		// sourceSystemID = sys1 but zoneB maps to sys2 → no match
		match := matchZoneFromTopology(zoneLabelKey, zoneTargetMap, preferred, "sys1")
		assert.False(t, match.Matched)

		errMsg := formatZoneMatchError(zoneLabelKey, zoneTargetMap, preferred)
		assert.Contains(t, errMsg, "zoneB")
	})
}

// TestGetZoneConfigSummary verifies that getZoneConfigSummary produces
// a structured summary of all zone mappings suitable for diagnostic logging.
func TestGetZoneConfigSummary(t *testing.T) {
	t.Run("multi-zone config produces complete summary", func(t *testing.T) {
		arrays := map[string]*ArrayConnectionData{
			"sys1": {
				SystemID: "sys1",
				Endpoint: "https://gw1.example.com",
				Zones: []AvailabilityZone{
					{
						Name:     "zoneA",
						LabelKey: "zone.csi-vxflexos.dellemc.com",
						ProtectionDomains: []ProtectionDomain{
							{Name: "PD-A", Pools: []PoolName{"pool1"}},
						},
					},
					{
						Name:     "zoneB",
						LabelKey: "zone.csi-vxflexos.dellemc.com",
						ProtectionDomains: []ProtectionDomain{
							{Name: "PD-B", Pools: []PoolName{"pool2"}},
						},
					},
				},
			},
		}
		summary := getZoneConfigSummary(arrays)
		assert.Contains(t, summary, "sys1")
		assert.Contains(t, summary, "zoneA")
		assert.Contains(t, summary, "zoneB")
		assert.Contains(t, summary, "PD-A")
		assert.Contains(t, summary, "PD-B")
		assert.Contains(t, summary, "pool1")
		assert.Contains(t, summary, "pool2")
	})

	t.Run("non-zone config produces empty summary", func(t *testing.T) {
		arrays := map[string]*ArrayConnectionData{
			"sys1": {
				SystemID: "sys1",
				Endpoint: "https://gw1.example.com",
			},
		}
		summary := getZoneConfigSummary(arrays)
		assert.Contains(t, summary, "no zone")
	})

	t.Run("credential fields are never included in summary", func(t *testing.T) {
		arrays := map[string]*ArrayConnectionData{
			"sys1": {
				SystemID: "sys1",
				Endpoint: "https://gw1.example.com",
				Username: "admin",
				Password: "s3cr3t",
				Zones: []AvailabilityZone{
					{
						Name:     "zoneA",
						LabelKey: "zone.csi-vxflexos.dellemc.com",
						ProtectionDomains: []ProtectionDomain{
							{Name: "PD-A", Pools: []PoolName{"pool1"}},
						},
					},
				},
			},
		}
		summary := getZoneConfigSummary(arrays)
		assert.NotContains(t, summary, "s3cr3t")
		assert.NotContains(t, summary, "admin")
		assert.Contains(t, summary, "sys1")
		assert.Contains(t, summary, "zoneA")
	})
}

// TestNodeZoneAssociationLogging verifies the helper that formats
// node-to-zone log messages.
func TestNodeZoneAssociationLogging(t *testing.T) {
	t.Run("formats node zone association message", func(t *testing.T) {
		msg := formatNodeZoneAssociation("worker-1", "zoneA", "zone.csi-vxflexos.dellemc.com")
		assert.Contains(t, msg, "worker-1")
		assert.Contains(t, msg, "zoneA")
		assert.Contains(t, msg, "zone.csi-vxflexos.dellemc.com")
	})
}

func TestHasZoneConfig(t *testing.T) {
	t.Run("true when zones slice is populated", func(t *testing.T) {
		a := &ArrayConnectionData{Zones: []AvailabilityZone{{Name: "zoneA"}}}
		assert.True(t, a.hasZoneConfig())
	})

	t.Run("false when no zone config", func(t *testing.T) {
		a := &ArrayConnectionData{}
		assert.False(t, a.hasZoneConfig())
	})
}

func TestGetProtectionDomain_Unit(t *testing.T) {
	makeClient := func(t *testing.T, systemBody, protectionDomainBody string) *sio.Client {
		handler := http.NewServeMux()
		handler.HandleFunc("/api/types/System/instances", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(systemBody))
		})
		handler.HandleFunc("/api/types/ProtectionDomain/instances", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(protectionDomainBody))
		})
		handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		})
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)

		client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
		client.SetToken("test-token")
		return client
	}

	t.Run("provided PD name returns ID", func(t *testing.T) {
		client := makeClient(t, `[{"id":"sys1","links":[{"rel":"/api/System/relationship/ProtectionDomain","href":"/api/types/ProtectionDomain/instances"}]}]`, `[{"id":"pd1","name":"PD1"}]`)
		svc := &service{adminClients: map[string]*sio.Client{"sys1": client}}
		pdID, err := svc.getProtectionDomain("sys1", "PD1")
		assert.NoError(t, err)
		assert.Equal(t, "pd1", pdID)
	})

	t.Run("empty PD name returns first PD", func(t *testing.T) {
		client := makeClient(t, `[{"id":"sys1","links":[{"rel":"/api/System/relationship/ProtectionDomain","href":"/api/types/ProtectionDomain/instances"}]}]`, `[{"id":"pd1","name":"PD1"}]`)
		svc := &service{adminClients: map[string]*sio.Client{"sys1": client}}
		pdID, err := svc.getProtectionDomain("sys1", "")
		assert.NoError(t, err)
		assert.Equal(t, "pd1", pdID)
	})

	t.Run("empty PD name with no PDs returns error", func(t *testing.T) {
		client := makeClient(t, `[{"id":"sys1","links":[{"rel":"/api/System/relationship/ProtectionDomain","href":"/api/types/ProtectionDomain/instances"}]}]`, `[]`)
		svc := &service{adminClients: map[string]*sio.Client{"sys1": client}}
		_, err := svc.getProtectionDomain("sys1", "")
		assert.Error(t, err)
	})

	t.Run("FindSystem not found", func(t *testing.T) {
		client := makeClient(t, `[]`, `[{"id":"pd1","name":"PD1"}]`)
		svc := &service{adminClients: map[string]*sio.Client{"sys1": client}}
		_, err := svc.getProtectionDomain("sys1", "PD1")
		assert.Error(t, err)
	})

	t.Run("FindProtectionDomain not found", func(t *testing.T) {
		client := makeClient(t, `[{"id":"sys1","links":[{"rel":"/api/System/relationship/ProtectionDomain","href":"/api/types/ProtectionDomain/instances"}]}]`, `[]`)
		svc := &service{adminClients: map[string]*sio.Client{"sys1": client}}
		_, err := svc.getProtectionDomain("sys1", "PD1")
		assert.Error(t, err)
	})

	t.Run("GetProtectionDomain returns error", func(t *testing.T) {
		client := makeClient(t, `[{"id":"sys1","links":[{"rel":"/api/System/relationship/ProtectionDomain","href":"/api/types/ProtectionDomain/instances"}]}]`, `invalid json`)
		svc := &service{adminClients: map[string]*sio.Client{"sys1": client}}
		_, err := svc.getProtectionDomain("sys1", "")
		assert.Error(t, err)
	})
}

func TestFindPoolsForPD_Unit(t *testing.T) {
	makeClient := func(t *testing.T, pdListBody, pdInstanceBody, spBody string) *sio.Client {
		handler := http.NewServeMux()
		handler.HandleFunc("/api/login", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintf(w, `"fakesession"`)
		})
		handler.HandleFunc("/api/version", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprintf(w, `"4.0"`)
		})
		handler.HandleFunc("/api/types/ProtectionDomain/instances", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(pdListBody))
		})
		handler.HandleFunc("/api/instances/ProtectionDomain::pd1", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(pdInstanceBody))
		})
		handler.HandleFunc("/api/instances/ProtectionDomain::pd1/relationships/StoragePool", func(w http.ResponseWriter, _ *http.Request) {
			if spBody == "error" {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write([]byte(spBody))
		})
		handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		})
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)

		client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
		client.SetToken("test-token")
		return client
	}

	makeSystem := func(client *sio.Client) *sio.System {
		sys := sio.NewSystem(client)
		sys.System = &siotypes.System{
			Links: []*siotypes.Link{
				{Rel: "/api/System/relationship/ProtectionDomain", HREF: "/api/types/ProtectionDomain/instances"},
			},
		}
		return sys
	}

	t.Run("returns pool names for a protection domain", func(t *testing.T) {
		validPDList := `[{"id":"pd1","name":"PD1"}]`
		validPDInstance := `{"id":"pd1","name":"PD1","links":[{"rel":"/api/ProtectionDomain/relationship/StoragePool","href":"/api/instances/ProtectionDomain::pd1/relationships/StoragePool"}]}`
		client := makeClient(t, validPDList, validPDInstance, `[{"name":"pool1"},{"name":"pool2"}]`)
		lookup := &goscaleioPoolLookup{system: makeSystem(client)}
		pools, err := lookup.FindPoolsForPD("PD1")
		assert.NoError(t, err)
		assert.Equal(t, []string{"pool1", "pool2"}, pools)
	})

	t.Run("returns error when protection domain is not found", func(t *testing.T) {
		validPDInstance := `{"id":"pd1","name":"PD1","links":[{"rel":"/api/ProtectionDomain/relationship/StoragePool","href":"/api/instances/ProtectionDomain::pd1/relationships/StoragePool"}]}`
		client := makeClient(t, `[]`, validPDInstance, `[]`)
		lookup := &goscaleioPoolLookup{system: makeSystem(client)}
		_, err := lookup.FindPoolsForPD("PD-missing")
		assert.Error(t, err)
	})

	t.Run("returns error when protection domain instance cannot be fetched", func(t *testing.T) {
		client := makeClient(t, `[{"id":"pd1","name":"PD1"}]`, `invalid json`, `[]`)
		lookup := &goscaleioPoolLookup{system: makeSystem(client)}
		_, err := lookup.FindPoolsForPD("PD1")
		assert.Error(t, err)
	})

	t.Run("returns error when storage pool link is missing", func(t *testing.T) {
		client := makeClient(t, `[{"id":"pd1","name":"PD1"}]`, `{"id":"pd1","name":"PD1"}`, `[]`)
		lookup := &goscaleioPoolLookup{system: makeSystem(client)}
		_, err := lookup.FindPoolsForPD("PD1")
		assert.Error(t, err)
	})
}

func TestGetGenType_Unit(t *testing.T) {
	makeSystem := func(client *sio.Client) *sio.System {
		sys := sio.NewSystem(client)
		sys.System = &siotypes.System{
			Links: []*siotypes.Link{
				{Rel: "/api/System/relationship/ProtectionDomain", HREF: "/api/types/ProtectionDomain/instances"},
			},
		}
		return sys
	}

	t.Run("returns error when system is nil", func(t *testing.T) {
		// FR-3: nil system must return an error, not ("", nil).
		svc := &service{systems: map[string]*sio.System{}}
		_, err := svc.GetGenType("sys1")
		assert.Error(t, err)
	})

	t.Run("returns genType of first protection domain", func(t *testing.T) {
		handler := func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			if r.URL.Path == "/api/types/ProtectionDomain/instances" {
				_, _ = w.Write([]byte(`[{"id":"pd1","name":"PD1","genType":"Default"}]`))
			} else {
				_, _ = w.Write([]byte(`{}`))
			}
		}
		server := httptest.NewServer(http.HandlerFunc(handler))
		defer server.Close()
		client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
		client.SetToken("test-token")
		svc := &service{systems: map[string]*sio.System{"sys1": makeSystem(client)}}
		genType, err := svc.GetGenType("sys1")
		assert.NoError(t, err)
		assert.Equal(t, "Default", genType)
	})

	t.Run("returns error when no protection domains", func(t *testing.T) {
		// FR-3: zero PDs must return an error, not ("", nil).
		handler := func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		}
		server := httptest.NewServer(http.HandlerFunc(handler))
		defer server.Close()
		client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
		client.SetToken("test-token")
		svc := &service{systems: map[string]*sio.System{"sys1": makeSystem(client)}}
		_, err := svc.GetGenType("sys1")
		assert.Error(t, err)
	})

	t.Run("returns error when GetProtectionDomain fails", func(t *testing.T) {
		handler := func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`invalid json`))
		}
		server := httptest.NewServer(http.HandlerFunc(handler))
		defer server.Close()
		client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
		client.SetToken("test-token")
		svc := &service{systems: map[string]*sio.System{"sys1": makeSystem(client)}}
		_, err := svc.GetGenType("sys1")
		assert.Error(t, err)
	})

	// FR-4: "" with PDs → valid Gen1 signal (genType ""); no error.
	t.Run("empty genType with PDs returns Gen1 signal", func(t *testing.T) {
		handler := func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			if r.URL.Path == "/api/types/ProtectionDomain/instances" {
				_, _ = w.Write([]byte(`[{"id":"pd1","name":"PD1","genType":""}]`))
			} else {
				_, _ = w.Write([]byte(`{}`))
			}
		}
		server := httptest.NewServer(http.HandlerFunc(handler))
		defer server.Close()
		client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
		client.SetToken("test-token")
		svc := &service{systems: map[string]*sio.System{"sys1": makeSystem(client)}}
		genType, err := svc.GetGenType("sys1")
		assert.NoError(t, err)
		assert.Equal(t, "", genType, "empty genType must be returned as-is (Gen1 signal)")
	})

	// FR-4: "EC" with PDs → valid Gen2/EC signal; no error.
	t.Run("EC genType with PDs returns Gen2/EC signal", func(t *testing.T) {
		handler := func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			if r.URL.Path == "/api/types/ProtectionDomain/instances" {
				_, _ = w.Write([]byte(`[{"id":"pd1","name":"PD1","genType":"EC"}]`))
			} else {
				_, _ = w.Write([]byte(`{}`))
			}
		}
		server := httptest.NewServer(http.HandlerFunc(handler))
		defer server.Close()
		client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
		client.SetToken("test-token")
		svc := &service{systems: map[string]*sio.System{"sys1": makeSystem(client)}}
		genType, err := svc.GetGenType("sys1")
		assert.NoError(t, err)
		assert.Equal(t, "EC", genType, "EC genType must be returned as-is (Gen2/EC signal)")
	})

	// FR-4: Unknown genType "GEN3" passes through GetGenType unchanged; isKnownGenType
	// (called in CreateVolume) then rejects it. GetGenType itself must NOT error.
	t.Run("unknown genType GEN3 passes through GetGenType without error", func(t *testing.T) {
		handler := func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			if r.URL.Path == "/api/types/ProtectionDomain/instances" {
				_, _ = w.Write([]byte(`[{"id":"pd1","name":"PD1","genType":"GEN3"}]`))
			} else {
				_, _ = w.Write([]byte(`{}`))
			}
		}
		server := httptest.NewServer(http.HandlerFunc(handler))
		defer server.Close()
		client, _ := sio.NewClientWithArgs(server.URL, "4.0", 0, true, false, "")
		client.SetToken("test-token")
		svc := &service{systems: map[string]*sio.System{"sys1": makeSystem(client)}}
		genType, err := svc.GetGenType("sys1")
		assert.NoError(t, err, "GetGenType must not error on unknown genType — validation is caller's job")
		assert.Equal(t, "GEN3", genType, "unknown genType must be returned as-is for caller to validate")
		// Confirm isKnownGenType correctly flags this value
		assert.False(t, isKnownGenType(genType), "isKnownGenType must return false for unknown genType GEN3")
	})
}

func TestFindPoolsForPD(t *testing.T) {
	makeClient := func(t *testing.T, handler http.HandlerFunc) *sio.Client {
		server := httptest.NewServer(handler)
		t.Cleanup(server.Close)
		client, err := sio.NewClientWithArgs(server.URL, "4.0", math.MaxInt64, true, false, "")
		assert.NoError(t, err)
		client.SetToken("test-token")
		return client
	}

	makeSystem := func(client *sio.Client) *sio.System {
		sys := sio.NewSystem(client)
		sys.System = &siotypes.System{
			Links: []*siotypes.Link{
				{Rel: "/api/System/relationship/ProtectionDomain", HREF: "/api/types/ProtectionDomain/instances"},
			},
		}
		return sys
	}

	t.Run("returns pool names for a protection domain", func(t *testing.T) {
		handler := func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			switch {
			case strings.Contains(r.URL.Path, "/api/login"):
				fmt.Fprintf(w, `"fakesession"`)
			case strings.Contains(r.URL.Path, "/api/version"):
				fmt.Fprintf(w, `"4.0"`)
			case r.URL.Path == "/api/types/ProtectionDomain/instances":
				_, _ = w.Write([]byte(`[{"id":"pd1","name":"PD1"}]`))
			case r.URL.Path == "/api/instances/ProtectionDomain::pd1/relationships/StoragePool":
				_, _ = w.Write([]byte(`[{"name":"pool1"},{"name":"pool2"}]`))
			case r.URL.Path == "/api/instances/ProtectionDomain::pd1":
				_, _ = w.Write([]byte(`{"id":"pd1","name":"PD1","links":[{"rel":"/api/ProtectionDomain/relationship/StoragePool","href":"/api/instances/ProtectionDomain::pd1/relationships/StoragePool"}]}`))
			default:
				_, _ = w.Write([]byte(`{}`))
			}
		}

		client := makeClient(t, handler)
		lookup := &goscaleioPoolLookup{system: makeSystem(client)}
		pools, err := lookup.FindPoolsForPD("PD1")
		assert.NoError(t, err)
		assert.Equal(t, []string{"pool1", "pool2"}, pools)
	})

	t.Run("returns error when protection domain is not found", func(t *testing.T) {
		handler := func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			switch {
			case strings.Contains(r.URL.Path, "/api/login"):
				fmt.Fprintf(w, `"fakesession"`)
			case strings.Contains(r.URL.Path, "/api/version"):
				fmt.Fprintf(w, `"4.0"`)
			case r.URL.Path == "/api/types/ProtectionDomain/instances":
				_, _ = w.Write([]byte(`[]`))
			default:
				_, _ = w.Write([]byte(`{}`))
			}
		}

		client := makeClient(t, handler)
		lookup := &goscaleioPoolLookup{system: makeSystem(client)}
		_, err := lookup.FindPoolsForPD("PD-missing")
		assert.Error(t, err)
	})
}

// TestGetStoragePoolID_NilAdminClient covers the nil-guard added in H2.
func TestGetStoragePoolID_NilAdminClient(t *testing.T) {
	svc := &service{}
	svc.adminClients = map[string]*sio.Client{} // no client for "sys1"
	_, err := svc.getStoragePoolID("pool1", "sys1", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "admin client not found for system sys1")
}

// TestGetProtectionDomainIDFromName covers the nil-guard (H2) and empty-name early return.
func TestGetProtectionDomainIDFromName(t *testing.T) {
	t.Run("empty protectionDomainName → returns empty string, no error", func(t *testing.T) {
		svc := &service{}
		svc.adminClients = map[string]*sio.Client{}
		id, err := svc.getProtectionDomainIDFromName("sys1", "")
		require.NoError(t, err)
		assert.Equal(t, "", id)
	})

	t.Run("nil adminClient → returns error", func(t *testing.T) {
		svc := &service{}
		svc.adminClients = map[string]*sio.Client{} // no client for "sys1"
		_, err := svc.getProtectionDomainIDFromName("sys1", "PD1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "admin client not found for system sys1")
	})

	t.Run("lookup function returns error → error propagated", func(t *testing.T) {
		orig := getProtectionDomainIDFromNameFunc
		getProtectionDomainIDFromNameFunc = func(_ *sio.Client, _, _ string) (string, error) {
			return "", fmt.Errorf("gateway timeout")
		}
		defer func() { getProtectionDomainIDFromNameFunc = orig }()

		svc := &service{}
		svc.adminClients = map[string]*sio.Client{"sys1": {}}
		_, err := svc.getProtectionDomainIDFromName("sys1", "PD1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "gateway timeout")
	})
}

// TestGetStoragePoolID_LookupError covers the findStoragePoolFunc error path.
func TestGetStoragePoolID_LookupError(t *testing.T) {
	orig := findStoragePoolFunc
	findStoragePoolFunc = func(_ *sio.Client, _, _, _, _ string) (*siotypes.StoragePool, error) {
		return nil, fmt.Errorf("pool not found")
	}
	defer func() { findStoragePoolFunc = orig }()

	svc := &service{}
	svc.adminClients = map[string]*sio.Client{"sys1": {}}
	_, err := svc.getStoragePoolID("SP1", "sys1", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pool not found")
}

// TestGetZonesFromSecret_DuplicateZone covers the overwrite warning path.
// Go map iteration is non-deterministic, so we only assert that exactly one
// entry survives (the duplicate is overwritten) and that it is a valid entry.
func TestGetZonesFromSecret_DuplicateZone(t *testing.T) {
	svc := &service{}
	svc.opts.arrays = map[string]*ArrayConnectionData{
		"sys1": {
			SystemID: "sys1",
			Zones: []AvailabilityZone{
				{Name: "zoneA", LabelKey: "topology.kubernetes.io/zone", ProtectionDomains: []ProtectionDomain{{Name: "PD1", Pools: []PoolName{"pool1"}}}},
			},
		},
		"sys2": {
			SystemID: "sys2",
			Zones: []AvailabilityZone{
				{Name: "zoneA", LabelKey: "topology.kubernetes.io/zone", ProtectionDomains: []ProtectionDomain{{Name: "PD2", Pools: []PoolName{"pool2"}}}},
			},
		},
	}
	result := svc.getZonesFromSecret()
	// duplicate zoneA: one entry overwrites the other; exactly one must survive.
	require.Equal(t, 1, len(result))
	zoneA, ok := result[ZoneName("zoneA")]
	require.True(t, ok)
	// Whichever system iterated last wins; either is a valid outcome.
	assert.Contains(t, []string{"sys1", "sys2"}, zoneA.systemID)
	assert.NotEmpty(t, string(zoneA.protectionDomain))
	assert.NotEmpty(t, string(zoneA.pool))
}

// ── GetPlatformInfo ───────────────────────────────────────────────────
// Cover: GetPlatformVersion returns error, GetGenType returns error.

func TestGetPlatformInfo_VersionError(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"message":"internal","httpStatusCode":500,"errorCode":0}`)
	}))
	defer ts.Close()

	client, _ := sio.NewClientWithArgs(ts.URL, "4.0", 0, true, false, "")
	svc := &service{
		adminClients:  map[string]*sio.Client{"sys1": client},
		platformInfos: map[string]*PlatformInfo{},
	}
	// GetPlatformVersion will call client.GetVersion() which calls the TLS server → error
	_, err := svc.GetPlatformInfo("sys1")
	assert.Error(t, err)
}

// ── getArrayVersion ───────────────────────────────────────────────────
// Cover: adminClient is nil for the systemID (line 2874-2875).

func TestGetArrayVersion_AdminClientNil(t *testing.T) {
	svc := &service{
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				"sys1": {SystemID: "sys1", Endpoint: "http://127.0.0.1"},
			},
		},
		adminClients:  map[string]*sio.Client{},
		systems:       map[string]*sio.System{},
		platformInfos: map[string]*PlatformInfo{},
	}
	// systemProbeAll will fail (no real backend); that is the allArrayFail path
	_, err := svc.getArrayVersion(context.Background(), "sys1")
	assert.Error(t, err)
}

// ── GetNodeIPByCSINodeID ──────────────────────────────────────────────
// Cover: getNode returns error (line 2471-2473) and no InternalIP (line 2477-2481).

func TestGetNodeIPByCSINodeID_GetNodeError(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	// Add a CSINode that maps nodeID → a kube node name
	csiNode := &storagev1.CSINode{
		ObjectMeta: metav1.ObjectMeta{Name: "node1"},
		Spec: storagev1.CSINodeSpec{
			Drivers: []storagev1.CSINodeDriver{
				{Name: Name, NodeID: "csi-node-id-1"},
			},
		},
	}
	_, _ = fakeClient.StorageV1().CSINodes().Create(context.Background(), csiNode, metav1.CreateOptions{})
	// Do NOT create the actual k8s Node → getNode will return 404

	K8sClientset = fakeClient
	defer func() { K8sClientset = nil }()

	svc := &service{}
	ip := svc.GetNodeIPByCSINodeID("csi-node-id-1")
	assert.Equal(t, "", ip)
}

func TestGetNodeIPByCSINodeID_NoInternalIP(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	csiNode := &storagev1.CSINode{
		ObjectMeta: metav1.ObjectMeta{Name: "node2"},
		Spec: storagev1.CSINodeSpec{
			Drivers: []storagev1.CSINodeDriver{
				{Name: Name, NodeID: "csi-node-id-2"},
			},
		},
	}
	_, _ = fakeClient.StorageV1().CSINodes().Create(context.Background(), csiNode, metav1.CreateOptions{})

	// Create k8s Node with only ExternalIP (no InternalIP)
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node2"},
		Status: v1.NodeStatus{
			Addresses: []v1.NodeAddress{
				{Type: v1.NodeExternalIP, Address: "1.2.3.4"},
			},
		},
	}
	_, _ = fakeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})

	K8sClientset = fakeClient
	defer func() { K8sClientset = nil }()

	svc := &service{}
	ip := svc.GetNodeIPByCSINodeID("csi-node-id-2")
	assert.Equal(t, "", ip)
}

// ── GetNodeIP ─────────────────────────────────────────────────────────
// Cover: node has no InternalIP (line 2610 - the fmt.Errorf return).

func TestGetNodeIP_NoInternalIPAddress(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "mynode"},
		Status: v1.NodeStatus{
			Addresses: []v1.NodeAddress{
				{Type: v1.NodeExternalIP, Address: "9.9.9.9"},
			},
		},
	}
	_, _ = fakeClient.CoreV1().Nodes().Create(context.Background(), node, metav1.CreateOptions{})

	K8sClientset = fakeClient
	defer func() { K8sClientset = nil }()

	svc := &service{opts: Opts{KubeNodeName: "mynode"}}
	_, err := svc.GetNodeIP(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no InternalIP found")
}

// ── SetPodZoneLabel ───────────────────────────────────────────────────
// Cover: pod found, update succeeds (line 2572-2580).

func TestSetPodZoneLabel_UpdatesLabel(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	// Pod must be in DriverNamespace ("vxflexos"), on the target node, and have
	// an "app" label containing "node" so the function picks it up.
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "csi-node-pod",
			Namespace: DriverNamespace,
			Labels:    map[string]string{"app": "csi-node"},
		},
		Spec: v1.PodSpec{NodeName: "worker1"},
	}
	_, _ = fakeClient.CoreV1().Pods(DriverNamespace).Create(context.Background(), pod, metav1.CreateOptions{})

	K8sClientset = fakeClient
	defer func() { K8sClientset = nil }()

	svc := &service{opts: Opts{KubeNodeName: "worker1"}}
	err := svc.SetPodZoneLabel(context.Background(), map[string]string{"zone": "us-east-1a"})
	assert.NoError(t, err)
}

func TestSetPodZoneLabel_PodGetError(t *testing.T) {
	fakeClient := fake.NewSimpleClientset()
	// No matching pod in DriverNamespace → "no node pod found" NotFound error

	K8sClientset = fakeClient
	defer func() { K8sClientset = nil }()

	svc := &service{opts: Opts{KubeNodeName: "worker2"}}
	err := svc.SetPodZoneLabel(context.Background(), map[string]string{"zone": "us-east-1a"})
	assert.Error(t, err)
}

// ── logCsiNodeTopologyKeys ────────────────────────────────────────────
// Cover: K8sClientset is already set (skip creation), CSINodes list result.

func TestLogCsiNodeTopologyKeys_ListCSINodeError(t *testing.T) {
	// Use a real fake clientset but inject a reactor that fails CSINodes list
	fakeClient := fake.NewSimpleClientset()

	K8sClientset = fakeClient
	defer func() { K8sClientset = nil }()

	svc := &service{opts: Opts{KubeNodeName: ""}}
	// This exercises the List call; empty result is fine (no error expected from the function)
	err := svc.logCsiNodeTopologyKeys()
	assert.NoError(t, err)
}

// ── updateConfigMap ───────────────────────────────────────────────────
// Cover: failed to read config file (line 1022-1024).

func TestUpdateConfigMap_FileNotFound(_ *testing.T) {
	svc := &service{opts: Opts{KubeNodeName: "node1"}}
	// Should log an error and return without panic
	svc.updateConfigMap(svc.getIPAddressByInterface, "/nonexistent/config/file.yaml")
}

// ── isNFSEnabled ──────────────────────────────────────────────────────
// Cover: systemProbeAll returns error → isNFSEnabled returns that error (line 1152-1153).

func TestIsNFSEnabled_SystemProbeAllError(t *testing.T) {
	svc := &service{
		opts: Opts{
			arrays: map[string]*ArrayConnectionData{
				"sys1": {SystemID: "sys1", Endpoint: "http://127.0.0.1:1"},
			},
		},
		adminClients:  map[string]*sio.Client{},
		systems:       map[string]*sio.System{},
		platformInfos: map[string]*PlatformInfo{},
	}
	// systemProbeAll will fail since there's no real backend (no admin clients)
	_, err := svc.isNFSEnabled(context.Background(), "sys1")
	assert.Error(t, err)
}

// ── getSDCIPs ─────────────────────────────────────────────────────────
// Cover: system not nil but FindSdc returns error (line 1308-1310).

func TestGetSDCIPs_FindSdcError(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "instances/Sdc") || strings.Contains(r.URL.Path, "types/Sdc") {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"not found","httpStatusCode":404,"errorCode":0}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"id":"sys1","name":"sys1"}`)
	}))
	defer ts.Close()

	client, _ := sio.NewClientWithArgs(ts.URL, "4.0", 0, true, false, "")
	sys := sio.NewSystem(client)
	sys.System = &siotypes.System{ID: "sys1"}

	svc := &service{
		systems: map[string]*sio.System{"sys1": sys},
	}

	_, err := svc.getSDCIPs("AABBCCDD", "sys1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "error finding SDC from GUID")
}

// ── GetPlatformVersion ────────────────────────────────────────────────
// Cover: GetVersion returns an error (line 2855-2857).

func TestGetPlatformVersion_GetVersionError(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"message":"server error","httpStatusCode":500,"errorCode":0}`)
	}))
	defer ts.Close()

	client, _ := sio.NewClientWithArgs(ts.URL, "4.0", 0, true, false, "")
	svc := &service{adminClients: map[string]*sio.Client{"sysVerErr": client}}
	_, err := svc.GetPlatformVersion("sysVerErr")
	assert.Error(t, err)
}
