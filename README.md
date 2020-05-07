# Cluster Registration

Contains controllers that support the registration of spoke clusters to a hub to
place them under management.

## Community, discussion, contribution, and support

Check the [CONTRIBUTING Doc](CONTRIBUTING.md) for how to contribute to the repo.

<!--

You can reach the maintainers of this project at:

- [#xxx on Slack](https://slack.com/signin?redir=%2Fmessages%2Fxxx)

-->

------

## Getting Started

### Prerequisites

Check the [Development Doc](docs/development.md) for how to contribute to the repo.

## How to Deploy
### Deploy Hub
1. Run `make deploy-hub`

### Deploy Spoke
1. Create a bootstrap kubeconfig secret in the namespace of `open-cluster-management`.
Ensure the server field in kubeconfig is accesible from cluster. 

    In a kind environment where hub and spoke component is deployed in one cluster, default kubernetes cluster ip can be used.

    ```sh
    cp ~/.kube/config /<path>/kubeconfig
    clusterIP=$(kubectl get svc kubernetes -n default -o jsonpath="{.spec.clusterIP}")
    kubectl config set clusters.kind-kind.server https://$clusterIP --kubeconfig /<path>/kubeconfig
    ```
    Then run the following command
    ```sh
    kubectl create secret generic bootstrap-secret --from-file=kubeconfig=/<path>/kubeconfig -n open-cluster-management
    ```
2. Run `make deploy-spoke`

## Security Response

If you've found a security issue that you'd like to disclose confidentially please contact Red Hat's Product Security team. Details at https://access.redhat.com/security/team/contact

<!--
## XXX References

If you have any further question about xxx, please refer to
[XXX help documentation](docs/xxx_help.md) for further information.
-->