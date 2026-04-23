---
description: "Requirements analysis phase. Collaboratively define and refine feature/bugfix requirements."
---

# /dev-require

Read `.claude/workflow/PROTOCOL.md` first. Resolve the feature directory. Verify current phase is `requirements`.

## Process

Read `<slug>/state.md` to get the feature description from the Context section (provided during `/dev-start`). Read `<slug>/requirements.md` to check current state.

Determine which path to take based on the content of `requirements.md`:

### Path A: Template is unfilled (still contains `{{FEATURE_NAME}}` or only HTML comments)

Use the feature description from state.md Context as the starting point — do NOT ask the user to describe the feature again.

**Step 1: Codebase exploration**

Use an **Explore agent** to search the codebase. Tell the agent specifically:
- Search for controllers, types, and reconcilers related to the feature keywords
- Find existing test cases that cover similar functionality
- Check if the area has integration tests in `test/integration/`
- Look for related constants, conditions, or error handling patterns

**Step 2: Draft the full requirements document**

Based on the feature description and exploration results, draft ALL sections of `requirements.md` at once.

Adapt the document structure based on the type:

**For features:**
- Summary, Scenarios (as user stories), Acceptance Criteria, Constraints, Dependencies, Out of Scope

**For bugfixes:**
- Summary (include the bug symptom), Reproduction Steps (replace Scenarios), Root Cause Analysis (if identifiable from code), Fix Criteria (replace Acceptance Criteria), Constraints, Dependencies, Out of Scope

**Step 3: Present for review**

Show the complete draft to the user in one go. Ask: "Anything you want to change or add?"

Iterate on specific sections the user flags until they're satisfied.

### Path B: User has manually edited the file (real content, not just template placeholders)

The user may have edited `requirements.md` directly in their editor. Respect their work:

1. Show what they've written. Do NOT overwrite it.
2. Run the same **Explore agent** codebase search to supplement their input.
3. Review their requirements against the exploration findings:
   - Are there gaps? (missing constraints, unmentioned dependencies, edge cases)
   - Are there conflicts with existing code patterns?
   - Is the scope clear?
4. Suggest additions or refinements, clearly marking what's new vs what the user already wrote.
5. Only update the file with the user's approval.

### Path C: Resuming (previously drafted by Claude, partially complete)

Show the current state of requirements.md. Ask what needs to change. Update only those sections.

## Completion Gate

Show a brief summary:
> "Requirements ready:
> - [summary in one line]
> - [N] acceptance/fix criteria
> - Dependencies: [list or none]
>
> Move to design phase?"

When confirmed:
1. Write final `<slug>/requirements.md`.
2. Update `<slug>/state.md`: check `[x] requirements`, set phase to `design`, update Context and timestamp.
3. Say: "Run `/dev-design` to start solution design."
