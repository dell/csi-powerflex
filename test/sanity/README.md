# Sanity Tests for CSI PowerFlex

This test runs the Kubernetes sanity test at https://github.com/kubernetes-csi/csi-test.

## Running

### Installing csi-sanity
1. Clone the Kubernetes [csi-test](https://github.com/kubernetes-csi/csi-test.git).
1. From the [releases](https://github.com/kubernetes-csi/csi-test/releases), checkout the supported CSI spec version you wish to test against.
1. Navigate to `cmd/csi-sanity` and run `make clean install`.

### Running Sanity Test
**Note:** Prior to running the sanity tests, ensure that the following files are up-to-date:
- `csi-powerflex/env.sh`
- `csi-powerflex/test/sanity/secrets.yaml`
- `csi-powerflex/test/sanity/volParams.yaml`

1. Build the PowerFlex version that you wish to test against.
	- `make clean build`
1. Navigate to the testing folder: `csi-powerflex/test/sanity/`.
1. Update `start_driver.sh` to include the correct parameters.
	- Ensure `-array-config`, `-driver-config-params`, and `-kubeconfig` are poiting to the correct locations.
1. Start the driver: `sh start_driver.sh`.
1. Open up another window and navigate to `csi-powerflex/test/sanity`.
1. Run `sh run.sh` to execute the test once.
	- Use `debug_help.sh` to run the test X number of times.
		- `sh debug_help.sh run.sh 100 30` - This runs `run.sh` 100 times, with 30 second breaks inbetween each run.
		- This is useful for catching non-consitent errors. A 30 second wait time is a good idea when running the tests over 5 times with the script, to allow enough time for the tests to cleanup. 

## Excluded Tests

1. `pagination should detect volumes added between pages and accept tokens when the last volume from a page is deleted`
	- **Reason:** The test attempts to delete all volumes on the page, even if they are not created by the test itself. This makes running the test on a non-idle server nearly impossible.

2. `check the presence of new volumes and absence of deleted ones in the volume list`
	- **Reason:** The test does not account for volumes created/deleted by other users while running. Here is an outline of the test:
		1. Checks number of volumes
		2. Adds a volume
		3. Checks number of volumes 
		4. Deletes a volume
		5. Checks number of volumes  
	- When it checks number of volumes in step 5, it is expecting the same value as when it checked in step 1. However, if someone else was adding/deleting volumes on the server, while this test was running, then the total number of volumes in steps 1 and 5 aren't gaurenteed to be equal. 

3. `should fail when the volume is missing`
	- **Reason:** The test: `should fail when the volume is missing` expects `NodeUnpublishVolume` to return an error when a volume is not found. The spec is a bit unclear, saying that the method should return an error, but also demanding that the method be idempotent. Since Kubelet might try forever if the "not found" error is returned, the node.go code was left unchanged and the test skipped. If we wanted to change the code to pass the test, we would need to stress test all supported versions of k8s to ensure that kubelet doesn't get stuck.
