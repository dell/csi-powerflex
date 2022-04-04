package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

	"github.com/onsi/ginkgo"
)

// return csidriver object with different fsgroup values
func getCSIDriver(name string, noFsGroup bool) *storagev1.CSIDriver {
	enabled := true
	disabled := false
	driver := &storagev1.CSIDriver{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: storagev1.CSIDriverSpec{
			AttachRequired:    &enabled,
			PodInfoOnMount:    &enabled,
			StorageCapacity:   &disabled,
			RequiresRepublish: &disabled,
			VolumeLifecycleModes: []storagev1.VolumeLifecycleMode{
				storagev1.VolumeLifecyclePersistent,
				storagev1.VolumeLifecycleEphemeral,
			},
		},
	}

	if !noFsGroup {
		fsGroupPolicy := storagev1.ReadWriteOnceWithFSTypeFSGroupPolicy
		driver.Spec = storagev1.CSIDriverSpec{
			AttachRequired:    &enabled,
			PodInfoOnMount:    &enabled,
			StorageCapacity:   &disabled,
			RequiresRepublish: &disabled,
			FSGroupPolicy:     &fsGroupPolicy,
			VolumeLifecycleModes: []storagev1.VolumeLifecycleMode{
				storagev1.VolumeLifecyclePersistent,
				storagev1.VolumeLifecycleEphemeral,
			},
		}
	}
	return driver
}

// update fsGroupPolicy in CSIDriver
func updateCsiDriver(client clientset.Interface, e2eCSIDriverName, fsPolicy string) error {
	_, err := client.StorageV1().CSIDrivers().Get(context.TODO(), e2eCSIDriverName, metav1.GetOptions{})
	if err != nil {
		framework.Logf("debug csidriver not found %s, %s", e2eCSIDriverName, fsPolicy)
	} else {

		framework.Logf("csidriver delete %s, %s", e2eCSIDriverName, fsPolicy)

		err = client.StorageV1().CSIDrivers().Delete(context.TODO(), e2eCSIDriverName, metav1.DeleteOptions{})
		if err != nil {
			framework.Logf("Error deleting driver %s: %v", e2eCSIDriverName, err)
			return err
		}
	}

	driver := getCSIDriver(e2eCSIDriverName, false)
	framework.Logf("csidriver make %s, %s", driver.Name, fsPolicy)
	parts := strings.Split(fsPolicy, "=")
	if len(parts) == 2 && len(parts[1]) > 0 {
		policy := parts[1]
		switch policy {
		case "None":
			*driver.Spec.FSGroupPolicy = storagev1.NoneFSGroupPolicy
	        case "File":
		        *driver.Spec.FSGroupPolicy = storagev1.FileFSGroupPolicy
		default:
			*driver.Spec.FSGroupPolicy = storagev1.ReadWriteOnceWithFSTypeFSGroupPolicy
		}
	} else if len(parts) == 1 {
		driver = getCSIDriver(e2eCSIDriverName, true)
		framework.Logf("default csidriver create %s, %s", e2eCSIDriverName, "no fsgroup policy")
	}

	driver, err = client.StorageV1().CSIDrivers().Create(context.TODO(), driver, metav1.CreateOptions{})
	if err != nil {
		framework.Logf("Error creating driver %s: %v", e2eCSIDriverName, err)
		return err
	}
	framework.Logf("csidriver created ok %s, %s", e2eCSIDriverName, fsPolicy)
	return err
}

