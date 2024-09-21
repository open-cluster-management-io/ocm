# Multiplehubs

In this demo, we are going to use 3 clusters: `hub1`, `hub2` and `agent` to show the MultipleHubs feature.

We will enable MultipleHubs feature on the managedcluster, the agent will first be registered to hub1.

Then we will trigger the managedcluster to switch to hub2 by manually by setting the ManagedCluster `hubAcceptsClient` to false on hub1.

After that, we will observe that the managedcluster will automatically register to hub2.

Next, We set the ManagedCluster `hubAcceptsClient` to true on hub1 and delete the hub2, we will observe that the managedcluster will automatically register back to hub1 and be available.

## Prerequisites

Run the following command to install the prerequisites:

```bash
sh setup.sh
```

## Switch agent to hub2

Run the following command to switch the managedcluster to hub2, first set the ManagedCluster `hubAcceptsClient` to false on hub1, then watch the managedcluster to be unaccepted from hub1:

```bash
kubectl --context kind-hub1 patch managedcluster agent --type=merge -p '{"spec":{"hubAcceptsClient":false}}'
kubectl --context kind-hub1 get managedcluster -w
```

Wait for the managedcluster to be `Available` on hub2:

```bash
kubectl --context kind-hub2 get managedcluster -w
```

## Switch agent to hub1

Set the ManagedCluster `hubAcceptsClient` back to true on hub1:
```bash
kubectl --context kind-hub1 patch managedcluster agent --type=merge -p '{"spec":{"hubAcceptsClient":true}}'
```

It takes about 5 minutes for the managedcluster to be updated to `Unknown` on hub1:

```bash
kubectl --context kind-hub1 get managedcluster -w
```

Delete the hub2:

```bash
kind delete cluster --name hub2
```

After 5 minutes, the managedcluster to be switched back to hub1 and be `Available`:

```bash
kubectl --context kind-hub1 get managedcluster -w
```
