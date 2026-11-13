package util

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/cloudevents/sdk-go/v2/binding"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/cluster"
	leasece "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/lease"
	grpcoptions "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc"
	pbv1 "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protobuf/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protocol"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// SPIFFEAgent is a minimal registration agent for integration tests: it has nothing but an SVID
// from a Workload API, and talks CloudEvents to the hub's SPIFFE listener.
type SPIFFEAgent struct {
	Cluster string
	conn    *grpc.ClientConn
	client  pbv1.CloudEventServiceClient
	source  io.Closer
}

// NewSPIFFEAgent dials the hub's SPIFFE listener at addr as agentID, an SVID served by the
// Workload API at socket. The agent acts for cluster.
func NewSPIFFEAgent(ctx context.Context, addr, hubID, agentID, socket, cluster string) (*SPIFFEAgent, error) {
	opts, src, err := grpcoptions.NewSPIFFEGRPCOptions(ctx, grpcoptions.SPIFFEConfig{
		URL: addr, HubSPIFFEID: hubID, AgentSPIFFEID: agentID, WorkloadAPISocket: socket,
	})
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(credentials.NewTLS(opts.Dialer.TLSConfig)))
	if err != nil {
		_ = src.Close()
		return nil, err
	}
	return &SPIFFEAgent{Cluster: cluster, conn: conn, client: pbv1.NewCloudEventServiceClient(conn), source: src}, nil
}

// Close closes the connection and the Workload API source.
func (a *SPIFFEAgent) Close() {
	_ = a.conn.Close()
	_ = a.source.Close()
}

func (a *SPIFFEAgent) publish(ctx context.Context, evt *cloudevents.Event) error {
	pbEvt := &pbv1.CloudEvent{}
	if err := protocol.WritePBMessage(ctx, (*binding.EventMessage)(evt), pbEvt); err != nil {
		return err
	}
	_, err := a.client.Publish(ctx, &pbv1.PublishRequest{Event: pbEvt})
	return err
}

// CreateCluster asks the hub to create ManagedCluster name, unaccepted, as a registration agent
// does on its first connection. It may name another cluster than the agent's, to test refusal.
func (a *SPIFFEAgent) CreateCluster(ctx context.Context, name string, annotations map[string]string) error {
	evt, err := clusterce.NewManagedClusterCodec().Encode(a.Cluster, types.CloudEventsType{
		CloudEventsDataType: clusterce.ManagedClusterEventDataType,
		SubResource:         types.SubResourceSpec,
		Action:              types.CreateRequestAction,
	}, &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Annotations: annotations},
		Spec:       clusterv1.ManagedClusterSpec{HubAcceptsClient: false},
	})
	if err != nil {
		return err
	}
	return a.publish(ctx, evt)
}

// UpdateLease renews lease, as the registration agent does once its cluster is accepted.
func (a *SPIFFEAgent) UpdateLease(ctx context.Context, lease *coordinationv1.Lease) error {
	lease = lease.DeepCopy()
	now := metav1.NewMicroTime(time.Now())
	lease.Spec.RenewTime = &now
	evt, err := leasece.NewLeaseCodec().Encode(a.Cluster, types.CloudEventsType{
		CloudEventsDataType: leasece.LeaseEventDataType,
		SubResource:         types.SubResourceSpec,
		Action:              types.UpdateRequestAction,
	}, lease)
	if err != nil {
		return err
	}
	return a.publish(ctx, evt)
}

// WatchCluster subscribes to the agent's ManagedCluster and returns a channel that gets the
// error that ends the stream.
func (a *SPIFFEAgent) WatchCluster(ctx context.Context) (<-chan error, error) {
	stream, err := a.client.Subscribe(ctx, &pbv1.SubscriptionRequest{
		Source:      types.SourceAll,
		ClusterName: a.Cluster,
		DataType:    clusterce.ManagedClusterEventDataType.String(),
	})
	if err != nil {
		return nil, err
	}
	if _, err := stream.Header(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() {
		for {
			if _, err := stream.Recv(); err != nil {
				done <- err
				return
			}
		}
	}()
	return done, nil
}

// FreeLocalPort returns a TCP port on 127.0.0.1 that nothing listens on right now.
func FreeLocalPort() (string, error) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer lis.Close()
	return fmt.Sprintf("%d", lis.Addr().(*net.TCPAddr).Port), nil
}
