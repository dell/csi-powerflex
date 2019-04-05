#!/bin/sh
echo "System ID         MDPM IPs"
DRV_CFG=/opt/emc/scaleio/sdc/bin/drv_cfg
$DRV_CFG --query_mdms | awk ' /MDM-ID/{ sub("^.*-", "", $10); sub("^.*-", "", $11); sub("^.*-", "", $12); sub(".*-", "", $13); print $2, 'x', $10, $11, $12, $13 }'

