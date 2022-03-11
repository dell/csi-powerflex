#!/bin/sh
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
go get github.com/tebeka/go2xunit
GOOS=linux CGO_ENABLED=0 GO111MODULE=on go test -v -coverprofile=c.linux.out -timeout 60m -coverpkg=github.com/dell/csi-vxflexos/service *test.go | /usr/bin/go go2xunit>integration.xml&
if [ -f ./csi-sanity ] ; then
    sleep 5
    ./csi-sanity --csi.endpoint=./unix_sock --csi.testvolumeparameters=./pool.yml --csi.testvolumesize 8589934592
fi
echo "copying integration.xml from " `pwd`
mv integration.xml /root/vxflexos/logs/PowerFlex_Int_test_results.xml
wait

