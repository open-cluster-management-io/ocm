/*
 * Copyright 2022 Contributors to the Open Cluster Management project
 */

package klusterletcontroller

import (
	"context"
	"fmt"
	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/version"
	"k8s.io/client-go/kubernetes"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/registration-operator/manifests"
	"open-cluster-management.io/registration-operator/pkg/helpers"
)

var (
	managedStaticResourceFiles = []string{
		"klusterlet/managed/klusterlet-registration-serviceaccount.yaml",
		"klusterlet/managed/klusterlet-registration-clusterrole.yaml",
		"klusterlet/managed/klusterlet-registration-clusterrole-addon-management.yaml",
		"klusterlet/managed/klusterlet-registration-clusterrolebinding.yaml",
		"klusterlet/managed/klusterlet-registration-clusterrolebinding-addon-management.yaml",
		"klusterlet/managed/klusterlet-work-serviceaccount.yaml",
		"klusterlet/managed/klusterlet-work-clusterrole.yaml",
		"klusterlet/managed/klusterlet-work-clusterrole-execution.yaml",
		"klusterlet/managed/klusterlet-work-clusterrolebinding.yaml",
		"klusterlet/managed/klusterlet-work-clusterrolebinding-execution.yaml",
		"klusterlet/managed/klusterlet-work-clusterrolebinding-execution-admin.yaml",
	}

	kube111StaticResourceFiles = []string{
		"klusterletkube111/klusterlet-registration-operator-clusterrolebinding.yaml",
		"klusterletkube111/klusterlet-work-clusterrolebinding.yaml",
	}
)

// managedReconcile apply resources to managed clusters
type managedReconcile struct {
	managedClusterClients *managedClusterClients
	kubeClient            kubernetes.Interface
	opratorNamespace      string
	kubeVersion           *version.Version
	recorder              events.Recorder
	cache                 resourceapply.ResourceCache
}

func (r *managedReconcile) reconcile(ctx context.Context, klusterlet *operatorapiv1.Klusterlet, config klusterletConfig) (*operatorapiv1.Klusterlet, reconcileState, error) {
	// For now, whether in Default or Hosted mode, the addons will be deployed on the managed cluster.
	// sync image pull secret from management cluster to managed cluster for addon namespace
	// TODO(zhujian7): In the future, we may consider deploy addons on the management cluster in Hosted mode.
	addonNamespace := fmt.Sprintf("%s-addon", config.KlusterletNamespace)
	// Ensure the addon namespace on the managed cluster
	err := ensureNamespace(ctx, r.managedClusterClients.kubeClient, klusterlet, addonNamespace)
	if err != nil {
		return klusterlet, reconcileStop, err
	}
	// Sync pull secret to the klusterlet addon namespace
	// The reason we keep syncing secret instead of adding a label to trigger addonsecretcontroller to sync is:
	// addonsecretcontroller only watch namespaces in the same cluster klusterlet is running on.
	// And if addons are deployed in default mode on the managed cluster, but klusterlet is deployed in hosted on management cluster, then we still need to sync the secret here in klusterlet-controller using `managedClusterClients.kubeClient`.
	err = syncPullSecret(ctx, r.kubeClient, r.managedClusterClients.kubeClient, klusterlet, r.opratorNamespace, addonNamespace, r.recorder)
	if err != nil {
		return klusterlet, reconcileStop, err
	}

	if config.InstallMode == operatorapiv1.InstallModeHosted {
		// In hosted mode, we should ensure the namespace on the managed cluster since
		// some resources(eg:service account) are still deployed on managed cluster.
		err := ensureNamespace(ctx, r.managedClusterClients.kubeClient, klusterlet, config.KlusterletNamespace)
		if err != nil {
			return klusterlet, reconcileStop, err
		}
	}

	managedResource := managedStaticResourceFiles
	// If kube version is less than 1.12, deploy static resource for kube 1.11 at first
	// TODO remove this when we do not support kube 1.11 any longer
	if cnt, err := r.kubeVersion.Compare("v1.12.0"); err == nil && cnt < 0 {
		managedResource = append(managedResource, kube111StaticResourceFiles...)
	}

	resourceResults := helpers.ApplyDirectly(
		ctx,
		r.managedClusterClients.kubeClient,
		r.managedClusterClients.apiExtensionClient,
		nil,
		r.managedClusterClients.dynamicClient,
		r.recorder,
		r.cache,
		func(name string) ([]byte, error) {
			template, err := manifests.KlusterletManifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			objData := assets.MustCreateAssetFromTemplate(name, template, config).Data
			helpers.SetRelatedResourcesStatusesWithObj(&klusterlet.Status.RelatedResources, objData)
			return objData, nil
		},
		managedResource...,
	)

	var errs []error
	for _, result := range resourceResults {
		if result.Error != nil {
			errs = append(errs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}

	if len(errs) > 0 {
		applyErrors := utilerrors.NewAggregate(errs)
		meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
			Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "ManagedClusterResourceApplyFailed",
			Message: applyErrors.Error(),
		})
		return klusterlet, reconcileStop, applyErrors
	}

	return klusterlet, reconcileContinue, nil
}
