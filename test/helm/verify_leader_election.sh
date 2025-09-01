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
# limitations under the License.

# To run this test, you must:
#     Not have any other tests currently running on cluster(This test will bring down and revive worker nodes)
#     Have driver installed with more than 1 controller pod
# This test will perform the following in order:
#     Ensure that the pod with lease is accepting requests, while pod without lease isn't
#	  Kill driver container on pod with lease and check that a leader election is triggered
#     Ssh into the lease holder pod's node and reboot it, triggering a  leader election
#	  Ssh into the lease holder pod's node and bring down the network interface (specified by -i flag),  forcing a lease transfer



function print_help(){
	echo '''This script tests controller-HA support and leader election for a driver
        Arguments:
        --namespace namespace driver is deployed in, for example: --namespace vxflexos
        --interface network interface to bring down during network fail test, for example: --interface ens192
        --time-down time to keep network interface down for during network fail test, in seconds. For example: --time-down 60
        --user user usually either "root" or "core". For example: --user root
        --skip-docker set this flag if docker is not the container engine being used. Docker tests need to be skipped for now
	--safe-mode set this flag to skip reboot step 

        for upstream k8s:
        ./verify_leader_election.sh --namespace vxflexos --interface ens192 --time-down 120 --user root
        will test the vxflexos driver leader election, and for the network fail test, interface ens192, while using root user

        for openshift:
         ./verify_leader_election.sh --namespace test-vxflexos --interface ens192 --time-down 120 --user core --skip-docker
        will test the driver leader election using core user, and will skip the docker tests since podman is specified
        will be brought down for 60 seconds
        '''

}


# This method gets the current holder of the lease, the node the holder of the lease is on, and the number of times Leader Election has
# been called. These values change frequently throughout the tests.
function get_holder() {
	echo
	k_version=$(kubectl version | grep 'Server Version' |  sed -E 's/.*v[0-9]+\.([0-9]+)\.[0-9]+.*/\1/')
	holder=$(kubectl get lease -n $NAMESPACE | awk '{print $2}' | sed -n '2 p')
	restarts=$(kubectl get pods -n $NAMESPACE -o wide | grep $holder | awk '{print $4}')
	if [ $k_version -ge 22 ] && [ $restarts -gt 0 ]; then
  		 node=$(kubectl get pods -n $NAMESPACE -o wide | grep $holder | awk '{print $9}')
	else
  		 node=$(kubectl get pods -n $NAMESPACE -o wide | grep $holder | awk '{print $7}')
	fi
	old_count=$(kubectl describe lease -n $NAMESPACE csi-vxflexos-dellemc-com | grep LeaderElection | wc -l)
	echo Holder of lease is: $holder on node: $node
}

# This method checks the driver log for a specified string, and fails/passes depending if the string
# was supposed to be found or not
# $1-pod to check, $2-expression to grep for, $3-0 if grep should fail, 1 if grep should pass
function check_driver_logs() {
	holder_lease=$(kubectl logs -n $NAMESPACE $1 driver | grep "${2}" | wc -l)
	if [ $holder_lease -ge 1 ] && [ $3 -eq 1 ]; then
		echo \""${2}"\" found in driver logs for pod $1
		echo
		echo kubectl logs -n $NAMESPACE $1 driver:
		kubectl logs -n $NAMESPACE $1 driver | grep "${2}"
		echo
	elif [ $holder_lease -ge 1 ] && [ $3 -eq 0 ]; then
		echo ERROR: \""${2}"\" should not have been in driver logs for pod $1, but was found
		echo
		echo kubectl logs -n $NAMESPACE $1 driver:
		kubectl logs -n $NAMESPACE $1 driver
		echo
		exit 1
	elif [ $holder_lease -eq 0 ] && [ $3 -eq 0 ]; then
		echo \""${2}"\" not found in driver logs for pod $1
		echo
		echo kubectl logs -n $NAMESPACE $1 driver:
		kubectl logs -n $NAMESPACE $1 driver
		echo
	else
		echo ERROR: \""${2}"\" should have been in driver logs for pod $1, but was not found
		echo
		echo kubectl logs -n $NAMESPACE $1 driver:
		kubectl logs -n $NAMESPACE $1 driver
		echo
		exit 1
	fi
	echo
}

function check_leases() {
	echo
	echo "Current State of Leases: "
	kubectl get lease -n $NAMESPACE
	echo
}

function send() {
	echo
	echo sshpass -p dangerous ssh -o StrictHostKeyChecking=no "$USER@${1}" "sudo ${2}"
	sshpass -p dangerous ssh -o StrictHostKeyChecking=no "$USER@${1}" "sudo ${2}"
}

#1 holder of lease
#ensure lease was transfered
function wait_transfer_lease() {
	new_holder=$(kubectl get lease -n $NAMESPACE | awk '{print $2}' | sed -n '2 p')
	while [ $1 == $new_holder ]; do
		sleep 20
		check_leases $1
		echo Transfering lease...
		new_holder=$(kubectl get lease -n $NAMESPACE | awk '{print $2}' | sed -n '2 p')
	done
}

