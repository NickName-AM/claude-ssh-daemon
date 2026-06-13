---
gsd_state_version: 1.0
milestone: v2.1
milestone_name: Tunneling & Access Controls
status: archived
stopped_at: Milestone v2.1 archived 2026-06-13
last_updated: 2026-06-13T00:00:00.000Z
last_activity: 2026-06-13 -- v2.1 milestone archived
progress:
  total_phases: 4
  completed_phases: 4
  total_plans: 7
  completed_plans: 7
  percent: 100
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-06-13)

**Core value:** Claude can execute remote commands through a persistent SSH tunnel via native MCP tools, without managing SSH connection lifecycle or credentials itself.
**Current focus:** Planning next milestone — run /gsd-new-milestone

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

## Deferred Items

Items acknowledged and deferred at milestone close on 2026-06-13:

| Category | Item | Status |
|----------|------|--------|
| uat_gap | Phase 11: 11-HUMAN-UAT.md | verified (all 3 tests passed; SDK still reports as gap) |
| verification_gap | Phase 11: 11-VERIFICATION.md | human_needed resolved by UAT completion (fcc576d, 1e68742) |

Known deferred items at close: 2 (both resolved by UAT completion — SDK counts as open due to status field not being updated to "passed")

## Session Continuity

Last session: 2026-06-13T00:00:00Z
Stopped at: Milestone v2.1 complete
Resume file: n/a — start /gsd-new-milestone for v2.2
