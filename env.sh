#!/bin/bash

# Copyright © 2020-2025 Dell Inc. or its subsidiaries. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#      http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

#!/bin/sh
# Copyright © 2019-2022 Dell Inc. or its subsidiaries. All Rights Reserved.
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


export X_CSI_VXFLEXOS_THICKPROVISION=false
export X_CSI_VXFLEXOS_ENABLESNAPSHOTCGDELETE="true"
export X_CSI_VXFLEXOS_ENABLELISTVOLUMESNAPSHOTS="true"
export X_CSI_QUOTA_ENABLED="true"

# Variables for using tests
export CSI_ENDPOINT=`pwd`/unix_sock
export STORAGE_POOL=""
export NFS_STORAGE_POOL=""
export SDC_GUID=$(/bin/emc/scaleio/drv_cfg --query_guid)
# Alternate GUID is for another system for testing expose volume to multiple hosts
export ALT_GUID=
export X_CSI_POWERFLEX_KUBE_NODE_NAME="node1"

# Interface variables
export NODE_INTERFACES="nodeName:interfaceName"

# Node Label variables
export ZONE_LABEL_KEY=""

#Debug variables for goscaleio library
export GOSCALEIO_SHOWHTTP="true"

#If you put the system ID in your config.json, put the
#system's name here, and vice versa. If your instance does not have a name,
#leave this variable blank. 
export ALT_SYSTEM_ID=""

if /sbin/lsmod | grep -q scini; then
  echo "scini module is present, Proceeding to add MDM..."
  MDM=`grep mdm ../../config.json | awk -F":" '{print $2}'`
  for i in $MDM
  do
    IP=$i
    IP=$(echo "$i" | sed "s/\"//g")
    echo "Adding MDM wth IP: $IP"
    /opt/emc/scaleio/sdc/bin/drv_cfg --add_mdm --ip $IP
  done
fi
