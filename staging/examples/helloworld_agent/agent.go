package helloworld_agent

import (
	"context"
	"reflect"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/basecontroller/factory"
	cmdfactory "open-cluster-management.io/addon-framework/pkg/cmd/factory"
	"open-cluster-management.io/addon-framework/pkg/lease"
	"open-cluster-management.io/addon-framework/pkg/version"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
)

func NewAgentCommand(addonName string) *cobra.Command {
	o := NewAgentOptions(addonName)
	cmd := cmdfactory.
		NewControllerCommandConfig("helloworld-addon-agent", version.Get(), o.RunAgent).
		NewCommand()
	cmd.Use = "agent"
	cmd.Short = "Start the addon agent"

	o.AddFlags(cmd)
	return cmd
}

// AgentOptions defines the flags for workload agent
type AgentOptions struct {
	HubKubeconfigFile     string
	ManagedKubeconfigFile string
	SpokeClusterName      string
	AddonName             string
	AddonNamespace        string
}

// NewAgentOptions returns the flags with default value set
func NewAgentOptions(addonName string) *AgentOptions {
	return &AgentOptions{AddonName: addonName}
}

func (o *AgentOptions) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	// This command only supports reading from config
	flags.StringVar(&o.HubKubeconfigFile, "hub-kubeconfig", o.HubKubeconfigFile,
		"Location of kubeconfig file to connect to hub cluster.")
	flags.StringVar(&o.ManagedKubeconfigFile, "managed-kubeconfig", o.ManagedKubeconfigFile,
		"Location of kubeconfig file to connect to the managed cluster.")
	flags.StringVar(&o.SpokeClusterName, "cluster-name", o.SpokeClusterName, "Name of spoke cluster.")
	flags.StringVar(&o.AddonNamespace, "addon-namespace", o.AddonNamespace, "Installation namespace of addon.")
	flags.StringVar(&o.AddonName, "addon-name", o.AddonName, "name of the addon.")
}

// RunAgent starts the controllers on agent to process work from hub.
func (o *AgentOptions) RunAgent(ctx context.Context, kubeconfig *rest.Config) error {
	// build managementKubeClient of the local cluster
	managementKubeClient, err := kubernetes.NewForConfig(kubeconfig)
	if err != nil {
		return err
	}

	spokeKubeClient := managementKubeClient
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

	// build kubeinformerfactory of hub cluster
	hubRestConfig, err := clientcmd.BuildConfigFromFlags("" /* leave masterurl as empty */, o.HubKubeconfigFile)
	if err != nil {
		return err
	}
	hubKubeClient, err := kubernetes.NewForConfig(hubRestConfig)
	if err != nil {
		return err
	}
	addonClient, err := addonv1alpha1client.NewForConfig(hubRestConfig)
	if err != nil {
		return err
	}
	hubKubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(hubKubeClient, 10*time.Minute, informers.WithNamespace(o.SpokeClusterName))

	// create an agent controller
	agent := newAgentController(
		spokeKubeClient,
		addonClient,
		hubKubeInformerFactory.Core().V1().ConfigMaps(),
		o.SpokeClusterName,
		o.AddonName,
		o.AddonNamespace,
	)
	// create a lease updater
	leaseUpdater := lease.NewLeaseUpdater(
		managementKubeClient,
		o.AddonName,
		o.AddonNamespace,
	)

	go hubKubeInformerFactory.Start(ctx.Done())
	go agent.Run(ctx, 1)
	go leaseUpdater.Start(ctx)

	<-ctx.Done()
	return nil
}

type agentController struct {
	spokeKubeClient    kubernetes.Interface
	addonClient        addonv1alpha1client.Interface
	hubConfigMapLister corev1lister.ConfigMapLister
	clusterName        string
	addonName          string
	addonNamespace     string
}

func newAgentController(
	spokeKubeClient kubernetes.Interface,
	addonClient addonv1alpha1client.Interface,
	configmapInformers corev1informers.ConfigMapInformer,
	clusterName string,
	addonName string,
	addonNamespace string,
) factory.Controller {
	c := &agentController{
		spokeKubeClient:    spokeKubeClient,
		addonClient:        addonClient,
		clusterName:        clusterName,
		addonName:          addonName,
		addonNamespace:     addonNamespace,
		hubConfigMapLister: configmapInformers.Lister(),
	}
	return factory.New().WithInformersQueueKeysFunc(
		func(obj runtime.Object) []string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return []string{key}
		}, configmapInformers.Informer()).
		WithSync(c.sync).ToController("helloworld-agent-controller")
}

func (c *agentController) sync(ctx context.Context, syncCtx factory.SyncContext, key string) error {
	klog.V(4).Infof("Reconciling addon deploy %q", key)

	clusterName, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is not in format: namespace/name
		return nil
	}

	cm, err := c.hubConfigMapLister.ConfigMaps(clusterName).Get(name)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	addon, err := c.addonClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Get(ctx, c.addonName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if !addon.DeletionTimestamp.IsZero() {
		return nil
	}

	configmap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cm.Name,
			Namespace: c.addonNamespace,
			Labels:    map[string]string{"synced-from-hub": ""},
		},
		Data: cm.Data,
	}

	existing, err := c.spokeKubeClient.CoreV1().ConfigMaps(c.addonNamespace).Get(ctx, configmap.Name, metav1.GetOptions{})
	switch {
	case errors.IsNotFound(err):
		_, createErr := c.spokeKubeClient.CoreV1().ConfigMaps(c.addonNamespace).Create(ctx, configmap, metav1.CreateOptions{})
		return createErr
	case err != nil:
		return err
	}

	if reflect.DeepEqual(existing.Data, configmap.Data) {
		return nil
	}

	configmap.ResourceVersion = existing.ResourceVersion
	_, err = c.spokeKubeClient.CoreV1().ConfigMaps(c.addonNamespace).Update(ctx, configmap, metav1.UpdateOptions{})
	return err
}
