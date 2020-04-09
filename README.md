# CSI Driver for VxFlex OS

## Description
CSI Driver for VxFlex OS is a Container Storage Interface ([CSI](https://github.com/container-storage-interface/spec))
driver that provides VxFlex OS support. It supports CSI specification version 1.1.

This project may be compiled as a stand-alone binary using Golang that, when
run, provides a valid CSI endpoint. This project can also be built
as a Golang plug-in in order to extend the functionality of other programs.

## Building

This project is a Go module (see golang.org Module information for explanation).
The dependencies for this project are in the go.mod file.

To build the source, execute `make clean build`.

To run unit tests, execute `make unit-test`.

To build a docker image, execute `make docker`.

You can run an integration test on a Linux system by populating the file `env.sh`
with values for your VxFlex OS system and then run "make integration-test".

## Runtime Dependencies
The Node portion of the driver can be run on any node that is configured as a
VxFlex OS SDC. This means that the `scini` kernel module must be loaded. Also,
if the `X_CSI_VXFLEXOS_SDCGUID` environment variable is not set, the driver will
try to query the SDC GUID by executing the binary
`/opt/emc/scaleio/sdc/bin/drv_cfg`. If that binary is not present, the Node
Service cannot be run.

## Installation

Installation in Kubernetes should be done using the `install.vxflexos` script
and accompanying Helm chart in the helm directory. For more information, please refer 
to the `CSI Driver for VxFlex OS Product Guide` and `CSI Driver for VxFlex OS Release Notes`.
The driver will be started in Kubernetes as a result of executing the installation
script.


## Using driver

A number of test helm charts and scripts are found in the directory test/helm.
Product Guide provides descriptions of how to run these and explains how they work.

If you want to interact with the driver directly,
you can use the Container Storage Client (`csc`) program provided via the
[GoCSI](https://github.com/rexray/gocsi) project:

```bash
$ go get github.com/rexray/gocsi
$ go install github.com/rexray/gocsi/csc
```
(This is only recommended for developers.)

Then, have `csc` use the same `CSI_ENDPOINT`, and you can issue commands
to the driver. Some examples...

Get the driver's supported versions and driver info:

```bash
$ ./csc -v 0.1.0 -e csi.sock identity plugin-info
...
"url"="https://github.com/dell/csi-vxflexos"
```

### Parameters
When using the driver, some commands accept additional parameters, some of which
may be required for the command to work, or may change the behavior of the
command. Those parameters are listed here.

* `CreateVolume`: `storagepool` The name of a storage pool *must* be passed
  in the `CreateVolume` command
* `GetCapacity`: `storagepool` *may* be passed in `GetCapacity` command. If it
  is, the returned capacity is the available capacity for creation within the
  given storage pool. Otherwise, it's the capacity for creation within the
  storage cluster.

Passing parameters with `csc` is demonstrated in this `CreateVolume` command:

```bash
$ ./csc -v 0.1.0 c create --cap 1,mount,xfs --params storagepool=pd1pool1 myvol
"6757e7d300000000"
```

## Capable operational modes
The CSI spec defines a set of AccessModes that a volume can have. CSI-ScaleIO
supports the following modes for volumes that will be mounted as a filesystem:

```
// Can only be published once as read/write on a single node,
// at any given time.
SINGLE_NODE_WRITER = 1;

// Can only be published once as readonly on a single node,
// at any given time.
SINGLE_NODE_READER_ONLY = 2;

// Can be published as readonly at multiple nodes simultaneously.
MULTI_NODE_READER_ONLY = 3;
```

This means that volumes can be mounted to either single node at a time, with
read-write or read-only permission, or can be mounted on multiple nodes, but all
must be read-only.

For volumes that are used as block devices, only the following are supported:

```
// Can only be published once as read/write on a single node, at
// any given time.
SINGLE_NODE_WRITER = 1;

// Can be published as read/write at multiple nodes
// simultaneously.
MULTI_NODE_MULTI_WRITER = 5;
```

This means that giving a workload read-only access to a block device is not
supported.

In general, volumes should be formatted with xfs or ext4.

## Support
The CSI Driver for Dell EMC VxFlex OS image available on Dockerhub is officially supported by Dell EMC.

The source code available on Github is unsupported and provided solely under the terms of the license attached to the source code. For clarity, Dell EMC does not provide support for any source code modifications.

For any CSI driver setup, configuration issues, questions or feedback, join the Dell EMC Container community athttps://www.dell.com/community/Containers/bd-p/Containers

For any Dell EMC storage issues, please contact Dell support at: https://www.dell.com/support.
