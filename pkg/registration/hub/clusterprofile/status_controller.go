package clusterprofile

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/csaupgrade"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	cpv1alpha2 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha2"
	cpclientset "sigs.k8s.io/cluster-inventory-api/client/clientset/versioned"
	cpclientv1alpha2 "sigs.k8s.io/cluster-inventory-api/client/clientset/versioned/typed/apis/v1alpha2"
	cpinformerv1alpha2 "sigs.k8s.io/cluster-inventory-api/client/informers/externalversions/apis/v1alpha2"
	cplisterv1alpha2 "sigs.k8s.io/cluster-inventory-api/client/listers/apis/v1alpha2"
	"sigs.k8s.io/structured-merge-diff/v6/fieldpath"

	informerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	listerv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	v1 "open-cluster-management.io/api/cluster/v1"
	v1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
)

const (
	byClusterName      = "by-cluster-name"
	statusFieldManager = "ocm-clusterprofile-status"
)

// clusterProfileStatusController updates ClusterProfile status and labels from ManagedCluster
// Queue key: cluster name (e.g., "cluster1")
type clusterProfileStatusController struct {
	clusterLister         listerv1.ManagedClusterLister
	clusterProfileClient  cpclientset.Interface
	clusterProfileLister  cplisterv1alpha2.ClusterProfileLister
	clusterProfileIndexer cache.Indexer
	// legacyStatusManager owns the status fields earlier releases wrote with merge patches.
	legacyStatusManager string
}

// indexByClusterName is the indexer function for ClusterProfile by cluster name
func indexByClusterName(obj interface{}) ([]string, error) {
	profile, ok := obj.(*cpv1alpha2.ClusterProfile)
	if !ok {
		return []string{}, fmt.Errorf("obj is supposed to be a ClusterProfile, but is %T", obj)
	}

	// Index by the cluster-name label
	if clusterName, ok := profile.Labels[v1.ClusterNameLabelKey]; ok {
		return []string{clusterName}, nil
	}

	// Fallback: use profile name as cluster name (lifecycle controller sets Name = cluster name)
	return []string{profile.Name}, nil
}

func NewClusterProfileStatusController(
	clusterInformer informerv1.ManagedClusterInformer,
	clusterProfileClient cpclientset.Interface,
	clusterProfileInformer cpinformerv1alpha2.ClusterProfileInformer) factory.Controller {

	// Add indexer for efficient lookup of profiles by cluster name
	err := clusterProfileInformer.Informer().AddIndexers(cache.Indexers{
		byClusterName: indexByClusterName,
	})
	if err != nil {
		utilruntime.HandleError(err)
	}

	c := &clusterProfileStatusController{
		clusterLister:         clusterInformer.Lister(),
		clusterProfileClient:  clusterProfileClient,
		clusterProfileLister:  clusterProfileInformer.Lister(),
		clusterProfileIndexer: clusterProfileInformer.Informer().GetIndexer(),
		// The API server names the manager of a request without fieldManager after the
		// user agent, which the hub leaves at the client-go default.
		legacyStatusManager: strings.Split(rest.DefaultKubernetesUserAgent(), "/")[0],
	}

	return factory.New().
		WithInformersQueueKeysFunc(c.clusterToQueueKey, clusterInformer.Informer()).
		WithFilteredEventsInformersQueueKeysFunc(
			c.profileToQueueKey,
			queue.UnionFilter(
				queue.FileterByLabel(v1.ClusterNameLabelKey),
				queue.FileterByLabelKeyValue(cpv1alpha2.LabelClusterManagerKey, ClusterProfileManagerName),
			),
			clusterProfileInformer.Informer()).
		WithSync(c.sync).
		ToController("ClusterProfileStatusController")
}

func (c *clusterProfileStatusController) clusterToQueueKey(obj runtime.Object) []string {
	cluster, ok := obj.(*v1.ManagedCluster)
	if !ok {
		return nil
	}
	return []string{cluster.Name}
}

func (c *clusterProfileStatusController) profileToQueueKey(obj runtime.Object) []string {
	profile, ok := obj.(*cpv1alpha2.ClusterProfile)
	if !ok {
		return nil
	}

	// Use cluster-name label (filtering already done by WithFilteredEventsInformersQueueKeysFunc)
	if clusterName, ok := profile.Labels[v1.ClusterNameLabelKey]; ok {
		return []string{clusterName}
	}

	// Fallback: profile name = cluster name
	return []string{profile.Name}
}

