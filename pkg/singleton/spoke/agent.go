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
	workOption *work.WorkloadAgentOptions) *AgentConfig {
	return &AgentConfig{
		registrationConfig: registration.NewSpokeAgentConfig(agentOption, registrationOption),
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
	klog.Info("Waiting for hub client config and managed cluster to be ready")
	if err := wait.PollUntilContextCancel(ctx, 1*time.Second, true, a.registrationConfig.IsHubKubeConfigValid); err != nil {
		return err
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
