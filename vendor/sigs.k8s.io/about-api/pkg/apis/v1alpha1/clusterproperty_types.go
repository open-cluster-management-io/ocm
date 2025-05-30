/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//+genclient
//+genclient:nonNamespaced
//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:printcolumn:name="value",type=string,JSONPath=`.spec.value`
//+kubebuilder:printcolumn:name="age",type=date,JSONPath=`.metadata.creationTimestamp`

// ClusterProperty is a resource provides a way to store identification related,
// cluster scoped information for multi-cluster tools while creating flexibility
// for implementations.
type ClusterProperty struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec represents the desired behavior.
	// +required
	Spec ClusterPropertySpec `json:"spec"`
}

// ClusterPropertySpec defines the desired state of ClusterProperty.
type ClusterPropertySpec struct {
	// Value is the property-dependent string.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Maxlength=128000
	// +required
	Value string `json:"value"`
}

//+kubebuilder:object:root=true

// ClusterPropertyList contains a list of ClusterProperty.
type ClusterPropertyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items is the list of ClusterProperty.
	Items []ClusterProperty `json:"items"`
}
