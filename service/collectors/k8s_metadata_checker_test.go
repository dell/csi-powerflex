// Copyright © 2024 Dell Inc. or its subsidiaries. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package collectors

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestK8sMetadataChecker_RefreshCacheAndValidation(t *testing.T) {
	pvManaged := corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-managed"},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver:           "csi-vxflexos.dellemc.com",
					VolumeHandle:     "system-vol-123",
					VolumeAttributes: map[string]string{"Protocol": "iSCSI"},
				},
			},
		},
	}
	pvOther := corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-other"},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver:       "other-driver",
					VolumeHandle: "system-vol-999",
				},
			},
		},
	}
	va := storagev1.VolumeAttachment{
		ObjectMeta: metav1.ObjectMeta{Name: "va-1"},
		Spec: storagev1.VolumeAttachmentSpec{
			Attacher: "csi-vxflexos.dellemc.com",
			Source: storagev1.VolumeAttachmentSource{
				PersistentVolumeName: &pvManaged.Name,
			},
		},
		Status: storagev1.VolumeAttachmentStatus{Attached: true},
	}

	client := fake.NewSimpleClientset(&pvManaged, &pvOther, &va)
	checker := NewK8sMetadataChecker(client, "csi-vxflexos.dellemc.com")

	require.True(t, checker.Available())
	require.NoError(t, checker.RefreshCache(context.Background()))

	managed, err := checker.IsDriverManaged(context.Background(), "system-vol-123")
	require.NoError(t, err)
	assert.True(t, managed)

	managed, err = checker.IsDriverManaged(context.Background(), "system-vol-999")
	require.NoError(t, err)
	assert.False(t, managed)

	managedByName, err := checker.IsDriverManagedByName(context.Background(), "pv-managed")
	require.NoError(t, err)
	assert.True(t, managedByName)

	managedByName, err = checker.IsDriverManagedByName(context.Background(), "pv-other")
	require.NoError(t, err)
	assert.False(t, managedByName)

	protocol := checker.GetProtocol(context.Background(), "system-vol-123")
	assert.Equal(t, "iSCSI", protocol)

	attached, err := checker.GetAttachmentStateForVolumeID(context.Background(), "system-vol-123", "system")
	require.NoError(t, err)
	assert.True(t, attached)

	attached, err = checker.GetAttachmentStateForPVName(context.Background(), "pv-managed")
	require.NoError(t, err)
	assert.True(t, attached)
}

func TestK8sMetadataChecker_NilClient(t *testing.T) {
	checker := NewK8sMetadataChecker(nil, "csi-vxflexos.dellemc.com")
	assert.False(t, checker.Available())

	_, err := checker.IsDriverManaged(context.Background(), "vol-1")
	assert.Error(t, err)

	assert.Equal(t, "unknown", checker.GetProtocol(context.Background(), "vol-1"))
}

func TestK8sMetadataChecker_DeleteCompletion(t *testing.T) {
	client := fake.NewSimpleClientset()
	checker := NewK8sMetadataChecker(client, "csi-vxflexos.dellemc.com")

	checker.volumeIDCache["vol-1"] = true
	checker.volumeNameCache["pv-1"] = true
	checker.protocolCache["vol-1"] = "NFS"
	checker.attachmentCache["pv-1"] = true
	checker.pvNameCache["vol-1"] = "pv-1"
	checker.arrayIDCache["vol-1"] = "system"

	checker.MarkDeleteComplete("system-vol-1")
	checker.MarkDeleteCompleteByName("pv-1")

	assert.NotContains(t, checker.volumeIDCache, "vol-1")
	assert.NotContains(t, checker.volumeNameCache, "pv-1")
	assert.NotContains(t, checker.protocolCache, "vol-1")
	assert.NotContains(t, checker.attachmentCache, "pv-1")
	assert.NotContains(t, checker.pvNameCache, "vol-1")
	assert.NotContains(t, checker.arrayIDCache, "vol-1")
}

func TestK8sMetadataChecker_GetAttachmentStateForPVName_NotFound(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	checker := NewK8sMetadataChecker(clientset, "csi-vxflexos")

	// Test getting attachment state for a PV that doesn't exist
	attached, err := checker.GetAttachmentStateForPVName(context.Background(), "nonexistent-pv")
	require.NoError(t, err)
	assert.False(t, attached)
}

func TestK8sMetadataChecker_IsDriverManagedByName_NotFound(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	checker := NewK8sMetadataChecker(clientset, "csi-vxflexos")

	// test checking if a PV is managed when it doesn't exist
	managed, err := checker.IsDriverManagedByName(context.Background(), "nonexistent-pv")
	require.NoError(t, err)
	assert.False(t, managed)
}

func TestK8sMetadataChecker_GetProtocol_NotFound(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	checker := NewK8sMetadataChecker(clientset, "csi-vxflexos")

	// Test getting protocol for a volume that doesn't exist
	protocol := checker.GetProtocol(context.Background(), "nonexistent-vol")
	assert.Equal(t, "unknown", protocol)
}

func TestK8sMetadataChecker_GetAttachmentStateForVolumeID_NotFound(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	checker := NewK8sMetadataChecker(clientset, "csi-vxflexos")

	// Test getting attachment state for a volume that doesn't exist
	attached, err := checker.GetAttachmentStateForVolumeID(context.Background(), "nonexistent-vol", "system-1")
	require.NoError(t, err)
	assert.False(t, attached)
}

func TestK8sMetadataChecker_ExtendedHelperBranches(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	checker := NewK8sMetadataChecker(clientset, "csi-vxflexos")

	assert.Equal(t, "", extractVolumeIDFromHandle(""))
	assert.Equal(t, "vol-123", extractVolumeIDFromHandle("system-vol-123"))
	assert.Equal(t, "system", extractArrayIDFromHandle("system-vol-123"))
	assert.Equal(t, "system", extractArrayIDFromHandle("system/vol-123"))

	for _, tc := range []struct {
		input string
		want  string
	}{
		{input: "iscsi", want: "iSCSI"},
		{input: "scsi", want: "iSCSI"},
		{input: "fc", want: "FC"},
		{input: "nvme-tcp", want: "NVMeTCP"},
		{input: "nvme_tcp", want: "NVMeTCP"},
		{input: "nfs", want: "NFS"},
		{input: "unknown-protocol", want: "unknown"},
	} {
		assert.Equal(t, tc.want, normalizeProtocol(tc.input))
	}

	assert.True(t, checker.Available())
	assert.Equal(t, "unknown", checker.GetProtocol(context.Background(), "vol-123"))

	attached, err := checker.GetAttachmentStateForPVName(context.Background(), "")
	require.NoError(t, err)
	assert.False(t, attached)

	managed, err := checker.IsDriverManagedByName(context.Background(), "")
	require.NoError(t, err)
	assert.False(t, managed)

	managed, err = checker.IsDriverManaged(context.Background(), "")
	require.NoError(t, err)
	assert.False(t, managed)

	checker.MarkDeleteComplete("")
	checker.MarkDeleteCompleteByName("")

	checker.pvNameCache["vol-123"] = "pv-1"
	checker.arrayIDCache["vol-123"] = "array-1"
	checker.attachmentCache["pv-1"] = true
	attached, err = checker.GetAttachmentStateForVolumeID(context.Background(), "system-vol-123", "array-2")
	require.NoError(t, err)
	assert.False(t, attached, "mismatched global IDs should not report attachment state")
}
