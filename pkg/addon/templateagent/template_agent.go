package templateagent

import (
	"context"
	"fmt"

	"github.com/valyala/fasttemplate"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	rbacv1lister "k8s.io/client-go/listers/rbac/v1"
	"k8s.io/klog/v2"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	addonconstants "open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	// Private value keys that are used internally by the addon template controller, should not be exposed to users.
	// All private value keys should begin with "__"
	NodePlacementPrivateValueKey        = "__NODE_PLACEMENT"
	RegistriesPrivateValueKey           = "__REGISTRIES"
	InstallNamespacePrivateValueKey     = "__INSTALL_NAMESPACE"
	ProxyPrivateValueKey                = "__PROXY"
	ResourceRequirementsPrivateValueKey = "__RESOURCE_REQUIREMENTS"
)

var PrivateValuesKeys = map[string]struct{}{
	NodePlacementPrivateValueKey:        {},
	RegistriesPrivateValueKey:           {},
	InstallNamespacePrivateValueKey:     {},
	ProxyPrivateValueKey:                {},
	ResourceRequirementsPrivateValueKey: {},
}

// templateBuiltinValues includes the built-in values for crd template agentAddon.
// the values for template config should begin with an uppercase letter, so we need
// to convert it to Values by JsonStructToValues.
// the built-in values can not be overridden by getValuesFuncs
type templateCRDBuiltinValues struct {
	ClusterName      string `json:"CLUSTER_NAME,omitempty"`
	InstallNamespace string `json:"INSTALL_NAMESPACE,omitempty"`
}

// templateDefaultValues includes the default values for crd template agentAddon.
// the values for template config should begin with an uppercase letter, so we need
// to convert it to Values by JsonStructToValues.
// the default values can be overridden by getValuesFuncs
type templateCRDDefaultValues struct {
	HubKubeConfigPath string `json:"HUB_KUBECONFIG,omitempty"`
}

type CRDTemplateAgentAddon struct {
	logger             klog.Logger
	getValuesFuncs     []addonfactory.GetValuesFunc
	trimCRDDescription bool

	hubKubeClient       kubernetes.Interface
	addonClient         addonv1alpha1client.Interface
	addonLister         addonlisterv1alpha1.ManagedClusterAddOnLister
	addonTemplateLister addonlisterv1alpha1.AddOnTemplateLister
	cmaLister           addonlisterv1alpha1.ClusterManagementAddOnLister
	rolebindingLister   rbacv1lister.RoleBindingLister
	addonName           string
	agentName           string
}

// NewCRDTemplateAgentAddon creates a CRDTemplateAgentAddon instance
func NewCRDTemplateAgentAddon(
	ctx context.Context,
	addonName string,
	hubKubeClient kubernetes.Interface,
	addonClient addonv1alpha1client.Interface,
	addonInformers addoninformers.SharedInformerFactory,
	rolebindingLister rbacv1lister.RoleBindingLister,
	getValuesFuncs ...addonfactory.GetValuesFunc,
) *CRDTemplateAgentAddon {

	a := &CRDTemplateAgentAddon{
		logger:             klog.FromContext(ctx),
		getValuesFuncs:     getValuesFuncs,
		trimCRDDescription: true,

		hubKubeClient:       hubKubeClient,
		addonClient:         addonClient,
		addonLister:         addonInformers.Addon().V1alpha1().ManagedClusterAddOns().Lister(),
		addonTemplateLister: addonInformers.Addon().V1alpha1().AddOnTemplates().Lister(),
		cmaLister:           addonInformers.Addon().V1alpha1().ClusterManagementAddOns().Lister(),
		rolebindingLister:   rolebindingLister,
		addonName:           addonName,
		agentName:           fmt.Sprintf("%s-agent", addonName),
	}

	return a
}

