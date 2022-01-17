package helmaddonfactory

import (
	"fmt"

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

// the annotation Name of customized chart values
const annotationValuesName string = "addon.open-cluster-management.io/helmchart-values"

// the built-in values
const (
	clusterName           string = "clusterName"
	addonInstallNamespace string = "addonInstallNamespace"
	hubKubeConfigSecret   string = "hubKubeConfigSecret"
)

type Values map[string]interface{}

type GetValuesFunc func(cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (Values, error)

type HelmAgentAddon struct {
	decoder           runtime.Decoder
	chart             *chart.Chart
	getValuesFuncs    []GetValuesFunc
	agentAddonOptions agent.AgentAddonOptions
}

func newHelmAgentAddon(
	scheme *runtime.Scheme,
	chart *chart.Chart,
	getValuesFuncs []GetValuesFunc,
	agentAddonOptions agent.AgentAddonOptions) *HelmAgentAddon {
	return &HelmAgentAddon{
		decoder:           serializer.NewCodecFactory(scheme).UniversalDeserializer(),
		chart:             chart,
		getValuesFuncs:    getValuesFuncs,
		agentAddonOptions: agentAddonOptions}
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
	for _, data := range templates {
		if len(data) == 0 {
			continue
		}
		klog.V(4).Infof("%v/n", data)
		object, _, err := a.decoder.Decode([]byte(data), nil, nil)
		if err != nil {
			return nil, err
		}
		objects = append(objects, object)
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
		}
		userValues, err := a.getValuesFuncs[i](cluster, addon)
		if err != nil {
			return overrideValues, err
		}
		overrideValues = MergeValues(overrideValues, userValues)
	}

	overrideValues = MergeValues(overrideValues, a.getBuiltInValues(cluster, addon))

	values, err := chartutil.ToRenderValues(a.chart, overrideValues,
		a.releaseOptions(cluster, addon), a.capabilities(cluster, addon))
	if err != nil {
		klog.Error("failed to render helm chart with values %v. err:%v", overrideValues, err)
		return values, err
	}

	return values, nil
}

func (a *HelmAgentAddon) getBuiltInValues(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) Values {
	builtInValues := Values{}

	builtInValues[clusterName] = cluster.GetName()

	installNamespace := addon.Spec.InstallNamespace
	if len(installNamespace) == 0 {
		installNamespace = AddonDefaultInstallNamespace
	}
	builtInValues[addonInstallNamespace] = installNamespace

	// TODO: hubKubeConfigSecret depends on the signer configuration in registration, and the registration is an array.
	if a.agentAddonOptions.Registration != nil {
		builtInValues[hubKubeConfigSecret] = fmt.Sprintf("%s-hub-kubeconfig", a.agentAddonOptions.AddonName)
	}
	return builtInValues
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
	return nil
}
