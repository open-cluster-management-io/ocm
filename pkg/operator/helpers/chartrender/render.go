package chartrender

import (
	"bufio"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/json"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"

	operatorv1 "open-cluster-management.io/api/operator/v1"

	clustermanagerchart "open-cluster-management.io/ocm/deploy/cluster-manager/chart"
	klusterletchart "open-cluster-management.io/ocm/deploy/klusterlet/chart"
)

var chartScheme = runtime.NewScheme()
var decoder runtime.Decoder

func init() {
	_ = scheme.AddToScheme(chartScheme)
	_ = apiextensionsv1.AddToScheme(chartScheme)
	_ = apiextensionsv1beta1.AddToScheme(chartScheme)
	_ = operatorv1.AddToScheme(chartScheme)

	decoder = serializer.NewCodecFactory(chartScheme).UniversalDeserializer()
}

func NewDefaultClusterManagerChartConfig() *clustermanagerchart.ChartConfig {
	return &clustermanagerchart.ChartConfig{
		ReplicaCount:         3,
		CreateBootstrapToken: false,
	}
}

func NewDefaultKlusterletChartConfig() *klusterletchart.ChartConfig {
	return &klusterletchart.ChartConfig{
		ReplicaCount: 3,
	}
}

func RenderChart[T *clustermanagerchart.ChartConfig | *klusterletchart.ChartConfig](config T,
	namespace string, chartName string, fs embed.FS) ([]runtime.Object, error) {
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

	objects, err := renderManifests(operatorChart, values)
	if err != nil {
		return nil, fmt.Errorf("error rendering cluster manager chart: %v", err)
	}

	return objects, nil
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

func renderManifests(chart *chart.Chart, values chartutil.Values) ([]runtime.Object, error) {
	var objects []runtime.Object
	crds := chart.CRDObjects()
	for _, crd := range crds {
		klog.V(4).Infof("%v/n", crd.File.Data)

		object, _, err := decoder.Decode(crd.File.Data, nil, nil)
		if err != nil {
			return nil, err
		}
		objects = append(objects, object)
	}

	helmEngine := engine.Engine{
		Strict:   true,
		LintMode: false,
	}

	templates, err := helmEngine.Render(chart, values)
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
				object, _, err := decoder.Decode(b, nil, nil)
				if err != nil {
					// In some conditions, resources will not be rendered
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
	return objects, nil
}
