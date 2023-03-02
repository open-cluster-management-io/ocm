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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/registration-operator/manifests"
	"open-cluster-management.io/registration-operator/pkg/helpers"
)

var (
	managementStaticResourceFiles = []string{
		"klusterlet/management/klusterlet-role-extension-apiserver.yaml",
		"klusterlet/management/klusterlet-registration-serviceaccount.yaml",
		"klusterlet/management/klusterlet-registration-role.yaml",
		"klusterlet/management/klusterlet-registration-rolebinding.yaml",
		"klusterlet/management/klusterlet-registration-rolebinding-extension-apiserver.yaml",
		"klusterlet/management/klusterlet-registration-clusterrole-addon-management.yaml",
		"klusterlet/management/klusterlet-registration-clusterrolebinding-addon-management.yaml",
		"klusterlet/management/klusterlet-work-serviceaccount.yaml",
		"klusterlet/management/klusterlet-work-role.yaml",
		"klusterlet/management/klusterlet-work-rolebinding.yaml",
		"klusterlet/management/klusterlet-work-rolebinding-extension-apiserver.yaml",
	}
)

type managementReconcile struct {
	kubeClient        kubernetes.Interface
	recorder          events.Recorder
	operatorNamespace string
	cache             resourceapply.ResourceCache
}

func (r *managementReconcile) reconcile(ctx context.Context, klusterlet *operatorapiv1.Klusterlet, config klusterletConfig) (*operatorapiv1.Klusterlet, reconcileState, error) {
	err := ensureNamespace(ctx, r.kubeClient, klusterlet, config.AgentNamespace)
	if err != nil {
		return klusterlet, reconcileStop, err
	}

	// Sync pull secret to the agent namespace
	err = syncPullSecret(ctx, r.kubeClient, r.kubeClient, klusterlet, r.operatorNamespace, config.AgentNamespace, r.recorder)
	if err != nil {
		return klusterlet, reconcileStop, err
	}

	resourceResults := helpers.ApplyDirectly(
		ctx,
		r.kubeClient,
		nil,
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
		managementStaticResourceFiles...,
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
			Type: klusterletApplied, Status: metav1.ConditionFalse, Reason: "ManagementClusterResourceApplyFailed",
			Message: applyErrors.Error(),
		})
		return klusterlet, reconcileStop, applyErrors
	}

	return klusterlet, reconcileContinue, nil
}

func (r *managementReconcile) clean(ctx context.Context, klusterlet *operatorapiv1.Klusterlet, config klusterletConfig) (*operatorapiv1.Klusterlet, reconcileState, error) {
	// Remove secrets
	secrets := []string{config.HubKubeConfigSecret}
	if config.InstallMode == operatorapiv1.InstallModeHosted {
		// In Hosted mod, also need to remove the external-managed-kubeconfig-registration and external-managed-kubeconfig-work
		secrets = append(secrets, []string{config.ExternalManagedKubeConfigRegistrationSecret, config.ExternalManagedKubeConfigWorkSecret}...)
	}
	for _, secret := range secrets {
		err := r.kubeClient.CoreV1().Secrets(config.AgentNamespace).Delete(ctx, secret, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return klusterlet, reconcileStop, err
		}
		r.recorder.Eventf("SecretDeleted", "secret %s is deleted", secret)
	}

	// remove static file on the management cluster
	err := removeStaticResources(ctx, r.kubeClient, nil, managementStaticResourceFiles, config)
	if err != nil {
		return klusterlet, reconcileStop, err
	}

	// The agent namespace on the management cluster should be removed **at the end**. Otherwise if any failure occurred,
	// the managed-external-kubeconfig secret would be removed and the next reconcile will fail due to can not build the
	// managed cluster clients.
	if config.InstallMode == operatorapiv1.InstallModeHosted {
		// remove the agent namespace on the management cluster
		err = r.kubeClient.CoreV1().Namespaces().Delete(ctx, config.AgentNamespace, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return klusterlet, reconcileStop, err
		}
	}

	return klusterlet, reconcileContinue, nil
}
