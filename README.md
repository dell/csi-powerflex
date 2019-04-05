# csi-vxflexos
VxFlex OS is a scale-out block storage platform from Dell EMC.  VxFlex OS optimized for scale-out Server SAN or hyper-converged infrastructure. CSI Driver for VxFlex OS allows deployed Pods to access the existing VxFlex OS volumes or dynamically provision new ones. The driver is CSI 1.0 compliant.

Features include:
- CSI 1.0 support​
  - Persistent volume create/list/delete/create-from-snapshot​
  - Snapshot create/delete/list​
  - Volume mount on the worker node​
- HELM charts installer​
- Volume prefix for easy LUN identification​
- Ability to set storagepool per the StorageClass​

Also available on

Prerequisites​ & dependencies:
- SDC installed on the worker node​
- Kubernetes v 1.13 & CentOS 7.3​
- Linux native multi-path​

