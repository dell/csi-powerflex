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
	"fmt"
	"strings"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// K8sMetadataChecker validates driver-managed volumes using Kubernetes PV and
// VolumeAttachment metadata.
type K8sMetadataChecker struct {
	k8sClient       kubernetes.Interface
	driverName      string
	volumeIDCache   map[string]bool
	volumeNameCache map[string]bool
	protocolCache   map[string]string
	pvNameCache     map[string]string
	arrayIDCache    map[string]string
	attachmentCache map[string]bool
	cacheMu         sync.RWMutex
}

// NewK8sMetadataChecker creates a Kubernetes-backed volume metadata checker.
func NewK8sMetadataChecker(k8sClient kubernetes.Interface, driverName string) *K8sMetadataChecker {
	return &K8sMetadataChecker{
		k8sClient:       k8sClient,
		driverName:      driverName,
		volumeIDCache:   make(map[string]bool),
		volumeNameCache: make(map[string]bool),
		protocolCache:   make(map[string]string),
		pvNameCache:     make(map[string]string),
		arrayIDCache:    make(map[string]string),
		attachmentCache: make(map[string]bool),
	}
}

// Available reports whether the checker has a Kubernetes client.
func (c *K8sMetadataChecker) Available() bool {
	return c != nil && c.k8sClient != nil
}

// RefreshCache loads driver-managed PVs and VolumeAttachments from Kubernetes.
func (c *K8sMetadataChecker) RefreshCache(ctx context.Context) error {
	if !c.Available() {
		return fmt.Errorf("K8sMetadataChecker: kubernetes client not initialized")
	}

	pvList, err := c.k8sClient.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("K8sMetadataChecker: failed to list PVs: %w", err)
	}

	vaList, err := c.k8sClient.StorageV1().VolumeAttachments().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("K8sMetadataChecker: failed to list VolumeAttachments: %w", err)
	}

	newVolumeIDCache := make(map[string]bool)
	newVolumeNameCache := make(map[string]bool)
	newProtocolCache := make(map[string]string)
	newPVNameCache := make(map[string]string)
	newArrayIDCache := make(map[string]string)
	newAttachmentCache := make(map[string]bool)

	for i := range pvList.Items {
		pv := &pvList.Items[i]
		if pv.Spec.CSI == nil || pv.Spec.CSI.Driver != c.driverName {
			continue
		}

		volumeID := extractVolumeIDFromHandle(pv.Spec.CSI.VolumeHandle)
		if volumeID == "" {
			continue
		}

		newVolumeIDCache[volumeID] = true
		newVolumeNameCache[pv.Name] = true
		newPVNameCache[volumeID] = pv.Name
		newArrayIDCache[volumeID] = extractArrayIDFromHandle(pv.Spec.CSI.VolumeHandle)

		if pv.Spec.CSI.VolumeAttributes != nil {
			if protocol, ok := pv.Spec.CSI.VolumeAttributes["Protocol"]; ok {
				normalized := normalizeProtocol(protocol)
				if normalized != "unknown" {
					newProtocolCache[volumeID] = normalized
					newProtocolCache[pv.Name] = normalized
				}
			}
		}
	}

	for i := range vaList.Items {
		va := &vaList.Items[i]
		if va.Spec.Attacher != c.driverName || va.Spec.Source.PersistentVolumeName == nil {
			continue
		}
		newAttachmentCache[*va.Spec.Source.PersistentVolumeName] = va.Status.Attached
	}

	c.cacheMu.Lock()
	c.volumeIDCache = newVolumeIDCache
	c.volumeNameCache = newVolumeNameCache
	c.protocolCache = newProtocolCache
	c.pvNameCache = newPVNameCache
	c.arrayIDCache = newArrayIDCache
	c.attachmentCache = newAttachmentCache
	c.cacheMu.Unlock()

	return nil
}

// IsDriverManaged reports whether the given volume ID belongs to this driver.
func (c *K8sMetadataChecker) IsDriverManaged(_ context.Context, volumeID string) (bool, error) {
	if !c.Available() {
		return false, fmt.Errorf("K8sMetadataChecker: kubernetes client not initialized")
	}

	volumeID = extractVolumeIDFromHandle(volumeID)
	if volumeID == "" {
		return false, nil
	}

	c.cacheMu.RLock()
	managed, exists := c.volumeIDCache[volumeID]
	c.cacheMu.RUnlock()
	if !exists {
		return false, nil
	}
	return managed, nil
}

