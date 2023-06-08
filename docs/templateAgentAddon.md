# Overview
This doc is used to introduce how to create an AddOn using Go template.

## Steps 
We have an example  in [helloworld](../examples/helloworld).
1. Need to import templates files into embed.FS.
2. Create and start templateAgentAddon instance like this:
   ```go
   mgr, err := addonmanager.New(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	agentAddon, err := addonfactory.NewAgentAddonFactory(addonName, FS, "manifests/templates").
		WithGetValuesFuncs(getValues, addonfactory.GetValuesFromAddonAnnotation).
		WithAgentRegistrationOption(registrationOption).
		BuildTemplateAgentAddon()
	if err != nil {
		return err
	}

	mgr.AddAgent(agentAddon)
	mgr.Start(ctx)
   ```

### Values definition 

We also name the config for the template as `Values`.

#### Built-in Values
AddOn built-in values
* `ClusterName`
* `AddonInstallNamespace`
* `HubKubeConfigSecret` (used when the AddOn is needed to register to the Hub cluster)

In the list of `GetValuesFuncs`, the values from the big index Func will override the one from low index Func.
The built-in Values will override the Values got from the list of `GetValuesFuncs`.

The config variable names should begin with uppercase. We can use `StructToValues` to convert config struct to `Values`.

#### Values from annotation of ManagedClusterAddon
We support a helper `GetValuesFunc` named `GetValuesFromAddonAnnotation` which can get values from annotation of ManagedClusterAddon.
The key of Values in annotation is `addon.open-cluster-management.io/values`,
and the value should be a valid json string which has key-value format.

