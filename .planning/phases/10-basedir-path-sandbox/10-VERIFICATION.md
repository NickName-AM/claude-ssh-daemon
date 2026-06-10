---
phase: 10-basedir-path-sandbox
verified: 2026-06-10T00:00:00Z
status: passed
score: 9/9 must-haves verified
overrides_applied: 0
---

# Phase 10: Base Dir Path Sandbox Verification Report

**Phase Goal:** File operations and exec `cwd` on hosts with `base_dir` set are confined to that directory tree.
**Verified:** 2026-06-10
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| #  | Truth | Status | Evidence |
|----|-------|--------|----------|
| 1  | `ssh_read_file` returns `isError: true` for paths outside `base_dir` | VERIFIED | `file_read.go` lines 47-56: guard on `withinBaseDir(baseDir, in.Path)` returning `IsError:true` with D-03 message |
| 2  | `ssh_write_file` returns `isError: true` for paths outside `base_dir` | VERIFIED | `file_write.go` lines 46-55: guard fires before `AllowOverwrite` check (line 64); D-03 message confirmed |
| 3  | `ssh_list_dir` returns `isError: true` for paths outside `base_dir` | VERIFIED | `dir.go` lines 119-128: guard on `withinBaseDir(baseDir, in.Path)` |
| 4  | `ssh_upload_file` returns `isError: true` for paths outside `base_dir` | VERIFIED | `transfer.go` lines 64-74: guard on `withinBaseDir(baseDir, in.RemotePath)` before AllowOverwrite (line 79) |
| 5  | `ssh_download_file` returns `isError: true` for paths outside `base_dir` | VERIFIED | `transfer.go` lines 131-141: guard on `withinBaseDir(baseDir, in.RemotePath)` before AllowOverwrite (line 145) |
| 6  | `ssh_exec` rejects `cwd` that resolves outside `base_dir` with `isError: true` | VERIFIED | `exec.go` lines 115-124: `withinBaseDir(baseDir, in.Cwd)` guard returning D-03 message |
| 7  | `ssh_exec` rejects empty `cwd` when `base_dir` is set | VERIFIED | `exec.go` lines 104-111: explicit D-01 guard: `[host X] cwd is required when base_dir is set` |
| 8  | When `base_dir` is absent or empty, all operations behave identically to before | VERIFIED | All guards are `if baseDir != ""` — short-circuit with no rejection when empty; confirmed by `TestExecBaseDirUnsetEmptyCwdAllowed`, `TestUploadBaseDirEmptyUnchanged`, and equivalent tests for read/list/write/download |
| 9  | Symlink-blind-spot limitation documented in all six file/exec tool descriptions | VERIFIED | `register.go`: `grep -c 'symlinks on the remote are not resolved'` = 6; exact identical sentence on ssh_exec, ssh_read_file, ssh_list_dir, ssh_write_file, ssh_upload_file, ssh_download_file; ssh_connection_status untouched |

