package clusterprofile

import (
	"cmp"

	cpv1alpha2 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha2"

	v1 "open-cluster-management.io/api/cluster/v1"
)

func inventoryMemberID(cluster *v1.ManagedCluster) string {
	return cmp.Or(cluster.Labels[cpv1alpha2.LabelInventoryMemberIDKey], cluster.Name)
}
