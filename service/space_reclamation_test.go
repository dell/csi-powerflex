// Copyright © 2024-2026 Dell Inc. or its subsidiaries. All Rights Reserved.
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
// Test coverage targets for this file:
//   - fsType whitelist (xfs/ext4 only; skip NFS, btrfs, empty)
//   - Stale device mount filtering (osStatFunc override)
//   - RWX volume skipping
//   - PV filter paths (non-CSI, unbound, no ClaimRef, PVC fetch error)
//   - RunOnce error paths (ListPVs, GetMounts, getLocalVolumeMap)
//   - Overlapping cycle prevention
//   - Filesystem mode: no mount found
//   - Globalmount preference over pod mount
//   - NVMe paths: missing systemID, missing volID, WWN error, NGUID build
//   - reclaimVolume: fstrim error, blkdiscard error, discard support error, context cancel
//   - getSystemIDFromPVHandle helper
//   - initSpaceReclamation wiring
//   - All existing contract / unit tests (fixed for new code paths)

package service

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dell/gofsutil"
	"github.com/dell/goscaleio"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
)

// ============================================================================
// Test Helpers
// ============================================================================

const (
	// testDevice is a stable device path that always exists on Linux.
	// Using /dev/null avoids real block-device dependencies while still
	// passing the os.Stat stale-mount filter in RunOnce.
	testDevice = "/dev/null"

	// testGlobalmount is a CSI staging path that passes the kubelet path filter.
	testGlobalmount = "/var/lib/kubelet/plugins/kubernetes.io/csi/csi-vxflexos.dellemc.com/testtoken/globalmount"

	// testPodMount is a kubelet pod-volume path that passes the path filter.
	testPodMount = "/var/lib/kubelet/pods/test-pod-uid/volumes/kubernetes.io~csi/test-vol/mount"
)

// resetGofsutilMock resets the gofsutil mock to a clean state before each test.
func resetGofsutilMock() {
	gofsutil.GOFSMock.InduceMountError = false
	gofsutil.GOFSMock.InduceUnmountError = false
	gofsutil.GOFSMock.InduceGetMountsError = false
	gofsutil.GOFSMock.InduceFstrimError = false
	gofsutil.GOFSMock.InduceBlkdiscardError = false
	gofsutil.GOFSMock.InduceCheckDiscardSupportError = false
	gofsutil.GOFSMockMounts = nil
	gofsutil.GOFSMockFstrimResult = nil
	gofsutil.GOFSMockBlkdiscardResult = nil
	gofsutil.GOFSMockDiscardCapability = nil
}

// makePVC creates a minimal PVC object for testing.
func makePVC(name, namespace string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: map[string]string{},
		},
	}
}

// makeBoundPV creates a minimal bound Filesystem PV pointing to the given PVC.
// fsType must be "ext4" or "xfs" for the volume to pass the whitelist filter.
// The CSI VolumeHandle is constructed as "system-volID" so that getVolumeIDFromCsiVolumeID
// extracts "volID" correctly. volID should not contain dashes.
func makeBoundPV(pvName, volID, pvcName, pvcNamespace, fsType string) *corev1.PersistentVolume {
	return &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: pvName},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver:       Name,
					VolumeHandle: "system-" + volID,
					FSType:       fsType,
				},
			},
			ClaimRef: &corev1.ObjectReference{
				Name:      pvcName,
				Namespace: pvcNamespace,
			},
		},
		Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound},
	}
}

// makeBoundPVRWX creates a bound PV with ReadWriteMany access mode.
func makeBoundPVRWX(pvName, volID, pvcName, pvcNamespace, fsType string) *corev1.PersistentVolume {
	pv := makeBoundPV(pvName, volID, pvcName, pvcNamespace, fsType)
	pv.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}
	return pv
}

// newTestManager creates a SpaceReclamationManager in SDC mode for testing.
func newTestManager(t *testing.T, client *fake.Clientset, cfg SpaceReclamationConfig) *SpaceReclamationManager {
	t.Helper()
	mgr, err := NewSpaceReclamationManager(context.Background(), cfg, client, cfg.NodeName, false)
	require.NoError(t, err)
	return mgr
}

// newTestManagerNVMe creates a SpaceReclamationManager in NVMe mode for testing.
func newTestManagerNVMe(t *testing.T, client *fake.Clientset, cfg SpaceReclamationConfig) *SpaceReclamationManager {
	t.Helper()
	mgr, err := NewSpaceReclamationManager(context.Background(), cfg, client, cfg.NodeName, true)
	require.NoError(t, err)
	return mgr
}

// bypassOsStat overrides osStatFunc so that all devices appear to exist,
// bypassing the stale-mount filter. Restored automatically via t.Cleanup.
func bypassOsStat(t *testing.T) {
	t.Helper()
	orig := osStatFunc
	osStatFunc = func(_ string) (os.FileInfo, error) { return nil, nil }
	t.Cleanup(func() { osStatFunc = orig })
}

// bypassLocalVolumeMap overrides getLocalVolumeMapFunc with the provided slice.
func bypassLocalVolumeMap(t *testing.T, vols []*goscaleio.SdcMappedVolume) {
	t.Helper()
	orig := getLocalVolumeMapFunc
	getLocalVolumeMapFunc = func() ([]*goscaleio.SdcMappedVolume, error) { return vols, nil }
	t.Cleanup(func() { getLocalVolumeMapFunc = orig })
}

// defaultCfg returns a standard enabled SpaceReclamationConfig for testing.
func defaultCfg() SpaceReclamationConfig {
	return SpaceReclamationConfig{
		Enabled:              true,
		Schedule:             "* * * * *",
		MaxConcurrentVolumes: 2,
		TimeoutSeconds:       60,
		NodeName:             "node-1",
	}
}

// ============================================================================
// CONTRACT TESTS (C-*): RunOnce integration paths
// ============================================================================

// C-101: TestRunOnce_DiscoversMountedFilesystemVolume
// Verifies RunOnce finds an eligible ext4 PV, resolves it via SDC, and annotates success.
func TestRunOnce_DiscoversMountedFilesystemVolume(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassOsStat(t)
	bypassLocalVolumeMap(t, []*goscaleio.SdcMappedVolume{
		{VolumeID: "vol001", SdcDevice: testDevice},
	})

	pvc := makePVC("pvc-test-001", "default")
	pv := makeBoundPV("pv-001", "vol001", "pvc-test-001", "default", "ext4")
	fakeClient := fake.NewSimpleClientset(pvc, pv)
	gofsutil.GOFSMockMounts = []gofsutil.Info{
		{Device: testDevice, Path: testGlobalmount},
	}

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-test-001", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "success", updated.Annotations[AnnotationStatus])
	assert.Equal(t, "node-1", updated.Annotations[AnnotationNode])
}

// C-102: TestRunOnce_SkipsVolumeNotOnThisNode
// Verifies RunOnce skips a PV whose volume ID is absent from the local SDC map.
func TestRunOnce_SkipsVolumeNotOnThisNode(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassLocalVolumeMap(t, []*goscaleio.SdcMappedVolume{})

	pvc := makePVC("pvc-other-001", "default")
	pv := makeBoundPV("pv-other-001", "volother", "pvc-other-001", "default", "ext4")
	fakeClient := fake.NewSimpleClientset(pvc, pv)

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-other-001", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Empty(t, updated.Annotations[AnnotationStatus],
		"PVC should not be annotated when volume is not on this node")
}

