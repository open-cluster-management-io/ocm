package managedclusterset

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	clustersetv1beta2 "open-cluster-management.io/api/client/cluster/clientset/versioned/typed/cluster/v1beta2"
	clusterinformerv1beta2 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta2"
	clusterlisterv1beta2 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta2"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	"open-cluster-management.io/ocm/pkg/common/queue"
)

const (
	autoUpdateAnnotation         = "cluster.open-cluster-management.io/autoupdate"
	DefaultManagedClusterSetName = "default"
)

var DefaultManagedClusterSet = &clusterv1beta2.ManagedClusterSet{
	ObjectMeta: metav1.ObjectMeta{
		Name: DefaultManagedClusterSetName,
	},
	Spec: clusterv1beta2.ManagedClusterSetSpec{
		ClusterSelector: clusterv1beta2.ManagedClusterSelector{
			SelectorType: clusterv1beta2.ExclusiveClusterSetLabel,
		},
	},
}

type defaultManagedClusterSetController struct {
	clusterSetClient clustersetv1beta2.ClusterV1beta2Interface
	clusterSetLister clusterlisterv1beta2.ManagedClusterSetLister
}

func NewDefaultManagedClusterSetController(
	clusterSetClient clustersetv1beta2.ClusterV1beta2Interface,
	clusterSetInformer clusterinformerv1beta2.ManagedClusterSetInformer) factory.Controller {

	c := &defaultManagedClusterSetController{
		clusterSetClient: clusterSetClient,
		clusterSetLister: clusterSetInformer.Lister(),
	}

	return factory.New().
		WithFilteredEventsInformersQueueKeysFunc(
			queue.QueueKeyByMetaName,
			queue.FilterByNames(DefaultManagedClusterSetName),
			clusterSetInformer.Informer(),
		).
		WithSync(c.sync).
		// use ResyncEvery to make sure:
		// 1. create the default clusterset once controller is launched
		// 2. the default clusterset be recreated once it is deleted for some reason
		ResyncEvery(10 * time.Second).
		ToController("DefaultManagedClusterSetController")
}

func (c *defaultManagedClusterSetController) sync(ctx context.Context, syncCtx factory.SyncContext, _ string) error {
	logger := klog.FromContext(ctx)
	logger.V(4).Info("Reconciling DefaultManagedClusterSet")
	defaultClusterSet, err := c.clusterSetLister.Get(DefaultManagedClusterSetName)
	if err != nil {
		// if the defaultClusterSet not found, apply it.
		if errors.IsNotFound(err) {
			_, err := c.clusterSetClient.ManagedClusterSets().Create(ctx, DefaultManagedClusterSet, metav1.CreateOptions{})
			if err == nil {
				syncCtx.Recorder().Eventf(ctx, "DefaultManagedClusterSetCreated",
					"Set the DefaultManagedClusterSet name to %+v. spec to %+v", DefaultManagedClusterSetName, DefaultManagedClusterSet.Spec)
			}
			return err
		}
		return err
	}

	if err := c.syncDefaultClusterSet(ctx, syncCtx, defaultClusterSet); err != nil {
		return fmt.Errorf("failed to sync DefaultManagedClusterSet %q: %w", DefaultManagedClusterSetName, err)
	}

	return nil
}

// syncDefaultClusterSet syncs default cluster set.
func (c *defaultManagedClusterSetController) syncDefaultClusterSet(ctx context.Context, syncCtx factory.SyncContext, originalDefaultClusterSet *clusterv1beta2.ManagedClusterSet) error {
	logger := klog.FromContext(ctx)
	defaultClusterSet := originalDefaultClusterSet.DeepCopy()

	// if the annotation has set to disable, default clusterset controller will not work.
	if hasAnnotation(defaultClusterSet, autoUpdateAnnotation, "false") {
		logger.V(4).Info("DefaultManagedClusterSetDisabled", "reason", "The DefaultManagedClusterSet is disabled by user")
		return nil
	}

	// if defaultClusterSet.Spec.ClusterSelector is changed, rollback the change.
	// All fields except Spec.ClusterSelector are editable.
	if !equality.Semantic.DeepEqual(defaultClusterSet.Spec.ClusterSelector, DefaultManagedClusterSet.Spec.ClusterSelector) {
		defaultClusterSet.Spec.ClusterSelector = DefaultManagedClusterSet.Spec.ClusterSelector

		_, err := c.clusterSetClient.ManagedClusterSets().Update(ctx, defaultClusterSet, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update status of ManagedClusterSet %q: %w", defaultClusterSet.Name, err)
		}

		syncCtx.Recorder().Eventf(ctx, "DefaultManagedClusterSetSpecRollbacked", "Rollback the DefaultManagedClusterSetSpec to %+v", defaultClusterSet.Spec)
	}

	return nil
}

func hasAnnotation(set *clusterv1beta2.ManagedClusterSet, key, value string) bool {
	if set.Annotations == nil {
		return false
	}
	if v, ok := set.Annotations[key]; ok && v == value {
		return true
	}
	return false
}
