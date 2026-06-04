# Architecture Patterns

**Domain:** Port forwarding + access controls on an existing Go SSH daemon
**Researched:** 2026-06-04
**Confidence:** HIGH — based on direct codebase inspection of all affected files

---

## Current Architecture (what exists)

```
cmd/claude-ssh-daemon/main.go
  └─ daemon.Run(ctx, cfg)
       ├─ builds registry: map[string]ssh.SSHExecutor (one per host in cfg.Hosts)
       ├─ tools.RegisterTools(server, registry, cfg)
       └─ acceptLoop → MCP server over Unix socket

internal/
  config/      config.go         — Config, HostConfig, Capabilities, Safeguards structs
  ssh/         executor.go       — SSHExecutor interface + ControlMasterExecutor
  tools/       register.go       — RegisterTools (capability-gated wiring)
               exec.go           — execHandler
               file_read.go      — readFileHandler
               file_write.go     — writeFileHandler
               dir.go            — listDirHandler
               transfer.go       — uploadHandler, downloadHandler
               status.go         — statusHandler
               resolve.go        — resolveExecutor, sortedKeys (shared helpers)
               safeguards.go     — formatInjectionWarning, isDestructiveCommand
  guard/       guard.go          — injection detection
  daemon/      daemon.go         — Run, acceptLoop
               transport.go      — Unix socket IOTransport
               socket.go         — createSocket
```

Key structural facts for the new milestone:
- `cfg` is passed by pointer to every handler closure at registration time. Adding fields to `HostConfig` or `Safeguards` is immediately visible to all handlers with no wiring changes.
- `registry` is `map[string]ssh.SSHExecutor` — a plain map closed over in every handler. A second map `map[string]*ForwardRegistry` can be built the same way and passed alongside it.
- Guard middleware does not exist at the SDK level; all guards fire inline inside handler closures. The same pattern applies to allowlist and base_dir checks.
- `resolveExecutor` is the single host-routing chokepoint — the resolved `hostName` string is available in every handler right after the `resolveExecutor` call.

---

## Recommended Architecture for v2.1

### 1. Forward State: `internal/forward` package

Create `internal/forward/registry.go`. This is new code with no overlap with existing packages, so a new package is warranted.

```
internal/forward/
  registry.go   — ForwardEntry, Registry struct, Start/Kill/List methods
```

**ForwardEntry:**
```go
type ForwardEntry struct {
    ID         string         // unique ID, e.g. "<localPort>:<remoteHost>:<remotePort>"
    LocalPort  int
    RemoteHost string
    RemotePort int
    Cmd        *exec.Cmd      // retained to call Cmd.Process.Kill()
    StartedAt  time.Time
}
```

**Registry:**
```go
type Registry struct {
    mu      sync.Mutex
    entries map[string]*ForwardEntry  // keyed by ID
}

func (r *Registry) Start(socketPath, user, host string, entry ForwardEntry) error
func (r *Registry) Kill(id string) error
func (r *Registry) List() []ForwardEntry
```

`Start` builds and starts `ssh -S <socket> -N -L <localPort>:<remoteHost>:<remotePort> <user>@<host>` via `exec.Cmd`. It retains the `*exec.Cmd` in the entry. The `ssh -N` subprocess runs in the background — `cmd.Start()`, not `cmd.Run()`. The process stays alive until `Kill` calls `cmd.Process.Kill()`.

**Process lifecycle:**
- `cmd.Start()` — non-blocking, process group inherits daemon's group (fine for local daemon)
- `cmd.Process.Kill()` — sends SIGKILL to the ssh subprocess. No orphan risk: ssh -N with -S exits immediately when the ControlMaster socket vanishes anyway.
- No `cmd.Wait()` goroutine is needed at kill time; the process is already dead. However, a background `cmd.Wait()` goroutine per entry is good hygiene to reap the zombie and detect unexpected exits.
- Context cancellation: pass a `context.WithCancel` derived from the daemon's root context into each `cmd.Start()` call so all forwards are killed on daemon shutdown automatically.

