/* Copyright Contributors to the Open Cluster Management project */

package server

import (
	_ "embed"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"open-cluster-management.io/ocm/poc/aggregate-api-server/internal/store"
)

//go:embed apiservices.txt
var apiServiceVersionsFile string

func apiServiceVersions() []string {
	versions := []string{}
	for _, line := range strings.Split(apiServiceVersionsFile, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			versions = append(versions, line)
		}
	}
	return versions
}

type Resource struct {
	Key        store.ResourceKey
	Kind       string
	Singular   string
	Namespaced bool
	HasStatus  bool
	ShortNames []string
}

func resources() []Resource {
	return []Resource{
		{Key: store.ResourceKey{Group: "work.open-cluster-management.io", Version: "v1", Resource: "manifestworks"}, Kind: "ManifestWork", Singular: "manifestwork", Namespaced: true, HasStatus: true, ShortNames: []string{"mw"}},
		{Key: store.ResourceKey{Group: "work.open-cluster-management.io", Version: "v1alpha1", Resource: "manifestworkreplicasets"}, Kind: "ManifestWorkReplicaSet", Singular: "manifestworkreplicaset", Namespaced: true, HasStatus: true, ShortNames: []string{"mwrs"}},
		{Key: store.ResourceKey{Group: "cluster.open-cluster-management.io", Version: "v1", Resource: "managedclusters"}, Kind: "ManagedCluster", Singular: "managedcluster", HasStatus: true, ShortNames: []string{"mcl", "mcls"}},
		{Key: store.ResourceKey{Group: "cluster.open-cluster-management.io", Version: "v1beta2", Resource: "managedclustersets"}, Kind: "ManagedClusterSet", Singular: "managedclusterset", HasStatus: true, ShortNames: []string{"mclset", "mclsets"}},
		{Key: store.ResourceKey{Group: "cluster.open-cluster-management.io", Version: "v1beta2", Resource: "managedclustersetbindings"}, Kind: "ManagedClusterSetBinding", Singular: "managedclustersetbinding", Namespaced: true, HasStatus: true, ShortNames: []string{"mclsetbinding", "mclsetbindings"}},
		{Key: store.ResourceKey{Group: "cluster.open-cluster-management.io", Version: "v1beta1", Resource: "placements"}, Kind: "Placement", Singular: "placement", Namespaced: true, HasStatus: true},
		{Key: store.ResourceKey{Group: "cluster.open-cluster-management.io", Version: "v1beta1", Resource: "placementdecisions"}, Kind: "PlacementDecision", Singular: "placementdecision", Namespaced: true, HasStatus: true},
		{Key: store.ResourceKey{Group: "cluster.open-cluster-management.io", Version: "v1alpha1", Resource: "addonplacementscores"}, Kind: "AddOnPlacementScore", Singular: "addonplacementscore", Namespaced: true, HasStatus: true},
		{Key: store.ResourceKey{Group: "cluster.open-cluster-management.io", Version: "v1alpha1", Resource: "clusterclaims"}, Kind: "ClusterClaim", Singular: "clusterclaim", HasStatus: false},
		{Key: store.ResourceKey{Group: "addon.open-cluster-management.io", Version: "v1alpha1", Resource: "managedclusteraddons"}, Kind: "ManagedClusterAddOn", Singular: "managedclusteraddon", Namespaced: true, HasStatus: true},
		{Key: store.ResourceKey{Group: "addon.open-cluster-management.io", Version: "v1beta1", Resource: "managedclusteraddons"}, Kind: "ManagedClusterAddOn", Singular: "managedclusteraddon", Namespaced: true, HasStatus: true},
		{Key: store.ResourceKey{Group: "addon.open-cluster-management.io", Version: "v1alpha1", Resource: "addondeploymentconfigs"}, Kind: "AddOnDeploymentConfig", Singular: "addondeploymentconfig", Namespaced: true},
		{Key: store.ResourceKey{Group: "addon.open-cluster-management.io", Version: "v1beta1", Resource: "addondeploymentconfigs"}, Kind: "AddOnDeploymentConfig", Singular: "addondeploymentconfig", Namespaced: true},
		{Key: store.ResourceKey{Group: "addon.open-cluster-management.io", Version: "v1alpha1", Resource: "addontemplates"}, Kind: "AddOnTemplate", Singular: "addontemplate", HasStatus: false},
		{Key: store.ResourceKey{Group: "addon.open-cluster-management.io", Version: "v1beta1", Resource: "addontemplates"}, Kind: "AddOnTemplate", Singular: "addontemplate", HasStatus: false},
		{Key: store.ResourceKey{Group: "addon.open-cluster-management.io", Version: "v1alpha1", Resource: "clustermanagementaddons"}, Kind: "ClusterManagementAddOn", Singular: "clustermanagementaddon", HasStatus: true},
		{Key: store.ResourceKey{Group: "addon.open-cluster-management.io", Version: "v1beta1", Resource: "clustermanagementaddons"}, Kind: "ClusterManagementAddOn", Singular: "clustermanagementaddon", HasStatus: true},
		{Key: store.ResourceKey{Group: "internal.open-cluster-management.io", Version: "v1beta1", Resource: "managedclusterinfos"}, Kind: "ManagedClusterInfo", Singular: "managedclusterinfo", Namespaced: true, HasStatus: true},
	}
}

