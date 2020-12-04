# Kubernetes Sanity Script Test

This test runs the Kubernetes sanity test at https://github.com/kubernetes-csi/csi-test.
The version was v3.1.0

To run the test:

	1. run `git clone https://github.com/kubernetes-csi/csi-test.git`
	2. Check out tag v3.1.0
	3. Cd to csi-test/cmd/csi-sanity
	4. run `make clean install`
	5. Make sure env.sh(in csi-vxflexos) is up to date
	6. Cd to csi-vxflexos/test/sanity and edit secrets.yaml and volParams.yaml to have the correct storage pool parameter 
	7. Build csi-vxflexos ("make clean build" in csi-vxflexos)
	8. Run start_driver.sh 
	9. Open up another window and bring it to csi-vxflexos/test/sanity
	10. Use run.sh to run the tests once
		Use debug_help.sh to run an sh file x amount of times
			debug_help.sh run.sh 100 30 runs run.sh 100 times, with thirty second breaks inbetween each run
			This is useful for catching non-consitent errors. A 30 second wait time is a good idea when running 
			the tests over 5 times with the script, to allow enough time for the tests to cleanup. 


##Excluded Tests

1.pagination should detect volumes added between pages and accept tokens when the last volume from a page is deleted

	Reason: The test attempts to delete all volumes on the page, even if they are not created by the test itself. 
	This makes running the test on a non-idle server nearly impossible.

2.check the presence of new volumes and absence of deleted ones in the volume list

	Reason: The test does not account for volumes created/deleted by other users while running. 
	Here is an outline of the test:

	1.Checks number of volumes
	2.Adds a volume
	3.Checks number of volumes 
	4.Deletes a volume
	5.Checks number of volumes  

	When it checks number of volumes in step 5, it is expecting the same value as when it checked in step 1. 
	However, if someone else was adding/deleting volumes on the server, while this test was running,
	then the total number of volumes in steps 1 and 5 aren't gaurenteed to be equal. 

3. should fail when the volume is missing

   Reason: The test: "should fail when the volume is missing" expects NodeUnpublishVolume to return an error when a volume 
   is not found. The spec is a bit unclear, saying that the method should return an error, but also demanding that the method be 
   idempotent. Since Kubelet might try forever if the "not found" error is returned, the node.go code was left 
   unchanged and the test skipped. If we wanted to change the code to pass the test, we would need to stress test all 
   supported versions of k8s to ensure that kubelet doesn't get stuck. Other then the testing, the actual change should be pretty quick. 
