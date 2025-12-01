/*
 * Copyright 2022 Contributors to the Open Cluster Management project
 */

package clustermanagercontroller

import (
	"context"
	"fmt"
	"strings"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"

	"open-cluster-management.io/ocm/manifests"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

var (
	// All deployments should be deployed in the management cluster.
	deploymentFiles = []string{
		"cluster-manager/management/registration/deployment.yaml",
		"cluster-manager/management/registration/webhook-deployment.yaml",
		"cluster-manager/management/work/webhook-deployment.yaml",
		"cluster-manager/management/placement/deployment.yaml",
		"cluster-manager/management/addon-manager/webhook-deployment.yaml",
	}

	addOnManagerDeploymentFiles = []string{
		"cluster-manager/management/addon-manager/deployment.yaml",
	}

	workControllerDeploymentFiles = []string{
		"cluster-manager/management/work/deployment.yaml",
	}

	grpcServerDeploymentFiles = []string{
		"cluster-manager/management/grpc-server/deployment.yaml",
	}
)

type runtimeReconcile struct {
	kubeClient    kubernetes.Interface
	hubKubeClient kubernetes.Interface
	hubKubeConfig *rest.Config

	ensureSAKubeconfigs func(ctx context.Context, clusterManagerName, clusterManagerNamespace string,
		hubConfig *rest.Config, hubClient, managementClient kubernetes.Interface, recorder events.Recorder,
		mwctrEnabled, addonManagerEnabled, grpcAuthEnabled bool) error

	cache    resourceapply.ResourceCache
	recorder events.Recorder
}

func (c *runtimeReconcile) reconcile(ctx context.Context, cm *operatorapiv1.ClusterManager,
	config manifests.HubConfig) (*operatorapiv1.ClusterManager, reconcileState, error) {
	// If AddOnManager is not enabled, remove related resources
	if !config.AddOnManagerEnabled {
		_, _, err := cleanResources(ctx, c.kubeClient, cm, config, addOnManagerDeploymentFiles...)
		if err != nil {
			return cm, reconcileStop, err
		}
	}

	// Remove work-controller deployment if feature not enabled
	if !config.WorkControllerEnabled {
		_, _, err := cleanResources(ctx, c.kubeClient, cm, config, workControllerDeploymentFiles...)
		if err != nil {
			return cm, reconcileStop, err
		}
	}

	// Remove grpc server deployment if the grpc auth is disabled
	if !config.GRPCAuthEnabled {
		_, _, err := cleanResources(ctx, c.kubeClient, cm, config, grpcServerDeploymentFiles...)
		if err != nil {
			return cm, reconcileStop, err
		}
	}

	if cm.Spec.RegistrationConfiguration != nil && cm.Spec.RegistrationConfiguration.RegistrationDrivers != nil {
		var enabledRegistrationDrivers []string
		for _, registrationDriver := range cm.Spec.RegistrationConfiguration.RegistrationDrivers {
			enabledRegistrationDrivers = append(enabledRegistrationDrivers, registrationDriver.AuthType)
			switch registrationDriver.AuthType {
			case operatorapiv1.AwsIrsaAuthType:
				if registrationDriver.AwsIrsa != nil {
					config.HubClusterArn = registrationDriver.AwsIrsa.HubClusterArn
					config.AutoApprovedARNPatterns = strings.Join(registrationDriver.AwsIrsa.AutoApprovedIdentities, ",")
					config.AwsResourceTags = strings.Join(registrationDriver.AwsIrsa.Tags, ",")
				}
			case operatorapiv1.CSRAuthType:
				if registrationDriver.CSR != nil {
					config.AutoApprovedCSRUsers = strings.Join(registrationDriver.CSR.AutoApprovedIdentities, ",")
				}
			case operatorapiv1.GRPCAuthType:
				if registrationDriver.GRPC != nil {
					config.GRPCAutoApprovedUsers = strings.Join(registrationDriver.GRPC.AutoApprovedIdentities, ",")
				}

				// Prefer ServerConfiguration image when set; otherwise fall back to registration image.
				if sc := cm.Spec.ServerConfiguration; sc == nil || strings.TrimSpace(sc.ImagePullSpec) == "" {
					config.GRPCServerImage = cm.Spec.RegistrationImagePullSpec
				} else {
					config.GRPCServerImage = sc.ImagePullSpec
				}
			}
		}
		config.EnabledRegistrationDrivers = strings.Join(enabledRegistrationDrivers, ",")
	}

	// In the Hosted mode, ensure the rbac kubeconfig secrets is existed for deployments to mount.
	// In this step, we get serviceaccount token from the hub cluster to form a kubeconfig and set it as a secret on the management cluster.
	// Before this step, the serviceaccounts in the hub cluster and the namespace in the management cluster should be applied first.
	if helpers.IsHosted(cm.Spec.DeployOption.Mode) {
		clusterManagerNamespace := helpers.ClusterManagerNamespace(cm.Name, cm.Spec.DeployOption.Mode)
		err := c.ensureSAKubeconfigs(ctx, cm.Name, clusterManagerNamespace,
			c.hubKubeConfig, c.hubKubeClient, c.kubeClient, c.recorder,
			config.WorkControllerEnabled, config.AddOnManagerEnabled, config.GRPCAuthEnabled)
		if err != nil {
			meta.SetStatusCondition(&cm.Status.Conditions, metav1.Condition{
				Type:    operatorapiv1.ConditionClusterManagerApplied,
				Status:  metav1.ConditionFalse,
				Reason:  operatorapiv1.ReasonServiceAccountSyncFailed,
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
			helpers.SetRelatedResourcesStatusesWithObj(ctx, &cm.Status.RelatedResources, objData)
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
	if config.AddOnManagerEnabled {
		deployResources = append(deployResources, addOnManagerDeploymentFiles...)
	}
	if config.WorkControllerEnabled {
		deployResources = append(deployResources, workControllerDeploymentFiles...)
	}
	if config.GRPCAuthEnabled {
		deployResources = append(deployResources, grpcServerDeploymentFiles...)
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
				helpers.SetRelatedResourcesStatusesWithObj(ctx, &cm.Status.RelatedResources, objData)
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
			Type:    operatorapiv1.ConditionProgressing,
			Status:  metav1.ConditionTrue,
			Reason:  operatorapiv1.ReasonDeploymentRolling,
			Message: fmt.Sprintf("Deployments %s is still rolling", strings.Join(progressingDeployments, ",")),
		})
	} else {
		meta.SetStatusCondition(&cm.Status.Conditions, metav1.Condition{
			Type:    operatorapiv1.ConditionProgressing,
			Status:  metav1.ConditionFalse,
			Reason:  operatorapiv1.ReasonUpToDate,
			Message: "Components of cluster manager are up to date",
		})
	}

	if len(appliedErrs) > 0 {
		meta.SetStatusCondition(&cm.Status.Conditions, metav1.Condition{
			Type:    operatorapiv1.ConditionClusterManagerApplied,
			Status:  metav1.ConditionFalse,
			Reason:  operatorapiv1.ReasonRuntimeResourceApplyFailed,
			Message: fmt.Sprintf("Failed to apply runtime resources: %v", utilerrors.NewAggregate(appliedErrs)),
		})
		return cm, reconcileStop, utilerrors.NewAggregate(appliedErrs)
	}

	return cm, reconcileContinue, nil
}

func (c *runtimeReconcile) clean(ctx context.Context, cm *operatorapiv1.ClusterManager,
	config manifests.HubConfig) (*operatorapiv1.ClusterManager, reconcileState, error) {
	// Remove All Static files
	managementResources := []string{namespaceResource} // because namespace is removed, we don't need to remove deployments explicitly
	return cleanResources(ctx, c.kubeClient, cm, config, managementResources...)
}

// getSAs return serviceaccount names of all hub components
func getSAs(mwctrEnabled, addonManagerEnabled, grpcAuthEnabled bool) []string {
	sas := []string{
		"registration-controller-sa",
		"registration-webhook-sa",
		"work-webhook-sa",
		"placement-controller-sa",
	}
	if mwctrEnabled {
		sas = append(sas, "work-controller-sa")
	}
	if addonManagerEnabled {
		sas = append(sas, "addon-manager-controller-sa")
	}
	if grpcAuthEnabled {
		sas = append(sas, "grpc-server-sa")
	}
	return sas
}
