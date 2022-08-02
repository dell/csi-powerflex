#!/bin/bash 
# The config.json should have connection data for a valid PowerFlex deployment set as "isDefault": true, and
# the iscsi packages must be installed on this system. This will make real calls to PowerFlex

rm -f unix_sock
source ../../env.sh

echo "Starting the csi-vxflexos driver. You should wait until the node setup is complete before running tests."

../../csi-vxflexos -array-config=<path to config.json> -driver-config-params=<path to driver-config-params.yaml> -kubeconfig=<path to kubeconfig>
