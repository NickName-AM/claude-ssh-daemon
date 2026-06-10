---
phase: 11-port-forwarding
reviewed: 2026-06-10T00:00:00Z
depth: standard
files_reviewed: 9
files_reviewed_list:
  - internal/forward/forward.go
  - internal/forward/forward_linux.go
  - internal/forward/forward_other.go
  - internal/forward/forward_test.go
  - internal/tools/forward.go
  - internal/tools/forward_test.go
  - internal/tools/register.go
  - internal/tools/status_test.go
  - internal/daemon/daemon.go
findings:
  critical: 2
  warning: 3
  info: 2
  total: 7
status: issues_found
---

# Phase 11: Code Review Report

**Reviewed:** 2026-06-10
**Depth:** standard
**Files Reviewed:** 9
**Status:** issues_found

## Summary

Phase 11 adds the port-forward subsystem: `internal/forward` (registry, subprocess lifecycle, port allocation) and `internal/tools/forward.go` (MCP tool handlers), wired into the daemon. The overall design is sound — capability gating is correct, the TOCTOU on port allocation is explicitly accepted, and the "store only after pollReady succeeds" invariant is respected.

Two blocking defects are present: a data race on `cmd.ProcessState` that would be caught by Go's race detector, and missing input validation that allows an empty `remote_host` or out-of-range `remote_port` (including 0) to reach `exec.Command("ssh")` with a malformed `-L` spec. Three warnings cover a logic gap in the failure-path hint, an unwanted final-iteration sleep in `pollReady`, and unbounded accumulation of dead entries in the registry.

---

## Critical Issues

### CR-01: Data race on `cmd.ProcessState` — `Status()` and `KillAll()` read without synchronization

**File:** `internal/forward/forward.go:93` (also lines 84, 116 in `internal/tools/forward.go`)

**Issue:** `cmd.Wait()` writes `cmd.ProcessState` inside the background goroutine started at line 168. `Status()` (line 93) reads `cmd.ProcessState` without holding any lock and without any atomic or happens-before edge with the background goroutine. `KillAll()` (line 84) reads `cmd.ProcessState` inside `f.mu.Lock()` but `cmd.Wait()` does not hold `f.mu`, so there is no synchronization between the writer and either reader. Under the Go memory model both accesses are data races; `go test -race` would flag them.

Additionally, `forwardPortHandler` (line 116 of `internal/tools/forward.go`) reads `cmd.ProcessState` after `PollReady` fails and after `cmd.Process.Kill()` is called, again with no synchronization with the background `cmd.Wait()` goroutine. `Process.Kill()` is non-blocking — the child may not have exited yet, so `ProcessState` may still be `nil` when checked, making the hint unreliable even setting aside the race.

**Fix:** Introduce a small wrapper that signals process exit safely. The simplest approach is to capture exit in the reaper goroutine and expose it via a channel or `atomic.Bool`:

```go
// In ForwardEntry, add:
exited atomic.Bool  // set to true by the reaper goroutine after cmd.Wait() returns

// In startForward, replace the goroutine with:
go func() {
    _ = cmd.Wait()
    entry.exited.Store(true) // set after Wait populates ProcessState
}()

// Status() becomes:
func Status(entry *ForwardEntry) string {
    if entry.exited.Load() {
        return "dead"
    }
    return "running"
}

// KillAll() condition becomes:
if entry.Cmd.Process != nil && !entry.exited.Load() {
    _ = entry.Cmd.Process.Kill()
}
```

This requires that `ForwardEntry` is fully constructed before `exited` is first written, which is already true because `Store` is called after `startForward` returns. Alternatively, store the channel in `ForwardEntry` and close it in the reaper goroutine — `Status()` then does a non-blocking receive.

---

### CR-02: No input validation on `remote_host` and `remote_port` — malformed `-L` spec reaches `exec.Command`

**File:** `internal/tools/forward.go:96` (input accepted from `ForwardPortInput`, lines 21–22)

**Issue:** `ForwardPortInput.RemoteHost` and `RemotePort` are passed directly to `forward.StartForward` without any validation. Three classes of bad input reach `exec.Command("ssh")`:

1. **Empty `remote_host`**: produces `lSpec = "localPort::remotePort"` (double colon). OpenSSH may interpret this as `bind_address=empty, port=localPort, host=empty, hostport=remotePort`, which is invalid. ssh exits non-zero and `pollReady` fails, but the error returned to the caller gives no indication that the input itself was the problem.

2. **`remote_port` == 0** (the Go zero value when the JSON field is absent or `0`): produces `lSpec = "localPort:host:0"`. ssh rejects port 0 and fails silently from the caller's perspective.

3. **`remote_port` outside `[1, 65535]`**: negative values are also accepted by the `int` type. ssh's error message will be surfaced only after the 500 ms `pollReady` timeout expires, giving the caller a confusing "did not become reachable within 500ms" message rather than an immediate "invalid port" error.

Since `remote_host` and `remote_port` are required fields (per the comment on line 19), the tool handler should validate them immediately and return `IsError: true` before allocating a port or spawning a subprocess.

