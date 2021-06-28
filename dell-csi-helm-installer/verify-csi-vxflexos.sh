#!/bin/bash
#
# Copyright (c) 2020 Dell Inc., or its subsidiaries. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#  http://www.apache.org/licenses/LICENSE-2.0

#
# verify-csi-vxflexos method
function verify-csi-vxflexos() {
  verify_k8s_versions "1.19" "1.21"
  verify_openshift_versions "4.6" "4.7"
  verify_namespace "${NS}"
  verify_required_secrets "${RELEASE}-config"
  verify_mdm_secret "${RELEASE}-config"
  verify_sdc_installation
  verify_alpha_snap_resources
  verify_snap_requirements
  verify_helm_3
  verify_configmap "driver-config"
}

# verify each ip format
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

# make MDM secret key with values from each array
# fail if MDM format is not valid
function verify_mdm_secret() {

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
      log error "SDC MDM string could not be determined, add mdm to config.json"
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

# Check if the SDC is installed and the kernel module loaded
function verify_sdc_installation() {
  if [ ${NODE_VERIFY} -eq 0 ]; then
    return
  fi
  log step "Verifying the SDC installation"

  local SDC_MINION_NODES=$(run_command kubectl get nodes -o wide | grep -v -e master -e INTERNAL -e infra | awk ' { print $6; }')

  error=0
  missing=()
  for node in $SDC_MINION_NODES; do
    # check is the scini kernel module is loaded
    run_command ssh ${NODEUSER}@$node "/sbin/lsmod | grep scini" >/dev/null 2>&1
    rv=$?
    if [ $rv -ne 0 ]; then
      missing+=($node)
      error=1
      found_warning "SDC was not found on node: $node"
    fi
  done
  check_error error
}

# verify configmap exists
function verify_configmap() {
  log step "Verifying that configmap exists"

  error=0
  for N in "${@}"; do
    # Make sure the configmap, driver-config exists
    run_command kubectl describe configmap -n vxflexos "${N}" >/dev/null 2>&1
    if [ $? -ne 0 ]; then
      error=1
      found_error "Configmap with logging config does not exist: ${N}"
    fi
  done

  check_error error
}




