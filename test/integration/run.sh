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

# This will run coverage analysis using the integration testing.
# The env.sh must point to a valid VxFlexOS deployment and and SDC must be installed
# on this system. This will make real calls to the SIO.
# NOTE: you must run this as root, as the plugin cannot retrieve the SdcGUID without being root!

#sh validate_http_unauthorized.sh
#rc=$?
#if [ $rc -ne 0 ]; then echo "failed http unauthorized test"; exit $rc; fi

rm -f unix_sock
. ../../env.sh
array_config="../../config.json"

# Validation of test configuration parameters set in env.sh and config.json
check_test_config()
{
  config_valid=true

  if echo "$VOL_NAME_SUFFIX" | grep -vEq '^[a-zA-Z0-9]{4,10}$'; then
    echo "Set VOL_NAME_SUFFIX in env.sh to 4-10 alphanumeric characters to help identify volumes that belong to your testing."
    config_valid=false
  fi

  [ ! -f "$array_config" ] && echo "Array config file $array_config not found. Create and populate it." && config_valid=false

  # For NFS tests, check if mount.nfs is installed
  ! which mount.nfs &>/dev/null && echo "mount.nfs is not installed and it is required for NFS tests"  && config_valid=false

  # Check if mkfs.xfs is installed
  ! which mkfs.xfs &>/dev/null && echo "mkfs.xfs is not installed"  && config_valid=false

  ! $config_valid && echo "Invalid test configuration. Review values in env.sh and $array_config." && exit 1 || true

}

print_test_config()
{
  echo
  echo "Test configuration:"
  echo "==================="
  echo "TEST_TAGS           = $TEST_TAGS"
  echo "VOL_NAME_SUFFIX     = $VOL_NAME_SUFFIX"
  echo "STORAGE_POOL        = $STORAGE_POOL"
  echo "NFS_STORAGE_POOL    = $NFS_STORAGE_POOL"
  echo "PROTECTION_DOMAIN   = $PROTECTION_DOMAIN"
  echo "ALT_SYSTEM_NAME     = $ALT_SYSTEM_NAME"
  echo "ALT_STORAGE_POOL    = $ALT_STORAGE_POOL"
  echo "X_CSI_VXFLEXOS_THICKPROVISION            = $X_CSI_VXFLEXOS_THICKPROVISION"
  echo "X_CSI_VXFLEXOS_ENABLESNAPSHOTCGDELETE    = $X_CSI_VXFLEXOS_ENABLESNAPSHOTCGDELETE"
  echo "X_CSI_VXFLEXOS_ENABLELISTVOLUMESNAPSHOTS = $X_CSI_VXFLEXOS_ENABLELISTVOLUMESNAPSHOTS"
  echo "X_CSI_POWERFLEX_KUBE_NODE_NAME           = $X_CSI_POWERFLEX_KUBE_NODE_NAME"
  echo "X_CSI_QUOTA_ENABLED = $X_CSI_QUOTA_ENABLED"
  echo "GOSCALEIO_SHOWHTTP  = $GOSCALEIO_SHOWHTTP"
  echo "NODE_INTERFACES     = $NODE_INTERFACES"
  echo "ZONE_LABEL_KEY      = $ZONE_LABEL_KEY"
  echo "SDC_GUID            = $SDC_GUID"
  echo "CSI_ENDPOINT        = $CSI_ENDPOINT"
  echo
  echo "Array config:"
  cat "$array_config" | sed 's/"password":.*/"password": "*****"/'
  echo
  echo "==================="
  echo
}

# Validate the test configuration
check_test_config && echo "Test configuration and environment validated" || exit 1

# Add MDMs from array config in config.json to the SDC driver
if /sbin/lsmod | grep -q scini; then
  echo "scini module is loaded, adding MDM..."
  MDM=`grep mdm ../../config.json | awk -F":" '{print $2}'`
  for i in $MDM
  do
    IP=$i
    IP=$(echo "$i" | sed "s/\"//g")
    echo "Adding MDM wth IP: $IP"
    /opt/emc/scaleio/sdc/bin/drv_cfg --add_mdm --ip $IP
  done
fi

# Non-configurable environment definitions
export ZONE_LABEL_KEY="zone.csi-vxflexos.dellemc.com"
export CSI_ENDPOINT=`pwd`/unix_sock
export SDC_GUID=$(/opt/emc/scaleio/sdc/bin/drv_cfg --query_guid)
export NODE_INTERFACES="worker-1-6MRQC8xZ3A4zt.domain:ens192"
export X_CSI_POWERFLEX_KUBE_NODE_NAME="node1"


testRun=$1
export TEST_TAGS="$2"

print_test_config

GOOS=linux CGO_ENABLED=0 GO111MODULE=on go test -tags=integration -v -coverprofile=c.linux.out -timeout 180m -coverpkg=github.com/dell/csi-vxflexos/service -run "^$testRun\$\$" &

if [ -f ./csi-sanity ] ; then
  echo "Running csi-sanity test..."
  sleep 5
  ./csi-sanity --csi.endpoint=./unix_sock --csi.testvolumeparameters=./pool.yml --csi.testvolumesize 8589934592
fi
wait

echo "Writing test report to /root/vxflexos/logs/integration.xml"
mv integration*.xml /root/vxflexos/logs/
