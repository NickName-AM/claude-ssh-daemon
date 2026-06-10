---
phase: 10-basedir-path-sandbox
plan: "03"
subsystem: tools/sandbox
tags: [sandbox, base_dir, transfer, exec, security]
dependency_graph:
  requires: ["10-01"]
  provides: [upload-basedir-guard, download-basedir-guard, exec-empty-cwd-rejection, exec-cwd-basedir-guard]
  affects: [internal/tools/transfer.go, internal/tools/exec.go]
tech_stack:
  added: []
  patterns: [withinBaseDir-guard, D-01-empty-cwd-rejection, D-02-cwd-containment, D-03-message-format, D-07-fail-fast-ordering]
key_files:
  created: []
  modified:
    - internal/tools/transfer.go
    - internal/tools/transfer_test.go
    - internal/tools/exec.go
    - internal/tools/exec_test.go
decisions:
  - "Guard in uploadHandler fires on in.RemotePath before SAFE-01 (allow_overwrite) per D-07"
  - "Guard in downloadHandler fires on in.RemotePath before SAFE-01 per D-07"
  - "D-01 empty-cwd guard is a separate block from D-02 cwd-containment, matching allowlist's separate-case style"
  - "AllowDelete:true set in newBaseDirExecServer helper to keep tests focused on base_dir guard"
metrics:
  duration: "~20 minutes"
  completed: "2026-06-10T10:18:00Z"
  tasks_completed: 2
  files_modified: 4
---

# Phase 10 Plan 03: Handler base_dir Guards Summary

**One-liner:** withinBaseDir guard wired into uploadHandler, downloadHandler, and execHandler with D-01 empty-cwd rejection and D-02 cwd containment, closing T-10-07/08/09.

## Tasks Completed

| Task | Name | Commits | Files |
|------|------|---------|-------|
| 1 | Guard uploadHandler and downloadHandler RemotePath with base_dir check | 0a50481 (test), e6d07b8 (feat) | transfer.go, transfer_test.go |
| 2 | Add empty-cwd rejection and cwd base_dir guard to execHandler | 686d6c5 (test), cb4caff (feat) | exec.go, exec_test.go |

## What Was Built

### Task 1 — Transfer handler base_dir guards

In `uploadHandler` and `downloadHandler`, inserted a `withinBaseDir` check on `in.RemotePath` after the `filepath.IsAbs(in.LocalPath)` guard and **before** the `cfg.Safeguards.AllowOverwrite` check (D-07: fail fast before any remote/local I/O).

Guard message format (D-03): `[host %s] path %q is outside base_dir %q`

Test helper `newBaseDirTransferServer` builds `cfg.Hosts` directly (not via `singleHostRegistry`) to preserve the `BaseDir` field — mirroring the `newExecAllowlistServer` pattern from Plan 01.

Test cases:
- Upload outside base_dir rejected; `RunCommand` (overwrite check) also NOT called — confirms D-07 ordering
- Upload traversal path (`/srv/app/../secret`) rejected
- Upload inside base_dir proceeds
- Upload with empty base_dir: unchanged behavior
- Download traversal rejected; Download inside passes; Download empty base_dir passes

### Task 2 — execHandler base_dir guards

Inserted two sequential guard blocks after the exec_allowlist block and before `exec.RunCommand`:

**D-01 (T-10-08):** When `base_dir != ""` and `in.Cwd == ""`, return IsError with exact message: `[host %s] cwd is required when base_dir is set`

**D-02 (T-10-09):** When `base_dir != ""` and `in.Cwd != ""` and `!withinBaseDir(baseDir, in.Cwd)`, return IsError with D-03 message: `[host %s] path %q is outside base_dir %q`

Test helper `newBaseDirExecServer` mirrors `newExecAllowlistServer`. Test cases cover all four behavior quadrants: empty-cwd+base_dir, outside-cwd+base_dir, inside-cwd+base_dir (pass-through), and base_dir-unset (unchanged).

## Verification

All tests pass:
- `go test ./internal/tools/ -run 'Exec|Upload|Download|BaseDir|Cwd'` — 63 tests pass
- `go build ./...` — clean
- `go vet ./internal/tools/` — clean

## Deviations from Plan

None — plan executed exactly as written.

## Known Stubs

None.

## Threat Flags

No new threat surface introduced. All three threats in the threat register are fully mitigated:
- T-10-07 (upload/download RemotePath): withinBaseDir guard before allow_overwrite
- T-10-08 (execHandler empty cwd): D-01 explicit rejection
- T-10-09 (execHandler non-empty cwd): withinBaseDir guard on in.Cwd

## TDD Gate Compliance

Both tasks followed RED/GREEN/REFACTOR:
- Task 1: test commit `0a50481` (RED) → feat commit `e6d07b8` (GREEN)
- Task 2: test commit `686d6c5` (RED) → feat commit `cb4caff` (GREEN)

## Self-Check: PASSED

Files verified:
- internal/tools/transfer.go — contains `withinBaseDir(baseDir, in.RemotePath)` in both handlers
- internal/tools/exec.go — contains D-01 (`cwd is required when base_dir is set`) and D-02 (`withinBaseDir(baseDir, in.Cwd)`)
- internal/tools/transfer_test.go — newBaseDirTransferServer + 7 base_dir test cases
- internal/tools/exec_test.go — newBaseDirExecServer + 5 base_dir test cases

Commits verified:
- 0a50481, e6d07b8, 686d6c5, cb4caff all present in git log
