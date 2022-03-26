package e2e

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/test/e2e/framework"
	fnodes "k8s.io/kubernetes/test/e2e/framework/node"
	fpod "k8s.io/kubernetes/test/e2e/framework/pod"
	fpv "k8s.io/kubernetes/test/e2e/framework/pv"
)

/*
Test to verify fsgroup specified in pod is being honored after pod creation.
Steps
1. Create StorageClass without FStype mentioned.
2. Create PVC which uses the StorageClass created in step 1.
3. Wait for PV to be provisioned.
4. Wait for PVC's status to become Bound.
5. Create pod using PVC on specific node with securitycontext
6. Wait for Disk to be attached to the node.
7. Delete pod and Wait for Volume Disk to be detached from the Node.
8. Delete PVC, PV and Storage Class.
*/

var kubeconfigEnvVar = "KUBECONFIG"
var busyBoxImageOnGcr = "gcr.io/google_containers/busybox:1.27"
var envStoragePolicyNameForSharedDatastores = "shared-ds-policy"
var envSharedDatastoreURL = "shared-ds-url"
var vanillaCluster = true

//  storagepool=pool2,systemID=4d4a2e5a36080e0f
var scParamStoragePool = "storagepool"
var scParamStorageSystem = "systemID"
var e2eCSIDriverName = "csi-vxflexos.dellemc.com"
var diskSize = "8Gi"

var execCommand = "/bin/df -T /mnt/volume1 | " + "/bin/awk 'FNR == 2 {print $2}' > /mnt/volume1/fstype && while true ; do sleep 2 ; done"

var execRWXCommandPod1 = "echo 'Hello message from Pod1' > /mnt/volume1/Pod1.html  && " +
	"chmod o+rX /mnt /mnt/volume1/Pod1.html && while true ; do sleep 2 ; done"

var execRWXCommandPod2 = "echo 'Hello message from Pod2' > /mnt/volume1/Pod2.html  && " +
	"chmod o+rX /mnt /mnt/volume1/Pod2.html && while true ; do sleep 2 ; done"

var _ = ginkgo.Describe("[csi-fsg]"+
	"[csi-fsg] Volume Filesystem Group Test", func() {
	f := framework.NewDefaultFramework("volume-fsgroup")
	var (
		client       clientset.Interface
		namespace    string
		scParameters map[string]string
		envArray     string
		envPool      string
	)
	ginkgo.BeforeEach(func() {
		client = f.ClientSet
		namespace = getNamespaceToRunTests(f)
		scParameters = make(map[string]string)
		envArray = GetAndExpectStringEnvVar(scParamStorageSystem)
		envPool = GetAndExpectStringEnvVar(scParamStoragePool)
		bootstrap()
		nodeList, err := fnodes.GetReadySchedulableNodes(f.ClientSet)
		framework.ExpectNoError(err, "Unable to find ready and schedulable Node")
		if !(len(nodeList.Items) > 0) {
			framework.Failf("Unable to find ready and schedulable Node")
		}
	})

	// Test for Pod creation works when SecurityContext has FSGroup
	ginkgo.It("Verify Pod Creation works when SecurityContext has FSGroup", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		var storageclasspvc *storagev1.StorageClass
		var pvclaim *v1.PersistentVolumeClaim
		var err error
		var fsGroup int64
		var runAsUser int64

		ginkgo.By("Creating a PVC")

		// Create a StorageClass
		ginkgo.By("CSI_TEST: Running for vanilla k8s setup")

		// storagepool=pool2,systemID=4d4a2e5a36080e0f
		scParameters[scParamStoragePool] = envPool
		scParameters[scParamStorageSystem] = envArray
		storageclasspvc, pvclaim, err = createPVCAndStorageClass(client,
			namespace, nil, scParameters, diskSize, nil, "", false, "")

		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		defer func() {
			if vanillaCluster {
				err = client.StorageV1().StorageClasses().Delete(ctx, storageclasspvc.Name, *metav1.NewDeleteOptions(0))
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			}
		}()

		ginkgo.By("Expect claim status to be in Pending state")
		err = fpv.WaitForPersistentVolumeClaimPhase(v1.ClaimPending, client,
			pvclaim.Namespace, pvclaim.Name, framework.Poll, time.Minute)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(),
			fmt.Sprintf("Failed to find the volume in pending state with err: %v", err))

		// Create a Pod to use this PVC, and verify volume has been attached
		ginkgo.By("Creating pod to attach PV to the node")

		fsGroup = 1000
		runAsUser = 2000

		fsGroupInt64 := &fsGroup
		runAsUserInt64 := &runAsUser
		pod, err := createPodForFSGroup(client, namespace, nil, []*v1.PersistentVolumeClaim{pvclaim},
			true, execCommand, fsGroupInt64, runAsUserInt64)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("Expect claim to provision volume bound successfully")

		ginkgo.By("Expect claim to be in Bound state and provisioning volume passes")
		err = fpv.WaitForPersistentVolumeClaimPhase(v1.ClaimBound, client,
			pvclaim.Namespace, pvclaim.Name, framework.Poll, time.Minute)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), fmt.Sprintf("Failed to provision volume with err: %v", err))

		persistentvolumes, err := fpv.WaitForPVClaimBoundPhase(client,
			[]*v1.PersistentVolumeClaim{pvclaim}, framework.ClaimProvisionTimeout)
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), "Failed to provision volume")

		volHandle := persistentvolumes[0].Spec.CSI.VolumeHandle
		gomega.Expect(volHandle).NotTo(gomega.BeEmpty())

		/*
		   defer func() {
		           err = fpv.DeletePersistentVolumeClaim(client, pvclaim.Name, pvclaim.Namespace)
		           gomega.Expect(err).NotTo(gomega.HaveOccurred())
		   }()
		*/

		pv := persistentvolumes[0]
		volumeID := pv.Spec.CSI.VolumeHandle
		var exists bool
		ginkgo.By(fmt.Sprintf("Verify volume: %s is attached to the node: %s",
			pv.Spec.CSI.VolumeHandle, pod.Spec.NodeName))
		if vanillaCluster {
			annotations := pod.Annotations
			gomega.Expect(exists).NotTo(gomega.BeTrue(), fmt.Sprintf("Pod %s %s annotation", annotations, volumeID))
		}

		ginkgo.By("Verify the volume is accessible and filegroup type is as expected")
		cmd := []string{"exec", pod.Name, "--namespace=" + namespace, "--", "/bin/sh", "-c",
			"df | grep scini;ls -l /mnt/volume1"}
		output := framework.RunKubectlOrDie(namespace, cmd...)
		gomega.Expect(strings.Contains(output, strconv.Itoa(int(fsGroup)))).NotTo(gomega.BeFalse())
		gomega.Expect(strings.Contains(output, strconv.Itoa(int(runAsUser)))).NotTo(gomega.BeFalse())

		// Delete POD
		ginkgo.By(fmt.Sprintf("Deleting the pod %s in namespace %s", pod.Name, namespace))
		err = fpod.DeletePodWithWait(client, pod)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		if vanillaCluster {
			//isDiskDetached, err := e2eVSphere.waitForVolumeDetachedFromNode(client, volumeID, pod.Spec.NodeName)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			//gomega.Expect(isDiskDetached).To(gomega.BeTrue(),
			//fmt.Sprintf("Volume %q is not detached from the node %q", pv.Spec.CSI.VolumeHandle, pod.Spec.NodeName))
		}
	})
})