func resourceFor(group, version, resource string) (Resource, bool) {
	for _, candidate := range resources() {
		if candidate.Key.Group == group && candidate.Key.Version == version && candidate.Key.Resource == resource {
			return candidate, true
		}
	}
	return Resource{}, false
}

func resourcesForGroupVersion(group, version string) []Resource {
	result := []Resource{}
	for _, resource := range resources() {
		if resource.Key.Group == group && resource.Key.Version == version {
			result = append(result, resource)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key.Resource < result[j].Key.Resource })
	return result
}

func apiGroups() metav1.APIGroupList {
	groupVersions := map[string]map[string]struct{}{}
	for _, resource := range resources() {
		if groupVersions[resource.Key.Group] == nil {
			groupVersions[resource.Key.Group] = map[string]struct{}{}
		}
		groupVersions[resource.Key.Group][resource.Key.Version] = struct{}{}
	}
	groups := make([]metav1.APIGroup, 0, len(groupVersions))
	for group, versions := range groupVersions {
		versionNames := make([]string, 0, len(versions))
		for version := range versions {
			versionNames = append(versionNames, version)
		}
		sort.Slice(versionNames, func(i, j int) bool { return versionNames[i] > versionNames[j] })
		versionDetails := make([]metav1.GroupVersionForDiscovery, 0, len(versionNames))
		for _, version := range versionNames {
			versionDetails = append(versionDetails, metav1.GroupVersionForDiscovery{GroupVersion: group + "/" + version, Version: version})
		}
		groups = append(groups, metav1.APIGroup{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "APIGroup"}, Name: group, Versions: versionDetails, PreferredVersion: versionDetails[0]})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
	return metav1.APIGroupList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "APIGroupList"}, Groups: groups}
}

func apiGroupFor(group string) (metav1.APIGroup, bool) {
	for _, candidate := range apiGroups().Groups {
		if candidate.Name == group {
			return candidate, true
		}
	}
	return metav1.APIGroup{}, false
}

func apiResourcesFor(group, version string) metav1.APIResourceList {
	list := metav1.APIResourceList{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "APIResourceList"}, GroupVersion: group + "/" + version}
	for _, resource := range resourcesForGroupVersion(group, version) {
		list.APIResources = append(list.APIResources, metav1.APIResource{
			Name: resource.Key.Resource, SingularName: resource.Singular, Namespaced: resource.Namespaced, Kind: resource.Kind,
			Verbs: metav1.Verbs{"create", "delete", "get", "list", "patch", "update", "watch"}, ShortNames: resource.ShortNames,
		})
		if resource.HasStatus {
			list.APIResources = append(list.APIResources, metav1.APIResource{
				Name: resource.Key.Resource + "/status", Namespaced: resource.Namespaced, Kind: resource.Kind, Verbs: metav1.Verbs{"get", "patch", "update"},
			})
		}
	}
	return list
}
