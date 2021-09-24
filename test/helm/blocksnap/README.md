Blocksnap Test
==============

This is a real use case that arises in data protection. The idea is to make a mounted file system from a volume for a first pod,
have that pod write some data to the file system, and then take a snap of the volume, and then to make a second pod which
uses the snap as a volume source for a block mounted volume.

The block volume created from the snapshot can then be used to transmit the volume efficiently by the 2nd pod to remote storage.

The test mounts the block volume in the 2nd pod in a local directory, and compares the data written by the 1st pod (which is a tar .tgz file)
with the data from mounting the snap contents in the 2nd pod, which should always be identical.

Running the Test
----------------

Execute "sh run.sh" to run the test in a kubernetes environment that supports v1 block snapshots. The test is preconfigured to use
the vxflexos CSI torage system in run.sh. You can edit run.sh and change the storageclass, snapclass (the volumesnapshotclass name),
and namespace parameters to run the test for a different type of CSI storage.

The test is constructed from three helm charts that are deployed in the following sequence:
1. 1vol creates one volume and deploys it in a pod
2. 1snap creates a snapshot from the volume in step 1.
3. 1volfromsnap creates a volume from the snapshot and deploys it in a second pod.

If the test runs succssfully, it compares the data generated in the first pod with the data from the snap in the second pod
to make sure they match. Then it deletes the helm deployments and waits until the pvcs in the namespace are deleted.
