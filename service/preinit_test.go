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
	"io/fs"
	"os"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

type MockArrayConfigurationProvider struct {
	mock.Mock
}

func (mock *MockArrayConfigurationProvider) GetArrayConfiguration() ([]*ArrayConnectionData, error) {
	args := mock.Called()
	return args.Get(0).([]*ArrayConnectionData), args.Error(1)
}

type MockFileWriterProvider struct {
	mock.Mock
}

func (mock *MockFileWriterProvider) WriteFile(filename string, data []byte, perm os.FileMode) error {
	args := mock.Called(filename, data, perm)
	return args.Error(0)
}

func TestNewPreInitService(t *testing.T) {
	svc := NewPreInitService()
	assert.NotNil(t, svc)
}

func TestPreInit(t *testing.T) {
	tests := []struct {
		name            string
		connectionInfo  []*ArrayConnectionData
		connectionError error
		nodeInfo        *corev1.Node
		errorExpected   bool
		expectedResult  string
	}{
		{
			name:           "should error on no connection info",
			connectionInfo: []*ArrayConnectionData{},
			errorExpected:  true,
			expectedResult: "unable to get zone label key: array connection data is empty",
		},
		{
			name:            "should handle error getting connection info",
			connectionInfo:  nil,
			connectionError: fmt.Errorf("don't care about error text"),
			errorExpected:   true,
			expectedResult:  "unable to get array configuration: don't care about error text",
		},
		{
			name: "should error when zone labels different",
			connectionInfo: []*ArrayConnectionData{
				{
					AvailabilityZone: &AvailabilityZone{
						LabelKey: "key1",
						Name:     "zone1",
					},
				},
				{
					AvailabilityZone: &AvailabilityZone{
						LabelKey: "key2",
						Name:     "zone1",
					},
				},
			},
			errorExpected:  true,
			expectedResult: "unable to get zone label key: zone label key is not the same for all arrays",
		},
		{
			name: "should configure all MDMs when no zone label single array",
			connectionInfo: []*ArrayConnectionData{
				{
					Mdm: "192.168.1.1,192.168.1.2",
				},
			},
			errorExpected:  false,
			expectedResult: "192.168.1.1,192.168.1.2",
		},
		{
			name: "should configure all MDMs when no zone label multi array",
			connectionInfo: []*ArrayConnectionData{
				{
					Mdm: "192.168.1.1,192.168.1.2",
				},
				{
					Mdm: "192.168.2.1,192.168.2.2",
				},
			},
			errorExpected:  false,
			expectedResult: "192.168.1.1,192.168.1.2\\&192.168.2.1,192.168.2.2",
		},
		{
			name: "should fail if zones configured but unable to fetch node labels",
			connectionInfo: []*ArrayConnectionData{
				{
					Mdm: "192.168.1.1,192.168.1.2",
					AvailabilityZone: &AvailabilityZone{
						LabelKey: "key1",
						Name:     "zone1",
					},
				},
			},
			errorExpected:  true,
			expectedResult: "unable to get node labels: rpc error: code = Internal desc = Unable to fetch the node labels. Error: nodes \"testnode\" not found",
		},
		{
			name: "should configure empty MDM list if node label not found for node",
			connectionInfo: []*ArrayConnectionData{
				{
					Mdm: "192.168.1.1,192.168.1.2",
					AvailabilityZone: &AvailabilityZone{
						LabelKey: "key1",
						Name:     "zone1",
					},
				},
			},
			nodeInfo: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "testnode",
					Labels: map[string]string{"label1": "value1", "label2": "value2"},
				},
			},
			errorExpected:  false,
			expectedResult: "",
		},
		{
			name: "should configure empty MDM list if node label present but value is nil",
			connectionInfo: []*ArrayConnectionData{
				{
					Mdm: "192.168.1.1,192.168.1.2",
					AvailabilityZone: &AvailabilityZone{
						LabelKey: "key1",
						Name:     "zone1",
					},
				},
			},
			nodeInfo: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "testnode",
					Labels: map[string]string{"key1": "", "label2": "value2"},
				},
			},
			errorExpected:  false,
			expectedResult: "",
		},
		{
			name: "should configure MDMs for only array matching zone label",
			connectionInfo: []*ArrayConnectionData{
				{
					Mdm: "192.168.1.1,192.168.1.2",
					AvailabilityZone: &AvailabilityZone{
						LabelKey: "key1",
						Name:     "zone1",
					},
				},
				{
					Mdm: "192.168.2.1,192.168.2.2",
					AvailabilityZone: &AvailabilityZone{
						LabelKey: "key1",
						Name:     "zone2",
					},
				},
			},
			nodeInfo: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "testnode",
					Labels: map[string]string{"key1": "zone1", "label2": "value2"},
				},
			},
			errorExpected:  false,
			expectedResult: "192.168.1.1,192.168.1.2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mockArrayConfigurationProvider := &MockArrayConfigurationProvider{}
			mockFileWriterProvider := &MockFileWriterProvider{}

			arrayConfigurationProviderImpl = mockArrayConfigurationProvider
			fileWriterProviderImpl = mockFileWriterProvider

			if test.nodeInfo == nil {
				K8sClientset = fake.NewClientset()
			} else {
				K8sClientset = fake.NewClientset(test.nodeInfo)
			}

			t.Setenv("NODENAME", "testnode")

			svc := NewPreInitService()

			mockArrayConfigurationProvider.On("GetArrayConfiguration").Return(test.connectionInfo, test.connectionError)
			mockFileWriterProvider.On("WriteFile", mock.Anything, mock.Anything, mock.Anything).Return(nil)

			err := svc.PreInit()
			if test.errorExpected {
				assert.Equal(t, test.expectedResult, err.Error())
			} else {
				assert.Nil(t, err)
				mockFileWriterProvider.AssertCalled(t, "WriteFile", nodeMdmsFile,
					[]byte(fmt.Sprintf("MDM=%s\n", test.expectedResult)), fs.FileMode(0o444))
			}
		})
	}
}

