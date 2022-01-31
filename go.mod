module github.com/dell/csi-vxflexos/v2

// In order to run unit tests on Windows, you need a stubbed Windows implementation
// of the gofsutil package. Use the following replace statements if necessary.

replace github.com/dell/gofsutil => ./gofsutil

//replace github.com/dell/goscaleio => ./goscaleio

//replace github.com/dell/gocsi => ./gocsi

//replace github.com/dell/dell-csi-extensions/podmon => ./dell-csi-extensions/podmon

//replace github.com/dell/dell-csi-extensions/volumeGroupSnapshot => ./dell-csi-extensions/volumeGroupSnapshot

go 1.16

require (
	github.com/akutz/memconn v0.1.0
	github.com/container-storage-interface/spec v1.5.0
	github.com/cucumber/godog v0.12.1
	github.com/dell/dell-csi-extensions/podmon v1.0.0
	github.com/dell/dell-csi-extensions/volumeGroupSnapshot v1.0.0
	github.com/dell/gocsi v1.5.0
	github.com/dell/gofsutil v1.7.0
	github.com/dell/goscaleio v1.6.0
	github.com/fsnotify/fsnotify v1.4.9
	github.com/golang/protobuf v1.5.2
	github.com/gorilla/mux v1.8.0
	github.com/kubernetes-csi/csi-lib-utils v0.9.1
	github.com/sirupsen/logrus v1.8.1
	github.com/spf13/viper v1.8.1
	github.com/stretchr/testify v1.7.0
	golang.org/x/net v0.0.0-20211015210444-4f30a5c0130f
	google.golang.org/grpc v1.42.0
	k8s.io/api v0.22.2
	k8s.io/apimachinery v0.22.2
	k8s.io/client-go v0.22.2
	sigs.k8s.io/yaml v1.2.0
)
