#!/bin/bash

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

