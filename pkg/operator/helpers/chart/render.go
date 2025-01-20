package chart

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"

	clustermanagerchart "open-cluster-management.io/ocm/deploy/cluster-manager/chart"
	klusterletchart "open-cluster-management.io/ocm/deploy/klusterlet/chart"
)

func NewDefaultClusterManagerChartConfig() *ClusterManagerChartConfig {
	return &ClusterManagerChartConfig{
		ReplicaCount:         3,
		CreateBootstrapToken: false,
		ClusterManager: ClusterManagerConfig{
			Create: true,
		},
	}
}

func NewDefaultKlusterletChartConfig() *KlusterletChartConfig {
	return &KlusterletChartConfig{
		ReplicaCount: 1,
		Klusterlet: KlusterletConfig{
			Create: true,
		},
	}
}

func RenderClusterManagerChart(config *ClusterManagerChartConfig, namespace string) ([][]byte, error) {
	if namespace == "" {
		return nil, fmt.Errorf("cluster manager chart namespace is required")
	}
	return renderChart(config, namespace, config.CreateNamespace,
		clustermanagerchart.ChartName, clustermanagerchart.ChartFiles)
}

func RenderKlusterletChart(config *KlusterletChartConfig, namespace string) ([][]byte, error) {
	if namespace == "" {
		return nil, fmt.Errorf("klusterlet chart namespace is required")
	}
	return renderChart(config, namespace, config.CreateNamespace,
		klusterletchart.ChartName, klusterletchart.ChartFiles)
}

func renderChart[T *ClusterManagerChartConfig | *KlusterletChartConfig](config T,
	namespace string, createNamespace bool, chartName string, fs embed.FS) ([][]byte, error) {
	// chartName is the prefix of chart path here
	operatorChart, err := LoadChart(fs, chartName)
	if err != nil {
		return nil, fmt.Errorf("failed to load %s chart: %w", chartName, err)
	}

	configValues, err := JsonStructToValues(config)
	if err != nil {
		return nil, fmt.Errorf("error generating values for chartConfig: %v", err)
	}

	releaseOptions := chartutil.ReleaseOptions{
		Name:      chartName,
		Namespace: namespace,
	}

	values, err := chartutil.ToRenderValues(operatorChart, configValues,
		releaseOptions, &chartutil.Capabilities{})
	if err != nil {
		klog.Errorf("failed to render helm chart with values %v. err:%v", values, err)
		return nil, err
	}

	rawObjects, err := renderManifests(operatorChart, values)
	if err != nil {
		return nil, fmt.Errorf("error rendering cluster manager chart: %v", err)
	}

	// make sure the ns object is at the top of slice when createNamespace is true.
	rstObjects := [][]byte{}
	if createNamespace {
		nsObj, err := newNamespaceRawObject(namespace)
		if err != nil {
			return nil, err
		}
		rstObjects = [][]byte{nsObj}
	}
	rstObjects = append(rstObjects, rawObjects...)

	return rstObjects, nil
}

func getFiles(manifestFS embed.FS) ([]string, error) {
	var res []string
	err := fs.WalkDir(manifestFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		res = append(res, path)
		return nil
	})
	return res, err
}

func stripPrefix(chartPrefix, path string) string {
	prefixNoPathSeparatorSuffix := strings.TrimSuffix(chartPrefix, string(filepath.Separator))
	chartPrefixLen := len(strings.Split(prefixNoPathSeparatorSuffix, string(filepath.Separator)))
	pathValues := strings.Split(path, string(filepath.Separator))
	return strings.Join(pathValues[chartPrefixLen:], string(filepath.Separator))
}

func LoadChart(chartFS embed.FS, chartPrefix string) (*chart.Chart, error) {
	files, err := getFiles(chartFS)
	if err != nil {
		return nil, err
	}

	var bfs []*loader.BufferedFile
	for _, fileName := range files {
		b, err := fs.ReadFile(chartFS, fileName)
		if err != nil {
			klog.Errorf("failed to read file %v. err:%v", fileName, err)
			return nil, err
		}
		if !strings.HasPrefix(fileName, chartPrefix) {
			continue
		}
		bf := &loader.BufferedFile{
			Name: stripPrefix(chartPrefix, fileName),
			Data: b,
		}
		bfs = append(bfs, bf)
	}

	userChart, err := loader.LoadFiles(bfs)
	if err != nil {
		klog.Errorf("failed to load chart. err:%v", err)
		return nil, err
	}
	return userChart, nil
}

// JsonStructToValues converts the given json struct to a Values
func JsonStructToValues(a interface{}) (chartutil.Values, error) {
	raw, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}

	vals, err := chartutil.ReadValues(raw)
	if err != nil {
		return nil, err
	}
	return vals, nil
}

func renderManifests(chart *chart.Chart, values chartutil.Values) ([][]byte, error) {
	var rawObjects [][]byte

	// make sure the CRDs are at the top.
	crds := chart.CRDObjects()
	for _, crd := range crds {
		klog.V(4).Infof("%v/n", crd.File.Data)
		rawObjects = append(rawObjects, crd.File.Data)
	}

	helmEngine := engine.Engine{
		Strict:   true,
		LintMode: false,
	}

	templates, err := helmEngine.Render(chart, values)
	if err != nil {
		return rawObjects, err
	}

	namespaceObjects := [][]byte{}
	for _, data := range templates {
		if len(data) == 0 {
			continue
		}

		// remove invalid template
		unstructuredObj := &unstructured.Unstructured{}
		if err = yaml.Unmarshal([]byte(data), unstructuredObj); err != nil {
			return nil, fmt.Errorf("error unmarshalling template: %v", err)
		}
		kind := unstructuredObj.GetKind()
		if kind == "" {
			continue
		}

		if kind == "Namespace" {
			namespaceObjects = append(namespaceObjects, []byte(data))
			continue
		}

		rawObjects = append(rawObjects, []byte(data))
	}
	// will create open-cluster-management-agent ns in klusterlet operator,
	// so need make sure namespaces are at the top of slice.
	if len(namespaceObjects) != 0 {
		result := append(namespaceObjects, rawObjects...)
		return result, nil
	}
	return rawObjects, nil
}

func newNamespaceRawObject(namespace string) ([]byte, error) {
	ns := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	return yaml.Marshal(ns)
}
