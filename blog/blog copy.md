

## What is an add-on

Open-cluster-management has a mechanism to help developers to develop an extension based on the foundation components for the purpose of  working with multiple clusters in various asepcts. The examples of  addons includes:

- A tool to collect alert events in the managed cluster, and send to the hub cluster.

- A network solution that uses hub to share the network infos and establish connection among managed clusters.

- A tool to spread security policies to multiple clusters.

In general, if a management tool needs different configuration for each managed clusters or a secured communitation between managed cluster and hub cluster, it can utilize the addon mechanism in open-cluster-management to ease the installation and day 2 operation.

Normally, an addon comprises of two components:

1. An agent running on hub cluster (aka. `addon-controller`);
2. An agent running on managted cluster (aka. `addon-agent`);

![architecture](/Users/leyan/workspaces/acm/addon-framework/blog/arch.png)

As shown in the above diagram,  some foundation components from open-cluster-management are required as well to manage the lifecycle of an addon:

- `registration-hub-controller` on hub cluster. It updates addon status according to status of cluster lease on hub.
- `registration-agent` on managed cluster. It helps to register the addon on the hub cluster and creates a secret contraing hub kubeconfig for the addon on the manated cluster. It also keeps updating the addon status on hub cluster based on the addon lease created on the managed cluster;
- `work-agent` on managed cluster. It distributes a list of manifests from the hub cluster to the managed cluster and enforces them to be applied on the managed cluster for this addon.
## How to develop an add-on

In this section, we'll develop a hello world addon. The basic logic of the addon is this:

1. The addon lists all configmaps in the cluster namespace on hub cluster;
2. The addon creates configmaps with the same names in the `default` namespace on managed cluster;
3. And then it monitors changes on those configmaps on hub cluster and applies the same change on managed cluster as well;

The easies way to build an addon is to leverage `addon-framework`, which is a library containing necessary interfaces and  default implementation for addon lifecycle management. 

### Build addon-controller

In order to build the addon-controller for `helloworld` addon, we need to implement `AgentAddon` interface from  `addon-framework`.

```go
// AgentAddon defines manifests of agent deployed on managed cluster
type AgentAddon interface {
	// Manifests returns a list of manifest resources to be deployed on the managed cluster for this addon
	Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error)
	// GetAgentAddonOptions returns the agent options.
	GetAgentAddonOptions() AgentAddonOptions
}
```

We’ll start out with a new file `helloworld.go`.

```sh
$ vi helloworld.go
```

 And create an struct with name `helloWorldAgent` which implements the above interface.

```go
type helloWorldAgent struct {
	kubeConfig *rest.Config
	agentName  string
}
var _ agent.AgentAddon = &helloWorldAgent{}
```
The `Manifests` method is expected to returns manifest resources which are required to deploy the addon-agent on managed cluster. Specifically, `helloworld` addon needs the following manifests:

1. The namespace to install addon-agent;
2. A serviceaccount to run the addon-agent;
3. A clusterrolebinding. It binds appropriate clusterrole to the servcieaccount;
4. And a deployment. It deploy the addon-agent;

```go
func (h *helloWorldAgent) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {
  ... ...
  objects := []runtime.Object{}

	// addon install namespace on managed cluster
	namespace, err := loadManifestFromFile("namespace.yaml", manifestConfig)
	if err != nil {
		return nil, err
	}
	objects = append(objects, namespace)

	// serviceaccount to run addon agent on managed cluster
	serviceAccount, err := loadManifestFromFile("serviceaccount.yaml", manifestConfig)
	if err != nil {
		return nil, err
	}
	objects = append(objects, serviceAccount)

	// clusterrolebinding for the above serviceaccount on managed cluster
	clusterrolebinding, err := loadManifestFromFile("clusterrolebinding.yaml", manifestConfig)
	if err != nil {
		return nil, err
	}
	objects = append(objects, clusterrolebinding)

	// deployment to deploy addon agent on managed cluster
	deployment, err := loadManifestFromFile("deployment.yaml", manifestConfig)
	if err != nil {
		return nil, err
	}
	objects = append(objects, deployment)
	
	return objects, nil
}
```

