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
	"strconv"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/kubectl"
	fnodes "k8s.io/kubernetes/test/e2e/framework/node"
	fpod "k8s.io/kubernetes/test/e2e/framework/pod"
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

var _ = ginkgo.Describe("PowerFlex Volume Filesystem Group Test", ginkgo.Label("csi-fsg"), ginkgo.Label("csi-ephemeral"), ginkgo.Serial, func() {
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
	ginkgo.It("[csi-fsg] Verify Pod Ephemeral FSGroup", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		updateFSGroupPolicyCSIDriver(client, testParameters["e2eCSIDriverName"], "fsPolicy=File")
		doOneCycleEphemeralTest(ctx, "ReadWriteOnceWithFSType")
	})
})

func doOneCycleEphemeralTest(ctx context.Context, policy string) {
	ginkgo.By("Creating a PVC")

	// Create a StorageClass
	ginkgo.By("CSI_TEST: Running for k8s setup")

	// Create a Pod to use this PVC, and verify volume has been attached
	ginkgo.By("Creating pod to attach PV to the node")

	p := getPodFromManifest("ephemeral.yaml")

	pod, err := createPod(namespace, p, client)
	// in case of error help debug by showing events
	if err != nil {
		getEvents(client, namespace, p.Name, "Pod")
		framework.Failf("stop tests ephemeral pod failed to create %s", p.Name)
	}

	ginkgo.By("Expect claim to provision volume bound successfully")

	ginkgo.By("Verify the volume is accessible and filegroup type is as expected")

	ddcmd1 := "dd bs=1024 count=1048576 </dev/urandom > /data0/file;ls -l /data0/file"
	ddcmd2 := "dd bs=1024 count=1048576 </dev/urandom > /data1/file;ls -l /data1/file"

	cmd := []string{
		"exec", pod.Name, "--namespace=" + namespace, "--", "/bin/sh", "-c",
		ddcmd1 + ";" + ddcmd2,
	}

	output := kubectl.RunKubectlOrDie(namespace, cmd...)

	if strings.Contains(output, "container not found") {
		framework.Failf("stop tests pod failed to start %s", output)
	}

	exists := false
	fsGroup := int64(54321)
	runAsUser := int64(54321)

	switch policy {
	case "ReadWriteOnceWithFSType":
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
}
