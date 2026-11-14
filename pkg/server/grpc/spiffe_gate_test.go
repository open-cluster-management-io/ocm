package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	addonlisterv1beta1 "open-cluster-management.io/api/client/addon/listers/addon/v1beta1"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	addonce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon/v1beta1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/cluster"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/event"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/lease"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/payload"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	grpcauthn "open-cluster-management.io/sdk-go/pkg/server/grpc/authn"
	"open-cluster-management.io/sdk-go/pkg/server/grpc/authz"
)

type countingAuthorizer struct{ calls int }

func (a *countingAuthorizer) AuthorizeRequest(ctx context.Context, _ any) (authz.Decision, context.Context, error) {
	a.calls++
	return authz.DecisionAllow, ctx, nil
}

func (a *countingAuthorizer) AuthorizeStream(_ context.Context, ss grpc.ServerStream, _ *grpc.StreamServerInfo) (authz.Decision, grpc.ServerStream, error) {
	a.calls++
	return authz.DecisionAllow, ss, nil
}

type ctxStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *ctxStream) Context() context.Context { return s.ctx }

func addOnLister(t *testing.T, addOns ...*addonv1beta1.ManagedClusterAddOn) addonlisterv1beta1.ManagedClusterAddOnLister {
	t.Helper()
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	for _, a := range addOns {
		require.NoError(t, indexer.Add(a))
	}
	return addonlisterv1beta1.NewManagedClusterAddOnLister(indexer)
}

func clusterLister(t *testing.T, clusters ...*clusterv1.ManagedCluster) clusterlisterv1.ManagedClusterLister {
	t.Helper()
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, c := range clusters {
		require.NoError(t, indexer.Add(c))
	}
	return clusterlisterv1.NewManagedClusterLister(indexer)
}

func pinnedCluster(name, td string) *clusterv1.ManagedCluster {
	c := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: clusterv1.ManagedClusterSpec{HubAcceptsClient: true}}
	if td != "" {
		c.Annotations = map[string]string{clusterv1.SPIFFETrustDomainAnnotationKey: td}
	}
	return c
}

