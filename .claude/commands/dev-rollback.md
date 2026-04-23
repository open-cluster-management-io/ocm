---
description: "Roll back workflow to a previous phase. Usage: /dev-rollback or /dev-rollback <phase>"
---

# /dev-rollback

Read `.claude/workflow/PROTOCOL.md` first. Resolve the feature directory.

## Process

### Step 1: Determine target

If the user provided a target phase (e.g., `/dev-rollback design`), use it. Otherwise ask which phase to roll back to. The target must be earlier than the current phase.

Valid targets: `requirements`, `design`, `implementation`, `verify`, `commit`.

### Step 2: Explain impact and confirm

**Lightweight rollbacks** (no reason needed, just confirm):
- **-> verify**: "Will re-run all verification checks."
- **-> commit**: "Will re-do the commit (e.g., to change commit message or included files)."

**Substantial rollbacks** (ask for reason):
- **-> requirements**: "Requirements will be revised. Design and implementation may need updates afterward. What changed?"
- **-> design**: "Design will be revised. Existing code stays on the branch but may need changes after redesign. What needs to change in the design?"
- **-> implementation**: Read `<slug>/design.md` and show the module list. Ask: "Which modules need rework?" Record the selected modules so `/dev-impl` knows where to start.

Note: Code is never automatically deleted or reverted. When re-entering a phase, you work with the existing code and adjust it based on the revised requirements/design.

### Step 3: Execute

1. Update `<slug>/state.md`:
   - Uncheck all phases from target onward
   - Set current phase to target
   - For substantial rollbacks, append to Context: `ROLLBACK [date]: <old phase> -> <target>. Reason: <user's reason>`
   - For rollback to implementation, also record which modules need rework in Context
   - Update timestamp
2. Suggest the next command based on the target phase.
