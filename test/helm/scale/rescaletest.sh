#!/bin/sh
if [ "$1" = "" ]; then echo "arg: replicas"; exit 2; fi
replicas=$1
target=$(expr $replicas \* 3)
echo replicas $replicas target $target
helm upgrade --set "name=pool1,namespace=test,replicas=$replicas,storageClass=vxflexos"  pool1 --namespace helmtest-vxflexos  10replicas
helm upgrade --set "name=pool2,namespace=test,replicas=$replicas,storageClass=vxflexos-pool2"  pool2 --namespace helmtest-vxflexos  10replicas
helm upgrade --set "name=pool3,namespace=test,replicas=$replicas,storageClass=vxflexos-pool3"  pool3 --namespace helmtest-vxflexos  10replicas

waitOnRunning() {
if [ "$1" = "" ]; then echo "arg: target" ; exit 2; fi
target=$1
running=$(kubectl get pods -n helmtest-vxflexos | grep "Running" | wc -l)
while [ $running -ne $target ];
do
	running=$(kubectl get pods -n helmtest-vxflexos | grep "Running" | wc -l)
	creating=$(kubectl get pods -n helmtest-vxflexos | grep "ContainerCreating" | wc -l)
	pvcs=$(kubectl get pvc -n helmtest-vxflexos | wc -l)
	date
	date >>log.output
	echo running $running creating $creating pvcs $pvcs
	echo running $running creating $creating pvcs $pvcs >>log.output
	sleep 30
done
}

waitOnRunning $target

