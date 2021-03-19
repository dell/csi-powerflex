This directory contains various test helm charts and test scripts:

Helm charts:
2replicas  Creates 2 filesystem mounts like 2vols but with Replicas = 2
2vols	Creates 2 filesystem mounts
2vols+restore	Upgraded version of 2vols that also mounts a volume created from snap  
2vols-multi-array Creates 2 volumes, each on a different array. You must fill in 2vols-multi-array/templates/pvc1.yaml first  
7vols	Creates 7 filesystem mounts  
10vols	Creates 10 filesystem mounts  
block1  Create a block volume using a pvc (NOT WORKING)
block2  Create a block volume using a pvc+pv+existing volume (NOT WORKING)

Scripts:
starttest.sh  -- Used to instantiate one of the helm charts above. Requires argument of directory name (e.g. 2vols)
stoptest.sh -- Stops currently running helm chart
snapcgtest.sh -- Used after starting a helm chart; tests snapping the persistent volumes into a consistency group
snaprestoretest.sh -- Used without previously starting a helm test; instantiates 2vols; then upgrades it to 2vols+restore (with volume from snapshot)
snaptest.sh -- Used after starting 2vols; snaps pvol0 twice

See the release notes for additional information and examples of using these tests.
