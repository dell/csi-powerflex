s will run coverage analysis using the integration testing.
# The env.sh must point to a valid Unisphere deployment and the iscsi packages must be installed
# on this system. This will make real calls to  Unisphere

rm -f unix_sock
. ../../env.sh
echo ENDPOINT $X_CSI_VXFLEXOS_ENDPOINT
echo "Starting the csi-vxflexos driver. You should wait until the node setup is complete before running tests."
../../csi-vxflexos

