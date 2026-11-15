package grpc

import (
	"context"
	"errors"
	"testing"

	coordv1 "k8s.io/api/coordination/v1"
	eventv1 "k8s.io/api/events/v1"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	cloudeventsaddon "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon"
	cloudeventscluster "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/cluster"
	cloudeventsevent "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/event"
	cloudeventslease "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/lease"
	cloudeventsoptions "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/options"
	grpcoptions "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc"
)

var errClient = errors.New("client failed")

// The clients are built without connecting, so an unreachable hub is enough here.
func unreachableHub() *grpcoptions.GRPCOptions {
	return &grpcoptions.GRPCOptions{Dialer: &grpcoptions.GRPCDialer{URL: "127.0.0.1:1"}}
}

func TestNewCloudEventsClients(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c, err := NewCloudEventsClients(ctx, unreachableHub(), "cluster1", true)
	if err != nil || c.ClusterClient == nil || c.ClusterInformer == nil || c.LeaseClient != nil || c.AddonClient != nil {
		t.Fatalf("cluster only: %+v, %v", c, err)
	}
	c, err = NewCloudEventsClients(ctx, unreachableHub(), "cluster1", false)
	if err != nil || c.LeaseClient == nil || c.EventsClient == nil || c.AddonClient == nil || c.AddonInformer == nil {
		t.Fatalf("all clients: %+v, %v", c, err)
	}
}

// An error from any of the client constructors is returned.
func TestNewCloudEventsClientsErrors(t *testing.T) {
	restore := func() {
		newClusterClientHolder = cloudeventscluster.NewClientHolder
		newLeaseClient = cloudeventslease.NewLeaseClient
		newEventClientHolder = cloudeventsevent.NewClientHolder
		newAddOnClient = cloudeventsaddon.ManagedClusterAddOnInterface
	}
	defer restore()
	cases := map[string]func(){
		"cluster": func() {
			newClusterClientHolder = func(context.Context, *cloudeventsoptions.GenericClientOptions[*clusterv1.ManagedCluster]) (
				*cloudeventscluster.ClientHolder, error) {
				return nil, errClient
			}
		},
		"lease": func() {
			newLeaseClient = func(context.Context, *cloudeventsoptions.GenericClientOptions[*coordv1.Lease], string) (
				*cloudeventslease.LeaseClient, error) {
				return nil, errClient
			}
		},
		"event": func() {
			newEventClientHolder = func(context.Context, *cloudeventsoptions.GenericClientOptions[*eventv1.Event]) (
				*cloudeventsevent.EventV1ClientWrapper, error) {
				return nil, errClient
			}
		},
		"add-on": func() {
			newAddOnClient = func(context.Context, *cloudeventsoptions.GenericClientOptions[*addonv1beta1.ManagedClusterAddOn]) (
				addonclient.Interface, error) {
				return nil, errClient
			}
		},
	}
	for name, fail := range cases {
		t.Run(name, func(t *testing.T) {
			restore()
			fail()
			if _, err := NewCloudEventsClients(context.Background(), unreachableHub(), "cluster1", false); !errors.Is(err, errClient) {
				t.Errorf("error %v, want the %s client's", err, name)
			}
		})
	}
}
