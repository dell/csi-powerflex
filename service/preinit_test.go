// Copyright © 2024 Dell Inc. or its subsidiaries. All Rights Reserved.
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
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPreInit(t *testing.T) {
	svc := NewPreInitService()
	err := svc.PreInit()
	assert.Nil(t, err)
}

func TestGetMdmList(t *testing.T) {
	tests := []struct {
		name           string
		connectionInfo []*ArrayConnectionData
		key            string
		zone           string
		errorExpected  bool
		expectedResult string
	}{
		{
			name:           "key is empty",
			connectionInfo: []*ArrayConnectionData{},
			key:            "",
			zone:           "testZone",
			errorExpected:  true,
			expectedResult: "",
		},
		{
			name:           "zone is empty",
			connectionInfo: []*ArrayConnectionData{},
			key:            "testKey",
			zone:           "",
			errorExpected:  true,
			expectedResult: "",
		},
		{
			name:           "key and zone are not empty with no arrays",
			connectionInfo: []*ArrayConnectionData{},
			key:            "testKey",
			zone:           "testZone",
			errorExpected:  false,
			expectedResult: "",
		},
		{
			name: "no zone info and no MDMs",
			connectionInfo: []*ArrayConnectionData{
				{
					SystemID: "testSystemID",
				},
			},
			key:            "testKey",
			zone:           "testZone",
			errorExpected:  false,
			expectedResult: "",
		},
		{
			name: "no zone info with MDMs",
			connectionInfo: []*ArrayConnectionData{
				{
					SystemID: "testSystemID",
					Mdm:      "192.168.0.10,192.168.0.20",
				},
			},
			key:            "testKey",
			zone:           "testZone",
			errorExpected:  false,
			expectedResult: "",
		},

		{
			name: "single MDM",
			connectionInfo: []*ArrayConnectionData{
				{
					SystemID: "testSystemID",
					Mdm:      "192.168.0.10",
					Zone:     ZoneInfo{Name: "testZone", LabelKey: "testKey"},
				},
			},
			key:            "testKey",
			zone:           "testZone",
			errorExpected:  false,
			expectedResult: "192.168.0.10",
		},
		{
			name: "two MDMs",
			connectionInfo: []*ArrayConnectionData{
				{
					SystemID: "testSystemID",
					Mdm:      "192.168.0.10,192.168.0.20",
					Zone:     ZoneInfo{Name: "testZone", LabelKey: "testKey"},
				},
			},
			key:            "testKey",
			zone:           "testZone",
			errorExpected:  false,
			expectedResult: "192.168.0.10,192.168.0.20",
		},
		{
			name: "two arrays",
			connectionInfo: []*ArrayConnectionData{
				{
					SystemID: "testSystemID1",
					Mdm:      "192.168.0.10,192.168.0.20",
					Zone:     ZoneInfo{Name: "testZone", LabelKey: "testKey"},
				},
				{
					SystemID: "testSystemID2",
					Mdm:      "192.168.1.10,192.168.1.20",
					Zone:     ZoneInfo{Name: "testZone", LabelKey: "testKey"},
				},
			},
			key:            "testKey",
			zone:           "testZone",
			errorExpected:  false,
			expectedResult: "192.168.0.10,192.168.0.20,192.168.1.10,192.168.1.20",
		},
		{
			name: "two arrays with multiple zones #1",
			connectionInfo: []*ArrayConnectionData{
				{
					SystemID: "testSystemID1",
					Mdm:      "192.168.0.10,192.168.0.20",
					Zone:     ZoneInfo{Name: "testZone1", LabelKey: "testKey"},
				},
				{
					SystemID: "testSystemID2",
					Mdm:      "192.168.1.10,192.168.1.20",
					Zone:     ZoneInfo{Name: "testZone2", LabelKey: "testKey"},
				},
			},
			key:            "testKey",
			zone:           "testZone1",
			errorExpected:  false,
			expectedResult: "192.168.0.10,192.168.0.20",
		},
		{
			name: "two arrays with multiple zones #2",
			connectionInfo: []*ArrayConnectionData{
				{
					SystemID: "testSystemID1",
					Mdm:      "192.168.0.10,192.168.0.20",
					Zone:     ZoneInfo{Name: "testZone1", LabelKey: "testKey"},
				},
				{
					SystemID: "testSystemID2",
					Mdm:      "192.168.1.10,192.168.1.20",
					Zone:     ZoneInfo{Name: "testZone2", LabelKey: "testKey"},
				},
			},
			key:            "testKey",
			zone:           "testZone2",
			errorExpected:  false,
			expectedResult: "192.168.1.10,192.168.1.20",
		},
		{
			name: "two arrays in same zone with different keys #1",
			connectionInfo: []*ArrayConnectionData{
				{
					SystemID: "testSystemID1",
					Mdm:      "192.168.0.10,192.168.0.20",
					Zone:     ZoneInfo{Name: "testZone", LabelKey: "testKey1"},
				},
				{
					SystemID: "testSystemID2",
					Mdm:      "192.168.1.10,192.168.1.20",
					Zone:     ZoneInfo{Name: "testZone", LabelKey: "testKey2"},
				},
			},
			key:            "testKey1",
			zone:           "testZone",
			errorExpected:  false,
			expectedResult: "192.168.0.10,192.168.0.20",
		},
		{
			name: "two arrays in same zone with different keys #2",
			connectionInfo: []*ArrayConnectionData{
				{
					SystemID: "testSystemID1",
					Mdm:      "192.168.0.10,192.168.0.20",
					Zone:     ZoneInfo{Name: "testZone", LabelKey: "testKey1"},
				},
				{
					SystemID: "testSystemID2",
					Mdm:      "192.168.1.10,192.168.1.20",
					Zone:     ZoneInfo{Name: "testZone", LabelKey: "testKey2"},
				},
			},
			key:            "testKey2",
			zone:           "testZone",
			errorExpected:  false,
			expectedResult: "192.168.1.10,192.168.1.20",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := getMdmList(test.connectionInfo, test.key, test.zone)
			fmt.Fprintf(os.Stderr, "%s: %v\n", test.name, result)
			if (err != nil) != test.errorExpected {
				t.Errorf("getMdmList() error = %v, wantErr %v", err, test.errorExpected)
				return
			}
			if !reflect.DeepEqual(test.expectedResult, result) {
				t.Errorf("getMdmList() = '%v', want '%v'", result, test.expectedResult)
			}
		})
	}
}
