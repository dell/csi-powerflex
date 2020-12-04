# Release Notes - CSI PowerFlex v1.3.0

## New Features/Changes
- Added support for OpenShift 4.5/4.6 with RHEL and CoreOS worker nodes
- Added automatic SDC deployment on OpenShift CoreOS nodes
- Added support for Red Hat Enterprise Linux (RHEL) 7.9
- Added support for Ubuntu 20.04
- Added support for volume cloning
- Added support for Controller high availability (multiple-controllers)

## Fixed Issues
There are no fixed issues in this release.

## Known Issues

| Issue | Workaround |
|-------|------------|
| Slow volume attached/detach | If your Kubernetes 1.17 or 1.18 cluster has a lot of VolumeAttachment objects, the attach/detach operations will be very slow. This is a known issue and affects all CSI plugins. It is tracked here: CSI VolumeAttachment slows pod startup time. To get around this problem you can upgrade to latest Kubernetes/OpenShift patches, which contains a partial fix: 1.17.8+, 1.18.5+|
