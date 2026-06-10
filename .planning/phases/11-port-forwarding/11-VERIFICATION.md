---
phase: 11-port-forwarding
verified: 2026-06-10T16:38:46Z
status: human_needed
score: 9/9 must-haves verified
overrides_applied: 0
human_verification:
  - test: "With PortForward=true in config, confirm ssh_forward_port and ssh_list_forwards appear in Claude Code's tool list"
    expected: "Both tools visible; ssh_forward_port has destructiveHint, ssh_list_forwards is read-only"
    why_human: "Tool registration conditional behavior is tested by existing unit tests, but actual MCP tools/list response over the live Unix socket cannot be verified without a running daemon"
  - test: "With PortForward=false in config, confirm neither ssh_forward_port nor ssh_list_forwards appear in Claude Code's tool list"
    expected: "Neither tool present in tools/list response"
    why_human: "Negative capability-gate check cannot be exercised programmatically without a running daemon instance"
  - test: "Send SIGTERM to a running daemon that has active port forwards; confirm ssh processes exit"
    expected: "KillAll() sends SIGKILL to child processes before ln.Close(); no orphaned ssh -L processes remain after daemon exit"
    why_human: "Shutdown signal sequence requires live daemon + live ssh subprocesses; cannot simulate with unit tests alone"
---

# Phase 11: Port Forwarding Verification Report

**Phase Goal:** Add `ssh_forward_port` and `ssh_list_forwards` MCP tools that create and enumerate local SSH port forwards via `ssh -L -S -N` subprocesses, with in-process liveness tracking and clean shutdown
**Verified:** 2026-06-10T16:38:46Z
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | A Forwarder can register, store, and enumerate active SSH port-forward entries keyed by hostName:localPort | VERIFIED | `internal/forward/forward.go` — Has/Store/Snapshot/Key all present with mutex protection; TestStoreAndHas passes |
| 2 | allocatePort returns a free ephemeral TCP port on 127.0.0.1 without panicking | VERIFIED | `net.Listen("tcp", "127.0.0.1:0")` at line 115 of forward.go; TCPAddr type assertion at line 120; TestAllocatePort passes |
| 3 | A duplicate key store is rejected so the same host:localPort cannot be registered twice | VERIFIED | `forwardPortHandler` step 4: `fwd.Has(key)` check returns IsError true before StartForward; TestForwardPortDuplicate passes with pre-seeded Forwarder |
| 4 | KillAll signals every live entry's process and does not panic on already-exited or nil processes | VERIFIED | `KillAll()` at line 80–88: guards `entry.Cmd.Process != nil && entry.Cmd.ProcessState == nil`; TestKillAllNilSafe passes |
| 5 | When PortForward capability is enabled, ssh_forward_port and ssh_list_forwards are registered MCP tools | VERIFIED | `register.go` line 69: `if cfg.Capabilities.PortForward {` gates both `mcp.AddTool` calls for the two tools |
| 6 | ssh_forward_port creates a tracked ssh -L forward and returns local_port, remote_host, remote_port, host, status=running | VERIFIED | `forwardPortHandler` step 7 returns `ForwardPortOutput{..., Status: "running"}`; ForwardEntry stored via `fwd.Store(key, entry)` after pollReady |
| 7 | Calling ssh_forward_port twice for the same host+local port returns IsError true (duplicate) | VERIFIED | Step 4 of forwardPortHandler returns IsError true when `fwd.Has(key)` is true; TestForwardPortDuplicate verifies deterministically via allocatePortFn seam |
| 8 | ssh_list_forwards returns an empty JSON array (not null) when no forwards exist, and per-entry status running or dead | VERIFIED | `listForwardsHandler` uses `make([]ForwardListEntry, 0)` (line 158); `forward.Status(e)` returns "running" or "dead" per ProcessState; TestListForwardsEmptyReturnsArray asserts `"forwards":[]` in JSON |
| 9 | On daemon shutdown the Forwarder's KillAll runs before the listener closes | VERIFIED | `daemon.go` line 152: `fwd.KillAll()` at line 152 textually precedes `ln.Close()` at line 155; comment cites D-07, T-11-08 |

**Score:** 9/9 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/forward/forward.go` | Forwarder struct, ForwardEntry struct, NewForwarder, allocatePort, startForward, pollReady, KillAll, Add/Store/Snapshot helpers | VERIFIED | 192 lines; all required types and functions present; exports AllocatePort/StartForward/PollReady as thin shims |
| `internal/forward/forward_linux.go` | Linux-specific setSysProcAttr with Pdeathsig | VERIFIED | 24 lines; `//go:build linux`; `Pdeathsig: syscall.SIGTERM` |
| `internal/forward/forward_other.go` | Non-Linux setSysProcAttr with Setpgid | VERIFIED | 20 lines; `//go:build !linux`; `Setpgid: true` |
| `internal/forward/forward_test.go` | Unit tests for allocatePort, registry add/duplicate, KillAll nil-safety, Status | VERIFIED | All 6 required test functions present; `go test ./internal/forward/` passes |
| `internal/tools/forward.go` | forwardPortHandler, listForwardsHandler, ForwardPortInput/Output, ForwardListEntry | VERIFIED | 171 lines; all handler functions and types present; allocatePortFn seam declared at line 16 |
| `internal/tools/register.go` | RegisterTools extended with *forward.Forwarder; PortForward-gated tool registration | VERIFIED | Signature at line 22 includes `fwd *forward.Forwarder`; PortForward gate at line 69 |
| `internal/daemon/daemon.go` | Forwarder construction, passed to RegisterTools, KillAll in shutdown path | VERIFIED | `forward.NewForwarder()` at line 129; `tools.RegisterTools(server, registry, cfg, fwd)` at line 130; `fwd.KillAll()` at line 152 |
| `internal/tools/forward_test.go` | Handler tests: success shape, duplicate error, empty-list array, unknown-host error | VERIFIED | All 3 required test functions present; `go test ./internal/tools/` passes including all 3 new tests |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `internal/daemon/daemon.go` | `tools.RegisterTools` | passes fwd as 4th argument | WIRED | Line 130: `tools.RegisterTools(server, registry, cfg, fwd)` — exact pattern match |
| `internal/daemon/daemon.go` | `forward.Forwarder.KillAll` | shutdown path before ln.Close() | WIRED | `fwd.KillAll()` at line 152 precedes `ln.Close()` at line 155 |
| `internal/tools/register.go` | `forwardPortHandler` | PortForward-gated mcp.AddTool | WIRED | `cfg.Capabilities.PortForward` gate at line 69 wraps both tool registrations |
| `internal/tools/forward.go` | `forward.Key`/`fwd.Has`/`fwd.Store`/`fwd.Snapshot` | handler uses registry methods | WIRED | All four registry methods called in forwardPortHandler and listForwardsHandler |
| `allocatePortFn` seam | test override in `forward_test.go` | package-level var | WIRED | `allocatePortFn` declared at line 16; TestForwardPortDuplicate overrides it at line 44 with t.Cleanup restore |

