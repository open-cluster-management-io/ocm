package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
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

func clusterLister(t *testing.T, clusters ...*clusterv1.ManagedCluster) clusterlisterv1.ManagedClusterLister {
	t.Helper()
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, c := range clusters {
		require.NoError(t, indexer.Add(c))
	}
	return clusterlisterv1.NewManagedClusterLister(indexer)
}

func pinnedCluster(name, td string) *clusterv1.ManagedCluster {
	c := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: name}}
	if td != "" {
		c.Annotations = map[string]string{clusterv1.SPIFFETrustDomainAnnotationKey: td}
	}
	return c
}

func TestPinningAuthorizer(t *testing.T) {
	agent := func(cluster, td string) context.Context {
		return context.WithValue(context.Background(), grpcauthn.ContextOCMIdentityKey,
			grpcauthn.OCMIdentity{ClusterName: cluster, TrustDomain: td, Role: grpcauthn.RoleWork})
	}
	addon := func(cluster, td string) context.Context {
		return context.WithValue(context.Background(), grpcauthn.ContextOCMIdentityKey,
			grpcauthn.OCMIdentity{ClusterName: cluster, TrustDomain: td, AddOnName: "my-addon", AgentName: "a"})
	}
	lister := clusterLister(t,
		pinnedCluster("cluster1", "ocm-hub.example.org"),
		pinnedCluster("cluster2", "east.example.org"),
		pinnedCluster("cluster3", "west.example.org"),
		pinnedCluster("legacy", ""),
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
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			next := &countingAuthorizer{}
			a := &pinningAuthorizer{next: next, clusters: lister, trustDomains: c.tds}

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
