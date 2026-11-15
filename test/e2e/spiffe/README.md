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

A third cluster, `ocm-e2e-east`, tests a federated trust domain: it runs its own SPIRE server and
agent in `east.example.org`, and the two servers federate. Each serves its trust bundle on a bundle
endpoint (`https_spiffe`, a NodePort on the kind network), and each has a
`ClusterFederatedTrustDomain` for the other. The hub's gRPC server entry federates with
`east.example.org`, and the east agents' entries with `ocm-hub.example.org`.

## Scripts

| Script | Does |
| --- | --- |
| `setup.sh` | creates the clusters, installs SPIRE, runs `smoke.sh` |
| `smoke.sh` | a pod that looks like cluster1's work agent gets its SVID |
| `images.sh` | builds the OCM images from this checkout and loads them into both clusters |
| `install-ocm.sh` | hub with the spiffe driver, join with no token, entries after `SPIFFEAdmissionReady`, accept |
| `check-deny.sh` | a denied cluster gets no work and its lease stops; accepting again resumes both |
| `check-rotation.sh` | `ManifestWork` changes keep arriving across SVID rotations |
| `check-delete.sh` | deleting a cluster: its work is cleaned up, then the cluster is held until every SVID has expired |
| `setup-federation.sh` | creates `ocm-e2e-east` with its own SPIRE in `east.example.org` and federates it with the hub's |
| `check-federation.sh` | joins `ocm-e2e-east` as `cluster3`: refused while `east.example.org` isn't in `agentTrustDomains`, then pinned to it, accepted and given work; refused again when pinned to another trust domain |
| `teardown.sh` | deletes the clusters |
