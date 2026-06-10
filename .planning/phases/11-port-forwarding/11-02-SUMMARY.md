---
phase: 11-port-forwarding
plan: "02"
subsystem: mcp-tools
tags: [port-forward, mcp, tools, daemon]
dependency_graph:
  requires: ["11-01"]
  provides: ["ssh_forward_port tool", "ssh_list_forwards tool", "Forwarder lifecycle in daemon"]
  affects: ["internal/tools", "internal/daemon", "internal/forward"]
tech_stack:
  added: []
  patterns:
    - "Exported shim wrappers (AllocatePort/StartForward/PollReady) over unexported forward helpers for cross-package access"
    - "Package-level var test seam (allocatePortFn) for deterministic duplicate-check testing without live ssh"
    - "Store-after-ready pattern (Pitfall 3): ForwardEntry only added to registry after pollReady succeeds"
key_files:
  created:
    - internal/tools/forward.go
    - internal/tools/forward_test.go
  modified:
    - internal/forward/forward.go
    - internal/tools/register.go
    - internal/tools/status_test.go
    - internal/daemon/daemon.go
decisions:
  - "Exported AllocatePort/StartForward/PollReady as thin wrappers over unexported helpers in forward package -- cleaner than exposing internals, required because tools is a separate package"
  - "allocatePortFn test seam declared in tools package as var -- allows TestForwardPortDuplicate to deterministically reach D-02 branch without spawning a live ssh process"
  - "KillAll placed before ln.Close in daemon.Run shutdown path (D-07) -- ensures forward subprocesses receive SIGKILL before the listener stops accepting connections"
metrics:
  duration: "~15 minutes"
  completed: "2026-06-10"
  tasks_completed: 3
  files_changed: 6
---

# Phase 11 Plan 02: MCP Tool Layer for Port Forwarding Summary

One-liner: Wire ssh_forward_port and ssh_list_forwards MCP tools through the capability-gated RegisterTools pattern with hermetic test coverage and clean daemon shutdown.

## What Was Built

Three implementation tasks completed:

**Task 1 -- forwardPortHandler and listForwardsHandler (internal/tools/forward.go)**

- Exported three shims from `internal/forward`: `AllocatePort`, `StartForward`, `PollReady` — required because the tools package cannot reference unexported forward helpers.
- Declared `var allocatePortFn = forward.AllocatePort` as a package-level test seam so tests can stub port allocation for deterministic D-02 duplicate-check coverage.
- `forwardPortHandler` follows the resolveExecutor-first, IsError-boundary pattern from exec.go: resolveExecutor → allocatePortFn → duplicate check (D-02) → StartForward → PollReady with cleanup-on-failure → Store-after-ready → return D-09 shape with status "running".
- `listForwardsHandler` uses `make([]ForwardListEntry, 0)` (not `nil`) so JSON serialises to `[]` not `null` (Pitfall 7, T-11-09).

**Task 2 -- RegisterTools extension and daemon wiring**

- Extended `RegisterTools` with 4th arg `fwd *forward.Forwarder`; added PortForward-gated block registering `ssh_forward_port` (DestructiveHint) and `ssh_list_forwards` (ReadOnlyHint).
- Updated `status_test.go` `newTestServer` helper to pass `forward.NewForwarder()` so all existing tests compile without change (Pitfall 5).
- `daemon.Run` constructs `fwd := forward.NewForwarder()`, passes it to RegisterTools, and calls `fwd.KillAll()` immediately before `ln.Close()` in the shutdown path (D-07, T-11-08).

**Task 3 -- Handler tests (internal/tools/forward_test.go)**

Three hermetic tests, no live ssh or ControlMaster socket:
- `TestListForwardsEmptyReturnsArray`: asserts non-nil empty slice marshals to JSON `[]`
- `TestForwardPortDuplicate`: stubs allocatePortFn to fixed port 54321, pre-seeds Forwarder, asserts IsError true with port+host in error text; no subprocess started
- `TestForwardPortUnknownHost`: asserts IsError true on resolveExecutor miss

## Verification

- `go build ./...` -- clean
- `go vet ./...` -- clean
- `go test ./internal/tools/ -count=1` -- pass (3 new + all existing tests)
- `go test ./... -count=1` -- pass (all 6 packages)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing critical functionality] Exported forward helpers for cross-package access**
- **Found during:** Task 1
- **Issue:** `allocatePort`, `startForward`, `pollReady` were all unexported in `internal/forward`. The tools package is a separate package and cannot reference unexported symbols. The plan said "if allocatePort is unexported, declare `var allocatePortFn func() (int, error)` initialized to the forward-package helper" but this is not possible without an exported entry point.
- **Fix:** Added thin exported shim wrappers `AllocatePort()`, `StartForward()`, `PollReady()` in `internal/forward/forward.go`, each delegating to the unexported implementation. This keeps the unexported helpers as the canonical implementation while exposing the minimal cross-package surface.
- **Files modified:** `internal/forward/forward.go`
- **Commit:** 7b5b7d1

## Known Stubs

None -- all handlers are fully wired; no placeholder data or TODO paths.

## Threat Flags

None -- no new network endpoints, auth paths, or trust boundaries beyond what the plan's threat model already covers (T-11-05 through T-11-SC).

## Self-Check

### Files exist

- internal/tools/forward.go: FOUND
- internal/tools/forward_test.go: FOUND
- internal/forward/forward.go (modified): FOUND
- internal/tools/register.go (modified): FOUND
- internal/tools/status_test.go (modified): FOUND
- internal/daemon/daemon.go (modified): FOUND

### Commits exist

- 7b5b7d1: feat(mcp): add forwardPortHandler and listForwardsHandler -- FOUND
- c1553fe: feat(mcp): wire forward package into RegisterTools and daemon lifecycle -- FOUND
- 9b420e5: test(mcp): add hermetic handler tests for forward port tools -- FOUND

## Self-Check: PASSED
