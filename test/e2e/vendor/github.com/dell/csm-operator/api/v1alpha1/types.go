/*

Copyright (c) 2021 Dell Inc., or its subsidiaries. All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
)

// CSMStateType - type representing the state of the ContainerStorageModule (in status)
type CSMStateType string

// CSMOperatorConditionType  defines the type of the last status update
type CSMOperatorConditionType string

// ImageType - represents type of image
type ImageType string

// DriverType - type representing the type of the driver. e.g. - powermax, unity
type DriverType string

// ModuleType - type representing the type of the modules. e.g. - authorization, podmon
type ModuleType string

const (
	// Replication - placeholder for replication constant
	Replication ModuleType = "replication"

	// Observability - placeholder for constant observability
	Observability ModuleType = "observability"

	// PodMon - placeholder for constant podmon
	PodMon ModuleType = "podmon"

	// VgSnapShotter - placeholder for constant vgsnapshotter
	VgSnapShotter ModuleType = "vgsnapshotter"

	// Authorization - placeholder for constant authorization
	Authorization ModuleType = "authorization"

	// ReverseProxy - placeholder for constant csireverseproxy
	ReverseProxy ModuleType = "csireverseproxy"

	// PowerFlex - placeholder for constant powerflex
	PowerFlex DriverType = "powerflex"

	// PowerMax - placeholder for constant powermax
	PowerMax DriverType = "powermax"

	// PowerScale - placeholder for constant isilon
	PowerScale DriverType = "isilon"

	// PowerScaleName - placeholder for constant PowerScale
	PowerScaleName DriverType = "powerscale"

	// Unity - placeholder for constant unity
	Unity DriverType = "unity"

	// PowerStore - placeholder for constant powerstore
	PowerStore DriverType = "powerstore"

	// Provisioner - placeholder for constant
	Provisioner = "provisioner"
	// Attacher - placeholder for constant
	Attacher = "attacher"
	// Snapshotter - placeholder for constant
	Snapshotter = "snapshotter"
	// Registrar - placeholder for constant
	Registrar = "registrar"
	// Resizer - placeholder for constant
	Resizer = "resizer"
	// Sdcmonitor - placeholder for constant
	Sdcmonitor = "sdc-monitor"
	// Externalhealthmonitor - placeholder for constant
	Externalhealthmonitor = "external-health-monitor"

	// EventDeleted - Deleted in event recorder
	EventDeleted = "Deleted"
	// EventUpdated - Updated in event recorder
	EventUpdated = "Updated"
	// EventCompleted - Completed in event recorder
	EventCompleted = "Completed"

	// Succeeded - constant
	Succeeded CSMOperatorConditionType = "Succeeded"
	// InvalidConfig - constant
	InvalidConfig CSMOperatorConditionType = "InvalidConfig"
	// Running - Constant
	Running CSMOperatorConditionType = "Running"
	// Error - Constant
	Error CSMOperatorConditionType = "Error"
	// Updating - Constant
	Updating CSMOperatorConditionType = "Updating"
	// Failed - Constant
	Failed CSMOperatorConditionType = "Failed"
)

// Module defines the desired state of a ContainerStorageModuleModules
type Module struct {
	// Name is name of ContainerStorageModule modules
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Name"
	Name ModuleType `json:"name" yaml:"name"`

	// Enabled is used to indicate wether or not to deploy a module
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Enabled"
	Enabled bool `json:"enabled" yaml:"enabled"`

	// ConfigVersion is the configuration version of the driver
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Config Version"
	ConfigVersion string `json:"configVersion,omitempty" yaml:"configVersion,omitempty"`

	// Components is the specification for SM components containers
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ContainerStorageModule components specification"
	Components []ContainerTemplate `json:"components,omitempty" yaml:"components,omitempty"`
}

// PodStatus - Represents PodStatus in a daemonset or deployment
type PodStatus struct {
	Available string `json:"available,omitempty"`
	Desired   string `json:"desired,omitempty"`
	Failed    string `json:"failed,omitempty"`
}

// Driver of CSIDriver
// +k8s:openapi-gen=true
type Driver struct {
	// CSIDriverType is the CSI Driver type for Dell Technologies - e.g, powermax, powerflex,...
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="CSI Driver Type"
	CSIDriverType DriverType `json:"csiDriverType" yaml:"csiDriverType"`

	// CSIDriverSpec is the specification for CSIDriver
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="CSI Driver Spec"
	CSIDriverSpec CSIDriverSpec `json:"csiDriverSpec" yaml:"csiDriverSpec"`

	// ConfigVersion is the configuration version of the driver
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Config Version"
	ConfigVersion string `json:"configVersion" yaml:"configVersion"`

	// Replicas is the count of controllers for Controller plugin
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Controller count"
	Replicas int32 `json:"replicas" yaml:"replicas"`

	// DNSPolicy is the dnsPolicy of the daemonset for Node plugin
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="DNSPolicy"
	DNSPolicy string `json:"dnsPolicy,omitempty" yaml:"dnsPolicy"`

	// Common is the common specification for both controller and node plugins
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Common specification"
	Common ContainerTemplate `json:"common" yaml:"common"`

	// Controller is the specification for Controller plugin only
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Controller Specification"
	Controller ContainerTemplate `json:"controller,omitempty" yaml:"controller"`

	// Node is the specification for Node plugin only
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Node specification"
	Node ContainerTemplate `json:"node,omitempty" yaml:"node"`

	// SideCars is the specification for CSI sidecar containers
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="CSI SideCars specification"
	SideCars []ContainerTemplate `json:"sideCars,omitempty" yaml:"sideCars"`

	// InitContainers is the specification for Driver InitContainers
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="InitContainers"
	InitContainers []ContainerTemplate `json:"initContainers,omitempty" yaml:"initContainers"`

	// SnapshotClass is the specification for Snapshot Classes
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Snapshot Classes"
	SnapshotClass []SnapshotClass `json:"snapshotClass,omitempty" yaml:"snapshotClass"`

	// ForceUpdate is the boolean flag used to force an update of the driver instance
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Force update"
	ForceUpdate bool `json:"forceUpdate,omitempty" yaml:"forceUpdate"`

	// AuthSecret is the name of the credentials secret for the driver
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Auth Secret"
	AuthSecret string `json:"authSecret,omitempty" yaml:"authSecret"`

	// TLSCertSecret is the name of the TLS Cert secret
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="TLSCert Secret"
	TLSCertSecret string `json:"tlsCertSecret,omitempty" yaml:"tlsCertSecret"`

	// ForceRemoveDriver is the boolean flag used to remove driver deployment when CR is deleted
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Force Remove Driver"
	ForceRemoveDriver bool `json:"forceRemoveDriver,omitempty" yaml:"forceRemoveDriver"`
}

//ContainerTemplate template
type ContainerTemplate struct {

	// Name is the name of Container
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Container Name"
	Name string `json:"name,omitempty" yaml:"name,omitempty"`

	// Enabled is used to indicate wether or not to deploy a module
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Enabled"
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`

	// Image is the image tag for the Container
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Container Image"
	Image ImageType `json:"image,omitempty" yaml:"image,omitempty"`

	// ImagePullPolicy is the image pull policy for the image
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Container Image Pull Policy",xDescriptors="urn:alm:descriptor:com.tectonic.ui:imagePullPolicy"
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty" yaml:"imagePullPolicy,omitempty"`

	// Args is the set of arguments for the container
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Container Arguments"
	Args []string `json:"args,omitempty" yaml:"args"`

	// Envs is the set of environment variables for the container
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Container Environment vars"
	Envs []corev1.EnvVar `json:"envs,omitempty" yaml:"envs"`

	// Tolerations is the list of tolerations for the driver pods
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Tolerations"
	Tolerations []corev1.Toleration `json:"tolerations,omitempty" yaml:"tolerations"`

	// NodeSelector is a selector which must be true for the pod to fit on a node.
	// Selector which must match a node's labels for the pod to be scheduled on that node.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="NodeSelector"
	NodeSelector map[string]string `json:"nodeSelector,omitempty" yaml:"nodeSelector"`
}

//SnapshotClass struct
type SnapshotClass struct {
	// Name is the name of the Snapshot Class
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Snapshot Class Name"
	Name string `json:"name" yaml:"name"`

	// Parameters is a map of driver specific parameters for snapshot class
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Snapshot Class Parameters"
	Parameters map[string]string `json:"parameters,omitempty" yaml:"parameters"`
}

//CSIDriverSpec struct
type CSIDriverSpec struct {
	FSGroupPolicy string `json:"fSGroupPolicy,omitempty" yaml:"fSGroupPolicy,omitempty"`
}
