# Feature Landscape: v2.1 Tunneling & Access Controls

**Domain:** SSH daemon MCP tools — port forwarding management, command allowlist, path sandboxing
**Researched:** 2026-06-04
**Project state:** ~7,500 LOC Go, v2.0 complete, adding features to existing multi-host aware tools

---

## Table Stakes

Features users and Claude expect. Missing = the tool fails its purpose.

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| ssh_port_forward: local_port, remote_host, remote_port, host params | Direct map to `ssh -L local_port:remote_host:remote_port` | Low | local_port=0 for auto-assign is table stakes; see below |
| ssh_kill_forward: by local_port + host | Port number is the natural stable ID the caller already knows | Low | Must match against active registry, not blind subprocess kill |
| ssh_list_forwards: returns local_port, remote_host, remote_port, host, started_at | Everything needed to use or kill a forward | Low | OpenSSH has no native `-O list` command; daemon must maintain its own registry |
| Port already in use → clear error, not panic | Calling ssh_port_forward on a taken port is common | Low | `net.Listen("tcp", ...)` returns EADDRINUSE; return IsError=true with port number |
| ControlMaster dead → clear error with re-establishment hint | Consistent with existing CheckSocket pattern | Low | Reuse existing error message format from CheckSocket |
| exec_allowlist absent → allow all (default-allow) | No allowlist key = unrestricted exec; existing behavior preserved | Low | Explicit design decision; document in config schema |
| exec_allowlist present + command not matched → deny with message | Allowlist semantics: default-deny when list is set | Low | Error message must include the blocked command and the list |
| exec_allowlist empty list `[]` → deny all commands | Empty list set explicitly = operator intent to block everything | Low | Distinguish from absent key; `[]` != absent |
| base_dir present → all file ops and exec cwd confined to that prefix | Prevents Claude from reading/writing outside the sandbox | Medium | Must apply to ssh_read_file, ssh_write_file, ssh_list_dir, ssh_upload_file, ssh_download_file, ssh_exec cwd |
| base_dir: absolute paths in tool calls silently rebased | User-supplied absolute paths must not escape the sandbox | Medium | Strip leading `/` then join with base_dir |
| base_dir: `../` traversal → error or silently rebased | Classic path traversal attempt | Medium | Use filepath.IsLocal or string-prefix check after filepath.Clean |

---

## Differentiators

Features that add value beyond the minimum.

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| ssh_port_forward: local_port=0 auto-assigns and returns the actual bound port | Claude can request an ephemeral port without guessing; returned port is immediately usable | Medium | Requires in-process net.Listener (not ssh -O forward) to capture the OS-assigned port; see Architecture section |
| ssh_list_forwards: duration_seconds field | At a glance, Claude knows if a tunnel is stale or fresh | Low | time.Since(startedAt).Seconds() at query time |
| ssh_kill_forward: idempotent — killing a dead/unknown forward returns success | Claude may call kill redundantly; crashing is worse than silent ok | Low | Check registry; if not found, return success with "forward not found (already removed?)" in message field |
| exec_allowlist: error message lists configured prefixes | Claude can self-correct by using an allowed prefix | Low | `command "foo bar" is blocked; allowed prefixes: [git, docker, systemctl]` |
| Port forward scoped to named host | Forward on "prod" vs "staging" tracked independently; same local_port can exist on different hosts | Low | Forward registry key = (host_name, local_port) tuple |
| ssh_port_forward: guard scan NOT applied | Port forward output is a structural status response, not user-controlled content; scanning would add false positives | Low | Consistent with existing binary-bypass decision |

---

## Anti-Features

Features to explicitly NOT build in this milestone.

