package spoke

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	registration "open-cluster-management.io/ocm/pkg/registration/spoke"
	work "open-cluster-management.io/ocm/pkg/work/spoke"
)

type AgentConfig struct {
	agentOption        *commonoptions.AgentOptions
	registrationOption *registration.SpokeAgentOptions
	workOption         *work.WorkloadAgentOptions
}

func NewAgentConfig(
	agentOption *commonoptions.AgentOptions,
	registrationOption *registration.SpokeAgentOptions,
	workOption *work.WorkloadAgentOptions) *AgentConfig {
	return &AgentConfig{
		agentOption:        agentOption,
		registrationOption: registrationOption,
		workOption:         workOption,
	}
}

func (a *AgentConfig) RunSpokeAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	registrationCfg := registration.NewSpokeAgentConfig(a.agentOption, a.registrationOption)
	// start registration agent at first
	go func() {
		if err := registrationCfg.RunSpokeAgent(ctx, controllerContext); err != nil {
			klog.Fatal(err)
		}
	}()

	// wait for the hub client config ready.
	klog.Info("Waiting for hub client config and managed cluster to be ready")
	if err := wait.PollUntilContextCancel(ctx, 1*time.Second, true, registrationCfg.IsHubKubeConfigValid); err != nil {
		return err
	}

	workCfg := work.NewWorkAgentConfig(a.agentOption, a.workOption)
	// start work agent
	go func() {
		if err := workCfg.RunWorkloadAgent(ctx, controllerContext); err != nil {
			klog.Fatal(err)
		}
	}()

	<-ctx.Done()
	return nil
}
