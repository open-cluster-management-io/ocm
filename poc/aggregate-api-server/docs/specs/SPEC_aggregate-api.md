# Aggregated API and authorization specification

## Purpose and ownership

The POC exposes selected OCM API group/versions through Kubernetes API
aggregation and stores objects in PostgreSQL. This contract is implemented by
`internal/server/handler.go`, `internal/server/generic_handler.go`,
`internal/server/resources.go`, and `internal/server/authorizer.go`.

This is not a Kubernetes generic API server. The generic routes use unstructured
JSON and do not reproduce CRD schemas, defaulting, CEL, admission, OpenAPI, or
conversion behavior. The server does not move Kubernetes-native resources into
an internal REST API.

## API catalog

The catalog is the union of `internal/server/apiservices.txt` and
`resources()` in `internal/server/resources.go`:

| Group/version | Resource(s) | Scope |
|---|---|---|
| `cluster.open-cluster-management.io/v1` | `managedclusters` | Cluster |
| `cluster.open-cluster-management.io/v1beta2` | `managedclustersets`, `managedclustersetbindings` | Cluster, Namespaced |
| `cluster.open-cluster-management.io/v1beta1` | `placements`, `placementdecisions` | Namespaced |
| `cluster.open-cluster-management.io/v1alpha1` | `addonplacementscores`, `clusterclaims` | Namespaced, Cluster |
| `addon.open-cluster-management.io/v1alpha1`, `v1beta1` | `managedclusteraddons`, `addondeploymentconfigs` | Namespaced |
| `addon.open-cluster-management.io/v1alpha1`, `v1beta1` | `addontemplates`, `clustermanagementaddons` | Cluster |
| `work.open-cluster-management.io/v1` | `manifestworks` | Namespaced |
| `work.open-cluster-management.io/v1alpha1` | `manifestworkreplicasets` | Namespaced |
| `internal.open-cluster-management.io/v1beta1` | `managedclusterinfos` | Namespaced |

`GET /apis`, `GET /apis/{group}`, and `GET /apis/{group}/{version}` provide
discovery. `/livez` and `/readyz` return `200 ok`; these and discovery routes
do not require an aggregation proxy client certificate.

For configured resources, the handler supports collection and named-resource
`GET`, collection `POST`, named `PUT`, named JSON merge `PATCH`, named
`DELETE`, and `GET` watch. Namespaced resources use
`/namespaces/{namespace}/{resource}`; collection paths without a namespace list
all namespaces. Cluster-scoped resources use `/{resource}`. Resources marked
with `HasStatus` also advertise `/status`; writes to that route are delegated
as the `status` subresource.

ManifestWork (`work.open-cluster-management.io/v1/manifestworks`) has a
specialized typed implementation. Generic routes cover the other descriptors,
including ManifestWorkReplicaSet.

## Request and response behavior

### Generic resources

- Create requires `metadata.name`; `generateName` is not supported. Names are
  checked as DNS subdomains. Namespaced resources require a namespace; a
  request-path namespace overrides an omitted object namespace and must match a
  supplied one. Cluster-scoped resources reject `metadata.namespace`.
- The route sets the descriptor's `apiVersion` and `kind`. On create, objects
  with a status subresource have incoming `status` removed.
- Get returns the stored object. List returns `{Kind}List` with a list resource
  version and supports label selectors, field selectors for
  `metadata.name`/`metadata.namespace`, `limit`, and a base64 continuation
  marker. Collections are served both namespaced and cluster-wide (all
  namespaces), for generic resources and for the typed ManifestWork path.
- Watch streams JSON `ADDED`, `MODIFIED`, and `DELETED` events from the event
  journal, polls at 250 ms by default, and applies label/field selectors to
  events. When a client sends `sendInitialEvents=true` (the watch-list
  protocol used by client-go reflectors and `kubectl wait`), the server first
  streams `ADDED` events for the current objects followed by a `BOOKMARK`
  event annotated `k8s.io/initial-events-end=true`, then continues the
  regular stream; the selector filters apply to the initial events. Watch
  bookmarks (beyond the initial-events marker) and journal compaction are
  absent.
