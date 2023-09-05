package utils

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
)

// AddOnDeploymentConfigGetter has a method to return a AddOnDeploymentConfig object
type AddOnDeploymentConfigGetter interface {
	Get(ctx context.Context, namespace, name string) (*addonapiv1alpha1.AddOnDeploymentConfig, error)
}

type defaultAddOnDeploymentConfigGetter struct {
	addonClient addonv1alpha1client.Interface
}

// NewAddOnDeploymentConfigGetter returns a AddOnDeploymentConfigGetter with addon client
func NewAddOnDeploymentConfigGetter(addonClient addonv1alpha1client.Interface) AddOnDeploymentConfigGetter {
	return &defaultAddOnDeploymentConfigGetter{addonClient: addonClient}
}

func (g *defaultAddOnDeploymentConfigGetter) Get(
	ctx context.Context, namespace, name string) (*addonapiv1alpha1.AddOnDeploymentConfig, error) {
	return g.addonClient.AddonV1alpha1().AddOnDeploymentConfigs(namespace).Get(ctx, name, metav1.GetOptions{})
}

// AgentInstallNamespaceFromDeploymentConfigFunc returns an agent install namespace helper function which will get the
// namespace from the addon deployment config. If the addon does not support addon deployment config or there is no
// matched addon deployment config, it will return an empty string.
func AgentInstallNamespaceFromDeploymentConfigFunc(
	adcgetter AddOnDeploymentConfigGetter,
) func(*addonapiv1alpha1.ManagedClusterAddOn) string {
	return func(addon *addonapiv1alpha1.ManagedClusterAddOn) string {
		if addon == nil {
			utilruntime.HandleError(fmt.Errorf("failed to get addon install namespace, addon is nil"))
			return ""
		}

		config, err := GetDesiredAddOnDeploymentConfig(addon, adcgetter)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("failed to get deployment config for addon %s: %v", addon.Name, err))
			return ""
		}
		if config == nil {
			return ""
		}

		return config.Spec.AgentInstallNamespace
	}
}

// GetDesiredAddOnDeployment returns the desired addonDeploymentConfig of the addon
func GetDesiredAddOnDeploymentConfig(
	addon *addonapiv1alpha1.ManagedClusterAddOn,
	adcgetter AddOnDeploymentConfigGetter,
) (*addonapiv1alpha1.AddOnDeploymentConfig, error) {

	ok, configRef := GetAddOnConfigRef(addon.Status.ConfigReferences,
		AddOnDeploymentConfigGVR.Group, AddOnDeploymentConfigGVR.Resource)
	if !ok {
		klog.InfoS("Addon deployment config in status is empty", "addonName", addon.Name)
		return nil, nil
	}

	desiredConfig := configRef.DesiredConfig
	if desiredConfig == nil || len(desiredConfig.SpecHash) == 0 {
		klog.InfoS("Addon deployment config spec hash is empty", "addonName", addon.Name)
		return nil, fmt.Errorf("addon %s deployment config desired spec hash is empty", addon.Name)
	}

	adc, err := adcgetter.Get(context.TODO(), desiredConfig.Namespace, desiredConfig.Name)
	if err != nil {
		return nil, err
	}

	/* If the addonDeploymentConfig.spec.proxy field is not set, the spec hash in managedclusteraddon status will be
	// different from the spec hash calculated here. This is because the spec hash in managedclusteraddon status is
	// calculated by getting the addon deployment config object using a dynamic client, which will not contain
	// addonDeploymentConfig.spec.proxy field if it is not set. However, the spec hash of the addonDeploymentConfig here
	// is calculated by getting the addon deployment config object using a typed client, which will contain
	// addonDeploymentConfig.spec.proxy field even if it is not set.
	// TODO: uncomment the comparison after the above issue is fixed

	specHash, err := GetAddOnDeploymentConfigSpecHash(adc)
	if err != nil {
		return nil, err
	}
	if specHash != desiredConfig.SpecHash {
		return nil, fmt.Errorf("addon %s deployment config spec hash %s is not equal to desired spec hash %s",
			addon.Name, specHash, desiredConfig.SpecHash)
	}
	*/
	return adc.DeepCopy(), nil
}

// GetAddOnDeploymentConfigSpecHash returns the sha256 hash of the spec field of the addon deployment config
func GetAddOnDeploymentConfigSpecHash(config *addonapiv1alpha1.AddOnDeploymentConfig) (string, error) {
	if config == nil {
		return "", fmt.Errorf("addon deployment config is nil")
	}
	uadc, err := runtime.DefaultUnstructuredConverter.ToUnstructured(config)
	if err != nil {
		return "", err
	}
	return GetSpecHash(&unstructured.Unstructured{
		Object: uadc,
	})
}

// GetAddOnConfigRef returns the first addon config ref for the given config type
func GetAddOnConfigRef(
	configReferences []addonapiv1alpha1.ConfigReference,
	group, resource string) (bool, addonapiv1alpha1.ConfigReference) {

	for _, config := range configReferences {
		if config.Group == group && config.Resource == resource {
			return true, config
		}
	}
	return false, addonapiv1alpha1.ConfigReference{}
}
