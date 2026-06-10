---
gsd_state_version: 1.0
milestone: v2.1
milestone_name: Tunneling & Access Controls
status: milestone_complete
stopped_at: Milestone complete (Phase 11 was final phase)
last_updated: 2026-06-10T16:44:07.290Z
last_activity: 2026-06-10 -- Phase 11 execution started
progress:
  total_phases: 4
  completed_phases: 2
  total_plans: 7
  completed_plans: 6
  percent: 50
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-06-10)

**Core value:** Claude can execute remote commands through a persistent SSH tunnel via native MCP tools, without managing SSH connection lifecycle or credentials itself.
**Current focus:** Milestone complete

## Current Position

Phase: 11
Plan: Not started
Status: Milestone complete
Last activity: 2026-06-10

Progress: [████████████████████░░░░░] 3/4 phases complete

## Performance Metrics

**Velocity:**

- Total plans completed: 7 (this milestone)
- Average duration: —
- Total execution time: —

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 08 | 1 | - | - |
| 09 | 1 | - | - |
| 10 | 3 | - | - |
| 11 | 2 | - | - |

*Updated after each plan completion*

## Accumulated Context

### Decisions

- Config: Use `*[]string` pointer for `exec_allowlist` to distinguish nil (allow-all) from empty slice (deny-all)
- Config: Use `path.Clean` (not `filepath.Clean`) for remote paths — POSIX, not OS-native
- Config: Clean and validate `base_dir` at startup (must be absolute if set)
- Sandbox: `withinBaseDir` is lexical-only (path.Clean + trailing-slash prefix); symlink blind spot accepted and documented in all six tool descriptions
- Sandbox: `ssh_exec` requires explicit `cwd` when `base_dir` is set (D-01); guards fire before any SSH I/O including `test -e` overwrite checks (D-07)
- Port forwarding: Use `ssh -L -S -N` via `os/exec` (not `-O forward`) — needed for list/liveness tracking
- Port forwarding: In-process `net.Listen` for port binding; port-readiness poll after `cmd.Start()`
- Port forwarding: `Setpgid` (macOS) / `Pdeathsig` (Linux) to prevent orphaned processes

### Pending Todos

None.

### Blockers/Concerns

- Platform divergence in Phase 11: `Pdeathsig` is Linux-only; macOS needs `Setpgid` + explicit signal. Verify exact `SysProcAttr` spelling at plan time.
- Port-0 TOCTOU in Phase 11: `net.Listen(":0")` then close then `ssh -L` creates a race — accepted (SAFE-01 precedent), document in code.

## Session Continuity

Last session: 2026-06-10T15:55:37.136Z
Stopped at: Phase 11 context gathered
Resume file: .planning/phases/11-port-forwarding/11-CONTEXT.md
