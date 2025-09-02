package addonfactory

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"helm.sh/helm/v3/pkg/releaseutil"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

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
	// Deprecated: use clusterClient to get the hosting cluster.
	hostingCluster        *clusterv1.ManagedCluster
	clusterClient         clusterclientset.Interface
	agentInstallNamespace func(addon *addonapiv1alpha1.ManagedClusterAddOn) (string, error)
	helmEngineStrict      bool
}

func newHelmAgentAddon(factory *AgentAddonFactory, chart *chart.Chart) *HelmAgentAddon {
	return &HelmAgentAddon{
		decoder:               serializer.NewCodecFactory(factory.scheme).UniversalDeserializer(),
		chart:                 chart,
		getValuesFuncs:        factory.getValuesFuncs,
		agentAddonOptions:     factory.agentAddonOptions,
		trimCRDDescription:    factory.trimCRDDescription,
		hostingCluster:        factory.hostingCluster,
		clusterClient:         factory.clusterClient,
		agentInstallNamespace: factory.agentInstallNamespace,
		helmEngineStrict:      factory.helmEngineStrict,
	}
}

func (a *HelmAgentAddon) Manifests(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	objects, err := a.renderManifests(cluster, addon)
	if err != nil {
		return nil, err
	}

	manifests := make([]manifest, 0, len(objects))
	for _, obj := range objects {
		a, err := meta.TypeAccessor(obj)
		if err != nil {
			return nil, err
		}
		manifests = append(manifests, manifest{
			Object: obj,
			Kind:   a.GetKind(),
		})
	}
	sortManifestsByKind(manifests, releaseutil.InstallOrder)

	for i, manifest := range manifests {
		objects[i] = manifest.Object
	}
	return objects, nil
}

func (a *HelmAgentAddon) renderManifests(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
	var objects []runtime.Object

	values, err := a.getValues(cluster, addon)
	if err != nil {
		return objects, err
	}

	helmEngine := engine.Engine{
		Strict:   a.helmEngineStrict,
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

	for k, data := range templates {
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
		klog.Errorf("failed to get defaultValue. err:%v", err)
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
		klog.Errorf("failed to get builtinValue. err:%v", err)
		return nil, err
	}

	overrideValues = MergeValues(overrideValues, builtinValues)

	releaseOptions, err := a.releaseOptions(addon)
	if err != nil {
		return nil, err
	}
	cap := a.capabilities(cluster, addon)
	values, err := chartutil.ToRenderValues(a.chart, overrideValues,
		releaseOptions, cap)
	if err != nil {
		klog.Errorf("failed to render helm chart with values %v. err:%v", overrideValues, err)
		return values, err
	}

	return values, nil
}

func (a *HelmAgentAddon) getValueAgentInstallNamespace(addon *addonapiv1alpha1.ManagedClusterAddOn) (string, error) {
	installNamespace := addon.Spec.InstallNamespace
	if len(installNamespace) == 0 {
		installNamespace = AddonDefaultInstallNamespace
	}
	if a.agentInstallNamespace != nil {
		ns, err := a.agentInstallNamespace(addon)
		if err != nil {
			klog.Errorf("failed to get agentInstallNamespace from addon %s. err: %v", addon.Name, err)
			return "", err
		}
		if len(ns) > 0 {
			installNamespace = ns
		}
	}
	return installNamespace, nil
}

func (a *HelmAgentAddon) getBuiltinValues(
	cluster *clusterv1.ManagedCluster,
	addon *addonapiv1alpha1.ManagedClusterAddOn) (Values, error) {
	builtinValues := helmBuiltinValues{}
	builtinValues.ClusterName = cluster.GetName()

	addonInstallNamespace, err := a.getValueAgentInstallNamespace(addon)
	if err != nil {
		return nil, err
	}
	builtinValues.AddonInstallNamespace = addonInstallNamespace

	builtinValues.InstallMode, _ = a.agentAddonOptions.HostedModeInfoFunc(addon, cluster)

	helmBuiltinValues, err := JsonStructToValues(builtinValues)
	if err != nil {
		klog.Errorf("failed to convert builtinValues to values %v.err:%v", builtinValues, err)
		return nil, err
	}
	return helmBuiltinValues, nil
}

// Deprecated: use "WithManagedClusterClient" in AgentAddonFactory to set a cluster client that
// can be used to get the hosting cluster.
func (a *HelmAgentAddon) SetHostingCluster(hostingCluster *clusterv1.ManagedCluster) {
	a.hostingCluster = hostingCluster
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
	} else if a.clusterClient != nil {
		_, hostingClusterName := a.agentAddonOptions.HostedModeInfoFunc(addon, cluster)
		if len(hostingClusterName) > 0 {
			hostingCluster, err := a.clusterClient.ClusterV1().ManagedClusters().
				Get(context.TODO(), hostingClusterName, metav1.GetOptions{})
			if err == nil { //nolint:gocritic
				defaultValues.HostingClusterCapabilities = *a.capabilities(hostingCluster, addon)
			} else if errors.IsNotFound(err) {
				klog.Infof("hostingCluster %s not found, skip providing default value hostingClusterCapabilities",
					hostingClusterName)
			} else {
				klog.Errorf("failed to get hostingCluster %s. err:%v", hostingClusterName, err)
				return nil, err
			}
		}
	}

	helmDefaultValues, err := JsonStructToValues(defaultValues)
	if err != nil {
		klog.Errorf("failed to convert defaultValues to values %v.err:%v", defaultValues, err)
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
	addon *addonapiv1alpha1.ManagedClusterAddOn) (chartutil.ReleaseOptions, error) {
	releaseOptions := chartutil.ReleaseOptions{
		Name: a.agentAddonOptions.AddonName,
	}
	namespace, err := a.getValueAgentInstallNamespace(addon)
	if err != nil {
		return releaseOptions, err
	}
	releaseOptions.Namespace = namespace
	return releaseOptions, nil
}

// manifest represents a manifest file, which has a name and some content.
type manifest struct {
	Object runtime.Object
	Kind   string
}

// sort manifests by kind.
//
// Results are sorted by 'ordering', keeping order of items with equal kind/priority
func sortManifestsByKind(manifests []manifest, ordering releaseutil.KindSortOrder) []manifest {
	sort.SliceStable(manifests, func(i, j int) bool {
		return lessByKind(manifests[i], manifests[j], manifests[i].Kind, manifests[j].Kind, ordering)
	})

	return manifests
}

func lessByKind(a interface{}, b interface{}, kindA string, kindB string, o releaseutil.KindSortOrder) bool {
	ordering := make(map[string]int, len(o))
	for v, k := range o {
		ordering[k] = v
	}

	first, aok := ordering[kindA]
	second, bok := ordering[kindB]

	if !aok && !bok {
		// if both are unknown then sort alphabetically by kind, keep original order if same kind
		if kindA != kindB {
			return kindA < kindB
		}
		return first < second
	}
	// unknown kind is last
	if !aok {
		return false
	}
	if !bok {
		return true
	}
	// sort different kinds, keep original order if same priority
	return first < second
}
