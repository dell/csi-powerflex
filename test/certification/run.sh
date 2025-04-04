#!/bin/bash
trap "echo Can't stop now!" INT

TIMESTAMP=$(date +%Y%m%d%H%M%S)
DATA=/home/LUNStress/$TIMESTAMP
TESTIMAGE=$(oc adm release info --image-for=tests)

mkdir $DATA || exit 1

for i in manifest.yaml ocp-manifest.yaml kubeconfig.yaml storageclass.yaml snapclass.yaml; do
    cp $i $DATA/$i
done

podman run -v $DATA:/data:z --rm -it $TESTIMAGE sh -c "KUBECONFIG=/data/kubeconfig.yaml TEST_CSI_DRIVER_FILES=/data/manifest.yaml TEST_OCP_CSI_DRIVER_FILES=/data/ocp-manifest.yaml /usr/bin/openshift-tests run openshift/csi --dry-run" |& tee test.log

tar -C $DATA -czf $TIMESTAMP/results-$TIMESTAMP.tar.gz results *yaml
