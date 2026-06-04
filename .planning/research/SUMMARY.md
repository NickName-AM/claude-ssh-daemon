# Project Research Summary

**Project:** claude-ssh-daemon v2.1 — Tunneling & Access Controls
**Domain:** Go SSH daemon — port forwarding, command allowlist, path sandboxing
**Researched:** 2026-06-04
**Confidence:** HIGH

## Executive Summary

v2.1 adds three orthogonal features to an already-shipped daemon: local port forwarding via in-process `net.Listener` + `ssh -L -S -N`, a per-host command allowlist enforced as a pre-execution prefix gate, and a per-host `base_dir` path sandbox applied to all file operations. All three are implementable with zero new go.mod dependencies — stdlib `net`, `strings`, `path`, `sync`, and the existing `os/exec` ControlMaster approach cover everything. The only library worth noting is `golang.org/x/crypto/ssh v0.52.0`, which now ships `NewControlClientConn`, but adopting it in this milestone is not justified since `ssh -L -S -N` via `os/exec` is simpler, already-tested in this codebase, and avoids taking on a brand-new API.

The recommended implementation approach uses in-process `net.Listener` for port forwarding (not `ssh -O forward`) because the daemon needs a list command and liveness tracking, neither of which the OpenSSH mux protocol provides. The `forward.Registry` is a new `internal/forward` package with a `sync.Mutex`-protected map; allowlist and `base_dir` helpers are pure string functions added to the existing `internal/tools/safeguards.go`. Config gets two new optional fields on `HostConfig` (`exec_allowlist []string`, `base_dir string`) — additive and backward-compatible.

The primary risks are process orphaning on ungraceful daemon exit, a startup race between `cmd.Start()` returning and the port actually listening, and the inherent shell-bypass limitation of first-token allowlist matching. All three have known mitigations: `Setpgid` + graceful shutdown hook for orphans; a bounded port-readiness poll for the race; and explicit documentation of the allowlist's coarse-filter semantics. The `base_dir` path check has a symlink blind-spot (the daemon cannot resolve remote symlinks without an extra round-trip) — document as a known limitation consistent with SAFE-01 precedent.

---

## Key Findings

### Stack Additions

No new dependencies. All features use the existing `os/exec` ControlMaster pattern, stdlib `net`, `path`, `sync`, and `strings`.

**Implementation choices:**

- **Port forwarding via `ssh -L -S -N` (not `-O forward`):** `-O forward` gives no list or liveness signal — the daemon would have no way to implement `ssh_list_forwards`. `ssh -L -S -N` spawns a distinct process the daemon can watch via `cmd.Wait()` and kill via `cmd.Process.Kill()`. Listener binding and port-0 ephemeral assignment are handled in-process with `net.Listen("tcp", "127.0.0.1:0")`.
- **Command allowlist via `strings.HasPrefix`:** prefix semantics (not basename-only, not regex) match operator intent — writing `"git"` to allow all git subcommands. Normalize to lowercase; use `filepath.Base(strings.Fields(cmd)[0])` only when checking for exact binary name restrictions.
- **BaseDir via `path.Clean` + `strings.HasPrefix`:** remote paths are POSIX, so `path.Clean` (not `filepath.Clean`) is correct. Append `/` to both sides of the prefix check to prevent `/base-dir-extra` false-pass.
- **`golang.org/x/crypto/ssh v0.52.0`:** `NewControlClientConn` is now available (merged 2026-05-22). Flag for v2.2+ (remote forwarding, SOCKS5). Do not add as a direct dependency in v2.1.

### Expected Features

**Must have (table stakes):**
- `ssh_port_forward` — local_port (0 = auto-assign), remote_host, remote_port, host params; returns actual bound port
- `ssh_kill_forward` — by local_port + host; idempotent (not-found returns success with message, not error)
- `ssh_list_forwards` — all active forwards (optionally filtered by host); includes `duration_seconds`; in-memory registry only — OpenSSH has no native list command
- Port conflict — clear error including which host owns the conflicting port
- ControlMaster dead — existing `CheckSocket` error format reused
- `exec_allowlist` absent — allow all (backward-compatible default)
- `exec_allowlist: []` (explicit empty) — deny all; distinct from absent
- `exec_allowlist: ["git"]` — deny all except commands whose string starts with `"git"`
- `base_dir` — all file ops and `ssh_exec` `cwd` param confined to prefix

