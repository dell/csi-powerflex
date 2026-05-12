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

package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dell/gofsutil"
	"github.com/dell/goscaleio"
	cron "github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
)

// ---- Annotation key constants ----

const (
	// LabelPrefix is the prefix for space reclamation PVC labels.
	LabelPrefix = "space-reclamation.csi.dell.com/"
	// LabelEnabled controls per-PVC opt-in/opt-out via labels.
	FstrimLabelEnabled = LabelPrefix + "enabled"
	BlockLabelEnabled  = LabelPrefix + "block-reclaim"

	// AnnotationPrefix is the prefix for space reclamation PVC annotations (for result metadata).
	AnnotationPrefix = "space-reclamation.csi.dell.com/"
	// AnnotationLastRunTime records the last reclamation timestamp.
	AnnotationLastRunTime = AnnotationPrefix + "last-run-time"
	// AnnotationBytesReclaim records bytes reclaimed.
	AnnotationBytesReclaim = AnnotationPrefix + "bytes-reclaimed"
	// AnnotationDuration records the reclamation duration in seconds.
	AnnotationDuration = AnnotationPrefix + "duration-seconds"
	// AnnotationStatus records the reclamation status.
	AnnotationStatus = AnnotationPrefix + "status"
	// AnnotationErrorMsg records any error message.
	AnnotationErrorMsg = AnnotationPrefix + "error-message"
	// AnnotationNode records the node where reclamation ran.
	AnnotationNode = AnnotationPrefix + "node"
)

// ---- Event reason constants ----

const (
	// EventReasonCompleted is the event reason for successful reclamation.
	EventReasonCompleted = "SpaceReclamationCompleted"
	// EventReasonFailed is the event reason for failed reclamation.
	EventReasonFailed = "SpaceReclamationFailed"
	// EventReasonTimeout is the event reason for timed-out reclamation.
	EventReasonTimeout = "SpaceReclamationTimeout"
	// EventReasonUnsupported is the event reason for unsupported devices.
	EventReasonUnsupported = "SpaceReclamationUnsupported"
)

// ---- Volume mode constants ----

// VolumeMode distinguishes filesystem from raw block volumes.
type VolumeMode corev1.PersistentVolumeMode

// Volume mode constants
const (
	VolumeModeFilesystem VolumeMode = VolumeMode(corev1.PersistentVolumeFilesystem)
	VolumeModeBlock      VolumeMode = VolumeMode(corev1.PersistentVolumeBlock)
)

// ---- Configuration ----

// SpaceReclamationConfig holds configuration for the space reclamation feature.
type SpaceReclamationConfig struct {
	// Enabled gates the entire subsystem.
	Enabled bool
	// Schedule is a cron expression (5-field). Default: "0 2 * * 0".
	Schedule string
	// MaxConcurrentVolumes is the max parallel reclamation jobs per node. Default: 2.
	MaxConcurrentVolumes int
	// TimeoutSeconds is the per-volume timeout. Default: 14400.
	TimeoutSeconds int
	// NodeName is the Kubernetes node name (from downward API or env var).
	NodeName string
}

// getEnvString reads an environment variable and returns a default if unset or empty.
func getEnvString(key, defaultVal string) string {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	return val
}

// getEnvBool reads an environment variable as a boolean, returning a default on error or empty.
func getEnvBool(key string, defaultVal bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		return defaultVal
	}
	return b
}

// getEnvInt reads an environment variable as an int, returning a default on error, empty, or negative.
func getEnvInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	if i < 0 {
		return defaultVal
	}
	return i
}

// ReadSpaceReclamationConfig reads configuration from environment variables.
func ReadSpaceReclamationConfig() SpaceReclamationConfig {
	cfg := SpaceReclamationConfig{
		Enabled:              getEnvBool(EnvSpaceReclamationEnabled, false),
		Schedule:             getEnvString(EnvSpaceReclamationSchedule, "0 2 * * 0"), // Default: weekly Sunday 2:00 AM
		MaxConcurrentVolumes: getEnvInt(EnvSpaceReclamationMaxConcurrent, 2),
		TimeoutSeconds:       getEnvInt(EnvSpaceReclamationTimeout, 14400),
		NodeName:             getEnvString(EnvKubeNodeName, ""),
	}
	return cfg
}

