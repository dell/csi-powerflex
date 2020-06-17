#!/bin/sh
while true
do
date
date >>log.output
running=$(kubectl get pods -n helmtest-vxflexos | grep "Running" | wc -l)
creating=$(kubectl get pods -n helmtest-vxflexos | grep "ContainerCreating" | wc -l)
pvcs=$(kubectl get pvc -n helmtest-vxflexos | wc -l)
echo running $running creating $creating pvcs $pvcs
echo running $running creating $creating pvcs $pvcs >>log.output
sleep 30
done

