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
	"strings"

	"golang.org/x/net/context"
)

const (
	nodeMdmsFile = "/data/node_mdms.txt"
)

func NewPreInitService() *service {
	return &service{}
}

func (s *service) PreInit() error {

	Log.Infof("PreInit running")

	arrayConfig, err := getArrayConfig(context.Background())
	if err != nil {
		return err
	}

	connectionData := make([]*ArrayConnectionData, 0)
	for _, v := range arrayConfig {
		connectionData = append(connectionData, v)
	}

	labelKey, err := getLabelKey(connectionData)
	if err != nil {
		return err
	}

	var mdmData string

	if labelKey == "" {
		Log.Debug("No zone key found, will configure all MDMs")
		sb := strings.Builder{}
		for _, connectionData := range connectionData {
			if connectionData.Mdm != "" {
				if sb.Len() > 0 {
					sb.WriteString(",")
				}
				sb.WriteString(connectionData.Mdm)
			}
		}
		mdmData = sb.String()
	} else {
		Log.Infof("Zone key detected, will configure MDMs for this node, key: %s", labelKey)
		nodeLabels, err := s.GetNodeLabels(context.Background())
		if err != nil {
			return err
		}
		zone := nodeLabels[labelKey]

		if zone == "" {
			return fmt.Errorf("No zone found, cannot configure this node")
		} else {
			Log.Infof("Zone found, will configure MDMs for this node, zone: %s", zone)
			mdmData, err = getMdmList(connectionData, labelKey, zone)
			if err != nil {
				return err
			}
		}
	}

	Log.Infof("Saving MDM list to %s, MDM=%s", nodeMdmsFile, mdmData)
	err = os.WriteFile(nodeMdmsFile, []byte(fmt.Sprintf("MDM=%s\n", mdmData)), 0644)
	return err
}

// Returns a string with the comma separated list of MDM addresses given a
// key and zone. The ordering of the MDM addresses is not guaranteed. An error is
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
				sb.WriteString(",")
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
