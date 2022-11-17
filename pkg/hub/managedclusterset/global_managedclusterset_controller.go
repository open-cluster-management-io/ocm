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
	clustersetv1beta2 "open-cluster-management.io/api/client/cluster/clientset/versioned/typed/cluster/v1beta2"
	clusterinformerv1beta2 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta2"
	clusterlisterv1beta2 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta2"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
)

const (
	GlobalManagedClusterSetName = "global"
)

var GlobalManagedClusterSet = &clusterv1beta2.ManagedClusterSet{
	ObjectMeta: metav1.ObjectMeta{
		Name: GlobalManagedClusterSetName,
	},
	Spec: clusterv1beta2.ManagedClusterSetSpec{
		ClusterSelector: clusterv1beta2.ManagedClusterSelector{
			SelectorType:  clusterv1beta2.LabelSelector,
			LabelSelector: &metav1.LabelSelector{},
		},
	},
}

type globalManagedClusterSetController struct {
	clusterSetClient clustersetv1beta2.ClusterV1beta2Interface
	clusterSetLister clusterlisterv1beta2.ManagedClusterSetLister
	eventRecorder    events.Recorder
}

func NewGlobalManagedClusterSetController(
	clusterSetClient clustersetv1beta2.ClusterV1beta2Interface,
	clusterSetInformer clusterinformerv1beta2.ManagedClusterSetInformer,
	recorder events.Recorder) factory.Controller {

	c := &globalManagedClusterSetController{
		clusterSetClient: clusterSetClient,
		clusterSetLister: clusterSetInformer.Lister(),
		eventRecorder:    recorder.WithComponentSuffix("global-managed-cluster-set-controller"),
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
				// filter clustersets except globalManagedClusterSet.
				return GlobalManagedClusterSetName != metaObj.GetObjectMeta().GetName()
			},
			clusterSetInformer.Informer(),
		).
		WithSync(c.sync).
		// use ResyncEvery to make sure:
		// 1. create the global clusterset once controller is launched
		// 2. the global clusterset be recreated once it is deleted for some reason
		ResyncEvery(10*time.Second).
		ToController("GlobalManagedClusterSetController", recorder)
}

func (c *globalManagedClusterSetController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	klog.V(4).Infof("Reconciling GlobalManagedClusterSet")
	globalClusterSet, err := c.clusterSetLister.Get(GlobalManagedClusterSetName)
	// if the globalClusterSet not found, apply it.
	if err != nil {
		if errors.IsNotFound(err) {
			_, err := c.clusterSetClient.ManagedClusterSets().Create(ctx, GlobalManagedClusterSet, metav1.CreateOptions{})
			if err == nil {
				c.eventRecorder.Eventf("GlobalManagedClusterSetCreated", "Set the GlobalManagedClusterSet name to %+v. spec to %+v", GlobalManagedClusterSetName, GlobalManagedClusterSet.Spec)
			}
			return err
		}
		return err
	}

	if err := c.applyGlobalClusterSet(ctx, globalClusterSet); err != nil {
		return fmt.Errorf("failed to sync GlobalManagedClusterSet %q: %w", GlobalManagedClusterSetName, err)
	}

	return nil
}

// applyGlobalClusterSet syncs global cluster set.
func (c *globalManagedClusterSetController) applyGlobalClusterSet(ctx context.Context, originalGlobalClusterSet *clusterv1beta2.ManagedClusterSet) error {
	globalClusterSet := originalGlobalClusterSet.DeepCopy()

	// if the annotation has set to disable, global clusterset controller will not work.
	if hasAnnotation(globalClusterSet, autoUpdateAnnotation, "false") {
		klog.V(4).Info("GlobalManagedClusterSetDisabled", "The GlobalManagedClusterSet is disabled by user")
		return nil
	}

	// if globalClusterSet.Spec is changed, rollback the change by update it to the original value.
	if !equality.Semantic.DeepEqual(globalClusterSet.Spec, GlobalManagedClusterSet.Spec) {
		globalClusterSet.Spec = GlobalManagedClusterSet.Spec

		_, err := c.clusterSetClient.ManagedClusterSets().Update(ctx, globalClusterSet, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update status of ManagedClusterSet %q: %w", globalClusterSet.Name, err)
		}

		c.eventRecorder.Eventf("GlobalManagedClusterSetSpecRollbacked", "Rollback the GlobalManagedClusterSetSpec to %+v", globalClusterSet.Spec)
	}

	return nil
}
