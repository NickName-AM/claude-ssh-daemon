---
phase: 09-command-allowlist
reviewed: 2026-06-05T04:53:13Z
depth: standard
files_reviewed: 2
files_reviewed_list:
  - internal/tools/exec.go
  - internal/tools/exec_test.go
findings:
  critical: 1
  warning: 1
  info: 1
  total: 3
status: issues_found
---

# Phase 09: Code Review Report

**Reviewed:** 2026-06-05T04:53:13Z
**Depth:** standard
**Files Reviewed:** 2
**Status:** issues_found

## Summary

Two files reviewed: `internal/tools/exec.go` (the `execHandler` implementation for the `ssh_exec` MCP tool, including the new ALWL allowlist enforcement) and `internal/tools/exec_test.go` (full test suite for that handler).

The core logic is structurally sound: nil/empty/populated allowlist semantics are correctly implemented, the destructive-command gate fires before executor resolution as intended, and guard scanning works correctly. However, one security gap exists in the interaction between the SAFE-02 destructive-command gate and the allowlist: when a `git`-prefixed allowlist entry is in effect, commands like `git rm -rf .` and `git clean -fdx` bypass the destructive-command block entirely because `isDestructiveCommand` only inspects the first token. The test suite has a corresponding coverage gap — no test exercises this interaction.

---

## Critical Issues

### CR-01: SAFE-02 Destructive Guard Bypassed via Git Subcommands When Allowlist Is Active

**File:** `internal/tools/exec.go:48-98`

**Issue:** `isDestructiveCommand` (SAFE-02) checks only `fields[0]` — the first space-delimited token — using `filepath.Base`. This is correct for plain destructive commands (`rm`, `/bin/rm`). However, when an allowlist prefix such as `"git "` is configured, commands like `"git rm -rf ."` and `"git clean -fdx"` pass through SAFE-02 because their first token is `"git"`, which is not in `destructiveCommands`. They then pass the allowlist check because `strings.HasPrefix("git rm -rf .", "git ")` is `true`. The result is that `git rm` and `git clean` reach the SSH executor even when `safeguards.allow_delete: false` — the stated intent of SAFE-02.

Both `git rm` and `git clean` can delete files recursively on the remote host, making this a meaningful bypass of the delete-protection safeguard.

The gate ordering in `execHandler` (SAFE-02 before allowlist) does not help here because SAFE-02 never fires for these commands.

**Fix:** Extend `isDestructiveCommand` to also inspect subcommands for well-known wrappers, or apply a secondary destructive-subcommand check after the allowlist passes. The minimal approach is to check for git subcommands known to delete files:

```go
// In safeguards.go, extend isDestructiveCommand or add a new check:

// destructiveGitSubcommands are git subcommands that delete files and require
// allow_delete=true regardless of allowlist configuration.
var destructiveGitSubcommands = map[string]struct{}{
    "rm":    {},
    "clean": {},
}

// isDestructiveGitCommand returns true for "git rm ..." and "git clean ..." forms.
func isDestructiveGitCommand(cmd string) bool {
    fields := strings.Fields(cmd)
    // Must be at least "git <subcmd>"
    if len(fields) < 2 {
        return false
    }
    if filepath.Base(fields[0]) != "git" {
        return false
    }
    _, ok := destructiveGitSubcommands[fields[1]]
    return ok
}
```

Then in `execHandler`, add a second destructive check after the allowlist passes:

```go
// After allowlist check passes, re-check for destructive git subcommands (SAFE-02).
if !cfg.Safeguards.AllowDelete {
    if isDestructiveGitCommand(in.Command) {
        return &mcp.CallToolResult{
            IsError: true,
            Content: []mcp.Content{&mcp.TextContent{
                Text: fmt.Sprintf("command %q is blocked: git rm/clean are destructive; set safeguards.allow_delete: true to allow"),
            }},
        }, ExecOutput{}, nil
    }
}
```

Alternatively, document in the SAFE-02 comments that the allowlist intentionally supersedes the destructive-command gate for git subcommands (opt-in for the operator), but this is a weaker resolution — the current code comments describe SAFE-02 as a pre-executor gate with no mention of this bypass.

---

## Warnings

### WR-01: Config Validation Does Not Reject Whitespace-Only Allowlist Entries

**File:** `internal/config/config.go:173` (cross-file; impacts exec.go behavior)

**Issue:** `Validate()` rejects `exec_allowlist` entries that equal `""` (exact empty string) but does not reject entries containing only whitespace (e.g., `"   "`). A whitespace-only prefix passes `entry == ""` validation, but `strings.HasPrefix(cmd, "   ")` is `false` for all normal commands (which do not start with spaces). This is functionally harmless for standard SSH commands but represents a misconfiguration that the operator could accidentally introduce. The config comment on the empty-string check says "empty string is a prefix of every command" — the same logic applies to a tab or narrow-space prefix that could match if the command is passed with leading whitespace by Claude.

More practically: the whitespace-only entry effectively acts as a never-matching prefix that silently denies all commands without a clear error. An operator who types `" "` instead of `"git "` by mistake would see all commands rejected with "not in exec_allowlist" and no hint that the allowlist entry is malformed.

**Fix:** In `config.go`, extend the allowlist entry validation to also reject whitespace-only strings:

```go
if strings.TrimSpace(entry) == "" {
    return fmt.Errorf(
        "config: hosts[%q].exec_allowlist[%d] must not be blank (whitespace-only string is not a useful prefix)",
        name, j,
    )
}
```

---

## Info

### IN-01: No Test for SAFE-02 / Allowlist Gate Interaction (Coverage Gap)

**File:** `internal/tools/exec_test.go`

**Issue:** There is no test that verifies behavior when a command matches the allowlist AND would be blocked by SAFE-02 if it were the first token. Specifically, no test checks `"git rm -rf ."` against `allowlist=["git "]` with `AllowDelete=false`. The existing tests for SAFE-02 (`TestSafe02BlocksDestructiveCommandByDefault`, etc.) use `newExecSafeguardsServer`, which builds `HostConfig` without an `ExecAllowlist` (nil = allow-all), so the allowlist codepath is never exercised in safeguards tests. Conversely, the allowlist tests use non-destructive commands (`"ls -la"`, `"cat secrets"`) that never trigger SAFE-02.

This gap is directly linked to CR-01 — the bypass is undetected because the interaction is untested.

**Fix:** Add a test that exercises the interaction explicitly:

```go
// TestSafe02GateFiresBeforeAllowlistForGitRm verifies that "git rm -rf ."
// is blocked by SAFE-02 even when the allowlist contains "git " (SAFE-02 must
// not be circumventable via allowlist inclusion of wrapper commands).
func TestSafe02GateFiresBeforeAllowlistForGitRm(t *testing.T) {
    mock := &toolsMockExecutor{}
    cs := newExecAllowlistServer(t, mock, &[]string{"git "})
    // Also configure AllowDelete=false (default) — but newExecAllowlistServer
    // doesn't set Safeguards; extend it or use a custom cfg.
    result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
        Name:      "ssh_exec",
        Arguments: map[string]any{"command": "git rm -rf ."},
    })
    require.NoError(t, err)
    require.True(t, result.IsError, "git rm must be blocked by SAFE-02 even when allowlist allows 'git '")
    require.False(t, mock.runCalled, "RunCommand must not be called for blocked git rm")
}
```

Note: `newExecAllowlistServer` currently constructs `cfg` without a `Safeguards` field, meaning `AllowDelete` defaults to `false` — the test helper already captures the right default. The test would currently fail, which is the evidence that CR-01 is real.

---

_Reviewed: 2026-06-05T04:53:13Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
