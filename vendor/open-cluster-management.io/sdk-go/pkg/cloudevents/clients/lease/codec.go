package lease

import (
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

var LeaseEventDataType = types.CloudEventsDataType{
	Group:    coordinationv1.GroupName,
	Version:  "v1",
	Resource: "leases",
}

// LeaseCodec is a codec to encode/decode a lease/cloudevent for an agent.
type LeaseCodec struct{}

func NewLeaseCodec() *LeaseCodec {
	return &LeaseCodec{}
}

// EventDataType always returns the event data type `io.k8s.coordination.k8s.v1.leases`.
func (c *LeaseCodec) EventDataType() types.CloudEventsDataType {
	return LeaseEventDataType
}

// Encode the lease to a cloudevent
func (c *LeaseCodec) Encode(source string, eventType types.CloudEventsType, lease *coordinationv1.Lease) (*cloudevents.Event, error) {
	if eventType.CloudEventsDataType != LeaseEventDataType {
		return nil, fmt.Errorf("unsupported cloudevents data type %s", eventType.CloudEventsDataType)
	}

	evt := types.NewEventBuilder(source, eventType).
		WithResourceID(lease.Name).
		WithClusterName(lease.Namespace).
		NewEvent()

	if lease.ResourceVersion != "" {
		evt.SetExtension(types.ExtensionResourceVersion, lease.ResourceVersion)
	}

	newLease := lease.DeepCopy()
	newLease.TypeMeta = metav1.TypeMeta{
		APIVersion: coordinationv1.GroupName + "/v1",
		Kind:       "Lease",
	}

	if err := evt.SetData(cloudevents.ApplicationJSON, newLease); err != nil {
		return nil, fmt.Errorf("failed to encode lease to a cloudevent: %v", err)
	}

	return &evt, nil
}

// Decode a cloudevent to a lease object
func (c *LeaseCodec) Decode(evt *cloudevents.Event) (*coordinationv1.Lease, error) {
	lease := &coordinationv1.Lease{}
	if err := evt.DataAs(lease); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event data %s, %v", string(evt.Data()), err)
	}

	return lease, nil
}