func (c *clusterProfileStatusController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	clusterName := key
	logger := klog.FromContext(ctx).WithValues("managedCluster", clusterName)

	logger.V(4).Info("Updating ClusterProfile status")

	cluster, err := c.clusterLister.Get(clusterName)
	if errors.IsNotFound(err) {
		logger.V(4).Info("Cluster not found, skipping status update")
		return nil
	}
	if err != nil {
		return err
	}

	// Use indexer for efficient lookup instead of listing all profiles across all namespaces
	allProfiles, err := c.getProfilesByClusterName(clusterName)
	if err != nil {
		return err
	}

	logger.V(4).Info("Found profiles to update", "count", len(allProfiles))

	updatedCount := 0
	var errs []error
	for _, profile := range allProfiles {
		if profile.Spec.ClusterManager.Name != ClusterProfileManagerName {
			continue
		}

		updated, err := c.updateClusterProfile(ctx, profile, cluster)
		if err != nil {
			logger.Error(err, "Failed to update profile",
				"namespace", profile.Namespace, "name", profile.Name)
			errs = append(errs, fmt.Errorf("failed to update ClusterProfile %s/%s: %w", profile.Namespace, profile.Name, err))
			continue
		}
		if updated {
			updatedCount++
		}
	}

	if updatedCount > 0 {
		logger.V(2).Info("Updated profiles", "count", updatedCount)
		syncCtx.Recorder().Eventf(ctx, "ClusterProfileStatusUpdated",
			"updated %d cluster profiles for cluster %s", updatedCount, clusterName)
	}

	return utilerrors.NewAggregate(errs)
}

// getProfilesByClusterName efficiently retrieves all profiles for a given cluster using the indexer
func (c *clusterProfileStatusController) getProfilesByClusterName(clusterName string) ([]*cpv1alpha2.ClusterProfile, error) {
	objs, err := c.clusterProfileIndexer.ByIndex(byClusterName, clusterName)
	if err != nil {
		return nil, err
	}

	profiles := make([]*cpv1alpha2.ClusterProfile, 0, len(objs))
	for _, obj := range objs {
		profile, ok := obj.(*cpv1alpha2.ClusterProfile)
		if !ok {
			continue
		}
		profiles = append(profiles, profile)
	}

	return profiles, nil
}

func (c *clusterProfileStatusController) updateClusterProfile(
	ctx context.Context,
	existing *cpv1alpha2.ClusterProfile,
	cluster *v1.ManagedCluster) (bool, error) {

	// Create a patcher for this specific namespace
	profiles := c.clusterProfileClient.ApisV1alpha2().ClusterProfiles(existing.Namespace)
	profilePatcher := patcher.NewPatcher[
		*cpv1alpha2.ClusterProfile, cpv1alpha2.ClusterProfileSpec, cpv1alpha2.ClusterProfileStatus](profiles)

	// Patch status first to avoid ResourceVersion conflict
	// If status has been updated, return early - labels will be updated in next reconcile
	updated, err := c.applyStatus(ctx, profiles, existing, cluster)
	if updated || err != nil {
		return updated, err
	}

	// Status wasn't updated, now safe to patch labels
	newProfile := existing.DeepCopy()
	syncLabelsFromCluster(newProfile, cluster)

	// Patch labels (patcher handles change detection)
	return profilePatcher.PatchLabelAnnotations(ctx, newProfile, newProfile.ObjectMeta, existing.ObjectMeta)
}