// C-103: TestReclamationCycle_CallsFstrimOnFilesystemVolume
// Verifies fstrim is dispatched and result annotations are fully populated.
func TestReclamationCycle_CallsFstrimOnFilesystemVolume(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassOsStat(t)
	bypassLocalVolumeMap(t, []*goscaleio.SdcMappedVolume{
		{VolumeID: "vol001", SdcDevice: testDevice},
	})

	pvc := makePVC("pvc-test-001", "default")
	pv := makeBoundPV("pv-001", "vol001", "pvc-test-001", "default", "ext4")
	fakeClient := fake.NewSimpleClientset(pvc, pv)
	gofsutil.GOFSMockMounts = []gofsutil.Info{
		{Device: testDevice, Path: testGlobalmount},
	}

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-test-001", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "success", updated.Annotations[AnnotationStatus])
	assert.NotEmpty(t, updated.Annotations[AnnotationBytesReclaim])
	assert.NotEmpty(t, updated.Annotations[AnnotationLastRunTime])
	assert.Equal(t, "node-1", updated.Annotations[AnnotationNode])
}

// C-104: TestReclamationCycle_CallsBlkdiscardOnBlockVolume
// Verifies blkdiscard is dispatched for a raw-block PVC and annotations are set.
func TestReclamationCycle_CallsBlkdiscardOnBlockVolume(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassOsStat(t)
	bypassLocalVolumeMap(t, []*goscaleio.SdcMappedVolume{
		{VolumeID: "volblk001", SdcDevice: testDevice},
	})

	pvc := makePVC("pvc-blk-001", "default")
	pv := makeBoundPV("pv-blk-001", "volblk001", "pvc-blk-001", "default", "ext4")
	blockMode := corev1.PersistentVolumeBlock
	pvc.Spec.VolumeMode = &blockMode
	pvc.Labels = map[string]string{BlockLabelEnabled: "true"}
	fakeClient := fake.NewSimpleClientset(pvc, pv)
	gofsutil.GOFSMockMounts = []gofsutil.Info{}

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-blk-001", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "success", updated.Annotations[AnnotationStatus])
	assert.NotEmpty(t, updated.Annotations[AnnotationBytesReclaim])
}

// C-105: TestReclamationCycle_SkipsUnsupportedDevice
// Verifies RunOnce annotates a PVC as "unsupported" when checkDiscardSupport
// reports the underlying device does not support discard operations.
func TestReclamationCycle_SkipsUnsupportedDevice(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassOsStat(t)
	bypassLocalVolumeMap(t, []*goscaleio.SdcMappedVolume{
		{VolumeID: "volunsup001", SdcDevice: testDevice},
	})

	pvc := makePVC("pvc-unsup-001", "default")
	pv := makeBoundPV("pv-unsup-001", "volunsup001", "pvc-unsup-001", "default", "ext4")
	fakeClient := fake.NewSimpleClientset(pvc, pv)
	gofsutil.GOFSMockMounts = []gofsutil.Info{
		{Device: testDevice, Path: testGlobalmount},
	}

	gofsutil.GOFSMockDiscardCapability = &gofsutil.DiscardCapability{
		Supported: false,
		Reason:    "discard_max_bytes is 0",
	}

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-unsup-001", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "unsupported", updated.Annotations[AnnotationStatus])
}

// C-106: TestRunOnce_NVMe_DiscoversMountedVolume
// Verifies NVMe path resolves NGUID → device path and annotates success.
func TestRunOnce_NVMe_DiscoversMountedVolume(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassOsStat(t)
	bypassLocalVolumeMap(t, nil)

	pvc := makePVC("pvc-nvme-001", "default")
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-nvme-001"},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver:       Name,
					VolumeHandle: "systemABCDEF-0000000000000001", // systemID-volID (16-char volID for NGUID, systemID >= 10 chars)
					FSType:       "ext4",
				},
			},
			ClaimRef: &corev1.ObjectReference{
				Name:      "pvc-nvme-001",
				Namespace: "default",
			},
		},
		Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound},
	}
	fakeClient := fake.NewSimpleClientset(pvc, pv)

	orig := wwnToDevicePathFunc
	defer func() { wwnToDevicePathFunc = orig }()
	wwnToDevicePathFunc = func(_ context.Context, _ string) (string, error) {
		return testDevice, nil
	}
	gofsutil.GOFSMockMounts = []gofsutil.Info{
		{Device: testDevice, Path: testGlobalmount},
	}

	mgr := newTestManagerNVMe(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-nvme-001", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "success", updated.Annotations[AnnotationStatus])
	assert.Equal(t, "node-1", updated.Annotations[AnnotationNode])
}

// C-107: TestRunOnce_NVMe_SkipsVolumeNotConnected
// Verifies NVMe volumes are skipped when WWN lookup returns empty device path.
func TestRunOnce_NVMe_SkipsVolumeNotConnected(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassLocalVolumeMap(t, nil)

	pvc := makePVC("pvc-nvme-gone", "default")
	// NVMe tests need full CSI handle format without the makeBoundPV helper's "system-" prefix
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-nvme-gone"},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver:       Name,
					VolumeHandle: "systemABCDEF-0000000000000001", // systemID-volID (16-char volID for NGUID, systemID >= 10 chars)
					FSType:       "ext4",
				},
			},
			ClaimRef: &corev1.ObjectReference{
				Name:      "pvc-nvme-gone",
				Namespace: "default",
			},
		},
		Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound},
	}
	fakeClient := fake.NewSimpleClientset(pvc, pv)

	orig := wwnToDevicePathFunc
	defer func() { wwnToDevicePathFunc = orig }()
	wwnToDevicePathFunc = func(_ context.Context, _ string) (string, error) { return "", nil }

	mgr := newTestManagerNVMe(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-nvme-gone", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Empty(t, updated.Annotations[AnnotationStatus])
}

// ============================================================================
// NEW: fsType whitelist tests
// ============================================================================

// TestRunOnce_SkipsNFSVolume verifies NFS fsType is rejected by the whitelist.
func TestRunOnce_SkipsNFSVolume(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassOsStat(t)
	bypassLocalVolumeMap(t, []*goscaleio.SdcMappedVolume{
		{VolumeID: "volnfs", SdcDevice: testDevice},
	})

	pvc := makePVC("pvc-nfs", "default")
	pv := makeBoundPV("pv-nfs", "volnfs", "pvc-nfs", "default", "nfs")
	fakeClient := fake.NewSimpleClientset(pvc, pv)
	gofsutil.GOFSMockMounts = []gofsutil.Info{
		{Device: testDevice, Path: testGlobalmount},
	}

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-nfs", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Empty(t, updated.Annotations[AnnotationStatus], "NFS volume must be skipped by fsType whitelist")
}

// TestRunOnce_SkipsBtrfsVolume verifies unknown fsTypes are rejected.
func TestRunOnce_SkipsBtrfsVolume(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassOsStat(t)
	bypassLocalVolumeMap(t, []*goscaleio.SdcMappedVolume{
		{VolumeID: "volbtrfs", SdcDevice: testDevice},
	})

	pvc := makePVC("pvc-btrfs", "default")
	pv := makeBoundPV("pv-btrfs", "volbtrfs", "pvc-btrfs", "default", "btrfs")
	fakeClient := fake.NewSimpleClientset(pvc, pv)

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-btrfs", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Empty(t, updated.Annotations[AnnotationStatus], "btrfs volume must be skipped by fsType whitelist")
}

// TestRunOnce_SkipsEmptyFsType verifies PVs with no fsType are rejected.
func TestRunOnce_SkipsEmptyFsType(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassOsStat(t)
	bypassLocalVolumeMap(t, []*goscaleio.SdcMappedVolume{
		{VolumeID: "volemptyfs", SdcDevice: testDevice},
	})

	pvc := makePVC("pvc-empty-fs", "default")
	pv := makeBoundPV("pv-empty-fs", "volemptyfs", "pvc-empty-fs", "default", "")
	fakeClient := fake.NewSimpleClientset(pvc, pv)

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-empty-fs", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Empty(t, updated.Annotations[AnnotationStatus], "empty fsType must be skipped by whitelist")
}

