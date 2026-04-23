---
description: "Generate pull request content. Currently local-only (no push to remote)."
---

# /dev-pr

Read `.claude/workflow/PROTOCOL.md` first. Resolve the feature directory. Verify current phase is `pr`.

## Process

### Step 1: Verify state

```bash
git log main..HEAD --oneline
git status
```

Check:
- There are commits ahead of main. If not, tell the user there's nothing to submit.
- Working tree is clean. If not, warn about uncommitted changes.

### Step 2: Generate PR content

Read `<slug>/requirements.md`, `<slug>/design.md`, and the commit history:
```bash
git log main..HEAD --format="%h %s"
```

**Title** (under 70 chars):
- If there's a single commit: reuse the commit message first line.
- If there are multiple commits: draft a title that summarizes the overall change.

**Body**:
```markdown
## Summary
<2-3 bullets from requirements summary>

## Changes
<key changes grouped by component, derived from design.md>

## Commits
<list of commits if more than one>

## Test Plan
<tests added/modified, which integration suites were verified>
```

### Step 3: Show PR preview

Display the full PR content (title + body) to the user for review. Let the user edit if needed.

> **Note**: Remote push and PR creation are disabled during workflow testing. To create a real PR later, push the branch and use `gh pr create --base main`.

## Completion

1. Update `<slug>/state.md`: check `[x] pr`, set phase to `done`, update Context with PR title, update timestamp.
2. Tell the user:
   > "PR content generated. Workflow complete.
   >
   > When ready to submit for real:
   > ```bash
   > git push -u origin <branch>
   > gh pr create --base main --title '<title>' --body '<body>'
   > ```
   >
   > Keep `.claude/workflow/<slug>/` until the PR is merged — if reviewers request changes, use `/dev-rollback implementation` to make fixes.
   > After merge: `rm -rf .claude/workflow/<slug>/`"
