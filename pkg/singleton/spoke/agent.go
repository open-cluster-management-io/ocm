package spoke

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apiserver/pkg/server/healthz"
	"k8s.io/klog/v2"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	registration "open-cluster-management.io/ocm/pkg/registration/spoke"
	work "open-cluster-management.io/ocm/pkg/work/spoke"
)

type AgentConfig struct {
	registrationConfig *registration.SpokeAgentConfig
	workConfig         *work.WorkAgentConfig
}

func NewAgentConfig(
	agentOption *commonoptions.AgentOptions,
	registrationOption *registration.SpokeAgentOptions,
	workOption *work.WorkloadAgentOptions,
	cancel context.CancelFunc,
) *AgentConfig {
	return &AgentConfig{
		registrationConfig: registration.NewSpokeAgentConfig(agentOption, registrationOption, cancel),
		workConfig:         work.NewWorkAgentConfig(agentOption, workOption),
	}
}

func (a *AgentConfig) RunSpokeAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// start registration agent at first
	go func() {
		if err := a.registrationConfig.RunSpokeAgent(ctx, controllerContext); err != nil {
			klog.Fatal(err)
		}
	}()

	// wait for the hub client config ready.
	// PollUntilContextCancel periodically executes the condition func `o.internalHubConfigValidFunc`
	// until one of the following conditions is met:
	// - condition returns `true`: Indicates the hub client configuration
	//   is ready, and the polling stops successfully.
	// - condition returns an error: This happens when loading the kubeconfig
	//   file fails or the kubeconfig is invalid. In such cases, the error is returned, causing the
	//   agent to exit with an error and triggering a new leader election.
	// - The context is canceled: In this case, no error is returned. This ensures that
	//   the current leader can release leadership, allowing a new pod to get leadership quickly.
	klog.Info("Waiting for hub client config and managed cluster to be ready")
	if err := wait.PollUntilContextCancel(ctx, 1*time.Second, true, a.registrationConfig.IsHubKubeConfigValid); err != nil {
		if err != context.Canceled {
			return err
		}
	}

	// start work agent
	go func() {
		if err := a.workConfig.RunWorkloadAgent(ctx, controllerContext); err != nil {
			klog.Fatal(err)
		}
	}()

	<-ctx.Done()
	return nil
}

func (o *AgentConfig) HealthCheckers() []healthz.HealthChecker {
	return o.registrationConfig.HealthCheckers()
}