func TestClusterGate(t *testing.T) {
	agent := func(cluster, td string) context.Context {
		return context.WithValue(context.Background(), grpcauthn.ContextOCMIdentityKey,
			grpcauthn.OCMIdentity{ClusterName: cluster, TrustDomain: td, Role: grpcauthn.RoleWork})
	}
	addonNamed := func(cluster, td, name string) context.Context {
		return context.WithValue(context.Background(), grpcauthn.ContextOCMIdentityKey,
			grpcauthn.OCMIdentity{ClusterName: cluster, TrustDomain: td, AddOnName: name, AgentName: "a"})
	}
	addon := func(cluster, td string) context.Context { return addonNamed(cluster, td, "my-addon") }
	now := metav1.Now()
	deleting := pinnedCluster("deleting", "ocm-hub.example.org")
	deleting.DeletionTimestamp = &now
	addOns := addOnLister(t,
		&addonv1beta1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{Namespace: "cluster2", Name: "my-addon"}},
		&addonv1beta1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{Namespace: "cluster2", Name: "going-addon",
			DeletionTimestamp: &now, Finalizers: []string{"x"}}},
	)
	denied := pinnedCluster("denied", "ocm-hub.example.org")
	denied.Spec.HubAcceptsClient = false
	registration := func(cluster, td string, role grpcauthn.AgentRole) context.Context {
		return context.WithValue(context.Background(), grpcauthn.ContextOCMIdentityKey,
			grpcauthn.OCMIdentity{ClusterName: cluster, TrustDomain: td, Role: role})
	}
	lister := clusterLister(t,
		denied,
		pinnedCluster("cluster1", "ocm-hub.example.org"),
		pinnedCluster("cluster2", "east.example.org"),
		pinnedCluster("cluster3", "west.example.org"),
		pinnedCluster("legacy", ""),
		deleting,
	)
	several := []string{"ocm-hub.example.org", "east.example.org", "west.example.org"}

	cases := []struct {
		name    string
		ctx     context.Context
		tds     []string
		wantErr string
	}{
		{"cluster2 from east", agent("cluster2", "east.example.org"), several, ""},
		{"cluster2's add-on from east", addon("cluster2", "east.example.org"), several, ""},
		{"cluster2 from west", agent("cluster2", "west.example.org"), several,
			`cluster "cluster2" is pinned to trust domain "east.example.org", not "west.example.org"`},
		{"cluster2's add-on from west", addon("cluster2", "west.example.org"), several, "is pinned to trust domain"},
		{"cluster3 from west", agent("cluster3", "west.example.org"), several, ""},
		{"no cluster yet: may create", agent("cluster4", "west.example.org"), several, ""},
		{"unpinned, one trust domain", agent("legacy", "ocm-hub.example.org"), []string{"ocm-hub.example.org"}, ""},
		{"unpinned, several trust domains", agent("legacy", "ocm-hub.example.org"), several, "is not pinned to a trust domain"},
		{"no SPIFFE identity", context.Background(), several, "no SPIFFE identity"},
		{"cluster being deleted: work agent may clean up", agent("deleting", "ocm-hub.example.org"), several, ""},
		{"cluster being deleted: singleton agent may clean up",
			registration("deleting", "ocm-hub.example.org", grpcauthn.RoleKlusterlet), several, ""},
		{"cluster being deleted: registration agent",
			registration("deleting", "ocm-hub.example.org", grpcauthn.RoleRegistration), several,
			"cluster deleting is being deleted"},
		{"cluster being deleted: add-on agent", addon("deleting", "ocm-hub.example.org"), several,
			"cluster deleting is being deleted"},
		{"cluster being deleted: work agent from another trust domain", agent("deleting", "east.example.org"), several,
			"is pinned to trust domain"},
		{"add-on not enabled", addonNamed("cluster2", "east.example.org", "other-addon"), several,
			"add-on other-addon is not enabled on cluster cluster2"},
		{"denied cluster: work agent", agent("denied", "ocm-hub.example.org"), several, "cluster denied is not accepted by the hub"},
		{"denied cluster: add-on agent", addon("denied", "ocm-hub.example.org"), several, "is not accepted by the hub"},
		{"denied cluster: registration agent keeps its pre-acceptance access",
			registration("denied", "ocm-hub.example.org", grpcauthn.RoleRegistration), several, ""},
		{"denied cluster: singleton agent too",
			registration("denied", "ocm-hub.example.org", grpcauthn.RoleKlusterlet), several, ""},
		{"add-on being deleted", addonNamed("cluster2", "east.example.org", "going-addon"), several,
			"add-on going-addon on cluster cluster2 is being deleted"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			next := &countingAuthorizer{}
			a := &clusterGate{next: next, clusters: lister, addOns: addOns, trustDomains: c.tds}

			decision, _, err := a.AuthorizeRequest(c.ctx, nil)
			streamDecision, _, streamErr := a.AuthorizeStream(c.ctx, &ctxStream{ctx: c.ctx}, &grpc.StreamServerInfo{})
			if c.wantErr != "" {
				require.Equal(t, authz.DecisionDeny, decision)
				require.ErrorContains(t, err, c.wantErr)
				require.Equal(t, authz.DecisionDeny, streamDecision)
				require.ErrorContains(t, streamErr, c.wantErr)
				require.Zero(t, next.calls)
				return
			}
			require.NoError(t, err)
			require.NoError(t, streamErr)
			require.Equal(t, authz.DecisionAllow, decision)
			require.Equal(t, authz.DecisionAllow, streamDecision)
			require.Equal(t, 2, next.calls)
		})
	}
}

