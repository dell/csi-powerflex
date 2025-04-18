// Copyright © 2024 Dell Inc. or its subsidiaries. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//      http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package e2e

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	yaml "gopkg.in/yaml.v3"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/kubectl"
	fnodes "k8s.io/kubernetes/test/e2e/framework/node"
	fpod "k8s.io/kubernetes/test/e2e/framework/pod"
	fpv "k8s.io/kubernetes/test/e2e/framework/pv"
	admissionapi "k8s.io/pod-security-admission/api"
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

var (
	client         clientset.Interface
	namespace      string
	testParameters map[string]string
	valueFilename  = "e2e-values.yaml"
)

// ginkgo suite is kicked off in suite_test.go  RunSpecsWithDefaultAndCustomReporters

// ginkgo.Describe is a ginkgo spec , runs a set of scenarios , like one happy path and several error tests

// ginkgo.BeforeEach defines a method that will run once

// followed bg ginkgo.It() that defines one scenario

// below we run 4 scenarios ,
//	each one changes the fsGroupPolicy in csiDriver ,
//			creates a new pod similar to helm test , with pvc/pv which used the fsGroup value (non root )

// 	each It method verifies expected results using gomega Matcher library

// 	each It method deletes the pv/pvc/pod --cleanup

// 	framework.Logf --will stop and not cleaup if we want to exit due to unexpected match , manual cleanup is expected

// notice the imports from framework, e2e test framework from kubernetes that provided methods to create/delete/list pv/pvc/pod

// utils.go calls these framework methods

// we can also create a pod or statefulset from a yaml file in

// testing-manifests folder. see GetStatefulSetFromManifest() in utils

var _ = ginkgo.Describe("Volume Filesystem Group Test", ginkgo.Label("csi-fsg"), ginkgo.Label("csi-fs"), ginkgo.Serial, func() {
	//  Building a namespace api object, basename volume-fsgroup
	f := framework.NewDefaultFramework("volume-fsgroup")

	f.NamespacePodSecurityLevel = admissionapi.LevelPrivileged

	framework.Logf("run e2e test default timeouts  %#v ", f.Timeouts)

	ginkgo.BeforeEach(func() {
		client = f.ClientSet
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		namespace = getNamespaceToRunTests(f)
		// setup other exteral environment for example array server
		bootstrap()
		nodeList, err := fnodes.GetReadySchedulableNodes(ctx, f.ClientSet)
		framework.ExpectNoError(err, "Unable to find ready and schedulable Node")
		if !(len(nodeList.Items) > 0) {
			framework.Failf("Unable to find ready and schedulable Node")
		}
		for _, node := range nodeList.Items {
			framework.Logf("ready nodes %s", node.Name)
		}
	})

	// in case you want to log and exit	framework.Fail("stop test")

	// Test for Pod creation works when SecurityContext has CSIDriver Fs Group Policy: ReadWriteOnceWithFSType
	ginkgo.It("[csi-fsg] Verify Pod FSGroup with fsPolicy=ReadWriteOnceWithFSType", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := updateFSGroupPolicyCSIDriver(client, testParameters["e2eCSIDriverName"], "fsPolicy=ReadWriteOnceWithFSType")
		if err != nil {
			framework.Failf("error updating CSIDriver: %v", err)
		}
		doOneCyclePVCTest(ctx, "ReadWriteOnceWithFSType", "")
		doOneCyclePVCTest(ctx, "ReadWriteOnceWithFSType", v1.ReadOnlyMany)
	})

	ginkgo.It("[csi-fsg] Verify Pod FSGroup with fsPolicy=None", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := updateFSGroupPolicyCSIDriver(client, testParameters["e2eCSIDriverName"], "fsPolicy=None")
		if err != nil {
			framework.Failf("error updating CSIDriver: %v", err)
		}
		doOneCyclePVCTest(ctx, "None", v1.ReadOnlyMany)
		doOneCyclePVCTest(ctx, "None", "")
	})
	// Test for ROX volume and  CSIDriver Fs Group Policy: File
	ginkgo.It("[csi-fsg] Verify Pod FSGroup with fsPolicy=File", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := updateFSGroupPolicyCSIDriver(client, testParameters["e2eCSIDriverName"], "fsPolicy=File")
		if err != nil {
			framework.Failf("error updating CSIDriver: %v", err)
		}
		doOneCyclePVCTest(ctx, "File", v1.ReadOnlyMany)
		doOneCyclePVCTest(ctx, "File", "")
	})

	// Test for Pod creation works when SecurityContext has  CSIDriver without Fs Group Policy:
	ginkgo.It("[csi-fsg] Verify Pod FSGroup with fsPolicy not set (should default)", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := updateFSGroupPolicyCSIDriver(client, testParameters["e2eCSIDriverName"], "fsPolicy=")
		if err != nil {
			framework.Failf("error updating CSIDriver: %v", err)
		}
		doOneCyclePVCTest(ctx, "", "")
	})
})

