# Phase 10: BaseDir Path Sandbox - Context

**Gathered:** 2026-06-10
**Status:** Ready for planning

<domain>
## Phase Boundary

Enforce that all file operations (`ssh_read_file`, `ssh_write_file`, `ssh_list_dir`, `ssh_upload_file`, `ssh_download_file`) and `ssh_exec` cwd on hosts with `base_dir` set are confined to that directory tree via lexical path validation. No symlink resolution. No new tools — enforcement added to 6 existing handlers.

</domain>

<decisions>
## Implementation Decisions

### Exec cwd enforcement (BDIR-02 design gap)
- **D-01:** When `base_dir` is set and `ssh_exec` is called with no `cwd` (empty string), the daemon MUST reject with `isError: true`. BDIR-02 says "rejects where cwd resolves outside base_dir" but empty cwd doesn't resolve lexically — explicit rejection closes the sandbox gap. Error message: `[host X] cwd is required when base_dir is set`.
- **D-02:** When `cwd` is non-empty and `base_dir` is set, apply the same `path.Clean` + `strings.HasPrefix` check as file operations. If it resolves outside, reject with `isError: true`.

### Error message format
- **D-03:** Path rejection errors MUST include the configured `base_dir` value. Format: `[host X] path "..." is outside base_dir "..."`. Consistent with the exec_allowlist error style (which reveals allowed prefixes). Helpful for debugging misconfigured callers.

### Validation helper placement
- **D-04:** Create `internal/tools/sandbox.go` with a shared `withinBaseDir(baseDir, requestedPath string) bool` helper. All 6 handlers call it — avoids repeating the 3-line `path.Clean` + trailing-slash prefix check. Phase 9 used inline (1 site); Phase 10 has 6 sites — shared helper is appropriate here.

### Path validation mechanics (locked by BDIR-03)
- **D-05:** Validation is purely lexical: `path.Clean(requestedPath)` then `strings.HasPrefix(cleaned+"/", strings.TrimRight(baseDir, "/")+"/")`. No symlink resolution. Document the symlink blind spot in each affected tool's schema description, consistent with SAFE-01 precedent.
- **D-06:** Use `path.Clean` (not `filepath.Clean`) for remote paths — POSIX, not OS-native (locked in STATE.md).

### Check ordering in write/transfer handlers
- **D-07:** Base_dir check runs AFTER `resolveExecutor` but BEFORE the `allow_overwrite` remote `test -e` check. Fail fast on out-of-sandbox paths without touching the remote.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements
- `.planning/REQUIREMENTS.md` §BDIR — BDIR-01 through BDIR-04 define all acceptance criteria for this phase
- `.planning/ROADMAP.md` §Phase 10 — Goal, dependencies, success criteria

### Prior implementation (pattern reference)
- `internal/tools/exec.go` lines 67–99 — exec_allowlist enforcement pattern (inline after resolveExecutor; `isError: true` with `[host %s]` prefix)
- `internal/config/config.go` — `HostConfig.BaseDir` field, already validated/cleaned at startup (BDIR-04 done)
- `internal/tools/transfer.go` — upload/download handler structure (both have `RemotePath` fields needing base_dir check)

### Config
- `internal/config/config.go` — `HostConfig` struct; `BaseDir string` field with startup validation

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `resolveExecutor()` in `internal/tools/resolve.go` — all 6 handlers call this first; base_dir check goes immediately after
- `newExecAllowlistServer()` test helper in `internal/tools/exec_test.go` — pattern for building a test MCP server with per-host config fields set (singleHostRegistry overwrites HostConfig, so tests set Hosts directly); mirror this pattern as `newBaseDirServer()` for Phase 10 tests

### Established Patterns
- Error format: `[host %s] <reason>` with `isError: true` and `mcp.TextContent` — used by all handlers
- Inline enforcement after `resolveExecutor`, before the actual SSH operation — Phase 9 exec_allowlist pattern
- `path.Clean` for remote paths (STATE.md decision), `filepath.Clean` for local paths (config startup)

### Integration Points
- `internal/tools/file_read.go` — `readFileHandler`: add base_dir check on `in.Path`
- `internal/tools/file_write.go` — `writeFileHandler`: add base_dir check on `in.Path` before allow_overwrite check
- `internal/tools/dir.go` — `listDirHandler`: add base_dir check on `in.Path`
- `internal/tools/transfer.go` — `uploadHandler`, `downloadHandler`: add base_dir check on `in.RemotePath`
- `internal/tools/exec.go` — `execHandler`: add empty-cwd rejection + cwd base_dir check after allowlist check

</code_context>

<specifics>
## Specific Ideas

- New file `internal/tools/sandbox.go` houses the `withinBaseDir` helper and its tests
- Symlink caveat language should match SAFE-01 precedent in tool description — short, factual, no alarm
- Test helper `newBaseDirServer(t, exec, baseDir string)` mirrors `newExecAllowlistServer` — build Hosts map directly, set BaseDir on the default host

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 10-BaseDir Path Sandbox*
*Context gathered: 2026-06-10*
