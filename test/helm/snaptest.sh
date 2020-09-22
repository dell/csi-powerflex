#!/bin/bash
NS=helmtest-vxflexos
source ./common.bash

echo "creating snap1 of pvol0"
kubectl create -f betasnap1.yaml
sleep 10
kubectl get volumesnapshot -n ${NS}
kubectl describe volumesnapshot -n ${NS}
sleep 10
echo "creating snap2 of pvol0"
kubectl create -f betasnap2.yaml
sleep 10
kubectl describe volumesnapshot -n ${NS}
sleep 10
echo "deleting snapshots..."
kubectl delete volumesnapshot pvol0-snap1 -n ${NS}
sleep 10
kubectl delete volumesnapshot pvol0-snap2 -n ${NS}
sleep 10
kubectl get volumesnapshot -n ${NS}

