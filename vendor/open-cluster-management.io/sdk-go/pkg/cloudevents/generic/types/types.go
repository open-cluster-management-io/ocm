package types

import (
	"fmt"
	"strings"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/google/uuid"
)

// HeartbeatCloudEventsType indicates the type of heartbeat cloud events.
const HeartbeatCloudEventsType = "io.open-cluster-management.cloudevents.heartbeat"

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

	// ResyncResponseAction represents the cloud event is for the resync response.
	ResyncResponseAction EventAction = "resync_response"

	// CreateRequestAction represents the cloud event is for resource create.
	CreateRequestAction EventAction = "create_request"

	// UpdateRequestAction represents the cloud event is for resource update.
	UpdateRequestAction EventAction = "update_request"

	// DeleteRequestAction represents the cloud event is for resource delete.
	DeleteRequestAction EventAction = "delete_request"

	// WatchRequestAction represents the cloud event is for resource watch.
	WatchRequestAction EventAction = "watch_request"
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

	// ExtensionWorkMeta is an extension attribute for work meta data.
	ExtensionWorkMeta = "metadata"
)

const (
	MQTTEventsTopicPattern          = `^(\$share/[a-z0-9-]+/)?([a-z]+)/([a-z0-9-]+|\+)/([a-z]+)/([a-z0-9-]+|\+)/(sourceevents|agentevents)$`
	MQTTSourceEventsTopicPattern    = `^(\$share/[a-z0-9-]+/)?([a-z]+)/([a-z0-9-]+|\+)/([a-z]+)/([a-z0-9-]+|\+)/sourceevents$`
	MQTTAgentEventsTopicPattern     = `^(\$share/[a-z0-9-]+/)?([a-z]+)/([a-z0-9-]+|\+)/([a-z]+)/([a-z0-9-]+|\+)/agentevents$`
	MQTTSourceBroadcastTopicPattern = `^(\$share/[a-z0-9-]+/)?([a-z]+)/([a-z0-9-]+|\+)/sourcebroadcast$`
	MQTTAgentBroadcastTopicPattern  = `^(\$share/[a-z0-9-]+/)?([a-z]+)/([a-z0-9-]+|\+)/agentbroadcast$`
	PubSubTopicPattern              = `^projects/PROJECT_ID/topics/(sourceevents|agentevents|sourcebroadcast|agentbroadcast)$`
	PubSubSubscriptionPattern       = `^projects/PROJECT_ID/subscriptions/(sourceevents-[a-z0-9-]+|agentevents-[a-z0-9-]+|sourcebroadcast-[a-z0-9-]+|agentbroadcast-[a-z0-9-]+)$`
)

// Topics represents required messaging system topics for a source or agent.
type Topics struct {
	// SourceEvents topic is a topic for sources to publish their resource create/update/delete events or status resync events
	//   - A source uses this topic to publish its resource create/update/delete request or status resync request with
	//     its sourceID to a specified agent
	//   - An agent subscribes to this topic with its cluster name to response sources resource create/update/delete
	//     request or status resync request
	// For MQTT: The topic format is `^(\$share/[a-z0-9-]+/)?([a-z]+)/([a-z0-9-]+|\+)/([a-z]+)/([a-z0-9-]+|\+)/sourceevents$`, e.g.
	// sources/+/clusters/+/sourceevents, sources/source1/clusters/+/sourceevents, sources/source1/clusters/cluster1/sourceevents
	// or $share/source-group/sources/+/clusters/+/sourceevents
	// For Pub/Sub: The topic should be pre-created with the format `projects/{projectID}/topics/sourceevents`,
	// e.g., `projects/my-project/topics/sourceevents`.
	SourceEvents string `json:"sourceEvents" yaml:"sourceEvents"`

	// AgentEvents topic is a topic for agents to publish their resource status update events or spec resync events
	//   - An agent using this topic to publish the resource status update request or spec resync request with its
	//     cluster name to a specified source.
	//   - A source subscribe to this topic with its sourceID to response agents resource status update request or spec
	//     resync request
	// For MQTT: The topic format is `^(\$share/[a-z0-9-]+/)?([a-z]+)/([a-z0-9-]+|\+)/([a-z]+)/([a-z0-9-]+|\+)/agentevents$`, e.g.
	// sources/+/clusters/+/agentevents, sources/source1/clusters/+/agentevents, sources/source1/clusters/cluster1/agentevents
	// or $share/agent-group/+/clusters/+/agentevents
	// For Pub/Sub: The topic should be pre-created with the format `projects/{projectID}/topics/agentevents`,
	// e.g. projects/my-project/topics/agentevents.
	AgentEvents string `json:"agentEvents" yaml:"agentEvents"`

	// SourceBroadcast is a topic for a source to publish its events to all agents, currently, we use
	// this topic to resync resource status from all agents for a source that does not known the exact agents, e.g.
	//   - A source uses this topic to publish its resource status resync request with its sourceID to all the agents
	//   - Each agent subscribes to this topic to response sources resource status resync request
	// For MQTT: The topic is optional and the format is `^(\$share/[a-z0-9-]+/)?([a-z]+)/([a-z0-9-]+|\+)/sourcebroadcast$`, e.g.
	// sources/+/sourcebroadcast, sources/source1/sourcebroadcast or $share/source-group/sources/+/sourcebroadcast
	// For Pub/Sub: The topic should be pre-created with the format `projects/{projectID}/topics/sourcebroadcast`,
	// e.g. projects/my-project/topics/sourcebroadcast.
	SourceBroadcast string `json:"sourceBroadcast,omitempty" yaml:"sourceBroadcast,omitempty"`

	// AgentBroadcast is a topic for a agent to publish its events to all sources, currently, we use
	// this topic to resync resources from all sources for an agent that does not known the exact sources, e.g.
	//   - An agent using this topic to publish the spec resync request with its cluster name to all the sources.
	//   - Each source subscribe to this topic to response agents spec resync request
	// For MQTT: The topic is optional and the format is `^(\$share/[a-z0-9-]+/)?([a-z]+)/([a-z0-9-]+|\+)/agentbroadcast$`, e.g.
	// clusters/+/agentbroadcast, clusters/cluster1/agentbroadcast or $share/agent-group/clusters/+/agentbroadcast
	// For Pub/Sub: The topic should be pre-created with the format `projects/{projectID}/topics/agentbroadcast`,
	// e.g. projects/my-project/topics/agentbroadcast.
	AgentBroadcast string `json:"agentBroadcast,omitempty" yaml:"agentBroadcast,omitempty"`
}

