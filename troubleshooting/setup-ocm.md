# Troubleshooting for setting up OCM

### I am getting an error about "Error: no CSR to approve for cluster <cluster-name>" when running the "clusteradm accept" command.

On the managed cluster, check the `klusterlet-registration-agent` pod log by using:

```
kubectl -n open-cluster-management-agent get pod
kubectl -n open-cluster-management-agent logs pod/klusterlet-registration-agent-<suffix> # Replace <suffix> with output from the "get pod" command.
```

If you are using `KinD` clusters to setup OCM, ensure you use the `--force-internal-endpoint-lookup` flag when you join the cluster using the `clusteradm join` command. For example:

```
 clusteradm join --force-internal-endpoint-lookup --wait --hub-token <hub-token> --hub-apiserver <hub-apiserver> --cluster-name <cluster-name>
```
