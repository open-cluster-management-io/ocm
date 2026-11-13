package grpc

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	grpcauthn "open-cluster-management.io/sdk-go/pkg/server/grpc/authn"
	"open-cluster-management.io/sdk-go/pkg/server/grpc/authz"
)

// authorizer is what the SPIFFE listener needs from the SAR authorizer.
type authorizer interface {
	authz.UnaryAuthorizer
	authz.StreamAuthorizer
}

// pinningAuthorizer refuses a SPIFFE agent whose SVID is in a different trust domain than the
// one its cluster is pinned to, then hands over to the next authorizer. Without it, a federated
// trust domain could issue an SVID for another trust domain's cluster and act as that cluster:
// the OCM user comes from the SVID's path, not its trust domain.
type pinningAuthorizer struct {
	next         authorizer
	clusters     clusterlisterv1.ManagedClusterLister
	trustDomains []string
}

var _ authorizer = &pinningAuthorizer{}

func (a *pinningAuthorizer) AuthorizeRequest(ctx context.Context, req any) (authz.Decision, context.Context, error) {
	if err := a.checkPin(ctx); err != nil {
		return authz.DecisionDeny, ctx, err
	}
	return a.next.AuthorizeRequest(ctx, req)
}

func (a *pinningAuthorizer) AuthorizeStream(ctx context.Context, ss grpc.ServerStream, info *grpc.StreamServerInfo) (authz.Decision, grpc.ServerStream, error) {
	if err := a.checkPin(ss.Context()); err != nil {
		return authz.DecisionDeny, nil, err
	}
	return a.next.AuthorizeStream(ctx, ss, info)
}

// checkPin compares the SVID's trust domain with the cluster's pin. The next authorizer makes
// sure the request is about the cluster in the SVID, so the SVID's cluster is the one to check.
func (a *pinningAuthorizer) checkPin(ctx context.Context) error {
	id, ok := grpcauthn.OCMIdentityFromContext(ctx)
	if !ok {
		return fmt.Errorf("no SPIFFE identity in the request")
	}
	cluster, err := a.clusters.Get(id.ClusterName)
	if apierrors.IsNotFound(err) {
		// The registration agent may create its cluster; the cluster service pins it then.
		return nil
	}
	if err != nil {
		return err
	}
	pinned, ok := cluster.Annotations[clusterv1.SPIFFETrustDomainAnnotationKey]
	if !ok {
		// A cluster created before the switch from another driver, or by an admin.
		if len(a.trustDomains) != 1 {
			return fmt.Errorf("cluster %q is not pinned to a trust domain: an admin must set the %s annotation",
				id.ClusterName, clusterv1.SPIFFETrustDomainAnnotationKey)
		}
		pinned = a.trustDomains[0]
	}
	if pinned != id.TrustDomain {
		return fmt.Errorf("cluster %q is pinned to trust domain %q, not %q", id.ClusterName, pinned, id.TrustDomain)
	}
	return nil
}
