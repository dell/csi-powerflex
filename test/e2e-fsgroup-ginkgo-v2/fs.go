package e2e

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes"
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

var driverNamespace = "vxflexos"
var e2eCSIDriverName = "csi-vxflexos.dellemc.com"

//  storagepool=pool2,systemID=4d4a2e5a36080e0f
var scParamStoragePoolKey = "storagepool"
var scParamStoragePoolValue = "pool1"

var scParamStorageSystemKey = "systemID"
var scParamStorageSystemValue = "4d4a2e5a36080e0f"

var diskSize = "8Gi"

// this is used in test container start up
var execCommand = "while true ; do sleep 2 ; done"

var (
	client       clientset.Interface
	namespace    string
	scParameters map[string]string
)

var _ = ginkgo.Describe("[Serial] [csi-fsg]"+
	"[csi-fsg] Volume Filesystem Group Test", func() {


	cfg, err := framework.LoadConfig()
	if err != nil {
                 framework.Failf("Unable to load config: %v", err)
         }

	//  Building a namespace api object, basename volume-fsgroup

	//options := framework.Options{}
	
	f := framework.NewDefaultFramework("volume-fsgroup")


	f.ClientSet = kubernetes.NewForConfigOrDie(cfg)

	f.SkipNamespaceCreation = false
	f.BeforeEach()

	//f =  framework.NewFramework("volume-fsgroup", options, kubernetes.NewForConfigOrDie(cfg))


	// prevent annoying psp warning
	f.SkipPrivilegedPSPBinding = true

	framework.Logf("run e2e test default timeouts  %#v ", f.Timeouts)

	framework.Logf("run e2e test clientConfig  %#v ", f.ClientConfig)

	framework.Logf("run e2e test framework  %#v ", f)

	ginkgo.BeforeEach(func() {
		client = f.ClientSet

		namespace = getNamespaceToRunTests(f)

		scParameters = make(map[string]string)

		// setup other exteral environment for example array server

		nodeList, err := fnodes.GetReadySchedulableNodes(f.ClientSet)

		framework.ExpectNoError(err, "Unable to find ready and schedulable Node")

		if !(len(nodeList.Items) > 0) {
			framework.Failf("Unable to find ready and schedulable Node")
		}
		for _, node := range nodeList.Items {
			framework.Logf("ready nodes %s", node.Name)
		}
	})

	// in case you want to log and exit	framework.Fail("stop test")

	// Test for Pod creation works when SecurityContext has CSIDriver Fs Group Policy:     ReadWriteOnceWithFSType
	ginkgo.It("[csi-fsg] Verify Pod FSGroup", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		updateCsiDriver(client, e2eCSIDriverName, "fsPolicy=ReadWriteOnceWithFSType")
		restartDriverPods(client, driverNamespace)
		doOneCyclePVCTest(ctx, "ReadWriteOnceWithFSType")

	})

	// Test for Pod creation works when SecurityContext has  CSIDriver Fs Group Policy:  None
	ginkgo.It("[csi-fsg] Verify Pod FSGroup", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		updateCsiDriver(client, e2eCSIDriverName, "fsPolicy=None")
		restartDriverPods(client, driverNamespace)
		doOneCyclePVCTest(ctx, "None")
	})

	// Test for Pod creation works when SecurityContext has  CSIDriver without Fs Group Policy:
	ginkgo.It("[csi-fsg] Verify Pod FSGroup", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		updateCsiDriver(client, e2eCSIDriverName, "fsPolicy=")
		restartDriverPods(client, driverNamespace)
		doOneCyclePVCTest(ctx, "")

	})

})

func doOneCyclePVCTest(ctx context.Context, policy string) {
	ginkgo.By("Creating a PVC")

	// Create a StorageClass
	ginkgo.By("CSI_TEST: Running for k8s setup")

	// storagepool=pool2,systemID=4d4a2e5a36080e0f

	scParameters[scParamStoragePoolKey] = scParamStoragePoolValue
	scParameters[scParamStorageSystemKey] = scParamStorageSystemValue

	storageclasspvc, pvclaim, err := createPVCAndStorageClass(client,
		namespace, nil, scParameters, diskSize, nil, "", false, "")

	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	defer func() {
		err = client.StorageV1().StorageClasses().Delete(ctx, storageclasspvc.Name, *metav1.NewDeleteOptions(0))
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}()

	ginkgo.By("Expect claim status to be in Pending state")
	err = fpv.WaitForPersistentVolumeClaimPhase(v1.ClaimPending, client,
		pvclaim.Namespace, pvclaim.Name, framework.Poll, time.Minute)

	gomega.Expect(err).NotTo(gomega.HaveOccurred(),
		fmt.Sprintf("Failed to find the volume in pending state with err: %v", err))

	// Create a Pod to use this PVC, and verify volume has been attached
	ginkgo.By("Creating pod to attach PV to the node")

	var fsGroup int64
	var runAsUser int64

	fsGroup = 54321
	runAsUser = 54321

	fsGroupInt64 := &fsGroup
	runAsUserInt64 := &runAsUser

	pod, err := createPodForFSGroup(client, namespace, nil, []*v1.PersistentVolumeClaim{pvclaim},
		true, execCommand, fsGroupInt64, runAsUserInt64)

	// in case of error help debug by showing events
	if err != nil {
		getEvents(client, pvclaim.Namespace, pvclaim.Name, "PersistentVolumeClaim")
		framework.Failf("stop tests pod failed to start policy %s", policy)

	}

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

	pv := persistentvolumes[0]
	volumeID := pv.Spec.CSI.VolumeHandle
	var exists bool
	ginkgo.By(fmt.Sprintf("Verify volume: %s is attached to the node: %s",
		pv.Spec.CSI.VolumeHandle, pod.Spec.NodeName))
	annotations := pod.Annotations
	gomega.Expect(exists).NotTo(gomega.BeTrue(), fmt.Sprintf("Pod %s %s annotation", annotations, volumeID))

	ginkgo.By("Verify the volume is accessible and filegroup type is as expected")

	cmd := []string{"exec", pod.Name, "--namespace=" + namespace, "--", "/bin/sh", "-c",
		"ls -l /mnt/volume1"}

	output := framework.RunKubectlOrDie(namespace, cmd...)

	if strings.Contains(output, "container not found") {
		framework.Failf("stop tests pod failed to start %s", output)
	}

	switch policy {
	case "ReadWriteOnceWithFSType":
		gomega.Expect(strings.Contains(output, strconv.Itoa(int(fsGroup)))).NotTo(gomega.BeFalse())
		gomega.Expect(strings.Contains(output, strconv.Itoa(int(runAsUser)))).NotTo(gomega.BeFalse())
	case "None":
		gomega.Expect(strings.Contains(output, strconv.Itoa(int(fsGroup)))).To(gomega.BeFalse())
		gomega.Expect(strings.Contains(output, strconv.Itoa(int(runAsUser)))).To(gomega.BeFalse())
	case "":
		gomega.Expect(strings.Contains(output, strconv.Itoa(int(fsGroup)))).NotTo(gomega.BeFalse())
		gomega.Expect(strings.Contains(output, strconv.Itoa(int(runAsUser)))).NotTo(gomega.BeFalse())
	default:
		exists = false
		gomega.Expect(exists).NotTo(gomega.BeTrue(), "Failed to test policy")
	}

	// Delete POD
	ginkgo.By(fmt.Sprintf("Deleting the pod %s in namespace %s", pod.Name, namespace))
	err = fpod.DeletePodWithWait(client, pod)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

}