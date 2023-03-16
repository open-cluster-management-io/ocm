package addonfactory

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	"open-cluster-management.io/addon-framework/pkg/agent"
)

// helmBuiltinValues includes the built-in values for helm agentAddon.
// the values in helm chart should begin with a lowercase letter, so we need convert it to Values by JsonStructToValues.
// the built-in values can not be overrided by getValuesFuncs
type helmBuiltinValues struct {
	ClusterName             string `json:"clusterName"`
	AddonInstallNamespace   string `json:"addonInstallNamespace"`
	HubKubeConfigSecret     string `json:"hubKubeConfigSecret,omitempty"`
	ManagedKubeConfigSecret string `json:"managedKubeConfigSecret,omitempty"`
	InstallMode             string `json:"installMode"`
}

// helmDefaultValues includes the default values for helm agentAddon.
// the values in helm chart should begin with a lowercase letter, so we need convert it to Values by JsonStructToValues.
// the default values can be overrided by getValuesFuncs
type helmDefaultValues struct {
	HubKubeConfigSecret        string                 `json:"hubKubeConfigSecret,omitempty"`
	ManagedKubeConfigSecret    string                 `json:"managedKubeConfigSecret,omitempty"`
	HostingClusterCapabilities chartutil.Capabilities `json:"hostingClusterCapabilities,omitempty"`
}

type HelmAgentAddon struct {
	decoder            runtime.Decoder
	chart              *chart.Chart
	getValuesFuncs     []GetValuesFunc
	agentAddonOptions  agent.AgentAddonOptions
	trimCRDDescription bool
	hostingCluster     *clusterv1.ManagedCluster
}

func newHelmAgentAddon(factory *AgentAddonFactory, chart *chart.Chart) *HelmAgentAddon {
	return &HelmAgentAddon{
		decoder:            serializer.NewCodecFactory(factory.scheme).UniversalDeserializer(),
		chart:              chart,
		getValuesFuncs:     factory.getValuesFuncs,
		agentAddonOptions:  factory.agentAddonOptions,
		trimCRDDescription: factory.trimCRDDescription,
		hostingCluster:     factory.hostingCluster,
	}
}

func (a *HelmAgentAddon) Manifests(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	var objects []runtime.Object

	values, err := a.getValues(cluster, addon)
	if err != nil {
		return objects, err
	}

	helmEngine := engine.Engine{
		Strict:   true,
		LintMode: false,
	}

	crds := a.chart.CRDObjects()
	for _, crd := range crds {
		klog.V(4).Infof("%v/n", crd.File.Data)
		object, _, err := a.decoder.Decode(crd.File.Data, nil, nil)
		if err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}

	templates, err := helmEngine.Render(a.chart, values)
	if err != nil {
		return objects, err
	}

	// sort the filenames of the templates so the manifests are ordered consistently
	keys := make([]string, 0, len(templates))
	for k := range templates {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		data := templates[k]

		if len(data) == 0 {
			continue
		}
		klog.V(4).Infof("rendered template: %v", data)

		yamlReader := yaml.NewYAMLReader(bufio.NewReader(strings.NewReader(data)))
		for {
			b, err := yamlReader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if len(b) != 0 {
				object, _, err := a.decoder.Decode(b, nil, nil)
				if err != nil {
					// In some conditions, resources will be provide by other hub-side components.
					// Example case: https://github.com/open-cluster-management-io/addon-framework/pull/72
					if runtime.IsMissingKind(err) {
						klog.V(4).Infof("Skipping template %v, reason: %v", k, err)
						continue
					}
					return nil, err
				}
				objects = append(objects, object)
			}
		}

	}

	if a.trimCRDDescription {
		objects = trimCRDDescription(objects)
	}
	return objects, nil
}

func (a *HelmAgentAddon) GetAgentAddonOptions() agent.AgentAddonOptions {
	return a.agentAddonOptions
}

func (a *HelmAgentAddon) getValues(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (chartutil.Values, error) {
	overrideValues := map[string]interface{}{}

	defaultValues, err := a.getDefaultValues(cluster, addon)
	if err != nil {
		klog.Error("failed to get defaultValue. err:%v", err)
		return nil, err
	}
	overrideValues = MergeValues(overrideValues, defaultValues)

	for i := 0; i < len(a.getValuesFuncs); i++ {
		if a.getValuesFuncs[i] != nil {
			userValues, err := a.getValuesFuncs[i](cluster, addon)
			if err != nil {
				return overrideValues, err
			}

			klog.V(4).Infof("index=%d, user values: %v", i, userValues)
			overrideValues = MergeValues(overrideValues, userValues)
			klog.V(4).Infof("index=%d, override values: %v", i, overrideValues)
		}
	}

	builtinValues, err := a.getBuiltinValues(cluster, addon)
	if err != nil {
		klog.Error("failed to get builtinValue. err:%v", err)
		return nil, err
	}

	overrideValues = MergeValues(overrideValues, builtinValues)

	values, err := chartutil.ToRenderValues(a.chart, overrideValues,
		a.releaseOptions(cluster, addon), a.capabilities(cluster, addon))
	if err != nil {
		klog.Errorf("failed to render helm chart with values %v. err:%v", overrideValues, err)
		return values, err
	}

	return values, nil
}

func (a *HelmAgentAddon) getBuiltinValues(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (Values, error) {
	builtinValues := helmBuiltinValues{}
	builtinValues.ClusterName = cluster.GetName()

	installNamespace := addon.Spec.InstallNamespace
	if len(installNamespace) == 0 {
		installNamespace = AddonDefaultInstallNamespace
	}
	builtinValues.AddonInstallNamespace = installNamespace

	builtinValues.InstallMode, _ = constants.GetHostedModeInfo(addon.GetAnnotations())

	helmBuiltinValues, err := JsonStructToValues(builtinValues)
	if err != nil {
		klog.Error("failed to convert builtinValues to values %v.err:%v", builtinValues, err)
		return nil, err
	}
	return helmBuiltinValues, nil
}

func (a *HelmAgentAddon) getDefaultValues(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (Values, error) {
	defaultValues := helmDefaultValues{}

	// TODO: hubKubeConfigSecret depends on the signer configuration in registration, and the registration is an array.
	if a.agentAddonOptions.Registration != nil {
		defaultValues.HubKubeConfigSecret = fmt.Sprintf("%s-hub-kubeconfig", a.agentAddonOptions.AddonName)
	}

	defaultValues.ManagedKubeConfigSecret = fmt.Sprintf("%s-managed-kubeconfig", addon.Name)

	if a.hostingCluster != nil {
		defaultValues.HostingClusterCapabilities = *a.capabilities(a.hostingCluster, addon)
	}

	helmDefaultValues, err := JsonStructToValues(defaultValues)
	if err != nil {
		klog.Error("failed to convert defaultValues to values %v.err:%v", defaultValues, err)
		return nil, err
	}
	return helmDefaultValues, nil
}

// only support Capabilities.KubeVersion
func (a *HelmAgentAddon) capabilities(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) *chartutil.Capabilities {
	return &chartutil.Capabilities{
		KubeVersion: chartutil.KubeVersion{Version: cluster.Status.Version.Kubernetes},
	}
}

// only support Release.Name, Release.Namespace
func (a *HelmAgentAddon) releaseOptions(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) chartutil.ReleaseOptions {
	installNamespace := addon.Spec.InstallNamespace
	if len(installNamespace) == 0 {
		installNamespace = AddonDefaultInstallNamespace
	}
	return chartutil.ReleaseOptions{Name: a.agentAddonOptions.AddonName, Namespace: installNamespace}
}
