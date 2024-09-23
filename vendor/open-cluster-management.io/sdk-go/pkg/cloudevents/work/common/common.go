package common

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	workv1 "open-cluster-management.io/api/work/v1"
)

const (
	// CloudEventsDataTypeAnnotationKey is the key of the cloudevents data type annotation.
	CloudEventsDataTypeAnnotationKey = "cloudevents.open-cluster-management.io/datatype"

	// CloudEventsResourceVersionAnnotationKey is the key of the manifestwork resourceversion annotation.
	//
	// This annotation is used for tracing the ManifestWork specific changes, the value of this annotation
	// should be a sequence number representing the ManifestWork specific generation.
	CloudEventsResourceVersionAnnotationKey = "cloudevents.open-cluster-management.io/resourceversion"

	// CloudEventsSequenceIDAnnotationKey is the key of the status update event sequence ID.
	// The sequence id represents the order in which status update events occur on a single agent.
	CloudEventsSequenceIDAnnotationKey = "cloudevents.open-cluster-management.io/sequenceid"
)

// CloudEventsOriginalSourceLabelKey is the key of the cloudevents original source label.
const CloudEventsOriginalSourceLabelKey = "cloudevents.open-cluster-management.io/originalsource"

// ManifestsDeleted represents the manifests are deleted.
const ManifestsDeleted = "Deleted"

const (
	CreateRequestAction = "create_request"
	UpdateRequestAction = "update_request"
	DeleteRequestAction = "delete_request"
)

var ManifestWorkGK = schema.GroupKind{Group: workv1.GroupName, Kind: "ManifestWork"}
var ManifestWorkGR = schema.GroupResource{Group: workv1.GroupName, Resource: "manifestworks"}