| Anti-Feature | Why Avoid | What to Do Instead |
|--------------|-----------|-------------------|
| Remote port forwarding (-R) | Out-of-scope for v2.1; adds reverse tunnel lifecycle complexity orthogonal to current goal | Caller uses ssh_exec to run `ssh -R ...` on the remote if needed |
| Dynamic forwarding / SOCKS proxy (-D) | Niche; different lifecycle semantics; no direct Claude use case identified | Defer; can be added in a future milestone |
| Persistent forwards across daemon restart | Daemon is ephemeral by design; persisting forward state adds durability complexity without value | User re-calls ssh_port_forward after daemon restart |
| Auto-restart dead forwards | Reconnection logic belongs to the ControlMaster layer the user owns | Return error on dead forward; user decides whether to re-establish |
| base_dir applied to ssh_exec stdout/stderr content | base_dir is a path confinement policy, not a content filter; guard already handles injection | Do nothing; guard handles content scanning |
| base_dir on exec command string itself | base_dir is about file paths, not command text; allowlist handles command restriction | Use exec_allowlist for command filtering |
| Wildcard/regex exec_allowlist entries | Prefix-match is simpler, auditable, and sufficient for the use cases (git, docker, systemctl) | Prefix only in v2.1; regex can be added later if needed |
| ssh_list_forwards showing connection counts or bandwidth | Requires in-process traffic accounting at the goroutine level; not worth the complexity | local_port + remote target + duration is sufficient |

---

## Feature Details and Edge Cases

### ssh_port_forward

**Parameters:**
- `local_port` int (required, 0 = auto-assign OS ephemeral port)
- `remote_host` string (required, e.g. `localhost` or `db.internal`)
- `remote_port` int (required)
- `host` string (optional, follows existing multi-host convention)

**Implementation approach — in-process listener, not `ssh -O forward`:**

`ssh -O forward` adds a forward to an existing ControlMaster session but has a critical limitation: there is no `ssh -O list` command to enumerate active forwards. The daemon would have no way to satisfy `ssh_list_forwards` if it delegates to OpenSSH's mux protocol.

The correct approach for this daemon is in-process local port binding:
1. Call `net.Listen("tcp", "127.0.0.1:<local_port>")` (or `"127.0.0.1:0"` for auto-assign).
2. Record the listener + metadata in a per-host forward registry.
3. Spawn a goroutine that accepts connections on the listener and pipes them through `ssh -S <socket> -W remote_host:remote_port` (ProxyCommand-style, using the existing ControlMaster socket).
4. Store the actual bound port (from `listener.Addr().(*net.TCPAddr).Port`) in the registry and return it to the caller.

This approach: owns the lifecycle, supports local_port=0, enables ssh_list_forwards, and matches the existing `os/exec` pattern.

**Success response fields:**
- `local_port` int (the actual bound port, important when input was 0)
- `remote_host` string
- `remote_port` int
- `host` string (resolved name)
- `started_at` RFC3339 timestamp

**Failure modes and expected behavior:**

| Failure | Behavior |
|---------|----------|
| local_port already in use | IsError=true; `"local port 8080 is already in use"` |
| remote_host:remote_port unreachable | NOT detected at bind time; detected only when a connection is accepted and the pipe fails; ssh_port_forward succeeds (the listener is bound), the pipe goroutine errors silently or logs |
| ControlMaster socket dead | IsError=true; reuse existing CheckSocket error format |
| local_port already forwarded by this daemon (same host) | IsError=true; `"local port 8080 already has an active forward on host prod; call ssh_kill_forward first"` |
| local_port < 1 or > 65535 (excluding 0) | IsError=true before attempting bind |
| local_port in privileged range (1-1023) | Attempt anyway; OS will return EACCES; surface that error |

**Important: remote unreachable is not detected at bind time.** This is the same behavior as native SSH — `ssh -L 8080:localhost:9999 host` succeeds even if port 9999 is not listening on the remote. The forward is "established" (listener is bound); errors appear when traffic flows. Document this clearly.

### ssh_kill_forward

**Parameters:**
- `local_port` int (required)
- `host` string (optional, default_host if absent)

**Kill behavior:**
1. Look up `(host_name, local_port)` in the forward registry.
2. Close the `net.Listener` (causes the accept goroutine to exit on next iteration).
3. Remove from registry.
4. Return success.

**Idempotency:** If `(host_name, local_port)` is not in the registry, return success with a `"not_found": true` field and a message `"no active forward on port 8080 for host prod (already removed?)"`. Do NOT return IsError=true — idempotent kill is the right UX for a tool Claude may call redundantly.

**Already-dead goroutine:** The accept goroutine exits when the listener is closed; no separate "is the goroutine still running?" check is needed. Close the listener, remove from registry — done.

### ssh_list_forwards

