/*
 * Copyright 2022 Contributors to the Open Cluster Management project
 */

package clustermanagercontroller

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"

	"open-cluster-management.io/ocm/manifests"
	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

var (
	// The hubWebhookResourceFiles should be deployed in the hub cluster
	// The service should may point to a external url which represent the webhook-server's address.
	hubRegistrationWebhookResourceFiles = []string{
		"cluster-manager/hub/registration/webhook-validatingconfiguration.yaml",
		"cluster-manager/hub/registration/webhook-mutatingconfiguration.yaml",
		"cluster-manager/hub/registration/webhook-clustersetbinding-validatingconfiguration.yaml",
	}
	hubWorkWebhookResourceFiles = []string{
		"cluster-manager/hub/work/webhook-validatingconfiguration.yaml",
	}
)

type webhookReconcile struct {
	kubeClient    kubernetes.Interface
	hubKubeClient kubernetes.Interface

	cache    resourceapply.ResourceCache
	recorder events.Recorder
}

func (c *webhookReconcile) reconcile(ctx context.Context, cm *operatorapiv1.ClusterManager,
	config manifests.HubConfig) (*operatorapiv1.ClusterManager, reconcileState, error) {
	var appliedErrs []error

	if !meta.IsStatusConditionFalse(cm.Status.Conditions, operatorapiv1.ConditionProgressing) {
		return cm, reconcileStop, commonhelpers.NewRequeueError("Deployment is not ready", clusterManagerReSyncTime)
	}

	webhookResources := hubRegistrationWebhookResourceFiles
	webhookResources = append(webhookResources, hubWorkWebhookResourceFiles...)
	// If all webhook pod running , then apply webhook config files
	resourceResults := helpers.ApplyDirectly(
		ctx,
		c.hubKubeClient,
		nil,
		c.recorder,
		c.cache,
		func(name string) ([]byte, error) {
			template, err := manifests.ClusterManagerManifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			objData := assets.MustCreateAssetFromTemplate(name, template, config).Data
			helpers.SetRelatedResourcesStatusesWithObj(ctx, &cm.Status.RelatedResources, objData)
			return objData, nil
		},
		webhookResources...,
	)

	for _, result := range resourceResults {
		if result.Error != nil {
			appliedErrs = append(appliedErrs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}

	if len(appliedErrs) > 0 {
		meta.SetStatusCondition(&cm.Status.Conditions, metav1.Condition{
			Type:    operatorapiv1.ConditionClusterManagerApplied,
			Status:  metav1.ConditionFalse,
			Reason:  operatorapiv1.ReasonWebhookApplyFailed,
			Message: fmt.Sprintf("Failed to apply webhook resources: %v", utilerrors.NewAggregate(appliedErrs)),
		})
		return cm, reconcileStop, utilerrors.NewAggregate(appliedErrs)
	}

	return cm, reconcileContinue, nil
}

func (c *webhookReconcile) clean(ctx context.Context, cm *operatorapiv1.ClusterManager,
	config manifests.HubConfig) (*operatorapiv1.ClusterManager, reconcileState, error) {
	// Remove All webhook files
	webhookResources := hubRegistrationWebhookResourceFiles
	webhookResources = append(webhookResources, hubWorkWebhookResourceFiles...)
	return cleanResources(ctx, c.kubeClient, cm, config, webhookResources...)
}
