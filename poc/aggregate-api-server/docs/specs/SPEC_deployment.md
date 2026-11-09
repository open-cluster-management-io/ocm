# Deployment and validation specification

## Purpose and ownership

`Makefile` and `deploy/` automate deployment of the isolated POC API server and
PostgreSQL, APIService registration, smoke validation, and teardown. They do
not deploy into or modify an OCM hub's CRD reconciliation configuration.

## Entry points

Run commands from `poc/aggregate-api-server`.

| Make target | Behavior |
|---|---|
| `test` | `go test ./...`; PostgreSQL integration tests require `POSTGRES_TEST_DSN`. |
| `image`, `push-image` | Build and optionally push the POC image using Docker or Podman. |
| `kind-create`, `kind-delete` | Create/reuse or delete the configured disposable Kind cluster. |
| `deploy-kind` | Create/reuse Kind, build/load image, install, then smoke test. |
| `deploy-local` | Install and smoke test in an explicitly named context, or current context only with `CONFIRM_CURRENT_CONTEXT=yes`. Image must be pullable. |
| `smoke` | Run destructive create/delete tests in a unique temporary namespace. Requires `KUBECONTEXT` or explicit current-context confirmation. |
| `undeploy` | Remove only owned POC APIServices, ClusterRoleBinding, and namespace. Requires explicit context or current-context confirmation. Namespace removal deletes disposable database data. |
| `redeploy-kind` | Undeploy from the configured Kind context, then install again; data is destroyed. |

The Makefile prefers Docker, falling back to Podman. Since `kind load
docker-image` invokes a Docker CLI even with the Podman provider, the Podman
path builds a Docker-qualified image, saves an archive, and invokes `kind load
image-archive`.

## Installation flow and guards

`deploy/install.sh` requires the image and a disposable database password. It
selects the explicit `OCM_API_POC_KUBECONTEXT` when provided and prints the
target context and server. Before installing, it:

1. Lists CRDs and refuses any served version overlapping the configured API
   Service group/versions in `internal/server/apiservices.txt`.
2. Lists APIServices and refuses any existing matching APIService.
3. Lists namespaces and either creates the target namespace with the POC owner
   label or refuses to reuse an unowned namespace.
4. Reads the extension-apiserver authentication ConfigMap for proxy CA and
   allowed names, then generates a short-lived self-signed serving certificate.
5. Creates/reuses database and proxy credentials, applies PostgreSQL and
   apiserver workloads, then registers and waits for each APIService.

The APIService certificate is mounted into the API server and its CA is
embedded in each registration object. The apiserver service account is bound
to `system:auth-delegator` for SAR requests. Deployment is designed for an
isolated cluster; the guards avoid known ownership conflicts but do not migrate
data or coordinate with OCM CRD reconciliation.

## Teardown and ownership

`deploy/uninstall.sh` requires an explicit context unless the caller confirms
current-context use. It lists APIServices, ClusterRoleBindings, and namespaces;
if any matching resource exists without
`app.kubernetes.io/managed-by=ocm-aggregate-api-poc`, cleanup refuses. Once
ownership is verified, it deletes the configured APIServices, the POC
auth-delegator binding, and the named namespace. Namespace deletion removes
the PostgreSQL `emptyDir` data.

`kind-delete` deletes the whole named Kind cluster and is more destructive than
`undeploy`.

## Smoke validation

`deploy/smoke.sh` waits for all configured APIServices, checks the current
principal can create ManifestWorks, creates a unique temporary namespace, and
cleans it up on exit. It exercises ManifestWork create/get/resourceVersion,
label-selected list, status patch, watch, and delete; ManagedClusterInfo
create/get/status/delete; and cluster-scoped ManagedCluster create/get/delete.
It uses `kubectl --validate=false` for objects without served OpenAPI schemas.
These checks establish basic aggregated routing and CRUD only; they do not
verify all descriptors, schema/admission semantics, restricted-principal RBAC,
or end-to-end OCM controller/agent workflows.

## Operational gaps

- The smoke test uses the current kubectl principal and does not establish a
  restricted-user authorization matrix.
- The remote-registry `deploy-local` path, multi-replica behavior, durable
  PostgreSQL storage, backup/restore, and etcd load reduction are not validated
  by this deployment spec.
- CRD conflict detection is version-scoped, but APIService takes ownership of
  all resources in that group/version. The POC catalog may not include every
  resource installed by an OCM distribution.
- The installer and smoke test have not established safe production rollout or
  data migration procedures.

## Acceptance checks

- Each deployment script passes `bash -n <script>` when checked separately.
- Unit/race tests, `go vet`, and `go build` pass in the POC module.
- Live deployment and smoke tests must only target a disposable cluster without
  conflicting CRDs; do not target a real hub.

## Related specifications

- [Aggregated API](SPEC_aggregate-api.md)
- [PostgreSQL storage](SPEC_postgres-storage.md)
- [Architecture](../../architecture.md)
