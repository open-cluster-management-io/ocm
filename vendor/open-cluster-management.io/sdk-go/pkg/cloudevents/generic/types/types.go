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

	// ExtensionStatusHash is the cloud event extension key of the status hash.
	ExtensionStatusHash = "statushash"
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

const (
	EventsTopicPattern          = `^(\$share/[a-z0-9-]+/)?([a-z]+)/([a-z0-9-]+|\+)/([a-z]+)/([a-z0-9-]+|\+)/(sourceevents|agentevents)$`
	SourceEventsTopicPattern    = `^(\$share/[a-z0-9-]+/)?([a-z]+)/([a-z0-9-]+|\+)/([a-z]+)/([a-z0-9-]+|\+)/sourceevents$`
	AgentEventsTopicPattern     = `^(\$share/[a-z0-9-]+/)?([a-z]+)/([a-z0-9-]+|\+)/([a-z]+)/([a-z0-9-]+|\+)/agentevents$`
	SourceBroadcastTopicPattern = `^(\$share/[a-z0-9-]+/)?([a-z]+)/([a-z0-9-]+|\+)/sourcebroadcast$`
	AgentBroadcastTopicPattern  = `^(\$share/[a-z0-9-]+/)?([a-z]+)/([a-z0-9-]+|\+)/agentbroadcast$`
)

// Topics represents required messaging system topics for a source or agent.
type Topics struct {
	// SourceEvents topic is a topic for sources to publish their resource create/update/delete events or status resync events
	//   - A source uses this topic to publish its resource create/update/delete request or status resync request with
	//     its sourceID to a specified agent
	//   - An agent subscribes to this topic with its cluster name to response sources resource create/update/delete
	//     request or status resync request
	// The topic format is `^(\$share/[a-z0-9-]+/)?([a-z]+)/([a-z0-9-]+|\+)/([a-z]+)/([a-z0-9-]+|\+)/sourceevents$`, e.g.
	// sources/+/clusters/+/sourceevents, sources/source1/clusters/+/sourceevents, sources/source1/clusters/cluster1/sourceevents
	// or $share/source-group/sources/+/clusters/+/sourceevents
	SourceEvents string `json:"sourceEvents" yaml:"sourceEvents"`

	// AgentEvents topic is a topic for agents to publish their resource status update events or spec resync events
	//   - An agent using this topic to publish the resource status update request or spec resync request with its
	//     cluster name to a specified source.
	//   - A source subscribe to this topic with its sourceID to response agents resource status update request or spec
	//     resync request
	// The topic format is `^(\$share/[a-z0-9-]+/)?([a-z]+)/([a-z0-9-]+|\+)/([a-z]+)/([a-z0-9-]+|\+)/agentevents$`, e.g.
	// sources/+/clusters/+/agentevents, sources/source1/clusters/+/agentevents, sources/source1/clusters/cluster1/agentevents
	// or $share/agent-group/+/clusters/+/agentevents
	AgentEvents string `json:"agentEvents" yaml:"agentEvents"`

	// SourceBroadcast is an optional topic, it is for a source to publish its events to all agents, currently, we use
	// this topic to resync resource status from all agents for a source that does not known the exact agents, e.g.
	//   - A source uses this topic to publish its resource status resync request with its sourceID to all the agents
	//   - Each agent subscribes to this topic to response sources resource status resync request
	// The topic format is `^(\$share/[a-z0-9-]+/)?([a-z]+)/([a-z0-9-]+|\+)/sourcebroadcast$`, e.g.
	// sources/+/sourcebroadcast, sources/source1/sourcebroadcast or $share/source-group/sources/+/sourcebroadcast
	SourceBroadcast string `json:"sourceBroadcast,omitempty" yaml:"sourceBroadcast,omitempty"`

	// AgentBroadcast is an optional topic, it is for a agent to publish its events to all sources, currently, we use
	// this topic to resync resources from all sources for an agent that does not known the exact sources, e.g.
	//   - An agent using this topic to publish the spec resync request with its cluster name to all the sources.
	//   - Each source subscribe to this topic to response agents spec resync request
	// The topic format is `^(\$share/[a-z0-9-]+/)?([a-z]+)/([a-z0-9-]+|\+)/agentbroadcast$`, e.g.
	// clusters/+/agentbroadcast, clusters/cluster1/agentbroadcast or $share/agent-group/clusters/+/agentbroadcast
	AgentBroadcast string `json:"agentBroadcast,omitempty" yaml:"agentBroadcast,omitempty"`
}

// ListOptions is the query options for listing the resource objects from the source/agent.
type ListOptions struct {
	// Source use the cluster name to restrict the list of returned objects by their cluster name.
	// Defaults to all clusters.
	ClusterName string

	// Agent use the source ID to restrict the list of returned objects by their source ID.
	// Defaults to all sources.
	Source string

	// CloudEventsDataType indicates the resource related cloud events data type.
	CloudEventsDataType CloudEventsDataType
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
		return nil, fmt.Errorf("unsupported cloudevents type format: %s", cloudEventsType)
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

	evt.SetExtension(ExtensionClusterName, b.clusterName)
	evt.SetExtension(ExtensionOriginalSource, b.originalSource)

	if len(b.resourceID) != 0 {
		evt.SetExtension(ExtensionResourceID, b.resourceID)
	}

	if b.resourceVersion != nil {
		evt.SetExtension(ExtensionResourceVersion, *b.resourceVersion)
	}

	if len(b.sequenceID) != 0 {
		evt.SetExtension(ExtensionStatusUpdateSequenceID, b.sequenceID)
	}

	if !b.deletionTimestamp.IsZero() {
		evt.SetExtension(ExtensionDeletionTimestamp, b.deletionTimestamp)
	}

	return evt
}
