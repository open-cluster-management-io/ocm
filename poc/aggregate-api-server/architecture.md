# Aggregate API server POC architecture

## Scope

This document describes only `poc/aggregate-api-server`, a disposable proof of
concept. It is not the architecture of OCM's production hub APIs and does not
describe a completed migration from Kubernetes CRD storage. Kubernetes remains
the authentication and authorization authority; PostgreSQL is the POC object
store.

## Components and ownership

| Component | Responsibility | Entry points |
|---|---|---|
| API registry and HTTP handlers | Advertise the configured OCM resources, route REST requests, and keep the specialized ManifestWork path separate from generic unstructured resources. | `internal/server/resources.go`, `internal/server/handler.go`, `internal/server/generic_handler.go` |
| Delegated authorization | Convert the aggregator's authenticated user and groups into a Kubernetes SubjectAccessReview. | `internal/server/authorizer.go` |
| PostgreSQL stores | Persist typed ManifestWork objects and generic OCM objects, revisions, and watch events. | `internal/store/postgres.go`, `internal/store/objects.go` |
| API server process | Initialize PostgreSQL, in-cluster Kubernetes access, proxy TLS verification, and the HTTPS listener. | `cmd/apiserver/main.go` |
| Deployment tooling | Build and deploy the service, register APIServices, run smoke tests, and remove only owned test resources. | `Makefile`, `deploy/` |

The implementation is isolated in its own Go module. It does not modify
cluster-manager CRD reconciliation or OCM controller/agent clients.

## Request and data flow

```text
kubectl / OCM typed client
        |
        v
Kubernetes API server (authn, front-proxy headers, APIService routing)
        |
        | verified aggregation-proxy client certificate
        v
POC HTTPS server
        |-- discovery and health --> handler
        |-- resource request --> X-Remote-User/Group --> SubjectAccessReview
        |                         |-- ManifestWork --> typed ManifestWork store
        |                         `-- other configured OCM resources --> generic object store
        v
PostgreSQL JSONB rows and watch journals
```

`main.go` opens PostgreSQL and applies both storage migrations, creates an
in-cluster client for SAR requests, loads the serving certificate and the
cluster's request-header client CA, and starts an HTTPS server. On shutdown it
gracefully stops the listener and closes the database connection.

The aggregator may contact the extension server for health and discovery
without a client certificate. The TLS server verifies any presented client
certificate against the request-header CA and allowed common names; the handler
requires a verified peer certificate for resource routes. Resource identity and
RBAC decisions come from the aggregator headers and a delegated SAR, not from
the database.

## API ownership boundary

The catalog in `internal/server/apiservices.txt` contains nine group/versions:

- `cluster.open-cluster-management.io`: `v1`, `v1beta2`, `v1beta1`, `v1alpha1`
- `addon.open-cluster-management.io`: `v1alpha1`, `v1beta1`
- `work.open-cluster-management.io`: `v1`, `v1alpha1`
- `internal.open-cluster-management.io`: `v1beta1`

The resource descriptor table in `internal/server/resources.go` defines the
resources exposed for those versions, their kind, scope, and status subresource.
`ManagedClusterInfo` is included as an OCM API observed on an existing hub; its CRD,
schema, and producer are outside this repository.

An APIService owns a whole group/version, not an individual resource. The
installer therefore refuses any selected group/version that overlaps a served
CRD or an existing APIService. It does not remove CRDs, import data, alter OCM
reconciliation, or safely coexist with another resource served in the same
group/version. The POC only advertises its configured resources, so any other
resource in an owned version would be masked. Treat deployment as isolated-test
only.

## Persistence boundary

ManifestWork uses its typed store and dedicated `manifestworks`, revision, and
event tables. The remaining configured resources use the generic
`poc_objects`/`poc_object_events` tables keyed by API group, version, resource,
namespace, and name. This means versions are separate storage keys; the generic
store does not perform conversion between served API versions.

Both stores persist JSONB and watch journals. PostgreSQL revisions are allocated
transactionally and writes serialize through a revision row. The deployment
manifest uses disposable `emptyDir` storage for PostgreSQL, so replacing its Pod
can lose POC data. Persistence, backups, HA, and production recovery are not
established by this design.

Kubernetes-native Lease, CSR, Secret, and ConfigMap flows are not moved. There
is no internal REST API for them and no controller/agent integration with this
database. Kubernetes namespaces, RBAC, service accounts, and related platform
objects remain Kubernetes-managed.

## Detailed contracts

- [Aggregated API and authorization](docs/specs/SPEC_aggregate-api.md)
- [PostgreSQL object and watch storage](docs/specs/SPEC_postgres-storage.md)
- [Deployment, safety, and smoke tests](docs/specs/SPEC_deployment.md)
- [Spec index](docs/specs/README.md)
