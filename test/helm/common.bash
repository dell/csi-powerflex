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

waitOnRunning() {
  TARGET=$(kubectl get pods -n ${NS} | grep "test" | wc -l)
  RUNNING=0
  while [ $RUNNING -ne $TARGET ];
  do
          sleep 10
          TARGET=$(kubectl get pods -n ${NS} | grep "test" | wc -l)
          RUNNING=$(kubectl get pods -n ${NS} | grep "Running" | wc -l)
          date
          echo running $RUNNING / $TARGET
          kubectl get pods -n ${NS}
  done
}

kMajorVersion=$(run_command kubectl version | grep 'Server Version' | sed -E 's/.*v([0-9]+)\.[0-9]+\.[0-9]+.*/\1/')
kMinorVersion=$(run_command kubectl version | grep 'Server Version' |  sed -E 's/.*v[0-9]+\.([0-9]+)\.[0-9]+.*/\1/')
