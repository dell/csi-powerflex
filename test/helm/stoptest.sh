#/bin/sh
helm delete --purge test
sleep 10
kubectl get pods -n test
sleep 20
sh deletepvcs.sh -n test
kubectl get persistentvolumes -o wide
