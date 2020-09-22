#!/bin/bash
[ "$1" = "" ] && {
    echo "requires test name as argument"
    exit 2
}
NS=helmtest-vxflexos
source ./common.bash

RELEASE=`basename "${1}"`

helm install  -n ${NS} "${RELEASE}"  $1

sleep 30
kubectl describe pods -n ${NS}
waitOnRunning
kubectl describe pods -n ${NS}
kubectl exec -n ${NS} vxflextest-0 -it df | grep data
kubectl exec -n ${NS} vxflextest-0 -it mount | grep data
