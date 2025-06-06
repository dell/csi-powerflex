// Copyright © 2022 Dell Inc. or its subsidiaries. All Rights Reserved.
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
	"path/filepath"
	"strings"

	"github.com/onsi/gomega"
	apps "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/kubernetes/test/e2e/framework/manifest"
	fpod "k8s.io/kubernetes/test/e2e/framework/pod"
	fpv "k8s.io/kubernetes/test/e2e/framework/pv"
	fss "k8s.io/kubernetes/test/e2e/framework/statefulset"
	"k8s.io/kubernetes/test/e2e/framework/testfiles"
	admissionapi "k8s.io/pod-security-admission/api"

	"github.com/onsi/ginkgo/v2"
)

// updateFSGroupPolicyCSIDriver updates the FSGroupPolicy of a CSI driver.
//
// It takes in the client interface, the name of the CSI driver, and the desired
// FSGroupPolicy as parameters. The function retrieves the CSI driver using the
// provided name and updates its FSGroupPolicy based on the provided fsPolicy.
// The function returns an error if the retrieval or update of the CSI driver
// fails.
//
// Parameters:
// - client: the client interface used to interact with the Kubernetes API.
// - e2eCSIDriverName: the name of the CSI driver to update.
// - fsPolicy: the desired FSGroupPolicy for the CSI driver.
//
// Returns:
// - error: an error if the retrieval or update of the CSI driver fails.
func updateFSGroupPolicyCSIDriver(client clientset.Interface, e2eCSIDriverName, fsPolicy string) error {
	csi, err := client.StorageV1().CSIDrivers().Get(context.TODO(), e2eCSIDriverName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	framework.Logf("csidriver update %s, %s", csi.Name, fsPolicy)

	parts := strings.Split(fsPolicy, "=")
	if len(parts) == 2 && len(parts[1]) > 0 {
		policy := parts[1]
		switch policy {
		case "None":
			*csi.Spec.FSGroupPolicy = storagev1.NoneFSGroupPolicy
		case "File":
			*csi.Spec.FSGroupPolicy = storagev1.FileFSGroupPolicy
		default:
			*csi.Spec.FSGroupPolicy = storagev1.ReadWriteOnceWithFSTypeFSGroupPolicy
		}
	} else {
		csi.Spec.FSGroupPolicy = nil
	}

	_, err = client.StorageV1().CSIDrivers().Update(context.TODO(), csi, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	framework.Logf("csidriver updated ok %s, %s", e2eCSIDriverName, fsPolicy)
	return nil
}

func getEvents(client clientset.Interface, ns string, name string, objtype string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	res, _ := client.CoreV1().PersistentVolumeClaims(ns).Get(ctx, name, metav1.GetOptions{})
	if res != nil {
		events, _ := client.CoreV1().Events(ns).List(context.TODO(),
			metav1.ListOptions{FieldSelector: "involvedObject.name=" + name, TypeMeta: metav1.TypeMeta{Kind: objtype}})
		for _, item := range events.Items {
			if strings.Contains(item.Message, "failed") ||
				strings.Contains(item.Message, "warning") ||
				strings.Contains(item.Message, "error") {
				framework.Logf("debug pvc events %s, %s", item.Reason, item.Message)
			}
		}
	}
}

// getPvFromClaim returns PersistentVolume for requested claim.
func getPvFromClaim(client clientset.Interface, namespace string, claimName string) *v1.PersistentVolume {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pvclaim, err := client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, claimName, metav1.GetOptions{})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	pv, err := client.CoreV1().PersistentVolumes().Get(ctx, pvclaim.Spec.VolumeName, metav1.GetOptions{})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	return pv
}

// CreateStatefulSet creates a StatefulSet from the manifest at manifestPath in the given namespace.
func CreateStatefulSet(ns string, ss *apps.StatefulSet, c clientset.Interface) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fmtString := "Creating statefulset %v/%v with %d replicas and selector %+v"
	framework.Logf(fmtString, ss.Namespace, ss.Name, *(ss.Spec.Replicas), ss.Spec.Selector)

	_, err := c.AppsV1().StatefulSets(ns).Create(ctx, ss, metav1.CreateOptions{})
	framework.ExpectNoError(err)
	fss.WaitForRunningAndReady(ctx, c, *ss.Spec.Replicas, ss)
}

