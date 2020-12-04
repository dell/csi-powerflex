# CSI Driver for Dell EMC PowerFlex Product Guide

**Version 1.3**

**December 2020**

## Product overview

The CSI Driver for Dell EMC PowerFlex is a plug-in that is installed into Kubernetes to provide persistent storage using Dell EMC PowerFlex storage system.

### CSI Driver components

The CSI Driver for Dell EMC PowerFlex has two components:

- Controller plug-in
- Node plug-in

#### Controller plug-in

The Controller plug-in is deployed in a StatefulSet in the Kubernetes cluster with maximum number of replicas set to 1. There is one Pod for the Controller plug-in that is scheduled on any node which is not necessarily the master.

This Pod contains the CSI Driver for Dell EMC PowerFlex container and a few side-car containers like the _provisioner_ , _attacher_ ,_external-snapshotter_ , and _resizer_ that the Kubernetes community provides.

The Controller plug-in primarily deals with provisioning activities such as, creating volumes, deleting volumes, attaching the volume to a node, and detaching the volume from a node. Also, the plug-in deals with creating snapshots, deleting snapshots, and creating a volume from snapshot. The CSI Driver for Dell EMC PowerFlex automates the creation and deletion of Storage Groups (SGs) and Masking Views when required.

#### Node plug-in

The Node plug-in is deployed in a DaemonSet in the Kubernetes cluster. The Node plug-in deploys the Pod containing the driver container on all nodes in the cluster (where the scheduler can schedule the Pod).

The Node plug-in performs tasks such as, identifying, publishing, and unpublishing a volume to the node.

### Capabilities of the CSI Driver for Dell EMC PowerFlex

The CSI Driver for Dell EMC PowerFlex has the following features:

| Capability | Supported | Not supported |
|------------|-----------| --------------|
|Provisioning | Creation, Deletion, Mounting, Unmounting, Online Expansion of PV  | CSI Ephemeral Volumes  |
|Export, Mount | Mount as FileSystem (ext4, xfs) or Raw Block, Topology | |
|Data protection | Creation of snapshots, Create volume from snapshots, Volume Cloning | |
|Types of volumes | Static, Dynamic| |
|Access mode | RWO, ROX, RWX (Raw Block only) | |
|Kubernetes | v1.17, v1.18, v1.19 | V1.16 or previous versions|
|Docker EE | v3.1 | Other versions|
|Installer | Helm v3.x, Operator | Helm v2.x |
|OpenShift | v4.5, v4.6 | Other versions |
|OS | RHEL 7.7-7.9, CentOS 7.6-7.8, Ubuntu 20.04, RHEL CoreOS, SLES 15 SP2 | Other Linux variants |
|PowerFlex | 3.0.x, 3.5.x | Other versions |
|Protocol |  |  |
|CSI Spec | 1.1 | Earlier versions | 
|Other | Controller HA, Volume prefixes, Mount Options |  | 

## Installation

This chapter contains the following sections:
- Installation overview
- Prerequisites
- Install Driver
- Upgrade Driver

### Installation Overview

The CSI Driver for Dell EMC PowerFlex can either be deployed by using Helm v3 charts and installation scripts or by using Dell EMC Storage CSI Operator on both Kubernetes and OpenShift platforms. The CSI Driver repository includes HELM charts that use a shell script to deploy the CSI Driver for Dell EMC PowerFlex. The shell script installs the CSI Driver container image along with the required Kubernetes sidecar containers.

If using a Helm installation, the controller section of the Helm chart installs the following components in a _Stateful Set_ in the namespace `vxflexos`:
- CSI Driver for Dell EMC PowerFlex
- Kubernetes Provisioner, which provisions the volumes
- Kubernetes Attacher, which attaches the volumes to the containers
- Kubernetes Snapshotter, which provides snapshot support
- Kubernetes Resizer, which resizes the volume

The node section of the Helm chart installs the following component in a _Daemon Set_ in the namespace _vxflexos_:
- CSI Driver for Dell EMC PowerFlex
- Kubernetes Registrar, which handles the driver registration

### Prerequisites

Before you install CSI Driver for Dell EMC PowerFlex, verify the requirements that are mentioned in this topic are installed and configured:
- Install either Kubernetes version 1.17, 1.18, or 1.19, or OpenShift version 4.5 or 4.6.
- Install Helm 3 (if using Helm for driver deployment)
- Install Dell CSI Operator (if using Operator for driver deployment)
- A user must exist on the array with a role _>= FrontEndConfigure_
- Verify that zero padding is enabled on the PowerFlex storage pools that must be used. Use PowerFlex GUI in the PowerFlex CLI to check this setting. See Dell EMC PowerFlex documentation for more information to configure this setting.
- Configure Docker service (if using Docker)
- Install VxFlex OS SDC


