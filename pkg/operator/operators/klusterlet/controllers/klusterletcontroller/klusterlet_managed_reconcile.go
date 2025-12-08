/*
 * Copyright 2022 Contributors to the Open Cluster Management project
 */

package klusterletcontroller

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/kubernetes"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/manifests"
	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

var managedStaticResourceFiles = []string{
	"klusterlet/managed/klusterlet-registration-serviceaccount.yaml",
	"klusterlet/managed/klusterlet-registration-clusterrole.yaml",
	"klusterlet/managed/klusterlet-registration-clusterrole-addon-management.yaml",
	"klusterlet/managed/klusterlet-registration-clusterrolebinding.yaml",
	"klusterlet/managed/klusterlet-registration-clusterrolebinding-addon-management.yaml",
	"klusterlet/managed/klusterlet-work-serviceaccount.yaml",
	"klusterlet/managed/klusterlet-work-clusterrole.yaml",
	"klusterlet/managed/klusterlet-work-clusterrole-execution.yaml",
	"klusterlet/managed/klusterlet-work-clusterrolebinding.yaml",
	"klusterlet/managed/klusterlet-work-clusterrolebinding-aggregate.yaml",
	"klusterlet/managed/klusterlet-work-clusterrolebinding-execution-admin.yaml",
}

// managedReconcile apply resources to managed clusters
type managedReconcile struct {
	managedClusterClients *managedClusterClients
	kubeClient            kubernetes.Interface
	operatorNamespace     string
	kubeVersion           *version.Version
	recorder              events.Recorder
	cache                 resourceapply.ResourceCache
	enableSyncLabels      bool
}

func (r *managedReconcile) reconcile(ctx context.Context, klusterlet *operatorapiv1.Klusterlet,
	config klusterletConfig) (*operatorapiv1.Klusterlet, reconcileState, error) {
	labels := helpers.GetKlusterletAgentLabels(klusterlet, r.enableSyncLabels)

	if !config.DisableAddonNamespace {
		// For now, whether in Default or Hosted mode, the addons will be deployed on the managed cluster.
		// sync image pull secret from management cluster to managed cluster for addon namespace
		// Ensure the addon namespace on the managed cluster
		if err := ensureNamespace(
			ctx,
			r.managedClusterClients.kubeClient,
			klusterlet, helpers.DefaultAddonNamespace, labels, r.recorder); err != nil {
			return klusterlet, reconcileStop, err
		}

		// Sync pull secret to the klusterlet addon namespace
		// The reason we keep syncing secret instead of adding a label to trigger addonsecretcontroller to sync is:
		// addonsecretcontroller only watch namespaces in the same cluster klusterlet is running on.
		// And if addons are deployed in default mode on the managed cluster, but klusterlet is deployed in hosted
		// on management cluster, then we still need to sync the secret here in klusterlet-controller using `managedClusterClients.kubeClient`.
		if err := syncPullSecret(
			ctx,
			r.kubeClient,
			r.managedClusterClients.kubeClient,
			klusterlet, r.operatorNamespace, helpers.DefaultAddonNamespace, labels, r.recorder); err != nil {
			return klusterlet, reconcileStop, err
		}
	}

	labels[klusterletNamespaceLabelKey] = klusterlet.Name
	if err := ensureNamespace(
		ctx, r.managedClusterClients.kubeClient, klusterlet, config.KlusterletNamespace, labels, r.recorder); err != nil {
		return klusterlet, reconcileStop, err
	}

	resourceResults := helpers.ApplyDirectly(
		ctx,
		r.managedClusterClients.kubeClient,
		r.managedClusterClients.apiExtensionClient,
		r.recorder,
		r.cache,
		func(name string) ([]byte, error) {
			template, err := manifests.KlusterletManifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			objData := assets.MustCreateAssetFromTemplate(name, template, config).Data
			helpers.SetRelatedResourcesStatusesWithObj(ctx, &klusterlet.Status.RelatedResources, objData)
			return objData, nil
		},
		managedStaticResourceFiles...,
	)

	var errs []error
	for _, result := range resourceResults {
		if result.Error != nil {
			errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}

	// add aggregation clusterrole for work, this is not allowed in library-go for now, so need an additional creating
	if err := r.createAggregationRule(ctx, klusterlet); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		applyErrors := utilerrors.NewAggregate(errs)
		meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
			Type: operatorapiv1.ConditionKlusterletApplied, Status: metav1.ConditionFalse, Reason: operatorapiv1.ReasonManagedClusterResourceApplyFailed,
			Message: applyErrors.Error(),
		})
		return klusterlet, reconcileStop, applyErrors
	}

	return klusterlet, reconcileContinue, nil
}

func (r *managedReconcile) createAggregationRule(ctx context.Context, klusterlet *operatorapiv1.Klusterlet) error {
	aggregateClusterRoleName := fmt.Sprintf("open-cluster-management:%s-work:aggregate", klusterlet.Name)
	_, err := r.managedClusterClients.kubeClient.RbacV1().ClusterRoles().Get(ctx, aggregateClusterRoleName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		aggregateClusterRole := &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: aggregateClusterRoleName,
			},
			AggregationRule: &rbacv1.AggregationRule{
				ClusterRoleSelectors: []metav1.LabelSelector{
					{
						MatchLabels: map[string]string{
							"open-cluster-management.io/aggregate-to-work": "true",
						},
					},
				},
			},
			Rules: []rbacv1.PolicyRule{},
		}
		aggregateClusterRole.SetLabels(helpers.GetKlusterletAgentLabels(klusterlet, r.enableSyncLabels))
		_, createErr := r.managedClusterClients.kubeClient.RbacV1().ClusterRoles().Create(ctx, aggregateClusterRole, metav1.CreateOptions{})
		return createErr
	}

	return nil
}

