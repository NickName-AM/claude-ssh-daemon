# Technology Stack — v2.1 Additions

**Project:** claude-ssh-daemon v2.1 (Tunneling & Access Controls)
**Researched:** 2026-06-04
**Scope:** Stack changes needed for port forwarding, command allowlist, and base_dir restriction. Existing stack (go-sdk v1.6.1, os/exec ControlMaster, testify) is already validated — this doc covers only what changes.

---

## 1. Port Forwarding: `ssh -O forward` via os/exec (no new dependency)

**Approach:** Use `ssh -S <socket> -O forward -L <localport>:<remotehost>:<remoteport> user@host` to attach a port forward to the already-running ControlMaster, then `ssh -S <socket> -O cancel -L ...` to tear it down.

**Confidence:** HIGH — verified against OpenSSH man page and wikibooks multiplexing cookbook.

### Why -O forward, not a separate `ssh -L` child process

A standalone `ssh -nNT -L ...` subprocess would open a second TCP connection to the remote host — bypassing the ControlMaster and requiring auth. The `-O forward` flag sends the forwarding request through the existing multiplexed connection, no second auth needed.

The trade-off: `-O forward` exits immediately (exit 0 on success, 255 on error). The actual listener lives inside the ControlMaster process. When the ControlMaster dies, all `-O forward`-created tunnels die with it — no orphan processes to clean up. This is the correct behavior for this daemon.

### Command shape

```
# Open:
ssh -S <socket> -O forward -L <localport>:<remotehost>:<remoteport> -o BatchMode=yes <user>@<host>

# Cancel:
ssh -S <socket> -O cancel -L <localport>:<remotehost>:<remoteport> -o BatchMode=yes <user>@<host>
```

Both commands exit immediately. Exit 255 = ControlMaster socket unreachable or forward request rejected. Exit 0 = success.

### Critical gotchas

1. **`-O forward` produces no stdout, only stderr on error.** The daemon must capture stderr and return it as the error message. The existing `cmd.Output()` pattern works; switch to `cmd.CombinedOutput()` or capture stderr separately for forward ops.

2. **No built-in list command.** `ssh -O forward` has no query/list capability (confirmed: OpenSSH PROTOCOL.mux defines no list-forwards message). The daemon must maintain its own in-memory registry of active forwards (keyed by `(host, localport)` or by a caller-supplied ID).

3. **`-O cancel` requires the exact same spec as `-O forward`.** The daemon must store the exact `-L localport:remotehost:remoteport` string used at open time to pass it verbatim at cancel time. Storing `{LocalPort, RemoteHost, RemotePort}` as a struct and reformatting is sufficient.

4. **Duplicate forward behavior:** If `-O forward` is called twice with the same local port, OpenSSH returns an error on the second call (port already in use). The daemon should check its registry before issuing the command and return a clear error rather than letting the ssh subprocess fail with an opaque message.

5. **Local port 0:** OpenSSH does NOT support ephemeral port assignment via `-L 0:...` for `-O forward`. The caller must supply an explicit port. The `ssh_port_forward` tool should require a `local_port` parameter (not accept 0).

6. **Bind address defaults to loopback.** Without a bind address, OpenSSH binds `127.0.0.1` (or `::1`) only. This is the correct default for this daemon — do not expose to 0.0.0.0. The spec string `localport:remotehost:remoteport` (no bind address prefix) achieves loopback-only binding.

### In-memory forward registry

The daemon needs a goroutine-safe registry. `sync.Mutex` + `map[string]ForwardEntry` is sufficient. One registry per host (or global, keyed by `hostName + ":" + localPort`). No external dependency.

```go
type ForwardEntry struct {
    HostName   string
    LocalPort  int
    RemoteHost string
    RemotePort int
}
```

### Integration with existing SSHExecutor

Add two methods to `SSHExecutor` interface:

```go
OpenForward(ctx context.Context, localPort int, remoteHost string, remotePort int) error
CancelForward(ctx context.Context, localPort int, remoteHost string, remotePort int) error
```

`ControlMasterExecutor` implements both by running the appropriate `ssh -O forward/cancel` subprocess. The MCP tool handlers own the registry — executors are stateless.

---

## 2. golang.org/x/crypto/ssh v0.52.0 — Now Available for Direct Forwarding

**Status:** `NewControlClientConn` was merged on 2026-05-22 and released in v0.52.0 (same date). The function lives in `ssh/control.go` (copyright 2026). This resolves the previously-open research flag.