// create pod from yaml manifest
func createPod(namespace string, pod *v1.Pod, client clientset.Interface) (*v1.Pod, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := client.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	framework.ExpectNoError(err)

	// Waiting for pod to be running.

	err = fpod.WaitForPodNameRunningInNamespace(ctx, client, pod.Name, namespace)
	if err != nil {
		return pod, fmt.Errorf("pod %q is not Running: %s", pod.Name, err.Error())
	}

	err = fpod.WaitTimeoutForPodReadyInNamespace(ctx, client, pod.Name, namespace, framework.PodStartTimeout)
	if err != nil {
		return pod, fmt.Errorf("pod is not Running: %s %s", pod.Name, err.Error())
	}

	// Get fresh pod info.
	p, err := client.CoreV1().Pods(namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("pod Get API error: %v", err)
	}
	return p, nil
}

// GetPodFromManifest creates a Pod from the yaml
// file present in the manifest path. eg. ephemeral.yaml
func getPodFromManifest(filename string) *v1.Pod {
	pwd, _ := os.Getwd()
	manifestPath := pwd + "/testing-manifests/pod/"

	podManifestFilePath := filepath.Join(manifestPath, filename)
	framework.Logf("Parsing pod from %v", podManifestFilePath)

	pod, err := manifest.PodFromManifest(podManifestFilePath)

	framework.ExpectNoError(err)

	for _, v := range pod.Spec.Volumes {
		framework.Logf("setting volume array info %s", v.Name)
		v.CSI.VolumeAttributes["storagepool"] = testParameters["scParamStoragePoolValue"]
		v.CSI.VolumeAttributes["systemID"] = testParameters["scParamStorageSystemValue"]
	}
	framework.Logf("debug pod details %+v", pod.Spec.Volumes[0].CSI.VolumeAttributes)

	return pod
}

// GetStatefulSetFromManifest creates a StatefulSet from the statefulset.yaml
// file present in the manifest path.
func GetStatefulSetFromManifest(ns string) *apps.StatefulSet {
	pwd, _ := os.Getwd()
	manifestPath := pwd + "/testing-manifests/statefulset/"

	ssManifestFilePath := filepath.Join(manifestPath, "statefulset.yaml")
	framework.Logf("Parsing statefulset from %v", ssManifestFilePath)
	ss, err := manifest.StatefulSetFromManifest(ssManifestFilePath, ns)
	framework.ExpectNoError(err)
	return ss
}

// not usedgit git
// bootstrap function takes care of initializing necessary tests context for e2e tests
func bootstrap() {
	var err error
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	// ctx
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	// connect(ctx, &e2eVSphere)
	if framework.TestContext.RepoRoot != "" {
		testfiles.AddFileSource(testfiles.RootFileSource{Root: framework.TestContext.RepoRoot})
	}
	framework.TestContext.Provider = "local"
}

// getNamespaceToRunTests returns the namespace in which the tests are expected
// to run. For test setups, returns random namespace name
func getNamespaceToRunTests(f *framework.Framework) string {
	return f.Namespace.Name
}