**Should have (differentiators):**
- `local_port: 0` auto-assigns OS ephemeral port and returns the actual bound port
- `duration_seconds` in `ssh_list_forwards` output
- Exec allowlist error message lists configured prefixes so Claude can self-correct
- Forward registry keyed by `(host_name, local_port)` — same local port on different hosts is independent

**Deferred to v2.2+:**
- Remote port forwarding (`-R`)
- Dynamic forwarding / SOCKS5 (`-D`) — different lifecycle, no identified Claude use case
- Persistent forwards across daemon restart
- `NewControlClientConn` / native SSH channel API
- Per-host `allow_forward` toggle (capability is currently global)

**Explicit non-features (do not build):**
- `base_dir` applied to exec command string (allowlist handles that)
- `base_dir` applied to stdout/stderr content (guard handles that)
- Wildcard or regex allowlist entries
- Bandwidth / connection-count stats in forward listing

### Architecture Approach

The architecture is additive: two new optional fields on `HostConfig`, one new package (`internal/forward`), one new tool file (`internal/tools/forward.go`), two new safeguard helpers in `safeguards.go`, and a signature change to `RegisterTools`. The `cfg *Config` pointer is already shared read-only across all handlers — new config fields are immediately visible. Forward state lives in a separate `map[string]*forward.Registry` (per-host, keyed same as executor registry) passed alongside the executor registry to `RegisterTools`. No global state, no middleware layer, no config mutation.

**New and changed components:**

1. `internal/config/config.go` — ADD `ExecAllowlist *[]string` and `BaseDir string` to `HostConfig`; add `Validate()` check that `base_dir` is absolute if set; clean `base_dir` at validation time
2. `internal/forward/registry.go` — NEW: `ForwardEntry`, `Registry` (Start/Kill/List), subprocess lifecycle with `cmd.Wait()` watcher goroutine and context-based shutdown
3. `internal/tools/safeguards.go` — ADD `checkAllowlist(allowlist *[]string, cmd string) string` and `checkBaseDir(baseDir, remotePath string) string`
4. `internal/tools/forward.go` — NEW: `portForwardHandler`, `killForwardHandler`, `listForwardsHandler`
5. `internal/tools/register.go` — CHANGE: `RegisterTools` signature adds `forwardRegistry map[string]*forward.Registry`; port_forward capability toggle already exists in `Capabilities`
6. `internal/daemon/daemon.go` — ADD: fwdRegistry construction (one `forward.Registry` per host), pass to `RegisterTools`; extend graceful shutdown to kill all active forwards before drain timeout

### Critical Pitfalls

1. **Port forward process orphaning on ungraceful exit** — On Linux set `cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}`; on macOS use `Setpgid: true` + explicit signal. Extend daemon graceful shutdown to call `Kill()` on all active forwards before drain. Without this, stale ssh processes hold ports across daemon restarts.

2. **Startup race: `cmd.Start()` returns before the port is listening** — After `cmd.Start()`, poll `net.Dial("tcp", "127.0.0.1:<port>")` in a loop (50ms interval, 3-5s timeout) before returning success. Return a clear timeout error if the poll expires.

3. **Empty allowlist semantics footgun** — Use `*[]string` pointer to distinguish absent (nil = allow all) from present-but-empty (non-nil empty slice = deny all). Go's zero value for a slice is `nil`, which JSON-decodes identically for absent vs. `"exec_allowlist": []` without the pointer.

4. **`base_dir` prefix false-pass without trailing slash** — `strings.HasPrefix("/srv/app-other/file", "/srv/app")` is true. Compare `path.Clean(remotePath)+"/"` against `path.Clean(baseDir)+"/"` to enforce a path separator boundary. Clean `base_dir` at config load time.

