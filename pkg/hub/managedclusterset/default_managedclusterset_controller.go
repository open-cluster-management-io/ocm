package managedclusterset

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	clustersetv1beta1 "open-cluster-management.io/api/client/cluster/clientset/versioned/typed/cluster/v1beta1"
	clusterinformerv1beta1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta1"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
)

const defaultManagedClusterSetName = "default"

var defaultManagedClusterSetSpec = clusterv1beta1.ManagedClusterSetSpec{
	ClusterSelector: clusterv1beta1.ManagedClusterSelector{
		SelectorType: clusterv1beta1.LegacyClusterSetLabel,
	},
}

var defaultManagedClusterSet = &clusterv1beta1.ManagedClusterSet{
	ObjectMeta: metav1.ObjectMeta{
		Name: defaultManagedClusterSetName,
	},
	Spec: defaultManagedClusterSetSpec,
}

type defaultManagedClusterSetController struct {
	clusterSetClient clustersetv1beta1.ClusterV1beta1Interface
	clusterSetLister clusterlisterv1beta1.ManagedClusterSetLister
	eventRecorder    events.Recorder
}

func NewDefaultManagedClusterSetController(
	clusterSetClient clustersetv1beta1.ClusterV1beta1Interface,
	clusterSetInformer clusterinformerv1beta1.ManagedClusterSetInformer,
	recorder events.Recorder) factory.Controller {

	c := &defaultManagedClusterSetController{
		clusterSetClient: clusterSetClient,
		clusterSetLister: clusterSetInformer.Lister(),
		eventRecorder:    recorder.WithComponentSuffix("default-managed-cluster-set-controller"),
	}

	return factory.New().
		WithFilteredEventsInformersQueueKeyFunc(
			func(obj runtime.Object) string {
				accessor, _ := meta.Accessor(obj)
				return accessor.GetName()
			},
			func(obj interface{}) bool {
				metaObj, ok := obj.(metav1.ObjectMetaAccessor)
				if !ok {
					return false
				}
				// filter clustersets except defaultManagedClusterSet.
				return defaultManagedClusterSetName != metaObj.GetObjectMeta().GetName()
			},
			clusterSetInformer.Informer(),
		).
		WithSync(c.sync).
		// use ResyncEvery to make sure:
		// 1. create the default clusterset once controller is launched
		// 2. the default clusterset be recreated once it is deleted for some reason
		ResyncEvery(10*time.Second).
		ToController("DefaultManagedClusterSetController", recorder)
}

func (c *defaultManagedClusterSetController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	defaultClusterSetName := defaultManagedClusterSetName
	klog.Infof("Reconciling DefaultManagedClusterSet")

	defaultClusterSet, err := c.clusterSetLister.Get(defaultClusterSetName)

	// if the defaultClusterSet not found, apply it.
	if errors.IsNotFound(err) {
		_, err := c.clusterSetClient.ManagedClusterSets().Create(ctx, defaultManagedClusterSet, metav1.CreateOptions{})
		if err == nil {
			c.eventRecorder.Eventf("DefaultManagedClusterSetCreated", "Set the DefaultManagedClusterSet name to %+v. spec to %+v", defaultClusterSetName, defaultManagedClusterSetSpec)
		}
		return err

	} else if err != nil {
		return err
	}

	// if defaultClusterSet is terminating, add the key to the controller queue with a second-long delay
	if !defaultClusterSet.DeletionTimestamp.IsZero() {
		syncCtx.Queue().AddAfter(defaultClusterSetName, 5*time.Second)
		return nil
	}

	if err := c.syncDefaultClusterSet(ctx, defaultClusterSet); err != nil {
		return fmt.Errorf("failed to sync DefaultManagedClusterSet %q: %w", defaultClusterSetName, err)
	}

	return nil
}

// syncDefaultClusterSet syncs default cluster set.
func (c *defaultManagedClusterSetController) syncDefaultClusterSet(ctx context.Context, originalDefaultClusterSet *clusterv1beta1.ManagedClusterSet) error {
	defaultClusterSet := originalDefaultClusterSet.DeepCopy()

	// if defaultClusterSet.Spec is changed, rollback the change by update it to the original value.
	if !equality.Semantic.DeepEqual(defaultClusterSet.Spec, defaultManagedClusterSetSpec) {
		defaultClusterSet.Spec = defaultManagedClusterSetSpec

		_, err := c.clusterSetClient.ManagedClusterSets().Update(ctx, defaultClusterSet, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update status of ManagedClusterSet %q: %w", defaultClusterSet.Name, err)
		}

		c.eventRecorder.Eventf("DefaultManagedClusterSetSpecRollbacked", "Rollback the DefaultManagedClusterSetSpec to %+v", defaultClusterSet.Spec)
	}

	return nil
}
