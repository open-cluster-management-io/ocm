package cluster

import (
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1 "open-cluster-management.io/api/cluster/v1"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

var ManagedClusterEventDataType = types.CloudEventsDataType{
	Group:    clusterv1.GroupVersion.Group,
	Version:  clusterv1.GroupVersion.Version,
	Resource: "managedclusters",
}

// ManagedClusterCodec is a codec to encode/decode a ManagedCluster/cloudevent for an agent.
type ManagedClusterCodec struct{}

func NewManagedClusterCodec() *ManagedClusterCodec {
	return &ManagedClusterCodec{}
}

// EventDataType always returns the event data type `io.open-cluster-management.cluster.v1.managedclusters`.
func (c *ManagedClusterCodec) EventDataType() types.CloudEventsDataType {
	return ManagedClusterEventDataType
}

// Encode the ManagedCluster to a cloudevent
func (c *ManagedClusterCodec) Encode(source string, eventType types.CloudEventsType, cluster *clusterv1.ManagedCluster) (*cloudevents.Event, error) {
	if eventType.CloudEventsDataType != ManagedClusterEventDataType {
		return nil, fmt.Errorf("unsupported cloudevents data type %s", eventType.CloudEventsDataType)
	}

	evt := types.NewEventBuilder(source, eventType).
		WithResourceID(cluster.Name).
		WithClusterName(cluster.Name).
		NewEvent()

	if cluster.ResourceVersion != "" {
		evt.SetExtension(types.ExtensionResourceVersion, cluster.ResourceVersion)
	}

	newCluster := cluster.DeepCopy()
	newCluster.TypeMeta = metav1.TypeMeta{
		APIVersion: clusterv1.GroupVersion.String(),
		Kind:       "ManagedCluster",
	}

	if err := evt.SetData(cloudevents.ApplicationJSON, newCluster); err != nil {
		return nil, fmt.Errorf("failed to encode managedcluster to a cloudevent: %v", err)
	}

	return &evt, nil
}

// Decode a cloudevent to a ManagedCluster
func (c *ManagedClusterCodec) Decode(evt *cloudevents.Event) (*clusterv1.ManagedCluster, error) {
	cluster := &clusterv1.ManagedCluster{}
	if err := evt.DataAs(cluster); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event data %s, %v", string(evt.Data()), err)
	}

	return cluster, nil
}
