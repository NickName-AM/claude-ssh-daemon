---
phase: 09-command-allowlist
reviewed: 2026-06-10T00:00:00Z
depth: standard
files_reviewed: 2
files_reviewed_list:
  - internal/tools/exec.go
  - internal/tools/exec_test.go
findings:
  critical: 1
  warning: 3
  info: 1
  total: 5
status: issues_found
---

# Phase 09: Code Review Report

**Reviewed:** 2026-06-10
**Depth:** standard
**Files Reviewed:** 2
**Status:** issues_found

## Summary

The three-state allowlist semantics (nil=allow-all, empty=deny-all, populated=prefix-match) are
correctly implemented. `strings.HasPrefix` is used throughout — not `strings.Contains` — so
substring bypass is correctly blocked. The test at line 506–515 of exec_test.go explicitly
verifies the HasPrefix/Contains distinction. Per-host independence is tested. The error messages
embed both the rejected command and the prefix list, satisfying the self-correction requirement.

One security-relevant gap exists: the allowlist check fires **after** the `SAFE-02` destructive
command block but **before** the executor — meaning a host with an allowlist that contains a
destructive prefix (e.g. `"rm "`) and `AllowDelete=false` would still be blocked by SAFE-02. This
ordering is actually safe, but the test suite does not verify the interaction, and the code comment
says SAFE-02 fires "before resolving executor" — not "before allowlist" — making the gate ordering
ambiguous in the comments (the code is correct; the description is incomplete).

A more serious gap: the allowlist prefix matching is case-sensitive with no normalization applied to
either the configured prefixes or the incoming command. Commands can be bypassed on any remote that
supports case-variation in shell built-ins or PATH entries. This is documented below.

Additionally, the `newExecAllowlistServer` helper at line 411 omits `MCPSocket` validation:
`config.Validate()` is never called on the config constructed in that helper, so the test bypasses
the config-layer's own empty-string validation for allowlist entries — a test infrastructure gap.

---

## Critical Issues

### CR-01: Allowlist prefix match is case-sensitive — case-variation bypass possible

**File:** `internal/tools/exec.go:83`
**Issue:** `strings.HasPrefix(in.Command, prefix)` performs a byte-exact, case-sensitive match.
An attacker (or an LLM prompted to probe the policy) can bypass an allowlist configured with
`["git "]` by sending `Git status` or `GIT status` on a remote where `$PATH` includes a
case-insensitive shell or where the remote filesystem is case-insensitive (macOS, most Windows
SFTP targets). Conversely, an allowlist configured with `["Git "]` will silently fail to cover
the canonical lowercase `git` invocations. The asymmetry between the allowlist author's intent
and the enforced invariant is a latent security bug.

There is no test that demonstrates this behavior, and no comment in the code or config schema
warns the operator that case matters — meaning most operators will assume case-insensitive
matching and write allowlists that are subtly incomplete or subtly over-permissive.

**Fix:** Normalize both the incoming command and each prefix to lowercase before comparison.
Because prefix intent is usually about the command name, not about case-sensitivity, this is
almost always the safer default. If exact case must be preserved, document the case-sensitivity
prominently in the config schema comment and add a test that proves it.

```go
// In the prefix-match loop (exec.go:82-87):
commandLower := strings.ToLower(in.Command)
for _, prefix := range *allowlist {
    if strings.HasPrefix(commandLower, strings.ToLower(prefix)) {
        matched = true
        break
    }
}
```

If case-sensitive matching is intentional policy, add a test and a prominent schema comment:
```go
// ExecAllowlist: prefix matching is CASE-SENSITIVE. "git " does not cover "Git ".
```

---

## Warnings

### WR-01: SAFE-02 / allowlist gate ordering not tested in combination — ambiguous comment

**File:** `internal/tools/exec.go:48-58` and `exec_test.go` (no test)
**Issue:** The code processes gates in this order: (1) SAFE-02 destructive block, (2) resolve
executor, (3) allowlist check. The code comment at line 48–49 says SAFE-02 fires "before
resolving executor" but does not mention the allowlist, making the relative ordering of SAFE-02
vs allowlist implicit. A future maintainer moving the allowlist block above the executor
resolution (a plausible refactor) would not know whether doing so should also move it above
SAFE-02. The test suite has no test verifying that a command matching both a destructive
pattern AND an allowlist prefix is correctly handled.

Concretely: if an operator adds `"rm "` to an allowlist to permit `rm -f /tmp/build/*` and
sets `AllowDelete=true`, that works. But if they set `AllowDelete=false`, SAFE-02 fires first
and blocks it — even though the allowlist would have passed it. The operator gets an error from
SAFE-02, not from the allowlist. Without a test the behavior is undocumented and could silently
regress.

**Fix:** Add a test documenting the gate order for the intersection case:

```go
// TestSafe02FiresBeforeAllowlist verifies that when a command matches a destructive
// pattern AND an allowlist prefix, SAFE-02 fires first and AllowDelete governs.
func TestSafe02FiresBeforeAllowlist(t *testing.T) {
    // allowlist includes "rm " but AllowDelete=false → SAFE-02 should block
    ...
    require.True(t, result.IsError)
    require.Contains(t, text.Text, "safeguards.allow_delete")
}
```