**Why in-memory map, not file:**
- Forwards are ephemeral by design (PROJECT.md "out of scope: persistent state")
- ssh -L bindings do not survive process restarts anyway (the TCP listener lives inside the ssh process)
- Daemon restart = all forward state is gone; this is correct behavior

**Where the map lives:**
The Registry is constructed in `daemon.Run`, alongside the executor registry, then passed to `tools.RegisterTools`. No global state.

**Per-host forward isolation:**
Build `map[string]*forward.Registry` — one registry per named host. Keyed the same way as the executor registry. This matches the existing multi-host pattern exactly and prevents port conflicts across hosts from appearing as the same entry.

### 2. Config Schema Changes

**HostConfig additions (internal/config/config.go):**

```go
type HostConfig struct {
    Socket       string   `json:"socket"`
    User         string   `json:"user"`
    Host         string   `json:"host"`
    ExecAllowlist []string `json:"exec_allowlist,omitempty"`  // NEW: allowlist of command prefixes
    BaseDir      string   `json:"base_dir,omitempty"`        // NEW: sandbox directory
}
```

Both fields are optional (omitempty). Absent `exec_allowlist` means no restriction. Absent `base_dir` means no path restriction. This is additive and backward-compatible — existing configs parse without changes.

`Validate()` does not need to precompile the allowlist (prefix matching is a strings.HasPrefix loop, not regex). BaseDir should be validated at load time: if non-empty, it must be an absolute path. Add to Validate():

```go
for name, h := range c.Hosts {
    if h.BaseDir != "" && !filepath.IsAbs(h.BaseDir) {
        return fmt.Errorf("config: hosts[%q].base_dir must be absolute if set", name)
    }
}
```

**No changes needed to Capabilities or Safeguards structs** — allowlist and base_dir are host-scoped policies, not global capabilities. They do not need a capability toggle because their presence (non-empty value) is the toggle.

### 3. Allowlist: Where Prefix-Match Logic Lives

**Location: `internal/tools/safeguards.go`** — alongside the existing `isDestructiveCommand` helper.

Add a new helper:

```go
// checkAllowlist returns an error string if cmd is not permitted by the allowlist.
// Returns "" when the allowlist is empty (no restriction).
func checkAllowlist(allowlist []string, cmd string) string
```

The check fires inside `execHandler`, immediately after the `isDestructiveCommand` gate and before `resolveExecutor`. The resolved `HostConfig` for the named host provides the allowlist slice.

**Integration point in execHandler:**

```go
// Resolve host config (not executor yet — we need the allowlist before SSH I/O).
hostCfg, hostName := resolveHostConfig(cfg, in.Host)
if denied := checkAllowlist(hostCfg.ExecAllowlist, in.Command); denied != "" {
    return &mcp.CallToolResult{IsError: true, ...}, ExecOutput{}, nil
}
exec, errResult := registry[hostName]
```

Note: `resolveExecutor` currently does both config lookup and executor lookup. Split this: add a `resolveHostConfig` helper in `resolve.go` that returns `(config.HostConfig, string, *mcp.CallToolResult)`. `execHandler` calls `resolveHostConfig` first (for the allowlist), then looks up the executor from registry. All other handlers continue using `resolveExecutor` unchanged.

Alternatively, keep `resolveExecutor` as-is and do the allowlist check after it — the host name is available from `resolveExecutor`'s return value, so `cfg.Hosts[hostName]` gives the HostConfig. This is simpler: no new helper needed, and the allowlist check uses the same post-resolve pattern already used by SAFE-02.

**Prefix-match semantics:**
```go
func checkAllowlist(allowlist []string, cmd string) string {
    if len(allowlist) == 0 {
        return ""
    }
    for _, prefix := range allowlist {
        if strings.HasPrefix(cmd, prefix) {
            return ""
        }
    }
    return fmt.Sprintf("command blocked by exec_allowlist; allowed prefixes: %s",
        strings.Join(allowlist, ", "))
}
```

Trim leading whitespace from `cmd` before matching to prevent trivial bypass via leading space. Do NOT use `strings.Fields(cmd)[0]` — that would match only the command name, not the prefix. The prefix should match the full command string from position 0 so that `"git"` allows `git status` but not `git push` if the user sets `["git status", "git log"]` instead.

