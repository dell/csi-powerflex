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
# limitations under the License

# This will run coverage analysis using the integration testing.
# The env.sh must point to a valid VxFlexOS deployment and and SDC must be installed
# on this system. This will make real calls to the SIO.
# NOTE: you must run this as root, as the plugin cannot retrieve the SdcGUID without being root!

sh validate_http_unauthorized.sh
rc=$?
if [ $rc -ne 0 ]; then echo "failed http unauthorized test"; exit $rc; fi

rm -f unix.sock
source ../../env.sh
echo $SDC_GUID
GOOS=linux CGO_ENABLED=0 GO111MODULE=on go test -v -coverprofile=c.linux.out -timeout 600m -coverpkg=github.com/dell/csi-vxflexos/service *test.go &
if [ -f ./csi-sanity ] ; then
    sleep 5
    ./csi-sanity --csi.endpoint=./unix_sock --csi.testvolumeparameters=./pool.yml --csi.testvolumesize 8589934592
fi
wait