#### Install Helm 3.0

Install Helm 3.0 on the master node before you install the CSI Driver for Dell EMC PowerFlex.

**Steps**

Run the `curl https://raw.githubusercontent.com/helm/helm/master/scripts/get-helm-3 | bash` command to install Helm 3.0.


#### Configure Docker service

Configure the mount propagation in Docker on all Kubernetes nodes before installing the CSI Driver for Dell EMC PowerFlex.

**Steps**

1. Edit the service section of `/etc/systemd/system/multi-user.target.wants/docker.service` file to add the following lines:
   ```bash
   docker.service
   [Service]...
   MountFlags=shared
   ```
2. Restart the docker service with `systemctl daemon-reload` and `systemctl restart docker` on all the nodes.
2. Restart the docker service with systemctl daemon-reload and systemctl restart docker on all the nodes.


#### Install PowerFlex Storage Data Client

Install the PowerFlex Storage Data Client (SDC) on all Kubernetes nodes or with OpenShift on the worker nodes.

For detailed PowerFlex installation procedure, see the _Dell EMC PowerFlex Deployment Guide_. Install the PowerFlex SDC as follows:

**Steps**

1. Download the PowerFlex SDC from Dell EMC Online support. The filename is EMC-ScaleIO-sdc-*.rpm, where * is the SDC name corresponding to the PowerFlex installation version.
2. Export the shell variable _MDM_IP_ in a comma-separated list using `export MDM_IP=xx.xxx.xx.xx,xx.xxx.xx.xx`, where xxx represents the actual IP address in your environment. This list contains the IP addresses of the MDMs.
3. Install the SDC per the _Dell EMC PowerFlex Deployment Guide_:
    - For Red Hat Enterprise Linux and Cent OS, run `rpm -iv ./EMC-ScaleIO-sdc-*.x86_64.rpm`, where * is the SDC name corresponding to the PowerFlex installation version.

#### Mount Options (Optional)

A user is able to specify additional mount options as needed for the driver. 
 
Mount options are specified in storageclass yaml under _mountOptions_. 

*_NOTE_*: Before utilizing mount options, you must first be fully aware of the potential impact and understand your environment's requirements for the specified option.

### Install CSI Driver for Dell EMC PowerFlex using HELM

Procedure to install CSI Driver for Dell EMC PowerFlex using HELM.

**Prerequisites**

Ensure that you meet the following prerequisites before you install the CSI Driver for Dell EMC PowerFlex:
- You have installed the SDCs on the worker nodes.
- You have the Helm chart from the Dell EMC GitHub repository, ready for this procedure.
- The dell-csi-helm-installer directory contains new scripts: `csi_install.sh` and `csi_uninstall.sh`. These scripts provide a more convenient way to install and uninstall the driver.
- Mount propagation is configured for Docker or other container runtime in all nodes.

*NOTE*: If your Kubernetes cluster does not already have the CRD for snapshot support, see Installing the Volume Snapshot CRDs before installing the driver.


**Steps**

1. Run `git clone https://github.com/dell/csi-vxflexos.git` to clone the git repository
2. Ensure that you've created namespace where you want to install the driver. You can run `kubectl create namespace vxflexos` to create a new one 
3. Edit the `helm/secret.yaml`, point to correct namespace and replace the values for the username and password parameters.
    These values can be obtained using base64 encoding as described in the following example:
    ```bash
    echo -n "myusername" | base64
    echo -n "mypassword" | base64
    ```
   where *myusername* & *mypassword* are credentials for a user with PowerFlex priviledges.
4. Create the secret by running `kubectl create -f secret.yaml` 
5. Collect information from the PowerFlex SDC (Storage Data Client) by executing the get_vxflexos_info.sh script located in the top-level helm directory.  This script shows the _VxFlex OS system ID_ and _MDM IP_ addresses. Make a note of the value for these parameters as they must be entered in the _myvalues.yaml_ file.
    - *NOTE:* Your SDC might have multiple VxFlex OS systems registered. Ensure that you choose the correct values.
6. Copy the default values.yaml file `cd helm && cp .csi-vxflexos/values.yaml myvalues.yaml`
7. Edit the newly created values file and provide values for the following parameters:
    - Set the systemName string variable to the VxFlex OS system name or system ID. This value was obtained by running the get_vxflexos_info.sh script mentioned earlier in this procedure.
    - Set the restGateway string variable to the URL of your system’s REST API Gateway. You can obtain this value from the
