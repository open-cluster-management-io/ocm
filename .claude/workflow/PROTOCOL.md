# Workflow Protocol

Shared conventions for all `/dev-*` commands. Every command MUST read this file first.

## Resolve Feature Directory

The workflow directory for the current feature is derived from the current git branch:

1. Run `git branch --show-current` to get the branch name.
2. Replace `/` with `-` to form the directory slug (e.g., `feature/placement-taint` -> `feature-placement-taint`).
3. The workflow directory is `.claude/workflow/<slug>/` (e.g., `.claude/workflow/feature-placement-taint/`).
4. If the directory does not exist and the command is NOT `/dev-start`, tell the user: "No workflow found for branch `<branch>`. Run `/dev-start` to initialize."

## State File Layout

Each feature directory contains:
- `state.md` — current phase and context
- `requirements.md` — requirements document
- `design.md` — design document

## Update State

When updating `.claude/workflow/<slug>/state.md`:
1. Check/uncheck the relevant phase in the Phases list.
2. Set `Current Phase` to the new phase name.
3. Update `Updated` to today's date.
4. Update the `Context` section with enough detail to resume cold in a new session (what was done, what's next, key decisions).

## Display Status

When showing workflow status, use this format:

```
=== OCM Dev Workflow ===
Feature:  <name> (<type>)
Branch:   <branch>
Phase:    <current phase>
Updated:  <date>

Progress:
  [x] requirements
  [x] design
  [ ] implementation  <-- current
  [ ] verify
  [ ] commit
  [ ] pr

Last context: <first 2-3 lines of Context section>
```

Then suggest the next command based on current phase.

## Phase Order

```
requirements -> design -> implementation -> verify -> commit -> pr
```

Never skip phases. Each phase must complete before the next begins.

## Phase-to-Command Mapping

| Phase          | Command        |
|----------------|----------------|
| requirements   | `/dev-require` |
| design         | `/dev-design`  |
| implementation | `/dev-impl`    |
| verify         | `/dev-verify`  |
| commit         | `/dev-commit`  |
| pr             | `/dev-pr`      |

## Code Standards (apply during implementation)

These must be followed during `/dev-impl`, not as a separate review step:
- Follow existing controller/reconciler patterns in the target package
- Import order: standard lib, third-party, `open-cluster-management.io/*`, local module
- Use Ginkgo/Gomega for integration tests, standard `testing` for unit tests (match the existing pattern in the target package)
- No unnecessary abstractions — match the complexity level of surrounding code
- Kubernetes API conventions: proper status updates, conditions, finalizers
- Max line length: 160 characters