func (a *CRDTemplateAgentAddon) Manifests(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {

	template, err := a.getDesiredAddOnTemplateInner(addon.Name, addon.Status.ConfigReferences)
	if err != nil {
		return nil, err
	}
	if template == nil {
		return nil, fmt.Errorf("addon %s/%s template not found in status", addon.Namespace, addon.Name)
	}
	return a.renderObjects(cluster, addon, template)
}

func (a *CRDTemplateAgentAddon) GetAgentAddonOptions() agent.AgentAddonOptions {
	// TODO: consider a new way for developers to define their supported config GVRs
	supportedConfigGVRs := []schema.GroupVersionResource{}
	for gvr := range utils.BuiltInAddOnConfigGVRs {
		supportedConfigGVRs = append(supportedConfigGVRs, gvr)
	}

	agentAddonOptions := agent.AgentAddonOptions{
		AddonName: a.addonName,
		HealthProber: &agent.HealthProber{
			Type: agent.HealthProberTypeWorkloadAvailability,
		},
		HostedModeInfoFunc:  addonconstants.GetHostedModeInfo,
		SupportedConfigGVRs: supportedConfigGVRs,
		Registration: &agent.RegistrationOption{
			CSRConfigurations:     a.TemplateCSRConfigurationsFunc(),
			PermissionConfig:      a.TemplatePermissionConfigFunc(),
			CSRApproveCheck:       a.TemplateCSRApproveCheckFunc(),
			CSRSign:               a.TemplateCSRSignFunc(),
			AgentInstallNamespace: a.TemplateAgentRegistrationNamespaceFunc,
		},
		AgentDeployTriggerClusterFilter: func(old, new *clusterv1.ManagedCluster) bool {
			return utils.ClusterImageRegistriesAnnotationChanged(old, new) ||
				// if the cluster changes from unknow to true, recheck the health of the addon immediately
				utils.ClusterAvailableConditionChanged(old, new)
		},
		// enable the ConfigCheckEnabled flag to check the configured condition before rendering manifests
		ConfigCheckEnabled: true,
	}

	template, err := a.GetDesiredAddOnTemplate(nil, "", a.addonName)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("GetAgentAddonOptions failed to get addon %s template: %v", a.addonName, err))
		return agentAddonOptions
	}
	if template == nil {
		utilruntime.HandleError(fmt.Errorf("GetAgentAddonOptions addon %s template is nil", a.addonName))
		return agentAddonOptions
	}
	agentAddonOptions.ManifestConfigs = template.Spec.AgentSpec.ManifestConfigs

	return agentAddonOptions
}

func (a *CRDTemplateAgentAddon) renderObjects(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn,
	template *addonapiv1alpha1.AddOnTemplate) ([]runtime.Object, error) {
	var objects []runtime.Object
	presetValues, configValues, privateValues, err := a.getValues(cluster, addon, template)
	if err != nil {
		return objects, err
	}
	a.logger.V(4).Info("Logging presetValues, configValues, and privateValues",
		"presetValues", presetValues,
		"configValues", configValues,
		"privateValues", privateValues)

	for _, manifest := range template.Spec.AgentSpec.Workload.Manifests {
		t := fasttemplate.New(string(manifest.Raw), "{{", "}}")
		manifestStr := t.ExecuteString(configValues)
		a.logger.V(4).Info("Addon render result",
			"addonNamespace", addon.Namespace,
			"addonName", addon.Name,
			"renderResult", manifestStr)
		object := &unstructured.Unstructured{}
		if err := object.UnmarshalJSON([]byte(manifestStr)); err != nil {
			return objects, err
		}

		object, err = a.decorateObject(template, object, presetValues, privateValues)
		if err != nil {
			return objects, err
		}
		objects = append(objects, object)
	}

	additionalObjects, err := a.injectAdditionalObjects(template, presetValues, privateValues)
	if err != nil {
		return objects, err
	}
	objects = append(objects, additionalObjects...)

	return objects, nil
}

func (a *CRDTemplateAgentAddon) decorateObject(
	template *addonapiv1alpha1.AddOnTemplate,
	obj *unstructured.Unstructured,
	orderedValues orderedValues,
	privateValues addonfactory.Values) (*unstructured.Unstructured, error) {
	decorators := []decorator{
		newDeploymentDecorator(a.logger, a.addonName, template, orderedValues, privateValues),
		newDaemonSetDecorator(a.logger, a.addonName, template, orderedValues, privateValues),
		newNamespaceDecorator(privateValues),
	}

	var err error
	for _, decorator := range decorators {
		obj, err = decorator.decorate(obj)
		if err != nil {
			return obj, err
		}
	}

	return obj, nil
}

