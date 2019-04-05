module github.com/dell/csi-vxflexos

// In order to run unit tests on Windows, you need a stubbed Windows implementation
// of the gofsutil package. Use the following replace statements if necessary.

// replace github.com/dell/gofsutil => ../gofsutil
// replace github.com/dell/goscaleio => ../goscaleio

require (
	github.com/DATA-DOG/godog v0.7.8
	github.com/akutz/gosync v0.1.0 // indirect
	github.com/akutz/memconn v0.0.0-20180118181749-6d806d44b3dd
	github.com/boltdb/bolt v1.3.1 // indirect
	github.com/container-storage-interface/spec v1.0.0
	github.com/coreos/bbolt v1.3.0 // indirect
	github.com/coreos/etcd v3.3.12+incompatible // indirect
	github.com/coreos/go-semver v0.2.0 // indirect
	github.com/coreos/go-systemd v0.0.0-20181031085051-9002847aa142 // indirect
	github.com/coreos/pkg v0.0.0-20180928190104-399ea9e2e55f // indirect
	github.com/dell/gofsutil v1.0.0
	github.com/dell/goscaleio v1.0.0
	github.com/dgrijalva/jwt-go v3.2.0+incompatible // indirect
	github.com/dustin/go-humanize v1.0.0 // indirect
	github.com/ghodss/yaml v1.0.0 // indirect
	github.com/gogo/protobuf v1.2.0 // indirect
	github.com/golang/go v0.0.0-20181101143845-21d2e15ee1be // indirect
	github.com/golang/groupcache v0.0.0-20181024230925-c65c006176ff // indirect
	github.com/golang/protobuf v1.2.0
	github.com/google/btree v0.0.0-20180813153112-4030bb1f1f0c // indirect
	github.com/gorilla/context v1.1.1 // indirect
	github.com/gorilla/mux v1.6.2
	github.com/gorilla/websocket v1.4.0 // indirect
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway v1.6.2 // indirect
	github.com/jonboulle/clockwork v0.1.0 // indirect
	github.com/prometheus/client_golang v0.9.2 // indirect
	github.com/rexray/gocsi v0.4.1-0.20190130185714-9407f5f4ed38
	github.com/sirupsen/logrus v1.0.4
	github.com/soheilhy/cmux v0.1.4 // indirect
	github.com/stretchr/testify v1.2.2
	github.com/thecodeteam/gosync v0.1.0 // indirect
	github.com/tmc/grpc-websocket-proxy v0.0.0-20171017195756-830351dc03c6 // indirect
	github.com/ugorji/go/codec v0.0.0-20181209151446-772ced7fd4c2 // indirect
	github.com/xiang90/probing v0.0.0-20160813154853-07dd2e8dfe18 // indirect
	golang.org/x/net v0.0.0-20181201002055-351d144fa1fc
	golang.org/x/time v0.0.0-20181108054448-85acf8d2951c // indirect
	google.golang.org/grpc v1.16.0
)
