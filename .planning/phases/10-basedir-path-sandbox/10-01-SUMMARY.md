---
phase: 10
plan: 01
subsystem: tools/sandbox
tags: [sandbox, security, path-validation, lexical-containment]
dependency_graph:
  requires: [internal/config (BaseDir field from Phase 8)]
  provides: [withinBaseDir helper for Phase 10 plans 02 and 03]
  affects: [internal/tools/sandbox.go, internal/tools/sandbox_test.go]
tech_stack:
  added: []
  patterns: [path.Clean + trailing-slash HasPrefix for POSIX lexical containment]
key_files:
  created:
    - internal/tools/sandbox.go
    - internal/tools/sandbox_test.go
  modified: []
decisions:
  - Root baseDir edge case handled via explicit cleanBase=="//" guard before the HasPrefix check
  - path.Clean applied to both baseDir and requestedPath before comparison (D-05/D-06)
  - Trailing-slash boundary on cleanBase prevents /base matching /base_extra sibling paths
metrics:
  duration: ~2m
  completed: "2026-06-10"
  tasks_completed: 1
  tasks_total: 1
  files_created: 2
  files_modified: 0
---

# Phase 10 Plan 01: withinBaseDir Lexical-Containment Helper Summary

**One-liner:** POSIX lexical path containment helper using path.Clean and trailing-slash boundary check, with 15 table-driven test cases covering traversal, siblings, root, and trailing-slash edge cases.

## What Was Built

Created `internal/tools/sandbox.go` exporting `withinBaseDir(baseDir, requestedPath string) bool` — the shared helper that all 6 file/exec handlers will call in Phase 10 plans 02 and 03 to enforce per-host `base_dir` confinement (BDIR-01, BDIR-02, BDIR-03).

The algorithm (per context D-05/D-06):
1. `path.Clean` both arguments (POSIX, not `filepath.Clean`)
2. Special-case `cleanBase == "/"` — any absolute path is contained by root
3. Exact-match check (`cleanPath == cleanBase`)
4. Trailing-slash prefix check (`strings.HasPrefix(cleanPath, cleanBase+"/")`) to prevent `/base` from falsely matching `/base_extra`

The companion `internal/tools/sandbox_test.go` provides 15 tests covering:
- Exact match at base dir
- Nested subpath and subdirectory
- Sibling directory with same prefix (D-05 boundary)
- Sibling file with same name prefix
- Parent directory escape
- Root `/` escape
- Absolute path outside base_dir
- Traversal sequence resolving outside (`/../../../etc/passwd`)
- Traversal sequence resolving inside (`src/../lib`)
- Trailing slash on baseDir
- Trailing slash on requestedPath
- Root baseDir containing any absolute path
- DotDot at root (`/../etc` → `/etc` within `/`)
- Deeply nested path

## Verification

All 15 `TestWithinBaseDir*` tests pass. Full `go test ./...` suite remains green (all 5 test packages pass).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Root baseDir edge case in initial implementation**
- **Found during:** Task 1 — running tests
- **Issue:** When `cleanBase == "/"`, appending `"/"` forms `"//"` which no `path.Clean`-ed path starts with, causing `withinBaseDir("/", "/etc/passwd")` to return false instead of true
- **Fix:** Added explicit guard: `if cleanBase == "/" { return strings.HasPrefix(cleanPath, "/") }`
- **Files modified:** internal/tools/sandbox.go
- **Commit:** b0c1c53 (same task commit — fixed before staging)

## Threat Flags

None — this plan creates no network endpoints, auth paths, or schema changes. The helper is a pure in-process lexical predicate with no I/O.

## Self-Check: PASSED

- [x] internal/tools/sandbox.go created and exists
- [x] internal/tools/sandbox_test.go created and exists
- [x] Commit b0c1c53 exists in git log
- [x] All 15 withinBaseDir tests pass
- [x] Full test suite passes (go test ./...)
