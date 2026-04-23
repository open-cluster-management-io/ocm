# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Open Cluster Management (OCM) — a CNCF project for multicluster Kubernetes management using a hub-spoke architecture. Five core components: **registration** (cluster registration & cert lifecycle), **work** (manifest distribution to managed clusters), **placement** (scheduling decisions across clusters), **registration-operator** (deploys/manages OCM components), **addon-manager** (add-on lifecycle).

APIs live in a separate repo (`open-cluster-management.io/api`). To test local API changes, use `go mod edit -replace open-cluster-management.io/api=/path/to/local/api` then `go mod tidy && go mod vendor && make update`. Remove the replace directive before submitting a PR.

## Build & Test Commands

```bash
make build                           # Build all binaries
make images IMAGE_TAG=dev            # Build all container images
make verify                          # All checks: lint + import formatting + CRD verification
make fmt-imports                     # Auto-fix import ordering

# Unit tests
make test-unit                       # All unit tests (./pkg/...)
go test ./pkg/placement/...          # Single package
go test -run TestName ./pkg/work/... # Single test

# Integration tests (use envtest, need kubebuilder assets)
make test-integration                          # All integration suites
make test-registration-integration             # Registration only
make test-placement-integration                # Placement only
make test-work-integration                     # Work only
make test-addon-integration                    # Addon only
make test-registration-operator-integration    # Operator only
# Focus on specific test:
make test-work-integration ARGS='-ginkgo.focus="pattern"'

# E2E tests (require kind cluster + KUBECONFIG)
IMAGE_TAG=e2e KLUSTERLET_DEPLOY_MODE=Singleton make test-e2e
make test-e2e ARGS='-ginkgo.focus="pattern"'

# Code generation (after API changes)
make update                          # Copy CRDs, update Helm charts, CSV manifests
```

## Code Conventions

- **All committed files must be in English** — no Chinese or other non-English text in source code, comments, tests, docs, commit messages, or workflow files
- **Go 1.25**, uses vendored dependencies (`go mod vendor`)
- **Test framework**: Ginkgo/Gomega (BDD-style) for integration/e2e; standard `testing` for unit tests
- **Import order** (enforced by `make verify-fmt-imports`): standard lib, third-party, `open-cluster-management.io/*`, local module
- **Max line length**: 160 characters
- **Commits require DCO sign-off**: `git commit -s`
- **PR title prefixes**: `:sparkles:` feature, `:bug:` fix, `:book:` docs, `:seedling:` misc, `:warning:` breaking change

## Architecture Notes

Each component runs on hub, spoke, or both:
- `pkg/registration/hub/` and `pkg/registration/spoke/` — hub approves CSRs and manages ManagedCluster; spoke bootstraps and renews certificates
- `pkg/work/hub/` and `pkg/work/spoke/` — hub tracks ManifestWork status; spoke applies manifests to managed cluster
- `pkg/placement/controllers/` — hub-only, schedules workloads via Placement/PlacementDecision resources
- `pkg/operator/` — manages deployment of all other components via ClusterManager (hub) and Klusterlet (spoke) CRs
- `pkg/addon/controllers/` — hub-only, manages add-on installation and lifecycle

Integration tests are compiled as separate test binaries (`go test -c`) then executed with Ginkgo flags. They live under `test/integration/{registration,placement,work,addon,operator}/`.

## Development Workflow

This repo has a structured development workflow via slash commands. Each feature/bugfix gets an isolated workflow directory based on its git branch (e.g., branch `feature/foo` -> `.claude/workflow/feature-foo/`). Multiple features can be developed in parallel without conflict.

**Phases**: requirements -> design -> implementation -> verify -> commit -> PR

**Commands**:
- `/dev-start` — Start a new feature/bugfix or resume the current branch's workflow
- `/dev-status` — Quick view of current and all active workflows
- `/dev-require` — Requirements analysis (collaborative)
- `/dev-design` — Solution design: packages, interfaces, dependencies, module breakdown
- `/dev-impl` — Implementation: write code + unit tests + integration tests, module by module
- `/dev-verify` — Run `make verify`, unit tests, and relevant integration tests
- `/dev-commit` — Stage and commit with DCO sign-off
- `/dev-pr` — Push and create GitHub PR
- `/dev-rollback` — Roll back to a previous phase

**Shared protocol**: All commands read `.claude/workflow/PROTOCOL.md` for shared conventions (how to resolve the feature directory, update state, display status, code standards).

**State persistence**: Each feature's state lives in `.claude/workflow/<branch-slug>/state.md`. The Context section captures enough detail to resume cold in a new session.
