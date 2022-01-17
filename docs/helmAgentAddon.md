# Overview
This doc is used to introduce how to migrate Helm Chart to an Addon.

## Limitations
1. Not all of built-in Objects of Helm Chart are support in Addon. We only support below:
* Capabilities.KubeVersion
* Release.Name
* Release.Namespace

2. Not support Hooks of Helm Chart currently.

## Migration 
We have an example for Helm Chart migration in [helloworld_helm](../examples/helloworld_helm).
1. Need to import Helm Chart files into embed.FS.
2. Create and start helmAgentAddon instance like this:
   ```go
   mgr, err := addonmanager.New(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	agentAddon, err := helmaddonfactory.NewAgentAddonFactoryWithHelmChartFS(addonName, FS, "manifests/charts/helloworld").
		WithScheme(scheme).
        WithGetValuesFuncs([]helmaddonfactory.GetValuesFunc{getValues, helmaddonfactory.GetValuesFromAddonAnnotation}).
		WithAgentRegistrationOption(newRegistrationOption()).
		Build()
	if err != nil {
		return err
	}

	mgr.AddAgent(agentAddon)
	mgr.Start(ctx)
   ```

### Values definition 
#### Built-in values
Addon built-in values
* `Value.clusterName`
* `Value.addonInstallNamespace`
* `Value.hubKubeConfigSecret` (used when the addon is needed to registered to the Hub cluster)

Helm Chart built-in values
* `Capabilities.KubeVersion` is the `ManagedCluster.Status.Version.Kubernetes`.
* `Release.Name`  is the addon name.
* `Release.Namespace`  is the `addonInstallNamespace`.

In the list of `GetValuesFuncs`, the values from the big index Func will override the one from low index Func.
The built-in values will override the values got from the list of `GetValuesFuncs`.


#### Values from annotation of ManagedClusterAddon
We support a helper `GetValuesFunc` named GetValuesFromAddonAnnotation which can get values from annotation of ManagedClusterAddon.
The key of the Helm Chart values in annotation is `addon.open-cluster-management.io/helmchart-values`,
and the value should be a valid json string which has key-value format.

