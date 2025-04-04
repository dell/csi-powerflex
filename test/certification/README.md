# OpenShift CSI certification tests

This directory contains information for running the RedHat OpenShift certification tests.

## Requirements

* An OpenShift cluster
  * The number of pods that can be scheduled per node must be configured to 350 or higher. OpenShift sets the limit to 250, but the LUN Stress test will attempt to schedule 260 pods on a single node.
* The driver already installed with controller pods scheduled to master nodes.
* A container runner, e.g. docker or podman.
* A Kubeconfig from the target cluster named `kubeconfig.yaml`.
* A [storage class](https://github.com/dell/csi-powerflex/tree/main/samples/storageclass) named `storageclass.yaml`
* A [volume snapshot class](https://github.com/dell/csi-powerflex/tree/main/samples/volumesnapshotclass) named `snapclass.yaml`

## Running the tests
1. Ensure all YAML manifests are in the same directory.
2. Prepare the following files:
    - `kubeconfig.yaml`
    - `upstream-manifest.yaml`
    - `storageclass.yaml`
    - `snapclass.yaml`
3. Create and prepare `pull-secret.json` with credentials to access the Red Hat catalog.
4. Pull the tester container image that corresponds to your OpenShift cluster version:
    ```bash
    $ podman pull --authfile pull-secret.json $(oc adm release info --image-for=tests)
    ```
    **Note**: There may be a newer [OCP End to End image](https://catalog.redhat.com/search?gs&q=OpenShift%20End-to-End%20Tests) available.
6. The `openshift-tests` binary in the container image can be found at `/usr/bin/openshift-tests`.
   - `/usr/bin/openshift-tests run openshift/csi --dry-run` will list the names of tests that will run.
   - `/usr/bin/openshift-tests run openshift/csi` will run all certification tests.
   - `/usr/bin/openshift-tests run openshift/csi --run=<regexp>` will run a set of monitors and specific tests.
   - `/usr/bin/openshift-tests run openshift/csi run-test <test name>` will run a single test, without any monitors.
7. Prepare `run.sh` for the test(s) you want to execute.

    Example for running the LUN Stress test:
    ```bash
    $ podman run -v $DATA:/data:z --rm -it $TESTIMAGE sh -c "KUBECONFIG=/data/kubeconfig.yaml TEST_CSI_DRIVER_FILES=/data/manifest.yaml TEST_OCP_CSI_DRIVER_FILES=/data/ocp-manifest.yaml /usr/bin/openshift-tests run-test 'External Storage [Driver: csi-vxflexos.dellemc.com] [Testpattern: Dynamic PV (filesystem volmode)] OpenShift CSI extended - SCSI LUN Overflow should use many PVs on a single node [Serial][Timeout:40m]' |& tee test.log
    ```
8. Run inside of the openshift-tests container image:
    ```bash
    $ sh run.sh
    ```

#### Tips:
- More usage on the `openshift-tests` container image can be found [here](https://github.com/openshift/origin/blob/main/test/extended/storage/csi/README.md#usage).
- By default, `X_CSI_MAX_VOLUMES_PER_NODE` is set to 0. Set this value to 350 or higher in the driver daemonset to guarantee the number of volumes that can be published by the controller to the node.
- If mounting errors occur, such as `mount(2) system call failed: Structure needs cleaning`, it may indicate too many files open on the worker node. Diagnose the issue by checking kernel logs or running the lsof command. Rebooting the worker nodes in the cluster will also decrease the number of open files.
