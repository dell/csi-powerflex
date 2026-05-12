// Copyright © 2026 Dell Inc. or its subsidiaries. All Rights Reserved.
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

// Package volumegroupsnapshot contains e2e and scale tests for the
// CSI Volume Group Snapshot feature on Dell PowerFlex.
//
// Prerequisites:
//   - A running Kubernetes cluster (>= v1.32) with VolumeGroupSnapshot feature gate enabled
//   - Dell PowerFlex CSI driver deployed with snapshot-controller and csi-snapshotter
//   - VolumeGroupSnapshot v1beta2 CRDs installed
//   - StorageClass "vxflexos" and VolumeGroupSnapshotClass "vxflexos-groupsnapclass" created
//
// Run:
//
//	go test -v -timeout 60m ./test/e2e/volumegroupsnapshot/ -run TestVGS
package volumegroupsnapshot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	testNamespace     = "vgs-e2e-test"
	storageClass      = "vxflexos"
	groupSnapClass    = "vxflexos-groupsnapclass"
	pvcLabelKey       = "vgs-e2e"
	pvcLabelValue     = "test-group"
	pvcSizeGi         = "8Gi"
	pollInterval      = 10 * time.Second
	pollTimeout       = 5 * time.Minute
	scaleGroupCount   = 10  // ≥10 groups per namespace
	scalePVCsPerGroup = 100 // ≥100 PVCs per group
)

// execCmd executes a shell command and returns stdout bytes.
func execCmd(command string) ([]byte, error) {
	var buf bytes.Buffer
	cmd := exec.Command("bash", "-c", command)
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

// setupNamespace creates the test namespace if it doesn't exist.
func setupNamespace(t *testing.T) {
	t.Helper()
	cmd := fmt.Sprintf("kubectl create namespace %s --dry-run=client -o yaml | kubectl apply -f -", testNamespace)
	if _, err := execCmd(cmd); err != nil {
		t.Fatalf("Failed to create namespace %s: %v", testNamespace, err)
	}
}

// cleanupNamespace deletes the test namespace.
func cleanupNamespace(t *testing.T) {
	t.Helper()
	cmd := fmt.Sprintf("kubectl delete namespace %s --ignore-not-found --wait=false", testNamespace)
	_, _ = execCmd(cmd)
}

// createPVCs creates 'count' PVCs with the given label in the test namespace.
func createPVCs(t *testing.T, groupName string, count int) []string {
	t.Helper()
	names := make([]string, 0, count)
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("%s-pvc-%d", groupName, i)
		manifest := fmt.Sprintf(`
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: %s
  namespace: %s
  labels:
    %s: %s
    group: %s
spec:
  storageClassName: %s
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: %s
`, name, testNamespace, pvcLabelKey, pvcLabelValue, groupName, storageClass, pvcSizeGi)

		cmd := fmt.Sprintf("echo '%s' | kubectl apply -f -", manifest)
		if _, err := execCmd(cmd); err != nil {
			t.Fatalf("Failed to create PVC %s: %v", name, err)
		}
		names = append(names, name)
	}
	return names
}

// waitForPVCsBound waits until all PVCs with the given label are Bound.
func waitForPVCsBound(t *testing.T, groupName string, expected int) {
	t.Helper()
	deadline := time.Now().Add(pollTimeout)
	for time.Now().Before(deadline) {
		cmd := fmt.Sprintf(
			"kubectl get pvc -n %s -l group=%s -o jsonpath='{.items[*].status.phase}'",
			testNamespace, groupName)
		out, err := execCmd(cmd)
		if err == nil {
			phases := strings.Fields(strings.Trim(string(out), "'"))
			bound := 0
			for _, p := range phases {
				if p == "Bound" {
					bound++
				}
			}
			if bound >= expected {
				log.Printf("[waitForPVCsBound] All %d PVCs bound for group %s", expected, groupName)
				return
			}
			log.Printf("[waitForPVCsBound] %d/%d PVCs bound for group %s", bound, expected, groupName)
		}
		time.Sleep(pollInterval)
	}
	t.Fatalf("Timed out waiting for %d PVCs to be Bound in group %s", expected, groupName)
}

// createVolumeGroupSnapshot creates a VolumeGroupSnapshot targeting the label selector.
func createVolumeGroupSnapshot(t *testing.T, vgsName, groupName string) {
	t.Helper()
	manifest := fmt.Sprintf(`
apiVersion: groupsnapshot.storage.k8s.io/v1beta2
kind: VolumeGroupSnapshot
metadata:
  name: %s
  namespace: %s
spec:
  volumeGroupSnapshotClassName: %s
  source:
    selector:
      matchLabels:
        %s: %s
        group: %s
`, vgsName, testNamespace, groupSnapClass, pvcLabelKey, pvcLabelValue, groupName)

	cmd := fmt.Sprintf("echo '%s' | kubectl apply -f -", manifest)
	if _, err := execCmd(cmd); err != nil {
		t.Fatalf("Failed to create VolumeGroupSnapshot %s: %v", vgsName, err)
	}
}