// ---- Volume Info ----

// VolumeInfo stores metadata about a staged volume for reclamation.
type VolumeInfo struct {
	VolumeID     string
	StagingPath  string     // Mount point for filesystem PVs; device path for block PVs
	DevicePath   string     // Underlying block device (e.g., /dev/sda, /dev/dm-0)
	VolumeMode   VolumeMode // Filesystem or Block
	PVCName      string
	PVCNamespace string
	PVC          *corev1.PersistentVolumeClaim // PVC object (fetched in RunOnce, reused in reclaimVolume)
}

// ---- Reclamation Result ----

// ReclamationResult represents the outcome of a reclamation operation.
type ReclamationResult struct {
	Status         string // "success", "error", "timeout", "unsupported"
	BytesReclaimed int64
	Duration       time.Duration
	ErrorMessage   string // populated on failure
	NodeName       string
}

// ---- PVC Annotator ----

// PVCAnnotator updates PVC annotations with reclamation results.
type PVCAnnotator struct {
	client   kubernetes.Interface
	maxRetry int
}

// NewPVCAnnotator creates a new PVCAnnotator.
func NewPVCAnnotator(client kubernetes.Interface) *PVCAnnotator {
	return &PVCAnnotator{
		client:   client,
		maxRetry: 3,
	}
}

// Annotate updates the PVC with reclamation result annotations.
// It handles 404 (PVC not found) and 409 (conflict, retry) responses.
func (a *PVCAnnotator) Annotate(ctx context.Context, pvcName, pvcNamespace string, result *ReclamationResult) error {
	var lastErr error
	for attempt := 0; attempt <= a.maxRetry; attempt++ {
		// GET the latest PVC
		pvc, err := a.client.CoreV1().PersistentVolumeClaims(pvcNamespace).Get(ctx, pvcName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get PVC %s/%s: %w", pvcNamespace, pvcName, err)
		}

		// Merge annotations
		if pvc.Annotations == nil {
			pvc.Annotations = make(map[string]string)
		}
		pvc.Annotations[AnnotationStatus] = result.Status
		pvc.Annotations[AnnotationLastRunTime] = time.Now().UTC().Format(time.RFC3339)
		pvc.Annotations[AnnotationBytesReclaim] = strconv.FormatInt(result.BytesReclaimed, 10)
		pvc.Annotations[AnnotationDuration] = fmt.Sprintf("%.3f", result.Duration.Seconds())
		pvc.Annotations[AnnotationNode] = result.NodeName
		if result.ErrorMessage != "" {
			pvc.Annotations[AnnotationErrorMsg] = result.ErrorMessage
		} else {
			// Clear error message on success to remove stale error states
			delete(pvc.Annotations, AnnotationErrorMsg)
		}

		// UPDATE the PVC
		_, err = a.client.CoreV1().PersistentVolumeClaims(pvcNamespace).Update(ctx, pvc, metav1.UpdateOptions{})
		if err == nil {
			return nil
		}
		lastErr = err
		// Retry on conflict (409)
		if strings.Contains(err.Error(), "the object has been modified") || strings.Contains(err.Error(), "Conflict") {
			continue
		}
		return fmt.Errorf("failed to update PVC %s/%s: %w", pvcNamespace, pvcName, err)
	}
	return lastErr
}

// ---- Event Emitter ----

// EventEmitter creates Kubernetes Events on PVCs.
type EventEmitter struct {
	recorder record.EventRecorder
}

// NewEventEmitter creates a new EventEmitter with a Kubernetes event recorder.
func NewEventEmitter(clientset kubernetes.Interface, driverName string) *EventEmitter {
	if clientset == nil {
		return &EventEmitter{}
	}
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{
		Interface: clientset.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: driverName})
	return &EventEmitter{recorder: recorder}
}

