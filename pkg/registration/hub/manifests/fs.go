package manifests

import "embed"

//go:embed rbac
var RBACManifests embed.FS

// ClusterSpecificRBACFiles are cluster-specific RBAC manifests.
// Created when a managed cluster is created.
// Updated according to the managed cluster's spec.hubAcceptsClient.
// Removed when a managed cluster is removed.
var ClusterSpecificRBACFiles = []string{
	"rbac/managedcluster-clusterrole.yaml",
	"rbac/managedcluster-clusterrolebinding.yaml",
}

// ClusterSpecificRoleBindings are also cluster-specific rolebindings.
// Created when a managed cluster is accepted and removed when a managed cluster is removed or not accepted.
var ClusterSpecificRoleBindings = []string{
	"rbac/managedcluster-registration-rolebinding.yaml",
	"rbac/managedcluster-work-rolebinding.yaml",
}

// CommonClusterRoleFiles are common clusterroles needed by any managed cluster.
var CommonClusterRoleFiles = []string{
	"rbac/managedcluster-registration-clusterrole.yaml",
	"rbac/managedcluster-work-clusterrole.yaml",
}
