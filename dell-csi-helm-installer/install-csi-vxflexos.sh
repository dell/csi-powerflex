#!/bin/bash
#
#Copyright © 2020-2022 Dell Inc. or its subsidiaries. All Rights Reserved.
 
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#      http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# make MDM secret key with values from each array
# fail if MDM format is not valid
function install_mdm_secret() {
  log smart_step "Get SDC config and make MDM string for multi array support"
  SECRET=${1}
  VAL=$(kubectl get secret ${SECRET} -n ${NS} -o go-template='{{ .data.config }}')
  if [ "${VAL}" == "" ]; then
    log error "secret ${SECRET} in namespace ${NS} not found"
  else
    JSON=$(kubectl get secret ${SECRET} -n ${NS} -o go-template='{{ .data.config }}' | base64 --decode)
    DATA=$(echo "${JSON}" | grep mdm | awk -F "\"" '{ print $(NF-1)}')
    MDM=$(echo ${DATA} | sed "s/ /\&/g")
    if [ "${MDM}" != "" ]; then
      ENC=$(echo ${MDM} | base64 | tr -d "\n")
      KRC=$(kubectl patch secret ${SECRET} -n ${NS} -p "{\"data\": { \"MDM\": \"${ENC}\"}}")
      VAL=$(kubectl get secret ${SECRET} -n ${NS} -o go-template='{{ .data.MDM }}' | base64 --decode)
      log smart_step "SDC MDM value created : ${VAL}"
    else
      log error "Secret is not configured properly, check documentation to create secret"
      exit 2
    fi
  fi

  for i in $(echo ${MDM} | tr "&" "\n"); do
    # check mdm for each array
    for p in $(echo $i | tr "," "\n"); do
      check_ip $p
    done
  done
}

# helper function to check IP validation
function check_ip() {
  IP=${1}
  REGEX="\b([0-9]{1,3}\.){3}[0-9]{1,3}\b"
  ADDR=$(echo $IP | grep -oE ${REGEX})
  RC=$?
  if [[ "$RC" -eq "0" ]]; then
    log smart_step "IP ADDR $ADDR format is ok"
  else
    log error "SDC MDM validation failed. IP address $ip format is not ok"
  fi
}

install_mdm_secret "${RELEASE}-config"
