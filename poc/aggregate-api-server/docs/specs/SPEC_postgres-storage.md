# PostgreSQL storage specification

## Purpose and ownership

The POC stores typed ManifestWork and other configured OCM resource objects in
PostgreSQL. Storage implementation lives in `internal/store/postgres.go` and
`internal/store/objects.go`; both are initialized by `store.OpenPostgres`.

This storage contract does not imply that Kubernetes-native Leases, CSRs,
Secrets, ConfigMaps, or OCM controller state has been migrated. No internal REST
service or client integration for those objects exists.

## Schema and keys

The typed ManifestWork store creates:

- `manifestwork_revision`: one global revision counter for typed ManifestWork.
- `manifestworks`: keyed by `(namespace, name)`, with numeric
  `resource_version` and JSONB `body`.
- `manifestwork_events`: ordered event journal including current and previous
  object bodies; indexed by namespace and revision.

The generic object store creates:

- `poc_object_revision`: one counter shared across generic OCM resources.
- `poc_objects`: keyed by `(api_group, api_version, resource, namespace,
  name)`, with numeric resource version and JSONB body.
- `poc_object_events`: revision-keyed `ADDED`/`MODIFIED`/`DELETED` journal
  records with current and optional previous JSONB bodies. A composite index
  supports group/version/resource/namespace watch reads.

Namespace is the empty string for cluster-scoped records. Group and version are
part of the storage key, so two served versions do not share a row or undergo
conversion.

## Write and concurrency behavior

Each write runs in a SQL transaction. A row update to the relevant revision
counter allocates the next monotonically increasing resource version, serializing
writes sharing that counter. Object and event writes commit atomically.

Create assigns UID, creation timestamp, generation 1, and resource version. An
existing key returns AlreadyExists. Update locks the row and requires the
submitted resource version to match; stale or missing versions return Conflict.
UID and creation/deletion metadata are preserved. Normal updates preserve
status and increment generation only when spec JSON changes; status updates
preserve the spec and metadata. Finalized deletion first sets deletion timestamp
and emits a modified event; clearing the finalizers removes the row and emits a
delete event.

The ManifestWork typed store implements corresponding typed metadata,
concurrency, status, and finalizer rules in its own tables and revision counter.

## Reads, lists, and watches

Get selects by the resource's full key and returns NotFound on a missing row.
List uses a repeatable-read transaction to read the revision and object rows in
a consistent snapshot. Generic list filtering and pagination are performed by
the HTTP handler after loading rows; selector evaluation is not pushed into
SQL. Continuation markers do not provide snapshot continuation semantics.

Watch reads at most 250 events per database query, advances its resource
version through the event stream, and polls from the handler. Generic watch
queries are scoped by group/version/resource and optionally namespace. Journals
are not compacted and there is no watch bookmark or expired-resource-version
recovery protocol.

## Lifecycle and operations

`OpenPostgres` opens the pgx `database/sql` driver, configures connection-pool
limits, pings the database, then creates tables and indexes with
`CREATE ... IF NOT EXISTS`. Closing the store closes the database handle.
Migrations are additive POC initialization; schema upgrade, backup, restore,
HA, and production retention procedures are not defined.

The Kind deployment's PostgreSQL Pod uses `emptyDir`. Deleting the POC namespace
or replacing the database Pod can destroy POC data. The disposable database
password is supplied through a Kubernetes Secret; this does not establish
production secret-management or encryption-at-rest requirements.

## Failure behavior and constraints

Database connection, query, decode, transaction, and commit failures are
returned as errors and become internal API errors (or watch `ERROR` events once
the stream has started). Unique-key violations map to AlreadyExists; missing
records map to NotFound; stale resource versions map to Conflict.

The single revision counter serializes writes, and the event journal grows
without compaction. Both are POC constraints, not validated throughput or
retention characteristics. JSONB storage does not apply CRD pruning, defaults,
validation, or version conversion.

## Acceptance checks

- Store tests exercise the typed ManifestWork lifecycle in memory or PostgreSQL
  when `POSTGRES_TEST_DSN` is set.
- The same opt-in PostgreSQL test exercises generic object creation, assigned
  metadata, status-only updates, event retrieval, and delete.
- Use a dedicated disposable database: tests create tables and write rows.

## Related specifications

- [Aggregated API](SPEC_aggregate-api.md)
- [Deployment](SPEC_deployment.md)
- [Architecture](../../architecture.md)