### 4. BaseDir: Where Path Validation Lives

**Location: `internal/tools/safeguards.go`** — add a helper alongside the existing guards.

```go
// checkBaseDir returns an error string if remotePath does not fall under baseDir.
// Returns "" when baseDir is empty (no restriction).
// Uses path.Clean to normalize before prefix comparison (prevents "../" bypass).
func checkBaseDir(baseDir, remotePath string) string
```

The critical correctness detail: `path.Clean` (not `filepath.Clean` — remote paths are POSIX) both the baseDir and remotePath before the prefix check, and ensure the prefix ends with `/` to prevent `/base-dir-extra` matching `/base-dir`:

```go
func checkBaseDir(baseDir, remotePath string) string {
    if baseDir == "" {
        return ""
    }
    clean := path.Clean(remotePath)
    base := path.Clean(baseDir)
    if clean == base || strings.HasPrefix(clean, base+"/") {
        return ""
    }
    return fmt.Sprintf("path %q is outside base_dir %q", remotePath, baseDir)
}
```

**Integration points (all in `internal/tools/`):**
- `readFileHandler`: check `in.Path` after `resolveExecutor`, before `DetectEncoding`
- `writeFileHandler`: check `in.Path` after `resolveExecutor`, before overwrite gate
- `listDirHandler`: check `in.Path` after `resolveExecutor`, before `ListDir`
- `uploadHandler`: check `in.RemotePath` after `resolveExecutor`, before overwrite gate
- `downloadHandler`: check `in.RemotePath` after `resolveExecutor`, before overwrite gate
- `execHandler`: when `in.Cwd` is non-empty, check `in.Cwd` (not `in.Command` — the cwd restriction sandboxes the working directory, not the command itself)

In each handler, the resolved `hostName` from `resolveExecutor` gives access to `cfg.Hosts[hostName].BaseDir`.

### 5. Port Forward Handlers: Where They Live and How They Share State

Create `internal/tools/forward.go` in the existing `tools` package — consistent with how exec, file_read, file_write, dir, transfer, and status each have their own file.

Three handlers:
- `portForwardHandler` — builds ssh -L command, calls `forward.Registry.Start`, returns ForwardOutput
- `killForwardHandler` — calls `forward.Registry.Kill(id)`, returns KillOutput
- `listForwardsHandler` — calls `forward.Registry.List()`, returns ListOutput

**Registration in `register.go`:**

```go
if cfg.Capabilities.PortForward {
    forwardRegistry := ... // passed in as new parameter
    mcp.AddTool(server, ..., portForwardHandler(registry, forwardRegistry, cfg))
    mcp.AddTool(server, ..., killForwardHandler(forwardRegistry, cfg))
    mcp.AddTool(server, ..., listForwardsHandler(forwardRegistry, cfg))
}
```

`RegisterTools` signature becomes:
```go
func RegisterTools(server *mcp.Server, registry map[string]ssh.SSHExecutor, forwardRegistry map[string]*forward.Registry, cfg *config.Config)
```

This mirrors the existing pattern: `daemon.Run` builds both maps, passes both to `RegisterTools`. The `forward.Registry` map is keyed by host name, same as the executor registry.

**How ssh_list_forwards sees live state:**
The `forwardRegistry` map is built once in `daemon.Run` and closed over in `listForwardsHandler`. Because it is a pointer map (values are `*forward.Registry`, not `forward.Registry`), mutations from `portForwardHandler` and `killForwardHandler` are visible to `listForwardsHandler` without any additional synchronization beyond the mutex inside `forward.Registry`. This is the same pattern the existing test suite relies on for the executor registry.

**Port allocation:** The handler accepts an explicit `local_port` parameter. If `local_port` is 0, allocate an ephemeral port by listening on `:0` and immediately closing it to discover the OS-assigned port before starting ssh -L on that port. (TOCTOU race is acceptable for a single-user personal daemon — same decision precedent as SAFE-01.)

### 6. `daemon.Run` Changes

