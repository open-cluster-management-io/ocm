---
description: "Show current workflow status. Quick view without changing anything."
---

# /dev-status

Read `.claude/workflow/PROTOCOL.md` first for shared conventions.

Resolve the feature directory from the current branch (per PROTOCOL.md).

## Current branch workflow

**If no workflow exists for this branch**: say "No active workflow on branch `<branch>`. Run `/dev-start` to begin."

**If workflow exists**: Display status using the "Display Status" format from PROTOCOL.md.

## All active workflows

List all workflow directories, excluding `templates/`:

```bash
ls -d .claude/workflow/*/ | grep -v templates
```

For each directory found, read its `state.md` and show a one-line summary:

```
All workflows:
  * feature-placement-taint (branch: feature/placement-taint) — phase: implementation  <-- current branch
    bugfix-csr-renewal      (branch: bugfix/csr-renewal)      — phase: design
```

Mark the current branch's workflow with `*`.

If there's only one workflow (the current one), skip this section.