**Fix:** Add validation at the top of `forwardPortHandler` before `allocatePortFn()`:

```go
// Validate required fields before allocating resources.
if in.RemoteHost == "" {
    return &mcp.CallToolResult{
        IsError: true,
        Content: []mcp.Content{&mcp.TextContent{
            Text: "remote_host is required and must not be empty",
        }},
    }, ForwardPortOutput{}, nil
}
if in.RemotePort < 1 || in.RemotePort > 65535 {
    return &mcp.CallToolResult{
        IsError: true,
        Content: []mcp.Content{&mcp.TextContent{
            Text: fmt.Sprintf("remote_port must be in [1, 65535], got %d", in.RemotePort),
        }},
    }, ForwardPortOutput{}, nil
}
```

---

## Warnings

### WR-01: Failure-path hint is structurally unreliable even after fixing CR-01

**File:** `internal/tools/forward.go:115–124`

**Issue:** After `PollReady` returns an error, the handler calls `cmd.Process.Kill()` (non-blocking) and then immediately checks `cmd.ProcessState != nil` (or, post-CR-01-fix, `entry.exited.Load()`) to decide whether to append a ControlMaster socket hint. Because `Kill()` is non-blocking and the reaper goroutine needs OS scheduling time to run `cmd.Wait()`, the `exited` flag will often still be `false` at the time of the check. In practice, the hint fires only when ssh exited on its own _before_ `Kill()` was called — which is actually the correct semantic, but the code comment ("ssh process exited immediately") implies it is meant to cover the kill path as well.

If the intent is "hint whenever ssh is already dead at the time of the Kill call", the check should be done _before_ calling `Kill()`, not after:

```go
// Check before killing, while we can still observe a self-exit.
alreadyExited := entry.exited.Load() // post CR-01 fix
if cmd.Process != nil {
    _ = cmd.Process.Kill()
}
hint := ""
if alreadyExited {
    hint = fmt.Sprintf(" (ssh process exited immediately — ControlMaster socket %s may be dead)", h.Socket)
}
```

---

### WR-02: `pollReady` sleeps 50 ms after the final failed dial attempt

**File:** `internal/forward/forward.go:182–191`

**Issue:** The loop body always sleeps `50 ms` _after_ a failed dial, including on the 10th (final) iteration. When all 10 attempts fail, the function adds one extra unnecessary 50 ms sleep before returning the error — extending the worst-case latency to ~550 ms instead of the documented 500 ms budget.

```go
// Fix: sleep before retrying, not after the last attempt.
for i := 0; i < 10; i++ {
    if i > 0 {
        time.Sleep(50 * time.Millisecond)
    }
    conn, err := net.Dial("tcp", addr)
    if err == nil {
        conn.Close()
        return nil
    }
}
return fmt.Errorf("ssh forward on port %d did not become reachable within 500ms", localPort)
```

---

### WR-03: Dead `ForwardEntry` records accumulate unboundedly in the registry

**File:** `internal/forward/forward.go` (no `Delete` method exists); `internal/tools/forward.go:153–169`

**Issue:** When a forwarded ssh process dies (e.g., ControlMaster socket drops), its `ForwardEntry` remains in the registry indefinitely with `status: "dead"`. There is no eviction or cleanup mechanism. Because `AllocatePort` uses port-0 assignment, each new forward request gets a fresh port and thus a different key, so dead entries never block new forwards. However, `Snapshot()` and `listForwardsHandler` iterate all entries — dead entries accumulate silently over a long daemon uptime and will clutter `ssh_list_forwards` output.

A `Delete(key string)` method on `Forwarder` would allow callers (or a future cleanup pass) to remove entries whose `Status` is "dead". Alternatively, `listForwardsHandler` could filter dead entries and document that behavior. At minimum, the design decision should be explicit.

---

## Info

### IN-01: `ForwardListEntry` omits `StartedAt` field present in `ForwardEntry`

**File:** `internal/tools/forward.go:37–43` vs `internal/forward/forward.go:22`

**Issue:** `ForwardEntry.StartedAt` (type `time.Time`) is populated at `time.Now()` on line 134 of `internal/tools/forward.go`, but `ForwardListEntry` and the JSON response for `ssh_list_forwards` do not include it. Operators have no way to tell how long a forward has been running. If the field was intentionally excluded (e.g., to keep the schema minimal), add a comment explaining the decision. If it was an oversight, add `started_at string` (formatted RFC 3339) to `ForwardListEntry`.

---

### IN-02: `TestAllocatePort` does not assert that two successive calls return distinct ports

**File:** `internal/forward/forward_test.go:26–34`

**Issue:** `TestAllocatePort` calls `allocatePort()` twice and asserts each port is `> 0`, but does not assert `port1 != port2`. Two successive port-0 allocations returning the same port would indicate the OS's ephemeral port table is exhausted — a rare but observable failure mode. The assertion costs one line and documents the expected uniqueness property:

```go
require.NotEqual(t, port1, port2, "successive allocations should return distinct ports")
```

---

_Reviewed: 2026-06-10_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
