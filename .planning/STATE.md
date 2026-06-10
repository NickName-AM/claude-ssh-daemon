---
gsd_state_version: 1.0
milestone: v2.1
milestone_name: Tunneling & Access Controls
status: ready_to_plan
stopped_at: Phase 10 complete (3/3) — ready to discuss Phase 11
last_updated: 2026-06-10T11:42:17.117Z
last_activity: 2026-06-10 -- Phase 10 execution started
progress:
  total_phases: 4
  completed_phases: 1
  total_plans: 5
  completed_plans: 4
  percent: 25
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-06-10)

**Core value:** Claude can execute remote commands through a persistent SSH tunnel via native MCP tools, without managing SSH connection lifecycle or credentials itself.
**Current focus:** Phase 11 — port forwarding

## Current Position

Phase: 11
Plan: Not started
Status: Ready to plan
Last activity: 2026-06-10

Progress: [████████████████░░░░] 4/5 plans (80%)

## Performance Metrics

**Velocity:**

- Total plans completed: 7 (this milestone)
- Average duration: —
- Total execution time: —

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 08 | 1 | - | - |
| 09 | 0 | - | - |
| 10 | 3 | - | - |

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

None yet.

### Blockers/Concerns

- Platform divergence in Phase 11: `Pdeathsig` is Linux-only; macOS needs `Setpgid` + explicit signal. Verify exact `SysProcAttr` spelling at plan time.
- Port-0 TOCTOU in Phase 11: `net.Listen(":0")` then close then `ssh -L` creates a race — accepted (SAFE-01 precedent), document in code.

## Session Continuity

Last session: 2026-06-10T17:45:00Z
Stopped at: Phase 10 complete (UAT 7/7 passed), ready to plan Phase 11
Resume file: None
