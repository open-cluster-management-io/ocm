# SPIFFE registration e2e

Two kind clusters, `ocm-e2e-hub` and `ocm-e2e-spoke`, with their own kubeconfig
(`_output/spiffe-e2e.kubeconfig`), so other clusters and the current kubectl context are untouched.

## SPIRE topology

One SPIRE server, on the hub, in trust domain `ocm-hub.example.org`. Each managed cluster runs only
a SPIRE agent and the SPIFFE CSI driver:

- the agent attests to the hub's server with `k8s_psat`; the server validates its token with the
  managed cluster's API (`kubeConfigs` + `nodeAttestor.externalK8sPSAT`);
- the server publishes its trust bundle into the managed cluster (`bundlePublisher.externalK8sConfigMap`);
- a controller manager per managed cluster runs next to the hub's server
  (`externalControllerManagers`) and turns that cluster's `ClusterSPIFFEID`s into entries, so the
  managed cluster's entries are ordinary `ClusterSPIFFEID`s applied on the managed cluster.

## Scripts

| Script | Does |
| --- | --- |
| `setup.sh` | creates the clusters, installs SPIRE, runs `smoke.sh` |
| `smoke.sh` | a pod that looks like cluster1's work agent gets its SVID |
| `images.sh` | builds the OCM images from this checkout and loads them into both clusters |
| `install-ocm.sh` | hub with the spiffe driver, join with no token, entries after `SPIFFEAdmissionReady`, accept |
| `teardown.sh` | deletes the two clusters |
