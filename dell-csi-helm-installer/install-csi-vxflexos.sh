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

# Validate MDM IP addresses from secret config
# Top-level MDM key in secret is no longer created or used

SDC_ENABLED=${SDC_ENABLED:-false}

function validate_mdm_ips() {
  log smart_step "Validating MDM IP addresses from secret config"
  SECRET=${1}
  VAL=$(kubectl get secret ${SECRET} -n ${NS} -o go-template='{{ .data.config }}')
  if [ "${VAL}" == "" ]; then
    log error "secret ${SECRET} in namespace ${NS} not found"
    return 1
  else
    JSON=$(kubectl get secret ${SECRET} -n ${NS} -o go-template='{{ .data.config }}' | base64 --decode)
    # More robust MDM extraction that handles various YAML formatting styles
    DATA=$(echo "${JSON}" | grep -v '^#' | grep -E '^\s*mdm\s*:\s*' | sed -E 's/^\s*mdm\s*:\s*//' | sed -E 's/^["'\'']//' | sed -E 's/["'\'']$//' | tr -d '\r')
    MDM=$(echo ${DATA} | sed "s/ /\&/g")
    if [ "${MDM}" != "" ]; then
      # Validate IP addresses
      for i in $(echo ${MDM} | tr "&" "\n"); do
        for p in $(echo $i | tr "," "\n"); do
          check_ip $p
          if [ $? -ne 0 ]; then
            log error "MDM IP validation failed for address: $p"
            return 1
          fi
        done
      done
      log smart_step "MDM IP validation successful"
    else
      log error "Secret is not configured properly, check documentation to create secret"
      return 1
    fi
  fi
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
    log error "SDC MDM validation failed. IP address $IP format is not ok"
    return 1
  fi
}

if [[ "${SDC_ENABLED}" == "true" ]]; then
  validate_mdm_ips "${RELEASE}-config"
  if [ $? -ne 0 ]; then
    log error "MDM validation failed, aborting installation"
    exit 1
  fi
  log smart_step "SDC is enabled; MDM will be derived from secret config by mdm-container init container"
else
  log smart_step "SDC installation is disabled, skipping MDM configuration"
fi
