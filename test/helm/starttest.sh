#!/bin/sh
[ "$1" = "" ] && {
    echo "requires test name as argument"
    exit 2
}
helm install -n test $1
sleep 30
kubectl describe pods -n test
sleep 10
kubectl describe pods -n test
kubectl exec -n test vxflextest-0 -it df | grep data
kubectl exec -n test vxflextest-0 -it mount | grep data
