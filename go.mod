module github.com/dell/csi-vxflexos

// In order to run unit tests on Windows, you need a stubbed Windows implementation
// of the gofsutil package. Use the following replace statements if necessary.

// replace github.com/dell/gofsutil => ../gofsutil
// replace github.com/dell/goscaleio => ../goscaleio

require (
	github.com/DATA-DOG/godog v0.7.8
	github.com/akutz/memconn v0.1.0
	github.com/boltdb/bolt v1.3.1 // indirect
	github.com/container-storage-interface/spec v1.1.0
	github.com/dell/gofsutil v1.0.0
	github.com/dell/goscaleio v1.0.0
	github.com/dustin/go-humanize v1.0.0 // indirect
	github.com/gogo/protobuf v1.2.0 // indirect
	github.com/golang/go v0.0.0-20181101143845-21d2e15ee1be // indirect
	github.com/golang/protobuf v1.3.1
	github.com/gorilla/context v1.1.1 // indirect
	github.com/gorilla/mux v1.6.2
	github.com/rexray/gocsi v1.1.0
	github.com/sirupsen/logrus v1.2.0
	github.com/stretchr/testify v1.3.0
	github.com/ugorji/go/codec v0.0.0-20181209151446-772ced7fd4c2 // indirect
	golang.org/x/net v0.0.0-20181220203305-927f97764cc3
	google.golang.org/grpc v1.19.0
)
