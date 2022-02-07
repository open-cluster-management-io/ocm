package cleanup_agent

import (
	"context"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/version"
)

func NewAgentCommand(addonName string) *cobra.Command {
	o := NewAgentOptions(addonName)
	cmdConfig := controllercmd.
		NewControllerCommandConfig("cleanup-agent", version.Get(), o.RunAgent)
	cmd := cmdConfig.NewCommand()
	cmd.Use = "cleanup"
	cmd.Short = "Clean up the synced configmap"

	cmdConfig.DisableLeaderElection = true
	o.AddFlags(cmd)
	return cmd
}

// AgentOptions defines the flags for workload agent
type AgentOptions struct {
	AddonName      string
	AddonNamespace string
}

func NewAgentOptions(addonName string) *AgentOptions {
	return &AgentOptions{AddonName: addonName}
}

func (o *AgentOptions) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&o.AddonNamespace, "addon-namespace", o.AddonNamespace, "Installation namespace of addon.")
}

func (o *AgentOptions) RunAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// build kubeclient of managed cluster
	spokeKubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	configMapList, err := spokeKubeClient.CoreV1().ConfigMaps(o.AddonNamespace).List(ctx, metav1.ListOptions{LabelSelector: "synced-from-hub="})
	if err != nil {
		return err
	}
	for _, configMap := range configMapList.Items {
		err := spokeKubeClient.CoreV1().ConfigMaps(o.AddonNamespace).Delete(ctx, configMap.Name, metav1.DeleteOptions{})
		if err != nil {
			klog.Errorf("failed to delete configmap %v. reason:%v", configMap.Name, err)
			continue
		}
	}

	return nil
}
