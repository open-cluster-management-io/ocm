package addonfactory

import (
	"fmt"
	"sort"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/agent"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// helmBuiltinValues includes the built-in values for helm agentAddon.
// the values in helm chart should begin with a lowercase letter, so we need convert it to Values by JsonStructToValues.
type helmBuiltinValues struct {
	ClusterName           string `json:"clusterName"`
	AddonInstallNamespace string `json:"addonInstallNamespace"`
	HubKubeConfigSecret   string `json:"hubKubeConfigSecret,omitempty"`
}

type HelmAgentAddon struct {
	decoder            runtime.Decoder
	chart              *chart.Chart
	getValuesFuncs     []GetValuesFunc
	agentAddonOptions  agent.AgentAddonOptions
	trimCRDDescription bool
}

func newHelmAgentAddon(factory *AgentAddonFactory, chart *chart.Chart) *HelmAgentAddon {
	return &HelmAgentAddon{
		decoder:            serializer.NewCodecFactory(factory.scheme).UniversalDeserializer(),
		chart:              chart,
		getValuesFuncs:     factory.getValuesFuncs,
		agentAddonOptions:  factory.agentAddonOptions,
		trimCRDDescription: factory.trimCRDDescription,
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
		object, _, err := a.decoder.Decode([]byte(data), nil, nil)
		if err != nil {
			if runtime.IsMissingKind(err) {
				klog.V(4).Infof("Skipping template %v, reason: %v", k, err)
				continue
			}
			return nil, err
		}
		objects = append(objects, object)
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

	for i := 0; i < len(a.getValuesFuncs); i++ {
		if a.getValuesFuncs[i] != nil {
			userValues, err := a.getValuesFuncs[i](cluster, addon)
			if err != nil {
				return overrideValues, err
			}
			overrideValues = MergeValues(overrideValues, userValues)
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

	// TODO: hubKubeConfigSecret depends on the signer configuration in registration, and the registration is an array.
	if a.agentAddonOptions.Registration != nil {
		builtinValues.HubKubeConfigSecret = fmt.Sprintf("%s-hub-kubeconfig", a.agentAddonOptions.AddonName)
	}

	helmBuiltinValues, err := JsonStructToValues(builtinValues)
	if err != nil {
		klog.Error("failed to convert builtinValues to values %v.err:%v", builtinValues, err)
		return nil, err
	}
	return helmBuiltinValues, nil
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

// validateChart validates chart by rendering and decoding chart with an empty cluster and addon
func (a *HelmAgentAddon) validateChart() error {
	// TODO: validate chart
	return nil
}