// EmitSuccess records a successful reclamation event on the PVC.
func (e *EventEmitter) EmitSuccess(pvc *corev1.PersistentVolumeClaim, bytesReclaimed int64, duration time.Duration) {
	if e.recorder == nil {
		return
	}
	msg := fmt.Sprintf("Space reclamation completed: %d bytes reclaimed in %.2fs", bytesReclaimed, duration.Seconds())
	e.recorder.Event(pvc, corev1.EventTypeNormal, EventReasonCompleted, msg)
}

// EmitFailure records a failed reclamation event on the PVC.
func (e *EventEmitter) EmitFailure(pvc *corev1.PersistentVolumeClaim, err error) {
	if e.recorder == nil {
		return
	}
	msg := fmt.Sprintf("Space reclamation failed: %v", err)
	e.recorder.Event(pvc, corev1.EventTypeWarning, EventReasonFailed, msg)
}

// EmitTimeout records a timed-out reclamation event on the PVC.
func (e *EventEmitter) EmitTimeout(pvc *corev1.PersistentVolumeClaim, timeout time.Duration) {
	if e.recorder == nil {
		return
	}
	msg := fmt.Sprintf("Space reclamation timed out after %v", timeout)
	e.recorder.Event(pvc, corev1.EventTypeWarning, EventReasonTimeout, msg)
}

// EmitUnsupported records an unsupported-device reclamation event on the PVC.
func (e *EventEmitter) EmitUnsupported(pvc *corev1.PersistentVolumeClaim, reason string) {
	if e.recorder == nil {
		return
	}
	msg := fmt.Sprintf("Device does not support space reclamation: %s", reason)
	e.recorder.Event(pvc, corev1.EventTypeWarning, EventReasonUnsupported, msg)
}

// ---- Eligibility ----

// IsEligible determines if a volume is eligible for reclamation based on
// global config and per-PVC labels.
// Returns (eligible, reason) where reason explains why not eligible (empty if eligible).
func IsEligible(globalEnabled bool, labels map[string]string, volumeMode VolumeMode) (bool, string) {
	// Block mode requires explicit opt-in via label
	if volumeMode == VolumeModeBlock {
		if labels == nil {
			return false, "block mode missing required label"
		}
		val, ok := labels[BlockLabelEnabled]
		if !ok {
			return false, "block mode missing required label"
		}
		if !strings.EqualFold(val, "true") {
			return false, fmt.Sprintf("block mode label is '%s' (must be 'true')", val)
		}
		return true, ""
	}

	// Filesystem mode: explicit label takes precedence, otherwise follow global config
	if labels == nil {
		if globalEnabled {
			return true, ""
		}
		return false, "global disabled"
	}
	val, ok := labels[FstrimLabelEnabled]
	if !ok {
		if globalEnabled {
			return true, ""
		}
		return false, "global disabled"
	}
	if strings.EqualFold(val, "true") {
		return true, ""
	}
	if strings.EqualFold(val, "false") {
		return false, "explicit opt-out via label"
	}
	return false, fmt.Sprintf("label is '%s' (must be 'true' or 'false')", val)
}

// ---- Injectable function variables (overridable in tests) ----

// getLocalVolumeMapFunc is overridable in tests to avoid real SDC calls.
var getLocalVolumeMapFunc = func() ([]*goscaleio.SdcMappedVolume, error) {
	return goscaleio.GetLocalVolumeMap()
}

// wwnToDevicePathFunc is overridable in tests to avoid real /dev/disk/by-id lookups.
var wwnToDevicePathFunc = func(ctx context.Context, nguid string) (string, error) {
	_, devPath, err := gofsutil.WWNToDevicePathX(ctx, nguid)
	return devPath, err
}

// checkDiscardSupportFunc is overridable in tests to avoid real sysfs reads.
var checkDiscardSupportFunc = func(ctx context.Context, devicePath string) (*gofsutil.DiscardCapability, error) {
	return gofsutil.CheckDiscardSupport(ctx, devicePath)
}