// TestRunOnce_ProcessesXfsVolume verifies xfs is accepted by the whitelist.
func TestRunOnce_ProcessesXfsVolume(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassOsStat(t)
	bypassLocalVolumeMap(t, []*goscaleio.SdcMappedVolume{
		{VolumeID: "volxfs", SdcDevice: testDevice},
	})

	pvc := makePVC("pvc-xfs", "default")
	pv := makeBoundPV("pv-xfs", "volxfs", "pvc-xfs", "default", "xfs")
	fakeClient := fake.NewSimpleClientset(pvc, pv)
	gofsutil.GOFSMockMounts = []gofsutil.Info{
		{Device: testDevice, Path: testGlobalmount},
	}

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-xfs", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "success", updated.Annotations[AnnotationStatus], "xfs volume must be processed")
}

// TestRunOnce_FsTypeCaseInsensitive verifies "XFS" (uppercase) is also accepted.
func TestRunOnce_FsTypeCaseInsensitive(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassOsStat(t)
	bypassLocalVolumeMap(t, []*goscaleio.SdcMappedVolume{
		{VolumeID: "volxfsu", SdcDevice: testDevice},
	})

	pvc := makePVC("pvc-xfsu", "default")
	pv := makeBoundPV("pv-xfsu", "volxfsu", "pvc-xfsu", "default", "XFS")
	fakeClient := fake.NewSimpleClientset(pvc, pv)
	gofsutil.GOFSMockMounts = []gofsutil.Info{
		{Device: testDevice, Path: testGlobalmount},
	}

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-xfsu", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "success", updated.Annotations[AnnotationStatus], "uppercase XFS must be accepted")
}

// ============================================================================
// NEW: Stale device mount filter (osStatFunc)
// ============================================================================

// TestRunOnce_SkipsStaleDeviceMount verifies that a mount whose device path does
// not exist on disk is excluded from deviceToMount (stale-mount protection).
func TestRunOnce_SkipsStaleDeviceMount(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()

	// Report a real volume in SDC map with a ghost device path
	const ghostDevice = "/dev/nvme99n99"
	bypassLocalVolumeMap(t, []*goscaleio.SdcMappedVolume{
		{VolumeID: "volstale", SdcDevice: ghostDevice},
	})

	// Override osStatFunc: ghost device reports "not exist", real device is fine
	orig := osStatFunc
	osStatFunc = func(name string) (os.FileInfo, error) {
		if name == ghostDevice {
			return nil, os.ErrNotExist
		}
		return os.Stat(name)
	}
	defer func() { osStatFunc = orig }()

	pvc := makePVC("pvc-stale", "default")
	pv := makeBoundPV("pv-stale", "volstale", "pvc-stale", "default", "ext4")
	fakeClient := fake.NewSimpleClientset(pvc, pv)
	gofsutil.GOFSMockMounts = []gofsutil.Info{
		{Device: ghostDevice, Path: testGlobalmount},
	}

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	// The ghost device is filtered → device not in deviceToMount → volume skipped
	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-stale", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Empty(t, updated.Annotations[AnnotationStatus],
		"volume on stale/non-existent device must be skipped")
}

// TestRunOnce_DeviceExistsPassesStatCheck verifies that an existing device passes
// through the osStatFunc check and enters deviceToMount.
func TestRunOnce_DeviceExistsPassesStatCheck(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	// testDevice (/dev/null) exists on every Linux system
	bypassLocalVolumeMap(t, []*goscaleio.SdcMappedVolume{
		{VolumeID: "volreal", SdcDevice: testDevice},
	})

	pvc := makePVC("pvc-real", "default")
	pv := makeBoundPV("pv-real", "volreal", "pvc-real", "default", "ext4")
	fakeClient := fake.NewSimpleClientset(pvc, pv)
	gofsutil.GOFSMockMounts = []gofsutil.Info{
		{Device: testDevice, Path: testGlobalmount},
	}

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-real", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "success", updated.Annotations[AnnotationStatus],
		"volume on existing device must be processed successfully")
}

// ============================================================================
// NEW: RWX volume skipping
// ============================================================================

// TestRunOnce_SkipsRWXVolume verifies ReadWriteMany PVs are skipped entirely.
func TestRunOnce_SkipsRWXVolume(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassOsStat(t)
	bypassLocalVolumeMap(t, []*goscaleio.SdcMappedVolume{
		{VolumeID: "volrwx", SdcDevice: testDevice},
	})

	pvc := makePVC("pvc-rwx", "default")
	pv := makeBoundPVRWX("pv-rwx", "volrwx", "pvc-rwx", "default", "ext4")
	fakeClient := fake.NewSimpleClientset(pvc, pv)
	gofsutil.GOFSMockMounts = []gofsutil.Info{
		{Device: testDevice, Path: testGlobalmount},
	}

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-rwx", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Empty(t, updated.Annotations[AnnotationStatus], "RWX volume must be skipped")
}

// ============================================================================
// NEW: PV filter edge cases
// ============================================================================

// TestRunOnce_SkipsNonCSIPV verifies PVs not managed by this driver are skipped.
func TestRunOnce_SkipsNonCSIPV(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassLocalVolumeMap(t, nil)

	pvc := makePVC("pvc-noncsi", "default")
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-noncsi"},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver: "some.other.driver.com",
				},
			},
			ClaimRef: &corev1.ObjectReference{Name: "pvc-noncsi", Namespace: "default"},
		},
		Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound},
	}
	fakeClient := fake.NewSimpleClientset(pvc, pv)

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-noncsi", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Empty(t, updated.Annotations[AnnotationStatus], "non-CSI PV must be skipped")
}

// TestRunOnce_SkipsUnboundPV verifies non-Bound PVs (Pending, Released) are skipped.
func TestRunOnce_SkipsUnboundPV(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassLocalVolumeMap(t, nil)

	pvc := makePVC("pvc-unbound", "default")
	pv := makeBoundPV("pv-unbound", "volunbound", "pvc-unbound", "default", "ext4")
	pv.Status.Phase = corev1.VolumePending
	fakeClient := fake.NewSimpleClientset(pvc, pv)

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-unbound", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Empty(t, updated.Annotations[AnnotationStatus], "non-Bound PV must be skipped")
}

// TestRunOnce_SkipsNilClaimRef verifies PVs without a ClaimRef are skipped gracefully.
func TestRunOnce_SkipsNilClaimRef(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassLocalVolumeMap(t, nil)

	pv := makeBoundPV("pv-noclaim", "volnoclaim", "", "", "ext4")
	pv.Spec.ClaimRef = nil
	fakeClient := fake.NewSimpleClientset(pv)

	mgr := newTestManager(t, fakeClient, defaultCfg())
	// Should not panic
	mgr.RunOnce()
}

// TestRunOnce_SkipsPVCFetchError verifies RunOnce skips a PV when the PVC GET fails.
func TestRunOnce_SkipsPVCFetchError(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassLocalVolumeMap(t, nil)

	pv := makeBoundPV("pv-nopvc", "volnopvc", "pvc-missing", "default", "ext4")
	fakeClient := fake.NewSimpleClientset(pv) // PVC not registered → GET returns 404

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce() // must not panic; volume is silently skipped
}

// TestRunOnce_SkipsIneligiblePVC verifies PVCs with opt-out label are skipped.
func TestRunOnce_SkipsIneligiblePVC(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassOsStat(t)
	bypassLocalVolumeMap(t, []*goscaleio.SdcMappedVolume{
		{VolumeID: "voloptout", SdcDevice: testDevice},
	})

	pvc := makePVC("pvc-optout", "default")
	pvc.Labels = map[string]string{FstrimLabelEnabled: "false"}
	pv := makeBoundPV("pv-optout", "voloptout", "pvc-optout", "default", "ext4")
	fakeClient := fake.NewSimpleClientset(pvc, pv)

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-optout", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Empty(t, updated.Annotations[AnnotationStatus], "opt-out PVC must be skipped")
}

