---
phase: 10-basedir-path-sandbox
reviewed: 2026-06-10T10:26:13Z
depth: standard
files_reviewed: 13
files_reviewed_list:
  - internal/tools/dir.go
  - internal/tools/dir_test.go
  - internal/tools/exec.go
  - internal/tools/exec_test.go
  - internal/tools/file_read.go
  - internal/tools/file_read_test.go
  - internal/tools/file_write.go
  - internal/tools/file_write_test.go
  - internal/tools/register.go
  - internal/tools/sandbox.go
  - internal/tools/sandbox_test.go
  - internal/tools/transfer.go
  - internal/tools/transfer_test.go
findings:
  critical: 0
  warning: 2
  info: 4
status: issues_found
---

# Phase 10: Code Review Report

**Reviewed:** 2026-06-10T10:26:13Z
**Depth:** standard
**Files Reviewed:** 13
**Status:** issues_found

## Summary

Phase 10 adds a `withinBaseDir` lexical-containment helper (sandbox.go) and base_dir
guards in the read, write, list, upload, download, and exec handlers. The core helper
is correct and well-tested: `path.Clean` is applied before comparison, the
trailing-slash boundary prevents `/base` matching `/base_extra`, the `baseDir == "/"`
degenerate case is handled, and relative or empty requested paths fail closed (a
cleaned relative path can never prefix-match an absolute base). Traversal sequences
(`../`) that resolve outside base_dir are rejected after cleaning; sequences that
resolve inside are accepted. The guard fires before any SSH I/O in every handler,
including before the SAFE-01 `test -e` overwrite probe (verified by tests asserting
`runCalled == false`). The symlink non-resolution limitation (BDIR-03) is documented
in the helper, in each tool description, and acknowledged in the plan — it is treated
here as accepted design, not a finding.

I attempted the standard escape vectors against `withinBaseDir`: relative paths,
`..` traversal, double slashes, trailing slashes, sibling-prefix names, root base,
empty path, embedded newlines, and case/unicode variants. All fail closed. The two
warnings below are about the layer above the helper, not the helper itself.

## Warnings

### WR-01: ssh_exec tool description overstates base_dir confinement — only `cwd` is checked, the command itself is unrestricted

**File:** `internal/tools/register.go:32` (description), `internal/tools/exec.go:104-124` (guard)
**Issue:** The registered `ssh_exec` description states: "When base_dir is configured
for the host, paths are confined to that directory by lexical checking only; symlinks
on the remote are not resolved and may point outside base_dir." This is the same
boilerplate used for the file tools, but for `ssh_exec` the guard validates only
`in.Cwd`. The command string is never inspected, so with `cwd: /srv/app` a command
like `cat /etc/passwd`, `cd / && ls`, or `tar -C / ...` reads or writes anywhere the
SSH user can reach. The cwd-only design matches the plan (BDIR-02/D-01/D-02), but the
description tells the model — and implies to the operator who set `base_dir` — a
confinement guarantee that does not exist for exec. The only honest framing is that
`base_dir` confines exec's *working directory*, and real command confinement requires
`exec_allowlist`. Disclosing only the symlink caveat while omitting the much larger
"command arguments are unchecked" caveat is a misleading security claim.
**Fix:** Use an exec-specific description, e.g.:
```go
Description: "Execute a remote shell command via the SSH ControlMaster session. " +
	"When base_dir is configured for the host, only the working directory (cwd) is " +
	"confined to base_dir by lexical checking; the command itself may reference any " +
	"path on the remote host. Combine with exec_allowlist to restrict commands.",
```
Mirror the same caveat in user-facing config documentation for `base_dir`.

### WR-02: Handlers fail open if the executor registry and cfg.Hosts ever diverge — zero-value HostConfig silently disables base_dir and exec_allowlist

