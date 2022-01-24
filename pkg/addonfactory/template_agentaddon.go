package addonfactory

import (
	"fmt"

	"github.com/openshift/library-go/pkg/assets"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// templateBuiltinValues includes the built-in values for template agentAddon.
// the values for template config should begin with an uppercase letter, so we need convert it to Values by StructToValues.
type templateBuiltinValues struct {
	ClusterName           string
	AddonInstallNamespace string
	HubKubeConfigSecret   string
}

type TemplateAgentAddon struct {
	decoder           runtime.Decoder
	templateData      map[string][]byte
	getValuesFuncs    []GetValuesFunc
	agentAddonOptions agent.AgentAddonOptions
}

func newTemplateAgentAddon(
	scheme *runtime.Scheme,
	getValuesFuncs []GetValuesFunc,
	agentAddonOptions agent.AgentAddonOptions) *TemplateAgentAddon {
	return &TemplateAgentAddon{
		decoder: serializer.NewCodecFactory(scheme).UniversalDeserializer(),

		getValuesFuncs:    getValuesFuncs,
		agentAddonOptions: agentAddonOptions}
}

func (a *TemplateAgentAddon) Manifests(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	var objects []runtime.Object

	configValues, err := a.getValues(cluster, addon)
	if err != nil {
		return objects, err
	}
	for file, data := range a.templateData {
		raw := assets.MustCreateAssetFromTemplate(file, data, configValues).Data
		object, _, err := a.decoder.Decode(raw, nil, nil)
		if err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}

	return objects, nil
}

func (a *TemplateAgentAddon) GetAgentAddonOptions() agent.AgentAddonOptions {
	return a.agentAddonOptions
}

func (a *TemplateAgentAddon) getValues(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (Values, error) {
	overrideValues := map[string]interface{}{}

	for i := 0; i < len(a.getValuesFuncs); i++ {
		if a.getValuesFuncs[i] != nil {
			userValues, err := a.getValuesFuncs[i](cluster, addon)
			if err != nil {
				return overrideValues, err
			}
			overrideValues = MergeValues(overrideValues, userValues)
		}
	}
	builtinValues := a.getBuiltinValues(cluster, addon)
	overrideValues = MergeValues(overrideValues, builtinValues)

	return overrideValues, nil
}

func (a *TemplateAgentAddon) getBuiltinValues(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) Values {
	builtinValues := templateBuiltinValues{}
	builtinValues.ClusterName = cluster.GetName()

	installNamespace := addon.Spec.InstallNamespace
	if len(installNamespace) == 0 {
		installNamespace = AddonDefaultInstallNamespace
	}
	builtinValues.AddonInstallNamespace = installNamespace

	// TODO: hubKubeConfigSecret depends on the signer configuration in registration, and the registration is an array.
	if a.agentAddonOptions.Registration != nil {
		builtinValues.HubKubeConfigSecret = fmt.Sprintf("%s-hub-kubeconfig", a.agentAddonOptions.AddonName)
	}

	return StructToValues(builtinValues)
}

// validateTemplateData validate template render and object decoder using  an empty cluster and addon
func (a *TemplateAgentAddon) validateTemplateData(file string, data []byte) error {
	configValues, err := a.getValues(&clusterv1.ManagedCluster{}, &addonapiv1alpha1.ManagedClusterAddOn{})
	if err != nil {
		return err
	}
	raw := assets.MustCreateAssetFromTemplate(file, data, configValues).Data
	_, _, err = a.decoder.Decode(raw, nil, nil)
	return err
}

func (a *TemplateAgentAddon) addTemplateData(file string, data []byte) {
	if a.templateData == nil {
		a.templateData = map[string][]byte{}
	}
	a.templateData[file] = data
}
