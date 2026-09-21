<!-- Copyright Contributors to the Open Cluster Management project -->

# Open Cluster Management Core

## Communication

- Be concise. Skip preambles and postambles.
- Comments explain why, not what.
- Errors should be actionable and specific.

## Constraints

- This repository contains the registration, placement, work, registration-operator, and addon-manager components.
- DCO is required. Sign commits with `git commit -s`.
- Generated CRDs, CSVs, and code are read-only. Change their sources, then run `make update`.
- Use `make fmt-imports` for Go import grouping; do not hand-format generated files.
- Follow `CONTRIBUTING.md` for AI-assisted contribution and disclosure requirements.

## Commands

Run `make help` for available targets. Common workflows:

```text
make test-unit GO_TEST_PACKAGES=./pkg/placement/...  # Unit tests for one component
make test-integration                               # All integration tests
make fmt-imports                                    # Format Go imports
make update                                         # Update generated artifacts
make verify                                         # Run formatting, CRD, and lint checks
```

## Style

- Follow existing controller-runtime and library-go patterns.
- Use `klog/v2` for logging; avoid raw `fmt` output in controllers.
