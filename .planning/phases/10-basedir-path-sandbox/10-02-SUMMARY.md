---
phase: 10-basedir-path-sandbox
plan: "02"
subsystem: tools/sandbox
tags: [sandbox, base_dir, file-ops, security, bdir-01, bdir-03]
dependency_graph:
  requires: ["10-01"]
  provides: ["withinBaseDir guard in readFileHandler, listDirHandler, writeFileHandler", "symlink caveat in all six tool descriptions"]
  affects: ["internal/tools/file_read.go", "internal/tools/dir.go", "internal/tools/file_write.go", "internal/tools/register.go"]
tech_stack:
  added: []
  patterns: ["base_dir guard after resolveExecutor", "newBaseDirServer test helper (mirrors newExecAllowlistServer)"]
key_files:
  created: []
  modified:
    - internal/tools/file_read.go
    - internal/tools/dir.go
    - internal/tools/file_write.go
    - internal/tools/file_read_test.go
    - internal/tools/dir_test.go
    - internal/tools/file_write_test.go
    - internal/tools/register.go
decisions:
  - "Guard placement: after resolveExecutor, before SSH I/O, and BEFORE allow_overwrite test -e in write handler (D-07)"
  - "Error format matches exec_allowlist precedent: [host X] path \"...\" is outside base_dir \"...\""
  - "newBaseDirServer sets cfg.Hosts directly (not via singleHostRegistry) to preserve BaseDir field"
metrics:
  duration: "~6 minutes"
  completed: "2026-06-10T10:20:30Z"
  tasks_completed: 2
  files_modified: 7
requirements: [BDIR-01, BDIR-03]
---

# Phase 10 Plan 02: Base Dir Handler Guards and Symlink Caveat Summary

**One-liner:** withinBaseDir lexical guard inserted in readFile/listDir/writeFile handlers with D-07-compliant ordering, plus identical symlink-blind-spot caveat on all six file/exec tool descriptions.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 (RED) | Failing base_dir tests | 6442645 | file_read_test.go, dir_test.go, file_write_test.go |
| 1 (GREEN) | Guard readFileHandler, listDirHandler, writeFileHandler | 93597ba | file_read.go, dir.go, file_write.go |
| 2 | Symlink caveat in tool descriptions | dfa0afd | register.go |

## What Was Built

### Task 1: base_dir Guard in Three Handlers

Added `withinBaseDir(baseDir, in.Path)` guard blocks in three handlers, all following the same pattern: read `baseDir := cfg.Hosts[hostName].BaseDir`; if non-empty and path fails the check, return `isError:true` with the D-03 message format before any SSH I/O.

**file_read.go** (`readFileHandler`): guard placed after `resolveExecutor`, before `DetectEncoding`. Reject message: `[host X] path "..." is outside base_dir "..."`.

**dir.go** (`listDirHandler`): same placement, same message format, before `exec.ListDir`.

**file_write.go** (`writeFileHandler`): guard placed after `resolveExecutor` and BEFORE the `if !cfg.Safeguards.AllowOverwrite` block (D-07 compliance — out-of-sandbox writes never issue a `test -e` SSH command).

**Test helper `newBaseDirServer`** added to `file_read_test.go`: builds `cfg.Hosts` directly with `BaseDir` set on the `"default"` host, mirrors `newExecAllowlistServer` pattern. Used across all three test files.

**Test coverage per handler (12 new tests):**
- Outside-path rejection (asserts `isError:true`, `[host default]`, `outside base_dir`, base_dir value)
- Traversal rejection (`/srv/app/../etc/passwd` cleans to outside)
- Inside-path pass-through (`isError:false`)
- Empty base_dir pass-through (unchanged behavior)

**WriteFile-specific assertion**: `TestWriteFileBaseDirOutsidePathRejected` verifies `mock.runCalled == false` (D-07 — `test -e` RunCommand must not execute when guard fires).

### Task 2: Symlink Caveat in Tool Descriptions (BDIR-03)

Appended identical sentence to `Description` of all six file/exec tools in `register.go`:

> When base_dir is configured for the host, paths are confined to that directory by lexical checking only; symlinks on the remote are not resolved and may point outside base_dir.

Tools updated: `ssh_exec`, `ssh_read_file`, `ssh_list_dir`, `ssh_write_file`, `ssh_upload_file`, `ssh_download_file`.

`ssh_connection_status` description is unchanged (not a file/exec operation).

## Verification Results

- `go test ./internal/tools/ -run 'BaseDir|ReadFile|ListDir|WriteFile'`: PASS (54 tests)
- `grep -c 'symlinks on the remote are not resolved' internal/tools/register.go`: 6
- `go build ./...`: OK
- `go vet ./internal/tools/`: OK

## Deviations from Plan

None — plan executed exactly as written.

## Known Stubs

None.

## Threat Flags

None — all threat register entries (T-10-04, T-10-06, T-10-03) addressed as planned. No new trust boundaries introduced.

## Self-Check: PASSED

- internal/tools/file_read.go: withinBaseDir guard present
- internal/tools/dir.go: withinBaseDir guard present
- internal/tools/file_write.go: withinBaseDir guard present (before AllowOverwrite check)
- internal/tools/register.go: 6 symlink caveats present
- Commits 6442645 (RED), 93597ba (GREEN), dfa0afd (descriptions) verified
