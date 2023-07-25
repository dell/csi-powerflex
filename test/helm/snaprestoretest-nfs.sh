#!/bin/bash
# Copyright © 2019-2022 Dell Inc. or its subsidiaries. All Rights Reserved.
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

NS=helmtest-vxflexos
source ./common.bash

echo "installing a 2 volume container"
bash starttest.sh 2vols-nfs
echo "done installing a 2 volume container"
echo "marking volume"
kubectl exec -n ${NS} vxflextest-0 -- touch /data0/orig
kubectl exec -n ${NS} vxflextest-0 -- ls -l /data0
kubectl exec -n ${NS} vxflextest-0 -- sync
kubectl exec -n ${NS} vxflextest-0 -- sync
echo "creating snap1 of pvol0"
kubectl create -f snap1.yaml
sleep 10
kubectl get volumesnapshot -n ${NS}
echo "updating container to add a volume sourced from snapshot"
helm upgrade -n helmtest-vxflexos 2vols-nfs 2vols+restore-nfs
echo "waiting for container to upgrade/stabalize"
sleep 20
waitOnRunning
kubectl describe pods -n ${NS}
kubectl exec -n ${NS} vxflextest-0 -- df | grep data
kubectl exec -n ${NS} vxflextest-0 -- mount | grep data
echo "updating container finished"
echo "marking volume"
kubectl exec -n ${NS} vxflextest-0 -- touch /data2/new
echo "listing /data0"
kubectl exec -n ${NS} vxflextest-0 -- ls -l /data0
echo "listing /data2"
kubectl exec -n ${NS} vxflextest-0 -- ls -l /data2
sleep 20
echo "deleting container"
bash stoptest.sh 2vols-nfs
sleep 5
echo "deleting snap"
kubectl delete volumesnapshot pvol0-snap1 -n ${NS}