#ensure a leader election was triggered, $1 = count of leader elections before this method was called
function verify_leader_election() {
	echo
	iteration=0
	new_count=$(kubectl describe lease -n $NAMESPACE csi-vxflexos-dellemc-com | grep LeaderElection | wc -l)
	while (($new_count == $1)); do
		if (($iteration == 60)); then
                	echo "While loop took 60 iterations and leader election was not detected. Exiting..."
                        exit 1
                fi
		echo Waiting for leader election to start...
		sleep 15
		new_count=$(kubectl describe lease -n $NAMESPACE csi-vxflexos-dellemc-com | grep LeaderElection | wc -l)
		iteration=$(($iteration +1))
	done
	echo Event triggered leader election
	kubectl describe lease -n $NAMESPACE csi-vxflexos-dellemc-com | grep LeaderElection | tail -1
	echo
}

while getopts  ":h-:" optchar; do
	case "${optchar}" in
 	-) 
	case "${OPTARG}" in
    	namespace)
      		NAMESPACE="${!OPTIND}"
		OPTIND=$((OPTIND + 1))
      		;;
	interface)
		NET_INT="${!OPTIND}"
		OPTIND=$((OPTIND + 1))
		;;
	time-down)
		TIME_DOWN="${!OPTIND}"
		OPTIND=$((OPTIND + 1))
		;;
	user)
		USER="${!OPTIND}"
		OPTIND=$((OPTIND + 1))
		;;
	skip-docker) 
		SKIPD="true"
		;;
	safe-mode)
		SAFE="true"
		;; 
	 *)
   		echo "Unknown option -${OPTARG}"
    		echo "For help, run  -h"
    		exit 1
    		;;
  	esac
	;; 
	
	 h)
                print_help
                exit 0
                ;;
        *)
                echo "Unknown option -${OPTARG}"
                echo "For help, run  -h"
                exit 1
                ;;

	esac
done

# Check current status of driver and leases
kubectl get pods -n $NAMESPACE -o wide
sleep 5
check_leases
get_holder
not_holder=$(kubectl get pods -n $NAMESPACE | grep controller | awk '{print $1}' | grep -v $holder | sed -n '1 p')
echo

# Check if Holder of lease acquired the lease, and a pod that doesn't hold the lease, hasn't acquired it
echo Ensure only pod with lease is accepting requests
check_driver_logs $holder "successfully acquired lease" 1

#docker only tests, will need to figure out how to translate to podman 
if [[ $SKIPD != "true" ]]; then
	# Shutdown lease holder's provisioner container, ensure a leader election is triggered
	echo Will ssh into node: $node to kill controller pod\'s provisioner container, to verify this triggers a leader election
	echo Since kubelet will restart the pod, we cannot check for lease transfer, since the controller pod may regain the lease
	send $node "docker kill \$(docker ps -f \"name=k8s_provisioner_${holder}\" | awk '{print \$1}'| sed -n '2 p')"
	verify_leader_election $old_count

	# Shutdown lease holder's driver container, ensure a leader election is triggered
	get_holder
	echo Will ssh into node: $node to kill controller pod\'s driver container, to verify this triggers a leader election
	echo Since kubelet will restart the pod, we cannot check for lease transfer, since the controller pod may regain the lease
	send $node "docker kill \$(docker ps -f \"name=k8s_driver_${holder}\" | awk '{print \$1}'| sed -n '2 p')"
	verify_leader_election $old_count
fi

if [[ $SAFE != "true" ]]; then
	# Reboot node that the controller pod with lease is assigned to, ensure this triggers a leader election
	get_holder
	echo worker node with pod with lease will be rebooted, to trigger a leader election
	echo ssh into node: $node for reboot
	send $node "reboot"
	verify_leader_election $old_count
	check_leases
	echo Ensure leader election triggered 
	get_holder
	check_driver_logs $holder "successfully acquired lease" 1
	kubectl get pods -n $NAMESPACE -o wide
	echo
else
	echo "Skipping reboot, due to --safe option used"
fi

#Bring down network interface of node that the controller pod with lease is assigned to, verify this triggers a leader election
echo Will now bring down network interface on worker node, to verify leader election triggered
echo ssh into node: $node to bring down network interface: ${NET_INT} for ${TIME_DOWN} seconds
echo This will take a few minutes...
sleep 5
TIME_OUT=$((${TIME_DOWN}+30))
echo timeout ${TIME_OUT} sshpass -p dangerous ssh -o StrictHostKeyChecking=no $USER@$node "sudo ifconfig ${NET_INT} down && sudo sleep ${TIME_DOWN} && sudo  ifconfig ${NET_INT} up && sudo reboot"
timeout ${TIME_OUT} sshpass -p dangerous ssh -o StrictHostKeyChecking=no $USER@$node "sudo ifconfig ${NET_INT} down && sudo sleep ${TIME_DOWN} && sudo ifconfig ${NET_INT} up && sudo reboot"
sleep 60
verify_leader_election $old_count
check_leases
echo Ensure lease is transfered
get_holder
check_driver_logs $holder "successfully acquired lease" 1

#check status of cluster, if test was able to complete these steps, it passed
kubectl get pods -n $NAMESPACE -o wide
echo
echo TEST PASSED
echo
echo Note: pods may not be fully recovered from this test. They should be back in a few minutes
echo
