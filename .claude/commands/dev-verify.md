---
description: "Pre-commit verification. Run lint, unit tests, and relevant integration tests."
---

# /dev-verify

Read `.claude/workflow/PROTOCOL.md` first. Resolve the feature directory. Verify current phase is `verify`.

## Process

Read `<slug>/design.md` to know which components were changed.

Run checks in order. Track results for the final summary.

### Step 1: Formatting and lint

**Auto-fixable issues** — fix without asking:
```bash
make fmt-imports
```

Then run full verification:
```bash
make verify
```

If lint reports code issues (not formatting), fix them, show the user what changed, and re-run `make verify`.

### Step 2: Build

```bash
go build ./cmd/...
```

### Step 3: Unit tests

```bash
make test-unit
```

If tests fail: diagnose the root cause, fix, and re-run only the failing tests first (`go test -run TestName ./pkg/path/...`), then re-run `make test-unit` for the full suite.

### Step 4: Integration tests

Based on the components modified (from design.md), run in two passes:

**Pass 1 — Focus on new/modified test cases only:**
```bash
make test-<component>-integration ARGS='-ginkgo.focus="<new test pattern>"'
```
This catches issues in the new code quickly without waiting for the full suite.

**Pass 2 — Full suite for regression:**
```bash
make test-<component>-integration
```

Relevant suites:
- Registration: `make test-registration-integration`
- Placement: `make test-placement-integration`
- Work: `make test-work-integration`
- Addon: `make test-addon-integration`
- Operator: `make test-registration-operator-integration`

If unsure which suites are relevant, ask the user.

### Failure Handling

- **Format/lint failures**: auto-fix, no need to ask.
- **Unit test failures**: diagnose, fix implementation or test, re-run the failing test, then re-run full suite.
- **Integration test failures**: show the failure log. If the fix is trivial (e.g., wrong assertion, missing RBAC rule), fix and re-run. If it requires rethinking the implementation, warn the user and suggest `/dev-rollback implementation`.
- After any fix that changes behavior (not just formatting), show the user what changed.

## Completion

When ALL checks pass, show a summary:

```
=== Verification Results ===
  make verify:        PASS
  go build:           PASS
  unit tests:         PASS (N tests)
  integration tests:  PASS (suites: <list>)

All checks passed. Run `/dev-commit` to commit.
```

Update `<slug>/state.md`: check `[x] verify`, set phase to `commit`, update Context with results summary, update timestamp.
