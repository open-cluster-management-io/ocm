package chart

import (
	"fmt"
	"os"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/yaml"

	operatorv1 "open-cluster-management.io/api/operator/v1"

	clustermanagerchart "open-cluster-management.io/ocm/deploy/cluster-manager/chart"
	klusterletchart "open-cluster-management.io/ocm/deploy/klusterlet/chart"
)

const (
	defaultRegistry = "quay.io/open-cluster-management"
	defaultVersion  = "latest"
)

var outputDebug = false

var decoder runtime.Decoder
var chartScheme = runtime.NewScheme()

func init() {
	_ = scheme.AddToScheme(chartScheme)
	_ = apiextensionsv1.AddToScheme(chartScheme)
	_ = apiextensionsv1beta1.AddToScheme(chartScheme)
	_ = operatorv1.AddToScheme(chartScheme)

	decoder = serializer.NewCodecFactory(chartScheme).UniversalDeserializer()
}

func TestClusterManagerConfig(t *testing.T) {
	cases := []struct {
		name           string
		namespace      string
		chartConfig    func() *clustermanagerchart.ChartConfig
		expectedObjCnt int
	}{
		{
			name:      "default config",
			namespace: "open-cluster-management",
			chartConfig: func() *clustermanagerchart.ChartConfig {
				config := NewDefaultClusterManagerChartConfig()
				return config
			},
			expectedObjCnt: 6,
		},
		{
			name:      "enable bootstrap config",
			namespace: "multicluster-engine",
			chartConfig: func() *clustermanagerchart.ChartConfig {
				config := NewDefaultClusterManagerChartConfig()
				config.CreateBootstrapToken = true
				return config
			},
			expectedObjCnt: 9,
		},
		{
			name:      "change images config",
			namespace: "ocm",
			chartConfig: func() *clustermanagerchart.ChartConfig {
				config := NewDefaultClusterManagerChartConfig()
				config.Images = clustermanagerchart.ImagesConfig{
					Registry:        "myrepo",
					Tag:             "v9.9.9",
					ImagePullPolicy: corev1.PullAlways,
					ImageCredentials: clustermanagerchart.ImageCredentials{
						CreateImageCredentials: true,
						UserName:               "test",
						Password:               "test",
					},
				}
				return config
			},
			expectedObjCnt: 7,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			registry := defaultRegistry
			version := defaultVersion
			config := c.chartConfig()

			if config.Images.Registry != "" {
				registry = config.Images.Registry
			}
			if config.Images.Tag != "" {
				version = config.Images.Tag
			}

			objects, err := RenderChart(config, c.namespace, clustermanagerchart.ChartName, clustermanagerchart.ChartFiles)
			if err != nil {
				t.Errorf("error rendering chart: %v", err)
			}
			if len(objects) != c.expectedObjCnt {
				t.Errorf("expected %d objects, got %d", c.expectedObjCnt, len(objects))
			}

			outputObjs := make([]runtime.Object, 0, len(objects))
			for _, o := range objects {
				obj, _, err := decoder.Decode(o, nil, nil)
				if err != nil {
					t.Errorf("error decoding object: %v", err)
				}
				outputObjs = append(outputObjs, obj)
				switch object := obj.(type) {
				case *appsv1.Deployment:
					if object.Namespace != c.namespace {
						t.Errorf("expected namespace is %s, but got %s", c.namespace, object.Namespace)
					}
					if object.Spec.Template.Spec.Containers[0].Image != fmt.Sprintf("%s/registration-operator:%s", registry, version) {
						t.Errorf("failed to render operator image")
					}

				case *apiextensionsv1.CustomResourceDefinition:
					if object.Name != "clustermanagers.operator.open-cluster-management.io" {
						t.Errorf(" got CRD name %s", object.Name)
					}
				case *operatorv1.ClusterManager:
					if object.Spec.PlacementImagePullSpec != fmt.Sprintf("%s/placement:%s", registry, version) ||
						object.Spec.RegistrationImagePullSpec != fmt.Sprintf("%s/registration:%s", registry, version) ||
						object.Spec.WorkImagePullSpec != fmt.Sprintf("%s/work:%s", registry, version) ||
						object.Spec.AddOnManagerImagePullSpec != fmt.Sprintf("%s/addon-manager:%s", registry, version) {
						t.Errorf("failed to render images")
					}
				}

			}

			// output is for debug
			if outputDebug {
				output(t, c.name, outputObjs...)
			}
		})
	}
}

