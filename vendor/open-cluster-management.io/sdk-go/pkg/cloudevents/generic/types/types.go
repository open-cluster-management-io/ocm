package types

import (
	"fmt"
	"strings"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/google/uuid"
)

const (
	// ClusterAll is the default argument to specify on a context when you want to list or filter resources across all
	// managed clusters.
	ClusterAll = ""

	// SourceAll is the default argument to specify on a context when you want to list or filter resources across all
	// sources.
	SourceAll = ""
)

// EventSubResource describes the subresource of a cloud event. Only `spec` and `status` are supported.
type EventSubResource string

const (
	// SubResourceSpec represents the cloud event data is from the resource spec.
	SubResourceSpec EventSubResource = "spec"

	// SubResourceSpec represents the cloud event data is from the resource status.
	SubResourceStatus EventSubResource = "status"
)

// EventAction describes the expected action of a cloud event.
type EventAction string

const (
	// ResyncRequestAction represents the cloud event is for the resync request.
	ResyncRequestAction EventAction = "resync_request"

	// ResyncRequestAction represents the cloud event is for the resync response.
	ResyncResponseAction EventAction = "resync_response"
)

const (
	// ExtensionResourceID is the cloud event extension key of the resource ID.
	ExtensionResourceID = "resourceid"

	// ExtensionResourceVersion is the cloud event extension key of the resource version.
	ExtensionResourceVersion = "resourceversion"

	// ExtensionStatusUpdateSequenceID is the cloud event extension key of the status update event sequence ID.
	// The status update event sequence id represents the order in which status update events occur on a single agent.
	ExtensionStatusUpdateSequenceID = "sequenceid"

	// ExtensionDeletionTimestamp is the cloud event extension key of the deletion timestamp.
	ExtensionDeletionTimestamp = "deletiontimestamp"

	// ExtensionClusterName is the cloud event extension key of the cluster name.
	ExtensionClusterName = "clustername"

	// ExtensionOriginalSource is the cloud event extension key of the original source.
	ExtensionOriginalSource = "originalsource"

	// ExtensionResourceMeta is the cloud event extension key of the original resource meta.
	ExtensionResourceMeta = "resourcemeta"
)

// ResourceAction represents an action on a resource object on the source or agent.
type ResourceAction string

const (
	// Added represents a resource is added on the source part.
	Added ResourceAction = "ADDED"

	// Modified represents a resource is modified on the source part.
	Modified ResourceAction = "MODIFIED"

	// StatusModified represents the status of a resource is modified on the agent part.
	StatusModified ResourceAction = "STATUSMODIFIED"

	// Deleted represents a resource is deleted from the source prat.
	Deleted ResourceAction = "DELETED"
)

// Topics represents required messaging system topics for a source or agent.
type Topics struct {
	// Spec is a topic for resource spec
	Spec string `json:"spec" yaml:"spec"`
	// Status is a  topic for resource status
	Status string `json:"status" yaml:"status"`
	// SpecResync is a topic for resource spec resync
	SpecResync string `json:"specResync" yaml:"specResync"`
	// StatusResync is a MQTT topic for resource status resync
	StatusResync string `json:"statusResync" yaml:"statusResync"`
}

// ListOptions is the query options for listing the resource objects from the source/agent.
type ListOptions struct {
	// Source use the cluster name to restrict the list of returned objects by their cluster name.
	// Defaults to all clusters.
	ClusterName string

	// Agent use the source ID to restrict the list of returned objects by their source ID.
	// Defaults to all sources.
	Source string
}