### Data-Flow Trace (Level 4)

Not applicable for this phase. The handlers produce structured JSON output from in-memory state (`Forwarder` registry), not from a rendered data component backed by a database. All data flows through the mutex-protected in-memory registry whose read path (Snapshot) is directly unit-tested.

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| `go build ./...` succeeds | `go build ./...` | exit 0 | PASS |
| `go vet ./...` clean | `go vet ./...` | exit 0 | PASS |
| `go test ./internal/forward/ -count=1` passes | `go test ./internal/forward/` | 6 tests pass | PASS |
| `go test ./internal/tools/ -count=1` passes | `go test ./internal/tools/` | all tests pass incl. 3 new | PASS |
| `go test ./... -count=1` full suite | `go test ./...` | 6 packages, all pass | PASS |

### Probe Execution

No probes declared in PLAN files. No `scripts/*/tests/probe-*.sh` files found. Step skipped.

### Requirements Coverage

No requirement IDs declared in either PLAN's frontmatter (`requirements: []` in both 11-01-PLAN.md and 11-02-PLAN.md). Coverage is tracked through the must-have truths above, all of which are verified.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| None | — | — | — | — |

No TBD/FIXME/XXX debt markers found in any phase-modified file. No stub patterns (return nil, empty arrays as stubs, TODO handlers) found. The `allocatePortFn = forward.AllocatePort` assignment is a documented test seam, not a stub — it delegates to a real implementation.

**Notable deviation from plan (documented, benign):** Plan 11-01 specified `runtime.GOOS == "linux"` branching for SysProcAttr. The implementation uses build-constrained files (`forward_linux.go` / `forward_other.go`) instead, which is architecturally superior — the Go compiler evaluates struct literals at compile time regardless of runtime branch conditions, making the plan's approach a compile-time error on macOS. The actual outcome (Pdeathsig on Linux, Setpgid elsewhere, `go build` exits 0 on macOS) satisfies the plan's intent and acceptance criteria.

**TestFullSevenToolSurface not updated:** `transfer_test.go` still asserts exactly 7 tools when exec/file_read/file_write are enabled (PortForward=false in that test's config). This is correct behavior — the two new tools are PortForward-gated and the test does not enable PortForward. The existing test remains accurate. A test asserting the 9-tool surface with all 4 capabilities enabled was not added; this is a test coverage gap but does not affect runtime correctness.

### Human Verification Required

#### 1. Tools Visible in Live MCP Session (PortForward=true)

**Test:** Run the daemon with a config that has `capabilities.port_forward: true`. Connect with Claude Code and call `tools/list`.
**Expected:** `ssh_forward_port` (destructiveHint=true) and `ssh_list_forwards` (readOnlyHint=true) both appear in the tool list.
**Why human:** Live MCP session over Unix socket cannot be driven in a unit test without a running daemon process.

#### 2. Tools Absent When PortForward=false

**Test:** Run the daemon with a config that has `capabilities.port_forward: false` (or omitted). Call `tools/list`.
**Expected:** Neither `ssh_forward_port` nor `ssh_list_forwards` appear. The daemon emits a "tool not registered" log line for port_forward.
**Why human:** Negative capability-gate verification requires live daemon + MCP client.

#### 3. Clean Shutdown Kills Active Forwards

**Test:** With a working ControlMaster session, call `ssh_forward_port`. Verify the local port is reachable. Send SIGTERM to the daemon. Verify the port is no longer reachable and no orphaned `ssh` processes remain (`pgrep -f "ssh -L"`).
**Expected:** `fwd.KillAll()` terminates the subprocess; port unreachable within ~100ms of shutdown.
**Why human:** Requires a real ControlMaster socket, a live SSH host, and signal-based lifecycle testing — not expressible as a hermetic unit test.

### Gaps Summary

No gaps. All 9 must-have truths are verified in the codebase. All required artifacts exist and are substantively implemented. All key links are wired. The module builds, vets, and tests clean (`go test ./...` 6/6 packages passing). Three human verification items remain for live-daemon behavioral confirmation.

---

_Verified: 2026-06-10T16:38:46Z_
_Verifier: Claude (gsd-verifier)_