func TestKlusterletConfig(t *testing.T) {

	cases := []struct {
		name           string
		namespace      string
		chartConfig    func() *klusterletchart.ChartConfig
		expectedObjCnt int
	}{
		{
			name:      "default config",
			namespace: "open-cluster-management",
			chartConfig: func() *klusterletchart.ChartConfig {
				config := NewDefaultKlusterletChartConfig()
				config.Klusterlet.ClusterName = "testCluster"
				config.Klusterlet.Mode = operatorv1.InstallModeSingleton
				return config
			},
			expectedObjCnt: 6,
		},

		{
			name:      "change images config",
			namespace: "ocm",
			chartConfig: func() *klusterletchart.ChartConfig {
				config := NewDefaultKlusterletChartConfig()
				config.Images = klusterletchart.ImagesConfig{
					Registry:        "myrepo",
					Tag:             "v9.9.9",
					ImagePullPolicy: corev1.PullAlways,
					ImageCredentials: klusterletchart.ImageCredentials{
						CreateImageCredentials: true,
						UserName:               "test",
						Password:               "test",
					},
				}
				config.Klusterlet.ClusterName = "testCluster"
				config.Klusterlet.Mode = operatorv1.InstallModeSingleton
				return config
			},
			expectedObjCnt: 7,
		},
		{
			name:      "hosted mode",
			namespace: "ocm",
			chartConfig: func() *klusterletchart.ChartConfig {
				config := NewDefaultKlusterletChartConfig()
				config.NoOperator = true
				config.Klusterlet.Name = "klusterlet2"
				config.Klusterlet.ClusterName = "testCluster"
				config.Klusterlet.Mode = operatorv1.InstallModeSingletonHosted
				return config
			},
			expectedObjCnt: 2,
		},
		{
			name:      "noOperator",
			namespace: "ocm",
			chartConfig: func() *klusterletchart.ChartConfig {
				config := NewDefaultKlusterletChartConfig()
				config.NoOperator = true
				config.Klusterlet.Name = "klusterlet2"
				config.Klusterlet.Namespace = "open-cluster-management-test"
				config.Klusterlet.ClusterName = "testCluster"
				return config
			},
			expectedObjCnt: 2,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			registry := defaultRegistry
			version := defaultVersion
			config := c.chartConfig()
			if config.Images.Registry != "" {
				registry = config.Images.Registry
			}
			if config.Images.Tag != "" {
				version = config.Images.Tag
			}

			objects, err := RenderChart(config, c.namespace, klusterletchart.ChartName, klusterletchart.ChartFiles)
			if err != nil {
				t.Errorf("error rendering chart: %v", err)
			}
			if len(objects) != c.expectedObjCnt {
				t.Errorf("expected %d objects, got %d", c.expectedObjCnt, len(objects))
			}

			outputObjs := make([]runtime.Object, 0, len(objects))
			for _, o := range objects {
				obj, _, err := decoder.Decode(o, nil, nil)
				if err != nil {
					t.Errorf("error decoding object: %v", err)
				}
				outputObjs = append(outputObjs, obj)
				switch object := obj.(type) {
				case *appsv1.Deployment:
					if object.Namespace != c.namespace {
						t.Errorf("expected namespace is %s, but got %s", c.namespace, object.Namespace)
					}
					if object.Spec.Template.Spec.Containers[0].Image != fmt.Sprintf("%s/registration-operator:%s", registry, version) {
						t.Errorf("failed to render operator image")
					}

				case *apiextensionsv1.CustomResourceDefinition:
					if object.Name != "klusterlets.operator.open-cluster-management.io" {
						t.Errorf(" got CRD name %s", object.Name)
					}
				case *operatorv1.Klusterlet:
					if object.Spec.RegistrationImagePullSpec != fmt.Sprintf("%s/registration:%s", registry, version) ||
						object.Spec.WorkImagePullSpec != fmt.Sprintf("%s/work:%s", registry, version) ||
						object.Spec.ImagePullSpec != fmt.Sprintf("%s/registration-operator:%s", registry, version) {
						t.Errorf("failed to render images")
					}

					if object.Spec.ClusterName != config.Klusterlet.ClusterName {
						t.Errorf(" expected %s, got %s", config.Klusterlet.ClusterName, object.Spec.ClusterName)
					}
					switch config.Klusterlet.Mode {
					case "", operatorv1.InstallModeSingleton, operatorv1.InstallModeDefault:
						if config.Klusterlet.Mode == "" && object.Spec.DeployOption.Mode != operatorv1.InstallModeSingleton {
							t.Errorf(" expected Singleton, got %s", object.Spec.DeployOption.Mode)
						}
						if config.Klusterlet.Mode != "" && object.Spec.DeployOption.Mode != config.Klusterlet.Mode {
							t.Errorf(" expected %s, got %s", config.Klusterlet.Mode, object.Spec.DeployOption.Mode)
						}
						switch config.Klusterlet.Name {
						case "":
							if object.Name != "klusterlet" {
								t.Errorf(" expected klusterlet, got %s", object.Name)
							}
						default:
							if object.Name != config.Klusterlet.Name {
								t.Errorf(" expected %s, got %s", config.Klusterlet.Name, object.Name)
							}
						}
						switch config.Klusterlet.Namespace {
						case "":
							if object.Spec.Namespace != "open-cluster-management-agent" {
								t.Errorf(" expected open-cluster-management-agent, got %s", object.Spec.Namespace)
							}
						default:
							if object.Spec.Namespace != config.Klusterlet.Namespace {
								t.Errorf(" expected %s, got %s", config.Klusterlet.Namespace, object.Spec.Namespace)
							}
						}
					case operatorv1.InstallModeSingletonHosted, operatorv1.InstallModeHosted:
						if object.Spec.DeployOption.Mode != config.Klusterlet.Mode {
							t.Errorf(" expected %s, got %s", config.Klusterlet.Mode, object.Spec.DeployOption.Mode)
						}
						if object.Name != fmt.Sprintf("klusterlet-%s", object.Spec.ClusterName) {
							t.Errorf(" expected %s, got %s",
								fmt.Sprintf("klusterlet-%s", object.Spec.ClusterName), object.Name)
						}
						if object.Spec.Namespace != fmt.Sprintf("open-cluster-management-%s", object.Spec.ClusterName) {
							t.Errorf(" expected %s, got %s",
								fmt.Sprintf("open-cluster-management-%s", object.Spec.ClusterName), object.Spec.Namespace)
						}
					}

				}
			}

			// output is for debug
			if outputDebug {
				output(t, c.name, outputObjs...)
			}
		})
	}
}

func output(t *testing.T, name string, objects ...runtime.Object) {
	tmpDir, err := os.MkdirTemp("./", "tmp-"+name)
	if err != nil {
		t.Fatalf("failed to create temp %v", err)
	}

	for i, o := range objects {
		data, err := yaml.Marshal(o)
		if err != nil {
			t.Fatalf("failed yaml marshal %v", err)
		}

		err = os.WriteFile(fmt.Sprintf("%v/%v-%v.yaml", tmpDir, i, o.GetObjectKind().GroupVersionKind().Kind), data, 0600)
		if err != nil {
			t.Fatalf("failed to Marshal object.%v", err)
		}

	}
}
