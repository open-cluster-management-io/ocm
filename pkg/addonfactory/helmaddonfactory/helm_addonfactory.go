package helmaddonfactory

import (
	"embed"
	"io/fs"
	"path/filepath"
	"strings"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/agent"
)

const AddonDefaultInstallNamespace = "open-cluster-management-agent-addon"

// HelmAgentAddonFactory builds an agentAddon instance from helm chart.
type HelmAgentAddonFactory struct {
	scheme            *runtime.Scheme
	chartPrefix       string
	chartFS           embed.FS
	getValuesFuncs    []GetValuesFunc
	agentAddonOptions agent.AgentAddonOptions
}

// NewAgentAddonFactoryWithHelmChartFS builds an addon agent with chart fs.
// chartPrefix is the path prefix based on the fs path.
func NewAgentAddonFactoryWithHelmChartFS(addonName string, fs embed.FS, chartPrefix string) *HelmAgentAddonFactory {
	return &HelmAgentAddonFactory{
		chartFS:     fs,
		chartPrefix: chartPrefix,
		agentAddonOptions: agent.AgentAddonOptions{
			AddonName:       addonName,
			Registration:    nil,
			InstallStrategy: nil,
		},
	}
}

// WithScheme is an optional configuration, only used when the helm chart has customized resource types.
func (f *HelmAgentAddonFactory) WithScheme(scheme *runtime.Scheme) *HelmAgentAddonFactory {
	f.scheme = scheme
	return f
}

// WithGetValuesFuncs adds a list of the getValues func.
// the values got from the big index Func will override the one from small index Func.
func (f *HelmAgentAddonFactory) WithGetValuesFuncs(getValuesFuncs []GetValuesFunc) *HelmAgentAddonFactory {
	f.getValuesFuncs = getValuesFuncs
	return f
}

// WithInstallStrategy defines the installation strategy of the manifests prescribed by Manifests(..).
func (f *HelmAgentAddonFactory) WithInstallStrategy(strategy *agent.InstallStrategy) *HelmAgentAddonFactory {
	if strategy.InstallNamespace == "" {
		strategy.InstallNamespace = AddonDefaultInstallNamespace
	}
	f.agentAddonOptions.InstallStrategy = strategy

	return f
}

// WithAgentRegistrationOption defines how agent is registered to the hub cluster.
func (f *HelmAgentAddonFactory) WithAgentRegistrationOption(option *agent.RegistrationOption) *HelmAgentAddonFactory {
	f.agentAddonOptions.Registration = option
	return f
}

// Build creates a new agentAddon.
func (f *HelmAgentAddonFactory) Build() (agent.AgentAddon, error) {
	if f.scheme == nil {
		f.scheme = runtime.NewScheme()
		_ = scheme.AddToScheme(f.scheme)
		_ = apiextensionsv1.AddToScheme(f.scheme)
		_ = apiextensionsv1beta1.AddToScheme(f.scheme)
	}

	//	chart, err := loader.Load(f.chartPath)
	userChart, err := f.loadChart()
	if err != nil {
		return nil, err
	}
	// TODO: validate chart
	agentAddon := newHelmAgentAddon(f.scheme, userChart, f.getValuesFuncs, f.agentAddonOptions)

	return agentAddon, nil
}

func (f *HelmAgentAddonFactory) loadChart() (*chart.Chart, error) {
	files, err := f.getChartFiles()
	if err != nil {
		return nil, err
	}

	var bfs []*loader.BufferedFile
	for _, fileName := range files {
		b, err := fs.ReadFile(f.chartFS, fileName)
		if err != nil {
			klog.Errorf("failed to read file %v. err:%v", fileName, err)
			return nil, err
		}

		bf := &loader.BufferedFile{
			Name: f.stripPrefix(fileName),
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

func (f *HelmAgentAddonFactory) getChartFiles() ([]string, error) {
	var res []string
	err := fs.WalkDir(f.chartFS, ".", func(path string, d fs.DirEntry, err error) error {
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

func (f *HelmAgentAddonFactory) stripPrefix(path string) string {
	chartPrefixLen := len(strings.Split(f.chartPrefix, string(filepath.Separator)))
	pathValues := strings.Split(path, string(filepath.Separator))
	return strings.Join(pathValues[chartPrefixLen:], string(filepath.Separator))
}
