#!/bin/sh
force=no
if [ "$1" == "--force" ];
then
	force=yes
fi

pvcs=$(kubectl get pvc -n test | awk '/pvol/ { print $1; }')
echo deleting... $pvcs
for pvc in $pvcs
do
if [ $force == "yes" ];
then
	echo kubectl delete --force --grace-period=0 pvc $pvc -n test
	kubectl delete --force --grace-period=0 pvc $pvc -n test
else
	echo kubectl delete pvc $pvc -n test
	kubectl delete pvc $pvc -n test
fi
done

