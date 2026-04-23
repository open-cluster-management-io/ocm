---
description: "Solution design phase. Create detailed implementation plan with packages, interfaces, and dependencies."
---

# /dev-design

Read `.claude/workflow/PROTOCOL.md` first. Resolve the feature directory. Verify current phase is `design`.

## Process

Read `<slug>/requirements.md` and `<slug>/design.md`.

Determine which path to take based on the content of `design.md`:

### Path A: Template is unfilled (still contains `{{FEATURE_NAME}}` or only HTML comments)

**Step 1: Codebase research**

Use an **Explore agent** (very thorough). The focus at this stage is on implementation details, not feature discovery (that was done in `/dev-require`). Tell the agent specifically:
- Read the source code of files that will be modified — understand their structure, patterns, and conventions
- Find how similar features are implemented: controller setup, registration in manager, RBAC rules, feature gates
- Identify the exact interfaces, types, and functions to extend or implement
- Locate the integration test suite and test helpers for the target component
- Check if `open-cluster-management.io/api` changes are needed (new types, new fields, new conditions)

Present findings to the user as context for design discussion.

**Step 2: Draft the full design document**

Based on requirements and code research, draft ALL sections of `<slug>/design.md` at once:
1. **Overview** — high-level approach
2. **Package & File Changes** — every file to create or modify, with purpose
3. **Interface Definitions** — actual Go type/interface definitions for key new types
4. **Dependencies** — internal packages, external deps, API repo changes
5. **Implementation Modules** — break work into ordered, independently implementable units. Each module should:
   - Touch a small number of files (ideally within one package)
   - Be independently testable
   - Include source file, test file, integration test (if applicable), and key test scenarios
6. **Test Strategy** — which suites (unit/integration), what helpers needed
7. **Risks & Mitigations**

Show the complete draft to the user in one go. Ask: "Anything you want to change or add?" Iterate on sections the user flags.

### Path B: User has manually edited the file (real content, not just template placeholders)

1. Show what the user has written. Do NOT overwrite it.
2. Run the same **Explore agent** research to validate and supplement the design.
3. Review their design:
   - Are there missing files or packages that need changes?
   - Are the interfaces compatible with existing code patterns?
   - Are implementation modules properly ordered and scoped?
   - Is the test strategy complete?
4. Suggest additions or refinements. Only update the file with the user's approval.

### Path C: Resuming (previously drafted, partially complete)

Show the current state of design.md. Ask what needs to change. Update only those sections.

## Validation

Before finalizing, verify:
- Every acceptance criterion from requirements.md maps to at least one implementation module
- No circular dependencies between modules
- Each module is small enough to implement in one cycle (if a module touches more than 3 files or spans multiple packages, suggest splitting it)
- Test strategy covers all acceptance criteria
- API changes (if any) are called out as a prerequisite that must be merged first

## Completion Gate

Show summary and ask: "Ready to start implementation?"

When confirmed:
1. Write final `<slug>/design.md`.
2. Update `<slug>/state.md`: check `[x] design`, set phase to `implementation`, update Context with module list and starting point, update timestamp.
3. Say: "Run `/dev-impl` to start implementation with module 1."