**Score:** 9/9 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/tools/sandbox.go` | `withinBaseDir` lexical containment predicate | VERIFIED | Exists, substantive (47 lines), imports `path` not `path/filepath`, handles root edge case, used by all 5 handler files |
| `internal/tools/sandbox_test.go` | Table-driven unit tests | VERIFIED | 14 individual test functions covering traversal, siblings, root, trailing slash, exact match |
| `internal/tools/file_read.go` | base_dir guard in `readFileHandler` | VERIFIED | Guard present at lines 47-56 |
| `internal/tools/dir.go` | base_dir guard in `listDirHandler` | VERIFIED | Guard present at lines 119-128 |
| `internal/tools/file_write.go` | base_dir guard in `writeFileHandler` before `allow_overwrite` | VERIFIED | Guard at line 47, AllowOverwrite check at line 64 — correct ordering |
| `internal/tools/transfer.go` | base_dir guard on `RemotePath` in both upload and download | VERIFIED | Upload guard lines 64-74 before AllowOverwrite line 79; Download guard lines 131-141 before AllowOverwrite line 145 |
| `internal/tools/exec.go` | empty-cwd rejection and cwd base_dir guard | VERIFIED | D-01 guard at lines 104-111; D-02 guard at lines 115-124; both after allowlist block, before `RunCommand` line 126 |
| `internal/tools/register.go` | Symlink limitation in six tool descriptions | VERIFIED | Identical sentence on all 6 tools; count = 6 |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `file_read.go` | `withinBaseDir` | guard after resolveExecutor | WIRED | `withinBaseDir(baseDir, in.Path)` at line 48 |
| `dir.go` | `withinBaseDir` | guard after resolveExecutor | WIRED | `withinBaseDir(baseDir, in.Path)` at line 120 |
| `file_write.go` | `withinBaseDir` | guard before AllowOverwrite | WIRED | `withinBaseDir(baseDir, in.Path)` at line 47; AllowOverwrite at line 64 |
| `transfer.go` (upload) | `withinBaseDir` | guard on RemotePath | WIRED | `withinBaseDir(baseDir, in.RemotePath)` at line 65 |
| `transfer.go` (download) | `withinBaseDir` | guard on RemotePath | WIRED | `withinBaseDir(baseDir, in.RemotePath)` at line 132 |
| `exec.go` | `withinBaseDir` | cwd guard after allowlist | WIRED | `withinBaseDir(baseDir, in.Cwd)` at line 116 |
| `exec.go` | D-01 empty-cwd rejection | before RunCommand | WIRED | `in.Cwd == ""` guard at line 104; `RunCommand` at line 126 |
| `register.go` | six tool descriptions | symlink caveat text | WIRED | 6 occurrences of identical caveat sentence confirmed |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| `withinBaseDir` — all 14 unit tests pass | `go test ./internal/tools/ -run TestWithinBaseDir -v` | 14 PASS | PASS |
| base_dir guards for read/list/write handlers | `go test ./internal/tools/ -run 'BaseDir\|ReadFile\|ListDir\|WriteFile'` | All PASS | PASS |
| base_dir guards for upload/download handlers | `go test ./internal/tools/ -run 'Upload\|Download\|BaseDir'` | All PASS | PASS |
| exec empty-cwd and cwd containment guards | `go test ./internal/tools/ -run 'Exec\|BaseDir\|Cwd'` | All PASS (incl. `TestExecBaseDirEmptyCwdRejected`, `TestExecBaseDirOutsideCwdRejected`, `TestExecBaseDirInsideCwdAllowed`, `TestExecBaseDirUnsetEmptyCwdAllowed`) | PASS |
| Full test suite unchanged | `go test ./...` | 5 packages all pass | PASS |
| Build clean | `go build ./...` | No output (success) | PASS |
| `go vet` clean | `go vet ./internal/tools/` | No output (success) | PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| BDIR-01 | Plans 02, 03 | Five file tools return isError:true for out-of-base_dir paths | SATISFIED | Guards in all 5 handlers confirmed; tests assert IsError:true for out-of-sandbox paths |
| BDIR-02 | Plan 03 | ssh_exec rejects cwd outside base_dir | SATISFIED | Both empty-cwd (D-01) and containment (D-02) guards in exec.go; tests confirm both cases |
| BDIR-03 | Plans 01, 02 | Lexical check only; symlink limitation documented | SATISFIED | sandbox.go uses `path.Clean` (POSIX, not filepath); doc comment on withinBaseDir; 6 tool descriptions carry identical caveat |
| BDIR-04 | Phase 8 (traceability shows Phase 8) | base_dir per-host; absent=no restriction; absolute path validated at startup | SATISFIED | config.go lines 164-168: non-empty base_dir validated as absolute and cleaned; all handler guards are `if baseDir != ""` (absent=no restriction) |

Note: BDIR-04 is listed in REQUIREMENTS.md traceability as Phase 8. It is substantively implemented (config validation) and exercised by Phase 10 guards. No gap exists.

### Anti-Patterns Found

No anti-patterns detected. No TBD, FIXME, or XXX markers in any phase-modified files. No stub patterns. No hardcoded empty returns. No orphaned code.

### Human Verification Required

None. All success criteria are mechanically verifiable through code inspection and test execution. No visual, real-time, or external-service behavior is involved.

### Gaps Summary

None. All four ROADMAP success criteria are met:

1. All five file tools (`ssh_read_file`, `ssh_write_file`, `ssh_list_dir`, `ssh_upload_file`, `ssh_download_file`) contain `withinBaseDir` guards that return `isError: true` for out-of-sandbox paths, including traversal sequences. Guards fire before any SSH I/O.

2. `ssh_exec` rejects requests with empty `cwd` (D-01 guard) and with `cwd` resolving outside `base_dir` (D-02 guard), both returning `isError: true`.

3. All guards are conditional on `baseDir != ""` — zero behavior change when `base_dir` is absent or empty from the host config.

4. The symlink-blind-spot limitation is documented in the `withinBaseDir` function comment and in all six file/exec tool descriptions in `register.go` (count verified: 6).

---

_Verified: 2026-06-10T00:00:00Z_
_Verifier: Claude (gsd-verifier)_