// applyStatus applies only the status fields derived from the ManagedCluster, so other
// managers keep their fields and ours are pruned once their source disappears.
func (c *clusterProfileStatusController) applyStatus(
	ctx context.Context,
	profiles cpclientv1alpha2.ClusterProfileInterface,
	existing *cpv1alpha2.ClusterProfile,
	cluster *v1.ManagedCluster) (bool, error) {

	// Earlier releases wrote status with merge patches. Take over those fields once so
	// the apply can prune them; managed fields can only be set through the main resource.
	// The helper writes through the FieldsV1 it shares with its input, so pass a copy of the cached profile.
	upgradePatch, err := csaupgrade.UpgradeManagedFieldsPatch(existing.DeepCopy(),
		sets.New(c.legacyStatusManager), statusFieldManager, csaupgrade.Subresource("status"))
	if err != nil {
		return false, err
	}
	if upgradePatch != nil {
		_, err = profiles.Patch(ctx, existing.Name, types.JSONPatchType, upgradePatch, metav1.PatchOptions{})
		return true, err
	}

	ownedFields, otherFields, err := statusFieldOwners(existing)
	if err != nil {
		return false, fmt.Errorf("read ClusterProfile managed fields: %w", err)
	}
	if removals := orphanedPropertyRemovals(existing, cluster, ownedFields, otherFields); len(removals) > 0 {
		// Ownership was read from this snapshot, so a newer write must fail the patch rather
		// than be removed. Replacing the resourceVersion, as csaupgrade does, returns a 409.
		precondition := map[string]any{"op": "replace", "path": "/metadata/resourceVersion", "value": existing.ResourceVersion}
		patch, err := json.Marshal(append([]map[string]any{precondition}, removals...))
		if err != nil {
			return false, err
		}
		return true, patchStatus(ctx, profiles, existing.Name, types.JSONPatchType, patch,
			metav1.PatchOptions{FieldManager: statusFieldManager})
	}
	status := statusFromCluster(existing, cluster, ownedFields)
	if statusApplied(existing, ownedFields, status) {
		return false, nil
	}
	payload, err := json.Marshal(map[string]any{
		"apiVersion": cpv1alpha2.GroupVersion.String(),
		"kind":       cpv1alpha2.ClusterProfileKind,
		"metadata": map[string]any{
			"name": existing.Name, "namespace": existing.Namespace,
			"uid": existing.UID, "resourceVersion": existing.ResourceVersion,
		},
		"status": status,
	})
	if err != nil {
		return false, err
	}
	// Force ownership: these fields mirror the ManagedCluster, so a field set by another
	// writer must not stop them from syncing.
	return true, patchStatus(ctx, profiles, existing.Name, types.ApplyPatchType, payload,
		metav1.PatchOptions{FieldManager: statusFieldManager, Force: ptr.To(true)})
}

// patchStatus records each status write, as patcher.PatchStatus did.
func patchStatus(ctx context.Context, profiles cpclientv1alpha2.ClusterProfileInterface, name string,
	pt types.PatchType, data []byte, opts metav1.PatchOptions) error {
	startTime := time.Now()
	_, err := profiles.Patch(ctx, name, pt, data, opts, "status")
	patcher.RecordPatcherOperation(patcher.OperationPatchStatus,
		cpv1alpha2.ClusterProfileKind+"."+cpv1alpha2.GroupVersion.Group, err, time.Since(startTime))
	return err
}

// statusFieldOwners returns the status fields we applied and the fields other managers own.
func statusFieldOwners(profile *cpv1alpha2.ClusterProfile) (owned, others *fieldpath.Set, err error) {
	owned, others = &fieldpath.Set{}, &fieldpath.Set{}
	for _, entry := range profile.ManagedFields {
		if entry.FieldsV1 == nil {
			continue
		}
		fields := &fieldpath.Set{}
		if err := fields.FromJSON(entry.FieldsV1.GetRawReader()); err != nil {
			return nil, nil, err
		}
		if entry.Manager == statusFieldManager && entry.Subresource == "status" &&
			entry.Operation == metav1.ManagedFieldsOperationApply {
			owned = fields
		} else {
			others = others.Union(fields)
		}
	}
	return owned, others, nil
}

// orphanedPropertyRemovals removes the entries of removed claims that other managers
// share without owning their value. Pruning only our fields would leave such an entry
// without its required value, and an apply cannot remove fields it never applied.
func orphanedPropertyRemovals(profile *cpv1alpha2.ClusterProfile, cluster *v1.ManagedCluster,
	ownedFields, otherFields *fieldpath.Set) []map[string]any {
	claimed := sets.New[string]()
	for _, claim := range cluster.Status.ClusterClaims {
		claimed.Insert(claim.Name)
	}
	var removals []map[string]any
	// Remove from the end so the indexes of the remaining entries stay valid.
	for i := len(profile.Status.Properties) - 1; i >= 0; i-- {
		name := profile.Status.Properties[i].Name
		if claimed.Has(name) || !ownedFields.Has(valuePath(name)) || otherFields.Has(valuePath(name)) ||
			fieldsBelow(otherFields, propertyPath(name)).Empty() {
			continue
		}
		removals = append(removals, map[string]any{"op": "remove", "path": fmt.Sprintf("/status/properties/%d", i)})
	}
	return removals
}

func syncLabelsFromCluster(profile *cpv1alpha2.ClusterProfile, cluster *v1.ManagedCluster) {
	mclLabels := cluster.GetLabels()
	mclSetLabel := mclLabels[v1beta2.ClusterSetLabel]

	requiredLabels := map[string]string{
		cpv1alpha2.LabelClusterManagerKey:    ClusterProfileManagerName,
		cpv1alpha2.LabelClusterSetKey:        mclSetLabel,
		cpv1alpha2.LabelInventoryMemberIDKey: inventoryMemberID(cluster),
		// Keep the cluster-name label that lifecycle controller added
		v1.ClusterNameLabelKey: cluster.Name,
	}

	modified := false
	resourcemerge.MergeMap(&modified, &profile.Labels, requiredLabels)
}