```go
// After building executor registry:
fwdRegistry := make(map[string]*forward.Registry, len(cfg.Hosts))
for name := range cfg.Hosts {
    fwdRegistry[name] = forward.NewRegistry(ctx) // pass daemon ctx for auto-kill on shutdown
}
tools.RegisterTools(server, registry, fwdRegistry, cfg)
```

`forward.NewRegistry(ctx)` stores the context so that each `cmd.Start()` call inside `Start()` uses a context-derived child. When the daemon's context is cancelled (SIGTERM/SIGINT), all ssh -L subprocesses are killed before daemon.Run returns.

No other changes to `daemon.go` are needed.

### 7. No New Package for Allowlist or BaseDir

Both allowlist and base_dir are pure string-manipulation helpers with no state. They belong in `internal/tools/safeguards.go` alongside `isDestructiveCommand` and `formatInjectionWarning`. Extracting them to a new package would add import overhead and package-boundary overhead for what are ~10-line pure functions.

The only new package is `internal/forward` — justified because it owns subprocess state (ForwardEntry, cmd.Start/Kill) that is distinct from the stateless SSH operations in `internal/ssh`.

---

## Component Boundaries After v2.1

| Component | Responsibility | Changed? |
|-----------|---------------|----------|
| `internal/config` | Config structs, Validate, JSON loading | ADD: ExecAllowlist, BaseDir to HostConfig |
| `internal/ssh` | SSHExecutor interface, ControlMasterExecutor | No change |
| `internal/forward` | ForwardEntry, Registry (Start/Kill/List), subprocess lifecycle | NEW |
| `internal/tools` | MCP tool handlers, safeguard helpers, resolveExecutor | ADD: 3 new handlers, 2 new helpers, RegisterTools signature change |
| `internal/daemon` | Run, acceptLoop, socket lifecycle | ADD: fwdRegistry construction, pass to RegisterTools |
| `internal/guard` | Injection detection | No change |

---

## Data Flow Changes

**Port forward (ssh_port_forward):**
```
Claude → MCP → portForwardHandler
  → resolveExecutor(registry, cfg, host) → hostName
  → fwdRegistry[hostName].Start(hostCfg.Socket, user, host, entry)
    → exec.Cmd{ssh -S socket -N -L port:remoteHost:remotePort user@host}
    → cmd.Start()
  → return ForwardOutput{ID, LocalPort, ...}
```

**Kill forward (ssh_kill_forward):**
```
Claude → MCP → killForwardHandler
  → resolveHostName (hostParam or default)
  → fwdRegistry[hostName].Kill(id)
    → cmd.Process.Kill()
    → delete entry from map
  → return KillOutput{Killed: true}
```

**List forwards (ssh_list_forwards):**
```
Claude → MCP → listForwardsHandler
  → optional host filter (omit = list all hosts)
  → fwdRegistry[hostName].List() → []ForwardEntry
  → return ListOutput{Forwards: [...]}
```

**Allowlist check in ssh_exec:**
```
Claude → MCP → execHandler
  → SAFE-02: isDestructiveCommand gate (existing)
  → resolveExecutor → exec, hostName
  → checkAllowlist(cfg.Hosts[hostName].ExecAllowlist, in.Command) → gate
  → exec.RunCommand(...)
```

**BaseDir check in ssh_read_file (and all file ops):**
```
Claude → MCP → readFileHandler
  → resolveExecutor → exec, hostName
  → checkBaseDir(cfg.Hosts[hostName].BaseDir, in.Path) → gate
  → exec.DetectEncoding / ReadFile
```

---

## Build Order and Dependencies

**These three features have no implementation dependencies on each other** and can be built in parallel. They share only config struct additions.

### Recommended build order:

**Step 1 (prerequisite, no parallelism available): Config schema additions**
- Add `ExecAllowlist []string` and `BaseDir string` to `HostConfig`
- Add BaseDir absolute-path validation to `Validate()`
- Update config tests
- This unblocks Steps 2, 3, and 4 in parallel

**Step 2 (parallel with 3 and 4): Allowlist**
- Add `checkAllowlist` to `internal/tools/safeguards.go`
- Add allowlist gate to `execHandler` (after `resolveExecutor`, before `RunCommand`)
- Add allowlist tests to `exec_test.go` or `safeguards_test.go`
- No new files, no new packages

