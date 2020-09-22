#!/bin/sh
namespace=test
helmsettings="storageclass=vxflexos,snapclass=vxflexos-snapclass"
alias k=kubectl

# arg1=pod name
waitOnRunning() {
	echo "waiting on $1 to reach running"
	running=0
	while [ $running -ne 1 ] ;
	do 
		k get pods -n $namespace | grep $1
		running=$(k get pods -n $namespace | grep $1 | grep Running | wc -l)
		sleep 5
	done
}

# arg1=volumesnapshot name
waitOnSnapshotReady() {
	echo "waiting on $1 to reach ready"
	ready="false"
	while [ "$ready" != "true" ] ;
	do
		name=$(k get volumesnapshot -n $namespace | grep $1 | awk ' { print $1; }')
		ready=$(k get volumesnapshot -n $namespace | grep $1 | awk ' { print $2; }')
		echo $name ready: $ready
		sleep 5
	done
}

# waitOnNoPvc()
waitOnNoPvc() {
	echo "waiting on all pvcs to be deleted from namespace"
	pvcs=$(k get pvc -n $namespace | grep -v NAME | wc -l)
	while [ $pvcs -gt 0 ] ;
	do
		pvcs=$(k get pvc -n $namespace | grep -v NAME | wc -l)
		k get pvc -n $namespace
		sleep 5
	done
}

helm install --set $helmsettings -n $namespace 1vol 1vol
waitOnRunning vol-0

# Write some data into the file system.
echo "k exec -it -n test vxflextest-0 -- tar czvf /data0/data.tgz /usr"
k exec -it -n test vol-0 -- tar czvf /data0/data.tgz /usr
# Sync the data onto the file system
k exec -it -n test vol-0 -- sync
k exec -it -n test vol-0 -- ls -l /data0/data.tgz 
sumA=$(k exec -it -n test vol-0 -- md5sum /data0/data.tgz | awk ' {print $1}')
echo sumA $sumA
k exec -it -n test vol-0 -- sync

helm install --set $helmsettings -n $namespace 1snap 1snap
waitOnSnapshotReady vol0-snap1

helm install --set $helmsettings -n test 1volfromsnap 1volfromsnap
waitOnRunning copy-0
k get pods -n test

echo "Checking the data"
echo "k exec -it -n test copy-0 -- mkdir /tmp/foo"
k exec -it -n test copy-0 -- mkdir /tmp/foo
echo "k exec -it -n test copy-0 -- mount /data0 /tmp/foo"
k exec -it -n test copy-0 -- mount /data0 /tmp/foo
echo "k exec -it -n test copy-0 -- ls -l /tmp/foo/data.tgz"
k exec -it -n test copy-0 -- ls -l /tmp/foo/data.tgz
echo "k exec -it -n test copy-0 -- tar tzvf /tmp/foo/data.tgz | tail -20"
k exec -it -n test copy-0 -- tar tzvf /tmp/foo/data.tgz | tail -20
sumB=$(k exec -it -n test copy-0 -- md5sum /tmp/foo/data.tgz | awk ' {print $1}')

echo sumA $sumA sumB $sumB
if [ "$sumA" != "$sumB" ] ; then
	echo "Different checksums- test failed"
	exit 2
fi

sleep 30
helm delete -n $namespace 1volfromsnap

sleep 30
helm delete -n $namespace 1snap

sleep 30
helm delete -n $namespace 1vol

waitOnNoPvc
