---
description: "Implementation phase. Write code, unit tests, and integration tests module by module based on design."
---

# /dev-impl

Read `.claude/workflow/PROTOCOL.md` first. Resolve the feature directory. Verify current phase is `implementation`.

## Process

Read `<slug>/design.md` to get the ordered module list. Check `<slug>/state.md` Context to see which module we're on.

### Pre-check: Detect existing changes

Before starting, run `git diff --name-only` to check if the user has already written code for any modules. If files listed in the design have been modified:
1. Show which files already have changes.
2. Ask the user: "It looks like you've already started on some code. Which module should I pick up from? Should I review your changes or continue from where you left off?"
3. Adjust the starting module accordingly. Do NOT overwrite user-written code.

### Per-Module Cycle

Do **one module at a time**. After each module, pause for user confirmation before proceeding to the next.

#### 1. Understand context

- Read the existing files that will be modified. Understand the patterns, naming, error handling, and logging style used there.
- Read existing tests in the same package to understand test patterns and helpers.

#### 2. Write implementation

- Implement the module's functionality following the design.
- Follow the code standards from PROTOCOL.md (import order, line length, patterns).
- Match the style of surrounding code — don't introduce new patterns unless the design explicitly calls for it.
- Confirm it compiles: `go build ./pkg/path/...` (or the relevant cmd package).

#### 3. Write tests

- **Unit tests**: add `*_test.go` alongside the source in `pkg/`. Use the same test framework as the existing tests in that package (standard `testing` or Ginkgo/Gomega).
- **Integration tests**: if the design calls for it, add test cases in `test/integration/<component>/`. Follow existing Ginkgo patterns. Do NOT run integration tests at this stage — they will be verified in `/dev-verify`.
- Tests should cover: happy path, error cases, edge cases from the acceptance criteria.

#### 4. Run unit tests

Run only unit tests for the changed packages:
```bash
go test ./pkg/path/...
```

Also run `go vet ./pkg/path/...` on changed packages.

If tests fail, fix and re-run until green.

#### 5. Module checkpoint

Show the user a brief summary of what was done:
```
Module N/M complete: <module name>
  Files changed: <list>
  Tests added:   <list>
  Unit tests:    PASS

Continue to module N+1: <next module name>? (yes / review changes / stop here)
```

- **yes**: proceed to the next module.
- **review changes**: show `git diff` for this module's files, wait for feedback.
- **stop here**: update state and end. The user can resume later with `/dev-impl`.

#### 6. Update progress

Update `<slug>/state.md` Context after each module:
- Mark which module was completed
- Note the next module to work on
- Record any design deviations or decisions made during implementation

### Design Deviation

If the design is not feasible for a module:
1. STOP and explain to the user what doesn't work.
2. Propose alternatives.
3. If design changes are needed: "Run `/dev-rollback design` to revise the design."

## Completion

After ALL modules are done:
1. Run `go build ./cmd/...` to ensure the full project compiles.
2. Update `<slug>/state.md`: check `[x] implementation`, set phase to `verify`, update Context, update timestamp.
3. Say: "All modules implemented. Run `/dev-verify` to run full verification (lint, unit tests, integration tests)."
