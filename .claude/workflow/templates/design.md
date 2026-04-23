# Design: {{FEATURE_NAME}}

## Overview

<!-- High-level approach, 2-3 sentences -->

## Package & File Changes

<!--
List each package/file to be created or modified:

### pkg/component/subpackage/

- `new_file.go` — description of what it contains
- `existing_file.go` — what changes and why

-->

## Interface Definitions

<!--
```go
// Key new or modified interfaces/types
type FooReconciler interface {
    Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
}
```
-->

## Dependencies

<!--
- Internal: which existing packages does this depend on?
- External: any new dependencies needed?
- API repo: any API changes required? (must be merged first)
-->

## Implementation Modules

<!--
Ordered list of implementation units. Each module is implemented in one dev cycle
(write code -> write unit tests -> write integration tests if needed -> verify):

1. **Module name** — brief description
   - Source file: `path/to/foo.go`
   - Test file: `path/to/foo_test.go`
   - Integration test: `test/integration/<component>/xxx_test.go` (if applicable)
   - Key test scenarios: ...

2. **Module name** — brief description
   ...
-->

## Test Strategy

<!--
- Unit tests: which packages?
- Integration tests: which test suite (registration/placement/work/addon/operator)?
- New integration test cases needed?
- Any test helpers or fixtures required?
-->

## Risks & Mitigations

<!-- Known risks and how to handle them -->
