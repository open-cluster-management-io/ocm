package managedcluster

import (
	"context"
	"fmt"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/sets"
	clusterv1alpha1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	ocmfeature "open-cluster-management.io/api/feature"
	"open-cluster-management.io/ocm/pkg/features"
	aboutv1alpha1 "sigs.k8s.io/about-api/pkg/apis/v1alpha1"
	aboutv1alpha1listers "sigs.k8s.io/about-api/pkg/generated/listers/apis/v1alpha1"
	"sort"
	"strings"
)

const labelCustomizedOnly = "open-cluster-management.io/spoke-only"

type claimReconcile struct {
	recorder                     events.Recorder
	claimLister                  clusterv1alpha1listers.ClusterClaimLister
	aboutLister                  aboutv1alpha1listers.ClusterPropertyLister
	maxCustomClusterClaims       int
	reservedClusterClaimSuffixes []string
}

func (r *claimReconcile) reconcile(ctx context.Context, cluster *clusterv1.ManagedCluster) (*clusterv1.ManagedCluster, reconcileState, error) {
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
func (r *claimReconcile) exposeClaims(_ context.Context, cluster *clusterv1.ManagedCluster) error {
	var reservedClaims, customClaims []clusterv1.ManagedClusterClaim
	var clusterClaims []*clusterv1alpha1.ClusterClaim

	// clusterClaim with label `open-cluster-management.io/spoke-only` will not be synced to managedCluster.Status at hub.
	requirement, err := labels.NewRequirement(labelCustomizedOnly, selection.DoesNotExist, []string{})
	if err != nil {
		return err
	}
	selector := labels.NewSelector().Add(*requirement)

	if features.SpokeMutableFeatureGate.Enabled(ocmfeature.ClusterClaim) {
		clusterClaims, err = r.claimLister.List(selector)
		if err != nil {
			return fmt.Errorf("unable to list cluster claims: %w", err)
		}
	}

	// check if the cluster claim is one of the reserved claims or has a reserved suffix.
	// if so, it will be treated as a reserved claim and will always be exposed.
	reservedClaimNames := sets.New(clusterv1alpha1.ReservedClusterClaimNames[:]...)
	reservedClaimSuffixes := sets.New(r.reservedClusterClaimSuffixes...)

	// when ClusterProperties feature is not enabled, the informer will not be started and the lister will
	// return an empty list, so we do not need to check featuregate here.
	propertiesMap := map[string]*aboutv1alpha1.ClusterProperty{}
	if features.SpokeMutableFeatureGate.Enabled(ocmfeature.ClusterProperty) {
		clusterProperties, err := r.aboutLister.List(selector)
		if err != nil {
			return fmt.Errorf("unable to list cluster properties: %w", err)
		}

		for _, property := range clusterProperties {
			propertiesMap[property.Name] = property
		}
	}

	// convert claim to properties
	for _, clusterClaim := range clusterClaims {
		// if the claim has the same name with the property, ignore it.
		if _, ok := propertiesMap[clusterClaim.Name]; ok {
			continue
		}
		propertiesMap[clusterClaim.Name] = &aboutv1alpha1.ClusterProperty{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterClaim.Name,
			},
			Spec: aboutv1alpha1.ClusterPropertySpec{
				Value: clusterClaim.Spec.Value,
			},
		}
	}

	for _, property := range propertiesMap {
		managedClusterClaim := clusterv1.ManagedClusterClaim{
			Name:  property.Name,
			Value: property.Spec.Value,
		}
		if matchReservedClaims(reservedClaimNames, reservedClaimSuffixes, managedClusterClaim) {
			reservedClaims = append(reservedClaims, managedClusterClaim)
			reservedClaimNames.Insert(property.Name)
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
