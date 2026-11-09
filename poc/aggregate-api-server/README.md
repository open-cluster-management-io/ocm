# OCM SQL-backed APIService POC

This is a bounded proof of concept for serving selected Open Cluster Management
CRD APIs through Kubernetes API aggregation, with PostgreSQL as the object
store. It is **not** a production replacement for OCM CRD storage.

The POC registers nine group/versions in an isolated test cluster, where no
conflicting CRDs are installed. An `APIService` owns the entire group/version,
not an individual CRD. The installer refuses deployment if any served CRD or
APIService conflicts with a selected group/version. Never install this POC into
an OCM hub; it does not change cluster-manager CRD reconciliation or migrate
existing hub data.

The resource inventory and compatibility matrix are documented in
[docs/hub-api-compatibility.md](docs/hub-api-compatibility.md).

Architecture and implementation contracts are documented in
[architecture.md](architecture.md) and the [spec index](docs/specs/README.md).

The end-to-end proof of technology is the
[aggregate API demo](../../solutions/aggregate-api-demo): the standard
three-cluster OCM developer flow with the hub's OCM group/versions served
from PostgreSQL after the POC takes over the CRD-backed APIServices. Its
recorded results and object counts are in the
[demo README](../../solutions/aggregate-api-demo/README.md#proof-of-technology-output-recorded-live-run-nov-2026).
The takeover mode it uses (`OCM_API_POC_TAKEOVER=yes`) exists only for that
disposable demo flow; the default installer mode refuses to run when a
selected group/version is already served.

## What it exercises

- Aggregator discovery and generic unstructured REST routes for the OCM APIs
  listed below, including create, get, list, update, merge-patch, status,
  delete, and watch.
- The watch-list protocol (`sendInitialEvents` with the
  `k8s.io/initial-events-end` bookmark) that client-go reflectors and
  `kubectl wait` use, plus label/field selectors on initial events.
- Cluster-wide (all-namespace) collection lists and watches, including the
  typed ManifestWork path used by the hub controllers' informers.
- Field-selector authorization: a collection list/watch scoped by
  `fieldSelector=metadata.name=X` is delegated as a get of `X`, matching
  kube-apiserver semantics so resourceNames-scoped RBAC (spoke agents) works.
- Specialized ManifestWork API support tested with the generated typed OCM
  client.
- PostgreSQL persistence with server metadata, monotonically increasing
  resource versions, optimistic concurrency, status/spec separation,
  finalizers, and a durable watch event journal.
- API aggregation proxy TLS client-certificate verification and delegated
  authorization through `SubjectAccessReview`.

### Aggregated OCM CRDs in this POC

| API group/version | Resources |
|---|---|
| `cluster.open-cluster-management.io/v1` | ManagedCluster |
| `cluster.open-cluster-management.io/v1beta2` | ManagedClusterSet, ManagedClusterSetBinding |
| `cluster.open-cluster-management.io/v1beta1` | Placement, PlacementDecision |
| `cluster.open-cluster-management.io/v1alpha1` | AddOnPlacementScore, ClusterClaim |
| `addon.open-cluster-management.io/v1alpha1`, `v1beta1` | ManagedClusterAddOn, AddOnDeploymentConfig, AddOnTemplate, ClusterManagementAddOn |
| `work.open-cluster-management.io/v1` | ManifestWork |
| `work.open-cluster-management.io/v1alpha1` | ManifestWorkReplicaSet |
| `internal.open-cluster-management.io/v1beta1` | ManagedClusterInfo |

ManagedClusterInfo was confirmed on an existing hub cluster. Its CRD is provided
outside this repository, so this POC includes its API route but does not own or
reconcile its schema or its current producer/controller.

## Run unit tests

```bash
make test
```

Discovery and generic-resource tests exercise CRUD, status isolation, and
delegated LIST authorization against an in-memory store. The generated typed
ManifestWork client test exercises its API path and WATCH. PostgreSQL lifecycle
tests are enabled when `POSTGRES_TEST_DSN` is set; otherwise they are skipped.

Use a dedicated disposable test database for `POSTGRES_TEST_DSN`: the test
creates the POC tables and appends watch journal rows.

## Deploy with make

The Makefile drives deployment into a disposable cluster that does not have
any CRDs from the selected group/versions installed. It auto-detects docker
and falls back to podman (setting `KIND_EXPERIMENTAL_PROVIDER=podman`). With
podman, the image is loaded into kind through `kind load image-archive` with a docker.io
qualified tag, because `kind load docker-image` requires a `docker` CLI even
under the podman provider.

### Kind (fully automated)

```bash
make deploy-kind     # create/reuse kind cluster, build+load image, deploy, smoke test
make smoke KUBECONTEXT=kind-ocm-api-poc
make undeploy KUBECONTEXT=kind-ocm-api-poc
make redeploy-kind   # undeploy + deploy
make kind-delete     # delete the kind cluster entirely
```

Override `KIND_CLUSTER_NAME` (default `ocm-api-poc`), `NAMESPACE`
(default `ocm-api-poc`), or `IMAGE` as needed. The database password is
generated automatically; pass `DB_PASSWORD=...` to choose your own.

### An existing kubectl context

The image must be pullable by the target cluster, so build and push it to a
registry first:

```bash
make image push-image IMAGE=quay.io/<you>/ocm-aggregate-api-poc:dev
make deploy-local KUBECONTEXT=<kubectl-context> IMAGE=quay.io/<you>/ocm-aggregate-api-poc:dev
```

To use the current context instead of naming one, confirm explicitly:

```bash
make deploy-local CONFIRM_CURRENT_CONTEXT=yes IMAGE=quay.io/<you>/ocm-aggregate-api-poc:dev
```

The installer refuses to run when a selected group/version has a served CRD or
APIService already installed, and requires POC ownership labels before reusing
its namespace. It prints the context and API server before deploying and runs
smoke tests after deployment. Set
`LOCAL_PULL_POLICY=Always` to force re-pulls on redeploy.

### What the smoke test covers

Availability for all nine APIServices; ManifestWork create/get/list/status/watch
and delete; ManagedClusterInfo create/get/status/delete; and cluster-scoped
ManagedCluster create/get/delete. It requires kubectl >= 1.24 for
`--subresource` support. Smoke tests use a unique temporary namespace and
refuse to reuse a pre-existing namespace.

### Removing the deployment

```bash
make undeploy KUBECONTEXT=kind-ocm-api-poc  # also deletes PostgreSQL's disposable data
```

## Database-backed internal Kubernetes-native data

Kubernetes-native resources are deliberately **not** registered through these
APIServices. The generic PostgreSQL object-store interface can represent a
keyed unstructured object, but there is no internal REST API or controller
integration for the following yet; they are **not migrated**:
managed-cluster Leases/heartbeats, CSRs and certificate approval/signing,
Secrets, ConfigMaps, and related internal registration data.
Their existing OCM producers and consumers still use Kubernetes APIs. Moving
them requires a separately authenticated internal REST contract and coordinated
controller/agent changes. Kubernetes infrastructure such as namespaces, RBAC,
and service accounts remains Kubernetes-managed.

## Compatibility gaps and risks

- The generic routes are not a Kubernetes generic API server. Apart from
  selected basic metadata checks, they do not implement CRD structural schema
  validation, pruning, defaults, CEL, admission webhooks, OpenAPI schemas, or
  conversion pipelines. In particular, serving both AddOn API versions does
  not perform their CRD conversion.
- Only JSON merge patch is implemented. Server-side apply, strategic merge
  patch, watch bookmarks, collection deletion, full field-selector support,
  and snapshot-consistent continuation tokens are out of scope.
- Label selectors are evaluated after fetching the namespace's rows. The watch
  journal is not compacted. Both choices are suitable only for a disposable
  POC, not scale testing.
- PostgreSQL's single revision-row lock serializes writes to preserve watch
  order. This is correct for a small demonstration but is a throughput
  bottleneck.
- The POC does not demonstrate lower etcd load by itself. A separate workload
  and baseline measurement are required.
- APIService registration takes ownership of all resources in each selected
  group/version. The POC only exposes the configured OCM resource list; other
  resources in those versions would be masked. Migration/rollout and
  coexistence with cluster-manager reconciliation remain unresolved.

## Follow-on decisions

This prototype uses PostgreSQL as a provisional choice because the evaluated
hub use case needs concurrent writers and a path to multi-replica operation.
The choice should be revisited against future evaluation results. SQLite remains
useful for a local single-process prototype, but this implementation does not
claim SQLite/PostgreSQL equivalence or production readiness.
