---
description: "Start a new feature/bugfix workflow or resume an existing one. Entry point for all development."
---

# /dev-start

Read `.claude/workflow/PROTOCOL.md` first for shared conventions.

## Step 1: Detect current branch

Run `git branch --show-current` to get the current branch name.

## Step 2: Route based on branch and workflow state

### Case A: Already on a feature/bugfix branch WITH a workflow directory

Display status (use the "Display Status" format from PROTOCOL.md). Summarize the Context section. Suggest the next command.

### Case B: Already on a feature/bugfix branch WITHOUT a workflow directory

The user has already created a branch manually. Use this branch directly — do NOT create a new one.

1. Ask the user: What is this feature or bugfix? (brief description)
2. Determine type from branch name prefix (`feature/` -> feature, `bugfix/` -> bugfix). If the prefix doesn't match either, ask the user.
3. Create `.claude/workflow/<slug>/` directory (derive slug from the current branch name per PROTOCOL.md).
4. Copy templates into the directory and fill in placeholders:
   - `templates/state.md` -> `<slug>/state.md` (set Context to the user's feature description)
   - `templates/requirements.md` -> `<slug>/requirements.md`
   - `templates/design.md` -> `<slug>/design.md`
5. Tell the user: "Workflow initialized on existing branch `<branch>`. Run `/dev-require` to start requirements analysis."

### Case C: On `main` (or any branch that is not a feature/bugfix branch)

Need to create a new branch.

1. Ask the user:
   - What is the feature or bugfix? (brief description)
   - Is this a `feature` or `bugfix`?
2. Propose a branch name: `<type>/<short-slug>` (e.g. `feature/placement-taint-support`). Confirm with the user.
3. Create the branch: `git checkout -b <branch>`.
4. Now derive the slug from the NEW branch name and create `.claude/workflow/<slug>/` directory.
5. Copy templates into the directory and fill in placeholders:
   - `templates/state.md` -> `<slug>/state.md` (set Context to the user's feature description)
   - `templates/requirements.md` -> `<slug>/requirements.md`
   - `templates/design.md` -> `<slug>/design.md`
6. Tell the user: "Workflow initialized. Run `/dev-require` to start requirements analysis."

## Rules

- One workflow per branch. The branch name IS the workflow identity.
- The slug is ALWAYS derived from the final branch name (after creation if needed), never from `main`.
- Do NOT include rollback logic here — tell the user to use `/dev-rollback` instead.