**Confidence:** HIGH — verified by reading `golang/crypto` git history, confirming `control.go` exists at master with a 2026-05-22 commit, and confirming `v0.52.0` tag date.

**API:**

```go
// Connects to an existing ControlMaster Unix socket in proxy mode.
// Returns a full ssh.Conn — all SSH channels, forwarding, etc. available.
func NewControlClientConn(c net.Conn) (Conn, <-chan NewChannel, <-chan *Request, error)
```

**Should v2.1 use this instead of `-O forward`?** No, for this milestone.

Reasons:
- `-O forward` via os/exec works correctly and requires zero new dependency.
- `NewControlClientConn` gives a full `ssh.Conn` which can open `direct-tcpip` channels natively — more powerful but also more code surface. That's valuable for a future milestone (e.g., socks5 proxy, remote forwarding, keepalive).
- The go.mod currently has no `golang.org/x/crypto` direct dependency; adding it for only port forwarding when os/exec does the job cleanly is not justified.
- `NewControlClientConn` is brand new (weeks old as of research date). Letting it stabilize before adopting is prudent.

**Flag for v2.2+:** When implementing remote forwarding (`-R`), SOCKS5 (`-D`), or connection keepalives, `NewControlClientConn` + `ssh.Client.Listen()` is the right path. Record in PROJECT.md open questions.

---

## 3. Process Lifecycle — No New Dependency

**Confidence:** HIGH — stdlib is sufficient.

### For `-O forward` / `-O cancel` subprocess calls

Both subcommands exit immediately. Use the same `exec.CommandContext` pattern as existing executor methods. No background goroutines needed. No process group management needed.

### For potential future long-lived `ssh -L` processes (not needed in v2.1)

If the approach were ever changed to spawn a background `ssh -nNT -L` process instead of using `-O forward`, the required stdlib pattern is:

```go
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}  // creates new process group
cmd.Start()         // does not wait
// store cmd.Process for later
syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)  // kills whole group
cmd.Wait()
```

`Pdeathsig` is Linux-only; for macOS portability use `Setpgid` + explicit signal on daemon shutdown. However, because v2.1 uses `-O forward` (no long-lived subprocess), none of this applies now.

---

## 4. Command Allowlist — stdlib strings package, no new dependency

**Confidence:** HIGH

**Approach:** Prefix-match using `strings.Fields` to extract the first token, then `filepath.Base` to handle path-prefixed commands (same pattern as the existing `isDestructiveCommand`), then check the resolved base against the allowlist entries.

**Critical security concern — prefix match semantics:**

The PROJECT.md says "prefix-match" for the allowlist. This means `exec_allowlist: ["git", "make"]` allows any command that starts with `git` or `make` as its first token.

There is a non-obvious injection angle: a user-supplied command like `git; rm -rf /` passes the allowlist check because the first token is `git`, but the shell expansion on the remote side runs `rm -rf /`. This is not a problem specific to the allowlist implementation — it's a general property of the `ssh_exec` tool, which sends the command as a shell string. The allowlist's security guarantee is: "Claude cannot run arbitrary commands when an allowlist is configured," which is satisfied by first-token prefix matching. Document this scope limitation explicitly in the tool docstring.

**Implementation pattern:**

```go
func isAllowed(command string, allowlist []string) bool {
    if len(allowlist) == 0 {
        return true  // no allowlist = allow all
    }
    fields := strings.Fields(command)
    if len(fields) == 0 {
        return false
    }
    base := filepath.Base(fields[0])
    for _, prefix := range allowlist {
        if strings.HasPrefix(base, prefix) {
            return true
        }
    }
    return false
}
```

Note: If the intent is exact first-token match (e.g., `git` allows `git push` but not `gitk`), use `base == prefix` rather than `HasPrefix`. The PROJECT.md says "prefix-match" — clarify with the project owner before implementing. Both options use stdlib only.

**Config addition to `HostConfig`:**

```go
type HostConfig struct {
    Socket        string   `json:"socket"`
    User          string   `json:"user"`
    Host          string   `json:"host"`
    ExecAllowlist []string `json:"exec_allowlist,omitempty"`
}
```

---

## 5. base_dir Restriction — stdlib `path/filepath`, no new dependency

**Confidence:** HIGH for the Go stdlib approach. Note: `os.Root` (Go 1.24) exists but does not apply here (see below).

### The correct pattern