5. **Allowlist shell-bypass is inherent, not a bug** — First-token prefix matching cannot stop `bash -c 'rm -rf /'`. Document the allowlist as a coarse policy filter, not a security boundary. Operators needing real enforcement should use `ForceCommand` in `sshd_config` on the remote side.

---

## Implications for Roadmap

These three features have no implementation dependencies on each other — they share only the config struct additions. The natural build order is: config first (unblocks everything), then allowlist and base_dir in parallel (pure string logic, no new state), then port forwarding last (new state, goroutine lifecycle, most complex).

### Phase 1: Config Schema + Validation

**Rationale:** Both `ExecAllowlist` and `BaseDir` must land on `HostConfig` before any handler code can be written. Config validation (base_dir must be absolute, cleaned at load time, `*[]string` pointer for allowlist) has zero handler dependencies and is easy to test in isolation. Getting this right first prevents the empty-list footgun from being baked in incorrectly.

**Delivers:** `HostConfig` with `ExecAllowlist *[]string` and `BaseDir string`; updated `Validate()`; config-level unit tests covering nil/empty/populated/absent variants.

**Avoids:** Pitfall 5 (empty list semantics), Pitfall 14 (trailing slash normalization fixed at load time).

### Phase 2a: Command Allowlist

**Rationale:** Pure string matching, no new state, touches only `execHandler` and `safeguards.go`. Proves the HostConfig extension pattern works end-to-end through MCP. Fastest feedback loop.

**Delivers:** `checkAllowlist` helper; gate in `execHandler` after `resolveExecutor`; table-driven tests covering absent/nil/empty/populated allowlist, case normalization, basename extraction, shell-bypass documentation in tool description.

