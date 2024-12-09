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
	"strings"

	"golang.org/x/net/context"
)

const (
	defaultNodeMdmsFile = "/data/node_mdms.txt"
)

type PreInitService interface {
	PreInit() error
}

func NewPreInitService() PreInitService {
	return &service{}
}

type ArrayConfigurationProvider interface {
	GetArrayConfiguration() ([]*ArrayConnectionData, error)
}

type FileWriterProvider interface {
	WriteFile(filename string, data []byte, perm os.FileMode) error
}

type DefaultArrayConfigurationProvider struct{}

func (s *DefaultArrayConfigurationProvider) GetArrayConfiguration() ([]*ArrayConnectionData, error) {
	arrayConfig, err := getArrayConfig(nil)
	if err != nil {
		return nil, err
	}

	connectionData := make([]*ArrayConnectionData, 0)
	for _, v := range arrayConfig {
		connectionData = append(connectionData, v)
	}

	return connectionData, nil
}

type DefaultFileWriterProvider struct{}

func (s *DefaultFileWriterProvider) WriteFile(filename string, data []byte, perm os.FileMode) error {
	return os.WriteFile(filename, data, perm)
}

var (
	arrayConfigurationProviderImpl ArrayConfigurationProvider = &DefaultArrayConfigurationProvider{}
	fileWriterProviderImpl         FileWriterProvider         = &DefaultFileWriterProvider{}
	nodeMdmsFile                                              = defaultNodeMdmsFile
)

func (s *service) PreInit() error {
	Log.Infof("PreInit running")

	arrayConfig, err := arrayConfigurationProviderImpl.GetArrayConfiguration()
	if err != nil {
		return fmt.Errorf("unable to get array configuration: %v", err)
	}

	labelKey, err := getLabelKey(arrayConfig)
	if err != nil {
		return fmt.Errorf("unable to get zone label key: %v", err)
	}

	var mdmData string

	if labelKey == "" {
		Log.Debug("No zone key found, will configure all MDMs")
		sb := strings.Builder{}
		for _, connectionData := range arrayConfig {
			if connectionData.Mdm != "" {
				if sb.Len() > 0 {
					sb.WriteString("\\&")
				}
				sb.WriteString(connectionData.Mdm)
			}
		}
		mdmData = sb.String()
	} else {
		Log.Infof("Zone key detected, will configure MDMs for this node, key: %s", labelKey)
		nodeLabels, err := s.GetNodeLabels(context.Background())
		if err != nil {
			return fmt.Errorf("unable to get node labels: %v", err)
		}

		zone, ok := nodeLabels[labelKey]

		if ok && zone == "" {
			Log.Errorf("node key found but zone is missing, will not configure MDMs for this node, key: %s", labelKey)
		}

		if zone != "" {
			Log.Infof("zone found, will configure MDMs for this node, zone: %s", zone)
			mdmData, err = getMdmList(arrayConfig, labelKey, zone)
			if err != nil {
				return fmt.Errorf("unable to get MDM list: %v", err)
			}
		}
	}

	Log.Infof("Saving MDM list to %s, MDM=%s", nodeMdmsFile, mdmData)
	err = fileWriterProviderImpl.WriteFile(nodeMdmsFile, []byte(fmt.Sprintf("MDM=%s\n", mdmData)), fs.FileMode(0o444))
	return err
}

// Returns a string with the list of MDM addresses given a
// key and zone. The MDMs for each array is separated by an ampersand.
// The ordering of the MDM addresses is not guaranteed. An error is
// returned if either the key or zone are empty.
func getMdmList(connectionData []*ArrayConnectionData, key, zone string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("key is empty")
	}
	if zone == "" {
		return "", fmt.Errorf("zone is empty")
	}

	sb := &strings.Builder{}
	for _, connectionData := range connectionData {
		if connectionData.Mdm != "" && connectionData.Zone.LabelKey == key && connectionData.Zone.Name == zone {
			if sb.Len() > 0 {
				sb.WriteString("\\&")
			}
			sb.WriteString(connectionData.Mdm)
		}
	}

	return sb.String(), nil
}

// Returns the label key for the given set of array configurations.
// It is expected that the value for labelKey is the same for all arrays.
// An empty string is returned if the labelKey is not present in all arrays.
// An error is returned if the key cannot be determined.
func getLabelKey(connectionData []*ArrayConnectionData) (string, error) {
	if len(connectionData) == 0 {
		return "", fmt.Errorf("array connection data is empty")
	}

	labelKey := connectionData[0].Zone.LabelKey

	for _, v := range connectionData {
		if v.Zone.LabelKey != labelKey {
			return "", fmt.Errorf("zone label key is not the same for all arrays")
		}
	}

	return labelKey, nil
}