// delete pods in a given namespace
func restartDriverPods(client clientset.Interface, ns string) error {
	// for given namespace delete each pod
	pods, err := fpod.GetPodsInNamespace(client, ns, map[string]string{})
	framework.Logf("debug pod list %s %d", ns, len(pods))
	for i := range pods {
		p := pods[i]
		err = fpod.DeletePodWithWait(client, p)
		err := client.CoreV1().Pods(ns).Delete(context.TODO(), p.Name, metav1.DeleteOptions{})
		if err != nil {
			framework.Logf("debug pod delete %s", err.Error())
		}

		time.Sleep(10 * time.Second)
		podslist, err := fpod.GetPodsInNamespace(client, ns, map[string]string{})

		for j := range podslist {
			po := podslist[j]
			framework.Logf("debug check pod %s, %s", po.Name, ns)
			err = fpod.WaitTimeoutForPodReadyInNamespace(client, po.Name, ns, framework.PodStartTimeout)
			if err != nil {
				return fmt.Errorf("pod is not Running: %s %v", po.Name, err)
			}
		}
	}
	return err
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
	framework.Logf(fmt.Sprintf("Creating statefulset %v/%v with %d replicas and selector %+v",
		ss.Namespace, ss.Name, *(ss.Spec.Replicas), ss.Spec.Selector))

	_, err := c.AppsV1().StatefulSets(ns).Create(ctx, ss, metav1.CreateOptions{})
	framework.ExpectNoError(err)
	fss.WaitForRunningAndReady(c, *ss.Spec.Replicas, ss)
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
func bootstrap(withoutDc ...bool) {
	var err error
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	// ctx
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	//connect(ctx, &e2eVSphere)
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
	pvclaimlabels map[string]string, accessMode v1.PersistentVolumeAccessMode) *v1.PersistentVolumeClaim {
	disksize := diskSize
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
			Resources: v1.ResourceRequirements{
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
func getStorageClassSpec(scName string, scParameters map[string]string,
	allowedTopologies []v1.TopologySelectorLabelRequirement, scReclaimPolicy v1.PersistentVolumeReclaimPolicy,
	bindingMode storagev1.VolumeBindingMode, allowVolumeExpansion bool) *storagev1.StorageClass {

	vals := make([]string, 0)
	vals = append(vals, e2eCSIDriverName)

	topo := v1.TopologySelectorLabelRequirement{
		// 4d4a2e5a36080e0f"
		Key:    e2eCSIDriverName + "/" + scParamStorageSystemValue,
		Values: vals,
	}

	allowedTopologies = append(allowedTopologies, topo)

	if bindingMode == "" {
		bindingMode = storagev1.VolumeBindingWaitForFirstConsumer
	}

	var sc = &storagev1.StorageClass{
		TypeMeta: metav1.TypeMeta{
			Kind: "StorageClass",
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "sc-",
		},
		Provisioner:          e2eCSIDriverName,
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

	if scParameters != nil {
		sc.Parameters = scParameters
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
	pvclaimlabels map[string]string, scParameters map[string]string, ds string,
	allowedTopologies []v1.TopologySelectorLabelRequirement, bindingMode storagev1.VolumeBindingMode,
	allowVolumeExpansion bool, accessMode v1.PersistentVolumeAccessMode,
	names ...string) (*storagev1.StorageClass, *v1.PersistentVolumeClaim, error) {
	scName := ""
	if len(names) > 0 {
		scName = names[0]
	}
	storageclass, err := createStorageClass(client, scParameters,
		allowedTopologies, "", bindingMode, allowVolumeExpansion, scName)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	pvclaim, err := createPVC(client, pvcnamespace, pvclaimlabels, ds, storageclass, accessMode)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	return storageclass, pvclaim, err
}

// createStorageClass helps creates a storage class with specified name,
// storageclass parameters.
func createStorageClass(client clientset.Interface, scParameters map[string]string,
	allowedTopologies []v1.TopologySelectorLabelRequirement,
	scReclaimPolicy v1.PersistentVolumeReclaimPolicy, bindingMode storagev1.VolumeBindingMode,
	allowVolumeExpansion bool, scName string) (*storagev1.StorageClass, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var storageclass *storagev1.StorageClass
	var err error
	isStorageClassPresent := false
	ginkgo.By(fmt.Sprintf("Creating StorageClass %s with scParameters: %+v and allowedTopologies: %+v "+
		"and ReclaimPolicy: %+v and allowVolumeExpansion: %t",
		scName, scParameters, allowedTopologies, scReclaimPolicy, allowVolumeExpansion))

	storageclass, err = client.StorageV1().StorageClasses().Get(ctx, scName, metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		gomega.Expect(err).To(gomega.HaveOccurred())
	}

	if storageclass != nil && err == nil {
		isStorageClassPresent = true
	}

	if !isStorageClassPresent {
		storageclass, err = client.StorageV1().StorageClasses().Create(ctx, getStorageClassSpec(scName,
			scParameters, allowedTopologies, scReclaimPolicy, bindingMode, allowVolumeExpansion), metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred(), fmt.Sprintf("Failed to create storage class with err: %v", err))
	}

	return storageclass, err
}

// createPVC helps creates pvc with given namespace and labels using given
// storage class.
func createPVC(client clientset.Interface, pvcnamespace string, pvclaimlabels map[string]string, ds string,
	storageclass *storagev1.StorageClass, accessMode v1.PersistentVolumeAccessMode) (*v1.PersistentVolumeClaim, error) {
	pvcspec := getPersistentVolumeClaimSpecWithStorageClass(pvcnamespace, ds, storageclass, pvclaimlabels, accessMode)
	ginkgo.By(fmt.Sprintf("Creating PVC using the Storage Class %s with disk size %s and labels: %+v accessMode: %+v",
		storageclass.Name, ds, pvclaimlabels, accessMode))
	pvclaim, err := fpv.CreatePVC(client, pvcnamespace, pvcspec)
	gomega.Expect(err).NotTo(gomega.HaveOccurred(), fmt.Sprintf("Failed to create pvc with err: %v", err))
	framework.Logf("PVC created: %v in namespace: %v", pvclaim.Name, pvcnamespace)
	return pvclaim, err
}

// createPodForFSGroup helps create pod with fsGroup.
func createPodForFSGroup(client clientset.Interface, namespace string,
	nodeSelector map[string]string, pvclaims []*v1.PersistentVolumeClaim,
	isPrivileged bool, command string, fsGroup *int64, runAsUser *int64) (*v1.Pod, error) {
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

	pod := fpod.MakePod(namespace, nodeSelector, pvclaims, isPrivileged, command)
	pod.Spec.Containers[0].Image = busyBoxImageOnGcr
	nonRoot := true
	pod.Spec.SecurityContext = &v1.PodSecurityContext{
		RunAsNonRoot: &nonRoot,
		RunAsUser:    runAsUser,
		RunAsGroup:   runAsGroup,
		FSGroup:      fsGroup,
	}
	var err error
	pod, err = client.CoreV1().Pods(namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("pod Create API error: %v", err)
	}
	// Waiting for pod to be running.

	err = fpod.WaitForPodNameRunningInNamespace(client, pod.Name, namespace)
	if err != nil {
		return pod, fmt.Errorf("pod %q is not Running: %s", pod.Name, err.Error())
	}

	err = fpod.WaitTimeoutForPodReadyInNamespace(client, pod.Name, namespace, framework.PodStartTimeout)
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
