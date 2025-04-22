package managedcluster

import (
	"context"
	"fmt"
	"sort"

	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"

	clusterv1alpha1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	ocmfeature "open-cluster-management.io/api/feature"

	"open-cluster-management.io/ocm/pkg/features"
)

const labelCustomizedOnly = "open-cluster-management.io/spoke-only"

type claimReconcile struct {
	recorder               events.Recorder
	claimLister            clusterv1alpha1listers.ClusterClaimLister
	maxCustomClusterClaims int
}

func (r *claimReconcile) reconcile(ctx context.Context, cluster *clusterv1.ManagedCluster) (*clusterv1.ManagedCluster, reconcileState, error) {
	if !features.SpokeMutableFeatureGate.Enabled(ocmfeature.ClusterClaim) {
		return cluster, reconcileContinue, nil
	}
	// current managed cluster has not joined the hub yet, do nothing.
	if !meta.IsStatusConditionTrue(cluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined) {
		r.recorder.Eventf("ManagedClusterIsNotAccepted", "Managed cluster %q does not join the hub yet", cluster.Name)
		return cluster, reconcileContinue, nil
	}

	err := r.exposeClaims(ctx, cluster)
	return cluster, reconcileContinue, err
}

// exposeClaims saves cluster claims fetched on managed cluster into status of the
// managed cluster on hub. Some of the customized claims might not be exposed once
// the total number of the claims exceeds the value of `cluster-claims-max`.
func (r *claimReconcile) exposeClaims(ctx context.Context, cluster *clusterv1.ManagedCluster) error {
	var reservedClaims, customClaims []clusterv1.ManagedClusterClaim

	// clusterClaim with label `open-cluster-management.io/spoke-only` will not be synced to managedCluster.Status at hub.
	requirement, _ := labels.NewRequirement(labelCustomizedOnly, selection.DoesNotExist, []string{})
	selector := labels.NewSelector().Add(*requirement)
	clusterClaims, err := r.claimLister.List(selector)
	if err != nil {
		return fmt.Errorf("unable to list cluster claims: %w", err)
	}

	reservedClaimNames := sets.NewString(clusterv1alpha1.ReservedClusterClaimNames[:]...)
	for _, clusterClaim := range clusterClaims {
		managedClusterClaim := clusterv1.ManagedClusterClaim{
			Name:  clusterClaim.Name,
			Value: clusterClaim.Spec.Value,
		}
		if reservedClaimNames.Has(clusterClaim.Name) {
			reservedClaims = append(reservedClaims, managedClusterClaim)
			continue
		}
		customClaims = append(customClaims, managedClusterClaim)
	}

	// sort claims by name
	sort.SliceStable(reservedClaims, func(i, j int) bool {
		return reservedClaims[i].Name < reservedClaims[j].Name
	})

	sort.SliceStable(customClaims, func(i, j int) bool {
		return customClaims[i].Name < customClaims[j].Name
	})

	// truncate custom claims if the number exceeds `max-custom-cluster-claims`
	if n := len(customClaims); n > r.maxCustomClusterClaims {
		customClaims = customClaims[:r.maxCustomClusterClaims]
		r.recorder.Eventf("CustomClusterClaimsTruncated",
			"%d cluster claims are found. It exceeds the max number of custom cluster claims (%d). %d custom cluster claims are not exposed.",
			n, r.maxCustomClusterClaims, n-r.maxCustomClusterClaims)
	}

	// merge reserved claims and custom claims
	claims := append(reservedClaims, customClaims...) // nolint:gocritic
	cluster.Status.ClusterClaims = claims
	return nil
}