**Parameters:**
- `host` string (optional; if absent, returns forwards for ALL hosts)

**Response:** Array of forward objects. Empty array if none active (not an error).

**Per-forward fields:**
- `local_port` int
- `remote_host` string
- `remote_port` int
- `host` string
- `started_at` string (RFC3339)
- `duration_seconds` int (time.Since(startedAt).Seconds(), rounded)

**No OpenSSH native list:** Confirmed — `ssh -O list` does not exist. The daemon's own registry is the authoritative source. This is actually an advantage: the daemon knows exactly what it created.

**Multi-host behavior:** When `host` param is absent, iterate all hosts and return all forwards sorted by host name then local_port for deterministic output (consistent with existing sortedKeys pattern).

### exec_allowlist

**Config shape:**
```json
"hosts": {
  "prod": {
    "socket": "...",
    "user": "...",
    "host": "...",
    "exec_allowlist": ["git", "docker compose", "systemctl status"]
  }
}
```

**Matching semantics:** A command matches if it has any configured prefix as a string prefix. Prefix match is the right model for this tool: operators want to allow `git status`, `git pull`, etc. by writing `"git"`, not enumerate every subcommand. `strings.HasPrefix(command, prefix)` after trimming leading whitespace from the command string.

**Empty list vs. absent key:**
- Key absent (or `null`): allow all commands (default-allow, existing behavior preserved for all existing configs).
- Key present as `[]`: deny all commands. This is intentional — an operator who sets an explicit empty list intends to block exec entirely on that host.
- Key present as `["git"]`: allow only commands that start with `"git"`.

**Error message format (denied):**
`command "rm -rf /tmp/x" is blocked by exec_allowlist; allowed prefixes for host "prod": [docker, git, systemctl]`

Include the sorted prefix list in the error. Claude reads this and can self-correct. Consistent with existing `resolveExecutor` pattern of listing available options in errors.

**Gate ordering:** Allowlist check fires AFTER the existing SAFE-02 (allow_delete) check. Both are pre-executor gates. Order: allow_delete → allowlist → resolveExecutor → RunCommand.

**Guard interaction:** exec_allowlist is a pre-execution gate. Guard still scans stdout/stderr for injection. No interaction issues.

### base_dir

**Config shape:**
```json
"hosts": {
  "prod": {
    "socket": "...",
    "user": "...",
    "host": "...",
    "base_dir": "/home/deploy/app"
  }
}
```

