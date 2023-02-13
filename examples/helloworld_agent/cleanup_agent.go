package helloworld_agent

import (
	"context"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	cmdfactory "open-cluster-management.io/addon-framework/pkg/cmd/factory"
	"open-cluster-management.io/addon-framework/pkg/version"
)

func NewCleanupAgentCommand(addonName string) *cobra.Command {
	o := NewCleanupAgentOptions(addonName)
	cmdConfig := cmdfactory.
		NewControllerCommandConfig("cleanup-agent", version.Get(), o.RunAgent)
	cmd := cmdConfig.NewCommand()
	cmd.Use = "cleanup"
	cmd.Short = "Clean up the synced configmap"

	o.AddFlags(cmd)
	return cmd
}

// CleanupAgentOptions defines the flags for workload agent
type CleanupAgentOptions struct {
	AddonName             string
	AddonNamespace        string
	ManagedKubeconfigFile string
}

func NewCleanupAgentOptions(addonName string) *CleanupAgentOptions {
	return &CleanupAgentOptions{AddonName: addonName}
}

func (o *CleanupAgentOptions) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&o.AddonNamespace, "addon-namespace", o.AddonNamespace, "Installation namespace of addon.")
	flags.StringVar(&o.ManagedKubeconfigFile, "managed-kubeconfig", o.ManagedKubeconfigFile,
		"Location of kubeconfig file to connect to the managed cluster.")
}

func (o *CleanupAgentOptions) RunAgent(ctx context.Context, kubeConfig *rest.Config) error {
	// build kubeclient of managed cluster
	spokeKubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}
	if len(o.ManagedKubeconfigFile) != 0 {
		managedRestConfig, err := clientcmd.BuildConfigFromFlags("", /* leave masterurl as empty */
			o.ManagedKubeconfigFile)
		if err != nil {
			return err
		}
		spokeKubeClient, err = kubernetes.NewForConfig(managedRestConfig)
		if err != nil {
			return err
		}
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
