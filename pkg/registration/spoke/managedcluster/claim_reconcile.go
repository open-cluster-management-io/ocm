package managedcluster

import (
	"context"
	"fmt"
	"sort"

	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	clusterv1alpha1listers "open-cluster-management.io/api/client/cluster/listers/cluster/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	ocmfeature "open-cluster-management.io/api/feature"
	aboutv1alpha1 "sigs.k8s.io/about-api/pkg/apis/v1alpha1"
	aboutv1alpha1listers "sigs.k8s.io/about-api/pkg/generated/listers/apis/v1alpha1"

	"open-cluster-management.io/ocm/pkg/features"
)

const labelCustomizedOnly = "open-cluster-management.io/spoke-only"

type claimReconcile struct {
	recorder               events.Recorder
	claimLister            clusterv1alpha1listers.ClusterClaimLister
	aboutLister            aboutv1alpha1listers.ClusterPropertyLister
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
	fmt.Printf("clusterClaims: %+v \n and err: %+v", clusterClaims, err)
	if err != nil {
		return fmt.Errorf("unable to list cluster claims: %w", err)
	}

	clusterProperties, err := r.aboutLister.List(selector)
	fmt.Printf("clusterProperties: %+v \n and err: %+v", clusterProperties, err)
	if err != nil {
		return fmt.Errorf("unable to list cluster properties: %w", err)
	}

	propertiesMap := map[string]*aboutv1alpha1.ClusterProperty{}
	for _, property := range clusterProperties {
		propertiesMap[property.Name] = &aboutv1alpha1.ClusterProperty{
			ObjectMeta: metav1.ObjectMeta{
				Name: property.Name,
			},
			Spec: aboutv1alpha1.ClusterPropertySpec{
				Value: property.Spec.Value,
			},
		}
	}
	// convert claim to properties
	for _, clusterClaim := range clusterClaims {
		// if the claim has the same name with the property, ignore it.
		if _, ok := propertiesMap[clusterClaim.Name]; !ok {
			propertiesMap[clusterClaim.Name] = &aboutv1alpha1.ClusterProperty{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterClaim.Name,
				},
				Spec: aboutv1alpha1.ClusterPropertySpec{
					Value: clusterClaim.Spec.Value,
				},
			}
		}
	}

	reservedClaimNames := make(map[string]bool)
	for _, name := range clusterv1alpha1.ReservedClusterClaimNames {
		reservedClaimNames[name] = false
	}
	for _, property := range propertiesMap {
		managedClusterClaim := clusterv1.ManagedClusterClaim{
			Name:  property.Name,
			Value: property.Spec.Value,
		}
		if _, ok := reservedClaimNames[property.Name]; ok {
			reservedClaims = append(reservedClaims, managedClusterClaim)
			reservedClaimNames[property.Name] = true
			continue
		}
		customClaims = append(customClaims, managedClusterClaim)
	}

	for _, clusterClaim := range clusterClaims {
		managedClusterClaim := clusterv1.ManagedClusterClaim{
			Name:  clusterClaim.Name,
			Value: clusterClaim.Spec.Value,
		}
		if val, ok := reservedClaimNames[clusterClaim.Name]; ok && !val {
			reservedClaims = append(reservedClaims, managedClusterClaim)
			reservedClaimNames[clusterClaim.Name] = true
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
	claims := append(reservedClaims, customClaims...)
	cluster.Status.ClusterClaims = claims
	return nil
}
