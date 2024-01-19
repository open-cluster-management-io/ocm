package common

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	workv1 "open-cluster-management.io/api/work/v1"
)

const (
	// CloudEventsDataTypeAnnotationKey is the key of the cloudevents data type annotation.
	CloudEventsDataTypeAnnotationKey = "cloudevents.open-cluster-management.io/datatype"

	// CloudEventsGenerationAnnotationKey is the key of the manifestwork generation annotation.
	CloudEventsGenerationAnnotationKey = "cloudevents.open-cluster-management.io/generation"
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

var ManifestWorkGR = schema.GroupResource{Group: workv1.GroupName, Resource: "manifestworks"}
