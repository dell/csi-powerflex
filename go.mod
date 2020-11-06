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
	github.com/gogo/protobuf v1.2.0 // indirect
	github.com/golang/protobuf v1.3.1
	github.com/gorilla/context v1.1.1 // indirect
	github.com/gorilla/mux v1.6.2
	github.com/rexray/gocsi v1.1.0
	github.com/sirupsen/logrus v1.4.2
	github.com/stretchr/testify v1.3.0
	golang.org/x/crypto v0.0.0-20191011191535-87dc89f01550 // indirect
	golang.org/x/net v0.0.0-20200226121028-0de0cce0169b
	google.golang.org/grpc v1.19.0
)