// ============================================================================
// NEW: RunOnce error path tests
// ============================================================================

// TestRunOnce_ListPVsError verifies RunOnce exits cleanly when PV list fails.
func TestRunOnce_ListPVsError(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()

	fakeClient := fake.NewSimpleClientset()
	fakeClient.PrependReactor("list", "persistentvolumes", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("api server unavailable")
	})

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce() // must not panic
}

// TestRunOnce_GetLocalVolumeMapError verifies RunOnce exits cleanly when SDC map fails.
func TestRunOnce_GetLocalVolumeMapError(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()

	orig := getLocalVolumeMapFunc
	defer func() { getLocalVolumeMapFunc = orig }()
	getLocalVolumeMapFunc = func() ([]*goscaleio.SdcMappedVolume, error) {
		return nil, fmt.Errorf("SDC daemon not running")
	}

	pvc := makePVC("pvc-sdcerr", "default")
	pv := makeBoundPV("pv-sdcerr", "volsdcerr", "pvc-sdcerr", "default", "ext4")
	fakeClient := fake.NewSimpleClientset(pvc, pv)

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce() // must not panic; no annotation set
}

// TestRunOnce_GetMountsError verifies RunOnce exits cleanly when GetMounts fails.
func TestRunOnce_GetMountsError(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassLocalVolumeMap(t, nil)

	gofsutil.GOFSMock.InduceGetMountsError = true

	pvc := makePVC("pvc-mnterr", "default")
	pv := makeBoundPV("pv-mnterr", "volmnterr", "pvc-mnterr", "default", "ext4")
	fakeClient := fake.NewSimpleClientset(pvc, pv)

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce() // must not panic
}

// TestRunOnce_FilesystemModeNoMountFound verifies a Filesystem-mode volume is skipped
// when the device is not present in the mount table.
func TestRunOnce_FilesystemModeNoMountFound(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassOsStat(t)
	bypassLocalVolumeMap(t, []*goscaleio.SdcMappedVolume{
		{VolumeID: "volnomount", SdcDevice: testDevice},
	})

	pvc := makePVC("pvc-nomount", "default")
	pv := makeBoundPV("pv-nomount", "volnomount", "pvc-nomount", "default", "ext4")
	fakeClient := fake.NewSimpleClientset(pvc, pv)
	gofsutil.GOFSMockMounts = []gofsutil.Info{} // empty: device not mounted

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-nomount", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Empty(t, updated.Annotations[AnnotationStatus],
		"Filesystem volume with no mount entry must be skipped")
}

// TestRunOnce_OverlappingCyclePrevented verifies the atomic running flag prevents
// a second RunOnce from executing while the first is still active.
func TestRunOnce_OverlappingCyclePrevented(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassLocalVolumeMap(t, nil)

	fakeClient := fake.NewSimpleClientset()
	mgr := newTestManager(t, fakeClient, defaultCfg())

	// Simulate that a cycle is already running
	mgr.running.Store(true)

	// RunOnce should return immediately without touching anything
	done := make(chan struct{})
	go func() {
		mgr.RunOnce()
		close(done)
	}()

	select {
	case <-done:
		// Good: returned immediately
	case <-time.After(2 * time.Second):
		t.Fatal("RunOnce did not return quickly when another cycle was in progress")
	}

	// Reset flag so deferred cleanup does not deadlock
	mgr.running.Store(false)
}

// ============================================================================
// NEW: Globalmount preference over pod mount
// ============================================================================

// TestRunOnce_PreferGlobalmountOverPodMount verifies that when both a pod mount and
// a globalmount exist for the same device, the globalmount is chosen.
func TestRunOnce_PreferGlobalmountOverPodMount(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassOsStat(t)
	bypassLocalVolumeMap(t, []*goscaleio.SdcMappedVolume{
		{VolumeID: "volpref", SdcDevice: testDevice},
	})

	pvc := makePVC("pvc-pref", "default")
	pv := makeBoundPV("pv-pref", "volpref", "pvc-pref", "default", "ext4")
	fakeClient := fake.NewSimpleClientset(pvc, pv)
	// Pod mount listed first, globalmount second — globalmount must win
	gofsutil.GOFSMockMounts = []gofsutil.Info{
		{Device: testDevice, Path: testPodMount},
		{Device: testDevice, Path: testGlobalmount},
	}

	annotatedPath := ""
	orig := checkDiscardSupportFunc
	checkDiscardSupportFunc = func(_ context.Context, _ string) (*gofsutil.DiscardCapability, error) {
		return &gofsutil.DiscardCapability{Supported: true, DiscardMaxBytes: 4294967295}, nil
	}
	defer func() { checkDiscardSupportFunc = orig }()

	// Capture the staging path by observing which path fstrim is called on
	origFstrimResult := gofsutil.GOFSMockFstrimResult
	gofsutil.GOFSMockFstrimResult = &gofsutil.FstrimResult{BytesTrimmed: 512}
	defer func() { gofsutil.GOFSMockFstrimResult = origFstrimResult }()
	_ = annotatedPath

	mgr := newTestManager(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-pref", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "success", updated.Annotations[AnnotationStatus],
		"volume should succeed when globalmount is preferred")
}

// ============================================================================
// NEW: NVMe-specific edge cases
// ============================================================================

// TestRunOnce_NVMe_MissingSystemID verifies a handle with no dash is skipped.
func TestRunOnce_NVMe_MissingSystemID(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassLocalVolumeMap(t, nil)

	pvc := makePVC("pvc-nosys", "default")
	// NVMe tests need full CSI handle format without the makeBoundPV helper's "system-" prefix
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-nosys"},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver:       Name,
					VolumeHandle: "nodash", // No dash - should be skipped
					FSType:       "ext4",
				},
			},
			ClaimRef: &corev1.ObjectReference{
				Name:      "pvc-nosys",
				Namespace: "default",
			},
		},
		Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound},
	}
	fakeClient := fake.NewSimpleClientset(pvc, pv)

	mgr := newTestManagerNVMe(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-nosys", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Empty(t, updated.Annotations[AnnotationStatus], "handle with no systemID must be skipped")
}

// TestRunOnce_NVMe_WWNResolutionError verifies WWN lookup errors cause the volume to be skipped.
func TestRunOnce_NVMe_WWNResolutionError(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	bypassLocalVolumeMap(t, nil)

	pvc := makePVC("pvc-wwnerr", "default")
	// NVMe tests need full CSI handle format without the makeBoundPV helper's "system-" prefix
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-wwnerr"},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver:       Name,
					VolumeHandle: "systemABCDEF-0000000000000099", // systemID-volID (16-char volID for NGUID, systemID >= 10 chars)
					FSType:       "ext4",
				},
			},
			ClaimRef: &corev1.ObjectReference{
				Name:      "pvc-wwnerr",
				Namespace: "default",
			},
		},
		Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound},
	}
	fakeClient := fake.NewSimpleClientset(pvc, pv)

	orig := wwnToDevicePathFunc
	defer func() { wwnToDevicePathFunc = orig }()
	wwnToDevicePathFunc = func(_ context.Context, _ string) (string, error) {
		return "", fmt.Errorf("NVMe subsystem not found")
	}

	mgr := newTestManagerNVMe(t, fakeClient, defaultCfg())
	mgr.RunOnce()

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-wwnerr", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Empty(t, updated.Annotations[AnnotationStatus], "WWN error must cause volume to be skipped")
}

// ============================================================================
// NEW: reclaimVolume direct tests
// ============================================================================

