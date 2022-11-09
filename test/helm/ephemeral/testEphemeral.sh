# Copyright © 2021-2022 Dell Inc. or its subsidiaries. All Rights Reserved.
# 
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#     http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

#This test creates a pod with 2 inline volumes, one ext4, and one xfs. To use, replace the systemName and storagepool fields in sample.yaml
 
kubectl create -f sample.yaml

count=$(kubectl get pods | grep my-csi-app-inline-volume | grep Running | wc -l)
limit=20
iteration=0
time_limit=$[$limit*3]


while [ $count -lt 1 ]; do 
	if [ $iteration -eq $limit ]; then 
			echo "Pod is not ready after $time_limit seconds, timed out"
			exit 2 
	fi
	echo "Waiting for pod to be ready"
	sleep 3
	count=$(kubectl get pods | grep my-csi-app-inline-volume | grep Running | wc -l)
	iteration=$[$iteration +1]
done
 
echo
echo "kubectl get pods | grep my-csi-app-inline-volumes"
kubectl get pods | grep my-csi-app-inline-volumes
echo
echo "kubectl exec my-csi-app-inline-volumes -- mount | grep data"
kubectl exec my-csi-app-inline-volumes -- mount | grep data
echo
echo "Pod ready, writing 1 Gb to  inline vols:"
echo
echo Before:
echo "kubectl exec my-csi-app-inline-volumes -- df | grep data"
kubectl exec my-csi-app-inline-volumes -- df | grep data
echo
echo kubectl exec -it  my-csi-app-inline-volumes -- sh -c "dd bs=1024 count=1048576 </dev/urandom > data0/file"
kubectl exec -it  my-csi-app-inline-volumes -- sh -c "dd bs=1024 count=1048576 </dev/urandom > data0/file"
echo
echo kubectl exec -it  my-csi-app-inline-volumes -- sh -c "dd bs=1024 count=1048576 </dev/urandom > data1/file"
kubectl exec -it  my-csi-app-inline-volumes -- sh -c "dd bs=1024 count=1048576 </dev/urandom > data1/file"
echo
echo After:
echo "kubectl exec my-csi-app-inline-volumes -- df | grep data"
kubectl exec my-csi-app-inline-volumes -- df | grep data
echo
echo Test Passed
echo
echo Cleaning up
kubectl delete -f sample.yaml