// osStatFunc is overridable in tests to control device existence checks.
var osStatFunc = os.Stat

// ---- Space Reclamation Manager ----

// SpaceReclamationManager orchestrates periodic space reclamation on staged volumes.
type SpaceReclamationManager struct {
	config      SpaceReclamationConfig
	useNVME     bool
	annotator   *PVCAnnotator
	emitter     *EventEmitter
	k8sClient   kubernetes.Interface
	semaphore   chan struct{}
	volumeLocks sync.Map
	ctx         context.Context
	cronSched   *cron.Cron
	running     atomic.Bool // Flag to prevent overlapping RunOnce cycles
}

// NewSpaceReclamationManager creates a new SpaceReclamationManager.
// Returns error if the cron schedule expression is invalid.
func NewSpaceReclamationManager(
	ctx context.Context,
	config SpaceReclamationConfig,
	k8sClient kubernetes.Interface,
	nodeName string,
	useNVME bool,
) (*SpaceReclamationManager, error) {
	// Validate the cron expression by attempting to parse it
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := parser.Parse(config.Schedule)
	if err != nil {
		log.Errorf("SpaceReclamation: invalid cron schedule %q: %v. Set environment variable X_CSI_SPACE_RECLAMATION_SCHEDULE. Example formats: \"0 2 * * *\" (daily at 2 AM), \"*/5 * * * *\" (every 5 minutes), \"0 */6 * * *\" (every 6 hours)", config.Schedule, err)
		return nil, fmt.Errorf("invalid cron schedule: %q: %w", config.Schedule, err)
	}

	config.NodeName = nodeName

	semSize := config.MaxConcurrentVolumes
	if semSize <= 0 {
		log.Warnf("SpaceReclamation: MaxConcurrentVolumes is %d, using default value 1", semSize)
		semSize = 1
	}

	mgr := &SpaceReclamationManager{
		config:    config,
		useNVME:   useNVME,
		annotator: NewPVCAnnotator(k8sClient),
		emitter:   NewEventEmitter(k8sClient, Name),
		k8sClient: k8sClient,
		semaphore: make(chan struct{}, semSize),
		ctx:       ctx,
	}
	return mgr, nil
}

// Start begins the cron-based reclamation scheduler.
func (m *SpaceReclamationManager) Start() error {
	m.cronSched = cron.New(cron.WithParser(cron.NewParser(
		cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
	)))
	_, err := m.cronSched.AddFunc(m.config.Schedule, m.RunOnce)
	if err != nil {
		log.Errorf("SpaceReclamation: failed to add cron job: %v", err)
		return fmt.Errorf("failed to add cron job: %w", err)
	}
	m.cronSched.Start()
	log.Infof("SpaceReclamation: scheduler running with schedule %q", m.config.Schedule)
	return nil
}

