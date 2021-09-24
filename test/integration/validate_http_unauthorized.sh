#!/bin/sh
source ../../env.sh
rm -rf unix_sock

../../csi-vxflexos --driver-config-params=../../service/features/driver-config/logConfig.yaml --array-config="wrong_config.json" 2>stderr 
grep "Unauthorized" stderr
rc=$?
echo rc $rc
if [ $rc -ne 0 ]; then echo "failed..."; else echo "passed"; fi
exit $rc