// TestClusterGateDataType checks the SAR authorizer's SPIFFE check, whatever the role: a denied
// cluster may only watch and update its ManagedCluster, lease and events, so a singleton agent gets
// no work either; a cluster being deleted may only use ManifestWorks, to clean up.
func TestClusterGateDataType(t *testing.T) {
	denied := pinnedCluster("denied", "ocm-hub.example.org")
	denied.Spec.HubAcceptsClient = false
	now := metav1.Now()
	deleting := pinnedCluster("deleting", "ocm-hub.example.org")
	deleting.DeletionTimestamp = &now
	deniedDeleting := pinnedCluster("denied-deleting", "ocm-hub.example.org")
	deniedDeleting.Spec.HubAcceptsClient = false
	deniedDeleting.DeletionTimestamp = &now
	a := &clusterGate{clusters: clusterLister(t, denied, deleting, deniedDeleting,
		pinnedCluster("cluster1", "ocm-hub.example.org"))}
	of := func(dt types.CloudEventsDataType) types.CloudEventsType {
		return types.CloudEventsType{CloudEventsDataType: dt, SubResource: types.SubResourceSpec,
			Action: types.WatchRequestAction}
	}
	cases := []struct {
		name    string
		role    grpcauthn.AgentRole
		cluster string
		t       types.CloudEventsType
		wantErr string
	}{
		{"denied: singleton agent watches work", grpcauthn.RoleKlusterlet, "denied",
			of(payload.ManifestBundleEventDataType), "cluster denied is not accepted by the hub"},
		{"denied: singleton agent updates work status", grpcauthn.RoleKlusterlet, "denied",
			types.CloudEventsType{CloudEventsDataType: payload.ManifestBundleEventDataType,
				SubResource: types.SubResourceStatus, Action: types.UpdateRequestAction},
			"is not accepted by the hub"},
		{"denied: singleton agent watches add-ons", grpcauthn.RoleKlusterlet, "denied",
			of(addonce.ManagedClusterAddOnEventDataType), "is not accepted by the hub"},
		{"denied: registration agent watches add-ons", grpcauthn.RoleRegistration, "denied",
			of(addonce.ManagedClusterAddOnEventDataType), "is not accepted by the hub"},
		{"denied: singleton agent watches its ManagedCluster", grpcauthn.RoleKlusterlet, "denied",
			of(cluster.ManagedClusterEventDataType), ""},
		{"denied: lease", grpcauthn.RoleKlusterlet, "denied", of(lease.LeaseEventDataType), ""},
		{"denied: events", grpcauthn.RoleRegistration, "denied", of(event.EventEventDataType), ""},
		{"accepted: singleton agent watches work", grpcauthn.RoleKlusterlet, "cluster1",
			of(payload.ManifestBundleEventDataType), ""},
		{"not created yet", grpcauthn.RoleKlusterlet, "cluster9", of(payload.ManifestBundleEventDataType), ""},
		{"deleting: work agent watches work", grpcauthn.RoleWork, "deleting",
			of(payload.ManifestBundleEventDataType), ""},
		{"deleting: singleton agent releases a work finalizer", grpcauthn.RoleKlusterlet, "deleting",
			types.CloudEventsType{CloudEventsDataType: payload.ManifestBundleEventDataType,
				SubResource: types.SubResourceStatus, Action: types.UpdateRequestAction}, ""},
		{"deleting: singleton agent updates its ManagedCluster", grpcauthn.RoleKlusterlet, "deleting",
			of(cluster.ManagedClusterEventDataType), "cluster deleting is being deleted: only ManifestWork cleanup is allowed"},
		{"deleting: lease", grpcauthn.RoleKlusterlet, "deleting", of(lease.LeaseEventDataType),
			"only ManifestWork cleanup is allowed"},
		{"deleting: add-ons", grpcauthn.RoleKlusterlet, "deleting", of(addonce.ManagedClusterAddOnEventDataType),
			"only ManifestWork cleanup is allowed"},
		{"denied and deleting: work agent cleans up", grpcauthn.RoleWork, "denied-deleting",
			of(payload.ManifestBundleEventDataType), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id := grpcauthn.OCMIdentity{ClusterName: c.cluster, TrustDomain: "ocm-hub.example.org", Role: c.role}
			err := a.checkDataType(id, c.cluster, c.t)
			if c.wantErr != "" {
				require.ErrorContains(t, err, c.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
