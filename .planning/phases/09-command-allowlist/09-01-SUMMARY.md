---
phase: 09-command-allowlist
plan: "01"
subsystem: mcp
tags: [allowlist, exec, security, access-control]
dependency_graph:
  requires: [phase-08-config-schema]
  provides: [exec-allowlist-enforcement]
  affects: [internal/tools/exec.go, internal/tools/exec_test.go]
tech_stack:
  added: []
  patterns: [strings.HasPrefix prefix-match guard, three-state *[]string pointer semantics]
key_files:
  created: []
  modified:
    - internal/tools/exec.go
    - internal/tools/exec_test.go
decisions:
  - Use strings.HasPrefix (not Contains) to prevent substring bypass of prefix guard (T-09-02)
  - Look up allowlist by resolved hostName from resolveExecutor, never in.Host, to prevent routing bypass (T-09-01)
  - nil pointer = allow-all, non-nil empty = deny-all, non-nil populated = prefix enforcement (three-state semantics)
  - Prefix list embedded in rejection error so Claude can self-correct without guessing (ALWL-03 hard requirement)
metrics:
  duration: "pre-implemented in prior phase execution"
  completed: "2026-06-10"
  tasks_completed: 2
  tasks_total: 2
---

# Phase 09 Plan 01: Per-host exec_allowlist enforcement Summary

Per-host `exec_allowlist` enforcement added to `execHandler` in `internal/tools/exec.go`. The guard runs between `resolveExecutor` and `RunCommand`, reading `cfg.Hosts[hostName].ExecAllowlist` with three-state semantics: nil pointer passes through (allow-all, ALWL-01 baseline), non-nil empty slice rejects every command (ALWL-02), non-nil populated slice rejects commands not matching any prefix via `strings.HasPrefix`, embedding the prefix list in the error so Claude can self-correct (ALWL-03).

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Insert per-host exec_allowlist guard in execHandler | 96e0e6a | internal/tools/exec.go |
| 2 | Add allowlist enforcement tests for all three states and per-host independence | f8c3a8f | internal/tools/exec_test.go |

## Behaviors Delivered

- **ALWL-01 baseline preserved**: `ExecAllowlist == nil` on a host → any command passes through to `RunCommand` unchanged. Verified by `TestExecAllowlistNilPassesThrough`.
- **ALWL-02 deny-all**: `ExecAllowlist` non-nil empty slice (`&[]string{}`) → every command rejected with `IsError=true`, `RunCommand` never reached, error text contains `"exec_allowlist is empty"`. Verified by `TestExecAllowlistEmptySliceDeniesAll`.
- **ALWL-03 prefix allow**: `ExecAllowlist &[]string{"git ", "make "}` with command `"git status"` → `strings.HasPrefix` match → `IsError=false`, `RunCommand` reached. Verified by `TestExecAllowlistPrefixAllowsMatchingCommand`.
- **ALWL-03 prefix reject**: `ExecAllowlist &[]string{"git "}` with command `"cat file"` → no prefix match → `IsError=true`, `RunCommand` never reached, error text contains both the rejected command and the configured prefixes. Verified by `TestExecAllowlistPrefixRejectsNonMatchingCommand`.
- **HasPrefix-not-Contains guard**: `"some git status"` with allowlist `["git "]` → rejected (`strings.HasPrefix` not `strings.Contains`). Inline negative sub-case in `TestExecAllowlistPrefixRejectsNonMatchingCommand`.
- **Per-host independence**: Two-host config where `"web"` has `&[]string{"git "}` and `"db"` has nil → `"cat secrets"` on `"web"` rejected (`webMock.runCalled=false`), same command on `"db"` allowed (`dbMock.runCalled=true`). Verified by `TestExecAllowlistPerHostIndependence`.

## Threat Model Compliance

| Threat ID | Status | Implementation |
|-----------|--------|----------------|
| T-09-01 (Elevation of Privilege - routing bypass) | Mitigated | Lookup keyed by `hostName` from `resolveExecutor`, never `in.Host`; guard runs after resolution |
| T-09-02 (Tampering - prefix matching function) | Mitigated | `strings.HasPrefix` used exclusively; negative test locks HasPrefix-not-Contains |
| T-09-03 (Tampering - shell wrapper bypass) | Accepted | Documented limitation per REQUIREMENTS.md; no code mitigation |
| T-09-04 (DoS - empty-string prefix matches everything) | Mitigated | Phase 8 `config.Validate()` rejects empty-string entries; no re-validation in handler |

## Test Coverage

Five behaviors covered, each with runCalled false on rejection and true on allow:
- `TestExecAllowlistNilPassesThrough` - ALWL-01 nil allow-all
- `TestExecAllowlistEmptySliceDeniesAll` - ALWL-02 deny-all
- `TestExecAllowlistPrefixAllowsMatchingCommand` - ALWL-03 prefix allow
- `TestExecAllowlistPrefixRejectsNonMatchingCommand` - ALWL-03 prefix reject + HasPrefix negative guard
- `TestExecAllowlistPerHostIndependence` - per-host independence

`go test ./...` passes with no regressions. `go build ./...` and `go vet ./internal/tools/` exit 0.

## Deviations from Plan

None - plan executed exactly as written. Both tasks were already implemented and committed in prior execution (`f8c3a8f` for TDD RED/tests, `96e0e6a` for GREEN/implementation). The executor verified all acceptance criteria and ran the full test suite confirming correctness.

## TDD Gate Compliance

The plan has `tdd="true"` tasks. The git log confirms the correct RED/GREEN sequence:
1. RED: `f8c3a8f test(mcp): add allowlist enforcement tests for all three states and per-host independence`
2. GREEN: `96e0e6a feat(mcp): add per-host exec_allowlist enforcement in execHandler`

Both gate commits are present in the correct order.

## Self-Check: PASSED

- `internal/tools/exec.go` found, contains `strings.HasPrefix`, `cfg.Hosts[hostName].ExecAllowlist`, `strings.Join(*allowlist`, `exec_allowlist is empty`
- `internal/tools/exec_test.go` found, contains `newExecAllowlistServer`, `newMultiHostAllowlistServer`, `exec_allowlist is empty` assertion, 20 `runCalled` references
- Commit `96e0e6a` verified in git log
- Commit `f8c3a8f` verified in git log
- `go build ./...` exits 0
- `go vet ./internal/tools/` exits 0
- `go test ./internal/tools/ -run 'Allowlist|Exec' -v` all PASS
- `go test ./...` all packages PASS
