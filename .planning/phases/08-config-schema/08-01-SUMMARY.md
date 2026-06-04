---
phase: 08-config-schema
plan: 01
subsystem: config
tags: [go, json, config, ssh, access-control]

# Dependency graph
requires: []
provides:
  - HostConfig.ExecAllowlist *[]string field with three-state nil/empty/populated semantics
  - HostConfig.BaseDir string field with absolute-path validation and path.Clean normalisation
  - Validate() base_dir rejection (non-absolute) and cleaning (trailing/double slashes)
affects: [09-allowlist-enforcement, 10-base-dir-sandbox]

# Tech tracking
tech-stack:
  added: [path (stdlib, POSIX path utilities)]
  patterns:
    - "*[]string pointer-to-slice for three-state optional JSON list fields"
    - "Map value write-back pattern for mutating HostConfig values in c.Hosts"
    - "path.IsAbs + path.Clean for per-host POSIX path validation at Validate() time"

key-files:
  created: []
  modified:
    - internal/config/config.go
    - internal/config/config_test.go

key-decisions:
  - "ExecAllowlist uses *[]string to distinguish nil (absent, allow-all) from non-nil empty (deny-all) after JSON decoding"
  - "base_dir validation runs inside the existing sorted-keys per-host loop in Validate() for deterministic error order"
  - "path (POSIX) imported alongside path/filepath (OS-native) — both coexist, path for remote, filepath for local config path"

patterns-established:
  - "Three-state optional list: use *[]string with omitempty; test all three states via JSON round-trip (not struct literals)"
  - "Per-host struct mutation: copy to local var, mutate, write c.Hosts[name] = h — compiler does not catch missing write-back"

requirements-completed: [ALWL-01, ALWL-04, BDIR-04]

# Metrics
duration: 15min
completed: 2026-06-04
---

# Phase 8 Plan 01: Config Schema Summary

**HostConfig extended with ExecAllowlist *[]string (three-state allowlist) and BaseDir string (absolute-path validated, path.Clean normalised) as schema foundation for v2.1 access controls**

## Performance

- **Duration:** ~15 min
- **Started:** 2026-06-04T20:28:00Z
- **Completed:** 2026-06-04T20:43:05Z
- **Tasks:** 2
- **Files modified:** 2

## Accomplishments

- Extended HostConfig with ExecAllowlist *[]string — nil pointer signals allow-all (absent key), non-nil empty signals deny-all (explicit []), non-nil populated signals prefix list (ALWL-01)
- Extended HostConfig with BaseDir string — Validate() rejects non-absolute paths and path.Clean-normalises valid paths in place with map write-back (BDIR-04)
- Added "path" (POSIX) import alongside existing "path/filepath" (OS-native); both coexist correctly
- Added 4 new test functions covering all three allowlist states, per-host independence (ALWL-04), base_dir rejection/cleaning, and backward compatibility for legacy configs

## Task Commits

Each task was committed atomically:

1. **Task 1: Extend HostConfig and add base_dir validation in Validate()** - `b7311e8` (feat)
2. **Task 2: Add tests for allowlist three-state, per-host independence, base_dir validation, and backward compat** - `fd5c4f3` (test)

**Plan metadata:** (docs commit below)

## Files Created/Modified

- `internal/config/config.go` - Added "path" import, ExecAllowlist *[]string and BaseDir string fields to HostConfig, base_dir validation/cleaning in Validate() per-host loop
- `internal/config/config_test.go` - Added TestExecAllowlistThreeStates, TestExecAllowlistPerHostIndependence, TestBaseDirValidation, TestLegacyBackwardCompatNewFields (155 lines, 4 functions, 11 subtests)

## Decisions Made

- Used *[]string (pointer-to-slice) for ExecAllowlist as locked in STATE.md — only approach where JSON decoder distinguishes absent key (nil pointer) from explicit empty array (non-nil pointer to empty slice)
- base_dir validation placed inside the existing sorted-keys per-host loop in Validate(), consistent with existing socket/user/host checks; deterministic error ordering preserved
- path.IsAbs and path.Clean used from "path" package (POSIX), not "path/filepath" (OS-native) — per locked project decision that remote paths are always POSIX

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Phase 9 (allowlist enforcement): cfg.Hosts[name].ExecAllowlist is ready to consume — nil = allow-all, non-nil empty = deny-all, non-nil populated = prefix list
- Phase 10 (base_dir path sandbox): cfg.Hosts[name].BaseDir is ready to consume — always canonical (path.Clean applied), always absolute if non-empty, empty means no restriction
- go test ./... passes with no regressions across all 5 packages

---
*Phase: 08-config-schema*
*Completed: 2026-06-04*
