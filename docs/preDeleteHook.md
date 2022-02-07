# Overview
This doc is used to introduce how to add a pre-delete manifestWork for an AddOn. 

We support to use `Jobs` or `Pods` as a pre-delete manifestWork for an AddOn.

# How it works
1. Add the label `open-cluster-management.io/addon-pre-delete` to the `Jobs` or `Pods` manifests.
2. The `Jobs` or `Pods` will not be applied until the managedClusterAddon is deleted.
3. The `Jobs` or `Pods` will be applied on the managed cluster by applying the manifestWork named `addon-<addon name>-pre-delete` when the managedClusterAddon is deleting.
4. After the `Jobs` are `Completed` or `Pods` are in `Succeeded` phase, all the deployed manifestWorks will be deleted.

# Example
See the example [helloworld_helm](../examples/helloworld_helm)
