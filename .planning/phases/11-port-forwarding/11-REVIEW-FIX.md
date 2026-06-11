---
phase: 11-port-forwarding
fixed_at: 2026-06-11T20:51:00Z
review_path: .planning/phases/11-port-forwarding/11-REVIEW.md
iteration: 1
findings_in_scope: 5
fixed: 5
skipped: 0
status: all_fixed
---

# Phase 11: Code Review Fix Report

**Fixed at:** 2026-06-11
**Source review:** .planning/phases/11-port-forwarding/11-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 5 (2 Critical, 3 Warning)
- Fixed: 5
- Skipped: 0

## Fixed Issues

### CR-01: Data race on `cmd.ProcessState`

**Files modified:** `internal/forward/forward.go`, `internal/forward/forward_test.go`
**Commit:** 9ffb860
**Applied fix:** Added `exited atomic.Bool` field to `ForwardEntry`. The reaper goroutine now calls `entry.exited.Store(true)` after `cmd.Wait()` returns. `Status()` and `KillAll()` read `entry.exited.Load()` instead of `cmd.ProcessState`, eliminating the data race. `StartForward` / `startForward` now return `*ForwardEntry` instead of `*exec.Cmd` so the goroutine and callers share the same instance. Added `HasExited(entry *ForwardEntry) bool` as a cross-package accessor. Updated `TestStatusRunningVsDead` to set the flag directly rather than calling `cmd.Wait()` manually.

### CR-02: No input validation on `remote_host` and `remote_port`

**Files modified:** `internal/tools/forward.go`
**Commit:** 4a34407
**Applied fix:** Added validation at the top of `forwardPortHandler` (after `resolveExecutor`, before `allocatePortFn`). Returns `IsError: true` with a descriptive message if `remote_host` is empty or `remote_port` is outside `[1, 65535]`. No resources are allocated when validation fails.

### WR-01: Failure-path hint fires after `Kill()` instead of before

**Files modified:** `internal/tools/forward.go`
**Commit:** 4a34407
**Applied fix:** Moved the `alreadyExited` check to before `cmd.Process.Kill()`. The check uses `forward.HasExited(entry)` (the atomic flag from CR-01). `Kill()` is non-blocking so checking after it races the reaper goroutine; checking before captures the self-exit state reliably.

### WR-02: `pollReady` sleeps 50ms after the final failed dial attempt

**Files modified:** `internal/forward/forward.go`
**Commit:** 9ffb860
**Applied fix:** Restructured the loop to `if i > 0 { time.Sleep(50ms) }` before each dial attempt. The sleep now only runs between attempts (iterations 1–9), not after the last failed attempt, reducing worst-case latency from ~550ms to ~500ms.

### WR-03: Dead `ForwardEntry` records accumulate unboundedly

**Files modified:** `internal/forward/forward.go`
**Commit:** 9ffb860
**Applied fix:** Added `Delete(key string)` method to `Forwarder`. Acquires the mutex and deletes the entry from the map. Callers (or a future background sweep) can now evict dead entries to prevent unbounded growth during long daemon uptime.

---

_Fixed: 2026-06-11_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
