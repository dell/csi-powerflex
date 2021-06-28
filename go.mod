module github.com/dell/csi-vxflexos

// In order to run unit tests on Windows, you need a stubbed Windows implementation
// of the gofsutil package. Use the following replace statements if necessary.

//replace github.com/dell/gofsutil => ./gofsutil

//replace github.com/dell/goscaleio => ./goscaleio

//replace github.com/dell/gocsi => ./gocsi

//replace github.com/dell/dell-csi-extensions/podmon => ./dell-csi-extensions/podmon

//replace github.com/dell/dell-csi-extensions/volumeGroupSnapshot => ./dell-csi-extensions/volumeGroupSnapshot

go 1.16

require (
	github.com/akutz/memconn v0.1.0
	github.com/container-storage-interface/spec v1.3.0
	github.com/cucumber/godog v0.10.0
	github.com/dell/dell-csi-extensions/podmon v0.1.0
	github.com/dell/dell-csi-extensions/volumeGroupSnapshot v0.1.0
	github.com/dell/gocsi v1.3.0
	github.com/dell/gofsutil v1.4.0
	github.com/dell/goscaleio v1.4.0
	github.com/fsnotify/fsnotify v1.4.7
	github.com/golang/protobuf v1.4.3
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/googleapis/gnostic v0.4.0 // indirect
	github.com/gorilla/context v1.1.1 // indirect
	github.com/gorilla/mux v1.6.2
	github.com/kubernetes-csi/csi-lib-utils v0.7.0
	github.com/sirupsen/logrus v1.4.2
	github.com/spf13/viper v1.7.1
	github.com/stretchr/testify v1.7.0
	golang.org/x/net v0.0.0-20201202161906-c7110b5ffcbb
	google.golang.org/grpc v1.29.0
	k8s.io/client-go v0.18.6
	k8s.io/utils v0.0.0-20200912215256-4140de9c8800 // indirect
	sigs.k8s.io/yaml v1.2.0

)