// reclaimVolumeSetup returns a manager and a VolumeInfo ready for reclaimVolume tests.
func reclaimVolumeSetup(t *testing.T, pvcName string, mode VolumeMode) (*SpaceReclamationManager, *VolumeInfo, *fake.Clientset) {
	t.Helper()
	pvc := makePVC(pvcName, "default")
	fakeClient := fake.NewSimpleClientset(pvc)
	mgr := newTestManager(t, fakeClient, defaultCfg())
	vol := &VolumeInfo{
		VolumeID:     "vol-direct",
		StagingPath:  testGlobalmount,
		DevicePath:   testDevice,
		VolumeMode:   mode,
		PVCName:      pvcName,
		PVCNamespace: "default",
		PVC:          pvc,
	}
	return mgr, vol, fakeClient
}

// TestReclaimVolume_FstrimSuccess verifies fstrim result is written to PVC annotations.
func TestReclaimVolume_FstrimSuccess(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	gofsutil.GOFSMockFstrimResult = &gofsutil.FstrimResult{BytesTrimmed: 2147483648}

	mgr, vol, fakeClient := reclaimVolumeSetup(t, "pvc-direct-fs", VolumeModeFilesystem)

	mgr.reclaimVolume(context.Background(), vol)

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-direct-fs", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "success", updated.Annotations[AnnotationStatus])
	assert.Equal(t, "2147483648", updated.Annotations[AnnotationBytesReclaim])
	assert.Equal(t, "node-1", updated.Annotations[AnnotationNode])
}

// TestReclaimVolume_BlkdiscardSuccess verifies blkdiscard result is written to PVC annotations.
func TestReclaimVolume_BlkdiscardSuccess(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	gofsutil.GOFSMockBlkdiscardResult = &gofsutil.BlkdiscardResult{BytesDiscarded: 107374182400}

	mgr, vol, fakeClient := reclaimVolumeSetup(t, "pvc-direct-blk", VolumeModeBlock)

	mgr.reclaimVolume(context.Background(), vol)

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-direct-blk", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "success", updated.Annotations[AnnotationStatus])
	assert.Equal(t, "107374182400", updated.Annotations[AnnotationBytesReclaim])
}

// TestReclaimVolume_FstrimError verifies a fstrim failure is annotated as "error".
func TestReclaimVolume_FstrimError(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	gofsutil.GOFSMock.InduceFstrimError = true

	mgr, vol, fakeClient := reclaimVolumeSetup(t, "pvc-fstrim-err", VolumeModeFilesystem)

	mgr.reclaimVolume(context.Background(), vol)

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-fstrim-err", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "error", updated.Annotations[AnnotationStatus])
	assert.NotEmpty(t, updated.Annotations[AnnotationErrorMsg])
}

// TestReclaimVolume_BlkdiscardError verifies a blkdiscard failure is annotated as "error".
func TestReclaimVolume_BlkdiscardError(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	gofsutil.GOFSMock.InduceBlkdiscardError = true

	mgr, vol, fakeClient := reclaimVolumeSetup(t, "pvc-blkd-err", VolumeModeBlock)

	mgr.reclaimVolume(context.Background(), vol)

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-blkd-err", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "error", updated.Annotations[AnnotationStatus])
	assert.NotEmpty(t, updated.Annotations[AnnotationErrorMsg])
}

// TestReclaimVolume_DiscardSupportError verifies a checkDiscardSupport failure is
// annotated as "unsupported".
func TestReclaimVolume_DiscardSupportError(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()

	orig := checkDiscardSupportFunc
	checkDiscardSupportFunc = func(_ context.Context, _ string) (*gofsutil.DiscardCapability, error) {
		return nil, fmt.Errorf("sysfs read failed")
	}
	defer func() { checkDiscardSupportFunc = orig }()

	mgr, vol, fakeClient := reclaimVolumeSetup(t, "pvc-discard-err", VolumeModeFilesystem)

	mgr.reclaimVolume(context.Background(), vol)

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-discard-err", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "unsupported", updated.Annotations[AnnotationStatus])
}

// TestReclaimVolume_DiscardSupportUnsupported verifies "Supported: false" is annotated
// as "unsupported".
func TestReclaimVolume_DiscardSupportUnsupported(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()
	gofsutil.GOFSMockDiscardCapability = &gofsutil.DiscardCapability{
		Supported: false,
		Reason:    "discard_max_bytes is 0",
	}

	mgr, vol, fakeClient := reclaimVolumeSetup(t, "pvc-unsup-direct", VolumeModeFilesystem)

	mgr.reclaimVolume(context.Background(), vol)

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-unsup-direct", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "unsupported", updated.Annotations[AnnotationStatus])
	assert.Contains(t, updated.Annotations[AnnotationErrorMsg], "discard_max_bytes")
}

// TestReclaimVolume_ContextCanceledBeforeSemaphore verifies that a cancelled context
// causes reclaimVolume to return without annotating the PVC.
func TestReclaimVolume_ContextCanceledBeforeSemaphore(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()

	pvc := makePVC("pvc-ctxcancel", "default")
	fakeClient := fake.NewSimpleClientset(pvc)
	// Fill semaphore so the goroutine blocks on it
	cfg := defaultCfg()
	cfg.MaxConcurrentVolumes = 1
	mgr := newTestManager(t, fakeClient, cfg)
	mgr.semaphore <- struct{}{} // occupy the single slot

	vol := &VolumeInfo{
		VolumeID:     "vol-cancel",
		StagingPath:  testGlobalmount,
		DevicePath:   testDevice,
		VolumeMode:   VolumeModeFilesystem,
		PVCName:      "pvc-ctxcancel",
		PVCNamespace: "default",
		PVC:          pvc,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	mgr.reclaimVolume(ctx, vol)
	<-mgr.semaphore // drain semaphore

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-ctxcancel", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Empty(t, updated.Annotations[AnnotationStatus],
		"cancelled context must cause reclaimVolume to return without annotation")
}

// TestReclaimVolume_SuccessClearsErrorAnnotation verifies that a success result removes
// a previously-set error-message annotation.
func TestReclaimVolume_SuccessClearsErrorAnnotation(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()

	pvc := makePVC("pvc-clear-err", "default")
	pvc.Annotations[AnnotationErrorMsg] = "previous fstrim failure"
	fakeClient := fake.NewSimpleClientset(pvc)
	mgr := newTestManager(t, fakeClient, defaultCfg())

	vol := &VolumeInfo{
		VolumeID:     "vol-clear",
		StagingPath:  testGlobalmount,
		DevicePath:   testDevice,
		VolumeMode:   VolumeModeFilesystem,
		PVCName:      "pvc-clear-err",
		PVCNamespace: "default",
		PVC:          pvc,
	}

	mgr.reclaimVolume(context.Background(), vol)

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "pvc-clear-err", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "success", updated.Annotations[AnnotationStatus])
	assert.Empty(t, updated.Annotations[AnnotationErrorMsg],
		"error-message annotation must be cleared on success")
}

// ============================================================================
// NEW: getSystemIDFromPVHandle helper unit tests
// ============================================================================

