package klusterletcontroller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

// namespaceReconcile is the reconciler to handle the case when spec.namespace is changed, and we need to
// clean the old namespace. The reconciler should be run as the last reconciler, and only operate when
// apply status is true, which ensures that old namespace should only be deleted after the agent in new
// namespace has been deployed.
type namespaceReconcile struct {
	managedClusterClients *managedClusterClients
}

func (r *namespaceReconcile) reconcile(
	ctx context.Context,
	klusterlet *operatorapiv1.Klusterlet,
	config klusterletConfig) (*operatorapiv1.Klusterlet, reconcileState, error) {
	cond := meta.FindStatusCondition(klusterlet.Status.Conditions, operatorapiv1.ConditionKlusterletApplied)
	if cond == nil || cond.Status == metav1.ConditionFalse || klusterlet.Generation != klusterlet.Status.ObservedGeneration {
		return klusterlet, reconcileContinue, nil
	}
	// filters namespace for klusterlet and not in hosted mode
	namespaces, err := r.managedClusterClients.kubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf(
			"%s=%s",
			klusterletNamespaceLabelKey, klusterlet.Name),
	})
	if err != nil {
		return klusterlet, reconcileStop, err
	}
	skippedNamespaces := sets.New[string](config.KlusterletNamespace)
	if !config.DisableAddonNamespace {
		skippedNamespaces.Insert(helpers.DefaultAddonNamespace)
	}
	for _, ns := range namespaces.Items {
		if skippedNamespaces.Has(ns.Name) {
			continue
		}
		if err := r.managedClusterClients.kubeClient.CoreV1().Namespaces().Delete(
			ctx, ns.Name, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
			return klusterlet, reconcileStop, err
		}
	}
	return klusterlet, reconcileContinue, nil
}

func (r *namespaceReconcile) clean(_ context.Context, klusterlet *operatorapiv1.Klusterlet,
	_ klusterletConfig) (*operatorapiv1.Klusterlet, reconcileState, error) {
	// noop
	return klusterlet, reconcileContinue, nil
}
