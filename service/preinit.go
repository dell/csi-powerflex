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
	"errors"
	"strings"
)

const (
	nodeMdmList = "/data/node_mdms.txt"
)

func NewPreInitService() *service {
	return &service{}
}

func (s *service) PreInit() error {

	Log.Infof("PreInit running")

	// arrayConfig, err := getArrayConfig(context.Background())
	// if err != nil {
	// 	return err
	// }

	// // Temp for work in progress.
	// mdmData := []byte("MDM=192.168.0.10,192.168.0.20")
	// Log.Infof("Saving MDM list to %s", nodeMdmList)
	// err = os.WriteFile(nodeMdmList, mdmData, 0644)
	return nil
}

// Returns a string with the unique comma separated list of MDM addresses given a
// key and zone. The ordering of the MDM addresses is not guaranteed. An error is
// returned if either the key or zone are empty.
func getMdmList(connectionInfo []*ArrayConnectionData, key, zone string) (string, error) {

	if key == "" {
		return "", errors.New("key is empty")
	}
	if zone == "" {
		return "", errors.New("zone is empty")
	}

	sb := &strings.Builder{}
	for _, connectionData := range connectionInfo {
		if connectionData.Mdm != "" && connectionData.Zone.LabelKey == key && connectionData.Zone.Name == zone {
			if sb.Len() > 0 {
				sb.WriteString(",")
			}
			sb.WriteString(connectionData.Mdm)
		}
	}

	return sb.String(), nil
}
