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
