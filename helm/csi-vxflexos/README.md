# Dell EMC VxFlex Open Storage CSI Driver
> **NOTE:** Dell EMC VxFlex Open Storage was previously known as _ScaleIO_.

## TL;DR;

Add the repo (if you haven't already):
```bash
$ helm repo add vxflex https://vxflex-os.github.io/charts
```

Install the driver:
```bash
$ helm install --values myvalues.yaml --name vxflexos --namespace vxflexos ./csi-vxflexos
```

## Introduction

This chart bootstraps the VxFlex OS CSI driver on a [Kubernetes](http://kubernetes.io) cluster using the [Helm](https://helm.sh) package manager.

## Prerequisites

- Kubernetes 1.13 or later with feature gates as enabled in the release instructions.
- VxFlex OS storage data client (SDC) deployed and configured on each Kubernetes node
- VxFlex OS REST API gateway (with approved VxFlex certificate)
- VxFlex OS configured storage pool
- You must configure a Kubernetes secret containing the VxFlex OS username and password.

## Installing the Chart

To install the chart with the release name `vxflexos`:

```bash
$ helm install --values myvalues.yaml --name vxflexos --namespace vxflexos ./csi-vxflexos
```
> **Tip**: List all releases using `helm list`

There are a number of required values that must be set either via the command-line or a [`values.yaml`](values.yaml) file. Those values are listed in the configuration section below.

## Uninstalling the Chart

To uninstall/delete the `my-release` deployment:

```bash
$ helm delete vxflexos [--purge]
```

The command removes all the Kubernetes components associated with the chart and deletes the release. The purge option also removes the provisioned release name, so that the name itself can also be reused.

## Configuration

The following table lists the primary configurable parameters of the VxFlex OS driver chart and their default values. More detailed information can be found in the [`values.yaml`](values.yaml) file in this repository.

| Parameter | Description | Required | Default |
| --------- | ----------- | -------- |-------- |
| systemName | Name of the VxFlex system   | true | - |
| restGateway | REST API gateway HTTPS endpoint VxFlex system | true | - |
| storagePool | VxFlex Storage Pool to use with in the Kubernetes storage class | true | - |
| volumeNamePrefix | String to prepend to any volumes created by the driver | false | csivol |
| controllerCount | Number of driver controllers to create | false | 1 |
| storageClass.name | Name of the storage class to be defined | false | vxflex |
| storageClass.isDefault | Whether or not to make this storage class the default | false | true |
| storageClass.reclaimPolicy | What should happen when a volume is removed | false | Delete |

Specify each parameter using the `--set key=value[,key=value]` argument to `helm install` or in a myvalues.yaml file. For example,

```bash
$ helm install --name vxflex-csi --namespace vxflex \
  --set systemName=vxflexos,restGateway=https://123.0.0.1,storagePool=sp \
    vxflex/vxflex-csi
```
Alternatively, a YAML file that specifies the values for the parameters can be provided while installing the chart. For example,

```bash
$ helm install --name vxflexos -f values.yaml vxflex/csi-vxflexos
```

```yaml
# values.yaml

systemName: vxflex
restGateway: 123.0.0.1
storagePool: sp
```

> **Tip**: You can add required parameters and then use the default [`values.yaml`](values.yaml)
