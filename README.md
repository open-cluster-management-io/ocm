# AddOn Framework

This is to define an AddOn framework library.

Still in PoC phase.

## Community, discussion, contribution, and support

Check the [CONTRIBUTING Doc](CONTRIBUTING.md) for how to contribute to the repo.

<!--

You can reach the maintainers of this project at:

- [#xxx on Slack](https://slack.com/signin?redir=%2Fmessages%2Fxxx)

-->

------
## Purpose

This library is used to implement AddOn management like installation, registration etc.
So developers could be able to develop their own addon functions more easily.

## Concepts

* [ManagedClusterAddon](https://github.com/open-cluster-management-io/api/blob/main/addon/v1alpha1/types_managedclusteraddon.go)
* [ClusterManagementAddOn](https://github.com/open-cluster-management-io/api/blob/main/addon/v1alpha1/types_clustermanagementaddon.go)

## Design Doc

* [AddOn Framework](https://github.com/open-cluster-management-io/enhancements/tree/main/enhancements/sig-architecture/8-addon-framework)

## AddOn Consumers
* [cluster-proxy](https://github.com/open-cluster-management-io/cluster-proxy) 

* [managed-serviceaccount](https://github.com/open-cluster-management-io/managed-serviceaccount)

## AddOn Examples 

We have examples to implement AddOn using Helm Chart or Go Template. You can find more details in [examples](examples/README.md).
