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

#!/bin/bash
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
# limitations under the License

[ "$1" = "" ] && {
    echo "requires test name as argument"
    exit 2
}
NS=helmtest-vxflexos
source ./common.bash

RELEASE=`basename "${1}"`

helm install -n ${NS} "${RELEASE}"  $1

sleep 30
kubectl describe pods -n ${NS}
waitOnRunning
kubectl describe pods -n ${NS}
kubectl exec -n ${NS} vxflextest-0 -- df | grep data
kubectl exec -n ${NS} vxflextest-0 -- mount | grep data
