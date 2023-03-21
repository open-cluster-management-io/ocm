/*
Copyright 2022.

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
	work "open-cluster-management.io/api/work/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=manifestworkreplicasets,shortName=mwrs,scope=Namespaced
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Placement",type="string",JSONPath=".status.conditions[?(@.type==\"PlacementVerified\")].reason",description="Reason"
// +kubebuilder:printcolumn:name="Found",type="string",JSONPath=".status.conditions[?(@.type==\"PlacementVerified\")].status",description="Configured"
// +kubebuilder:printcolumn:name="ManifestWorks",type="string",JSONPath=".status.conditions[?(@.type==\"ManifestworkApplied\")].reason",description="Reason"
// +kubebuilder:printcolumn:name="Applied",type="string",JSONPath=".status.conditions[?(@.type==\"ManifestworkApplied\")].status",description="Applied"

// ManifestWorkReplicaSet is the Schema for the ManifestWorkReplicaSet API. This custom resource is able to apply
// ManifestWork using Placement for 0..n ManagedCluster(in their namespaces). It will also remove the ManifestWork custom resources
// when deleted. Lastly the specific ManifestWork custom resources created per ManagedCluster namespace will be adjusted based on PlacementDecision
// changes.
type ManifestWorkReplicaSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Spec reperesents the desired ManifestWork payload and Placement reference to be reconciled
	Spec ManifestWorkReplicaSetSpec `json:"spec,omitempty"`

	// Status represent the current status of Placing ManifestWork resources
	Status ManifestWorkReplicaSetStatus `json:"status,omitempty"`
}

// ManifestWorkReplicaSetSpec defines the desired state of ManifestWorkReplicaSet
type ManifestWorkReplicaSetSpec struct {
	// ManifestWorkTemplate is the ManifestWorkSpec that will be used to generate a per-cluster ManifestWork
	ManifestWorkTemplate work.ManifestWorkSpec `json:"manifestWorkTemplate"`

	// PacementRefs is a list of the names of the Placement resource, from which a PlacementDecision will be found and used
	// to distribute the ManifestWork.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +required
	PlacementRefs []LocalPlacementReference `json:"placementRefs"`
}

// ManifestWorkReplicaSetStatus defines the observed state of ManifestWorkReplicaSet
type ManifestWorkReplicaSetStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Conditions contains the different condition statuses for distrbution of ManifestWork resources
	// Valid condition types are:
	// 1. AppliedManifestWorks represents ManifestWorks have been distributed as per placement All, Partial, None, Problem
	// 2. PlacementRefValid
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Summary totals of resulting ManifestWorks
	Summary ManifestWorkReplicaSetSummary `json:"summary"`
}

// localPlacementReference is the name of a Placement resource in current namespace
type LocalPlacementReference struct {
	// Name of the Placement resource in the current namespace
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// ManifestWorkReplicaSetSummary provides reference counts of all ManifestWorks that are associated with a
// given ManifestWorkReplicaSet resource, for their respective states
type ManifestWorkReplicaSetSummary struct {
	// Total number of ManifestWorks managed by the ManifestWorkReplicaSet
	Total int `json:"total"`
	// TODO: Progressing is the number of ManifestWorks with condition Progressing: true
	Progressing int `json:"progressing"`
	// Available is the number of ManifestWorks with condition Available: true
	Available int `json:"available"`
	// TODO: Degraded is the number of ManifestWorks with condition Degraded: true
	Degraded int `json:"degraded"`
	// Applied is the number of ManifestWorks with condition Applied: true
	Applied int `json:"Applied"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
//
// ManifestWorkReplicaSetList contains a list of ManifestWorkReplicaSet
type ManifestWorkReplicaSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManifestWorkReplicaSet `json:"items"`
}

const (
	// ReasonPlacementDecisionNotFound is a reason for ManifestWorkReplicaSetConditionPlacementVerified condition type
	// representing placement decision is not found for the ManifestWorkSet
	ReasonPlacementDecisionNotFound = "PlacementDecisionNotFound"
	// ReasonPlacementDecisionEmpty is a reason for ManifestWorkReplicaSetConditionPlacementVerified condition type
	// representing the placement decision is empty for the ManifestWorkSet
	ReasonPlacementDecisionEmpty = "PlacementDecisionEmpty"
	// ReasonAsExpected is a reason for ManifestWorkReplicaSetConditionManifestworkApplied condition type representing
	// the ManifestWorkSet is applied correctly.
	ReasonAsExpected = "AsExpected"
	// ReasonProcessing is a reason for ManifestWorkReplicaSetConditionManifestworkApplied condition type representing
	// the ManifestWorkSet is under processing
	ReasonProcessing = "Processing"
	// ReasonNotAsExpected is a reason for ManifestWorkReplicaSetConditionManifestworkApplied condition type representing
	// the ManifestWorkSet is not applied correctly.
	ReasonNotAsExpected = "NotAsExpected"

	// ManifestWorkSetConditionPlacementVerified indicates if Placement is valid
	//
	// Reason: AsExpected, PlacementDecisionNotFound, PlacementDecisionEmpty or NotAsExpected
	ManifestWorkReplicaSetConditionPlacementVerified string = "PlacementVerified"

	// ManifestWorkSetConditionManifestworkApplied confirms that a ManifestWork has been created in each cluster defined by PlacementDecision
	//
	// Reason: AsExpected, NotAsExpected or Processing
	ManifestWorkReplicaSetConditionManifestworkApplied string = "ManifestworkApplied"
)