**Step 3 (parallel with 2 and 4): BaseDir**
- Add `checkBaseDir` to `internal/tools/safeguards.go`
- Add baseDir gate to each of: `readFileHandler`, `writeFileHandler`, `listDirHandler`, `uploadHandler`, `downloadHandler`
- Add cwd gate to `execHandler` (separate from allowlist gate — they check different things)
- Add baseDir tests to each handler's test file, plus `safeguards_test.go`
- No new files, no new packages

**Step 4 (parallel with 2 and 3, but most isolated): Port forwarding**
- Create `internal/forward/registry.go` (ForwardEntry, Registry, Start/Kill/List)
- Create `internal/forward/registry_test.go`
- Create `internal/tools/forward.go` (portForwardHandler, killForwardHandler, listForwardsHandler)
- Create `internal/tools/forward_test.go`
- Update `RegisterTools` signature in `register.go`
- Update `daemon.Run` to build fwdRegistry
- Capability toggle `port_forward` already exists in `Capabilities` struct and `logCapabilities` — it just needs the handler wiring

**Step 5 (integration): Wire and test together**
- Integration test: allowlist + baseDir can both apply to same host simultaneously
- Verify no interaction between forward registry and executor registry
- Verify daemon shutdown kills all ssh -L subprocesses

---

## Anti-Patterns to Avoid

### Do Not Store Forward State in the Config Struct

`cfg *Config` is shared read-only across all handlers. Adding mutable forward state to it would require a mutex around the entire config. Keep forward state in `forward.Registry`, passed separately.

### Do Not Add a Middleware Layer for Allowlist/BaseDir

go-sdk has no middleware intercept point (established in PROJECT.md Key Decisions). The per-handler inline gate pattern is the correct approach here — it is auditable and already established by SAFE-01 and SAFE-02.

### Do Not Use `filepath.Clean` for Remote Path Normalization

`filepath.Clean` uses the local OS separator. Remote paths are always POSIX. Use `path.Clean` (from `path`, not `path/filepath`) in `checkBaseDir`.

### Do Not Use `cmd.Run()` for Port Forwards

`cmd.Run()` blocks until the process exits. ssh -L with -N runs until killed. Use `cmd.Start()` and retain the `*exec.Cmd` for later `Process.Kill()`.

### Do Not Use a Global Forward Registry

A global var would make testing harder and would conflict with the existing pattern of dependency injection through handler closures. The `map[string]*forward.Registry` passed to `RegisterTools` follows the exact same construction pattern as `map[string]ssh.SSHExecutor`.

### Do Not Validate ExecAllowlist as Regex

The spec says prefix-match, not regex. Prefix-match is simpler, cannot panic on malformed patterns, and is what operators expect ("allow anything starting with 'git'"). Regex here would be unexpected behavior.

---

## Testing Approach

**Allowlist:** Unit tests in `safeguards_test.go` for `checkAllowlist`. Integration tests in `exec_test.go` using mock executor with populated `cfg.Hosts[name].ExecAllowlist`.

**BaseDir:** Unit tests in `safeguards_test.go` for `checkBaseDir` (table-driven, covering path traversal attempts). Integration tests per handler.

**Forward registry:** Unit tests in `internal/forward/registry_test.go` using mock ssh command (substitute a no-op binary or `echo` to avoid real SSH dependency). Test `Start`, `Kill`, `List`, and concurrent access under the mutex.

**Forward handlers:** `internal/tools/forward_test.go` using `mcp.NewInMemoryTransports()` (established pattern from existing test suite) with a mock forward registry interface.

---

## Sources

- Direct codebase inspection: `internal/config/config.go`, `internal/ssh/executor.go`, `internal/tools/{register,exec,resolve,safeguards,file_read,transfer,status}.go`, `internal/daemon/daemon.go`
- PROJECT.md: architectural constraints, key decisions, current state
- CLAUDE.md: stack decisions, testing strategy, go-sdk InMemoryTransport availability
- Confidence: HIGH — all findings are from direct code inspection, not external sources
