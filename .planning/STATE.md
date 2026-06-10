---
gsd_state_version: 1.0
milestone: v2.1
milestone_name: Tunneling & Access Controls
status: planning
stopped_at: Phase 10 context gathered
last_updated: "2026-06-10T08:02:09.336Z"
last_activity: 2026-06-05
progress:
  total_phases: 4
  completed_phases: 1
  total_plans: 2
  completed_plans: 1
  percent: 25
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-06-04)

**Core value:** Claude can execute remote commands through a persistent SSH tunnel via native MCP tools, without managing SSH connection lifecycle or credentials itself.
**Current focus:** Phase 10 — basedir path sandbox

## Current Position

Phase: 10
Plan: Not started
Status: Ready to plan
Last activity: 2026-06-05

Progress: [░░░░░░░░░░] 0%

## Performance Metrics

**Velocity:**

- Total plans completed: 1 (this milestone)
- Average duration: —
- Total execution time: —

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 08 | 1 | - | - |
| 09 | 0 | - | - |

*Updated after each plan completion*

## Accumulated Context

### Decisions

- Config: Use `*[]string` pointer for `exec_allowlist` to distinguish nil (allow-all) from empty slice (deny-all)
- Config: Use `path.Clean` (not `filepath.Clean`) for remote paths — POSIX, not OS-native
- Config: Clean and validate `base_dir` at startup (must be absolute if set)
- Port forwarding: Use `ssh -L -S -N` via `os/exec` (not `-O forward`) — needed for list/liveness tracking
- Port forwarding: In-process `net.Listen` for port binding; port-readiness poll after `cmd.Start()`
- Port forwarding: `Setpgid` (macOS) / `Pdeathsig` (Linux) to prevent orphaned processes

### Pending Todos

None yet.

### Blockers/Concerns

- Platform divergence in Phase 11: `Pdeathsig` is Linux-only; macOS needs `Setpgid` + explicit signal. Verify exact `SysProcAttr` spelling at plan time.
- Port-0 TOCTOU in Phase 11: `net.Listen(":0")` then close then `ssh -L` creates a race — accepted (SAFE-01 precedent), document in code.

## Session Continuity

Last session: 2026-06-10T08:02:09.326Z
Stopped at: Phase 10 context gathered
Resume file: .planning/phases/10-basedir-path-sandbox/10-CONTEXT.md
