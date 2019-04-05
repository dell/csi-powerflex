#!/bin/sh
if [ "$1" = "" ]; then echo "arg: replicas"; exit 2; fi
replicas=$1
target=$(expr $replicas \* 3)
echo replicas $replicas target $target
helm install --set "name=pool1,replicas=$replicas,storageClass=vxflexos"  -n pool1 --namespace test  50replicas
helm install --set "name=pool2,replicas=$replicas,storageClass=vxflexos-pool2"  -n pool2 --namespace test  50replicas
helm install --set "name=pool3,replicas=$replicas,storageClass=vxflexos-pool3"  -n pool3 --namespace test  50replicas

waitOnRunning() {
if [ "$1" = "" ]; then echo "arg: target" ; exit 2; fi
target=$1
running=$(kubectl get pods -n test | grep "Running" | wc -l)
while [ $running -ne $target ];
do
	running=$(kubectl get pods -n test | grep "Running" | wc -l)
	creating=$(kubectl get pods -n test | grep "ContainerCreating" | wc -l)
	pvcs=$(kubectl get pvc -n test | wc -l)
	date
	date >>log.output
	echo running $running creating $creating pvcs $pvcs
	echo running $running creating $creating pvcs $pvcs >>log.output
	sleep 30
done
}
waitOnRunning $target

sh rescaletest.sh 0 0



