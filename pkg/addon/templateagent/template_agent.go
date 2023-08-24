package templateagent

import (
	"context"
	"fmt"

	"github.com/valyala/fasttemplate"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	rbacv1lister "k8s.io/client-go/listers/rbac/v1"
	"k8s.io/klog/v2"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	addonlisterv1alpha1 "open-cluster-management.io/api/client/addon/listers/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	NodePlacementPrivateValueKey = "__NODE_PLACEMENT"
	RegistriesPrivateValueKey    = "__REGISTRIES"
)

// templateBuiltinValues includes the built-in values for crd template agentAddon.
// the values for template config should begin with an uppercase letter, so we need
// to convert it to Values by JsonStructToValues.
// the built-in values can not be overridden by getValuesFuncs
type templateCRDBuiltinValues struct {
	ClusterName           string `json:"CLUSTER_NAME,omitempty"`
	AddonInstallNamespace string `json:"INSTALL_NAMESPACE,omitempty"`
}

// templateDefaultValues includes the default values for crd template agentAddon.
// the values for template config should begin with an uppercase letter, so we need
// to convert it to Values by JsonStructToValues.
// the default values can be overridden by getValuesFuncs
type templateCRDDefaultValues struct {
	HubKubeConfigPath     string `json:"HUB_KUBECONFIG,omitempty"`
	ManagedKubeConfigPath string `json:"MANAGED_KUBECONFIG,omitempty"`
}

type CRDTemplateAgentAddon struct {
	logger             klog.Logger
	getValuesFuncs     []addonfactory.GetValuesFunc
	trimCRDDescription bool

	hubKubeClient       kubernetes.Interface
	addonClient         addonv1alpha1client.Interface
	addonLister         addonlisterv1alpha1.ManagedClusterAddOnLister
	addonTemplateLister addonlisterv1alpha1.AddOnTemplateLister
	rolebindingLister   rbacv1lister.RoleBindingLister
	addonName           string
	agentName           string
}

// NewCRDTemplateAgentAddon creates a CRDTemplateAgentAddon instance
func NewCRDTemplateAgentAddon(
	ctx context.Context,
	addonName, agentName string,
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
		rolebindingLister:   rolebindingLister,
		addonName:           addonName,
		agentName:           agentName,
	}

	return a
}

func (a *CRDTemplateAgentAddon) Manifests(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {

	template, err := a.GetDesiredAddOnTemplateByAddon(addon)
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
	return agent.AgentAddonOptions{
		AddonName:       a.addonName,
		InstallStrategy: nil,
		HealthProber: &agent.HealthProber{
			Type: agent.HealthProberTypeDeploymentAvailability,
		},
		SupportedConfigGVRs: supportedConfigGVRs,
		Registration: &agent.RegistrationOption{
			CSRConfigurations: a.TemplateCSRConfigurationsFunc(),
			PermissionConfig:  a.TemplatePermissionConfigFunc(),
			CSRApproveCheck:   a.TemplateCSRApproveCheckFunc(),
			CSRSign:           a.TemplateCSRSignFunc(),
		},
		AgentDeployTriggerClusterFilter: utils.ClusterImageRegistriesAnnotationChanged,
	}
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
		objects = append(objects, object)
	}

	objects, err = a.decorateObjects(template, objects, presetValues, configValues, privateValues)
	if err != nil {
		return objects, err
	}
	return objects, nil
}

func (a *CRDTemplateAgentAddon) decorateObjects(
	template *addonapiv1alpha1.AddOnTemplate,
	objects []runtime.Object,
	orderedValues orderedValues,
	configValues, privateValues addonfactory.Values) ([]runtime.Object, error) {
	decorators := []deploymentDecorator{
		newEnvironmentDecorator(orderedValues),
		newVolumeDecorator(a.addonName, template),
		newNodePlacementDecorator(privateValues),
		newImageDecorator(privateValues),
	}
	for index, obj := range objects {
		deployment, err := utils.ConvertToDeployment(obj)
		if err != nil {
			continue
		}

		for _, decorator := range decorators {
			err = decorator.decorate(deployment)
			if err != nil {
				return objects, err
			}
		}
		objects[index] = deployment
	}

	return objects, nil
}

// GetDesiredAddOnTemplateByAddon returns the desired template of the addon
func (a *CRDTemplateAgentAddon) GetDesiredAddOnTemplateByAddon(
	addon *addonapiv1alpha1.ManagedClusterAddOn) (*addonapiv1alpha1.AddOnTemplate, error) {
	ok, templateRef := AddonTemplateConfigRef(addon.Status.ConfigReferences)
	if !ok {
		a.logger.V(4).Info("Addon template config in status is empty", "addonName", addon.Name)
		return nil, nil
	}

	desiredTemplate := templateRef.DesiredConfig
	if desiredTemplate == nil || desiredTemplate.SpecHash == "" {
		a.logger.Info("Addon template spec hash is empty", "addonName", addon.Name)
		return nil, fmt.Errorf("addon %s template desired spec hash is empty", addon.Name)
	}

	template, err := a.addonTemplateLister.Get(desiredTemplate.Name)
	if err != nil {
		return nil, err
	}

	return template.DeepCopy(), nil
}
