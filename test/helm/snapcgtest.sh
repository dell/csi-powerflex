#!/bin/sh
rm -f .volsnapclass.yaml
kubectl delete volumesnapshotclass vxflexos-cgsnap -n test

# Collect the list of "Bound" volumes and translate them to VxFlex OS ids
vols=$( kubectl get pv | grep Bound | awk ' { print $1; }' )
ids=""
for avol in $vols;
do
    echo $avol
    ids="${ids} $(kubectl describe persistentvolume $avol | grep VolumeHandle | awk ' { print $2; }')"
done
ids=$(echo $ids | tr ' ' ',' )
echo ids $ids

# Edit the snapshotclass to have a valid VOLUME_ID_LIST
sed  <volumesnapshotclass.yaml s/__VOLUME_ID_LIST__/${ids}/ >.volsnapclass.yaml
cat .volsnapclass.yaml

echo "creating volume snapshot class..."
kubectl create -f .volsnapclass.yaml
sleep 3
echo "creating volume snapshot"
kubectl create -f snapcg.yaml
sleep 20
echo "snapshot created:"
kubectl get volumesnapshot -n test
sleep 60
echo "deleting volume snapshot"
kubectl delete -f snapcg.yaml
sleep 20
echo "snapshot deleted:"
kubectl get volumesnapshot -n test
kubectl delete volumesnapshotclass vxflexos-cgsnap -n test