// RunOnce executes one reclamation cycle using on-demand discovery.
// Called by the cron scheduler.
// It discovers eligible volumes by:
//  1. Listing all Bound PowerFlex PVs from Kubernetes.
//  2. Checking live PVC annotations for eligibility (fail fast, no SDC/mount work for ineligible PVCs).
//  3. Looking up the volume in goscaleio.GetLocalVolumeMap() to confirm it is on this node.
//  4. Resolving the mount path from gofsutil.GetMounts() to determine VolumeMode.
func (m *SpaceReclamationManager) RunOnce() {
	log.Info("SpaceReclamation: starting RunOnce cycle")

	// Prevent overlapping cycles
	if !m.running.CompareAndSwap(false, true) {
		log.Warn("SpaceReclamation: previous scheduled run is still in progress, skipping this cycle")
		return
	}
	defer m.running.Store(false)

	// Create a cycle-level timeout context that applies to the entire reclamation run.
	timeout := time.Duration(m.config.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(m.ctx, timeout)
	defer cancel()

	// Step 1: List all PVs in the cluster (field selector on status.phase not supported, filter client-side)
	pvList, err := m.k8sClient.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Errorf("SpaceReclamation: failed to list PersistentVolumes: %v", err)
		return
	}
	log.Infof("SpaceReclamation: found %d total PVs", len(pvList.Items))

	// Step 2: Build a map of volumeID → SdcMappedVolume for O(1) lookup (call once, not per volume).
	localVols, err := getLocalVolumeMapFunc()
	if err != nil {
		log.Errorf("SpaceReclamation: failed to get local volume map: %v", err)
		return
	}
	log.Infof("SpaceReclamation: found %d local volumes on this node are mapped to SDC %v", len(localVols), localVols)
	localVolMap := make(map[string]*goscaleio.SdcMappedVolume, len(localVols))
	for _, v := range localVols {
		localVolMap[v.VolumeID] = v
		log.Infof("SpaceReclamation: local volume - VolumeID: %s, SdcDevice: %s", v.VolumeID, v.SdcDevice)
	}

	// Step 3: Build a map of device → mountPath for O(1) lookup (call once, not per volume).
	mounts, err := gofsutil.GetMounts(ctx)
	if err != nil {
		log.Errorf("SpaceReclamation: failed to get mounts: %v", err)
		return
	}
	log.Infof("SpaceReclamation: found %d total mounts and %v", len(mounts), mounts)
	deviceToMount := make(map[string]string, len(mounts))
	for _, mnt := range mounts {
		// Filter to only CSI-related mounts (pod mounts and CSI staging paths).
		// NVMe: device mounted at both /pods/... (pod) and /plugins/kubernetes.io/csi/.../globalmount (staging)
		// SDC: device mounted at /pods/... (pod) only; /plugins/vxflexos.emc.dell.com/disks/... is a symlink, not a mount
		if !strings.Contains(mnt.Path, "/var/lib/kubelet/pods/") &&
			!strings.Contains(mnt.Path, "/var/lib/kubelet/plugins/kubernetes.io/csi/") {
			continue
		}
		log.Infof("SpaceReclamation: CSI mount - Device: %s, Path: %s", mnt.Device, mnt.Path)

		// Validate device exists before adding to map (prevents stale mount entries)
		if _, err := osStatFunc(mnt.Device); os.IsNotExist(err) {
			log.Warnf("SpaceReclamation: skipping mount for non-existent device %s (stale mount entry)", mnt.Device)
			continue
		}

		// Prefer globalmount (staging path) over pod mount paths.
		// For NVMe nodes: device has both mounts, globalmount is more stable and works even without a pod.
		// For SDC nodes: only pod mount is available (disk symlink path is filtered out above).
		currentPath, exists := deviceToMount[mnt.Device]
		if !exists {
			deviceToMount[mnt.Device] = mnt.Path
		} else if strings.Contains(mnt.Path, "/plugins/kubernetes.io/csi/") && !strings.Contains(currentPath, "/plugins/kubernetes.io/csi/") {
			// Replace with globalmount (staging path) if current is a pod mount
			deviceToMount[mnt.Device] = mnt.Path
		}
	}

	var wg sync.WaitGroup
	for i := range pvList.Items {
		pv := &pvList.Items[i]

		// Filter: only process PVs managed by this driver (check CSI driver field)
		if pv.Spec.CSI == nil || pv.Spec.CSI.Driver != Name {
			log.Infof("SpaceReclamation: skipping PV %s (not managed by this driver)", pv.Name)
			continue
		}
		// Filter: only process Bound PVs (field selector not supported, filter client-side)
		if pv.Status.Phase != corev1.VolumeBound {
			log.Infof("SpaceReclamation: skipping PV %s (phase: %s)", pv.Name, pv.Status.Phase)
			continue
		}

		log.Infof("SpaceReclamation: processing PV %s with AccessModes: %v", pv.Name, pv.Spec.AccessModes)

		// Skip RWX volumes - space reclamation not supported for multi-node access
		isRWX := false
		for _, accessMode := range pv.Spec.AccessModes {
			if accessMode == corev1.ReadWriteMany {
				log.Infof("SpaceReclamation: AccessModes detected:%s for %s", accessMode, pv.Name)
				isRWX = true
				break
			}
			log.Infof("SpaceReclamation: AccessModes detected other than RWX :%s for %s", accessMode, pv.Name)
		}
		if isRWX {
			log.Infof("SpaceReclamation: skipping RWX volume %s", pv.Name)
			continue
		}

		pvcRef := pv.Spec.ClaimRef
		if pvcRef == nil {
			log.Infof("SpaceReclamation: skipping PV %s (no PVC reference)", pv.Name)
			continue
		}

		// Step 4 (fail fast): Check live PVC annotations for eligibility before any SDC/mount work.
		pvc, err := m.k8sClient.CoreV1().PersistentVolumeClaims(pvcRef.Namespace).Get(ctx, pvcRef.Name, metav1.GetOptions{})
		if err != nil {
			log.Warnf("SpaceReclamation: failed to get PVC %s/%s: %v", pvcRef.Namespace, pvcRef.Name, err)
			continue
		}
		var volMode VolumeMode
		if pvc.Spec.VolumeMode != nil {
			volMode = VolumeMode(*pvc.Spec.VolumeMode)
		} else {
			// Default to Filesystem mode if not specified
			volMode = VolumeModeFilesystem
		}
		log.Infof("SpaceReclamation: PV %s has VolumeMode: %s, Labels: %v", pv.Name, volMode, pvc.Labels)
		eligible, reason := IsEligible(m.config.Enabled, pvc.Labels, volMode)
		if !eligible {
			log.Infof("SpaceReclamation: skipping PV %s (not eligible: %s)", pv.Name, reason)
			continue
		}
		log.Infof("SpaceReclamation: PV %s is eligible for reclamation", pv.Name)

		// Step 5: Confirm volume is on this node and resolve its device path.
		// The CSI VolumeHandle encodes as "<systemID>-<volID>" (last dash-token is volID).
		csiHandle := pv.Spec.CSI.VolumeHandle
		// Only check fsType for filesystem mode volumes (block volumes don't have fsType)
		fsType := strings.ToLower(pv.Spec.CSI.FSType)
		if volMode == VolumeModeFilesystem {
			if fsType != "xfs" && fsType != "ext4" {
				log.Infof("SpaceReclamation: skipping PV %s (unsupported fsType: %q)", pv.Name, pv.Spec.CSI.FSType)
				continue
			}
		}
		volID := getVolumeIDFromCsiVolumeID(csiHandle)
		if volID == "" {
			log.Warnf("SpaceReclamation: could not extract volID from handle %q", csiHandle)
			continue
		}

		var devicePath string
		if m.useNVME {
			// NVMe path: derive NGUID from volID + systemID, then look up device via /dev/disk/by-id.
			systemID := getSystemIDFromPVHandle(csiHandle)
			if systemID == "" {
				log.Warnf("SpaceReclamation: could not extract systemID from handle %q", csiHandle)
				continue
			}
			nguid, err := buildNGUID(volID, systemID)
			if err != nil {
				log.Warnf("SpaceReclamation: could not build NGUID for vol %s system %s: %v", volID, systemID, err)
				continue
			}
			devPath, err := wwnToDevicePathFunc(ctx, nguid)
			if err != nil || devPath == "" {
				log.Infof("SpaceReclamation: PV %s not on this node (NVMe)", pv.Name)
				continue
			}
			devicePath = devPath
			log.Infof("SpaceReclamation: PV %s resolved to device %s via NVMe NGUID lookup (nguid=%s)", pv.Name, devicePath, nguid)
		} else {
			// SDC path: confirm volume is in the local SDC volume map.
			sdcVol, onThisNode := localVolMap[volID]
			if !onThisNode {
				log.Infof("SpaceReclamation: PV %s not on this node (SDC)", pv.Name)
				continue
			}
			devicePath = sdcVol.SdcDevice
			log.Infof("SpaceReclamation: PV %s resolved to device %s via SDC", pv.Name, devicePath)
		}

		// Step 6: Resolve VolumeMode and staging path from mount table.
		var stagingPath string
		if volMode == VolumeModeFilesystem {
			if mountPath, mounted := deviceToMount[devicePath]; mounted {
				stagingPath = mountPath
				log.Infof("SpaceReclamation: PV %s filesystem mode, device path: %s and staging path: %s", pv.Name, devicePath, stagingPath)
			} else {
				log.Warnf("SpaceReclamation: PV %s filesystem mode but no mount found for device %s", pv.Name, devicePath)
				continue
			}
		} else {
			stagingPath = devicePath
			log.Infof("SpaceReclamation: PV %s block mode, device path: %s and staging path: %s", pv.Name, devicePath, stagingPath)
		}

		vol := &VolumeInfo{
			VolumeID:     volID,
			StagingPath:  stagingPath,
			DevicePath:   devicePath,
			VolumeMode:   volMode,
			PVCName:      pvcRef.Name,
			PVCNamespace: pvcRef.Namespace,
			PVC:          pvc,
		}

		log.Infof("SpaceReclamation: submitting reclamation job for PV %s (VolumeID: %s, Device: %s, Path: %s, Mode: %s, FsType: %s)",
			pv.Name, volID, devicePath, stagingPath, volMode, fsType)
		wg.Add(1)
		go func(v *VolumeInfo) {
			defer wg.Done()
			m.reclaimVolume(ctx, v)
		}(vol)
	}
	wg.Wait()
	log.Info("SpaceReclamation: completed RunOnce cycle")
}

