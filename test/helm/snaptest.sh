#!/bin/sh
echo "creating snap1 of pvol0"
kubectl create -f snap1.yaml
sleep 10
kubectl get volumesnapshot -n helmtest-vxflexos
kubectl describe volumesnapshot -n helmtest-vxflexos
sleep 10
echo "creating snap2 of pvol0"
kubectl create -f snap2.yaml
sleep 10
kubectl describe volumesnapshot -n helmtest-vxflexos
sleep 10
echo "deleting snapshots..."
kubectl delete volumesnapshot pvol0-snap1 -n helmtest-vxflexos
sleep 10
kubectl delete volumesnapshot pvol0-snap2 -n helmtest-vxflexos
sleep 10
kubectl get volumesnapshot -n helmtest-vxflexos