// IsDriverManagedByName reports whether the PV/file-system name belongs to this driver.
func (c *K8sMetadataChecker) IsDriverManagedByName(_ context.Context, volumeName string) (bool, error) {
	if !c.Available() {
		return false, fmt.Errorf("K8sMetadataChecker: kubernetes client not initialized")
	}

	if volumeName == "" {
		return false, nil
	}

	c.cacheMu.RLock()
	managed, exists := c.volumeNameCache[volumeName]
	c.cacheMu.RUnlock()
	if !exists {
		return false, nil
	}
	return managed, nil
}

// GetProtocol returns the protocol resolved from PV VolumeAttributes.
func (c *K8sMetadataChecker) GetProtocol(_ context.Context, volumeID string) string {
	if !c.Available() {
		return "unknown"
	}

	volumeID = extractVolumeIDFromHandle(volumeID)
	if volumeID == "" {
		return "unknown"
	}

	c.cacheMu.RLock()
	protocol, exists := c.protocolCache[volumeID]
	c.cacheMu.RUnlock()
	if !exists {
		return "unknown"
	}
	return protocol
}

// GetAttachmentStateForVolumeID returns the attachment state for a given volume ID.
func (c *K8sMetadataChecker) GetAttachmentStateForVolumeID(_ context.Context, volumeID string, globalID string) (bool, error) {
	if !c.Available() {
		return false, fmt.Errorf("K8sMetadataChecker: kubernetes client not initialized")
	}

	volumeID = extractVolumeIDFromHandle(volumeID)
	if volumeID == "" {
		return false, nil
	}

	c.cacheMu.RLock()
	pvName, pvExists := c.pvNameCache[volumeID]
	arrayID, arrayExists := c.arrayIDCache[volumeID]
	attached, attachedExists := c.attachmentCache[pvName]
	c.cacheMu.RUnlock()

	if !pvExists {
		return false, nil
	}
	if arrayExists && arrayID != "" && globalID != "" {
		if !strings.Contains(arrayID, globalID) && !strings.Contains(globalID, arrayID) {
			return false, nil
		}
	}
	if !attachedExists {
		return false, nil
	}
	return attached, nil
}

// GetAttachmentStateForPVName returns the attachment state for a given PV name.
func (c *K8sMetadataChecker) GetAttachmentStateForPVName(_ context.Context, pvName string) (bool, error) {
	if !c.Available() {
		return false, fmt.Errorf("K8sMetadataChecker: kubernetes client not initialized")
	}

	if pvName == "" {
		return false, nil
	}

	c.cacheMu.RLock()
	attached, exists := c.attachmentCache[pvName]
	c.cacheMu.RUnlock()
	if !exists {
		return false, nil
	}
	return attached, nil
}

// MarkDeleteComplete removes a volume from validation caches.
func (c *K8sMetadataChecker) MarkDeleteComplete(volumeID string) {
	volumeID = extractVolumeIDFromHandle(volumeID)
	if volumeID == "" {
		return
	}

	c.cacheMu.Lock()
	delete(c.volumeIDCache, volumeID)
	delete(c.pvNameCache, volumeID)
	delete(c.arrayIDCache, volumeID)
	delete(c.protocolCache, volumeID)
	c.cacheMu.Unlock()
}

// MarkDeleteCompleteByName removes a volume name from validation caches.
func (c *K8sMetadataChecker) MarkDeleteCompleteByName(volumeName string) {
	if volumeName == "" {
		return
	}

	c.cacheMu.Lock()
	delete(c.volumeNameCache, volumeName)
	delete(c.protocolCache, volumeName)
	delete(c.attachmentCache, volumeName)
	c.cacheMu.Unlock()
}

func extractVolumeIDFromHandle(handle string) string {
	if handle == "" {
		return ""
	}
	if strings.Contains(handle, "/") {
		parts := strings.Split(handle, "/")
		return parts[0]
	}
	if strings.Contains(handle, "-") {
		parts := strings.Split(handle, "-")
		if len(parts) > 1 {
			return strings.Join(parts[1:], "-")
		}
	}
	return handle
}

func extractArrayIDFromHandle(handle string) string {
	if handle == "" {
		return ""
	}
	if strings.Contains(handle, "/") {
		parts := strings.Split(handle, "/")
		if len(parts) > 1 {
			return parts[len(parts)-2]
		}
	}
	if strings.Contains(handle, "-") {
		parts := strings.Split(handle, "-")
		if len(parts) > 1 {
			return parts[0]
		}
	}
	return ""
}

func normalizeProtocol(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "iscsi", "scsi":
		return "iSCSI"
	case "fc":
		return "FC"
	case "nvmetcp", "nvme-tcp", "nvme_tcp":
		return "NVMeTCP"
	case "nfs":
		return "NFS"
	default:
		return "unknown"
	}
}
