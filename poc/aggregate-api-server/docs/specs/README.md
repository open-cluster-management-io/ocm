# Aggregate API server POC specifications

These documents cover only `poc/aggregate-api-server`; they are not contracts
for OCM's production hub APIs.

- [POC architecture](../../architecture.md) — subsystem boundaries, ownership,
  request flow, and persistence/deployment context.
- [Aggregated API and authorization](SPEC_aggregate-api.md) — discovery,
  resource routes, generic and ManifestWork behavior, authentication, and SAR.
- [PostgreSQL storage](SPEC_postgres-storage.md) — schema, transactions,
  resource versions, watch journals, and data lifecycle.
- [Deployment and validation](SPEC_deployment.md) — Make targets, APIService
  registration guards, cleanup ownership, and smoke tests.
