#!/bin/sh
force=no
if [ "$1" == "--force" ];
then
	force=yes
fi

pvcs=$(kubectl get pvc -n helmtest-vxflexos | awk '/pvol/ { print $1; }')
echo deleting... $pvcs
for pvc in $pvcs
do
if [ $force == "yes" ];
then
	echo kubectl delete --force --grace-period=0 pvc $pvc -n helmtest-vxflexos
	kubectl delete --force --grace-period=0 pvc $pvc -n helmtest-vxflexos
else
	echo kubectl delete pvc $pvc -n helmtest-vxflexos
	kubectl delete pvc $pvc -n helmtest-vxflexos
fi
done