VxFlex OS administrator.
    - Set the storagePool string variable to a default (already existing) storage pool name in your VxFlex OS system.
        - New storage pools can be created in VxFlex OS UI and CLI utilities.
    - Set the mdmIP string variable to a comma separated list of MDM IP addresses.
    - Set the volumeNamePrefix string variable so that volumes created by the driver have a default prefix. If one VxFlex OS
system is servicing several different Kubernetes installations or users, these prefixes help you distinguish them.
    - The controllerCount variable is used by advanced users to deploy multiple controller instances. The specified default
value 1 is designed to work as expected. You can modify the value of this variable to set the desired number of CSI
controller replicas.
    - Set the enablelistvolumesnapshot variable false unless instructed otherwise, by Dell EMC support. It causes snapshots
to be included in the CSI operation ListVolumes.
    - The Helm charts create a Kubernetes StorageClass while deploying CSI Driver for Dell EMC VxFlex OS. The StorageClass
section includes following variables:
        - The name string variable defines the name of the Kubernetes storage class that the Helm charts will create. For
example, the vxflexos base name will be used to generate names such as vxflexos and vxflexos-xfs.
        - The isDefault variable (valid values for this variable are true or false) will set the newly created storage class as
default for Kubernetes.
            - Set this value to true only if you expect VxFlex OS to be your principle storage provider, as it will be used in
PersistentVolumeClaims where no storageclass is provided. After installation, you can add custom storage
classes if desired.
            - All strings must be contained within double quotes.
        - The reclaimPolicy string variable defines whether the volumes will be retained or deleted when the assigned pod is destroyed. The valid values for this variable are Retain or Delete.

8. Install the driver using `csi-install.sh` bash script by running `cd ../dell-csi-helm-installer && ./csi-install.sh --namespace vxflexos --values ../helm/myvalues.yaml`

*NOTE:* 
- For detailed instructions on how to run the install scripts, refer to the readme document in the dell-csi-helm-installer folder.
- This script also runs the verify.sh script that is present in the same directory. You will be prompted to enter the credentials for each of the Kubernetes nodes. The verify.sh script needs the credentials to check if SDC has been configured on all nodes. You can also skip the verification step by specifiying the --skip-verify-node option.

### Install using Operator

Starting from version 1.1.4, CSI Driver for Dell EMC PowerFlex can also be installed using the new Dell CSI Operator.

The Dell CSI Operator is a Kubernetes Operator, which can be used to install and manage the CSI Drivers that are provided by Dell EMC for various storage platforms.