// getPersistentVolumeClaimSpecWithStorageClass return the PersistentVolumeClaim
// spec with specified storage class.
func getPersistentVolumeClaimSpecWithStorageClass(namespace string, ds string, storageclass *storagev1.StorageClass,
	pvclaimlabels map[string]string, accessMode v1.PersistentVolumeAccessMode,
) *v1.PersistentVolumeClaim {
	disksize := testParameters["diskSize"]
	if ds != "" {
		disksize = ds
	}
	if accessMode == "" {
		// If accessMode is not specified, set the default accessMode.
		accessMode = v1.ReadWriteOnce
	}
	claim := &v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "pvc-",
			Namespace:    namespace,
		},
		Spec: v1.PersistentVolumeClaimSpec{
			AccessModes: []v1.PersistentVolumeAccessMode{
				accessMode,
			},
			Resources: v1.VolumeResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceName(v1.ResourceStorage): resource.MustParse(disksize),
				},
			},
			StorageClassName: &(storageclass.Name),
		},
	}

	if pvclaimlabels != nil {
		claim.Labels = pvclaimlabels
	}

	return claim
}

// getStorageClassSpec returns Storage Class Spec with supplied storage
// class parameters.
func getStorageClassSpec(scName string, testParameters map[string]string,
	allowedTopologies []v1.TopologySelectorLabelRequirement, scReclaimPolicy v1.PersistentVolumeReclaimPolicy,
	bindingMode storagev1.VolumeBindingMode, allowVolumeExpansion bool,
) *storagev1.StorageClass {
	vals := make([]string, 0)
	vals = append(vals, testParameters["e2eCSIDriverName"])

	topo := v1.TopologySelectorLabelRequirement{
		Key:    testParameters["e2eCSIDriverName"] + "/" + testParameters["scParamStorageSystemValue"],
		Values: vals,
	}

	allowedTopologies = append(allowedTopologies, topo)

	if bindingMode == "" {
		bindingMode = storagev1.VolumeBindingImmediate
	}

	sc := &storagev1.StorageClass{
		TypeMeta: metav1.TypeMeta{
			Kind: "StorageClass",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "sc-",
		},
		Provisioner:          testParameters["e2eCSIDriverName"],
		VolumeBindingMode:    &bindingMode,
		AllowVolumeExpansion: &allowVolumeExpansion,
	}
	// If scName is specified, use that name, else auto-generate storage class
	// name.

	if scName != "" {
		sc.ObjectMeta = metav1.ObjectMeta{
			Name: scName,
		}
	}

	if testParameters != nil {
		sc.Parameters = testParameters
	}
	if allowedTopologies != nil {
		sc.AllowedTopologies = []v1.TopologySelectorTerm{
			{
				MatchLabelExpressions: allowedTopologies,
			},
		}
	}
	if scReclaimPolicy != "" {
		sc.ReclaimPolicy = &scReclaimPolicy
	}

	return sc
}

// createPVCAndStorageClass helps creates a storage class with specified name,
// storageclass parameters and PVC using storage class.
func createPVCAndStorageClass(client clientset.Interface, pvcnamespace string,
	pvclaimlabels map[string]string, testParameters map[string]string, ds string,
	allowedTopologies []v1.TopologySelectorLabelRequirement, bindingMode storagev1.VolumeBindingMode,
	allowVolumeExpansion bool, accessMode v1.PersistentVolumeAccessMode,
	names ...string,
) (*storagev1.StorageClass, *v1.PersistentVolumeClaim, error) {
	scName := ""
	if len(names) > 0 {
		scName = names[0]
	}
	storageclass, err := createStorageClass(client, testParameters,
		allowedTopologies, "", bindingMode, allowVolumeExpansion, scName)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	pvclaim, err := createPVC(client, pvcnamespace, pvclaimlabels, ds, storageclass, accessMode)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	return storageclass, pvclaim, err
}