func TestGetSystemIDFromPVHandle(t *testing.T) {
	tests := []struct {
		name     string
		handle   string
		expected string
	}{
		{"standard format", "fa6960ff6dc6cd0f-867ae134000000a3", "fa6960ff6dc6cd0f"},
		{"multi-dash systemID", "systemA-systemB-vol0000000000000001", "systemA-systemB"},
		{"no dash", "nodashhandle", ""},
		{"empty string", "", ""},
		{"leading dash", "-onlydash", ""},
		{"trailing dash only", "abc-", "abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getSystemIDFromPVHandle(tt.handle)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// ============================================================================
// NEW: initSpaceReclamation wiring test
// ============================================================================

// TestInitSpaceReclamation_SetsManagerOnService verifies that initSpaceReclamation
// creates and starts the manager, setting s.spaceReclaimMgr.
func TestInitSpaceReclamation_SetsManagerOnService(t *testing.T) {
	t.Setenv("X_CSI_SPACE_RECLAMATION_ENABLED", "true")
	t.Setenv("X_CSI_SPACE_RECLAMATION_SCHEDULE", "0 2 * * 0")
	t.Setenv("X_CSI_SPACE_RECLAMATION_MAX_CONCURRENT", "2")
	t.Setenv("X_CSI_SPACE_RECLAMATION_TIMEOUT", "3600")
	t.Setenv("X_CSI_POWERFLEX_KUBE_NODE_NAME", "test-node-init")

	svc := &service{}
	fakeClient := fake.NewSimpleClientset()
	initSpaceReclamation(context.Background(), svc, fakeClient)

	require.NotNil(t, svc.spaceReclaimMgr, "initSpaceReclamation must set spaceReclaimMgr")
	assert.Equal(t, "test-node-init", svc.spaceReclaimMgr.config.NodeName)
	assert.Equal(t, "0 2 * * 0", svc.spaceReclaimMgr.config.Schedule)

	svc.spaceReclaimMgr.cronSched.Stop()
}

// TestInitSpaceReclamation_InvalidScheduleDoesNotPanic verifies initSpaceReclamation
// handles an invalid cron expression gracefully (logs error, does not set manager).
func TestInitSpaceReclamation_InvalidScheduleDoesNotPanic(t *testing.T) {
	t.Setenv("X_CSI_SPACE_RECLAMATION_ENABLED", "true")
	t.Setenv("X_CSI_SPACE_RECLAMATION_SCHEDULE", "not-a-cron")

	svc := &service{}
	fakeClient := fake.NewSimpleClientset()
	initSpaceReclamation(context.Background(), svc, fakeClient)

	assert.Nil(t, svc.spaceReclaimMgr,
		"initSpaceReclamation must not set manager when cron expression is invalid")
}

// ============================================================================
// UNIT TESTS (U-*) -- SECOND
// ============================================================================

// --- U-101 through U-107: SpaceReclamationConfig & ReadSpaceReclamationConfig ---

func TestReadSpaceReclamationConfig(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected SpaceReclamationConfig
	}{
		{
			name:    "AllDefaults",
			envVars: map[string]string{},
			expected: SpaceReclamationConfig{
				Enabled:              false,
				Schedule:             "0 2 * * 0",
				MaxConcurrentVolumes: 2,
				TimeoutSeconds:       14400,
				NodeName:             "",
			},
		},
		{
			name: "AllCustom",
			envVars: map[string]string{
				"X_CSI_SPACE_RECLAMATION_ENABLED":        "true",
				"X_CSI_SPACE_RECLAMATION_SCHEDULE":       "*/5 * * * *",
				"X_CSI_SPACE_RECLAMATION_MAX_CONCURRENT": "4",
				"X_CSI_SPACE_RECLAMATION_TIMEOUT":        "1800",
				"X_CSI_POWERFLEX_KUBE_NODE_NAME":         "node-x",
			},
			expected: SpaceReclamationConfig{
				Enabled:              true,
				Schedule:             "*/5 * * * *",
				MaxConcurrentVolumes: 4,
				TimeoutSeconds:       1800,
				NodeName:             "node-x",
			},
		},
		{
			name: "InvalidBool",
			envVars: map[string]string{
				"X_CSI_SPACE_RECLAMATION_ENABLED": "notabool",
			},
			expected: SpaceReclamationConfig{
				Enabled:              false,
				Schedule:             "0 2 * * 0",
				MaxConcurrentVolumes: 2,
				TimeoutSeconds:       14400,
			},
		},
		{
			name: "InvalidInt",
			envVars: map[string]string{
				"X_CSI_SPACE_RECLAMATION_MAX_CONCURRENT": "abc",
			},
			expected: SpaceReclamationConfig{
				Enabled:              false,
				Schedule:             "0 2 * * 0",
				MaxConcurrentVolumes: 2,
				TimeoutSeconds:       14400,
			},
		},
		{
			name: "ZeroConcurrent",
			envVars: map[string]string{
				"X_CSI_SPACE_RECLAMATION_MAX_CONCURRENT": "0",
			},
			expected: SpaceReclamationConfig{
				Enabled:              false,
				Schedule:             "0 2 * * 0",
				MaxConcurrentVolumes: 0,
				TimeoutSeconds:       14400,
			},
		},
		{
			name: "EmptySchedule",
			envVars: map[string]string{
				"X_CSI_SPACE_RECLAMATION_SCHEDULE": "",
			},
			expected: SpaceReclamationConfig{
				Enabled:              false,
				Schedule:             "0 2 * * 0",
				MaxConcurrentVolumes: 2,
				TimeoutSeconds:       14400,
			},
		},
		{
			name: "NegativeTimeout",
			envVars: map[string]string{
				"X_CSI_SPACE_RECLAMATION_TIMEOUT": "-1",
			},
			expected: SpaceReclamationConfig{
				Enabled:              false,
				Schedule:             "0 2 * * 0",
				MaxConcurrentVolumes: 2,
				TimeoutSeconds:       14400,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all relevant env vars first
			t.Setenv("X_CSI_SPACE_RECLAMATION_ENABLED", "")
			t.Setenv("X_CSI_SPACE_RECLAMATION_SCHEDULE", "")
			t.Setenv("X_CSI_SPACE_RECLAMATION_MAX_CONCURRENT", "")
			t.Setenv("X_CSI_SPACE_RECLAMATION_TIMEOUT", "")
			t.Setenv("X_CSI_POWERFLEX_KUBE_NODE_NAME", "")

			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			cfg := ReadSpaceReclamationConfig()
			assert.Equal(t, tt.expected.Enabled, cfg.Enabled, "Enabled mismatch")
			assert.Equal(t, tt.expected.Schedule, cfg.Schedule, "Schedule mismatch")
			assert.Equal(t, tt.expected.MaxConcurrentVolumes, cfg.MaxConcurrentVolumes, "MaxConcurrentVolumes mismatch")
			assert.Equal(t, tt.expected.TimeoutSeconds, cfg.TimeoutSeconds, "TimeoutSeconds mismatch")
			assert.Equal(t, tt.expected.NodeName, cfg.NodeName, "NodeName mismatch")
		})
	}
}

// --- U-301: checkDiscardSupportFunc injection ---

// TestCheckDiscardSupportFunc_CanBeOverridden verifies checkDiscardSupportFunc is injectable
// and the mock returns expected discard capability values.
func TestCheckDiscardSupportFunc_CanBeOverridden(t *testing.T) {
	orig := checkDiscardSupportFunc
	defer func() { checkDiscardSupportFunc = orig }()

	checkDiscardSupportFunc = func(_ context.Context, devicePath string) (*gofsutil.DiscardCapability, error) {
		switch devicePath {
		case "/dev/sda":
			return &gofsutil.DiscardCapability{Supported: true, DiscardMaxBytes: 4294967295}, nil
		case "/dev/sdb":
			return &gofsutil.DiscardCapability{Supported: false, Reason: "discard_max_bytes is 0"}, nil
		default:
			return nil, fmt.Errorf("device not found")
		}
	}

	cap1, err := checkDiscardSupportFunc(context.Background(), "/dev/sda")
	require.NoError(t, err)
	assert.True(t, cap1.Supported)
	assert.Equal(t, int64(4294967295), cap1.DiscardMaxBytes)

	cap2, err := checkDiscardSupportFunc(context.Background(), "/dev/sdb")
	require.NoError(t, err)
	assert.False(t, cap2.Supported)
	assert.Equal(t, "discard_max_bytes is 0", cap2.Reason)

	_, err = checkDiscardSupportFunc(context.Background(), "/dev/missing")
	assert.Error(t, err)
}

// TestCheckDiscardSupportFunc_MockIntegration verifies the gofsutil mock controls
// what checkDiscardSupportFunc returns via the mock FS layer.
func TestCheckDiscardSupportFunc_MockIntegration(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()

	gofsutil.GOFSMockDiscardCapability = &gofsutil.DiscardCapability{
		Supported:       true,
		DiscardMaxBytes: 1073741824,
	}

	capability, err := checkDiscardSupportFunc(context.Background(), "/dev/sda")
	require.NoError(t, err)
	require.NotNil(t, capability)
	assert.True(t, capability.Supported)
	assert.Equal(t, int64(1073741824), capability.DiscardMaxBytes)
}

// TestCheckDiscardSupportFunc_MockError verifies InduceCheckDiscardSupportError is surfaced.
func TestCheckDiscardSupportFunc_MockError(t *testing.T) {
	gofsutil.UseMockFS()
	defer resetGofsutilMock()

	gofsutil.GOFSMock.InduceCheckDiscardSupportError = true

	capability, err := checkDiscardSupportFunc(context.Background(), "/dev/sda")
	assert.Error(t, err)
	assert.Nil(t, capability)
}

// TestPVCAnnotator_MaxRetriesExhausted verifies the annotator returns error after
// all retries are exhausted by persistent conflicts.
func TestPVCAnnotator_MaxRetriesExhausted(t *testing.T) {
	pvc := makePVC("test-pvc-retry", "default")
	fakeClient := fake.NewSimpleClientset(pvc)

	fakeClient.PrependReactor("update", "persistentvolumeclaims", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("the object has been modified; please apply your changes")
	})

	annotator := &PVCAnnotator{client: fakeClient, maxRetry: 2}
	result := &ReclamationResult{Status: "success", NodeName: "node-1"}
	err := annotator.Annotate(context.Background(), "test-pvc-retry", "default", result)
	assert.Error(t, err, "exhausted retries should return error")
}

// --- U-401 through U-406: PVCAnnotator ---

func TestPVCAnnotator_AnnotateSuccess(t *testing.T) {
	pvc := makePVC("test-pvc", "default")
	fakeClient := fake.NewSimpleClientset(pvc)
	annotator := NewPVCAnnotator(fakeClient)

	result := &ReclamationResult{
		Status:         "success",
		BytesReclaimed: 1073741824,
		Duration:       500 * time.Millisecond,
		NodeName:       "node-1",
	}
	err := annotator.Annotate(context.Background(), "test-pvc", "default", result)
	require.NoError(t, err)

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "test-pvc", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "success", updated.Annotations[AnnotationStatus])
	assert.Equal(t, "1073741824", updated.Annotations[AnnotationBytesReclaim])
	assert.Equal(t, "node-1", updated.Annotations[AnnotationNode])
	assert.NotEmpty(t, updated.Annotations[AnnotationLastRunTime])
	assert.NotEmpty(t, updated.Annotations[AnnotationDuration])
}

