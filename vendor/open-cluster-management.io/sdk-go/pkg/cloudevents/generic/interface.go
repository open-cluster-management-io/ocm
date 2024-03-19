package generic

import (
	"context"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubetypes "k8s.io/apimachinery/pkg/types"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// ResourceHandler handles the received resource object.
type ResourceHandler[T ResourceObject] func(action types.ResourceAction, obj T) error

// StatusHashGetter gets the status hash of one resource object.
type StatusHashGetter[T ResourceObject] func(obj T) (string, error)

type ResourceObject interface {
	// GetUID returns the resource ID of this object. The resource ID represents the unique identifier for this object.
	// The source should ensure its uniqueness and consistency.
	GetUID() kubetypes.UID

	// GetResourceVersion returns the resource version of this object. The resource version is a required int64 sequence
	// number property that must be incremented by the source whenever this resource changes.
	// The source should guarantee its incremental nature.
	GetResourceVersion() string

	// GetDeletionTimestamp returns the deletion timestamp of this object. The deletiontimestamp is an optional
	// timestamp property representing the resource is deleting from the source, the agent needs to clean up the
	// resource from its cluster.
	GetDeletionTimestamp() *metav1.Time
}

type Lister[T ResourceObject] interface {
	// List returns the list of resource objects that are maintained by source/agent.
	List(options types.ListOptions) ([]T, error)
}

type Codec[T ResourceObject] interface {
	// EventDataType indicates which type of the event data the codec is used for.
	EventDataType() types.CloudEventsDataType

	// Encode a resource object to cloudevents event.
	// Each event should have the following extensions: `resourceid`, `resourceversion` and `clustername`.
	// The source set the `deletiontimestamp` extension to indicate one resource object is deleting from a source.
	// The agent set the `originalsource` extension to indicate one resource belonged to which source.
	Encode(source string, eventType types.CloudEventsType, obj T) (*cloudevents.Event, error)

	// Decode a cloudevents event to a resource object.
	Decode(event *cloudevents.Event) (T, error)
}

type CloudEventsClient[T ResourceObject] interface {
	// Resync the resources of one source/agent by sending resync request.
	// The second parameter is used to specify cluster name/source ID for a source/agent.
	//   - A source sends the resource status resync request to a cluster with the given cluster name.
	//     If setting this parameter to `types.ClusterAll`, the source will broadcast the resync request to all clusters.
	//   - An agent sends the resources spec resync request to a source with the given source ID.
	//     If setting this parameter to `types.SourceAll`, the agent will broadcast the resync request to all sources.
	Resync(context.Context, string) error

	// Publish the resources spec/status event to the broker.
	Publish(ctx context.Context, eventType types.CloudEventsType, obj T) error

	// Subscribe the resources status/spec event to the broker to receive the resources status/spec and use
	// ResourceHandler to handle them.
	Subscribe(ctx context.Context, handlers ...ResourceHandler[T])

	// ReconnectedChan returns a chan which indicates the source/agent client is reconnected.
	// The source/agent client callers should consider sending a resync request when receiving this signal.
	ReconnectedChan() <-chan struct{}
}