- Authorization mirrors kube-apiserver requestinfo semantics: a collection
  list or watch whose field selector requires an exact `metadata.name=X`
  match is delegated as a request for that named resource. This is what lets
  the resourceNames-scoped RBAC OCM grants spoke agents authorize their
  field-selector-scoped informers.
- Update and status update require the object's current resource version. A
  status update preserves the existing object/spec and replaces status; a
  normal update preserves existing status.
- Patch accepts only `application/merge-patch+json`, with a 2 MiB maximum read
  enforced by `MaxBytesReader`. Generic JSON decoding uses a 4 MiB
  `LimitReader`; it does not implement complete API-server body handling.
  Strategic merge, JSON Patch, and server-side apply are not implemented.
- Delete accepts a resource-version precondition. Finalizers set
  `deletionTimestamp`; object removal occurs after finalizers are cleared.

### ManifestWork-specific behavior

The typed route validates the basic embedded-manifest shape and status
conditions, enforces resource-version concurrency, isolates status writes, and
honors finalizers. It supports the generated OCM typed client. It does not
implement the CRD's complete structural schema, CEL, defaults, admission, or
OpenAPI behavior.

## Authentication and authorization

For API resource paths, `Handler.RequireProxyClientCert` requires TLS peer
certificate evidence. `cmd/apiserver/main.go` configures
`tls.VerifyClientCertIfGiven`, trusts the request-header client CA, and accepts
only configured certificate common names. The aggregator's health and
discovery probes may connect without a client certificate.

`SARAuthorizer` requires `X-Remote-User`, passes `X-Remote-Group` values, and
creates a SubjectAccessReview using the actual API group, version, verb,
namespace, resource, subresource, and name. Collection GET is authorized as
`list`, watch as `watch`. A denied review returns Forbidden; missing user
identity returns Unauthorized; SAR API errors fail the request.

The process's Kubernetes service account requires permissions to create
SubjectAccessReviews (provided in the POC manifest through
`system:auth-delegator`). The resource API does not bypass Kubernetes RBAC by
accessing PostgreSQL directly.

## Errors and compatibility constraints

Handlers return Kubernetes `Status` responses for API errors. Unsupported
resource paths return NotFound; unsupported HTTP methods return
MethodNotSupported; unsupported patch content types and malformed selectors
return BadRequest; resource-version mismatches return Conflict.

The AddOn resources expose both configured API versions, but the store keys
objects by group/version and does not convert between versions. Data written
through one version is not transparently shared with another. This must not be
treated as production API compatibility.

APIService owns a complete group/version. The deployment guard refuses an
existing served CRD or APIService for a configured version, but the POC only
advertises its descriptor catalog; other resources from those versions would
be masked. The server does not publish CRD OpenAPI schemas.

## Acceptance checks

- `go test ./...` covers discovery, generic ManagedClusterInfo CRUD/status,
  cluster scope checks, LIST SAR verb, the field-selector list authorization,
  the cluster-wide ManifestWork list, the watch-list initial-events bookmark,
  and the generated ManifestWork client.
- `deploy/smoke.sh` verifies all nine APIService availabilities, ManifestWork
  CRUD/status/watch, ManagedClusterInfo status, and ManagedCluster CRUD.
- `solutions/aggregate-api-demo/local-up-aggregate.sh` runs the full OCM
  dev-environment flow (clusteradm init, takeover, join, accept, ManifestWork
  delivery) against the aggregate server; see its README for the verified
  results and comparison counts.
- PostgreSQL lifecycle behavior requires `POSTGRES_TEST_DSN`; tests skip that
  path when it is unset.

## Related specifications

- [Storage](SPEC_postgres-storage.md)
- [Deployment](SPEC_deployment.md)
- [Architecture](../../architecture.md)
