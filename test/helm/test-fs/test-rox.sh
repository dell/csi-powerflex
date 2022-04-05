#1. Create pvc0.yaml, should see new pvc and pv
#2. Create podTest.yaml, should see pod up and running
#3. edit existing pvc, pv to be read only many
#4. create podTest2.yaml, it needs to be brought up on a different worker node than podTest.yaml
#kubectl exec -it -n helmtest-vxflexos  pod-test-ro -- /bin/sh
#should see that ownership didn't transfer through on read write many 


echo kubectl create -f pvc0.yaml
kubectl create -f pvc0.yaml
echo

echo sleep 5
sleep 5
echo

echo kubectl get pvc -n helmtest-vxflexos
kubectl get pvc -n helmtest-vxflexos
echo


pvToFocus=$(kubectl get pvc -n helmtest-vxflexos | sed -n '2 p' |  awk '{print $3}')

echo "kubectl get pv | grep $pvToFocus"
kubectl get pv | grep $pvToFocus
echo 

echo kubectl create -f podTest.yaml
kubectl create -f podTest.yaml
echo 


echo sleep 20
sleep 20
echo


echo kubectl get pods -n helmtest-vxflexos -o wide
kubectl get pods -n helmtest-vxflexos -o wide
echo

echo kubectl exec -it -n helmtest-vxflexos  pod-test-ro -- touch data0/foo
kubectl exec -it -n helmtest-vxflexos  pod-test -- touch data0/foo
echo

echo kubectl exec -it -n helmtest-vxflexos  pod-test-ro -- ls -al | grep data
kubectl exec -it -n helmtest-vxflexos  pod-test -- ls -al | grep data
echo


echo kubectl patch pv $pvToFocus  -p '{"spec": {"accessModes":["ReadOnlyMany"]}}'
kubectl patch pv $pvToFocus  -p '{"spec": {"accessModes":["ReadOnlyMany"]}}'
echo

echo "kubectl get pv | grep $pvToFocus"
kubectl get pv | grep $pvToFocus
echo

echo kubectl delete -f podTest.yaml
kubectl delete -f podTest.yaml
echo

sleep 10


echo kubectl create -f podTest2.yaml
kubectl create -f podTest2.yaml
echo 

echo sleep 20
sleep 20
echo


echo kubectl get pods -n helmtest-vxflexos -o wide
kubectl get pods -n helmtest-vxflexos -o wide
echo

echo kubectl exec -it -n helmtest-vxflexos  pod-test-ro -- touch data0/foo
kubectl exec -it -n helmtest-vxflexos  pod-test-ro -- touch data0/foo
echo

echo kubectl exec -it -n helmtest-vxflexos  pod-test-ro -- ls -al | grep data
kubectl exec -it -n helmtest-vxflexos  pod-test-ro -- ls -al | grep data
echo

echo Test Done, cleaning up

kubectl delete pods  -n helmtest-vxflexos pod-test pod-test-ro
kubectl delete pvc pvol0 -n helmtest-vxflexos

