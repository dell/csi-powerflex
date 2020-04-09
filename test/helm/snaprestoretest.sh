#!/bin/sh
NS=test
source ./common.bash

echo "installing a 2 volume container"
sh starttest.sh 2vols
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
helm upgrade ${NS} 2vols+restore
echo "waiting for container to upgrade/stabalize"
sleep 20
waitOnRunning
kubectl describe pods -n ${NS}
kubectl exec -n ${NS} vxflextest-0 -it df | grep data
kubectl exec -n ${NS} vxflextest-0 -it mount | grep data
echo "updating container finished"
echo "marking volume"
kubectl exec -n ${NS} vxflextest-0 -- touch /data2/new
echo "listing /data0"
kubectl exec -n ${NS} vxflextest-0 -- ls -l /data0
echo "listing /data2"
kubectl exec -n ${NS} vxflextest-0 -- ls -l /data2
sleep 20
echo "deleting container"
sh stoptest.sh
sleep 5
echo "deleting snap"
kubectl delete volumesnapshot pvol0-snap1 -n ${NS}
