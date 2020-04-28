package hub

import (
	"context"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
)

type nucleusHubController struct {
	kubeClient         kubernetes.Interface
	apiExtensionClient apiextensionsclient.Interface
}

// NewNucleusHubController construct nucleus hub controller
func NewNucleusHubController(
	kubeClient kubernetes.Interface,
	apiExtensionClient apiextensionsclient.Interface,
	kubeInformers informers.SharedInformerFactory,
	recorder events.Recorder) factory.Controller {
	controller := &nucleusHubController{
		kubeClient:         kubeClient,
		apiExtensionClient: apiExtensionClient,
	}

	return factory.New().WithSync(controller.sync).ToController("NucleusHubController", recorder)
}

func (m *nucleusHubController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	return nil
}
