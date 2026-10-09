package clusterprofile

import (
	"cmp"

	v1 "open-cluster-management.io/api/cluster/v1"
)

// The key defined by KEP-4322 is not yet exposed by cluster-inventory-api v0.1.3.
const InventoryMemberIDLabelKey = "multicluster.x-k8s.io/inventory-member-id"

func inventoryMemberID(cluster *v1.ManagedCluster) string {
	return cmp.Or(cluster.Labels[InventoryMemberIDLabelKey], cluster.Name)
}
