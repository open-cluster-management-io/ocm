package registration

import (
	"context"
	"errors"
	"fmt"

	certificatesv1 "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1alpha1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1alpha1"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

// addonRegistrationController reconciles instances of ManagedClusterAddon on the hub.
type addonRegistrationController struct {
	addonClient               addonv1alpha1client.Interface
	managedClusterLister      clusterlister.ManagedClusterLister
	managedClusterAddonLister addonlisterv1alpha1.ManagedClusterAddOnLister
	agentAddons               map[string]agent.AgentAddon
	mcaFilterFunc             utils.ManagedClusterAddOnFilterFunc
}

func NewAddonRegistrationController(
	addonClient addonv1alpha1client.Interface,
	clusterInformers clusterinformers.ManagedClusterInformer,
	addonInformers addoninformerv1alpha1.ManagedClusterAddOnInformer,
	agentAddons map[string]agent.AgentAddon,
	mcaFilterFunc utils.ManagedClusterAddOnFilterFunc,
) factory.Controller {
	c := &addonRegistrationController{
		addonClient:               addonClient,
		managedClusterLister:      clusterInformers.Lister(),
		managedClusterAddonLister: addonInformers.Lister(),
		agentAddons:               agentAddons,
		mcaFilterFunc:             mcaFilterFunc,
	}

	return factory.New().WithFilteredEventsInformersQueueKeysFunc(
		func(obj runtime.Object) []string {
			key, _ := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			return []string{key}
		},
		func(obj interface{}) bool {
			accessor, _ := meta.Accessor(obj)
			if _, ok := c.agentAddons[accessor.GetName()]; !ok {
				return false
			}

			return true
		},
		addonInformers.Informer()).
		// clusterLister is used, so wait for cache sync
		WithBareInformers(clusterInformers.Informer()).
		WithSync(c.sync).ToController("addon-registration-controller")
}

// buildRegistrationConfigs builds registration configs from new configs and existing registrations.
// For KubeAPIServerClientSignerName, handling depends on kubeClientDriver:
//   - "": uses subject from newConfigs
//   - "token": preserves subject from existing registrations, or empty if not found
//   - "csr": uses subject from newConfigs, or sets default if empty
//
// For other signer names, always uses subject from newConfigs.
func buildRegistrationConfigs(newConfigs, existingRegistrations []addonapiv1alpha1.RegistrationConfig,
	kubeClientDriver, clusterName, addonName string) []addonapiv1alpha1.RegistrationConfig {
	result := []addonapiv1alpha1.RegistrationConfig{}

	for i := range newConfigs {
		config := addonapiv1alpha1.RegistrationConfig{
			SignerName: newConfigs[i].SignerName,
			Subject:    newConfigs[i].Subject,
		}

		// Only apply special handling for KubeAPIServerClientSignerName
		if config.SignerName != certificatesv1.KubeAPIServerClientSignerName {
			result = append(result, config)
			continue
		}

		// If kubeClientDriver is not set, use subject from newConfigs as-is
		if kubeClientDriver == "" {
			result = append(result, config)
			continue
		}

		if kubeClientDriver == "token" {
			// Token driver - preserve existing subject set by agent
			found := false
			for j := range existingRegistrations {
				if existingRegistrations[j].SignerName == config.SignerName {
					config.Subject = existingRegistrations[j].Subject
					found = true
					break
				}
			}
			// If no matching existing registration found, clear subject (agent will set it).
			if !found {
				config.Subject = addonapiv1alpha1.Subject{}
			}
		} else if kubeClientDriver == "csr" {
			// CSR driver - use subject from newConfigs, or set default if empty
			if equality.Semantic.DeepEqual(config.Subject, addonapiv1alpha1.Subject{}) {
				config.Subject = addonapiv1alpha1.Subject{
					User:   agent.DefaultUser(clusterName, addonName, addonName),
					Groups: agent.DefaultGroups(clusterName, addonName),
				}
			}
		}
		// For other drivers, use subject from newConfigs

		result = append(result, config)
	}

	return result
}

func (c *addonRegistrationController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	klog.V(4).Infof("Reconciling addon registration %q", key)

	clusterName, addonName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is not in format: namespace/name
		return nil
	}

	agentAddon, ok := c.agentAddons[addonName]
	if !ok {
		return nil
	}

	// Get ManagedCluster
	managedCluster, err := c.managedClusterLister.Get(clusterName)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	managedClusterAddon, err := c.managedClusterAddonLister.ManagedClusterAddOns(clusterName).Get(addonName)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if c.mcaFilterFunc != nil && !c.mcaFilterFunc(managedClusterAddon) {
		return nil
	}

	managedClusterAddonCopy := managedClusterAddon.DeepCopy()

	// wait until the mca's ownerref is set.
	if !utils.IsOwnedByCMA(managedClusterAddonCopy) {
		klog.Warningf("OwnerReferences is not set for %q", key)
		return nil
	}

	addonPatcher := patcher.NewPatcher[
		*addonapiv1alpha1.ManagedClusterAddOn,
		addonapiv1alpha1.ManagedClusterAddOnSpec,
		addonapiv1alpha1.ManagedClusterAddOnStatus](c.addonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName))

	// patch supported configs
	var supportedConfigs []addonapiv1alpha1.ConfigGroupResource
	for _, config := range agentAddon.GetAgentAddonOptions().SupportedConfigGVRs {
		supportedConfigs = append(supportedConfigs, addonapiv1alpha1.ConfigGroupResource{
			Group:    config.Group,
			Resource: config.Resource,
		})
	}
	managedClusterAddonCopy.Status.SupportedConfigs = supportedConfigs

	statusChanged, err := addonPatcher.PatchStatus(ctx, managedClusterAddonCopy, managedClusterAddonCopy.Status, managedClusterAddon.Status)
	if statusChanged {
		if err != nil {
			return fmt.Errorf("failed to patch status(supported configs) of managedclusteraddon: %w", err)
		}
		return nil
	}

	// if supported configs not change, continue to patch condition RegistrationApplied, status.Registrations and status.Namespace
	registrationOption := agentAddon.GetAgentAddonOptions().Registration
	if registrationOption == nil {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    addonapiv1alpha1.ManagedClusterAddOnRegistrationApplied,
			Status:  metav1.ConditionTrue,
			Reason:  addonapiv1alpha1.RegistrationAppliedNilRegistration,
			Message: "Registration of the addon agent is configured",
		})
		_, err = addonPatcher.PatchStatus(ctx, managedClusterAddonCopy, managedClusterAddonCopy.Status, managedClusterAddon.Status)
		if err != nil {
			return fmt.Errorf("failed to patch status condition(registrationOption nil) of managedclusteraddon: %w", err)
		}
		return nil
	}

	// Track whether permission is ready
	permissionReady := true

	if registrationOption.PermissionConfig != nil {
		err = registrationOption.PermissionConfig(managedCluster, managedClusterAddonCopy)
		if err != nil {
			// Check if this is a subject not ready error
			var subjectErr *agent.SubjectNotReadyError
			if errors.As(err, &subjectErr) {
				klog.Infof("Permission configuration pending for addon %q: %v", key, subjectErr)
				permissionReady = false
			} else {
				// This is a real error, set condition to false and return immediately
				meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
					Type:    addonapiv1alpha1.ManagedClusterAddOnRegistrationApplied,
					Status:  metav1.ConditionFalse,
					Reason:  addonapiv1alpha1.RegistrationAppliedSetPermissionFailed,
					Message: fmt.Sprintf("Failed to set permission for hub agent: %v", err),
				})
				if _, patchErr := addonPatcher.PatchStatus(
					ctx, managedClusterAddonCopy, managedClusterAddonCopy.Status, managedClusterAddon.Status); patchErr != nil {
					return fmt.Errorf("failed to patch status condition (set permission for hub agent) of managedclusteraddon: %w", patchErr)
				}
				return err
			}
		}
	}

	if registrationOption.CSRConfigurations == nil {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    addonapiv1alpha1.ManagedClusterAddOnRegistrationApplied,
			Status:  metav1.ConditionTrue,
			Reason:  addonapiv1alpha1.RegistrationAppliedNilRegistration,
			Message: "Registration of the addon agent is configured",
		})
		_, err = addonPatcher.PatchStatus(ctx, managedClusterAddonCopy, managedClusterAddonCopy.Status, managedClusterAddon.Status)
		if err != nil {
			return fmt.Errorf("failed to patch status condition(CSRConfigurations nil) of managedclusteraddon: %w", err)
		}
		return nil
	}

	configs, err := registrationOption.CSRConfigurations(managedCluster, managedClusterAddonCopy)
	if err != nil {
		return fmt.Errorf("failed to get csr configurations: %w", err)
	}

	managedClusterAddonCopy.Status.Registrations = buildRegistrationConfigs(configs, managedClusterAddon.Status.Registrations,
		managedClusterAddon.Status.KubeClientDriver, clusterName, addonName)

	// explicitly set the default namespace value, since the mca.spec.installNamespace is deprceated and
	//  the addonDeploymentConfig.spec.agentInstallNamespace could be empty
	managedClusterAddonCopy.Status.Namespace = "open-cluster-management-agent-addon"

	// Set the default namespace to registrationOption.Namespace
	if len(registrationOption.Namespace) > 0 {
		managedClusterAddonCopy.Status.Namespace = registrationOption.Namespace
	}

	if len(managedClusterAddonCopy.Spec.InstallNamespace) > 0 {
		managedClusterAddonCopy.Status.Namespace = managedClusterAddonCopy.Spec.InstallNamespace
	}

	if registrationOption.AgentInstallNamespace != nil {
		ns, err := registrationOption.AgentInstallNamespace(managedClusterAddonCopy)
		if err != nil {
			return err
		}
		// Override if agentInstallNamespace or InstallNamespace is specified
		if len(ns) > 0 {
			managedClusterAddonCopy.Status.Namespace = ns
		}
	}

	// Set condition based on whether permission is ready
	if permissionReady {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    addonapiv1alpha1.ManagedClusterAddOnRegistrationApplied,
			Status:  metav1.ConditionTrue,
			Reason:  addonapiv1alpha1.RegistrationAppliedSetPermissionApplied,
			Message: "Registration of the addon agent is configured",
		})
	} else {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    addonapiv1alpha1.ManagedClusterAddOnRegistrationApplied,
			Status:  metav1.ConditionFalse,
			Reason:  "PermissionConfigPending",
			Message: "registration subject not ready",
		})
	}

	_, err = addonPatcher.PatchStatus(ctx, managedClusterAddonCopy, managedClusterAddonCopy.Status, managedClusterAddon.Status)
	if err != nil {
		return fmt.Errorf("failed to patch status condition of managedclusteraddon: %w", err)
	}
	return nil
}
