package managedcluster

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	aboutv1alpha1listers "sigs.k8s.io/about-api/pkg/generated/listers/apis/v1alpha1"

	clusterv1alpha1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	"open-cluster-management.io/ocm/pkg/features"
)

const labelCustomizedOnly = "open-cluster-management.io/spoke-only"

type claimReconcile struct {
	claimLister                  clusterv1alpha1listers.ClusterClaimLister
	aboutLister                  aboutv1alpha1listers.ClusterPropertyLister
	maxCustomClusterClaims       int
	reservedClusterClaimSuffixes []string
}

func (r *claimReconcile) reconcile(ctx context.Context, syncCtx factory.SyncContext, cluster *clusterv1.ManagedCluster) (*clusterv1.ManagedCluster, reconcileState, error) {
	// current managed cluster has not joined the hub yet, do nothing.
	if !meta.IsStatusConditionTrue(cluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined) {
		syncCtx.Recorder().Eventf(ctx, "ManagedClusterIsNotAccepted", "Managed cluster %q does not join the hub yet", cluster.Name)
		return cluster, reconcileContinue, nil
	}

	err := r.exposeClaims(ctx, syncCtx, cluster)
	return cluster, reconcileContinue, err
}

// exposeClaims saves cluster claims fetched on managed cluster into status of the
// managed cluster on hub. Some of the customized claims might not be exposed once
// the total number of the claims exceeds the value of `cluster-claims-max`.
func (r *claimReconcile) exposeClaims(ctx context.Context, syncCtx factory.SyncContext, cluster *clusterv1.ManagedCluster) error {
	var reservedClaims, customClaims []clusterv1.ManagedClusterClaim
	var clusterClaims []*clusterv1alpha1.ClusterClaim
	claimsMap := map[string]clusterv1.ManagedClusterClaim{}

	// clusterClaim with label `open-cluster-management.io/spoke-only` will not be synced to managedCluster.Status at hub.
	requirement, err := labels.NewRequirement(labelCustomizedOnly, selection.DoesNotExist, []string{})
	if err != nil {
		return err
	}
	selector := labels.NewSelector().Add(*requirement)

	if features.SpokeMutableFeatureGate.Enabled(ocmfeature.ClusterProperty) {
		clusterProperties, err := r.aboutLister.List(selector)
		if err != nil {
			return fmt.Errorf("unable to list cluster properties: %w", err)
		}

		for _, property := range clusterProperties {
			claimsMap[property.Name] = clusterv1.ManagedClusterClaim{
				Name:  property.Name,
				Value: property.Spec.Value,
			}
		}
	}

	if features.SpokeMutableFeatureGate.Enabled(ocmfeature.ClusterClaim) {
		clusterClaims, err = r.claimLister.List(selector)
		if err != nil {
			return fmt.Errorf("unable to list cluster claims: %w", err)
		}

		for _, claim := range clusterClaims {
			// if the claim has the same name with the property, ignore it.
			if _, ok := claimsMap[claim.Name]; !ok {
				claimsMap[claim.Name] = clusterv1.ManagedClusterClaim{
					Name:  claim.Name,
					Value: claim.Spec.Value,
				}
			}
		}
	}

	// check if the cluster claim is one of the reserved claims or has a reserved suffix.
	// if so, it will be treated as a reserved claim and will always be exposed.
	reservedClaimNames := sets.New(clusterv1alpha1.ReservedClusterClaimNames[:]...)
	reservedClaimSuffixes := sets.New(r.reservedClusterClaimSuffixes...)

	for _, managedClusterClaim := range claimsMap {
		if matchReservedClaims(reservedClaimNames, reservedClaimSuffixes, managedClusterClaim) {
			reservedClaims = append(reservedClaims, managedClusterClaim)
			// reservedClaimNames.Insert(managedClusterClaim.Name)
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
		syncCtx.Recorder().Eventf(ctx, "CustomClusterClaimsTruncated",
			"%d cluster claims are found. It exceeds the max number of custom cluster claims (%d). %d custom cluster claims are not exposed.",
			n, r.maxCustomClusterClaims, n-r.maxCustomClusterClaims)
	}

	// merge reserved claims and custom claims
	claims := append(reservedClaims, customClaims...) // nolint:gocritic
	cluster.Status.ClusterClaims = claims
	return nil
}

func matchReservedClaims(reservedClaims, reservedSuffixes sets.Set[string], claim clusterv1.ManagedClusterClaim) bool {
	if reservedClaims.Has(claim.Name) {
		return true
	}

	for suffix := range reservedSuffixes {
		if strings.HasSuffix(claim.Name, suffix) {
			return true
		}
	}
	return false
}
