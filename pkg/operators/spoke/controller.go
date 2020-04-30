package spoke

import (
	"context"

	"k8s.io/client-go/kubernetes"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
)

type nucleusAgentController struct {
	kubeClient kubernetes.Interface
}

// NewNucleusAgentController construct nucleus agent controller
func NewNucleusAgentController(
	kubeClient kubernetes.Interface,
	recorder events.Recorder) factory.Controller {
	controller := &nucleusAgentController{
		kubeClient: kubeClient,
	}

	return factory.New().WithSync(controller.sync).ToController("NucleusHubController", recorder)
}

func (m *nucleusAgentController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	return nil
}
