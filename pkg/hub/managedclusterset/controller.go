package managedclusterset

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	clientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterinformerv1beta1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta1"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

const (
	clusterSetLabel = "cluster.open-cluster-management.io/clusterset"
)

// managedClusterSetController reconciles instances of ManagedClusterSet on the hub.
type managedClusterSetController struct {
	clusterClient    clientset.Interface
	clusterLister    clusterlisterv1.ManagedClusterLister
	clusterSetLister clusterlisterv1beta1.ManagedClusterSetLister
	eventRecorder    events.Recorder

	// clusterSetsMap caches the mappings between clusters and clustersets.
	// With the mappings, it's easy to find out which clustersets are impacted on a
	// change of cluster.
	clusterSetsMap map[string]string

	// mapLock protects the read/write of clusterSetsMap
	mapLock sync.RWMutex
}

// NewManagedClusterSetController creates a new managed cluster set controller
func NewManagedClusterSetController(
	clusterClient clientset.Interface,
	clusterInformer clusterinformerv1.ManagedClusterInformer,
	clusterSetInformer clusterinformerv1beta1.ManagedClusterSetInformer,
	recorder events.Recorder) factory.Controller {
	c := &managedClusterSetController{
		clusterClient:    clusterClient,
		clusterLister:    clusterInformer.Lister(),
		clusterSetLister: clusterSetInformer.Lister(),
		eventRecorder:    recorder.WithComponentSuffix("managed-cluster-set-controller"),

		clusterSetsMap: map[string]string{},
	}
	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, clusterSetInformer.Informer()).
		// register two event handlers to cluster informer. One haneles the current parent
		// clusterset, and the other handles the original parent clusterset. The order of
		// the registering matters. originalClusterSetQueueKeyFunc must be called before
		// currentClusterSetQueueKeyFunc is called, otherwise clusterSetsMap will be updated
		// unexpectedly
		//
		// TODO, create a PR for library-go to accept a queue key function, which
		// is able to produce a []string instead of string based on one object, when
		// registering event handler. And then refactor the logic here.
		WithInformersQueueKeyFunc(c.originalClusterSetQueueKeyFunc, clusterInformer.Informer()).
		WithInformersQueueKeyFunc(c.currentClusterSetQueueKeyFunc, clusterInformer.Informer()).
		WithSync(c.sync).
		ToController("ManagedClusterSetController", recorder)
}

// originalClusterSetQueueKeyFunc returns the original clusterset the cluster previously
// belonged to.
func (c *managedClusterSetController) originalClusterSetQueueKeyFunc(obj runtime.Object) string {
	c.mapLock.RLock()
	defer c.mapLock.RUnlock()

	accessor, _ := meta.Accessor(obj)
	clusterName := accessor.GetName()
	originalClusterSetName := c.clusterSetsMap[clusterName]

	var currentClusterSetName string
	if clusterLabels := accessor.GetLabels(); clusterLabels != nil {
		currentClusterSetName = clusterLabels[clusterSetLabel]
	}

	// return the name of clusterset it previously belonged to only if the parent clusterset
	// is changed
	if originalClusterSetName != currentClusterSetName {
		return originalClusterSetName
	}
	return ""
}

// currentClusterSetQueueKeyFunc returns the current clusterset the cluster currently
// belongs to.
func (c *managedClusterSetController) currentClusterSetQueueKeyFunc(obj runtime.Object) string {
	accessor, _ := meta.Accessor(obj)

	// always return the name of clusterset it currently belongs to
	if clusterLabels := accessor.GetLabels(); clusterLabels != nil {
		return clusterLabels[clusterSetLabel]
	}

	return ""
}

func (c *managedClusterSetController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	clusterSetName := syncCtx.QueueKey()
	if len(clusterSetName) == 0 {
		return nil
	}
	klog.Infof("Reconciling ManagedClusterSet %s", clusterSetName)

	clusterSet, err := c.clusterSetLister.Get(clusterSetName)
	if errors.IsNotFound(err) {
		// cluster set not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}

	// no work to do if the cluster set is deleted
	if !clusterSet.DeletionTimestamp.IsZero() {
		return nil
	}

	if err := c.syncClusterSet(ctx, clusterSet); err != nil {
		return fmt.Errorf("failed to sync ManagedClusterSet %q: %w", clusterSetName, err)
	}
	return nil
}

// syncClusterSet syncs a particular cluster set
func (c *managedClusterSetController) syncClusterSet(ctx context.Context, originalClusterSet *clusterv1beta1.ManagedClusterSet) error {
	clusterSet := originalClusterSet.DeepCopy()

	// find out the containing clusters of clusterset
	selector := labels.SelectorFromSet(labels.Set{
		clusterSetLabel: clusterSet.Name,
	})
	clusters, err := c.clusterLister.List(selector)
	if err != nil {
		return fmt.Errorf("failed to list ManagedClusters: %w", err)
	}

	//update the cluster-to-clusterset mappings
	c.updateClusterSetsMap(clusterSet.Name, clusters)

	// update clusterset status
	emptyCondition := metav1.Condition{
		Type: clusterv1beta1.ManagedClusterSetConditionEmpty,
	}
	if count := len(clusters); count == 0 {
		emptyCondition.Status = metav1.ConditionTrue
		emptyCondition.Reason = "NoClusterMatched"
		emptyCondition.Message = "No ManagedCluster selected"
	} else {
		emptyCondition.Status = metav1.ConditionFalse
		emptyCondition.Reason = "ClustersSelected"
		emptyCondition.Message = fmt.Sprintf("%d ManagedClusters selected", count)
	}
	meta.SetStatusCondition(&clusterSet.Status.Conditions, emptyCondition)

	// skip update if cluster set status does not change
	if reflect.DeepEqual(clusterSet.Status.Conditions, originalClusterSet.Status.Conditions) {
		return nil
	}

	_, err = c.clusterClient.ClusterV1beta1().ManagedClusterSets().UpdateStatus(ctx, clusterSet, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update status of ManagedClusterSet %q: %w", clusterSet.Name, err)
	}

	return nil
}

// updateClusterSetsMap updates the cluster-to-clusterset mappings with memebers of a
// given cluster set
func (c *managedClusterSetController) updateClusterSetsMap(clusterSetName string, clusters []*clusterv1.ManagedCluster) {
	c.mapLock.Lock()
	defer c.mapLock.Unlock()

	newMembers := sets.NewString()
	for _, cluster := range clusters {
		newMembers.Insert(cluster.Name)
	}

	originalMembers := sets.NewString()
	for c, cs := range c.clusterSetsMap {
		if cs != clusterSetName {
			continue
		}
		originalMembers.Insert(c)
	}

	toBeAdded := newMembers.Difference(originalMembers)
	toBeRemoved := originalMembers.Difference(newMembers)
	for clusterName := range toBeAdded {
		c.clusterSetsMap[clusterName] = clusterSetName
	}
	for clusterName := range toBeRemoved {
		delete(c.clusterSetsMap, clusterName)
	}
}
