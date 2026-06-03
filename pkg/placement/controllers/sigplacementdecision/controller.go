package sigplacementdecision

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	cpv1alpha1 "sigs.k8s.io/cluster-inventory-api/apis/v1alpha1"
	cpclientset "sigs.k8s.io/cluster-inventory-api/client/clientset/versioned"
	cpinformerv1alpha1 "sigs.k8s.io/cluster-inventory-api/client/informers/externalversions/apis/v1alpha1"
	cplisterv1alpha1 "sigs.k8s.io/cluster-inventory-api/client/listers/apis/v1alpha1"

	informerv1beta1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta1"
	listerv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	v1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

const (
	SchedulerName = "open-cluster-management"
)

// sigPlacementDecisionController syncs OCM PlacementDecision to SIG MC PlacementDecision.
// Queue key: namespace/name of the OCM PlacementDecision.
type sigPlacementDecisionController struct {
	ocmPDLister listerv1beta1.PlacementDecisionLister
	sigPDClient cpclientset.Interface
	sigPDLister cplisterv1alpha1.PlacementDecisionLister
}

func NewSIGPlacementDecisionController(
	ocmPDInformer informerv1beta1.PlacementDecisionInformer,
	sigPDClient cpclientset.Interface,
	sigPDInformer cpinformerv1alpha1.PlacementDecisionInformer,
) factory.Controller {
	c := &sigPlacementDecisionController{
		ocmPDLister: ocmPDInformer.Lister(),
		sigPDClient: sigPDClient,
		sigPDLister: sigPDInformer.Lister(),
	}

	return factory.New().
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaNamespaceName, ocmPDInformer.Informer()).
		WithInformersQueueKeysFunc(c.sigPDToQueueKey, sigPDInformer.Informer()).
		WithSync(c.sync).
		ToController("SIGPlacementDecisionController")
}

func (c *sigPlacementDecisionController) sigPDToQueueKey(obj runtime.Object) []string {
	sigPD, ok := obj.(*cpv1alpha1.PlacementDecision)
	if !ok {
		return nil
	}
	if sigPD.Labels[cpv1alpha1.LabelClusterManagerKey] != SchedulerName {
		return nil
	}
	key, _ := cache.MetaNamespaceKeyFunc(sigPD)
	return []string{key}
}

func (c *sigPlacementDecisionController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	logger := klog.FromContext(ctx).WithValues("namespace", namespace, "name", name)

	logger.V(4).Info("Reconciling SIG PlacementDecision")

	ocmPD, err := c.ocmPDLister.PlacementDecisions(namespace).Get(name)
	if errors.IsNotFound(err) {
		return c.deleteSIGPlacementDecision(ctx, logger, namespace, name)
	}
	if err != nil {
		return err
	}

	return c.syncSIGPlacementDecision(ctx, logger, syncCtx, ocmPD)
}

