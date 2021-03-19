module github.com/dell/csi-vxflexos

// In order to run unit tests on Windows, you need a stubbed Windows implementation
// of the gofsutil package. Use the following replace statements if necessary.

//replace github.com/dell/gofsutil => ./gofsutil

//replace github.com/dell/goscaleio => ./goscaleio

//replace github.com/dell/gocsi => ./gocsi

//replace github.com/dell/dell-csi-extensions/podmon => ./dell-csi-extensions/podmon

go 1.15

require (
	github.com/akutz/memconn v0.1.0
	github.com/container-storage-interface/spec v1.2.0
	github.com/cucumber/godog v0.10.0
	github.com/dell/dell-csi-extensions/podmon v0.1.0
	github.com/dell/gocsi v1.2.3
	github.com/dell/gofsutil v1.4.0
	github.com/dell/goscaleio v1.3.0
	github.com/golang/protobuf v1.4.3
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/googleapis/gnostic v0.4.0 // indirect
	github.com/gorilla/context v1.1.1 // indirect
	github.com/gorilla/mux v1.6.2
	github.com/kubernetes-csi/csi-lib-utils v0.7.0
	github.com/sirupsen/logrus v1.4.2
	github.com/stretchr/testify v1.6.1
	golang.org/x/net v0.0.0-20200822124328-c89045814202
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	google.golang.org/grpc v1.29.0
	k8s.io/client-go v0.18.6
	k8s.io/utils v0.0.0-20200912215256-4140de9c8800 // indirect

)
