#!/bin/sh

#unisphere: https://10.247.73.217:8443
unisphere: https://10.247.73.133:8443

# This should be like https://111.222.333.444
export X_CSI_VXFLEXOS_ENDPOINT=""
export X_CSI_VXFLEXOS_USER=""
export X_CSI_VXFLEXOS_PASSWORD=""
export X_CSI_VXFLEXOS_INSECURE="true"
export X_CSI_VXFLEXOS_SYSTEMNAME=""
export X_CSI_VXFLEXOS_THICKPROVISION=false
export X_CSI_VXFLEXOS_ENABLESNAPSHOTCGDELETE="true"
export X_CSI_VXFLEXOS_ENABLELISTVOLUMESNAPSHOTS="true"

# Variables for using tests
export CSI_ENDPOINT=`pwd`/unix_sock
export STORAGE_POOL=""
export SDC_GUID=$(/bin/emc/scaleio/drv_cfg --query_guid)
# Alternate GUID is for another system for testing expose volume to multiple hosts
export ALT_GUID=""

#Debug variables for goscaleio library
export GOSCALEIO_SHOWHTTP="true"