// createStorageClass helps creates a storage class with specified name,
// storageclass parameters.
func createStorageClass(client clientset.Interface, testParameters map[string]string,
	allowedTopologies []v1.TopologySelectorLabelRequirement,
	scReclaimPolicy v1.PersistentVolumeReclaimPolicy, bindingMode storagev1.VolumeBindingMode,
	allowVolumeExpansion bool, scName string,
) (*storagev1.StorageClass, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var storageclass *storagev1.StorageClass
	var err error
	isStorageClassPresent := false
	ginkgo.By(fmt.Sprintf("Creating StorageClass %s with scParameters: %+v and allowedTopologies: %+v "+
		"and ReclaimPolicy: %+v and allowVolumeExpansion: %t",
		scName, testParameters, allowedTopologies, scReclaimPolicy, allowVolumeExpansion))

	storageclass, err = client.StorageV1().StorageClasses().Get(ctx, scName, metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		gomega.Expect(err).To(gomega.HaveOccurred())
	}

	if storageclass != nil && err == nil {
		isStorageClassPresent = true
	}

	if !isStorageClassPresent {
		storageclass, err = client.StorageV1().StorageClasses().Create(ctx, getStorageClassSpec(scName,
			testParameters, allowedTopologies, scReclaimPolicy, bindingMode, allowVolumeExpansion), metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), fmt.Sprintf("Failed to create storage class with err: %v", err))
	}

	return storageclass, err
}

// createPVC helps creates pvc with given namespace and labels using given
// storage class.
func createPVC(client clientset.Interface, pvcnamespace string, pvclaimlabels map[string]string, ds string,
	storageclass *storagev1.StorageClass, accessMode v1.PersistentVolumeAccessMode,
) (*v1.PersistentVolumeClaim, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pvcspec := getPersistentVolumeClaimSpecWithStorageClass(pvcnamespace, ds, storageclass, pvclaimlabels, accessMode)
	ginkgo.By(fmt.Sprintf("Creating PVC using the Storage Class %s with disk size %s and labels: %+v accessMode: %+v",
		storageclass.Name, ds, pvclaimlabels, accessMode))
	pvclaim, err := fpv.CreatePVC(ctx, client, pvcnamespace, pvcspec)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), fmt.Sprintf("Failed to create pvc with err: %v", err))
	framework.Logf("PVC created: %v in namespace: %v", pvclaim.Name, pvcnamespace)
	return pvclaim, err
}

// createPodForFSGroup helps create pod with fsGroup.
func createPodForFSGroup(client clientset.Interface, namespace string,
	nodeSelector map[string]string, pvclaims []*v1.PersistentVolumeClaim,
	level admissionapi.Level, command string, fsGroup *int64, runAsUser *int64,
) (*v1.Pod, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if len(command) == 0 {
		command = "trap exit TERM; while true; do sleep 1; done"
	}
	if fsGroup == nil {
		fsGroup = func(i int64) *int64 {
			return &i
		}(1000)
	}
	if runAsUser == nil {
		runAsUser = func(i int64) *int64 {
			return &i
		}(2000)
	}
	// set group id to non root id
	runAsGroup := func(i int64) *int64 {
		return &i
	}(3000)

	pod := fpod.MakePod(namespace, nodeSelector, pvclaims, level, command)
	pod.Spec.Containers[0].Image = testParameters["busyBoxImageOnGcr"]
	nonRoot := true
	pod.Spec.SecurityContext = &v1.PodSecurityContext{
		RunAsNonRoot: &nonRoot,
		RunAsUser:    runAsUser,
		RunAsGroup:   runAsGroup,
		FSGroup:      fsGroup,
	}
	var err error
	pod, err = client.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("pod Create API error: %v", err)
	}
	// Waiting for pod to be running.

	err = fpod.WaitForPodNameRunningInNamespace(ctx, client, pod.Name, namespace)
	if err != nil {
		return pod, fmt.Errorf("pod %q is not Running: %s", pod.Name, err.Error())
	}

	err = fpod.WaitTimeoutForPodReadyInNamespace(ctx, client, pod.Name, namespace, framework.PodStartTimeout)
	if err != nil {
		return pod, fmt.Errorf("pod is not Running: %s %s", pod.Name, err.Error())
	}

	// Get fresh pod info.
	pod, err = client.CoreV1().Pods(namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
	if err != nil {
		return pod, fmt.Errorf("pod Get API error: %v", err)
	}
	return pod, nil
}