func (c *sigPlacementDecisionController) deleteSIGPlacementDecision(
	ctx context.Context, logger klog.Logger, namespace, name string) error {
	existing, err := c.sigPDLister.PlacementDecisions(namespace).Get(name)
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if existing.Labels[cpv1alpha1.LabelClusterManagerKey] != SchedulerName {
		return nil
	}

	err = c.sigPDClient.ApisV1alpha1().PlacementDecisions(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	logger.V(2).Info("Deleted SIG PlacementDecision")
	return nil
}

func (c *sigPlacementDecisionController) syncSIGPlacementDecision(
	ctx context.Context, logger klog.Logger, syncCtx factory.SyncContext,
	ocmPD *v1beta1.PlacementDecision) error {

	desired := buildSIGPlacementDecision(ocmPD)

	existing, err := c.sigPDLister.PlacementDecisions(ocmPD.Namespace).Get(ocmPD.Name)
	if errors.IsNotFound(err) {
		_, err = c.sigPDClient.ApisV1alpha1().PlacementDecisions(ocmPD.Namespace).Create(ctx, desired, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create SIG PlacementDecision %s/%s: %w", ocmPD.Namespace, ocmPD.Name, err)
		}
		logger.V(2).Info("Created SIG PlacementDecision")
		syncCtx.Recorder().Eventf(ctx, "SIGPlacementDecisionCreated",
			"created SIG PlacementDecision %s/%s", ocmPD.Namespace, ocmPD.Name)
		return nil
	}
	if err != nil {
		return err
	}

	if existing.Labels[cpv1alpha1.LabelClusterManagerKey] != SchedulerName {
		return nil
	}

	return c.updateSIGPlacementDecision(ctx, logger, syncCtx, existing, desired)
}

func (c *sigPlacementDecisionController) updateSIGPlacementDecision(
	ctx context.Context, logger klog.Logger, syncCtx factory.SyncContext,
	existing *cpv1alpha1.PlacementDecision, desired *cpv1alpha1.PlacementDecision) error {

	if sigPDEqual(existing, desired) {
		return nil
	}

	updated := existing.DeepCopy()
	updated.Labels = desired.Labels
	updated.Decisions = desired.Decisions
	updated.SchedulerName = desired.SchedulerName

	_, err := c.sigPDClient.ApisV1alpha1().PlacementDecisions(updated.Namespace).Update(ctx, updated, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update SIG PlacementDecision %s/%s: %w", updated.Namespace, updated.Name, err)
	}

	logger.V(2).Info("Updated SIG PlacementDecision")
	syncCtx.Recorder().Eventf(ctx, "SIGPlacementDecisionUpdated",
		"updated SIG PlacementDecision %s/%s", updated.Namespace, updated.Name)
	return nil
}

func buildSIGPlacementDecision(ocmPD *v1beta1.PlacementDecision) *cpv1alpha1.PlacementDecision {
	labels := map[string]string{
		cpv1alpha1.LabelClusterManagerKey: SchedulerName,
	}

	if placementName, ok := ocmPD.Labels[v1beta1.PlacementLabel]; ok {
		labels[cpv1alpha1.PlacementKeyLabel] = placementName
		labels[cpv1alpha1.DecisionKeyLabel] = placementName
	}

	if groupIndex, ok := ocmPD.Labels[v1beta1.DecisionGroupIndexLabel]; ok {
		labels[cpv1alpha1.DecisionIndexLabel] = groupIndex
	}

	decisions := make([]cpv1alpha1.ClusterDecision, 0, len(ocmPD.Status.Decisions))
	for _, d := range ocmPD.Status.Decisions {
		decisions = append(decisions, cpv1alpha1.ClusterDecision{
			ClusterProfileRef: cpv1alpha1.ClusterProfileReference{
				Name: d.ClusterName,
			},
			Reason: d.Reason,
		})
	}

	return &cpv1alpha1.PlacementDecision{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ocmPD.Name,
			Namespace: ocmPD.Namespace,
			Labels:    labels,
		},
		Decisions:     decisions,
		SchedulerName: SchedulerName,
	}
}

func sigPDEqual(existing *cpv1alpha1.PlacementDecision, desired *cpv1alpha1.PlacementDecision) bool {
	if existing.SchedulerName != desired.SchedulerName {
		return false
	}

	if len(existing.Decisions) != len(desired.Decisions) {
		return false
	}
	for i := range existing.Decisions {
		if existing.Decisions[i].ClusterProfileRef.Name != desired.Decisions[i].ClusterProfileRef.Name ||
			existing.Decisions[i].ClusterProfileRef.Namespace != desired.Decisions[i].ClusterProfileRef.Namespace ||
			existing.Decisions[i].Reason != desired.Decisions[i].Reason {
			return false
		}
	}

	for key, desiredVal := range desired.Labels {
		if existingVal, ok := existing.Labels[key]; !ok || existingVal != desiredVal {
			return false
		}
	}

	return true
}
