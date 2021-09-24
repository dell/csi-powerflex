# CSI Driver for Dell EMC PowerFlex

[![Go Report Card](https://goreportcard.com/badge/github.com/dell/csi-vxflexos?style=flat-square)](https://goreportcard.com/report/github.com/dell/csi-vxflexos)
[![License](https://img.shields.io/github/license/dell/csi-vxflexos?style=flat-square&color=blue&label=License)](https://github.com/dell/csi-vxflexos/blob/master/LICENSE)
[![Docker](https://img.shields.io/docker/pulls/dellemc/csi-vxflexos.svg?logo=docker&style=flat-square&label=Pulls)](https://hub.docker.com/r/dellemc/csi-vxflexos)
[![Last Release](https://img.shields.io/github/v/release/dell/csi-vxflexos?label=Latest&style=flat-square&logo=go)](https://github.com/dell/csi-vxflexos/releases)

**Repository for CSI Driver for Dell EMC PowerFlex**

## Description
CSI Driver for PowerFlex is part of the [CSM (Container Storage Modules)](https://github.com/dell/csm) open-source suite of Kubernetes storage enablers for Dell EMC products. CSI Driver for PowerFlex is a Container Storage Interface (CSI) driver that provides support for provisioning persistent storage using Dell EMC PowerFlex storage array. 

It supports CSI specification version 1.3.

This project may be compiled as a stand-alone binary using Golang that, when run, provides a valid CSI endpoint. It also can be used as a precompiled container image.

## Support
For any CSI driver issues, questions or feedback, please follow our [support process](https://github.com/dell/csm/blob/main/docs/SUPPORT.md)

## Building
This project is a Go module (see golang.org Module information for explanation).
The dependencies for this project are in the go.mod file.

To build the source, execute `make clean build`.

To run unit tests, execute `make unit-test`.

To build an image, execute `make docker`.

You can run an integration test on a Linux system by populating the file `env.sh`
with values for your PowerFlex system and then run "make integration-test".

## Runtime Dependencies
The Node portion of the driver can only be run on nodes which have network connectivity to a “`PowerFlex Cluster`” via PowerFlex SDC Client (which is used by the driver). This means that the `scini` kernel module must be loaded. 

Also, if the `X_CSI_VXFLEXOS_SDCGUID` environment variable is not set, the driver will attempt to query the SDC GUID automatically. If this fails, the driver will not run.

## Driver Installation
Please consult the [Installation Guide](https://dell.github.io/csm-docs/docs/csidriver/installation)

## Using driver
Please refer to the section `Testing Drivers` in the [Documentation](https://dell.github.io/csm-docs/docs/csidriver/installation/test/) for more info.

## Documentation
For more detailed information on the driver, please refer to [Container Storage Modules documentation](https://dell.github.io/csm-docs/).
