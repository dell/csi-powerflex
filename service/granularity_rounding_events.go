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

package service

// granularity_rounding_events.go
//
// RoundingEventEmitter emits Normal Kubernetes events on PVCs whenever the
// PowerFlex CSI driver rounds a requested volume size up to the array-native
// granularity boundary (1 GiB for Gen2/EC arrays, 8 GiB for Gen1 arrays).
//
// The emitter follows the EventEmitter pattern established in space_reclamation.go
// (FR-5).  It is nil-safe: if the Kubernetes client is unavailable the emitter
// silently discards all events rather than failing the volume operation.

import (
	"fmt"

	csmlog "github.com/Ecosystems/container-storage-modules/src/csmlog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
)

// ── Event reason constants ──────────────────────────────────────────────────

const (
	// EventReasonVolumeSizeRounded is the event reason used when the driver
	// rounds a volume size to the array-native granularity boundary.
	EventReasonVolumeSizeRounded = "VolumeSizeRounded"
)

// ── RoundingEventEmitter ───────────────────────────────────────────────────

// RoundingEventEmitter creates Normal Kubernetes Events on PVCs when the
// requested volume size is rounded up to the array-native granularity (FR-5).
type RoundingEventEmitter struct {
	recorder record.EventRecorder
}

// NewRoundingEventEmitter creates a new RoundingEventEmitter backed by a
// real Kubernetes event recorder.  If clientset is nil an inert emitter is
// returned that discards all events without panicking.
func NewRoundingEventEmitter(clientset kubernetes.Interface, driverName string) *RoundingEventEmitter {
	if clientset == nil {
		return &RoundingEventEmitter{}
	}
	eventBroadcaster := record.NewBroadcaster()
	// Enable debug-level logging of event routing to make event flow visible
	// in debug logs without extra cost (follows space_reclamation.go pattern).
	eventBroadcaster.StartLogging(csmlog.Debugf)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{
		Interface: clientset.CoreV1().Events(""),
	})
	recorder := eventBroadcaster.NewRecorder(
		scheme.Scheme,
		corev1.EventSource{Component: driverName},
	)
	return &RoundingEventEmitter{recorder: recorder}
}

// EmitRounded emits a Normal K8s event on the named PVC when volume size was
// rounded from originalBytes to roundedBytes by the given operation.
//
// The event is suppressed (no-op) when:
//   - the recorder is nil (Kubernetes client unavailable)
//   - originalBytes == roundedBytes (no rounding occurred — exact granularity)
func (e *RoundingEventEmitter) EmitRounded(
	pvcName, pvcNamespace string,
	originalBytes, roundedBytes int64,
	operation, genType string,
) {
	if e.recorder == nil {
		return
	}
	if originalBytes == roundedBytes {
		// No rounding occurred; suppress the event (AC-12).
		return
	}
	// Construct a minimal PVC reference for the event target.
	// We do not need to fetch the full PVC object — just enough for the
	// event infrastructure to record Name/Namespace/Kind.
	pvcRef := &corev1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PersistentVolumeClaim",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: pvcNamespace,
		},
	}
	msg := fmt.Sprintf(
		"Volume size rounded from %d bytes to %d bytes for %s operation (genType: %q, array-native granularity applied)",
		originalBytes, roundedBytes, operation, genType,
	)
	e.recorder.Event(pvcRef, corev1.EventTypeNormal, EventReasonVolumeSizeRounded, msg)
}
