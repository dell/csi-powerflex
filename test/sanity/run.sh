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

rm -rf /tmp/csi-staging
rm -rf /tmp/csi-mount

csi-sanity --ginkgo.v --csi.endpoint=./unix_sock --csi.testvolumeexpandsize 25769803776 --csi.testvolumesize 17179869184 --csi.testvolumeparameters=volParams.yaml --csi.secrets=secrets.yaml --ginkgo.skip "pagination should detect volumes added between pages and accept tokens when the last volume from a page is deleted|check the presence of new volumes and absence of deleted ones in the volume list|should fail when the volume is missing"