type clusterProfileStatusApply struct {
	Version    *cpv1alpha2.ClusterVersion `json:"version,omitempty"`
	Properties []propertyApply            `json:"properties,omitempty"`
	Conditions []metav1.Condition         `json:"conditions,omitempty"`
}

// A claim supplies only the property's name and value. Applying a full Property
// would also send its zero lastObservedTime, which another writer may publish.
type propertyApply struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func statusFromCluster(profile *cpv1alpha2.ClusterProfile, cluster *v1.ManagedCluster, ownedFields *fieldpath.Set) clusterProfileStatusApply {
	status := clusterProfileStatusApply{}
	if version := cluster.Status.Version.Kubernetes; version != "" {
		status.Version = &cpv1alpha2.ClusterVersion{Kubernetes: version}
	}
	for _, claim := range cluster.Status.ClusterClaims {
		status.Properties = append(status.Properties, propertyApply{Name: claim.Name, Value: claim.Value})
	}
	for _, mapping := range []struct{ source, target string }{
		{v1.ManagedClusterConditionAvailable, cpv1alpha2.ClusterConditionControlPlaneHealthy},
		{v1.ManagedClusterConditionJoined, "Joined"},
	} {
		source := meta.FindStatusCondition(cluster.Status.Conditions, mapping.source)
		current := meta.FindStatusCondition(profile.Status.Conditions, mapping.target)
		// Start from the published condition to keep its lastTransitionTime. Without a
		// source, keep reapplying our last condition but leave others' conditions alone.
		// A condition counts as ours when we own any of its fields: pruning only those
		// would leave it without a required field and reject the apply.
		path := conditionPath(mapping.target)
		if current != nil && (source != nil || ownedFields.Has(path) || !fieldsBelow(ownedFields, path).Empty()) {
			previous := *current
			previous.ObservedGeneration = 0 // ManagedCluster does not report the ClusterProfile generation.
			status.Conditions = append(status.Conditions, previous)
		}
		if source != nil {
			meta.SetStatusCondition(&status.Conditions, metav1.Condition{
				Type: mapping.target, Status: source.Status, Reason: source.Reason, Message: source.Message,
			})
		}
	}
	return status
}

// statusApplied reports whether the fields we own already hold the desired status,
// so there is no value to write or release.
func statusApplied(profile *cpv1alpha2.ClusterProfile, ownedFields *fieldpath.Set, desired clusterProfileStatusApply) bool {
	applied := clusterProfileStatusApply{}
	if ownedFields.Has(fieldpath.MakePathOrDie("status", "version", "kubernetes")) {
		applied.Version = &cpv1alpha2.ClusterVersion{Kubernetes: profile.Status.Version.Kubernetes}
	}
	for _, property := range profile.Status.Properties {
		// Earlier releases may have left us only the value. Count it so a removed claim releases it.
		if ownedFields.Has(propertyPath(property.Name)) || ownedFields.Has(valuePath(property.Name)) {
			applied.Properties = append(applied.Properties, propertyApply{Name: property.Name, Value: property.Value})
		}
	}
	for _, condition := range profile.Status.Conditions {
		if ownedFields.Has(conditionPath(condition.Type)) {
			condition.ObservedGeneration = 0
			applied.Conditions = append(applied.Conditions, condition)
		}
	}
	return equality.Semantic.DeepEqual(sortedStatus(applied), sortedStatus(desired))
}

// List order carries no meaning, so compare entries by key.
func sortedStatus(status clusterProfileStatusApply) clusterProfileStatusApply {
	status.Properties = slices.SortedFunc(slices.Values(status.Properties), func(a, b propertyApply) int {
		return strings.Compare(a.Name, b.Name)
	})
	status.Conditions = slices.SortedFunc(slices.Values(status.Conditions), func(a, b metav1.Condition) int {
		return strings.Compare(a.Type, b.Type)
	})
	return status
}

func propertyPath(name string) fieldpath.Path {
	return fieldpath.MakePathOrDie("status", "properties", fieldpath.KeyByFields("name", name))
}

func valuePath(name string) fieldpath.Path {
	return fieldpath.MakePathOrDie("status", "properties", fieldpath.KeyByFields("name", name), "value")
}

func conditionPath(conditionType string) fieldpath.Path {
	return fieldpath.MakePathOrDie("status", "conditions", fieldpath.KeyByFields("type", conditionType))
}

// fieldsBelow returns the fields of set under path.
func fieldsBelow(set *fieldpath.Set, path fieldpath.Path) *fieldpath.Set {
	for _, element := range path {
		set = set.WithPrefix(element)
	}
	return set
}