// Subscriptions represents required messaging system subscriptions for a source or agent.
// Not all messaging systems use explicit subscriptions, e.g., MQTT uses topic subscriptions directly.
// But for systems like Google Pub/Sub, subscriptions are necessary to receive messages from topics.
type Subscriptions struct {
	// SourceEvents subscription is a subscription for a agent to receive resource create/update/delete events or status resync events
	//   - A source publishes to the topic attched by this subscription to send its resource create/update/delete request or status resync request
	//     to a specified agent with cluster name
	//   - An agent uses this subscription to receive resource create/update/delete request or status resync request from sources
	//     with by filtering its cluster name
	// For Pub/Sub: The subscription should be pre-created with the format `projects/{projectID}/subscriptions/sourceevents-{clusterName}` and filter `attr."ce-clustername"="{clusterName}"`,
	// e.g. projects/my-project/subscriptions/sourceevents-cluster1 with filter `attr."ce-clustername"="cluster1"`.
	SourceEvents string `json:"sourceEvents" yaml:"sourceEvents"`

	// AgentEvents subscription is a subscription for a source to receive resource status update events or spec resync events
	//   - An agent publishes to the topic attched by this subscription to send its resource status update request or spec resync request
	//     to a specified source with sourceID
	//   - A source uses this subscription to receive resource status update request or spec resync request from agents
	//     by filtering its sourceID
	// For Pub/Sub: The subscription should be pre-created with the format `projects/{projectID}/subscriptions/agentevents-{sourceID}` and filter `attr."ce-originalsource"="{sourceID}"`,
	// e.g. projects/my-project/subscriptions/agentevents-source1 with filter `attr."ce-originalsource"="source1"`.
	AgentEvents string `json:"agentEvents" yaml:"agentEvents"`
	// SourceBroadcast subscription is a subscription for an agent to receive status resync events from all sources.
	//   - A source publishes to the topic attached by this subscription to send its status resync request to all agents
	//   - Each agent uses this subscription to receive status resync request from sources
	// For Pub/Sub: The subscription should be pre-created with the format `projects/{projectID}/subscriptions/sourcebroadcast-{clusterName}`,
	// e.g. projects/my-project/subscriptions/sourcebroadcast-cluster1.
	SourceBroadcast string `json:"sourceBroadcast" yaml:"sourceBroadcast"`
	// AgentBroadcast subscription is a subscription for a source to receive spec resync events from all agents.
	//   - An agent publishes to the topic attached by this subscription to send its spec resync request to all sources
	//   - Each source uses this subscription to receive spec resync request from agents
	// For Pub/Sub: The subscription should be pre-created with the format `projects/{projectID}/subscriptions/agentbroadcast-{sourceID}`,
	// e.g. projects/my-project/subscriptions/agentbroadcast-source1.
	AgentBroadcast string `json:"agentBroadcast" yaml:"agentBroadcast"`
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