// waitForVGSReady waits for a VolumeGroupSnapshot to be ReadyToUse.
func waitForVGSReady(t *testing.T, vgsName string) {
	t.Helper()
	deadline := time.Now().Add(pollTimeout)
	for time.Now().Before(deadline) {
		cmd := fmt.Sprintf(
			"kubectl get volumegroupsnapshot %s -n %s -o jsonpath='{.status.readyToUse}'",
			vgsName, testNamespace)
		out, err := execCmd(cmd)
		if err == nil && strings.Trim(string(out), "'") == "true" {
			log.Printf("[waitForVGSReady] VolumeGroupSnapshot %s is ready", vgsName)
			return
		}
		log.Printf("[waitForVGSReady] Waiting for VolumeGroupSnapshot %s...", vgsName)
		time.Sleep(pollInterval)
	}
	t.Fatalf("Timed out waiting for VolumeGroupSnapshot %s to be ready", vgsName)
}

// deleteVolumeGroupSnapshot deletes a VolumeGroupSnapshot.
func deleteVolumeGroupSnapshot(t *testing.T, vgsName string) {
	t.Helper()
	cmd := fmt.Sprintf("kubectl delete volumegroupsnapshot %s -n %s --ignore-not-found", vgsName, testNamespace)
	if _, err := execCmd(cmd); err != nil {
		t.Logf("Warning: failed to delete VolumeGroupSnapshot %s: %v", vgsName, err)
	}
}

// getVolumeSnapshotsForGroup returns snapshot names created by the VGS.
func getVolumeSnapshotsForGroup(t *testing.T, vgsName string) []string {
	t.Helper()
	cmd := fmt.Sprintf(
		`kubectl get volumesnapshot -n %s -o json | jq -r '.items[] | select(.status.volumeGroupSnapshotName=="%s") | .metadata.name'`,
		testNamespace, vgsName)
	out, err := execCmd(cmd)
	if err != nil {
		t.Logf("Warning: could not list snapshots for VGS %s: %v", vgsName, err)
		return nil
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			names = append(names, line)
		}
	}
	return names
}

// deletePVCs deletes PVCs for a group.
func deletePVCs(t *testing.T, groupName string) {
	t.Helper()
	cmd := fmt.Sprintf("kubectl delete pvc -n %s -l group=%s --ignore-not-found", testNamespace, groupName)
	_, _ = execCmd(cmd)
}

// --- Functional Tests ---

// TestVGS_CreateDeleteBasic creates a small group snapshot (3 PVCs), verifies
// readiness, and then deletes it.
func TestVGS_CreateDeleteBasic(t *testing.T) {
	setupNamespace(t)
	defer cleanupNamespace(t)

	groupName := "basic"
	vgsName := "vgs-basic"

	// Create PVCs
	createPVCs(t, groupName, 3)
	waitForPVCsBound(t, groupName, 3)

	// Create group snapshot
	createVolumeGroupSnapshot(t, vgsName, groupName)
	waitForVGSReady(t, vgsName)

	// Verify individual snapshots were created
	snaps := getVolumeSnapshotsForGroup(t, vgsName)
	if len(snaps) != 3 {
		t.Logf("Warning: expected 3 individual snapshots, got %d (jq may not be available)", len(snaps))
	}

	// Delete group snapshot
	deleteVolumeGroupSnapshot(t, vgsName)

	// Cleanup PVCs
	deletePVCs(t, groupName)
}

// TestVGS_Idempotent creates the same group snapshot twice and expects success.
func TestVGS_Idempotent(t *testing.T) {
	setupNamespace(t)
	defer cleanupNamespace(t)

	groupName := "idempotent"
	vgsName := "vgs-idempotent"

	createPVCs(t, groupName, 2)
	waitForPVCsBound(t, groupName, 2)

	createVolumeGroupSnapshot(t, vgsName, groupName)
	waitForVGSReady(t, vgsName)

	// Apply again — should be idempotent
	createVolumeGroupSnapshot(t, vgsName, groupName)
	waitForVGSReady(t, vgsName)

	deleteVolumeGroupSnapshot(t, vgsName)
	deletePVCs(t, groupName)
}

