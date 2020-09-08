package managedclusterset

import (
	"context"
	"fmt"
	"reflect"

	clientset "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	clusterinformerv1 "github.com/open-cluster-management/api/client/cluster/informers/externalversions/cluster/v1"
	clusterinformerv1alpha1 "github.com/open-cluster-management/api/client/cluster/informers/externalversions/cluster/v1alpha1"
	clusterlisterv1 "github.com/open-cluster-management/api/client/cluster/listers/cluster/v1"
	clusterlisterv1alpha1 "github.com/open-cluster-management/api/client/cluster/listers/cluster/v1alpha1"
	clusterv1alpha1 "github.com/open-cluster-management/api/cluster/v1alpha1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
)

// managedClusterSetController reconciles instances of ManagedClusterSet on the hub.
type managedClusterSetController struct {
	clusterClient    clientset.Interface
	clusterLister    clusterlisterv1.ManagedClusterLister
	clusterSetLister clusterlisterv1alpha1.ManagedClusterSetLister
	eventRecorder    events.Recorder
}

// NewManagedClusterSetController creates a new managed cluster set controller
func NewManagedClusterSetController(
	clusterClient clientset.Interface,
	clusterInformer clusterinformerv1.ManagedClusterInformer,
	clusterSetInformer clusterinformerv1alpha1.ManagedClusterSetInformer,
	recorder events.Recorder) factory.Controller {
	c := &managedClusterSetController{
		clusterClient:    clusterClient,
		clusterLister:    clusterInformer.Lister(),
		clusterSetLister: clusterSetInformer.Lister(),
		eventRecorder:    recorder.WithComponentSuffix("managed-cluster-set-controller"),
	}
	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, clusterSetInformer.Informer()).
		// TODO, create a PR for library-go to accept a queue key function, which
		// is able to produce a []string instead of string based on one object, when
		// registering event handler. And then refactor the logic here.
		WithInformers(clusterInformer.Informer()).
		WithSync(c.sync).
		ToController("ManagedClusterSetController", recorder)
}

func (c *managedClusterSetController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()

	// if the sync is triggered by change of ManagedCluster, reconcile all cluster sets
	if key == "key" {
		// TODO: instead of resync all clustersets, sync those which might be impacted by
		// the ManagedCluster change only
		if err := c.syncAllClusterSets(ctx); err != nil {
			return fmt.Errorf("failed to sync all ManagedClusterSets: %w", err)
		}
		return nil
	}

	clusterSet, err := c.clusterSetLister.Get(key)
	if errors.IsNotFound(err) {
		// cluster set not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}

	if err = c.syncClusterSet(ctx, clusterSet); err != nil {
		return fmt.Errorf("failed to sync ManagedClusterSet %q: %w", key, err)
	}

	return nil
}

// syncAllClusterSets syncs all cluster sets
func (c *managedClusterSetController) syncAllClusterSets(ctx context.Context) error {
	clusterSets, err := c.clusterSetLister.List(labels.Everything())
	if err != nil {
		return err
	}

	errs := []error{}
	for _, clusterSet := range clusterSets {
		if err = c.syncClusterSet(ctx, clusterSet); err != nil {
			errs = append(errs, err)
		}
	}

	return operatorhelpers.NewMultiLineAggregate(errs)
}

// syncClusterSet syncs a particular cluster set
func (c *managedClusterSetController) syncClusterSet(ctx context.Context, clusterSet *clusterv1alpha1.ManagedClusterSet) error {
	klog.V(4).Infof("Reconciling ManagedClusterSet %s", clusterSet.Name)
	// no work to do if the cluster set is deleted
	if !clusterSet.DeletionTimestamp.IsZero() {
		return nil
	}

	selectedClusters := sets.NewString()
	selectedCondition := metav1.Condition{
		Type: clusterv1alpha1.ManagedClusterSetConditionEmpty,
	}

	errs := []error{}
	for _, selector := range clusterSet.Spec.ClusterSelectors {
		clusters, err := c.selectClusters(selector)
		if err != nil {
			errs = append(errs, err)
		}
		selectedClusters.Insert(clusters...)
	}

	if count := selectedClusters.Len(); count == 0 {
		selectedCondition.Status = metav1.ConditionTrue
		selectedCondition.Reason = "NoClusterMatched"
		selectedCondition.Message = "No ManagedCluster selected"
	} else {
		selectedCondition.Status = metav1.ConditionFalse
		selectedCondition.Reason = "ClustersSelected"
		selectedCondition.Message = fmt.Sprintf("%d ManagedClusters selected", count)
	}

	if err := c.updateClusterSetStatus(ctx, clusterSet, selectedCondition); err != nil {
		errs = append(errs, err)
	}

	return operatorhelpers.NewMultiLineAggregate(errs)
}

// selectClusters returns names of managed clusters which match the cluster selector
func (c *managedClusterSetController) selectClusters(selector clusterv1alpha1.ClusterSelector) (clusterNames []string, err error) {
	switch {
	case len(selector.ClusterNames) > 0 && selector.LabelSelector != nil:
		// return error if both ClusterNames and LabelSelector is specified for they are mutually exclusive
		// This case should be handled by validating webhook
		return nil, fmt.Errorf("both ClusterNames and LabelSelector is specified in ClusterSelector: %v", selector.LabelSelector)
	case len(selector.ClusterNames) > 0:
		// select clusters with cluster names
		for _, clusterName := range selector.ClusterNames {
			_, err = c.clusterLister.Get(clusterName)
			switch {
			case errors.IsNotFound(err):
				continue
			case err != nil:
				return nil, fmt.Errorf("unable to fetch ManagedCluster %q: %w", clusterName, err)
			default:
				clusterNames = append(clusterNames, clusterName)
			}
		}
		return clusterNames, nil
	case selector.LabelSelector != nil:
		// select clusters with label selector
		labelSelector, err := convertLabels(selector.LabelSelector)
		if err != nil {
			// This case should be handled by validating webhook
			return nil, fmt.Errorf("invalid label selector: %v, %w", selector.LabelSelector, err)
		}
		clusters, err := c.clusterLister.List(labelSelector)
		if err != nil {
			return nil, fmt.Errorf("unable to list ManagedClusters with label selector: %v, %w", selector.LabelSelector, err)
		}
		for _, cluster := range clusters {
			clusterNames = append(clusterNames, cluster.Name)
		}
		return clusterNames, nil
	default:
		// no cluster selected if neither ClusterNames nor LabelSelector is specified
		return clusterNames, nil
	}
}

// updateClusterSetStatus updates the status of cluster set with a new status condition
// No work if the status of the cluster set does not change
func (c *managedClusterSetController) updateClusterSetStatus(ctx context.Context, clusterSet *clusterv1alpha1.ManagedClusterSet, newCondition metav1.Condition) error {
	newClusterSet := clusterSet.DeepCopy()
	meta.SetStatusCondition(&newClusterSet.Status.Conditions, newCondition)

	// skip update if cluster set status does not change
	if reflect.DeepEqual(clusterSet.Status.Conditions, newClusterSet.Status.Conditions) {
		return nil
	}

	_, err := c.clusterClient.ClusterV1alpha1().ManagedClusterSets().UpdateStatus(ctx, newClusterSet, metav1.UpdateOptions{})
	return err
}

// convertLabels returns label
func convertLabels(labelSelector *metav1.LabelSelector) (labels.Selector, error) {
	if labelSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(labelSelector)
		if err != nil {
			return labels.Nothing(), err
		}

		return selector, nil
	}

	return labels.Everything(), nil
}
