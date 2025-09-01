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
# Copyright © 2020-2022 Dell Inc. or its subsidiaries. All Rights Reserved.
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

NS=helmtest-vxflexos
source ./common.bash

echo "installing a 2 volume container"
bash starttest.sh 2vols
echo "done installing a 2 volume container"
echo "marking volume"
kubectl exec -n $NS vxflextest-0 -- touch /data0/orig
kubectl exec -n $NS vxflextest-0 -- ls -l /data0
kubectl exec -n $NS vxflextest-0 -- sync
kubectl exec -n $NS vxflextest-0 -- sync

echo "Calculating checksum of /data0/orig"
data0checksum=$(kubectl exec vxflextest-0 -n $NS -- md5sum /data0/orig)
echo $data0checksum

echo "updating container to add a volume cloned from another volume"
helm upgrade -n $NS 2vols 2vols+clone
echo "waiting for container to upgrade/stabalize"
sleep 20
waitOnRunning

kubectl describe pods -n $NS
kubectl exec -n $NS vxflextest-0 -- df | grep data
kubectl exec -n $NS vxflextest-0 -- mount | grep data
echo "updating container finished"
echo "marking volume"
kubectl exec -n $NS vxflextest-0 -- touch /data2/new
echo "listing /data0"
kubectl exec -n $NS vxflextest-0 -- ls -l /data0
echo "listing /data2"
kubectl exec -n $NS vxflextest-0 -- ls -l /data2

echo "Calculating checksum of the cloned file(/data2/orig)"
data2checksum=$(kubectl exec vxflextest-0 -n $NS -- md5sum /data2/orig)
echo $data2checksum
echo "Comparing checksums"
echo $data0checksum
echo $data2checksum
data0chs=$(echo $data0checksum | awk '{print $1}')
data2chs=$(echo $data2checksum | awk '{print $1}')
if [ "$data0chs" = "$data2chs" ]; then
    echo "Both the checksums match!!!"
else
    echo "Checksums don't match"
fi

sleep 5

echo "deleting container"
bash stoptest.sh 2vols
sleep 5
