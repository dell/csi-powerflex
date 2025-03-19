#!/bin/sh
# Copyright © 2019-2025 Dell Inc. or its subsidiaries. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#      http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License

# A 4 to 10 alphanumeric characters unique string to be used as a suffix
# in volume names to help identify the ownership of undeleted volumes in the array.
# Your user name would be a good option.
export VOL_NAME_SUFFIX=

# System name for the default system specified in config.json
export SYSTEM_NAME=
export STORAGE_POOL=
export NFS_STORAGE_POOL=
export PROTECTION_DOMAIN=

# System name for the alternative (non-default) system specified in config.json
export ALT_SYSTEM_NAME=
export ALT_STORAGE_POOL=

# Set to true to print debug messages from goscaleio library
export GOSCALEIO_SHOWHTTP="false"

export X_CSI_VXFLEXOS_THICKPROVISION=false
export X_CSI_VXFLEXOS_ENABLESNAPSHOTCGDELETE="true"
export X_CSI_VXFLEXOS_ENABLELISTVOLUMESNAPSHOTS="true"
# Set to true to enable quota by default for created NFS volumes
export X_CSI_QUOTA_ENABLED="true"