func (a *CRDTemplateAgentAddon) injectAdditionalObjects(
	template *addonapiv1alpha1.AddOnTemplate,
	orderedValues orderedValues,
	privateValues addonfactory.Values) ([]runtime.Object, error) {
	injectors := []objectsInjector{
		newProxyHandler(a.logger, a.addonName, privateValues),
	}

	decorators := []decorator{
		// decorate the namespace of the additional objects
		newNamespaceDecorator(privateValues),
	}

	var objs []runtime.Object
	for _, injector := range injectors {
		objects, err := injector.inject()
		if err != nil {
			return nil, err
		}

		for _, object := range objects {
			// convert the runtime.Object to unstructured.Unstructured
			unstructuredMapObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(object)
			if err != nil {
				return nil, err
			}
			unstructuredObject := &unstructured.Unstructured{Object: unstructuredMapObj}

			for _, decorator := range decorators {
				unstructuredObject, err = decorator.decorate(unstructuredObject)
				if err != nil {
					return nil, err
				}
			}

			objs = append(objs, unstructuredObject)
		}

	}

	return objs, nil
}

// getDesiredAddOnTemplateInner returns the desired template of the addon,
// if the desired template is not found in the configReferences, it will
// return nil and no error, the caller should handle the nil template case.
func (a *CRDTemplateAgentAddon) getDesiredAddOnTemplateInner(
	addonName string, configReferences []addonapiv1alpha1.ConfigReference,
) (*addonapiv1alpha1.AddOnTemplate, error) {
	ok, templateRef := AddonTemplateConfigRef(configReferences)
	if !ok {
		a.logger.Info("Addon template config in addon status is empty", "addonName", addonName)
		return nil, nil
	}

	desiredTemplate := templateRef.DesiredConfig
	if desiredTemplate == nil || desiredTemplate.SpecHash == "" {
		a.logger.Info("Addon template spec hash is empty", "addonName", addonName)
		return nil, fmt.Errorf("addon %s template desired spec hash is empty", addonName)
	}

	template, err := a.addonTemplateLister.Get(desiredTemplate.Name)
	if err != nil {
		return nil, err
	}

	return template.DeepCopy(), nil
}

// TemplateAgentRegistrationNamespaceFunc reads deployment/daemonset resources in the manifests and use that namespace
// as the default registration namespace. If addonDeploymentConfig is set, uses the namespace in it.
func (a *CRDTemplateAgentAddon) TemplateAgentRegistrationNamespaceFunc(
	addon *addonapiv1alpha1.ManagedClusterAddOn) (string, error) {
	template, err := a.getDesiredAddOnTemplateInner(addon.Name, addon.Status.ConfigReferences)
	if err != nil {
		return "", err
	}
	if template == nil {
		return "", fmt.Errorf("addon %s template not found in status", addon.Name)
	}

	// pick the namespace of the first deployment, if there is no deployment, pick the namespace of the first daemonset
	var desiredNS = "open-cluster-management-agent-addon"
	var firstDeploymentNamespace, firstDaemonSetNamespace string
	for _, manifest := range template.Spec.AgentSpec.Workload.Manifests {
		object := &unstructured.Unstructured{}
		if err := object.UnmarshalJSON(manifest.Raw); err != nil {
			a.logger.Error(err, "failed to extract the object")
			continue
		}

		if firstDeploymentNamespace == "" {
			if _, err = utils.ConvertToDeployment(object); err == nil {
				firstDeploymentNamespace = object.GetNamespace()
				break
			}
		}
		if firstDaemonSetNamespace == "" {
			if _, err = utils.ConvertToDaemonSet(object); err == nil {
				firstDaemonSetNamespace = object.GetNamespace()
			}
		}
	}

	if firstDeploymentNamespace != "" {
		desiredNS = firstDeploymentNamespace
	} else if firstDaemonSetNamespace != "" {
		desiredNS = firstDaemonSetNamespace
	}

	overrideNs, err := utils.AgentInstallNamespaceFromDeploymentConfigFunc(
		utils.NewAddOnDeploymentConfigGetter(a.addonClient))(addon)
	if err != nil {
		return "", err
	}
	if len(overrideNs) > 0 {
		desiredNS = overrideNs
	}
	return desiredNS, nil
}