func doOneCyclePVCTest(ctx context.Context, policy string, accessMode v1.PersistentVolumeAccessMode) {
	ginkgo.By("Creating a PVC")

	// Create a StorageClass
	ginkgo.By("CSI_TEST: Running for k8s setup")

	testParameters[testParameters["scParamStoragePoolKey"]] = testParameters["scParamStoragePoolValue"]
	testParameters[testParameters["scParamStorageSystemKey"]] = testParameters["scParamStorageSystemValue"]
	testParameters[testParameters["scParamFsTypeKey"]] = testParameters["scParamFsTypeValue"]

	storageclasspvc, pvclaim, err := createPVCAndStorageClass(client,
		namespace, nil, testParameters, testParameters["diskSize"], nil, "", false, "")

	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	defer func() {
		err = client.StorageV1().StorageClasses().Delete(ctx, storageclasspvc.Name, *metav1.NewDeleteOptions(0))
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}()

	ginkgo.By("Expect claim status to be in Pending state")
	err = fpv.WaitForPersistentVolumeClaimPhase(ctx, v1.ClaimPending, client,
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
		admissionapi.LevelPrivileged, testParameters["execCommand"], fsGroupInt64, runAsUserInt64)
	// in case of error help debug by showing events
	if err != nil {
		getEvents(client, pvclaim.Namespace, pvclaim.Name, "PersistentVolumeClaim")
		framework.Failf("stop tests pod failed to start policy %s", policy)

	}

	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	ginkgo.By("Expect claim to provision volume bound successfully")

	ginkgo.By("Expect claim to be in Bound state and provisioning volume passes")
	err = fpv.WaitForPersistentVolumeClaimPhase(ctx, v1.ClaimBound, client,
		pvclaim.Namespace, pvclaim.Name, framework.Poll, time.Minute)

	gomega.Expect(err).NotTo(gomega.HaveOccurred(), fmt.Sprintf("Failed to provision volume with err: %v", err))

	persistentvolumes, err := fpv.WaitForPVClaimBoundPhase(ctx, client,
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

	cmd := []string{
		"exec", pod.Name, "--namespace=" + namespace, "--", "/bin/sh", "-c",
		"ls -l /mnt/volume1",
	}

	output := kubectl.RunKubectlOrDie(namespace, cmd...)

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
	case "File":
		gomega.Expect(strings.Contains(output, strconv.Itoa(int(fsGroup)))).NotTo(gomega.BeFalse())
		gomega.Expect(strings.Contains(output, strconv.Itoa(int(runAsUser)))).NotTo(gomega.BeFalse())
	default:
		exists = false
		gomega.Expect(exists).NotTo(gomega.BeTrue(), "Failed to test policy")
	}

	// Delete POD
	ginkgo.By(fmt.Sprintf("Deleting the pod %s in namespace %s", pod.Name, namespace))
	err = fpod.DeletePodWithWait(ctx, client, pod)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// if we need a ROX volume to mount, we need to take the RWO one, alter it, then publish it to another pod
	if accessMode == v1.ReadOnlyMany {

		ginkgo.By("Changing PV to be ROX")
		pv.Spec.AccessModes = []v1.PersistentVolumeAccessMode{v1.ReadOnlyMany}

		accessChange := "{\"spec\": {\"accessModes\":[\"ReadOnlyMany\"]}}"

		cmd := []string{"patch", "pv", pv.Name, "-p", accessChange}

		output := kubectl.RunKubectlOrDie(namespace, cmd...)

		fmt.Printf("output: %s\n", output)

		ginkgo.By("Creating Pod to use ROX volume")

		fsGroup = 54321
		runAsUser = 54321
		newFsGroupInt64 := &fsGroup
		newRunAsUserInt64 := &runAsUser

		pod, err = createPodForFSGroup(client, namespace, nil, []*v1.PersistentVolumeClaim{pvclaim},
			admissionapi.LevelPrivileged, testParameters["execCommand"], newFsGroupInt64, newRunAsUserInt64)
		// in case of error help debug by showing events
		if err != nil {
			getEvents(client, pvclaim.Namespace, pvclaim.Name, "PersistentVolumeClaim")
			framework.Failf("stop tests pod failed to start policy %s", policy)

		}

		cmd = []string{
			"exec", pod.Name, "--namespace=" + namespace, "--", "/bin/sh", "-c",
			"ls -l /mnt/volume1",
		}

		output = kubectl.RunKubectlOrDie(namespace, cmd...)

		fmt.Printf("output: %s\n", output)

		// check output again, ROX means means file should not be modified
		switch policy {
		case "ReadWriteOnceWithFSType":
			gomega.Expect(strings.Contains(output, strconv.Itoa(int(fsGroup)))).NotTo(gomega.BeFalse())
		case "None":
			gomega.Expect(strings.Contains(output, strconv.Itoa(int(fsGroup)))).To(gomega.BeFalse())
		case "":
			gomega.Expect(strings.Contains(output, strconv.Itoa(int(fsGroup)))).To(gomega.BeFalse())
		case "File":
			gomega.Expect(strings.Contains(output, strconv.Itoa(int(fsGroup)))).NotTo(gomega.BeFalse())
		default:
			exists = false
			gomega.Expect(exists).NotTo(gomega.BeTrue(), "Failed to test policy")
		}

		// Delete POD
		ginkgo.By(fmt.Sprintf("Deleting the pod %s in namespace %s", pod.Name, namespace))
		err = fpod.DeletePodWithWait(ctx, client, pod)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

	}
}

func readYaml() (map[string]string, error) {
	yfile, err := os.ReadFile(valueFilename)
	if err != nil {
		return nil, err
	}

	data := make(map[string]string)

	err = yaml.Unmarshal(yfile, &data)
	if err != nil {
		return nil, err
	}

	return data, nil
}
