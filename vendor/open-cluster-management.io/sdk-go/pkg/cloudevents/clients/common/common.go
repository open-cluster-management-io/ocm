package common

import (
	certificatev1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
)

const (
	// CloudEventsDataTypeAnnotationKey is the key of the cloudevents data type annotation.
	CloudEventsDataTypeAnnotationKey = "cloudevents.open-cluster-management.io/datatype"

	// CloudEventsResourceVersionAnnotationKey is the key of the resource resourceversion annotation.
	//
	// This annotation is used for tracing a resource specific changes, the value of this annotation
	// should be a sequence number representing the resource specific generation.
	CloudEventsResourceVersionAnnotationKey = "cloudevents.open-cluster-management.io/resourceversion"

	// CloudEventsSequenceIDAnnotationKey is the key of the status update event sequence ID.
	// The sequence id represents the order in which status update events occur on a single agent.
	CloudEventsSequenceIDAnnotationKey = "cloudevents.open-cluster-management.io/sequenceid"
)

// CloudEventsOriginalSourceLabelKey is the key of the cloudevents original source label.
const CloudEventsOriginalSourceLabelKey = "cloudevents.open-cluster-management.io/originalsource"

const (
	CreateRequestAction = "create_request"
	UpdateRequestAction = "update_request"
	DeleteRequestAction = "delete_request"
)

// ResourceDeleted represents a resource is deleted.
const ResourceDeleted = "Deleted"

const ResourceFinalizer = "cloudevents.open-cluster-management.io/resource-cleanup"

var ManagedClusterGK = schema.GroupKind{Group: clusterv1.GroupName, Kind: "ManagedCluster"}
var ManagedClusterGR = schema.GroupResource{Group: clusterv1.GroupName, Resource: "managedclusters"}

var ManifestWorkGK = schema.GroupKind{Group: workv1.GroupName, Kind: "ManifestWork"}
var ManifestWorkGR = schema.GroupResource{Group: workv1.GroupName, Resource: "manifestworks"}

var CSRGK = schema.GroupKind{Group: certificatev1.GroupName, Kind: "CertificateSigningRequest"}
var CSRGR = schema.GroupResource{Group: certificatev1.GroupName, Resource: "certificatesigningrequests"}

var ManagedClusterAddOnGK = schema.GroupKind{Group: addonapiv1alpha1.GroupName, Kind: "ManagedClusterAddOn"}
var ManagedClusterAddOnGR = schema.GroupResource{Group: addonapiv1alpha1.GroupName, Resource: "managedclusteraddons"}
