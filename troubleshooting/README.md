
# Troubleshooting

This document describes common issues and troubleshooting guidance for
Open Cluster Management components.

---

## Klusterlet bootstrap fails with TLS handshake timeout due to MTU mismatch

Klusterlet bootstrap or certificate rotation may fail when the managed
cluster network MTU is larger than the hub cluster network MTU.

In such cases, some bootstrap API calls (for example,
`SelfSubjectAccessReview`) can generate TLS handshake packets that exceed
the hub path MTU. These packets may be silently dropped, resulting in
errors such as:


### Symptoms

- Klusterlet fails to complete bootstrap
- Managed cluster appears disconnected or degraded
- Failures recur during certificate rotation
- Basic connectivity checks (for example, `curl` to the hub API) may still succeed

### Resolution

Ensure that the managed cluster network MTU does not exceed the hub
cluster network MTU during bootstrap and certificate rotation.
Aligning MTU settings between hub and managed clusters prevents these
TLS handshake timeouts.

Related issue:
https://github.com/open-cluster-management-io/ocm/issues/1314

