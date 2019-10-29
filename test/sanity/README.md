# Sanity Script Test

This test runs the Kubernetes sanity test at https://github.com/kubernetes-csi/csi-test.
The version  was v2.2.0

To run the test:

	1. "go get github.com/kubernetes-csi/csi-test"
	2. "cd [path to csi-sanity]"
	3. "make clean install"
	4. Make sure env.sh(in csi-vxflexos) is up to date
	5. edit secrets.yaml and volParams.yaml to have the correct storage pool parameter 
	6. Build CSI-vxflexos ("make clean build" in top level of directory)
	7.Run start_driver.sh 
	8.Open up another window and bring it to csi-vxflexos/test/sanity
	9.Use run.sh to run the tests once
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