// This test will use the default providers to exercise the
// production use case.
func TestPreInitWithDefaultProviders(t *testing.T) {
	arrayConfigurationProviderImpl = &DefaultArrayConfigurationProvider{}
	fileWriterProviderImpl = &DefaultFileWriterProvider{}
	K8sClientset = fake.NewClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "",
		},
	})
	ArrayConfigFile = "features/array-config/simple-config.yaml"
	nodeMdmsFile = fmt.Sprintf("%s/node_mdms.txt", t.TempDir())
	svc := NewPreInitService()

	_ = os.Remove(nodeMdmsFile)
	err := svc.PreInit()

	assert.Nil(t, err)
	assert.FileExists(t, nodeMdmsFile)
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
					SystemID:         "testSystemID",
					Mdm:              "192.168.0.10",
					AvailabilityZone: &AvailabilityZone{Name: "testZone", LabelKey: "testKey"},
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
					SystemID:         "testSystemID",
					Mdm:              "192.168.0.10,192.168.0.20",
					AvailabilityZone: &AvailabilityZone{Name: "testZone", LabelKey: "testKey"},
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
					SystemID:         "testSystemID1",
					Mdm:              "192.168.0.10,192.168.0.20",
					AvailabilityZone: &AvailabilityZone{Name: "testZone", LabelKey: "testKey"},
				},
				{
					SystemID:         "testSystemID2",
					Mdm:              "192.168.1.10,192.168.1.20",
					AvailabilityZone: &AvailabilityZone{Name: "testZone", LabelKey: "testKey"},
				},
			},
			key:            "testKey",
			zone:           "testZone",
			errorExpected:  false,
			expectedResult: "192.168.0.10,192.168.0.20\\&192.168.1.10,192.168.1.20",
		},
		{
			name: "two arrays with one MDM each",
			connectionInfo: []*ArrayConnectionData{
				{
					SystemID:         "testSystemID1",
					Mdm:              "192.168.0.10",
					AvailabilityZone: &AvailabilityZone{Name: "testZone", LabelKey: "testKey"},
				},
				{
					SystemID:         "testSystemID2",
					Mdm:              "192.168.1.10",
					AvailabilityZone: &AvailabilityZone{Name: "testZone", LabelKey: "testKey"},
				},
			},
			key:            "testKey",
			zone:           "testZone",
			errorExpected:  false,
			expectedResult: "192.168.0.10\\&192.168.1.10",
		},
		{
			name: "two arrays with multiple zones 1",
			connectionInfo: []*ArrayConnectionData{
				{
					SystemID:         "testSystemID1",
					Mdm:              "192.168.0.10,192.168.0.20",
					AvailabilityZone: &AvailabilityZone{Name: "testZone1", LabelKey: "testKey"},
				},
				{
					SystemID:         "testSystemID2",
					Mdm:              "192.168.1.10,192.168.1.20",
					AvailabilityZone: &AvailabilityZone{Name: "testZone2", LabelKey: "testKey"},
				},
			},
			key:            "testKey",
			zone:           "testZone1",
			errorExpected:  false,
			expectedResult: "192.168.0.10,192.168.0.20",
		},
		{
			name: "two arrays with multiple zones 2",
			connectionInfo: []*ArrayConnectionData{
				{
					SystemID:         "testSystemID1",
					Mdm:              "192.168.0.10,192.168.0.20",
					AvailabilityZone: &AvailabilityZone{Name: "testZone1", LabelKey: "testKey"},
				},
				{
					SystemID:         "testSystemID2",
					Mdm:              "192.168.1.10,192.168.1.20",
					AvailabilityZone: &AvailabilityZone{Name: "testZone2", LabelKey: "testKey"},
				},
			},
			key:            "testKey",
			zone:           "testZone2",
			errorExpected:  false,
			expectedResult: "192.168.1.10,192.168.1.20",
		},
		{
			name: "two arrays in same zone with different keys 1",
			connectionInfo: []*ArrayConnectionData{
				{
					SystemID:         "testSystemID1",
					Mdm:              "192.168.0.10,192.168.0.20",
					AvailabilityZone: &AvailabilityZone{Name: "testZone", LabelKey: "testKey1"},
				},
				{
					SystemID:         "testSystemID2",
					Mdm:              "192.168.1.10,192.168.1.20",
					AvailabilityZone: &AvailabilityZone{Name: "testZone", LabelKey: "testKey2"},
				},
			},
			key:            "testKey1",
			zone:           "testZone",
			errorExpected:  false,
			expectedResult: "192.168.0.10,192.168.0.20",
		},
		{
			name: "two arrays in same zone with different keys 2",
			connectionInfo: []*ArrayConnectionData{
				{
					SystemID:         "testSystemID1",
					Mdm:              "192.168.0.10,192.168.0.20",
					AvailabilityZone: &AvailabilityZone{Name: "testZone", LabelKey: "testKey1"},
				},
				{
					SystemID:         "testSystemID2",
					Mdm:              "192.168.1.10,192.168.1.20",
					AvailabilityZone: &AvailabilityZone{Name: "testZone", LabelKey: "testKey2"},
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
			if (err != nil) != test.errorExpected {
				t.Errorf("getMdmList() error: '%v', expected: '%v'", err, test.errorExpected)
				return
			}
			if !reflect.DeepEqual(test.expectedResult, result) {
				t.Errorf("getMdmList() = '%v', expected '%v'", result, test.expectedResult)
			}
		})
	}
}

