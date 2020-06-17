#/bin/sh
helm delete -n helmtest-vxflexos $1 
sleep 10
kubectl get pods -n helmtest-vxflexos
sleep 20
sh deletepvcs.sh -n helmtest-vxflexos
kubectl get persistentvolumes -o wide
