module github.com/dell/csi-vxflexos

// In order to run unit tests on Windows, you need a stubbed Windows implementation
// of the gofsutil package. Use the following replace statements if necessary.

//replace github.com/dell/gofsutil => ./gofsutil

//replace github.com/dell/goscaleio => ./goscaleio

go 1.13

require (
	github.com/DATA-DOG/godog v0.7.13
	github.com/akutz/memconn v0.1.0
	github.com/container-storage-interface/spec v1.1.0
	github.com/dell/gofsutil v1.4.0
	github.com/dell/goscaleio v1.2.0
	github.com/golang/protobuf v1.4.2
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/googleapis/gnostic v0.4.0 // indirect
	github.com/gorilla/context v1.1.1 // indirect
	github.com/gorilla/mux v1.6.2
	github.com/kr/pretty v0.2.0 // indirect
	github.com/kubernetes-csi/csi-lib-utils v0.7.0
	github.com/rexray/gocsi v1.1.0
	github.com/sirupsen/logrus v1.4.2
	github.com/stretchr/testify v1.5.1
	golang.org/x/lint v0.0.0-20200302205851-738671d3881b // indirect
	golang.org/x/net v0.0.0-20200822124328-c89045814202
	golang.org/x/tools v0.0.0-20201001191422-af0a1b5f3ca7 // indirect
	google.golang.org/grpc v1.27.0
	gopkg.in/check.v1 v1.0.0-20190902080502-41f04d3bba15 // indirect
	k8s.io/client-go v0.18.6
	k8s.io/utils v0.0.0-20200912215256-4140de9c8800 // indirect

)
