package testing

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
)

type ManagedClusterBuilder struct {
	cluster *clusterapiv1.ManagedCluster
}

func NewManagedCluster(clusterName string) *ManagedClusterBuilder {
	return &ManagedClusterBuilder{
		cluster: &clusterapiv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
			},
		},
	}
}

func (b *ManagedClusterBuilder) WithLabel(name, value string) *ManagedClusterBuilder {
	if b.cluster.Labels == nil {
		b.cluster.Labels = map[string]string{}
	}
	b.cluster.Labels[name] = value
	return b
}

func (b *ManagedClusterBuilder) WithClaim(name, value string) *ManagedClusterBuilder {
	claimMap := map[string]string{}
	for _, claim := range b.cluster.Status.ClusterClaims {
		claimMap[claim.Name] = claim.Value
	}
	claimMap[name] = value

	clusterClaims := make([]clusterapiv1.ManagedClusterClaim, 0, len(claimMap))
	for k, v := range claimMap {
		clusterClaims = append(clusterClaims, clusterapiv1.ManagedClusterClaim{
			Name:  k,
			Value: v,
		})
	}

	b.cluster.Status.ClusterClaims = clusterClaims
	return b
}

func (b *ManagedClusterBuilder) WithResource(resourceName clusterapiv1.ResourceName, allocatable, capacity string) *ManagedClusterBuilder {
	if b.cluster.Status.Allocatable == nil {
		b.cluster.Status.Allocatable = make(map[clusterapiv1.ResourceName]resource.Quantity)
	}
	if b.cluster.Status.Capacity == nil {
		b.cluster.Status.Capacity = make(map[clusterapiv1.ResourceName]resource.Quantity)
	}

	b.cluster.Status.Allocatable[resourceName], _ = resource.ParseQuantity(allocatable)
	b.cluster.Status.Capacity[resourceName], _ = resource.ParseQuantity(capacity)
	return b
}

func (b *ManagedClusterBuilder) WithTaint(taint *clusterapiv1.Taint) *ManagedClusterBuilder {
	if b.cluster.Spec.Taints == nil {
		b.cluster.Spec.Taints = []clusterapiv1.Taint{}
	}
	b.cluster.Spec.Taints = append(b.cluster.Spec.Taints, *taint)
	return b
}

func (b *ManagedClusterBuilder) WithDeletionTimestamp() *ManagedClusterBuilder {
	if b.cluster.DeletionTimestamp.IsZero() {
		t := metav1.Now()
		b.cluster.DeletionTimestamp = &t
	}

	return b
}

func (b *ManagedClusterBuilder) Build() *clusterapiv1.ManagedCluster {
	return b.cluster
}