This operator is available as a community operator for upstream Kubernetes and can be deployed using [OperatorHub.io](https://operatorhub.io/). It is also available as a community operator for Openshift clusters and can be deployed using OpenShift Container Platform. Both these methods of installation use OLM (Operator Lifecycle Manager).

The operator can also be deployed directly by following the instructions available on [GitHub](https://github.com/dell/dell-csi-operator).

Instructions on how to deploy the CSI Driver for Dell EMC PowerFlex using the operator is available on [GitHub](https://github.com/dell/dell-csi-operator). There are sample manifests provided which can be edited to do an easy installation of the driver.

*NOTE:* The deployment of the driver using the operator does not use any Helm charts. The installation and configuration parameters are slightly different from the ones that are specified by the Helm installer.

Kubernetes Operators make it easy to deploy and manage entire lifecycle of complex Kubernetes applications. Operators use Custom Resource Definitions (CRD) which represents the application and use custom controllers to manage them.

### Upgrade CSI Driver for Dell EMC PowerFlex

You can upgrade the CSI Driver for Dell EMC PowerFlex using Helm or Dell CSI Operator.

#### Update Driver from v1.2 to v1.3 using Helm 
*TODO: Needs REview/UPDATE*
**Steps**
1. Run `git clone https://github.com/dell/csi-vxflexos.git` to clone the git repository and get the v1.3 driver
2. Update values file as needed
2. Run the `csi-install` script with the option _--upgrade_ by running: `cd ../dell-csi-helm-installer && ./csi-install.sh --namespace vxflexos --values ./myvalues.yaml --upgrade`

#### Update Driver from pre-v1.2 to v1.3 using Helm
A direct upgrade of the driver from an older version pre-v1.2 to version 1.3 is not supported because of breaking changes in Kubernetes APIs in the migration from alpha snapshots to beta snapshots. In order to update the driver in this situation you need to remove alpha snapshot related artifacts.

**Steps**
1. Before deleting the alpha snapshot CRDs, ensure that their version is v1alpha1 by examining the output of the `kubectl get crd` command.
2. Delete any VolumeSnapshotClass present in the cluster.
3. Delete all the alpha snapshot CRDs from the cluster by running the following commands:
   ```bash
   kubectl delete crd volumesnapshotclasses.snapshot.storage.k8s.io
   kubectl delete crd volumesnapshotcontents.snapshot.storage.k8s.io
   kubectl delete crd volumesnapshots.snapshot.storage.k8s.io
   ```
4. Uninstall the driver using the `csi-uninstall.sh` script by running the command: ./csi-uninstall.sh --namespace vxflexos
5. Install the driver using the steps described in the above section *Install CSI Driver for Dell EMC PowerFlex using HELM*

*NOTE:*
- If you are upgrading from a driver version which was installed using Helm v2, ensure that you install Helm3 before installing the driver.
- Installation of the CSI Driver for Dell EMC PowerFlex version 1.3 driver is not supported on Kubernetes upstream clusters running version 1.16. You must upgrade your cluster to 1.17, 1.18, or 1.19 before attempting to install the new version of the driver.
- To update any installation parameter after the driver has been installed, change the `myvalues.yaml` file and run the install script with the option --upgrade , for example: `./csi-install.sh --namespace vxflexos --values ./myvalues.yaml --upgrade`

#### Upgrade using Dell CSI Operator:

Follow the instructions for upgrade on the Dell CSI Operator [GitHub](https://github.com/dell/dell-csi-operator) page.


## Volume Snapshot Feature

Starting from version 1.2, the CSI PowerFlex driver supports beta snapshots. Previous versions of the driver supported alpha snapshots.

Volume Snapshots feature in Kubernetes has moved to beta in Kubernetes version 1.17. It was an alpha feature in earlier releases (1.13 onwards). The snapshot API version has changed from v1alpha1 to v1beta1 with this migration.

In order to use Volume Snapshots, ensure the following components have been deployed to your cluster:
- Kubernetes Volume Snaphshot CRDs
- Volume Snapshot Controller

*NOTE:* There isn't support for Volume Snapshots on OpenShift 4.3 (as it is based on upstream Kubernetes v1.16). When using the dell-csi-operator to install the CSI PowerFlex driver on an OpenShift cluster running v4.3, the external-snapshotter sidecar is not installed.

### Installing the Volume Snapshot CRDs

The Kubernetes Volume Snapshot CRDs can be obtained and installed from the external-snapshotter project on Github.

Alternately, you can install the CRDs by supplying the option _--snapshot-crd_ while installing the driver using the new `csi_install.sh` script. If you are installing the driver using the Dell CSI Operator, there is a helper script provided to install the snapshot CRDs - `scripts/install_snap_crds.sh`.

### Installing the Volume Snapshot Controller

Starting with the beta Volume Snapshots, the CSI external-snapshotter sidecar is split into two controllers:
- A common snapshot controller
- A CSI external-snapshotter sidecar

The common snapshot controller must be installed only once in the cluster irrespective the number of CSI drivers installed in the cluster. On OpenShift clusters 4.4 onwards, the common snapshot-controller is pre-installed. In the clusters where it is not present, it can be installed using `kubectl` and the manifests available on [GitHub](https://github.com/kubernetes-csi/external-snapshotter/tree/release-2.1/deploy/kubernetes/snapshot-controller).

*NOTE:*
- The manifests available on GitHub install v2.0.1 of the snapshotter image - [quay.io/k8scsi/snapshot-controller:v2.0.1](https://quay.io/repository/k8scsi/csi-snapshotter?tag=v2.1.1&tab=tags)
- Dell EMC recommends using v2.1.1 image of the snapshot-controller - [quay.io/k8scsi/csi-snapshotter:v2.1.1](https://quay.io/repository/k8scsi/csi-snapshotter?tag=v2.1.1&tab=tags)
- The CSI external-snapshotter sidecar is still installed along with the driver and does not involve any extra configuration.

### Upgrade from v1alpha1 to v1beta

All **v1alpha1** snapshot CRDs must be uninstalled from the cluster before installing the **v1beta1** snapshot CRDs and the common snapshot controller. For more information, see [external-snapshotter documentation](https://github.com/kubernetes-csi/external-snapshotter).

### Volume Snapshot Class

During the installation of CSI PowerFlex 1.3 driver, a Volume Snapshot Class is created using the new **v1beta1** snapshot APIs. This is the only Volume Snapshot Class required and there is no need to create any other Volume Snapshot Class.

Following is the manifest for the Volume Snapshot Class created during installation:
```
apiVersion: snapshot.storage.k8s.io/v1beta
kind: VolumeSnapshotClass
metadata:
  name: vxflexos-snapclass
driver: csi-vxflexos.dellemc.com
deletionPolicy: Delete
```
### Create Volume Snapshot

The following is a sample manifest for creating a Volume Snapshot using the **v1beta1** snapshot APIs:
```
apiVersion: snapshot.storage.k8s.io/v1beta
kind: VolumeSnapshot
metadata:
  name: pvol0-snap
  namespace: helmtest-vxflexos
spec:
  volumeSnapshotClassName: vxflexos-snapclass
  source:
    persistentVolumeClaimName: pvol
```
Once the VolumeSnapshot has been successfully created by the CSI PowerFlex driver, a VolumeSnapshotContent object is automatically created. Once the status of the VolumeSnapshot object has the _readyToUse_ field set to _true_ , it is available for use.

Following is the relevant section of VolumeSnapshot object status:
```
status:
  boundVolumeSnapshotContentName: snapcontent-5a8334d2-eb40-4917-83a2-98f238c4bda
  creationTime: "2020-07-16T08:42:12Z"
  readyToUse: true
```

### Creating PVCs with Volume Snapshots as Source

The following is a sample manifest for creating a PVC with a VolumeSnapshot as a source:
```
apiVersion: v
kind: PersistentVolumeClaim
metadata:
  name: restorepvc
  namespace: helmtest-vxflexos
spec:
  storageClassName: vxflexos
  dataSource:
    name: pvol0-snap
    kind: VolumeSnapshot
    apiGroup: snapshot.storage.k8s.io
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 8Gi
```


## Volume Cloning Feature
Starting from version 1.3, the CSI PowerFlex driver supports volume cloning. This allows specifying existing PVCs in the _dataSource_ field to indicate a user would like to clone a Volume.

The source PVC must be bound and available (not in use). Source and destination PVC must be in the same namespace and have the same Storage Class.

To clone a volume, you should first have an existing pvc, eg, pvol0:
```
kind: PersistentVolumeClaim
apiVersion: v1
metadata:
  name: pvol0
  namespace: helmtest-vxflexos
spec:
  storageClassName: vxflexos
  accessModes:
  - ReadWriteOnce
  volumeMode: Filesystem
  resources:
    requests:
      storage: 8Gi
```

The following is a sample manifest for cloning pvol0:
```
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: clonedpvc
  namespace: helmtest-vxflexos
spec:
  storageClassName: vxflexos
  dataSource:
    name: pvol0
    kind: PersistentVolumeClaim
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 8Gi
```


## Volume Expansion Feature

Starting from version 1.2, the CSI PowerFlex driver supports expansion of Persistent Volumes. This expansion is done online, that is, when PVC is attached to a node.

In order to use this feature, the storage class used to create the PVC needs to have the attribute _allowVolumeExpansion_ set to _true_. The storage classes created during the installation (both using Helm or dell-csi-operator) have the _allowVolumeExpansion_ set to _true_ by default.

In case you are creating more storage classes, make sure that this attribute is set to _true_ if you wish to expand any Persistent Volumes created using these new storage classes.

Following is a sample manifest for a storage class which allows for Volume Expansion:
```
apiVersion: storage.k8s.io/v
kind: StorageClass
metadata:
  name: vxflexos-expand
  annotations:
provisioner: csi-vxflexos.dellemc.com
reclaimPolicy: Delete
allowVolumeExpansion: true
parameters:
  storagepool: pool
volumeBindingMode: WaitForFirstConsumer
allowedTopologies:
- matchLabelExpressions:
  - key: csi-vxflexos.dellemc.com/sample
    values:
    - csi-vxflexos.dellemc.com
```
To resize a PVC, edit the existing PVC spec and set _spec.resources.requests.storage_ to the intended size.

For example, if you have a PVC - pvol0 of size 8Gi, then you can resize it to 16 Gi by updating the PVC:
```
spec:
  accessModes:
  - ReadWriteOnce
  resources:
    requests:
      storage: 16Gi #update from 8Gi
  storageClassName: vxflexos
  volumeMode: Filesystem
  volumeName: k8s-0e50dada
status:
  accessModes:
  - ReadWriteOnce
  capacity:
    storage: 8Gi
  phase: Bound
```
*NOTE:* Kubernetes Volume Expansion feature cannot be used to shrink a volume and volumes cannot be expanded to a value that is not a multiple of 8. If attempted, the driver will round up. For example, if the above PVC was edited to have a size of 20 Gb, the size would actually be expanded to 24 Gb, the closest multiple of 8.


## Raw Block Support

Starting from version 1.2, the CSI PowerFlex driver supports Raw Block volumes, which are created using the _volumeDevices_ list in the pod template spec with each entry accessing a volumeClaimTemplate specifying a _volumeMode: Block_.

Following is an example configuration of **Raw Block Outline**:

```
kind: StatefulSet
apiVersion: apps/v
metadata:
    name: powerflextest
    namespace: helmtest-vxflexos
spec:
    ...
    spec:
      ...
      containers:
        - name: test
        ...
        volumeDevices:
          - devicePath: "/dev/data0"
            name: pvol
    volumeClaimTemplates:
    - metadata:
        name: pvol
      spec:
        accessModes:
        - ReadWriteOnce
        volumeMode: Block
        storageClassName: vxflexos
        resources:
          requests:
          storage: 8Gi
```
Allowable access modes are _ReadWriteOnce_ , _ReadWriteMany_ , and for block devices that have been previously initialized,
_ReadOnlyMany_.

Raw Block volumes are presented as a block device to the pod by using a bind mount to a block device in the node's file system. The driver does not format or check the format of any file system on the block device. Raw Block volumes do support online Volume Expansion, but it is up to the application to manage reconfiguring the file system (if any) to the new size.

For additional information, see the [Kubernetes Raw Block Volume Support documentation](https://kubernetes.io/docs/concepts/storage/persistent-volumes/#raw-block-volume-support).


## Topology Support

Starting from version 1.2, the CSI PowerFlex driver supports Topology which forces volumes to be placed on worker nodes that have connectivity to the backend storage. This covers use cases where:
- The PowerFlex SDC may not be installed or running on some nodes
- Users have chosen to restrict the nodes on which the CSI driver is deployed.

This Topology support does not include customer defined topology, users cannot create their own labels for nodes and storage classed and expect the labels to be honored by the driver.


### Topology Usage

In order to utilize the Topology feature, the storage classes are modified to specify the _volumeBindingMode_ as _WaitForFirstConsumer_ and to specify the desired topology labels within _allowedTopologies_. This ensures that pod scheduling takes advantage of the topology and be guaranteed that the node selected has access to provisioned volumes.

Storage Class Example with Topology Support:
```
apiVersion: storage.k8s.io/v
kind: StorageClass
metadata:
  annotations:
    meta.helm.sh/release-name: vxflexos
    meta.helm.sh/release-namespace: vxflexos
    storageclass.beta.kubernetes.io/is-default-class: "true"
  creationTimestamp: "2020-05-27T13:24:55Z"
  labels:
    app.kubernetes.io/managed-by: Helm
  name: vxflexos
  resourceVersion: "170198"
  selfLink: /apis/storage.k8s.io/v1/storageclasses/vxflexos
  uid: abb094e6-2c25-42c1-b82e-bd80372e78b
parameters:
  storagepool: pool
provisioner: csi-vxflexos.dellemc.com
reclaimPolicy: Delete
volumeBindingMode: WaitForFirstConsumer
allowedTopologies:
- matchLabelExpressions:
  - key: csi-vxflexos.dellemc.com/6c29fd07674c
    values:
    - csi-vxflexos.dellemc.com
```
For additional information, see the [Kubernetes Topology documentation](https://kubernetes-csi.github.io/docs/topology.html).

*NOTE* In the manifest file of the operator, topology can be enabled on openshift by specifying the system name or _systemid_ in the allowed topologies field. _Volumebindingmode_ is also set to _WaitForFirstConsumer_ by default.

## Testing PowerFlex driver

This section provides multiple methods to test driver functionality in your environment.

### Test deploying a simple pod with PowerFlex storage

Test the deployment workflow of a simple pod on PowerFlex storage.

**Prerequisites**

In the source code, there is a directory that contains examples of how you can use the driver. To use these examples, you must create a _helmtest-vxflexos_ namespace, using `kubectl create namespace helmtest-vxflexos`, before you can start testing. HELM 3 must be installed to perform the tests.

The _starttest.sh_ script is located in the _csi-vxflexos/test/helm_ directory. This script is used in the following procedure to deploy helm charts that test the deployment of a simple pod.

**Steps**

1. Navigate to the test/helm directory, which contains the _starttest.sh_ and the _2vols_ directories. This directory contains a simple Helm chart that will deploy a pod that uses two PowerFlex volumes.
*NOTE:* Helm tests are designed assuming users are using the default _storageclass_ names (_vxflexos_ and _vxflexos-xfs_). If your _storageclass_ names differ from the default values, such as when deploying with the Dell CSI Operator, please update the templates in 2vols accordingly (located in _test/helm/2vols/templates_ directory). You can use `kubectl get sc` to check for the _storageclass_ names.
2. Run `sh starttest.sh 2vols` to deploy the pod. You should see the following:
```
Normal Pulled  38s kubelet, k8s113a-10-247-102-215.lss.emc.com Successfully pulled image "docker.io/centos:latest"
Normal Created 38s kubelet, k8s113a-10-247-102-215.lss.emc.com Created container
Normal Started 38s kubelet, k8s113a-10-247-102-215.lss.emc.com Started container
/dev/scinib 8125880 36852 7653216 1% /data
/dev/scinia 16766976 32944 16734032 1% /data
/dev/scinib on /data0 type ext4 (rw,relatime,data=ordered)
/dev/scinia on /data1 type xfs (rw,relatime,attr2,inode64,noquota)
```
3. To stop the test, run `sh stoptest.sh 2vols`. This script deletes the pods and the volumes depending on the retention setting you have configured.

**Results**

An outline of this workflow is described below:
1. The _2vols_ helm chart contains two PersistentVolumeClaim definitions, one in _pvc0.yaml_ , and the other in _pvc1_ .yaml. They are referenced by the _test.yaml_ which creates the pod. The contents of the _Pvc0.yaml_ file are described below:
```
kind: PersistentVolumeClaim
apiVersion: v
metadata:
  name: pvol
  namespace: helmtest-vxflexos
spec:
  accessModes:
  - ReadWriteOnce
  volumeMode: Filesystem
  resources:
    requests:
      storage: 8Gi
  storageClassName: vxflexos
```
2. The _volumeMode: Filesystem_ requires a mounted file system, and the _resources.requests.storage_ of 8Gi requires an 8 GB file. In this case, the _storageClassName: vxflexos_ directs the system to use one of the pre-defined storage classes created by the CSI Driver for Dell EMC PowerFlex installation process. This step yields a mounted _ext4_ file system. You can see the storage class definitions in the PowerFlex installation helm chart files _storageclass.yaml_ and _storageclass-xfs.yaml_.
3. If you compare _pvol0.yaml_ and _pvol1.yaml_ , you will find that the latter uses a different storage class; _vxflexos-xfs_. This class gives you an _xfs_ file system.
4. To see the volumes you created, run kubectl get persistentvolumeclaim –n helmtest-vxflexos and kubectl describe persistentvolumeclaim –n helmtest-vxflexos.
*NOTE:* For more information about Kubernetes objects like _StatefulSet_ and _PersistentVolumeClaim_ see [Kubernetes documentation: Concepts](https://kubernetes.io/docs/concepts/).

### Test creating snapshots

Test the workflow for snapshot creation. 

**Steps**

1. Start the _2vols_ container and leave it running.
    - Helm tests are designed assuming users are using the default _storageclass_ names (_vxflexos_ and _vxflexos-xfs_). If your _storageclass_ names differ from the default values, such as when deploying with the Operator, update the templates in 2vols accordingly (located in _test/helm/2vols/templates_ directory). You can use `kubectl get sc` to check for the _storageclass_ names.
    - Helm tests are designed assuming users are using the default _snapshotclass_ name. If your _snapshotclass_ names differ from the default values, update _snap1.yaml_ and _snap2.yaml_ accordingly.
2. Run `sh snaptest.sh` to start the test.

This will create a snapshot of each of the volumes in the container using _VolumeSnapshot_ objects defined in _snap1.yaml_ and
_snap2.yaml_. The following are the contents of _snap1.yaml_:

```
apiVersion: snapshot.storage.k8s.io/v1alpha
kind: VolumeSnapshot
metadata:
  name: pvol0-snap
  namespace: helmtest-vxflexos
spec:
  snapshotClassName: vxflexos-snapclass
  source:
    name: pvol
    kind: PersistentVolumeClaim
```

**Results**

The _snaptest.sh_ script will create a snapshot using the definitions in the _snap1.yaml_ file. The _spec.source_ section contains the volume that will be snapped. For example, if the volume to be snapped is _pvol0_ , then the created snapshot is named _pvol0-snap_.

*NOTE:* The _snaptest.sh_ shell script creates the snapshots, describes them, and then deletes them. You can see your snapshots using `kubectl get volumesnapshot -n test`.

Notice that this _VolumeSnapshot_ class has a reference to a _snapshotClassName: vxflexos-snapclass_. The CSI Driver for Dell EMC PowerFlex installation creates this class as its default snapshot class. You can see its definition in the installation directory file _volumesnapshotclass.yaml_.

### Test restoring from a snapshot

Test the restore operation workflow to restore from a snapshot.

**Prerequisites**

Ensure that you have stopped any previous test instance before performing this procedure.

**Steps**

1. Run `sh snaprestoretest.sh` to start the test.

This script deploys the _2vols_ example, creates a snap of _pvol0_, and then updates the deployed helm chart from the updateddirectory _2vols+restore_. This then adds an additional volume that is created from the snapshot.

*NOTE:*
- Helm tests are designed assuming users are using the default _storageclass_ names (_vxflexos_ and _vxflexos-xfs_). If your _storageclass_ names differ from the default values, such as when deploying with the Dell CSI Operator, update the templates for snap restore tests accordingly (located in _test/helm/2vols+restore/template_ directory). You can use `kubectl get sc` to check for the _storageclass_ names.
- Helm tests are designed assuming users are using the default _snapshotclass_ name. If your _snapshotclass_ names differ from the default values, update _snap1.yaml_ and _snap2.yaml_ accordingly.

**Results**

An outline of this workflow is described below:
1. The snapshot is taken using _snap1.yaml_.
2. _Helm_ is called to upgrade the deployment with a new definition, which is found in the _2vols+restore_ directory. The _csi-    vxflexos/test/helm/2vols+restore/templates_ directory contains the newly created _createFromSnap.yaml_ file. The script then creates a _PersistentVolumeClaim_ , which is a volume that is dynamically created from the snapshot. Then the helm deployment is upgraded to contain the newly created third volume. In other words, when the _snaprestoretest.sh_ creates a new volume with data from the snapshot, the restore operation is tested. The contents of the _createFromSnap.yaml_ are described below:

```
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: restorepvc
  namespace: helmtest-vxflexos
spec:
  storageClassName: vxflexos
  dataSource:
    name: pvol0-snap
    kind: VolumeSnapshot
    apiGroup: snapshot.storage.k8s.io
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 8Gi
```

*NOTE:* The _spec.dataSource_ clause, specifies a source _VolumeSnapshot_ named _pvol0-snap1_ which matches the snapshot's name in _snap1.yaml_.

## Troubleshooting

The following table lists the CSI Driver for Dell EMC PowerFlex troubleshooting scenarios when installing on Kubernetes:

**Table 1. Troubleshooting**

| Symptoms | Prevention, Resolution or Workaround |
|------------|--------------|
| The installation fails with the following error message: <br />```Node xxx does not have the SDC installed```| Install the PowerFlex SDC on listed nodes. The SDC must be installed on all the nodes that needs to pull an image of the driver. |
| When you run the command `kubectl describe pods vxflexos-controller-0 –n vxflexos`, the system indicates that the driver image could not be loaded. | - If on Kubernetes, edit the _daemon.json_ file found in the registry location and add <br />```{ "insecure-registries" :[ "hostname.cloudapp.net:5000" ] }```<br />- If on Openshift, run the command `oc edit image.config.openshift.io/cluster` and add registries to yaml file that is displayed when you run the command.|
|The `kubectl logs -n vxflexos vxflexos-controller-0` driver logs shows that the driver is not authenticated.| Check the username, password, and the gateway IP address for the PowerFlex system.|
|The `kubectl logs vxflexos-controller-0 -n vxflexos driver` logs shows that the system ID is incorrect.| Use the `get_vxflexos_info.sh` to find the correct system ID. Add the system ID to _myvalues.yaml_ script.|
|Defcontext mount option seems to be ignored, volumes still are not being labeled correctly.|Ensure SElinux is enabled on worker node, and ensure your container run time manager is properly configured to be utilized with SElinux.|
|Mount options that interact with SElinux are not working (like defcontext).|Check that your container orchestrator is properly configured to work with SElinux.|

© 2020 Dell Inc. or its subsidiaries. All rights reserved. Dell, EMC, and other trademarks are trademarks of Dell Inc. or its subsidiaries. Other
trademarks may be trademarks of their respective owners.
