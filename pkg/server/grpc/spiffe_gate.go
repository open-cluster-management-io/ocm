package grpc

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	addonlisterv1beta1 "open-cluster-management.io/api/client/addon/listers/addon/v1beta1"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/cluster"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/event"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/lease"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/payload"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	grpcauthn "open-cluster-management.io/sdk-go/pkg/server/grpc/authn"
	"open-cluster-management.io/sdk-go/pkg/server/grpc/authz"
)

// authorizer is what the SPIFFE listener needs from the SAR authorizer.
type authorizer interface {
	authz.UnaryAuthorizer
	authz.StreamAuthorizer
}

// clusterGate runs before the SubjectAccessReview on the SPIFFE listener and refuses:
//   - an agent whose SVID is in a different trust domain than the one its cluster is pinned to.
//     Without it, a federated trust domain could issue an SVID for another trust domain's
//     cluster and act as that cluster: the OCM user comes from the SVID's path, not its trust
//     domain;
//   - the registration agent and the add-on agents of a cluster that is being deleted. A closed
//     connection is reopened at once, and the SVIDs issued before the deletion stay valid until
//     they expire. The work agent (and the singleton agent) is let in: the hub deletes the
//     cluster's ManifestWorks and keeps the work RoleBinding until they are gone, and only the
//     work agent can remove what they applied and release their finalizers;
//   - an add-on agent whose ManagedClusterAddOn is gone or being deleted;
//   - the work agent and the add-on agents of a cluster that isn't accepted (hubAcceptsClient
//     false). The hub keeps the cluster's work RoleBinding when it is denied, so RBAC alone
//     would let them reconnect. The registration agent (and the singleton agent, which is also
//     the registration agent) is let in, to learn when the cluster is accepted again.
//
// The gate runs before the request is parsed, so it can only decide by the SVID. checkDataType
// runs inside the SAR authorizer, once the data type is known, whatever the role: for a cluster
// being deleted it allows only ManifestWork requests, and for a denied cluster it refuses work and
// add-on requests. That is what keeps the singleton agent, whose one SVID is also the
// registration agent's, to the same rules as the separate agents.
type clusterGate struct {
	next         authorizer
	clusters     clusterlisterv1.ManagedClusterLister
	addOns       addonlisterv1beta1.ManagedClusterAddOnLister
	trustDomains []string
}

var _ authorizer = &clusterGate{}

func (a *clusterGate) AuthorizeRequest(ctx context.Context, req any) (authz.Decision, context.Context, error) {
	if err := a.check(ctx); err != nil {
		return authz.DecisionDeny, ctx, err
	}
	return a.next.AuthorizeRequest(ctx, req)
}

func (a *clusterGate) AuthorizeStream(ctx context.Context, ss grpc.ServerStream, info *grpc.StreamServerInfo) (authz.Decision, grpc.ServerStream, error) {
	if err := a.check(ss.Context()); err != nil {
		return authz.DecisionDeny, nil, err
	}
	return a.next.AuthorizeStream(ctx, ss, info)
}

// check applies the gate to the cluster and add-on in the SVID. The next authorizer makes sure
// the request is about the cluster in the SVID, so the SVID's cluster is the one to check.
func (a *clusterGate) check(ctx context.Context) error {
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
	deleting := cluster.DeletionTimestamp != nil
	if deleting && (id.IsAddOn() || id.Role == grpcauthn.RoleRegistration) {
		return fmt.Errorf("cluster %s is being deleted", id.ClusterName)
	}
	// A cluster being deleted is cleaned up by its work agent even if it was denied, as with the
	// other drivers: the hub keeps the work RoleBinding of a denied cluster too.
	if !deleting && !cluster.Spec.HubAcceptsClient && (id.IsAddOn() || id.Role == grpcauthn.RoleWork) {
		return fmt.Errorf("cluster %s is not accepted by the hub", id.ClusterName)
	}
	if err := a.checkAddOn(id); err != nil {
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

// deniedClusterDataTypes are what an agent of a cluster that isn't accepted may still send or
// watch: its own ManagedCluster, to learn when it is accepted again, and the lease and events the
// registration agent keeps sending. Everything else, work and add-ons above all, is refused.
var deniedClusterDataTypes = map[types.CloudEventsDataType]bool{
	cluster.ManagedClusterEventDataType: true,
	lease.LeaseEventDataType:            true,
	event.EventEventDataType:            true,
}

// checkDataType is the SAR authorizer's SPIFFE check, for every agent role. For a cluster being
// deleted it allows only ManifestWork requests, so the work agent can clean up what the cluster's
// ManifestWorks applied and the deletion can finish. For a denied cluster it refuses the requests
// that are not in deniedClusterDataTypes. The SAR authorizer has already made sure the request is
// about the cluster in the SVID.
func (a *clusterGate) checkDataType(id grpcauthn.OCMIdentity, clusterName string, t types.CloudEventsType) error {
	mc, err := a.clusters.Get(clusterName)
	if apierrors.IsNotFound(err) {
		// Not created yet: RBAC allows nothing but creating it.
		return nil
	}
	if err != nil {
		return err
	}
	if mc.DeletionTimestamp != nil {
		if t.CloudEventsDataType == payload.ManifestBundleEventDataType {
			return nil
		}
		return fmt.Errorf("cluster %s is being deleted: only ManifestWork cleanup is allowed, %s from %s is refused",
			clusterName, t.CloudEventsDataType, id.User)
	}
	if deniedClusterDataTypes[t.CloudEventsDataType] {
		return nil
	}
	if !mc.Spec.HubAcceptsClient {
		return fmt.Errorf("cluster %s is not accepted by the hub: %s from %s is refused",
			clusterName, t.CloudEventsDataType, id.User)
	}
	return nil
}

func (a *clusterGate) checkAddOn(id grpcauthn.OCMIdentity) error {
	if !id.IsAddOn() {
		return nil
	}
	addon, err := a.addOns.ManagedClusterAddOns(id.ClusterName).Get(id.AddOnName)
	if apierrors.IsNotFound(err) {
		return fmt.Errorf("add-on %s is not enabled on cluster %s", id.AddOnName, id.ClusterName)
	}
	if err != nil {
		return err
	}
	if addon.DeletionTimestamp != nil {
		return fmt.Errorf("add-on %s on cluster %s is being deleted", id.AddOnName, id.ClusterName)
	}
	return nil
}
