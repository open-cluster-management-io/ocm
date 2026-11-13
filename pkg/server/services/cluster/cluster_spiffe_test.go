package cluster

import (
	"context"
	"testing"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clienttesting "k8s.io/client-go/testing"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/cluster"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	grpcauthn "open-cluster-management.io/sdk-go/pkg/server/grpc/authn"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestHandleStatusUpdatePinsTrustDomain(t *testing.T) {
	const key = clusterv1.SPIFFETrustDomainAnnotationKey
	spiffeCtx := func(td string) context.Context {
		return context.WithValue(context.Background(), grpcauthn.ContextOCMIdentityKey,
			grpcauthn.OCMIdentity{TrustDomain: td, ClusterName: "cluster2", Role: grpcauthn.RoleRegistration})
	}
	cluster := func(annotations map[string]string) *clusterv1.ManagedCluster {
		return &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster2", Annotations: annotations}}
	}
	event := func(action types.EventAction, c *clusterv1.ManagedCluster) *cloudevents.Event {
		evt := types.NewEventBuilder("test", types.CloudEventsType{
			CloudEventsDataType: clusterce.ManagedClusterEventDataType,
			SubResource:         types.SubResourceSpec,
			Action:              action,
		}).WithClusterName("cluster2").NewEvent()
		if err := evt.SetData(cloudevents.ApplicationJSON, c); err != nil {
			t.Fatal(err)
		}
		return &evt
	}
	written := func(t *testing.T, action clienttesting.Action) map[string]string {
		t.Helper()
		return action.(clienttesting.CreateAction).GetObject().(*clusterv1.ManagedCluster).Annotations
	}

	cases := []struct {
		name     string
		ctx      context.Context
		existing []runtime.Object
		evt      *cloudevents.Event
		verbs    []string
		want     string // "" means the annotation must be absent
	}{
		{
			name:  "SPIFFE agent creates its cluster: pinned to the SVID's trust domain",
			ctx:   spiffeCtx("east.example.org"),
			evt:   event(types.CreateRequestAction, cluster(nil)),
			verbs: []string{"create"},
			want:  "east.example.org",
		},
		{
			name:  "SPIFFE agent asks for another trust domain: the SVID's wins",
			ctx:   spiffeCtx("west.example.org"),
			evt:   event(types.CreateRequestAction, cluster(map[string]string{key: "east.example.org", "other": "kept"})),
			verbs: []string{"create"},
			want:  "west.example.org",
		},
		{
			name:  "non-SPIFFE agent may not pin",
			ctx:   context.Background(),
			evt:   event(types.CreateRequestAction, cluster(map[string]string{key: "east.example.org"})),
			verbs: []string{"create"},
		},
		{
			name:     "update keeps the pin",
			ctx:      spiffeCtx("west.example.org"),
			existing: []runtime.Object{cluster(map[string]string{key: "east.example.org"})},
			evt:      event(types.UpdateRequestAction, cluster(map[string]string{key: "west.example.org"})),
			verbs:    []string{"get", "update"},
			want:     "east.example.org",
		},
		{
			name:     "update restores a pin the agent removed",
			ctx:      spiffeCtx("east.example.org"),
			existing: []runtime.Object{cluster(map[string]string{key: "east.example.org"})},
			evt:      event(types.UpdateRequestAction, cluster(nil)),
			verbs:    []string{"get", "update"},
			want:     "east.example.org",
		},
		{
			name:     "update may not add a pin",
			ctx:      spiffeCtx("east.example.org"),
			existing: []runtime.Object{cluster(nil)},
			evt:      event(types.UpdateRequestAction, cluster(map[string]string{key: "east.example.org"})),
			verbs:    []string{"get", "update"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := clusterfake.NewSimpleClientset(c.existing...)
			informers := clusterinformers.NewSharedInformerFactory(client, 0)
			svc := NewClusterService(client, informers.Cluster().V1().ManagedClusters())
			if err := svc.HandleStatusUpdate(c.ctx, c.evt); err != nil {
				t.Fatal(err)
			}
			actions := client.Actions()
			testingcommon.AssertActions(t, actions, c.verbs...)
			got, ok := written(t, actions[len(actions)-1])[key]
			if c.want == "" {
				if ok {
					t.Errorf("annotation %s = %q, want absent", key, got)
				}
				return
			}
			if got != c.want {
				t.Errorf("annotation %s = %q, want %q", key, got, c.want)
			}
		})
	}
}