Add a clarifying comment in exec.go above the SAFE-02 block:
```go
// SAFE-02 fires before allowlist: destructive-command policy is unconditional.
// A command in the allowlist is still blocked here if AllowDelete is false.
```

### WR-02: Allowlist prefix `"git "` with trailing space rejects `"git"` (no arguments) — no test

**File:** `internal/tools/exec_test.go:471` (test uses `"git "` as prefix)
**Issue:** The allowlist tests configure `["git "]` (with a trailing space) as the example
prefix. The production code uses `strings.HasPrefix`, so `"git"` (no arguments) would be
rejected because `"git"` does not start with `"git "`. This is correct behavior for
`"git "` as a prefix, but it is also a likely operator mistake — someone who wants to allow
`git` and all sub-commands would naturally write `"git"` without the trailing space, which would
then match `"git_evil"`, `"gitconfig"`, etc.

The gap is that neither the config validation nor the test suite demonstrates what happens when
the prefix does not end with a space, and there is no warning in the config schema that a prefix
like `"git"` (no trailing space) matches the broader set of commands starting with those bytes.

The config validation does reject empty-string entries (config.go:172–178), but it does not
validate whether prefixes end with a word boundary. An operator who writes `["git"]` in the
allowlist will inadvertently allow `gitk`, `git-annex`, or any binary starting with `git`.

**Fix:** Add a test for prefix-without-trailing-space to document the behavior explicitly:

```go
// TestExecAllowlistPrefixNoTrailingSpaceMatchesBroadly verifies that "git" (no
// trailing space) also matches "gitconfig" and "git-evil" — operator warning.
func TestExecAllowlistPrefixNoTrailingSpaceMatchesBroadly(t *testing.T) {
    mock := &toolsMockExecutor{runResult: ssh.RunResult{Stdout: "ok\n", ExitCode: 0}}
    cs := newExecAllowlistServer(t, mock, &[]string{"git"})
    result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
        Name:      "ssh_exec",
        Arguments: map[string]any{"command": "git-evil --payload"},
    })
    require.NoError(t, err)
    require.False(t, result.IsError, "git-evil matches prefix 'git' — documents broad match risk")
}
```

Consider adding a config validation warning (not an error) or a schema comment about the
word-boundary risk.

### WR-03: `newExecAllowlistServer` bypasses `config.Validate()` — empty-string prefix guard is untested through the handler

**File:** `internal/tools/exec_test.go:411-428`
**Issue:** `newExecAllowlistServer` constructs `config.Config` directly and passes it to
`newTestServer` without calling `cfg.Validate()`. The production path that actually loads config
calls `Validate()`, which rejects empty-string entries in `exec_allowlist`
(config.go:172–178). Because the test helper bypasses `Validate()`, no test in exec_test.go
verifies that a config with an empty-string prefix `""` in the allowlist is rejected before it
reaches the handler. The handler itself has no defense against a `""` prefix — any command
string `strings.HasPrefix(cmd, "")` returns `true`, so a misconfigured `""` entry would silently
allow all commands through the allowlist gate.

**Fix:** Add a test or inline comment confirming that the empty-string defense lives in
`config.Validate()` and not in the handler. This is architecturally coherent but the handler has
no guard-in-depth. The handler should not trust that `Validate()` was called:

```go
// In the prefix-match loop, skip empty prefixes defensively:
for _, prefix := range *allowlist {
    if prefix == "" {
        continue // config.Validate() rejects this; skip defensively
    }
    if strings.HasPrefix(in.Command, prefix) {
        matched = true
        break
    }
}
```

Alternatively, add a test to exec_test.go that calls the handler with a zero-value
`Config` whose `ExecAllowlist` contains `""` and asserts that `""` is not used as
a match-all bypass.

---

## Info

### IN-01: Test helper comment at `exec_test.go:409` is misleading about why `singleHostRegistry` cannot be used

**File:** `internal/tools/exec_test.go:409-410`
**Issue:** The comment says `singleHostRegistry` "overwrites" the `ExecAllowlist` field. This
is technically correct — `singleHostRegistry` sets `cfg.Hosts` to a freshly constructed
map that has no `ExecAllowlist` field — but "overwrites" implies the field existed and was
clobbered. The real reason the helper cannot be used is that it creates a new
`map[string]config.HostConfig` with zero-value entries, so `ExecAllowlist` is `nil` regardless
of what was set beforehand. The comment would be clearer as:

```go
// newExecAllowlistServer builds a test server with exec_allowlist set on the
// single default host. cfg.Hosts is built directly (not via singleHostRegistry)
// because singleHostRegistry replaces cfg.Hosts entirely with zero-value entries,
// losing any HostConfig fields (ExecAllowlist, BaseDir) set by the caller.
```

This is a clarity-only finding; no behavioral defect.

---

_Reviewed: 2026-06-10_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
