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
	"os"
)

const (
	nodeMdmList = "/data/node_mdms.txt"
)

func NewPreInitService() *service {
	return &service{}
}

func (s *service) PreInit() error {

	Log.Infof("PreInit running")

	// Temp for work in progress.
	mdmData := []byte("MDM=10.247.38.50,10.247.38.53")
	Log.Infof("Saving MDM list to %s", nodeMdmList)
	err := os.WriteFile(nodeMdmList, mdmData, 0644)
	return err
}
