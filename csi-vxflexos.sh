#!/bin/bash
# Copyright © 2020-2022 Dell Inc. or its subsidiaries. All Rights Reserved.
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

if [ -z "$CSI_ENDPOINT" ]
then 
    echo "Warning: CSI_ENDPOINT is not set" 
else 
    socket_file=$CSI_ENDPOINT 
    if [[ $CSI_ENDPOINT == "unix://"* ]]
    then
        socket_file=$(echo $CSI_ENDPOINT | sed 's/^.\{7\}//')
    fi
    [ -e $socket_file ] && rm $socket_file
fi
exec "/csi-vxflexos"  "$@"