**Avoids:** Pitfall 4 (shell bypass — document, don't fix), Pitfall 10 (case sensitivity — normalize to lowercase), Pitfall 11 (full path vs basename — use `filepath.Base`).

### Phase 2b: BaseDir Path Sandbox (parallel with 2a)

**Rationale:** Pure path string manipulation, no new state. Touches 6 handlers but each change is a small check after `resolveExecutor`. Can be developed in parallel with the allowlist.

**Delivers:** `checkBaseDir` helper using `path.Clean` (POSIX remote paths); gate in `readFileHandler`, `writeFileHandler`, `listDirHandler`, `uploadHandler`, `downloadHandler`; `cwd` gate in `execHandler`; table-driven path tests covering traversal, trailing slash, absolute paths, exact base_dir match, symlink limitation documented.

**Avoids:** Pitfall 6 (traversal + symlink — lexical three-layer defense, document symlink limitation), Pitfall 7 (cwd vs path semantics — explicit documentation).

### Phase 3: Port Forwarding

**Rationale:** Most complex feature — owns subprocess state, goroutine lifecycle, and a new package. Doing this last means the config and safeguard patterns are already proven. The `port_forward` capability toggle already exists in `Capabilities` — only handler wiring and `internal/forward` package are missing.

**Delivers:** `internal/forward` package; `internal/tools/forward.go` (three handlers); updated `RegisterTools` signature; daemon shutdown extended to kill all active forwards; port-readiness poll; daemon-level cross-host port collision detection via a global port map under mutex.

**Avoids:** Pitfall 1 (orphaning — Pdeathsig/Setpgid + shutdown hook), Pitfall 2 (silent death — `cmd.Wait()` watcher goroutine), Pitfall 3 (startup race — port-readiness poll), Pitfall 8 (multi-host port collision — daemon-level port map), Pitfall 9 (orphan on host removal — registry keyed by tracking key, not executor lookup), Pitfall 12 (MCP negotiation — document Claude Code restart requirement), Pitfall 13 (stderr swallowed — capture and surface in error).

### Phase Ordering Rationale

- Phase 1 is a hard prerequisite because all three features share the config struct.
- Phases 2a and 2b are independent of each other and of Phase 3; doing them first validates the HostConfig extension and safeguard helper patterns with zero-state code.
- Phase 3 is last because it is the only feature with mutable daemon state and subprocess lifecycle. Its additional complexity deserves a clean context after the simpler features are done.
- All three features are in-scope for v2.1 — nothing should be deferred.

### Research Flags

Phases with standard patterns (no additional research needed):
- **Phase 1 (config):** Straightforward Go struct additions. `*[]string` pointer pattern is a known Go idiom.
- **Phase 2a (allowlist):** Pure string matching; pattern established by existing `isDestructiveCommand`.
- **Phase 2b (base_dir):** `path.Clean` + `strings.HasPrefix` with trailing slash is well-documented.

Phases that may need targeted research during planning:
- **Phase 3 — platform differences:** `Pdeathsig` is Linux-only; macOS requires `Setpgid` + explicit signal. Verify exact `SysProcAttr` spelling against the Go version in use before implementation.
- **Phase 3 — `cmd.Wait()` goroutine:** Must call `Wait()` to reap the process. Verify the correct goroutine+channel pattern for surfacing unexpected exits back to the forward registry without leaking goroutines.

---

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | No new dependencies. All patterns verified against existing codebase. `NewControlClientConn` availability confirmed from git history and v0.52.0 tag. |
| Features | HIGH | Based on direct codebase inspection plus OpenSSH documentation. All edge cases enumerated with confirmed behavior. |
| Architecture | HIGH | All findings from direct inspection of `internal/config`, `internal/ssh`, `internal/tools`, `internal/daemon`. No inference from external sources. |
| Pitfalls | HIGH | Critical pitfalls verified against codebase, OpenSSH docs, and Go stdlib behavior. Platform-specific `Pdeathsig` difference is documented fact. |

**Overall confidence:** HIGH

### Gaps to Address

- **`base_dir` + exec `cwd` semantics conflict:** FEATURES.md says apply `base_dir` to exec `cwd`; PITFALLS.md (Pitfall 7) says document instead. Recommendation: apply base_dir to exec `cwd` (a sandbox that allows `cwd: /etc` despite `base_dir: /srv/app` surprises operators), but make it explicit in the tool description. Resolve in requirements before Phase 2b implementation.

- **Allowlist prefix vs. first-token semantics:** STACK.md matches against command basename only; ARCHITECTURE.md matches against the full command string from position 0. These differ for entries like `"git status"`. Recommendation: full-string prefix match (ARCHITECTURE.md position) — gives operators fine-grained control while remaining simple. Resolve before Phase 2a implementation.

- **Port-0 TOCTOU in Phase 3:** `net.Listen(":0")`, close, then spawn `ssh -L` on the discovered port creates a race. This is acknowledged as the same SAFE-01 precedent. Confirm acceptability before implementation; document in code comments.

---

## Sources

### Primary (HIGH confidence)
- Direct codebase inspection: `internal/config/config.go`, `internal/ssh/executor.go`, `internal/tools/{register,exec,resolve,safeguards,file_read,transfer,status}.go`, `internal/daemon/daemon.go`
- OpenSSH man page — `-O forward`, `-O cancel`, `-L`, `-N`, `-S` flags
- golang/crypto git history — `NewControlClientConn` merged 2026-05-22, v0.52.0 tag confirmed
- Go Blog: `os.Root` traversal-safe API — https://go.dev/blog/osroot

### Secondary (MEDIUM confidence)
- OpenSSH/Cookbook/Multiplexing (Wikibooks) — `-O forward` no-list limitation, ControlMaster lifetime behavior
- https://dzx.cz/2021-04-02/go_path_traversal/ — `filepath.Rel` and `strings.HasPrefix` with trailing slash pattern
- https://medium.com/@felixge/killing-a-child-process-and-all-of-its-children-in-go — `Setpgid` + process group signal pattern

### Tertiary (reference)
- OWASP OS Command Injection Defense Cheat Sheet — allowlist scope documentation guidance
- MCP lifecycle specification — tools/list negotiation at initialize time (Pitfall 12 basis)

---
*Research completed: 2026-06-04*
*Ready for roadmap: yes*