// ResourceMeta represents a resource original meta data on the source
type ResourceMeta struct {
	Group     string `json:"group"`
	Version   string `json:"version"`
	Resource  string `json:"resource"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// CloudEventsDataType uniquely identifies the type of cloud event data.
type CloudEventsDataType struct {
	Group    string
	Version  string
	Resource string
}

func (t CloudEventsDataType) String() string {
	return fmt.Sprintf("%s.%s.%s", t.Group, t.Version, t.Resource)
}

// CloudEventsType represents the type of cloud events, which describes the type of cloud event data.
type CloudEventsType struct {
	// CloudEventsDataType uniquely identifies the type of cloud event data.
	CloudEventsDataType

	// SubResource represents the cloud event data is from the resource spec or status.
	SubResource EventSubResource

	// Action represents the expected action for this cloud event.
	Action EventAction
}

func (t CloudEventsType) String() string {
	return fmt.Sprintf("%s.%s.%s.%s.%s", t.Group, t.Version, t.Resource, t.SubResource, t.Action)
}

// ParseCloudEventsDataType parse  the cloud event data type to a struct object.
// The type format is `<reverse-group-of-resource>.<resource-version>.<resource-name>`.
func ParseCloudEventsDataType(cloudEventsDataType string) (*CloudEventsDataType, error) {
	types := strings.Split(cloudEventsDataType, ".")
	length := len(types)
	if length < 3 {
		return nil, fmt.Errorf("unsupported cloudevents data type format")
	}
	return &CloudEventsDataType{
		Group:    strings.Join(types[0:length-2], "."),
		Version:  types[length-2],
		Resource: types[length-1],
	}, nil
}

// ParseCloudEventsType parse the cloud event type to a struct object.
// The type format is `<reverse-group-of-resource>.<resource-version>.<resource-name>.<subresource>.<action>`.
// The `<subresource>` must be one of "spec" and "status".
func ParseCloudEventsType(cloudEventsType string) (*CloudEventsType, error) {
	types := strings.Split(cloudEventsType, ".")
	length := len(types)
	if length < 5 {
		return nil, fmt.Errorf("unsupported cloudevents type format")
	}

	subResource := EventSubResource(types[length-2])
	if subResource != SubResourceSpec && subResource != SubResourceStatus {
		return nil, fmt.Errorf("unsupported subresource %s", subResource)
	}

	return &CloudEventsType{
		CloudEventsDataType: CloudEventsDataType{
			Group:    strings.Join(types[0:length-4], "."),
			Version:  types[length-4],
			Resource: types[length-3],
		},
		SubResource: subResource,
		Action:      EventAction(types[length-1]),
	}, nil
}

type EventBuilder struct {
	source            string
	clusterName       string
	originalSource    string
	resourceID        string
	sequenceID        string
	resourceVersion   *int64
	eventType         CloudEventsType
	deletionTimestamp time.Time
}

func NewEventBuilder(source string, eventType CloudEventsType) *EventBuilder {
	return &EventBuilder{
		source:    source,
		eventType: eventType,
	}
}

func (b *EventBuilder) WithResourceID(resourceID string) *EventBuilder {
	b.resourceID = resourceID
	return b
}

func (b *EventBuilder) WithResourceVersion(resourceVersion int64) *EventBuilder {
	b.resourceVersion = &resourceVersion
	return b
}

func (b *EventBuilder) WithStatusUpdateSequenceID(sequenceID string) *EventBuilder {
	b.sequenceID = sequenceID
	return b
}

func (b *EventBuilder) WithClusterName(clusterName string) *EventBuilder {
	b.clusterName = clusterName
	return b
}

func (b *EventBuilder) WithOriginalSource(originalSource string) *EventBuilder {
	b.originalSource = originalSource
	return b
}

func (b *EventBuilder) WithDeletionTimestamp(timestamp time.Time) *EventBuilder {
	b.deletionTimestamp = timestamp
	return b
}

func (b *EventBuilder) NewEvent() cloudevents.Event {
	evt := cloudevents.NewEvent()
	evt.SetID(uuid.New().String())
	evt.SetType(b.eventType.String())
	evt.SetTime(time.Now())
	evt.SetSource(b.source)

	if len(b.resourceID) != 0 {
		evt.SetExtension(ExtensionResourceID, b.resourceID)
	}

	if b.resourceVersion != nil {
		evt.SetExtension(ExtensionResourceVersion, *b.resourceVersion)
	}

	if len(b.sequenceID) != 0 {
		evt.SetExtension(ExtensionStatusUpdateSequenceID, b.sequenceID)
	}

	if len(b.clusterName) != 0 {
		evt.SetExtension(ExtensionClusterName, b.clusterName)
	}

	if len(b.originalSource) != 0 {
		evt.SetExtension(ExtensionOriginalSource, b.originalSource)
	}

	if !b.deletionTimestamp.IsZero() {
		evt.SetExtension(ExtensionDeletionTimestamp, b.deletionTimestamp)
	}

	return evt
}
