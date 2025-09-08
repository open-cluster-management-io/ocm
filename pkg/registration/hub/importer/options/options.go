package options

import (
	"fmt"

	"github.com/spf13/pflag"
	"k8s.io/client-go/kubernetes"

	"open-cluster-management.io/ocm/pkg/registration/hub/importer"
)

type Options struct {
	APIServerURL         string
	AgentImage           string
	BootstrapSA          string
	ImporterRendererList []string
}

const (
	RenderFromConfigSecret string = "render-from-config-secret"
	RenderAuto             string = "render-auto"
)

func New() *Options {
	return &Options{
		BootstrapSA:          "open-cluster-management/agent-registration-bootstrap",
		ImporterRendererList: []string{RenderFromConfigSecret},
	}
}

// AddFlags registers flags for manager
func (m *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&m.APIServerURL, "hub-apiserver-url", m.APIServerURL,
		"APIServer URL of the hub cluster that the spoke cluster can access, Only used for spoke cluster import")
	fs.StringVar(&m.AgentImage, "agent-image", m.AgentImage,
		"Image of the agent to import, only singleton mode is used for importer and only registration-operator "+
			"image is needed.")
	fs.StringVar(&m.BootstrapSA, "bootstrap-serviceaccount", m.BootstrapSA,
		"Service account used to bootstrap the agent.")
	fs.StringSliceVar(&m.ImporterRendererList, "import-renderer-list", m.ImporterRendererList,
		"use the list of import renderers to render the klusterlet manifests for cluster importing")
}

func GetImporterRenderers(options *Options, kubeClient kubernetes.Interface,
	operatorNamespace string) ([]importer.KlusterletConfigRenderer, error) {
	var renderers []importer.KlusterletConfigRenderer
	if len(options.ImporterRendererList) == 0 {
		renderers = append(renderers,
			importer.RenderBootstrapHubKubeConfig(kubeClient, options.APIServerURL, options.BootstrapSA),
			importer.RenderImage(options.AgentImage),
			importer.RenderImagePullSecret(kubeClient, operatorNamespace),
		)
		return renderers, nil
	}

	for _, renderer := range options.ImporterRendererList {
		switch renderer {
		case RenderAuto:
			renderers = append(renderers,
				importer.RenderBootstrapHubKubeConfig(kubeClient, options.APIServerURL, options.BootstrapSA),
				importer.RenderImage(options.AgentImage),
				importer.RenderImagePullSecret(kubeClient, operatorNamespace),
			)
		case RenderFromConfigSecret:
			renderers = append(renderers,
				importer.RenderFromConfigSecret(kubeClient),
			)
		default:
			return renderers, fmt.Errorf("unknown importer renderer %s", renderer)
		}
	}
	return renderers, nil
}
