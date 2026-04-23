---
description: "Commit changes with DCO sign-off and properly formatted commit message."
---

# /dev-commit

Read `.claude/workflow/PROTOCOL.md` first. Resolve the feature directory. Verify current phase is `commit`.

## Process

### Step 1: Review changes

```bash
git status
git diff --stat
```

Categorize the output:
- **Source changes**: implementation and test files from `/dev-impl`
- **Format/lint fixes**: changes made by `/dev-verify` (e.g., import reordering, gofmt)
- **Workflow files**: anything under `.claude/workflow/` — NEVER commit these
- **Unexpected files**: files not mentioned in design.md — flag these to the user

If there are many files (>15), show a summary first:
```
N files changed, X insertions, Y deletions
  pkg/placement/... (5 files)
  test/integration/placement/... (2 files)
```
Ask the user if they want the full list.

### Step 2: Generate commit message

Read `<slug>/requirements.md` for the WHY and `<slug>/design.md` Overview for the WHAT. Also look at `git diff --stat` for the scope. Draft a commit message:

```
<emoji> <short description>

<one-paragraph explanation: what was changed and why>
```

Emoji prefixes (per project convention):
- `:sparkles:` — feature
- `:bug:` — bugfix
- `:seedling:` — misc

Do NOT include `Signed-off-by` in the message — `git commit -s` adds it automatically.

Show draft to user for editing.

### Step 3: Stage and commit

Stage all source and format/lint fix files. Exclude workflow files.

```bash
git add <specific files>    # NOT git add -A, NOT .claude/workflow/ files
git commit -s -m "<message>"
```

### Step 4: Confirm

```bash
git log -1 --stat
```

Show the commit hash and summary.

## Completion

1. Update `<slug>/state.md`: check `[x] commit`, set phase to `pr`, update Context with commit hash, update timestamp.
2. Say: "Run `/dev-pr` to create a pull request."
