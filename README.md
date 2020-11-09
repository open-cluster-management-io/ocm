# registration-operator

Minimum cluster registration and work

## Community, discussion, contribution, and support

Check the [CONTRIBUTING Doc](CONTRIBUTING.md) for how to contribute to the repo.

# How to Deploy

## Deploy all-in-one deployment on kind

1. Create a kind cluster
2. Deploy all compoenent on the kind cluster
    ```
    export KIND_CLUSTER={kind cluster name}
    make deploy
    ```
3. To clean the environment, run `make clean-deploy`

## Deploy on OCP

1. Deploy hub component
    ```
    export OLM_NAMESPACE=openshift-operator-lifecycle-manager
    make deploy-hub
    ```
2. Deploy agent component
    ```
    export KLUSTERLET_KUBECONFIG_CONTEXT={kube config context of managed cluster}
    export OLM_NAMESPACE=openshift-operator-lifecycle-manager
    make deploy-spoke
    ```
3. To clean the environment, run `make clean-hub` and `make clean-spoke`

## What is next

After a successfull deployment, a `certificatesigningrequest` and a `managedcluster` will
be created on the hub.

```
kubectl get csr
kubectl get managedcluster
```

Next approve the csr and set managecluster to be accepcted by hub with the following command

```
kubectl certificate approve {csr name}
kubectl patch managedcluster {cluster name} -p='{"spec":{"hubAcceptsClient":true}}' --type=merge
```

## Security Response

If you've found a security issue that you'd like to disclose confidentially please contact
Red Hat's Product Security team. Details at https://access.redhat.com/security/team/contact

## License

This code is released under the Apache 2.0 license. See the file LICENSE for more information.