func TestGetLabelKey(t *testing.T) {
	tests := []struct {
		name           string
		connectionInfo []*ArrayConnectionData
		errorExpected  bool
		expectedResult string
	}{
		{
			name:           "no connection info",
			connectionInfo: []*ArrayConnectionData{},
			errorExpected:  true,
			expectedResult: "array connection data is empty",
		},
		{
			name: "zone is empty",
			connectionInfo: []*ArrayConnectionData{
				{
					SystemID: "testSystemID1",
				},
				{
					SystemID: "testSystemID2",
				},
			},
			errorExpected:  false,
			expectedResult: "",
		},
		{
			name: "zone is not empty with different keys",
			connectionInfo: []*ArrayConnectionData{
				{
					SystemID:         "testSystemID1",
					AvailabilityZone: &AvailabilityZone{Name: "testZone", LabelKey: "testKey1"},
				},
				{
					SystemID:         "testSystemID2",
					AvailabilityZone: &AvailabilityZone{Name: "testZone", LabelKey: "testKey2"},
				},
			},
			errorExpected:  true,
			expectedResult: "label key is not the same for all arrays",
		},
		{
			name: "mix of empty and non empty zones",
			connectionInfo: []*ArrayConnectionData{
				{
					SystemID: "testSystemID1",
				},
				{
					SystemID:         "testSystemID2",
					AvailabilityZone: &AvailabilityZone{Name: "testZone", LabelKey: "testKey1"},
				},
			},
			errorExpected:  true,
			expectedResult: "label key is not the same for all arrays",
		},
		{
			name: "same key in all zones",
			connectionInfo: []*ArrayConnectionData{
				{
					SystemID:         "testSystemID1",
					AvailabilityZone: &AvailabilityZone{Name: "testZone", LabelKey: "testKey1"},
				},
				{
					SystemID:         "testSystemID2",
					AvailabilityZone: &AvailabilityZone{Name: "testZone", LabelKey: "testKey1"},
				},
			},
			errorExpected:  false,
			expectedResult: "testKey1",
		},
		{
			name: "case sensitivity test for key",
			connectionInfo: []*ArrayConnectionData{
				{
					SystemID:         "testSystemID1",
					AvailabilityZone: &AvailabilityZone{Name: "testZone", LabelKey: "testKey1"},
				},
				{
					SystemID:         "testSystemID2",
					AvailabilityZone: &AvailabilityZone{Name: "testZone", LabelKey: "TestKey1"},
				},
			},
			errorExpected:  true,
			expectedResult: "label key is not the same for all arrays",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := getLabelKey(test.connectionInfo)
			if (err != nil) != test.errorExpected {
				t.Errorf("getLabelKey() error: '%v', expected: '%v'", err, test.errorExpected)
				return
			}

			if err == nil && !reflect.DeepEqual(test.expectedResult, result) {
				t.Errorf("getLabelKey() = '%v', expected '%v'", result, test.expectedResult)
			}
		})
	}
}