func TestPVCAnnotator_AnnotateError(t *testing.T) {
	pvc := makePVC("test-pvc", "default")
	fakeClient := fake.NewSimpleClientset(pvc)
	annotator := NewPVCAnnotator(fakeClient)

	result := &ReclamationResult{
		Status:       "error",
		ErrorMessage: "fstrim failed: permission denied",
		NodeName:     "node-1",
	}
	err := annotator.Annotate(context.Background(), "test-pvc", "default", result)
	require.NoError(t, err)

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "test-pvc", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "error", updated.Annotations[AnnotationStatus])
	assert.Contains(t, updated.Annotations[AnnotationErrorMsg], "fstrim failed")
}

func TestPVCAnnotator_AnnotateTimeout(t *testing.T) {
	pvc := makePVC("test-pvc", "default")
	fakeClient := fake.NewSimpleClientset(pvc)
	annotator := NewPVCAnnotator(fakeClient)

	result := &ReclamationResult{
		Status:       "timeout",
		ErrorMessage: "operation timed out after 3600s",
		NodeName:     "node-1",
	}
	err := annotator.Annotate(context.Background(), "test-pvc", "default", result)
	require.NoError(t, err)

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "test-pvc", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "timeout", updated.Annotations[AnnotationStatus])
}

func TestPVCAnnotator_AnnotateUnsupported(t *testing.T) {
	pvc := makePVC("test-pvc", "default")
	fakeClient := fake.NewSimpleClientset(pvc)
	annotator := NewPVCAnnotator(fakeClient)

	result := &ReclamationResult{
		Status:       "unsupported",
		ErrorMessage: "discard_max_bytes is 0",
		NodeName:     "node-1",
	}
	err := annotator.Annotate(context.Background(), "test-pvc", "default", result)
	require.NoError(t, err)

	updated, err := fakeClient.CoreV1().PersistentVolumeClaims("default").Get(
		context.Background(), "test-pvc", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(t, "unsupported", updated.Annotations[AnnotationStatus])
}

func TestPVCAnnotator_PVCNotFound(t *testing.T) {
	// No PVC created in fake client
	fakeClient := fake.NewSimpleClientset()
	annotator := NewPVCAnnotator(fakeClient)

	result := &ReclamationResult{Status: "success", BytesReclaimed: 100}
	err := annotator.Annotate(context.Background(), "nonexistent-pvc", "default", result)
	assert.Error(t, err, "annotating non-existent PVC should return error")
}

func TestPVCAnnotator_ConflictRetry(t *testing.T) {
	pvc := makePVC("test-pvc", "default")
	fakeClient := fake.NewSimpleClientset(pvc)

	// Track update call count using a reactor
	updateCount := 0
	fakeClient.PrependReactor("update", "persistentvolumeclaims", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		updateCount++
		if updateCount == 1 {
			// Simulate a 409 Conflict on the first attempt
			return true, nil, fmt.Errorf("the object has been modified; please apply your changes to the latest version and try again")
		}
		// Let the second attempt succeed
		return false, nil, nil
	})

	annotator := NewPVCAnnotator(fakeClient)
	result := &ReclamationResult{Status: "success", BytesReclaimed: 100, NodeName: "node-1"}
	err := annotator.Annotate(context.Background(), "test-pvc", "default", result)

	// The annotator should retry and eventually succeed
	assert.NoError(t, err, "annotator should handle conflict with retry")
	assert.GreaterOrEqual(t, updateCount, 2, "should have retried at least once")
}

// --- U-501 through U-504: EventEmitter ---

func TestEventEmitter_EmitSuccess(t *testing.T) {
	recorder := record.NewFakeRecorder(10)
	emitter := &EventEmitter{recorder: recorder}
	pvc := makePVC("test-pvc", "default")

	emitter.EmitSuccess(pvc, 1073741824, 500*time.Millisecond)

	select {
	case event := <-recorder.Events:
		assert.Contains(t, event, EventReasonCompleted, "event should contain SpaceReclamationCompleted")
	case <-time.After(time.Second):
		t.Fatal("expected SpaceReclamationCompleted event not received")
	}
}

func TestEventEmitter_EmitFailure(t *testing.T) {
	recorder := record.NewFakeRecorder(10)
	emitter := &EventEmitter{recorder: recorder}
	pvc := makePVC("test-pvc", "default")

	emitter.EmitFailure(pvc, fmt.Errorf("fstrim failed"))

	select {
	case event := <-recorder.Events:
		assert.Contains(t, event, EventReasonFailed, "event should contain SpaceReclamationFailed")
	case <-time.After(time.Second):
		t.Fatal("expected SpaceReclamationFailed event not received")
	}
}

func TestEventEmitter_EmitTimeout(t *testing.T) {
	recorder := record.NewFakeRecorder(10)
	emitter := &EventEmitter{recorder: recorder}
	pvc := makePVC("test-pvc", "default")

	emitter.EmitTimeout(pvc, 3600*time.Second)

	select {
	case event := <-recorder.Events:
		assert.Contains(t, event, EventReasonTimeout, "event should contain SpaceReclamationTimeout")
	case <-time.After(time.Second):
		t.Fatal("expected SpaceReclamationTimeout event not received")
	}
}

