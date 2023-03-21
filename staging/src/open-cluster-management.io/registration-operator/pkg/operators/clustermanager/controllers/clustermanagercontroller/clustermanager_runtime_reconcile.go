/*
 * Copyright 2022 Contributors to the Open Cluster Management project
 */

package clustermanagercontroller

import (
	"context"
	"fmt"
	"strings"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/registration-operator/manifests"
	"open-cluster-management.io/registration-operator/pkg/helpers"
)

var (
	// All deployments should be deployed in the management cluster.
	deploymentFiles = []string{
		"cluster-manager/management/cluster-manager-registration-deployment.yaml",
		"cluster-manager/management/cluster-manager-registration-webhook-deployment.yaml",
		"cluster-manager/management/cluster-manager-work-webhook-deployment.yaml",
		"cluster-manager/management/cluster-manager-placement-deployment.yaml",
	}

	addOnManagerDeploymentFiles = []string{
		"cluster-manager/management/cluster-manager-addon-manager-deployment.yaml",
	}
)

type runtimeReconcile struct {
	kubeClient    kubernetes.Interface
	hubKubeClient kubernetes.Interface
	hubKubeConfig *rest.Config

	ensureSAKubeconfigs func(ctx context.Context, clusterManagerName, clusterManagerNamespace string, hubConfig *rest.Config, hubClient, managementClient kubernetes.Interface, recorder events.Recorder) error

	cache    resourceapply.ResourceCache
	recorder events.Recorder
}

func (c *runtimeReconcile) reconcile(ctx context.Context, cm *operatorapiv1.ClusterManager, config manifests.HubConfig) (*operatorapiv1.ClusterManager, reconcileState, error) {
	// If AddOnManager is not enabled, remove related resources
	if operatorapiv1.ComponentModeType(config.AddOnManagerComponentMode) != operatorapiv1.ComponentModeTypeEnable {
		_, _, err := cleanResources(ctx, c.kubeClient, cm, config, addOnManagerDeploymentFiles...)
		if err != nil {
			return cm, reconcileStop, err
		}
	}

	// In the Hosted mode, ensure the rbac kubeconfig secrets is existed for deployments to mount.
	// In this step, we get serviceaccount token from the hub cluster to form a kubeconfig and set it as a secret on the management cluster.
	// Before this step, the serviceaccounts in the hub cluster and the namespace in the management cluster should be applied first.
	if cm.Spec.DeployOption.Mode == operatorapiv1.InstallModeHosted {
		clusterManagerNamespace := helpers.ClusterManagerNamespace(cm.Name, cm.Spec.DeployOption.Mode)
		err := c.ensureSAKubeconfigs(ctx, cm.Name, clusterManagerNamespace, c.hubKubeConfig, c.hubKubeClient, c.kubeClient, c.recorder)
		if err != nil {
			meta.SetStatusCondition(&cm.Status.Conditions, metav1.Condition{
				Type:    clusterManagerApplied,
				Status:  metav1.ConditionFalse,
				Reason:  "ServiceAccountSyncFailed",
				Message: fmt.Sprintf("Failed to sync service account: %v", err),
			})
			return cm, reconcileStop, err
		}
	}

	// Apply management cluster resources(namespace and deployments).
	// Note: the certrotation-controller will create CABundle after the namespace applied.
	// And CABundle is used to render apiservice resources.
	managementResources := []string{namespaceResource}

	var appliedErrs []error
	resourceResults := helpers.ApplyDirectly(
		ctx,
		c.kubeClient, nil,
		c.recorder,
		c.cache,
		func(name string) ([]byte, error) {
			template, err := manifests.ClusterManagerManifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			objData := assets.MustCreateAssetFromTemplate(name, template, config).Data
			helpers.SetRelatedResourcesStatusesWithObj(&cm.Status.RelatedResources, objData)
			return objData, nil
		},
		managementResources...,
	)
	for _, result := range resourceResults {
		if result.Error != nil {
			appliedErrs = append(appliedErrs, fmt.Errorf("%q (%T): %v", result.File, result.Type, result.Error))
		}
	}

	var progressingDeployments []string
	deployResources := deploymentFiles
	if operatorapiv1.ComponentModeType(config.AddOnManagerComponentMode) == operatorapiv1.ComponentModeTypeEnable {
		deployResources = append(deployResources, addOnManagerDeploymentFiles...)
	}
	for _, file := range deployResources {
		updatedDeployment, currentGeneration, err := helpers.ApplyDeployment(
			ctx,
			c.kubeClient,
			cm.Status.Generations,
			cm.Spec.NodePlacement,
			func(name string) ([]byte, error) {
				template, err := manifests.ClusterManagerManifestFiles.ReadFile(name)
				if err != nil {
					return nil, err
				}
				objData := assets.MustCreateAssetFromTemplate(name, template, config).Data
				helpers.SetRelatedResourcesStatusesWithObj(&cm.Status.RelatedResources, objData)
				return objData, nil
			},
			c.recorder,
			file)
		if err != nil {
			appliedErrs = append(appliedErrs, err)
			continue
		}
		helpers.SetGenerationStatuses(&cm.Status.Generations, currentGeneration)

		if updatedDeployment.Generation != updatedDeployment.Status.ObservedGeneration || *updatedDeployment.Spec.Replicas != updatedDeployment.Status.ReadyReplicas {
			progressingDeployments = append(progressingDeployments, updatedDeployment.Name)
		}
	}

	if len(progressingDeployments) > 0 {
		meta.SetStatusCondition(&cm.Status.Conditions, metav1.Condition{
			Type:    clusterManagerProgressing,
			Status:  metav1.ConditionTrue,
			Reason:  "ClusterManagerDeploymentRolling",
			Message: fmt.Sprintf("Deployments %s is still rolling", strings.Join(progressingDeployments, ",")),
		})
	} else {
		meta.SetStatusCondition(&cm.Status.Conditions, metav1.Condition{
			Type:    clusterManagerProgressing,
			Status:  metav1.ConditionFalse,
			Reason:  "ClusterManagerUpToDate",
			Message: "Components of cluster manager are up to date",
		})
	}

	if len(appliedErrs) > 0 {
		meta.SetStatusCondition(&cm.Status.Conditions, metav1.Condition{
			Type:    clusterManagerApplied,
			Status:  metav1.ConditionFalse,
			Reason:  "RuntimeResourceApplyFailed",
			Message: fmt.Sprintf("Failed to apply runtime resources: %v", utilerrors.NewAggregate(appliedErrs)),
		})
		return cm, reconcileStop, utilerrors.NewAggregate(appliedErrs)
	}

	return cm, reconcileContinue, nil
}

func (c *runtimeReconcile) clean(ctx context.Context, cm *operatorapiv1.ClusterManager, config manifests.HubConfig) (*operatorapiv1.ClusterManager, reconcileState, error) {
	// Remove All Static files
	managementResources := []string{namespaceResource} // because namespace is removed, we don't need to remove deployments explicitly
	return cleanResources(ctx, c.kubeClient, cm, config, managementResources...)
}

// getSAs return serviceaccount names of all hub components
func getSAs(clusterManagerName string) []string {
	return []string{
		clusterManagerName + "-registration-controller-sa",
		clusterManagerName + "-registration-webhook-sa",
		clusterManagerName + "-work-webhook-sa",
		clusterManagerName + "-placement-controller-sa",
	}
}
