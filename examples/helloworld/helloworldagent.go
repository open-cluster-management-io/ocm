package main

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	corev1informers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/lease"
	"open-cluster-management.io/addon-framework/pkg/version"
)

// Helloworld Agent is an example that syncs configmap in cluster namespace of hub cluster
// to the install namespace in managedcluster.

func newAgentCommand() *cobra.Command {
	o := NewAgentOptions()
	cmd := controllercmd.
		NewControllerCommandConfig("addon-agent", version.Get(), o.RunAgent).
		NewCommand()
	cmd.Use = "agent"
	cmd.Short = "Start the addon agent"

	o.AddFlags(cmd)
	return cmd
}

// AgentOptions defines the flags for workload agent
type AgentOptions struct {
	HubKubeconfigFile string
	SpokeClusterName  string
}

// NewWorkloadAgentOptions returns the flags with default value set
func NewAgentOptions() *AgentOptions {
	return &AgentOptions{}
}

func (o *AgentOptions) AddFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	// This command only supports reading from config
	flags.StringVar(&o.HubKubeconfigFile, "hub-kubeconfig", o.HubKubeconfigFile, "Location of kubeconfig file to connect to hub cluster.")
	flags.StringVar(&o.SpokeClusterName, "cluster-name", o.SpokeClusterName, "Name of spoke cluster.")
}

// RunAgent starts the controllers on agent to process work from hub.
func (o *AgentOptions) RunAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// build kubeclient of managed cluster
	spokeKubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
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
	hubKubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(hubKubeClient, 10*time.Minute, informers.WithNamespace(o.SpokeClusterName))

	// create an agent contoller
	agent := newAgentController(
		spokeKubeClient,
		hubKubeInformerFactory.Core().V1().ConfigMaps(),
		o.SpokeClusterName,
		controllerContext.EventRecorder,
	)
	// create a lease updater
	leaseUpdater := lease.NewLeaseUpdater(
		spokeKubeClient,
		"helloworld",
		addOnAgentInstallationNamespace,
	)

	go hubKubeInformerFactory.Start(ctx.Done())
	go agent.Run(ctx, 1)
	go leaseUpdater.Start(ctx)

	<-ctx.Done()
	return nil
}

type agentController struct {
	spokeKubeClient    kubernetes.Interface
	hunConfigMapLister corev1lister.ConfigMapLister
	clusterName        string
	recorder           events.Recorder
}

func newAgentController(
	spokeKubeClient kubernetes.Interface,
	configmapInformers corev1informers.ConfigMapInformer,
	clusterName string,
	recorder events.Recorder,
) factory.Controller {
	c := &agentController{
		spokeKubeClient:    spokeKubeClient,
		clusterName:        clusterName,
		hunConfigMapLister: configmapInformers.Lister(),
		recorder:           recorder,
	}
	return factory.New().WithInformersQueueKeyFunc(
		func(obj runtime.Object) string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return key
		}, configmapInformers.Informer()).
		WithSync(c.sync).ToController("helloworld-agent-controller", recorder)
}

func (c *agentController) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	key := syncCtx.QueueKey()
	klog.V(4).Infof("Reconciling addon deploy %q", key)

	clusterName, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore addon whose key is not in format: namespace/name
		return nil
	}

	cm, err := c.hunConfigMapLister.ConfigMaps(clusterName).Get(name)
	switch {
	case errors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	configmap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cm.Name,
			Namespace: addOnAgentInstallationNamespace,
		},
		Data: cm.Data,
	}

	_, _, err = resourceapply.ApplyConfigMap(ctx, c.spokeKubeClient.CoreV1(), c.recorder, configmap)
	return err
}