And `GetAgentAddonOptions` method is expected to return addon configuration including addon name and options for addon registration.

```go
func (h *helloWorldAgent) GetAgentAddonOptions() agent.AgentAddonOptions {
	return agent.AgentAddonOptions{
		AddonName: "helloworld",
		Registration: &agent.RegistrationOption{
			CSRConfigurations: agent.KubeClientSignerConfigurations("helloworld", h.agentName),
			CSRApproveCheck:   agent.ApprovalAllCSRs,
			PermissionConfig:  h.setupAgentPermissions,
		},
	}
}
```

With the above configuration, addon-controller will expect a `CertificateSigningRequests` is created on hub cluster  with signer `kubernetes.io/kube-apiserver-client` when addon-agent bootstrapping.  It will have the default user name and inlcude `system:open-cluster-management:cluster:{cluster-name}:addon:helloworld` as one of the user groups. The addon-controller will approve the  bootstrap `CertificateSigningRequests` automalically, and grant this user group appropriate permissions on hub cluster by invoking  `PermissionConfig` method.

```go
func (h *helloWorldAgent) setupAgentPermissions(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) error {
	groups := agent.DefaultGroups(cluster.Name, addon.Name)
	config := struct {
		ClusterName string
		Group       string
	}{
		ClusterName: cluster.Name,
		Group:       groups[0],
	}

	err := applyManifestFromFile("role.yaml", config)
	if err != nil {
		return err
	}

	return applyManifestFromFile("rolebinding.yaml", config)
}
```

Finally, we need to assemble all above together with `addon-framework` in  `main.go`. 

```sh
$ vi main.go
```

Add a new command to start the addon-controller.

```go
func newControllerCommand() *cobra.Command {
	cmd := controllercmd.
		NewControllerCommandConfig("addon-controller", version.Get(), runController).
		NewCommand()
	cmd.Use = "controller"
	cmd.Short = "Start the addon controller"

	return cmd
}
```
`runController` method creates an addon manager, which is provided by `addon-framework`, and registers our addon-controller before starting it.

```go

func runController(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	mgr, err := addonmanager.New(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	agentRegistration := &helloWorldAgent{
		kubeConfig: controllerContext.KubeConfig,
		recorder:   controllerContext.EventRecorder,
		agentName:  utilrand.String(5),
	}
	mgr.AddAgent(agentRegistration)
	mgr.Start(ctx)

	<-ctx.Done()

	return nil
}
```

### Build `addon-agent`

First, we'll create a new file `helloworldagent.go`.
```sh
$ vi helloworldagent.go
```

And add another command to run `addon-agent`

```go
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
```
`RunAgent` method starts an agent controller and a lease updater.

```go
// RunAgent starts the controllers on agent to process work from hub.
func (o *AgentOptions) RunAgent(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
  ... ...
  // create an agent controller
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
		"default",
	)

	go hubKubeInformerFactory.Start(ctx.Done())
	go agent.Run(ctx, 1)
	go leaseUpdater.Start(ctx)

	<-ctx.Done()
	return nil
}
```
The agent controler copies configmaps from cluster namespace on hub cluster to the `default` namespace on managed cluster;

```go
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
		WithSync(c.sync).ToController(fmt.Sprintf("helloworld-agent-controller"), recorder)
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
			Namespace: "default",
		},
		Data: cm.Data,
	}

	_, _, err = resourceapply.ApplyConfigMap(c.spokeKubeClient.CoreV1(), c.recorder, configmap)
	return err
}
```

And the lease updater creates a lease for addon in the addon install namespace and keep updating it every 60 seconds.



**Noted:** The complete code of `helloworld` addon can be found: https://github.com/open-cluster-management-io/addon-framework/tree/main/examples/helloworld

