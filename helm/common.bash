#!/bin/bash

# Verify kubeadm and kubectl present
kubectl --help >&/dev/null || {
         echo "kubectl required for installation... exiting"; exit 2
}
kubeadm --help >&/dev/null || {
        echo "kubeadm required for installation... exiting"; exit 2
}

waitOnRunning() {
  TARGET=$(kubectl get pods -n ${NS} | grep ${NS} | wc -l)
  RUNNING=0
  while [ $RUNNING -ne $TARGET ];
  do
          sleep 10
          TARGET=$(kubectl get pods -n ${NS} | grep ${NS} | wc -l)
          RUNNING=$(kubectl get pods -n ${NS} | grep "Running" | wc -l)
          date
          echo running $RUNNING / $TARGET
          kubectl get pods -n ${NS}
  done
}

# Get the kubernetes major and minor version numbers.
kMajorVersion=$(kubectl version | grep 'Server Version' | sed -e 's/^.*Major:"//' -e 's/",.*//')
kMinorVersion=$(kubectl version | grep 'Server Version' | sed -e 's/^.*Minor:"//' -e 's/",.*//')

