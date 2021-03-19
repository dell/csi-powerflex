#!/bin/sh
rm -f stop
rm -f log.output

# Replias is the number of replicas for each pool
if [ "$1" = "" ]; then echo "arg: replicas"; exit 2; fi
replicas=$1
Replicas=$1
target=$(expr $replicas \* 3)
Target=$(expr $Replicas \* 3)

# This determines the number of volumes per pod
helmDir=10replicas
# Namespace to be used
ns=helmtest-vxflexos

export cont0="vxflexos-controller-0"
export node1=$(kubectl get pods -o wide -n vxflexos | grep 10.0.0.1 | awk '/vxflexos-node/{print $1;}')
export node2=$(kubectl get pods -o wide -n vxflexos | grep 10.0.0.1 | awk '/vxflexos-node/{print $1;}')

# Replias is the number of replicas for each pool
if [ "$1" = "" ]; then echo "arg: replicas"; exit 2; fi
replicas=$1
Replicas=$1
# Target is the number of pods desired (replicas * number of pools)
target=$(expr $replicas \* 3)
Target=$(expr $Replicas \* 3)

deployPods() {
echo deploying pods replicas $replicas target $target
helm install --set "name=pool1,namespace=$ns,replicas=$replicas,storageClass=vxflexos"  -n pool1 --namespace $ns  $helmDir
helm install --set "name=pool2,namespace=$ns,replicas=$replicas,storageClass=vxflexos-pool2"  -n pool2 --namespace $ns  $helmDir
helm install --set "name=pool3,namespace=$ns,replicas=$replicas,storageClass=vxflexos-pool3"  -n pool3 --namespace $ns  $helmDir
}

rescalePods() {
echo rescaling pods replicas $replicas target $target
helm upgrade --set "name=pool1,namespace=$ns,replicas=$1,storageClass=vxflexos"  pool1 --namespace $ns  $helmDir
helm upgrade --set "name=pool2,namespace=$ns,replicas=$1,storageClass=vxflexos-pool2"  pool2 --namespace $ns  $helmDir
helm upgrade --set "name=pool3,namespace=$ns,replicas=$1,storageClass=vxflexos-pool3"  pool3 --namespace $ns  $helmDir
}

helmDelete() {
echo "Deleting helm charts"
helm delete --purge pool1
helm delete --purge pool2
helm delete --purge pool3
}

printVmsize() {
	kubectl exec $cont0 -n vxflexos --container driver -- ps -eo cmd,vsz,rss | grep csi-vxflexos
	echo -n "$cont0 " >>log.output
	kubectl exec $cont0 -n vxflexos --container driver -- ps -eo cmd,vsz,rss | grep csi-vxflexos >>log.output
	echo -n "$cont0 " >>log.output
	kubectl logs $cont0 -n vxflexos driver | grep statistics | tail -1 >>log.output
	kubectl exec $node1 -n vxflexos --container driver -- ps -eo cmd,vsz,rss | grep csi-vxflexos
	echo -n "$node1 " >>log.output
	kubectl exec $node1 -n vxflexos --container driver -- ps -eo cmd,vsz,rss | grep csi-vxflexos >>log.output
	echo -n "$node1 " >>log.output
	kubectl logs $node1 -n vxflexos driver | grep statistics | tail -1 >>log.output
	kubectl exec $node2 -n vxflexos --container driver -- ps -eo cmd,vsz,rss | grep csi-vxflexos
	echo -n "$node2 " >>log.output
	kubectl exec $node2 -n vxflexos --container driver -- ps -eo cmd,vsz,rss | grep csi-vxflexos >>log.output
	echo -n "$node2 " >>log.output
	kubectl logs $node2 -n vxflexos driver | grep statistics | tail -1 >>log.output
}

waitOnRunning() {
if [ "$1" = "" ]; then echo "arg: target" ; exit 2; fi
target=$1
running=$(kubectl get pods -n $ns | grep "Running" | wc -l)
while [ $running -ne $target ];
do
	running=$(kubectl get pods -n $ns | grep "Running" | wc -l)
	creating=$(kubectl get pods -n $ns | grep "ContainerCreating" | wc -l)
	terminating=$(kubectl get pods -n $ns | grep "Terminating" | wc -l)
	pvcs=$(kubectl get pvc -n $ns | wc -l)
	date
	date >>log.output
	echo running $running creating $creating terminating $terminating pvcs $pvcs
	echo running $running creating $creating terminating $terminating pvcs $pvcs >>log.output
	printVmsize
	sleep 30
done
}

waitOnNoPods() {
count=$(kubectl get pods -n $ns | wc -l)
while [ $count -gt 0 ];
do
	echo "Waiting on all $count pods to be deleted"
	echo "Waiting on all $count pods to be deleted" >>log.output
	sleep 30
	count=$(kubectl get pods -n $ns | wc -l)
	echo pods $count
done
}


deletePvcs() {
force=""
pvcs=$(kubectl get pvc -n $ns | awk '/pvol/ { print $1; }')
echo deleting... $pvcs
for pvc in $pvcs
do
if [ "$force" == "yes" ];
then
	echo kubectl delete --force --grace-period=0 pvc $pvc -n $ns
	kubectl delete --force --grace-period=0 pvc $pvc -n $ns
else
	echo kubectl delete pvc $pvc -n $ns
	echo kubectl delete pvc $pvc -n $ns >>log.output
	kubectl delete pvc $pvc -n $ns
fi
done
}

#
# Longevity test loop. Runs until a "stop" file is found.
#
iter=1
while true;
do
	ts=$(date)
	replicas=$Replicas
	target=$Target
	echo "Longevity test iteration $iter replicas $replicas target $target $ts"
	echo "Longevity test iteration $iter replicas $replicas target $target $ts" >>log.output

	echo "deploying pods" >>log.output
	deployPods
	echo "waiting on running $target" >>log.output
	waitOnRunning $target
	echo "rescaling pods 0" >>log.output
	rescalePods 0
	echo "waiting on running 0" >>log.output
	waitOnRunning 0
	echo "waiting on no pods" >>log.output
	waitOnNoPods

	helmDelete
	deletePvcs

	echo "collecting final statistics"
	printVmsize
	

	echo "Longevity test iteration $iter completed $ts"
	echo "Longevity test iteration $iter completed $ts" >>log.output
	
	if [ -f stop ]; 
	then
		echo "stop detected... exiting"
		exit 0
	fi
	iter=$(expr $iter \+ 1)
done
exit 0