// reclaimVolume performs space reclamation on a single volume.
// ctx is the cycle-level context with a timeout shared across all volumes in the run.
func (m *SpaceReclamationManager) reclaimVolume(ctx context.Context, vol *VolumeInfo) {
	log.Infof("SpaceReclamation: starting reclamation for volume %s (PVC: %s/%s, Mode: %s, Device: %s, Path: %s)",
		vol.VolumeID, vol.PVCNamespace, vol.PVCName, vol.VolumeMode, vol.DevicePath, vol.StagingPath)

	// Acquire semaphore for concurrency control
	select {
	case m.semaphore <- struct{}{}:
		defer func() { <-m.semaphore }()
	case <-ctx.Done():
		return
	}

	// Acquire per-volume mutex to prevent duplicate jobs
	mu := &sync.Mutex{}
	actual, _ := m.volumeLocks.LoadOrStore(vol.VolumeID, mu)
	actualMu := actual.(*sync.Mutex)
	actualMu.Lock()
	defer actualMu.Unlock()

	// Check if device supports discard operations
	capability, err := checkDiscardSupportFunc(ctx, vol.DevicePath)
	reason := ""         // default: empty string
	unsupported := false // default: proceed with reclamation
	if err != nil {
		// Error checking discard support — treat as unsupported
		reason = fmt.Sprintf("failed to check discard support: %v", err)
		unsupported = true
	} else if capability != nil && !capability.Supported {
		// Successfully checked and device is unsupported
		reason = capability.Reason
		unsupported = true
	}
	if unsupported {
		log.Infof("SpaceReclamation: volume %s does not support discard (device: %s, reason: %s)", vol.VolumeID, vol.DevicePath, reason)
		// Annotate as unsupported
		result := &ReclamationResult{
			Status:       "unsupported",
			ErrorMessage: reason,
			NodeName:     m.config.NodeName,
		}
		if m.annotator != nil && m.k8sClient != nil && vol.PVCName != "" {
			_ = m.annotator.Annotate(ctx, vol.PVCName, vol.PVCNamespace, result)
		}
		// Emit event for unsupported device
		if m.emitter != nil && vol.PVC != nil {
			m.emitter.EmitUnsupported(vol.PVC, reason)
		}
		return
	}

	var bytesReclaimed int64
	var reclaimErr error
	start := time.Now()

	// Execute the reclamation operation
	switch vol.VolumeMode {
	case VolumeModeFilesystem:
		var fstrimResult *gofsutil.FstrimResult
		fstrimResult, reclaimErr = gofsutil.Fstrim(ctx, vol.StagingPath)
		if reclaimErr == nil && fstrimResult != nil {
			bytesReclaimed = fstrimResult.BytesTrimmed
		}
	case VolumeModeBlock:
		var blkResult *gofsutil.BlkdiscardResult
		blkResult, reclaimErr = gofsutil.Blkdiscard(ctx, vol.DevicePath)
		if reclaimErr == nil && blkResult != nil {
			bytesReclaimed = blkResult.BytesDiscarded
		}
	}

	duration := time.Since(start)

	// Build the result
	var result *ReclamationResult
	if reclaimErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result = &ReclamationResult{
				Status:       "timeout",
				ErrorMessage: fmt.Sprintf("operation timed out after %v", time.Duration(m.config.TimeoutSeconds)*time.Second),
				NodeName:     m.config.NodeName,
				Duration:     duration,
			}
		} else {
			result = &ReclamationResult{
				Status:       "error",
				ErrorMessage: reclaimErr.Error(),
				NodeName:     m.config.NodeName,
				Duration:     duration,
			}
		}
	} else {
		result = &ReclamationResult{
			Status:         "success",
			BytesReclaimed: bytesReclaimed,
			Duration:       duration,
			NodeName:       m.config.NodeName,
		}
	}

	// Annotate the PVC with results.
	// If ctx is already expired (e.g. after a timeout), the Kubernetes API calls inside
	// Annotate would immediately fail. Use a fresh context derived from the manager's
	// parent context so the annotation is always written regardless of reclamation outcome.
	annotateCtx := ctx
	if ctx.Err() != nil {
		var annotateCancel context.CancelFunc
		annotateCtx, annotateCancel = context.WithTimeout(m.ctx, 10*time.Second)
		defer annotateCancel()
	}
	if m.annotator != nil && m.k8sClient != nil && vol.PVCName != "" {
		_ = m.annotator.Annotate(annotateCtx, vol.PVCName, vol.PVCNamespace, result)
	}

	// Emit Kubernetes event based on result status
	if m.emitter != nil && vol.PVC != nil {
		switch result.Status {
		case "success":
			m.emitter.EmitSuccess(vol.PVC, result.BytesReclaimed, result.Duration)
		case "timeout":
			m.emitter.EmitTimeout(vol.PVC, time.Duration(m.config.TimeoutSeconds)*time.Second)
		case "error":
			m.emitter.EmitFailure(vol.PVC, errors.New(result.ErrorMessage))
		}
	}

	log.Infof("SpaceReclamation: completed reclamation for volume %s (PVC: %s/%s) - Status: %s, BytesReclaimed: %d, Duration: %v",
		vol.VolumeID, vol.PVCNamespace, vol.PVCName, result.Status, result.BytesReclaimed, result.Duration)
}

// getSystemIDFromPVHandle extracts the systemID from a CSI volume handle.
// The handle format is "<systemID>-<volID>"; this returns the prefix before the last "-".
func getSystemIDFromPVHandle(csiHandle string) string {
	i := strings.LastIndex(csiHandle, "-")
	if i <= 0 {
		return ""
	}
	return csiHandle[:i]
}

// initSpaceReclamation reads env and initializes the space reclamation manager on the service.
// This is called from BeforeServe when in node mode.
func initSpaceReclamation(ctx context.Context, s *service, k8sClient kubernetes.Interface) {
	cfg := ReadSpaceReclamationConfig()
	mgr, err := NewSpaceReclamationManager(ctx, cfg, k8sClient, cfg.NodeName, s.useNVME)
	if err != nil {
		log.Errorf("Failed to create SpaceReclamationManager: %v", err)
		return
	}
	if err := mgr.Start(); err != nil {
		log.Errorf("Failed to start SpaceReclamationManager: %v", err)
		return
	}
	s.spaceReclaimMgr = mgr
}