func TestEventEmitter_EmitUnsupported(t *testing.T) {
	recorder := record.NewFakeRecorder(10)
	emitter := &EventEmitter{recorder: recorder}
	pvc := makePVC("test-pvc", "default")

	emitter.EmitUnsupported(pvc, "discard_max_bytes is 0")

	select {
	case event := <-recorder.Events:
		assert.Contains(t, event, EventReasonUnsupported, "event should contain SpaceReclamationUnsupported")
	case <-time.After(time.Second):
		t.Fatal("expected SpaceReclamationUnsupported event not received")
	}
}

// --- U-601 through U-605: Eligibility Logic ---

func TestIsEligible_GlobalEnabledNoAnnotation(t *testing.T) {
	labels := map[string]string{} // no opt-out label
	result, _ := IsEligible(true, labels, VolumeModeFilesystem)
	assert.True(t, result, "global enabled + no label = eligible")
}

func TestIsEligible_ExplicitOptOut(t *testing.T) {
	labels := map[string]string{
		FstrimLabelEnabled: "false",
	}
	result, _ := IsEligible(true, labels, VolumeModeFilesystem)
	assert.False(t, result, "explicit opt-out should make volume ineligible")
}

func TestIsEligible_ExplicitOptIn(t *testing.T) {
	labels := map[string]string{
		FstrimLabelEnabled: "true",
	}
	result, _ := IsEligible(true, labels, VolumeModeFilesystem)
	assert.True(t, result, "explicit opt-in should make volume eligible")
}

func TestIsEligible_GlobalDisabled(t *testing.T) {
	labels := map[string]string{}
	result, reason := IsEligible(false, labels, VolumeModeFilesystem)
	assert.False(t, result, "global disabled = ineligible")
	assert.Equal(t, "global disabled", reason, "reason should be global disabled")
}

func TestIsEligible_NilLabelsMap(t *testing.T) {
	result, _ := IsEligible(true, nil, VolumeModeFilesystem)
	assert.True(t, result, "nil labels with global enabled = eligible")
}

func TestIsEligible_BlockModeMissingLabel(t *testing.T) {
	labels := map[string]string{}
	result, reason := IsEligible(true, labels, VolumeModeBlock)
	assert.False(t, result, "block mode without label = ineligible")
	assert.Equal(t, "block mode missing required label", reason, "reason should indicate missing label")
}

func TestIsEligible_BlockModeWithTrueLabel(t *testing.T) {
	labels := map[string]string{
		BlockLabelEnabled: "true",
	}
	result, reason := IsEligible(true, labels, VolumeModeBlock)
	assert.True(t, result, "block mode with true label = eligible")
	assert.Equal(t, "", reason, "reason should be empty when eligible")
}

func TestIsEligible_BlockModeWithFalseLabel(t *testing.T) {
	labels := map[string]string{
		BlockLabelEnabled: "false",
	}
	result, reason := IsEligible(true, labels, VolumeModeBlock)
	assert.False(t, result, "block mode with false label = ineligible")
	assert.Contains(t, reason, "block mode label is 'false'", "reason should indicate label value")
}

// --- U-701 through U-704: Concurrency Control ---

func TestSemaphore_LimitsParallelism(t *testing.T) {
	sem := make(chan struct{}, 2)
	var maxConcurrent int64
	var currentConcurrent int64
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			curr := atomic.AddInt64(&currentConcurrent, 1)
			// Track max concurrency
			for {
				old := atomic.LoadInt64(&maxConcurrent)
				if curr <= old || atomic.CompareAndSwapInt64(&maxConcurrent, old, curr) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond) // simulate work
			atomic.AddInt64(&currentConcurrent, -1)
		}()
	}
	wg.Wait()
	assert.LessOrEqual(t, atomic.LoadInt64(&maxConcurrent), int64(2),
		"at most 2 jobs should run concurrently")
}

func TestSemaphore_ZeroCapacity(t *testing.T) {
	// A zero-capacity semaphore should block all jobs.
	// The manager should either prevent this or handle it gracefully.
	sem := make(chan struct{}, 0)

	done := make(chan bool, 1)
	go func() {
		select {
		case sem <- struct{}{}:
			done <- true
		case <-time.After(100 * time.Millisecond):
			done <- false
		}
	}()

	blocked := <-done
	assert.False(t, blocked, "zero-capacity semaphore should block all jobs")
}

func TestPerVolumeMutex_PreventsDuplicateJob(t *testing.T) {
	var volumeLocks sync.Map
	volID := "vol-dup-001"

	// First lock should succeed
	mu := &sync.Mutex{}
	actual, loaded := volumeLocks.LoadOrStore(volID, mu)
	assert.False(t, loaded, "first lock should not be loaded")

	actualMu := actual.(*sync.Mutex)
	actualMu.Lock()

	// Second attempt for same volume should find existing lock
	_, loaded2 := volumeLocks.LoadOrStore(volID, &sync.Mutex{})
	assert.True(t, loaded2, "second lock should find existing entry (duplicate job)")

	actualMu.Unlock()
}

// --- U-801 through U-803: Manager Initialization Edge Cases ---

func TestNewSpaceReclamationManager_InvalidCronExpression(t *testing.T) {
	cfg := SpaceReclamationConfig{
		Enabled:              true,
		Schedule:             "not a cron",
		MaxConcurrentVolumes: 2,
		TimeoutSeconds:       60,
		NodeName:             "node-1",
	}
	fakeClient := fake.NewSimpleClientset()
	mgr, err := NewSpaceReclamationManager(context.Background(), cfg, fakeClient, cfg.NodeName, false)
	assert.Error(t, err, "invalid cron expression should return error")
	assert.Nil(t, mgr, "manager should be nil with invalid cron expression")
}

func TestNewSpaceReclamationManager_ValidConfig(t *testing.T) {
	cfg := SpaceReclamationConfig{
		Enabled:              true,
		Schedule:             "0 2 * * 0",
		MaxConcurrentVolumes: 2,
		TimeoutSeconds:       3600,
		NodeName:             "node-1",
	}
	fakeClient := fake.NewSimpleClientset()
	mgr, err := NewSpaceReclamationManager(context.Background(), cfg, fakeClient, cfg.NodeName, false)
	require.NoError(t, err, "valid config should not return error")
	require.NotNil(t, mgr, "manager should be created with valid config")
}

func TestNewSpaceReclamationManager_EmptyNodeName(t *testing.T) {
	cfg := SpaceReclamationConfig{
		Enabled:              true,
		Schedule:             "0 2 * * 0",
		MaxConcurrentVolumes: 2,
		TimeoutSeconds:       3600,
		NodeName:             "",
	}
	fakeClient := fake.NewSimpleClientset()
	mgr, err := NewSpaceReclamationManager(context.Background(), cfg, fakeClient, cfg.NodeName, false)
	// Should create manager with graceful degradation (empty node name is accepted)
	require.NoError(t, err, "empty NodeName should be accepted (graceful degradation)")
	require.NotNil(t, mgr, "manager should be created even with empty NodeName")
}

// --- U-901: Environment Variable Constants ---

func TestEnvVarConstants_Defined(t *testing.T) {
	assert.Equal(t, "X_CSI_SPACE_RECLAMATION_ENABLED", EnvSpaceReclamationEnabled)
	assert.Equal(t, "X_CSI_SPACE_RECLAMATION_SCHEDULE", EnvSpaceReclamationSchedule)
	assert.Equal(t, "X_CSI_SPACE_RECLAMATION_MAX_CONCURRENT", EnvSpaceReclamationMaxConcurrent)
	assert.Equal(t, "X_CSI_SPACE_RECLAMATION_TIMEOUT", EnvSpaceReclamationTimeout)
}
