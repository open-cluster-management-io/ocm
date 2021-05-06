package placementdecision

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	cache "k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	clusterclient "github.com/open-cluster-management/api/client/cluster/clientset/versioned"
	clusterinformerv1alpha1 "github.com/open-cluster-management/api/client/cluster/informers/externalversions/cluster/v1alpha1"
	clusterlisterv1alpha1 "github.com/open-cluster-management/api/client/cluster/listers/cluster/v1alpha1"
	clusterapiv1alpha1 "github.com/open-cluster-management/api/cluster/v1alpha1"
)

const (
	placementLabel = "cluster.open-cluster-management.io/placement"
)

// placementDecisionCreatingController creates PlacementDecisions for Placements
type placementDecisionCreatingController struct {
	clusterClient           clusterclient.Interface
	placementLister         clusterlisterv1alpha1.PlacementLister
	placementDecisionLister clusterlisterv1alpha1.PlacementDecisionLister
}

// NewPlacementDecisionCreatingController return an instance of placementDecisionCreatingController
func NewPlacementDecisionCreatingController(
	clusterClient clusterclient.Interface,
	placementInformer clusterinformerv1alpha1.PlacementInformer,
	placementDecisionInformer clusterinformerv1alpha1.PlacementDecisionInformer,
	recorder events.Recorder,
) factory.Controller {
	c := placementDecisionCreatingController{
		clusterClient:           clusterClient,
		placementLister:         placementInformer.Lister(),
		placementDecisionLister: placementDecisionInformer.Lister(),
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return key
		}, placementInformer.Informer()).
		WithFilteredEventsInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			labels := accessor.GetLabels()
			placementName := labels[placementLabel]
			return fmt.Sprintf("%s/%s", accessor.GetNamespace(), placementName)
		}, func(obj interface{}) bool {
			accessor, err := meta.Accessor(obj)
			if err != nil {
				return false
			}
			labels := accessor.GetLabels()
			if _, ok := labels[placementLabel]; ok {
				return true
			}
			return false
		}, placementDecisionInformer.Informer()).
		WithSync(c.sync).
		ToController("PlacementDecisionCreatingController", recorder)
}

func (c *placementDecisionCreatingController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	queueKey := syncCtx.QueueKey()
	namespace, name, err := cache.SplitMetaNamespaceKey(queueKey)
	if err != nil {
		// ignore placement whose key is not in format: namespace/name
		return nil
	}

	klog.V(4).Infof("Reconciling placement %q", queueKey)
	placement, err := c.placementLister.Placements(namespace).Get(name)
	if errors.IsNotFound(err) {
		// no work if placement is deleted
		return nil
	}
	if err != nil {
		return err
	}

	// no work if placement is deleting
	if !placement.DeletionTimestamp.IsZero() {
		return nil
	}

	// query placementdecisions with label selector
	requirement, err := labels.NewRequirement(placementLabel, selection.Equals, []string{placement.Name})
	if err != nil {
		return err
	}
	labelSelector := labels.NewSelector().Add(*requirement)
	placementDecisions, err := c.placementDecisionLister.PlacementDecisions(namespace).List(labelSelector)
	if err != nil {
		return err
	}

	// no work if PlacementDecision has been created
	if len(placementDecisions) > 0 {
		return nil
	}

	// otherwise create placementdecision
	return c.createPlacementDecision(ctx, placement)
}

// createPlacementDecision creates PlacementDecision for the Placement
func (c *placementDecisionCreatingController) createPlacementDecision(ctx context.Context, placement *clusterapiv1alpha1.Placement) error {
	owner := metav1.NewControllerRef(placement, clusterapiv1alpha1.GroupVersion.WithKind("Placement"))
	placementDecision := &clusterapiv1alpha1.PlacementDecision{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-", placement.Name),
			Namespace:    placement.Namespace,
			Labels: map[string]string{
				placementLabel: placement.Name,
			},
			OwnerReferences: []metav1.OwnerReference{*owner},
		},
	}

	_, err := c.clusterClient.ClusterV1alpha1().PlacementDecisions(placement.Namespace).Create(ctx, placementDecision, metav1.CreateOptions{})
	return err
}