func (r *managedReconcile) clean(ctx context.Context, klusterlet *operatorapiv1.Klusterlet,
	config klusterletConfig) (*operatorapiv1.Klusterlet, reconcileState, error) {
	// nothing should be done when deploy mode is hosted and hosted finalizer is not added.
	if helpers.IsHosted(klusterlet.Spec.DeployOption.Mode) && !commonhelpers.HasFinalizer(klusterlet.Finalizers, klusterletHostedFinalizer) {
		return klusterlet, reconcileContinue, nil
	}

	if err := r.cleanUpAppliedManifestWorks(ctx, klusterlet, config); err != nil {
		return klusterlet, reconcileStop, err
	}

	if err := removeStaticResources(ctx, r.managedClusterClients.kubeClient, r.managedClusterClients.apiExtensionClient,
		managedStaticResourceFiles, config); err != nil {
		return klusterlet, reconcileStop, err
	}

	// remove aggregate work clusterrole
	aggregateClusterRoleName := fmt.Sprintf("open-cluster-management:%s-work:aggregate", klusterlet.Name)
	if err := r.managedClusterClients.kubeClient.RbacV1().ClusterRoles().Delete(
		ctx, aggregateClusterRoleName, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		return klusterlet, reconcileStop, err
	}

	// remove the klusterlet namespace and klusterlet addon namespace on the managed cluster
	// For now, whether in Default or Hosted mode, the addons could be deployed on the managed cluster.
	namespaces := []string{config.KlusterletNamespace}
	if !config.DisableAddonNamespace {
		// check if other klusterlet agent namespaces still exist before deleting addon namespace
		hasOtherAgents, err := r.hasActiveKlusterletAgentNamespaces(ctx, config.KlusterletNamespace)
		if err != nil {
			return klusterlet, reconcileStop, fmt.Errorf("failed to check for active klusterlet agent namespaces: %v", err)
		}

		// only delete addon namespace if no other klusterlet agents exist
		if !hasOtherAgents {
			namespaces = append(namespaces, helpers.DefaultAddonNamespace)
		}
	}
	for _, namespace := range namespaces {
		if err := r.managedClusterClients.kubeClient.CoreV1().Namespaces().Delete(
			ctx, namespace, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
			return klusterlet, reconcileStop, err
		}
	}

	return klusterlet, reconcileContinue, nil
}

// cleanUpAppliedManifestWorks removes finalizer from the AppliedManifestWorks whose name starts with
// the hash of the given hub host.
func (r *managedReconcile) cleanUpAppliedManifestWorks(ctx context.Context, klusterlet *operatorapiv1.Klusterlet, _ klusterletConfig) error {
	appliedManifestWorks, err := r.managedClusterClients.appliedManifestWorkClient.List(ctx, metav1.ListOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("unable to list AppliedManifestWorks: %w", err)
	}

	if len(appliedManifestWorks.Items) == 0 {
		return nil
	}

	mwpatcher := patcher.NewPatcher[
		*workapiv1.AppliedManifestWork, workapiv1.AppliedManifestWorkSpec, workapiv1.AppliedManifestWorkStatus](
		r.managedClusterClients.appliedManifestWorkClient)

	var errs []error
	for index := range appliedManifestWorks.Items {
		// ignore AppliedManifestWork for other klusterlet
		if string(klusterlet.UID) != appliedManifestWorks.Items[index].Spec.AgentID {
			continue
		}

		// remove finalizer if exists
		if err := mwpatcher.RemoveFinalizer(ctx, &appliedManifestWorks.Items[index], workapiv1.AppliedManifestWorkFinalizer); err != nil {
			errs = append(errs, fmt.Errorf("unable to remove finalizer from AppliedManifestWork %q: %w", appliedManifestWorks.Items[index].Name, err))
		}
	}
	return utilerrors.NewAggregate(errs)
}

// hasActiveKlusterletAgentNamespaces checks if there are any other klusterlet agent namespaces
// on the managed cluster besides the one being deleted. This prevents premature deletion of
// the shared addon namespace when multiple klusterlets (from different hubs) are using it.
func (r *managedReconcile) hasActiveKlusterletAgentNamespaces(ctx context.Context, currentAgentNamespace string) (bool, error) {
	// Look for namespaces with klusterlet labels
	namespaces, err := r.managedClusterClients.kubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: klusterletNamespaceLabelKey,
	})
	if err != nil {
		return false, fmt.Errorf("failed to list klusterlet agent namespaces: %v", err)
	}

	// check if there exist namespaces other than the one being deleted
	if len(namespaces.Items) > 1 || len(namespaces.Items) == 1 && namespaces.Items[0].Name != currentAgentNamespace {
		return true, nil
	}

	return false, nil
}
