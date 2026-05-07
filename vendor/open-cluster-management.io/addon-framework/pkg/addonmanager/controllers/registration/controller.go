package registration

import (
	"context"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformerv1beta1 "open-cluster-management.io/api/client/addon/informers/externalversions/addon/v1beta1"
	addonlisterv1beta1 "open-cluster-management.io/api/client/addon/listers/addon/v1beta1"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlister "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"
)

// addonRegistrationController reconciles instances of ManagedClusterAddon on the hub.
type addonRegistrationController struct {
	addonClient               addonclient.Interface
	managedClusterLister      clusterlister.ManagedClusterLister
	managedClusterAddonLister addonlisterv1beta1.ManagedClusterAddOnLister
	agentAddons               map[string]agent.AgentAddon
	mcaFilterFunc             utils.ManagedClusterAddOnFilterFunc
}

func NewAddonRegistrationController(
	addonClient addonclient.Interface,
	clusterInformers clusterinformers.ManagedClusterInformer,
	addonInformers addoninformerv1beta1.ManagedClusterAddOnInformer,
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

// findExistingRegistration finds matching existing registration for a config.
// For KubeClient type, we match by index (assuming order is stable).
// For CustomSigner type, we match by SignerName.
func findExistingRegistration(config *addonapiv1beta1.RegistrationConfig, index int,
	newConfigs []agent.RegistrationConfig, existingRegistrations []addonapiv1beta1.RegistrationConfig) *addonapiv1beta1.RegistrationConfig {
	if config.Type == addonapiv1beta1.KubeClient {
		// For KubeClient, try to match by index first
		// Count how many KubeClient configs we've seen so far
		kubeClientIndex := 0
		for i := 0; i < index; i++ {
			if _, ok := newConfigs[i].(*agent.KubeClientRegistration); ok {
				kubeClientIndex++
			}
		}

		// Find the corresponding KubeClient in existingRegistrations
		currentIndex := 0
		for j := range existingRegistrations {
			if existingRegistrations[j].Type == addonapiv1beta1.KubeClient {
				if currentIndex == kubeClientIndex {
					return &existingRegistrations[j]
				}
				currentIndex++
			}
		}
	} else if config.Type == addonapiv1beta1.CustomSigner && config.CustomSigner != nil {
		// For CustomSigner, match by SignerName
		for j := range existingRegistrations {
			if existingRegistrations[j].Type == addonapiv1beta1.CustomSigner &&
				existingRegistrations[j].CustomSigner != nil &&
				existingRegistrations[j].CustomSigner.SignerName == config.CustomSigner.SignerName {
				return &existingRegistrations[j]
			}
		}
	}
	return nil
}

// buildRegistrationConfigs builds registration configs from new configs and existing registrations.
// In v1beta1, RegistrationConfig uses Type-based structure (KubeClient or CustomSigner).
// For KubeClient type, handling depends on kubeClientDriver:
//   - "": uses subject from newConfigs
//   - "token": preserves subject from existing registrations, or empty if not found
//   - "csr": uses subject from newConfigs, or sets default if empty
//
// For CustomSigner type, always uses subject from newConfigs.
func buildRegistrationConfigs(newConfigs []agent.RegistrationConfig, existingRegistrations []addonapiv1beta1.RegistrationConfig,
	clusterName, addonName string) []addonapiv1beta1.RegistrationConfig {
	result := []addonapiv1beta1.RegistrationConfig{}

	for i := range newConfigs {
		config := newConfigs[i].RegistrationAPI()

		// Only apply special handling for KubeClient type
		if config.Type != addonapiv1beta1.KubeClient {
			result = append(result, config)
			continue
		}

		// Ensure KubeClient config exists
		if config.KubeClient == nil {
			config.KubeClient = &addonapiv1beta1.KubeClientConfig{}
		}

		// Find matching existing registration
		existingReg := findExistingRegistration(&config, i, newConfigs, existingRegistrations)

		// Get driver from existing registration if available, otherwise use driver from newConfig
		driver := config.KubeClient.Driver
		if existingReg != nil && existingReg.KubeClient != nil && existingReg.KubeClient.Driver != "" {
			driver = existingReg.KubeClient.Driver
		}

		// Set the driver
		config.KubeClient.Driver = driver

		// If driver is empty, use config as-is (subject from newConfigs)
		if driver == "" {
			result = append(result, config)
			continue
		}

		if driver == "token" {
			// Token driver - preserve existing subject set by agent
			if existingReg != nil && existingReg.KubeClient != nil {
				config.KubeClient.Subject = existingReg.KubeClient.Subject
			} else {
				// If no matching existing registration found, clear subject (agent will set it).
				config.KubeClient.Subject = addonapiv1beta1.KubeClientSubject{}
			}
		} else if driver == "csr" {
			// CSR driver - use subject from newConfigs, or set default if empty
			if equality.Semantic.DeepEqual(config.KubeClient.Subject, addonapiv1beta1.KubeClientSubject{}) {
				config.KubeClient.Subject = addonapiv1beta1.KubeClientSubject{
					BaseSubject: addonapiv1beta1.BaseSubject{
						User:   agent.DefaultUser(clusterName, addonName, addonName),
						Groups: agent.DefaultGroups(clusterName, addonName),
					},
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
		*addonapiv1beta1.ManagedClusterAddOn,
		addonapiv1beta1.ManagedClusterAddOnSpec,
		addonapiv1beta1.ManagedClusterAddOnStatus](c.addonClient.AddonV1beta1().ManagedClusterAddOns(clusterName))

	// patch supported configs
	var supportedConfigs []addonapiv1beta1.ConfigGroupResource
	for _, config := range agentAddon.GetAgentAddonOptions().SupportedConfigGVRs {
		supportedConfigs = append(supportedConfigs, addonapiv1beta1.ConfigGroupResource{
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
			Type:    addonapiv1beta1.ManagedClusterAddOnRegistrationApplied,
			Status:  metav1.ConditionTrue,
			Reason:  addonapiv1beta1.RegistrationAppliedNilRegistration,
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
		err = registrationOption.PermissionConfig(ctx, managedCluster, managedClusterAddonCopy)
		if err != nil {
			// Check if this is a subject not ready error
			var subjectErr *agent.SubjectNotReadyError
			if errors.As(err, &subjectErr) {
				klog.Infof("Permission configuration pending for addon %q: %v", key, subjectErr)
				permissionReady = false
			} else {
				// This is a real error, set condition to false and return immediately
				meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
					Type:    addonapiv1beta1.ManagedClusterAddOnRegistrationApplied,
					Status:  metav1.ConditionFalse,
					Reason:  addonapiv1beta1.RegistrationAppliedSetPermissionFailed,
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

	if registrationOption.Configurations == nil {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    addonapiv1beta1.ManagedClusterAddOnRegistrationApplied,
			Status:  metav1.ConditionTrue,
			Reason:  addonapiv1beta1.RegistrationAppliedNilRegistration,
			Message: "Registration of the addon agent is configured",
		})
		_, err = addonPatcher.PatchStatus(ctx, managedClusterAddonCopy, managedClusterAddonCopy.Status, managedClusterAddon.Status)
		if err != nil {
			return fmt.Errorf("failed to patch status condition(CSRConfigurations nil) of managedclusteraddon: %w", err)
		}
		return nil
	}

	configs, err := registrationOption.Configurations(ctx, managedCluster, managedClusterAddonCopy)
	if err != nil {
		return fmt.Errorf("failed to get csr configurations: %w", err)
	}

	managedClusterAddonCopy.Status.Registrations = buildRegistrationConfigs(configs, managedClusterAddon.Status.Registrations,
		clusterName, addonName)

	// explicitly set the default namespace value, since the mca.spec.installNamespace is deprceated and
	//  the addonDeploymentConfig.spec.agentInstallNamespace could be empty
	// Priority (lowest to highest): default < annotation < registrationOption.Namespace < AgentInstallNamespace function
	managedClusterAddonCopy.Status.Namespace = "open-cluster-management-agent-addon"

	// Override with annotation if present
	if installNs, ok := managedClusterAddonCopy.Annotations[addonapiv1beta1.InstallNamespaceAnnotation]; ok && len(installNs) > 0 {
		managedClusterAddonCopy.Status.Namespace = installNs
	}

	// Override with AgentInstallNamespace function if provided (highest priority)
	if agentAddon.GetAgentAddonOptions().AgentInstallNamespace != nil {
		ns, err := agentAddon.GetAgentAddonOptions().AgentInstallNamespace(ctx, managedClusterAddonCopy)
		if err != nil {
			return err
		}
		if len(ns) > 0 {
			managedClusterAddonCopy.Status.Namespace = ns
		}
	}

	// Set condition based on whether permission is ready
	if permissionReady {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    addonapiv1beta1.ManagedClusterAddOnRegistrationApplied,
			Status:  metav1.ConditionTrue,
			Reason:  addonapiv1beta1.RegistrationAppliedSetPermissionApplied,
			Message: "Registration of the addon agent is configured",
		})
	} else {
		meta.SetStatusCondition(&managedClusterAddonCopy.Status.Conditions, metav1.Condition{
			Type:    addonapiv1beta1.ManagedClusterAddOnRegistrationApplied,
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
