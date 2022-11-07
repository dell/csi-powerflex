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
	"testing"

	csi "github.com/container-storage-interface/spec/lib/go/csi"
	siotypes "github.com/dell/goscaleio/types/v1"
	"github.com/stretchr/testify/assert"
)

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
						Mode: csi.VolumeCapability_AccessMode_UNKNOWN},
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
						Mode: csi.VolumeCapability_AccessMode_UNKNOWN},
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
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
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
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER},
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
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY},
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
						Mode: csi.VolumeCapability_AccessMode_SINGLE_NODE_READER_ONLY},
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
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY},
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
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY},
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
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY},
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
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_READER_ONLY},
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
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER},
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
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER},
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
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER},
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
						Mode: csi.VolumeCapability_AccessMode_MULTI_NODE_MULTI_WRITER},
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
			s, _ := valVolumeCaps(tt.caps, tt.vol)

			assert.Equal(st, tt.supported, s)
		})
	}
}