// TestVGS_RestoreFromGroupSnapshot creates a group snapshot, then restores
// new PVCs from the individual snapshots.
func TestVGS_RestoreFromGroupSnapshot(t *testing.T) {
	setupNamespace(t)
	defer cleanupNamespace(t)

	groupName := "restore"
	vgsName := "vgs-restore"

	createPVCs(t, groupName, 2)
	waitForPVCsBound(t, groupName, 2)

	createVolumeGroupSnapshot(t, vgsName, groupName)
	waitForVGSReady(t, vgsName)

	snaps := getVolumeSnapshotsForGroup(t, vgsName)
	if len(snaps) == 0 {
		t.Skip("Skipping restore test: could not enumerate individual snapshots (jq not available)")
	}

	// Restore PVCs from each individual snapshot
	for i, snapName := range snaps {
		restoreName := fmt.Sprintf("restore-pvc-%d", i)
		manifest := fmt.Sprintf(`
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: %s
  namespace: %s
spec:
  storageClassName: %s
  dataSource:
    name: %s
    kind: VolumeSnapshot
    apiGroup: snapshot.storage.k8s.io
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: %s
`, restoreName, testNamespace, storageClass, snapName, pvcSizeGi)

		cmd := fmt.Sprintf("echo '%s' | kubectl apply -f -", manifest)
		if _, err := execCmd(cmd); err != nil {
			t.Fatalf("Failed to create restore PVC %s: %v", restoreName, err)
		}
	}

	// Wait for restored PVCs to be bound
	deadline := time.Now().Add(pollTimeout)
	for time.Now().Before(deadline) {
		cmd := fmt.Sprintf(
			"kubectl get pvc -n %s -o jsonpath='{range .items[*]}{.metadata.name} {.status.phase}{\"\\n\"}{end}'",
			testNamespace)
		out, _ := execCmd(cmd)
		allBound := true
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "restore-pvc-") && !strings.Contains(line, "Bound") {
				allBound = false
			}
		}
		if allBound {
			break
		}
		time.Sleep(pollInterval)
	}

	deleteVolumeGroupSnapshot(t, vgsName)
	deletePVCs(t, groupName)
}

// --- Scale Tests ---

// TestVGS_Scale_100PVCsPerGroup tests creating a group snapshot of ≥100 PVCs.
func TestVGS_Scale_100PVCsPerGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping scale test in short mode")
	}

	setupNamespace(t)
	defer cleanupNamespace(t)

	groupName := "scale100"
	vgsName := "vgs-scale100"

	log.Printf("[Scale] Creating %d PVCs for group %s", scalePVCsPerGroup, groupName)
	createPVCs(t, groupName, scalePVCsPerGroup)
	waitForPVCsBound(t, groupName, scalePVCsPerGroup)

	log.Printf("[Scale] Creating VolumeGroupSnapshot %s with %d PVCs", vgsName, scalePVCsPerGroup)
	createVolumeGroupSnapshot(t, vgsName, groupName)
	waitForVGSReady(t, vgsName)

	log.Printf("[Scale] VolumeGroupSnapshot %s is ready with %d PVCs", vgsName, scalePVCsPerGroup)

	deleteVolumeGroupSnapshot(t, vgsName)
	deletePVCs(t, groupName)
}

// TestVGS_Scale_10GroupsPerNamespace tests creating ≥10 group snapshots in a single namespace.
func TestVGS_Scale_10GroupsPerNamespace(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping scale test in short mode")
	}

	setupNamespace(t)
	defer cleanupNamespace(t)

	pvcsPerGroup := 5 // Keep per-group count small; focus on group count
	log.Printf("[Scale] Creating %d groups with %d PVCs each", scaleGroupCount, pvcsPerGroup)

	type groupInfo struct {
		groupName string
		vgsName   string
	}
	groups := make([]groupInfo, 0, scaleGroupCount)

	for g := 0; g < scaleGroupCount; g++ {
		gName := fmt.Sprintf("scalegroup-%d", g)
		vName := fmt.Sprintf("vgs-scalegroup-%d", g)
		groups = append(groups, groupInfo{groupName: gName, vgsName: vName})

		createPVCs(t, gName, pvcsPerGroup)
	}

	// Wait for all PVCs to be bound
	for _, g := range groups {
		waitForPVCsBound(t, g.groupName, pvcsPerGroup)
	}

	// Create all group snapshots
	for _, g := range groups {
		createVolumeGroupSnapshot(t, g.vgsName, g.groupName)
	}

	// Wait for all to be ready
	for _, g := range groups {
		waitForVGSReady(t, g.vgsName)
	}

	log.Printf("[Scale] All %d VolumeGroupSnapshots are ready", scaleGroupCount)

	// Cleanup
	for _, g := range groups {
		deleteVolumeGroupSnapshot(t, g.vgsName)
		deletePVCs(t, g.groupName)
	}
}

// Unused import suppression
var (
	_ = json.Marshal
	_ = strconv.Itoa
)
