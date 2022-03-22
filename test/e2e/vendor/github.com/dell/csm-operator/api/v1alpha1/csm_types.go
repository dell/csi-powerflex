/*

Copyright (c) 2021 Dell Inc., or its subsidiaries. All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package v1alpha1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ContainerStorageModuleSpec defines the desired state of ContainerStorageModule
type ContainerStorageModuleSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Driver is a CSI Drivers for Dell Technologies
	Driver Driver `json:"driver,omitempty" yaml:"driver,omitempty"`

	// Modules is list of Container Storage Module modules you want to deploy
	Modules []Module `json:"modules,omitempty" yaml:"modules,omitempty"`
}

// ContainerStorageModuleStatus defines the observed state of ContainerStorageModule
type ContainerStorageModuleStatus struct {
	// ControllerStatus is the status of Controller pods
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="ControllerStatus",xDescriptors="urn:alm:descriptor:com.tectonic.ui:podStatuses"
	ControllerStatus PodStatus `json:"controllerStatus,omitempty"`

	// NodeStatus is the status of Controller pods
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="NodeStatus",xDescriptors="urn:alm:descriptor:com.tectonic.ui:podStatuses"
	NodeStatus PodStatus `json:"nodeStatus,omitempty"`

	// State is the state of the driver installation
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="State",xDescriptors="urn:alm:descriptor:text"
	State CSMStateType `json:"state,omitempty" yaml:"state"`
}

// +kubebuilder:validation:Optional
// +kubebuilder:resource:scope=Namespaced,shortName={"csm"}
// +kubebuilder:printcolumn:name="CreationTime",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:printcolumn:name="CSIDriverType",type=string,JSONPath=`.spec.driver.csiDriverType`,description="Type of CSIDriver"
// +kubebuilder:printcolumn:name="ConfigVersion",type=string,JSONPath=`.spec.driver.configVersion`,description="Version of CSIDriver"
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`,description="State of Installation"
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ContainerStorageModule is the Schema for the containerstoragemodules API
type ContainerStorageModule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ContainerStorageModuleSpec   `json:"spec,omitempty"`
	Status ContainerStorageModuleStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ContainerStorageModuleList contains a list of ContainerStorageModule
type ContainerStorageModuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ContainerStorageModule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ContainerStorageModule{}, &ContainerStorageModuleList{})
}

// GetCSMStatus - Returns a pointer to the driver instance
func (cr *ContainerStorageModule) GetCSMStatus() *ContainerStorageModuleStatus {
	return &cr.Status
}

// GetControllerName - Returns a controller
func (cr *ContainerStorageModule) GetControllerName() string {
	return fmt.Sprintf("%s-controller", cr.Name)
}

// GetNodeName - Returns the name of the daemonset for the driver
func (cr *ContainerStorageModule) GetNodeName() string {
	return fmt.Sprintf("%s-node", cr.Name)
}

// GetContainerStorageModuleSpec - Returns a pointer to the GetContainerStorageModuleSpec instance
func (cr *ContainerStorageModule) GetContainerStorageModuleSpec() *ContainerStorageModuleSpec {
	return &cr.Spec
}

// GetDriverType - Returns the driver type
func (cr *ContainerStorageModule) GetDriverType() DriverType {
	return cr.Spec.Driver.CSIDriverType
}

// IsBeingDeleted  - Returns  true if a deletion timestamp is set
func (cr *ContainerStorageModule) IsBeingDeleted() bool {
	return !cr.ObjectMeta.DeletionTimestamp.IsZero()
}

// HasFinalizer returns true if the item has the specified finalizer
func (cr *ContainerStorageModule) HasFinalizer(finalizerName string) bool {
	for _, item := range cr.ObjectMeta.Finalizers {
		if item == finalizerName {
			return true
		}
	}
	return false
}
