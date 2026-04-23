---
description: "Start a new feature/bugfix workflow or resume an existing one. Entry point for all development."
---

# /dev-start

Read `.claude/workflow/PROTOCOL.md` first for shared conventions.

## Step 1: Detect current branch

Run `git branch --show-current` to get the current branch name.

## Step 2: Route based on branch and workflow state

### Case A: Current branch already HAS a workflow directory

Display status (use the "Display Status" format from PROTOCOL.md). Summarize the Context section. Suggest the next command.

### Case B: Current branch does NOT have a workflow directory

The user is on a branch without a workflow. This could be `main`, a `feature/` branch, a `bugfix/` branch, or any other branch name.

Ask the user:
1. What is this feature or bugfix? (brief description)
2. Is this a `feature` or `bugfix`? (auto-detect from branch prefix if possible)
3. **Do you want to use the current branch (`<branch>`) or create a new one?**
   - If the branch prefix already matches the type (e.g., on `feature/foo` and type is feature), default to using the current branch.
   - If on `main`, default to creating a new branch.
   - Otherwise, let the user choose.

**If using the current branch:**
1. Derive slug from the current branch name per PROTOCOL.md.
2. Create `.claude/workflow/<slug>/` directory.
3. Copy templates into the directory and fill in placeholders:
   - `templates/state.md` -> `<slug>/state.md` (set Context to the user's feature description)
   - `templates/requirements.md` -> `<slug>/requirements.md`
   - `templates/design.md` -> `<slug>/design.md`
4. Tell the user: "Workflow initialized on branch `<branch>`. Run `/dev-require` to start requirements analysis."

**If creating a new branch:**
1. Propose a branch name: `<type>/<short-slug>` (e.g. `feature/placement-taint-support`). Confirm with the user.
2. Create the branch: `git checkout -b <branch>`.
3. Derive slug from the NEW branch name and create `.claude/workflow/<slug>/` directory.
4. Copy templates and fill in placeholders (same as above).
5. Tell the user: "Workflow initialized. Run `/dev-require` to start requirements analysis."

## Rules

- One workflow per branch. The branch name IS the workflow identity.
- The slug is ALWAYS derived from the final branch name (after creation if needed).
- Do NOT include rollback logic here — tell the user to use `/dev-rollback` instead.
- Never force a branch switch without the user's explicit approval.
