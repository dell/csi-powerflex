#!/bin/sh
export X_CSI_VXFLEXOS_THICKPROVISION=false
export X_CSI_VXFLEXOS_ENABLESNAPSHOTCGDELETE="true"
export X_CSI_VXFLEXOS_ENABLELISTVOLUMESNAPSHOTS="true"

# Variables for using tests
export CSI_ENDPOINT=`pwd`/unix_sock
export STORAGE_POOL=""
export SDC_GUID=$(/bin/emc/scaleio/drv_cfg --query_guid)
# Alternate GUID is for another system for testing expose volume to multiple hosts
export ALT_GUID=

#Debug variables for goscaleio library
export GOSCALEIO_SHOWHTTP="true"

MDM=`grep mdm ../../config.json | awk -F":" '{print $2}'`
for i in $MDM
do
IP=$i
IP=$(echo "$i" | sed "s/\"//g")
echo $IP
 /opt/emc/scaleio/sdc/bin/drv_cfg --add_mdm --ip $IP
done