**File:** `internal/tools/exec.go:72,104,115`; `internal/tools/dir.go:119`; `internal/tools/file_read.go:47`; `internal/tools/file_write.go:46`; `internal/tools/transfer.go:64,131`
**Issue:** Every handler resolves the host via the *registry* map
(`resolveExecutor`, `internal/tools/resolve.go:34`) and then separately indexes the
*config* map: `cfg.Hosts[hostName].BaseDir`. Go map indexing on a missing key returns
the zero `HostConfig`, so if `hostName` exists in the registry but not in `cfg.Hosts`,
`BaseDir` is `""` and `ExecAllowlist` is `nil` — both security gates silently
deactivate (fail open) instead of erroring. The invariant holds today because
`daemon.Run` builds the registry from `cfg.Hosts` (`internal/daemon/daemon.go:118-122`),
but nothing enforces it: any future caller of `RegisterTools` (tests already construct
registry and cfg.Hosts independently, e.g. `exec_test.go:411-428`) can break it
without a compile error or runtime failure, and the failure mode is "sandbox off",
not "request denied." Security checks keyed on a cross-map invariant should verify
the lookup.
**Fix:** Make `resolveExecutor` return the `HostConfig` alongside the executor, using
the comma-ok form and erroring on a miss:
```go
hostCfg, ok := cfg.Hosts[name]
if !ok {
	return nil, "", config.HostConfig{}, errResult("host %q has no config entry", name)
}
return exec, name, hostCfg, nil
```
Handlers then read `hostCfg.BaseDir` / `hostCfg.ExecAllowlist`, eliminating six
independent unchecked lookups.

## Info

### IN-01: parseLsLine silently ignores size parse errors

**File:** `internal/tools/dir.go:65`
**Issue:** `size, _ := strconv.ParseInt(fields[4], 10, 64)` discards the error, so a
malformed or non-numeric size field (e.g. `ls` variants that print a device
major/minor pair, or locale-shifted columns) yields a silent `Size: 0` rather than a
detectable parse failure.
**Fix:** Either check the error and skip/flag the entry, or document that `Size` is
best-effort and 0 on parse failure.

### IN-02: No tests assert the guard's behavior for relative remote paths

**Files:** `internal/tools/sandbox_test.go`, `internal/tools/file_read_test.go`, `internal/tools/exec_test.go`
**Issue:** `withinBaseDir` correctly denies relative requested paths (a cleaned
relative path can never prefix-match an absolute base), and the exec guard correctly
denies a relative `cwd`. But no test pins this down — every test case uses absolute
paths. Since the schema only *describes* paths as absolute (not validated), a
relative input is a reachable case, and this deny-by-construction behavior could
regress unnoticed (e.g. if someone later "helpfully" joins relative paths onto
base_dir).
**Fix:** Add cases such as
`require.False(t, withinBaseDir("/srv/app", "src/main.go"))` and an exec-handler test
with `cwd: "subdir"` and base_dir set.

### IN-03: base_dir guard block duplicated verbatim in five handlers

**Files:** `internal/tools/dir.go:119-128`, `internal/tools/file_read.go:47-56`, `internal/tools/file_write.go:46-55`, `internal/tools/transfer.go:64-74`, `internal/tools/transfer.go:131-141`
**Issue:** The same ~10-line `if baseDir := cfg.Hosts[hostName].BaseDir; baseDir != "" { if !withinBaseDir(...) { return &mcp.CallToolResult{...} } }`
block (including the identical error string) appears five times, plus a variant in
exec.go. Any future fix (e.g. WR-02, or changing the error text) must be applied in
six places.
**Fix:** Extract a helper in sandbox.go, e.g.
`func baseDirViolation(hostName, baseDir, p string) *mcp.CallToolResult` returning
nil when allowed, and call it from each handler.

### IN-04: config.Validate uses filepath.Clean/IsAbs on a remote POSIX path while sandbox.go mandates path.Clean (D-06)

**File:** `internal/config/config.go:165-168` (cross-file; not in the changed-file set)
**Issue:** sandbox.go's doc comment explicitly requires POSIX `path.Clean` "not
filepath.Clean (D-06)" because `base_dir` is a remote POSIX path, yet the
`Validate()` code that establishes the helper's documented precondition uses
`filepath.IsAbs` and `filepath.Clean`. On the supported platforms (macOS/Linux) the
two packages behave identically for these inputs, so there is no current defect —
but the inconsistency contradicts the project's own stated decision and would break
the precondition if the daemon were ever built for Windows.
**Fix:** Use `path.IsAbs` / `path.Clean` in the `BaseDir` branch of `Validate()` for
consistency with D-06.

---

_Reviewed: 2026-06-10T10:26:13Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
