# Changing the API

This document is oriented at developers who want to change existing APIs for Open Cluster Management. As Open Cluster Management uses [Kubernetes Custom Resource Definitions](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/) to define its own API resources, a set of API change guidelines will also be applied to any [Kubernetes Custom Resource Definitions].

## Contents

- [Things you should consider before you change the APIs](#things-you-should-consider-before-you-change-the-apis)
- [Versions in the APIs](#versions-in-the-apis)
- [How to Add a New Version](#how-to-add-a-new-version)
- [Migrate stored objects to the new version](#migrate-stored-objects-to-the-new-version)

### Things you should consider before you change the APIs

Before attempting a change to the API, you should familiarize yourself with a number of [existing API types](https://github.com/open-cluster-management/api) and with the API conventions.

You should also consider the compatibility of the API changes. Open Cluster Management considers forwards and backwards compatibility of its APIs as top priority.

An API change is considered compatible if it:

- adds new functionality that is not required for correct behavior (e.g., does not add a new required field)
- does not change existing semantics, including:
  * the semantic meaning of default values and behavior
  * interpretation of existing API types, fields, and values
  * which fields are required and which are not
  * mutable fields do not become immutable
  * valid values do not become invalid
  * explicitly invalid values do not become valid

Let's see an example for compatible API changes.

In a hypothetical API of version `v1beta1`, the Application struct looks like this:

```golang
// v1beta1
type Application struct {
  Name        string `json:"name"`
  Replicas    int    `json:"replicas"`
}
```

If we add a new `Param` field, it is safe to add new fields without changing the API version, so we can simply change it to:

```golang
// v1beta1
type Application struct {
  Name        string `json:"name"`
  Replicas    int    `json:"replicas"`
  Param       string `json:"param"`
}
```

The version of the API is still `v1beta1`, we just need to add default value for the new `Param` field so that API calls and stored objects that used to work will continue to work.

Next time, we may consider to allow multiple `Param` values, then we can't simply replace `Param string` with `Param []string`. We have two options for this case:

1. Add new `Params []string` field and the new field must be inclusive of the singular field, also we must handle all the cases of version skew, multiple clients, rollbacks and so on.
2. Bump a new version(eg. `v1beta2`) for the API object and replace the old singular field `Param string` with the new plural field `Params []string`, we must implement api conversion logic to/from versioned APIs so that older clients that only know the singular field will continue to succeed and produce the same results as before the change, while newer clients can use your change without impacting older clients.

```golang
// v1beta2
type Frobber struct {
  Height int
  Width  int
  Params []string
}
```

### Versions in the APIs

For most API changes, you may find it easiest to change the versioned APIs first. Since Open Cluster Management uses Custom Resource Definitions to define its own API resources, when a CustomResourceDefinition is created, the first version is set in the CustomResourceDefinition `spec.versions` list to an appropriate stability level and a version number. For example `v1beta1` would indicate that the first version is not yet stable. All custom resource objects will initially be stored at this version. As our API evolve, we need to upgrade the API to a new version with conversion between API representations. For example, we may begin using the `v1beta1` API, later it might be necessary to add new version such as `v1beta2`.

### How to add a new version

1. Pick a conversion strategy. Since custom resource objects need to be able to be served at both versions, that means they will sometimes be served at a different version than their storage version. In order for this to be possible, the custom resource objects must sometimes be converted between the version they are stored at and the version they are served at. If the conversion involves schema changes and requires custom logic, a conversion webhook should be used.
2. If using conversion webhooks, create and deploy the conversion webhook. See the [Webhook conversion](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definition-versioning/#webhook-conversion) for more details.
3. Update the CustomResourceDefinition to include the new version in the `spec.versions` list with `served:true`. Also, set `spec.conversion` field to the selected conversion strategy. If using a conversion webhook, configure `spec.conversion.webhookClientConfig` field to call the webhook.

Once the new version is added, clients may incrementally migrate to the new version. It is perfectly safe for some clients to use the old version while others use the new version.

### Migrate stored objects to the new version

Although it is safe for clients to use both the old and new version before, during and after upgrading the objects to a new stored version, but we need to deprecate and drop the old version support, for those API objects created in the old version before we introduce the new version, we need to migration the stored object in the APIServer yo new version.

We have two options to acheived this:

Option 1: Use the [Storage Version Migrator](https://github.com/kubernetes-sigs/kube-storage-version-migrator)
Option 2: Manually upgrade the existing objects to a new stored version

We prefer to option 1 because the migration process with option 1 is totally transparent to users, while option 2 need users' assistent and may introduce service interrupt.

For option 1, all we need to do is creating a migration request by the `StorageVersionMigration` resource similar to below:

```yaml
apiVersion: migration.k8s.io/v1alpha1
kind: StorageVersionMigration
metadata:
  name: multiclusterobservabilities-storage-version-migration
spec:
  resource:
    group: observability.open-cluster-management.io
    resource: multiclusterobservabilities
    version: v1beta2
```

Then the migration work will be done in a few seconds:

```bash
$ oc get storageversionmigration multiclusterobservabilities-storage-version-migration -o jsonpath="{.status}"
{"conditions":[{"lastUpdateTime":"2021-04-06T07:10:12Z","status":"True","type":"Succeeded"}]}
```