**Scope of enforcement:**
- `ssh_read_file`: `path` param must resolve within base_dir
- `ssh_write_file`: `path` param must resolve within base_dir
- `ssh_list_dir`: `path` param must resolve within base_dir
- `ssh_upload_file`: `remote_path` param must resolve within base_dir
- `ssh_download_file`: `remote_path` param must resolve within base_dir
- `ssh_exec`: `cwd` param (if set) must resolve within base_dir; command string is NOT filtered by base_dir (that is allowlist's job)

**Path resolution algorithm for remote paths:**

Note: base_dir is a remote server path policy. The daemon cannot call `filepath.EvalSymlinks` on remote paths because those paths exist on the remote filesystem. `os.Root` and `filepath.EvalSymlinks` are local-only tools.

The correct approach for remote paths is lexical: the daemon can only validate what it knows statically (the raw path string before it hits the remote). Symlinks on the remote server cannot be resolved without a remote stat call.

Recommended algorithm:
```
1. filepath.Clean(path) — normalize .., ., double slashes
2. If path is relative, join with base_dir: filepath.Join(base_dir, path)
3. If path is absolute and does NOT start with base_dir: error
4. If path is absolute and DOES start with base_dir: allow
5. After Clean+Join, verify result strings.HasPrefix(resolved, base_dir+"/") OR resolved == base_dir
```

Step 5's string prefix check is safe after filepath.Clean because Clean eliminates `..` sequences. This is the pattern recommended by https://rowin.dev/blog/preventing-path-traversal-attacks-in-go and https://dzx.cz/2021-04-02/go_path_traversal/.

**Symlink caveat:** A remote symlink like `/home/deploy/app/link -> /etc/passwd` would pass this check because the daemon only sees the source path. This is acceptable for this threat model (the operator configured base_dir, and symlinks on the remote server are under operator control). Document this limitation explicitly. This is the same TOCTOU acceptance already made for SAFE-01 (noted in PROJECT.md key decisions).

**Absolute path rebasing:** If Claude passes `/etc/passwd` and base_dir is `/home/deploy/app`, the path does not have base_dir as a prefix → return IsError=true with a clear message. Do NOT silently rebase absolute paths — silent rebasing would confuse Claude about what was actually accessed.

**Error message format:**
`path "/etc/passwd" is outside base_dir "/home/deploy/app" for host "prod"`

**base_dir absent:** No enforcement. Existing behavior preserved.

**base_dir + exec cwd:** When cwd is set in ssh_exec AND base_dir is configured, validate cwd against base_dir using the same algorithm. When cwd is absent, no validation needed (command runs in the shell's default remote cwd, which is outside daemon control).

---

## Feature Dependencies

```
ssh_kill_forward → ssh_port_forward (needs an active forward to kill)
ssh_list_forwards → ssh_port_forward (reads the registry created by port_forward)

exec_allowlist → ssh_exec (wraps the existing exec handler with a pre-gate)
base_dir → ssh_read_file, ssh_write_file, ssh_list_dir, ssh_upload_file, ssh_download_file, ssh_exec (wraps all path-bearing handlers)

All three features → HostConfig (new fields added to existing HostConfig struct)
Port forwarding → ForwardRegistry (new daemon-level state, per-host map protected by sync.RWMutex)
```

**Dependency on existing tools:** All three feature areas are additive. They wrap or extend existing handlers without replacing them. The `resolveExecutor` pattern is already available for host lookup. The HostConfig struct gets two new optional fields (`exec_allowlist []string`, `base_dir string`). The Capabilities struct gets `port_forward bool` (already present in config.go as of this reading).

---

## MVP Recommendation

All three feature groups are in-scope for v2.1 and have comparable complexity. Suggested implementation order:

1. **exec_allowlist** — Pure config + string matching in one handler. No new state. Fastest to implement and test. Proves the HostConfig extension pattern.
2. **base_dir** — Pure path string manipulation in multiple handlers. No new state. Higher blast radius (touches 6 handlers) but each change is a small check at the top.
3. **Port forwarding** — Requires new daemon state (ForwardRegistry with sync.RWMutex), goroutine lifecycle management, and 3 new tools. Highest complexity; do last to avoid state management interacting with ongoing allowlist/base_dir work.

**Defer:** Nothing in this milestone should be deferred. All three are the milestone definition.

---

## Phase-Specific Complexity Notes

| Feature | Complexity Driver | Test Strategy |
|---------|------------------|---------------|
| Port forward registry | sync.RWMutex correctness; goroutine leak on kill | Unit test with mock listener; integration test with real socket |
| exec_allowlist empty list semantics | Must not regress default-allow for existing configs | Table-driven test: absent key, nil slice, empty slice, populated slice |
| base_dir absolute path edge cases | Path with trailing slash, path == base_dir exactly, path with encoded sequences | Table-driven path tests; no SSH required |
| Remote unreachable at forward time | NOT detected at bind time — must document, not fix | Integration test showing forward succeeds even when remote port is down |
| Port forward + ControlMaster dead | CheckSocket before accepting connections, or handle pipe error gracefully | Mock executor returning error |

---

## Sources

- OpenSSH multiplexing documentation: https://en.wikibooks.org/wiki/OpenSSH/Cookbook/Multiplexing
- ssh -O forward/cancel demonstration: https://gist.github.com/aculich/4265549
- Go traversal-resistant file APIs (os.Root, Go 1.24): https://go.dev/blog/osroot
- Go path traversal prevention: https://rowin.dev/blog/preventing-path-traversal-attacks-in-go
- Go path traversal prevention: https://dzx.cz/2021-04-02/go_path_traversal/
- SSH port forwarding in Go (implementation patterns): https://eli.thegreenplace.net/2022/ssh-port-forwarding-with-go/
- sshtunnel API (Python reference for tunnel metadata patterns): https://sshtunnel.readthedocs.io/en/latest/index.html
- Allowlist semantics (default-deny when set): https://dev.to/mateuscechetto/allowlist-vs-denylist-when-to-use-them-5d6c
