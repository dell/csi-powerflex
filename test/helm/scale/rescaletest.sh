#!/bin/bash

# Copyright © 2020-2025 Dell Inc. or its subsidiaries. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#      http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

#!/bin/sh
# Copyright © 2019-2022 Dell Inc. or its subsidiaries. All Rights Reserved.
# 
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#      http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

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

