# Helm tests
This folder contains Helm charts and shell scripts which can be used to test various features of the CSI PowerFlex driver.

The Helm charts typically deploy a StatefulSet with a number of PVCs created using the provided storage class names.  
For e.g. - the test `2vols` will deploy a StatefulSet which runs a single CentOS container and uses `2` PVCs.

Additionally, some tests create cloned volumes using a source `Volume` or a `VolumeSnapshot`

## Helm charts

| Name    | Description |
|---------|-------|
|2vols    | Creates 2 filesystem mounts |
|7vols	  | Creates 7 filesystem mounts |
|10vols	  | Creates 10 filesystem mounts |
|xfspre   | Create an XFS formated PV and attaches to a pod |
|2replicas | Creates 2 filesystem mounts like 2vols but with Replicas = 2 |
|2vols+restore | Upgraded version of 2vols that also mounts a volume created from snap |
|2vols-multi-array | Creates 2 volumes, each on a different array. You must fill in 2vols-multi-array/templates/pvc1.yaml first |
| 2vols + clone | 


## Scripts
| Name           | Description |
|----------------|-------|
| starttest.sh  | Script to instantiate one of the Helm charts above. Requires argument of directory name (e.g. 2vols)
| stoptest.sh | Stops currently running Helm chart
| snapcgtest.sh       | Used after starting a Helm chart; tests snapping the persistent volumes into a consistency group
| snaprestoretest.sh    | Used without previously starting a Helm test; instantiates 2vols; then upgrades it to 2vols+restore (with volume from snapshot)
| snaptest.sh   | Used after starting 2vols; snaps pvol0 twice
| volumeclonetest.sh   | Used after starting 2vols; snaps pvol0 twice


## Usage

### starttest.sh
The starttest.sh script is used to deploy Helm charts that test the deployment of a simple pod
with various storage configurations. The stoptest.sh script will delete the Helm chart and cleanup after the test.

Procedure
1. Navigate to the test/helm directory, which contains the starttest.sh and various Helm charts.

2. Run the starttest.sh script with an argument of the specific Helm chart to deploy and test. For example:
> bash starttest.sh <testname>

  Example:
    ```
   bash starttest.sh 2vols
    ```
3. After the test has completed, run the stoptest.sh script to delete the Helm chart and cleanup the volumes.
> bash stoptest.sh <testname>

  Example:
    ```
   bash stoptest.sh 2vols
    ```

To run the tests, follow the procedure given below:
1. Navigate to the test/helm directory
2. Run the desired script with the following command
> bash <script-name>

  Example:
    ```
   bash snaptest.sh
    ```
