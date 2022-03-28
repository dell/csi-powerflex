#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail

source env-e2e-test.sh
#if [ -z "${VALUES_FILE-}" ]; then
#    echo "!!!! FATAL: VALUES_FILE was not provided"
#    exit 1
#fi
#export E2E_VALUES_FILE=$VALUES_FILE

export GO111MODULE=on
export ACK_GINKGO_DEPRECATIONS=1.16.4
export ACK_GINKGO_RC=true

if ! ( go mod vendor && go get -u github.com/onsi/ginkgo/ginkgo); then
    echo "go mod vendor or go get ginkgo error"
    exit 1
fi

PATH=$PATH:$(go env GOPATH)/bin

OPTS=()

if [ -z "${GINKGO_OPTS-}" ]; then
    OPTS=(-v)
else
    read -ra OPTS <<<"-v $GINKGO_OPTS"
fi

ginkgo -mod=mod "${OPTS[@]}"

# Checking for test status
TEST_PASS=$?
if [[ $TEST_PASS -ne 0 ]]; then
    exit 1
fi