`filepath.Clean` alone does NOT prevent path traversal — it normalizes `../` but does not enforce directory boundaries. The correct Go pattern is:

```go
func sandboxPath(baseDir, requested string) (string, error) {
    // If requested is relative, anchor it under baseDir.
    // If absolute, reject or re-anchor depending on policy.
    var abs string
    if filepath.IsAbs(requested) {
        // Policy choice: reject absolute paths, or allow if they're already under baseDir.
        abs = filepath.Clean(requested)
    } else {
        abs = filepath.Clean(filepath.Join(baseDir, requested))
    }
    // Confirm the resolved path is inside baseDir.
    rel, err := filepath.Rel(baseDir, abs)
    if err != nil || strings.HasPrefix(rel, "..") {
        return "", fmt.Errorf("path %q escapes base_dir %q", requested, baseDir)
    }
    return abs, nil
}
```

This works on both Linux and macOS (pure string operations on the remote path string — no local filesystem access needed, since the path is remote).

**Why not `os.Root` (Go 1.24)?**

`os.Root` is a local filesystem API — it opens a real directory on the local machine and prevents local file operations from escaping it. The `base_dir` restriction in this daemon applies to _remote_ paths sent as strings to SSH commands. There is no local file to open. `os.Root` does not apply.

**Why not `cyphar/filepath-securejoin`?**

This library handles symlink resolution on the local filesystem. Remote symlinks are not visible to the daemon. The string-based `filepath.Rel` check is the correct tool. No new dependency justified.

### Application surface

`base_dir` must be applied to:
- `ssh_read_file`: `remote_path` parameter
- `ssh_write_file`: `remote_path` parameter
- `ssh_list_dir`: `remote_path` parameter
- `ssh_upload_file`: `remote_path` parameter (destination)
- `ssh_download_file`: `remote_path` parameter (source)
- `ssh_exec`: the `cwd` parameter (NOT the command itself — restricting cwd, not what commands can run)

**Config addition to `HostConfig`:**

```go
type HostConfig struct {
    Socket        string   `json:"socket"`
    User          string   `json:"user"`
    Host          string   `json:"host"`
    ExecAllowlist []string `json:"exec_allowlist,omitempty"`
    BaseDir       string   `json:"base_dir,omitempty"`
}
```

---

## Summary: New Dependencies

**None.** All three features are implementable with existing dependencies plus Go stdlib.

| Feature | Implementation | Dependency |
|---------|---------------|------------|
| Port forwarding | `ssh -O forward/cancel` via os/exec | stdlib os/exec (already used) |
| Forward registry | `sync.Mutex` + `map` | stdlib sync |
| Command allowlist | `strings.Fields` + `filepath.Base` + prefix/exact match | stdlib (already used) |
| base_dir restriction | `filepath.Clean` + `filepath.Rel` + `strings.HasPrefix` | stdlib (already used) |

The only library worth watching is `golang.org/x/crypto/ssh v0.52.0` for future milestones — `NewControlClientConn` is now available and should be flagged as the path for v2.2+ remote forwarding or socks5 work.

---

## go.mod — No Changes Required for v2.1

Current go.mod is sufficient. Do not add `golang.org/x/crypto` as a direct dependency for this milestone.

---

## Sources

- OpenSSH man page (`man ssh`) — `-O forward`, `-O cancel`, `-L` syntax
- [OpenSSH/Cookbook/Multiplexing (Wikibooks)](https://en.wikibooks.org/wiki/OpenSSH/Cookbook/Multiplexing) — `-O forward` without new session, no list mechanism
- [golang/crypto control.go commit (2026-05-22)](https://github.com/golang/crypto/blob/master/ssh/control.go) — `NewControlClientConn` merged, verified from git log
- [golang/crypto v0.52.0 tag (2026-05-22)](https://api.github.com/repos/golang/crypto/git/refs/tags/v0.52.0) — confirmed release date
- [Traversal-resistant file APIs — Go Blog](https://go.dev/blog/osroot) — `os.Root` Go 1.24, local filesystem only
- [Preventing path traversal in Go](https://dzx.cz/2021-04-02/go_path_traversal/) — `filepath.Rel` pattern
- [Go proposal #32958 — x/crypto/ssh: add NewControlClientConn](https://github.com/golang/go/issues/32958) — history of the proposal
- [Go os/exec process groups (Medium)](https://medium.com/@felixge/killing-a-child-process-and-all-of-its-children-in-go-54079af94773) — Setpgid + process group kill
