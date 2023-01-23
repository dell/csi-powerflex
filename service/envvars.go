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

const (
	// EnvSDCGUID is the name of the environment variable used to set the
	// GUID of the SDC. This is only used by the Node Service, and removes
	// a need for calling an external binary to retrieve the GUID
	EnvSDCGUID = "X_CSI_VXFLEXOS_SDCGUID"

	// EnvThick is the name of the environment variable used to specify
	// that thick provisioning should be used when creating volumes
	EnvThick = "X_CSI_VXFLEXOS_THICKPROVISIONING"

	// EnvAutoProbe is the name of the environment variable used to specify
	// that the controller service should automatically probe itself if it
	// receives incoming requests before having been probed, in direct
	// violation of the CSI spec
	EnvAutoProbe = "X_CSI_VXFLEXOS_AUTOPROBE"

	// EnvAllowRWOMultiPodAccess is the name of the environment variable that specifies
	// within a single node multiple pods should be able to access the same Filesystem volume with access mode ReadWriteOnce.
	// Multi-node access is still not allowed for ReadWriteOnce Filesystem volumes.
	// Enabling this option techincally violates the CSI 1.3 spec in the NodePublishVolume stating the required error returns.
	EnvAllowRWOMultiPodAccess = "X_CSI_ALLOW_RWO_MULTI_POD_ACCESS"

	// EnvIsHealthMonitorEnabled is the name of the environment variable that specifies if
	// the driver should be reporting on volume condition. To do so, requires the alpha feature gate CSIVolumeHealth set
	// to true in the cluster. If the feature gate is on, this should be enabled. Otherwise, this should be set to false.
	EnvIsHealthMonitorEnabled = "X_CSI_HEALTH_MONITOR_ENABLED"

	// EnvIsSDCRenameEnabled is the name of the environment variable that specifies if the renaming for SDC is to be
	// carried out or not. This is only used by the Node Service.
	EnvIsSDCRenameEnabled = "X_CSI_RENAME_SDC_ENABLED"

	// EnvSDCPrefix is the name of the environment variable used to set the prefix for SDC name. This is only used by
	// the Node Service.
	EnvSDCPrefix = "X_CSI_RENAME_SDC_PREFIX"
)
