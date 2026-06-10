# Milestone v2.1: Tunneling & Access Controls

**Status:** In Progress
**Phases:** 8–11
**Current Phase:** 11

## Overview

Access control layer and port-forwarding capability. Phases 8–10 added per-host config schema (`exec_allowlist`, `base_dir`), command-allowlist enforcement on `ssh_exec`, and a lexical base-directory path sandbox across all six file/exec/transfer tools. Phase 11 adds local port forwarding as a new MCP tool.

## Phases

### Phase 8: Config Schema

**Goal**: Extend `HostConfig` with `exec_allowlist` (`*[]string`) and `base_dir` (`string`) fields, validated at startup
**Depends on**: Phase 7
**Plans**: 1 plan
**Status**: ✅ Complete

Plans:

- [x] 08-01-PLAN.md — ExecAllowlist + BaseDir fields, Validate() path checks, table-driven tests

---

### Phase 9: Command Allowlist

**Goal**: Enforce `exec_allowlist` in `execHandler` — nil=allow-all, empty=deny-all, non-empty=prefix-match — with structured MCP error on reject
**Depends on**: Phase 8
**Plans**: 1 plan
**Status**: ✅ Complete

Plans:

- [x] 09-01-PLAN.md — allowlist guard in execHandler, per-host test coverage

---

### Phase 10: BaseDir Path Sandbox

**Goal**: Confine all six MCP file/exec/transfer tools to a per-host `base_dir` using a lexical containment predicate (`withinBaseDir`), with symlink-blind-spot documentation
**Depends on**: Phase 9
**Plans**: 3 plans
**Status**: ✅ Complete

Plans:

- [x] 10-01-PLAN.md — withinBaseDir predicate + sandbox.go, readFile + listDir guards
- [x] 10-02-PLAN.md — writeFile + upload + download guards, symlink docs
- [x] 10-03-PLAN.md — execHandler cwd enforcement (D-01 empty cwd, D-02 out-of-sandbox cwd)

---

### Phase 11: Port Forwarding

**Goal**: Add `ssh_forward_port` and `ssh_list_forwards` MCP tools that create and enumerate local SSH port forwards via `ssh -L -S -N` subprocesses, with in-process liveness tracking and clean shutdown
**Depends on**: Phase 10
**Plans**: 2 plans
**Status**: 🔲 Not started

Plans:
**Wave 1**

- [x] 11-01-PLAN.md — internal/forward package: Forwarder registry, allocatePort/startForward/pollReady/KillAll helpers, unit tests

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 11-02-PLAN.md — ssh_forward_port + ssh_list_forwards handlers, RegisterTools wiring, daemon shutdown KillAll, handler tests

Canonical refs:

- `.planning/phases/11-port-forwarding/` (phase directory)

---
