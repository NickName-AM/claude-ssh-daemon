# Roadmap: claude-ssh-daemon

## Milestones

- ✅ **v1.0 MVP** - Phases 1-3 (shipped)
- ✅ **v1.1 Prompt Injection Safeguards** - Phases 4-6 (shipped 2026-06-03)
- ✅ **v2.0 Multi-Host** - Phase 7 (shipped)
- 🚧 **v2.1 Tunneling & Access Controls** - Phases 8-11 (in progress)

## Phases

<details>
<summary>✅ v1.0 MVP + v1.1 Safeguards + v2.0 Multi-Host (Phases 1-7) - SHIPPED</summary>

Phases 1-7 are complete. See `.planning/milestones/` for archived plans and execution history.

</details>

### 🚧 v2.1 Tunneling & Access Controls (In Progress)

**Milestone Goal:** Add local port forwarding and per-host access controls so Claude can reach remote services via tunnels and operators can lock down what Claude is allowed to execute or touch.

## Phase Details

### Phase 8: Config Schema
**Goal**: HostConfig carries the new per-host fields that all three v2.1 features depend on
**Depends on**: Phase 7
**Requirements**: ALWL-01, ALWL-04, BDIR-04
**Success Criteria** (what must be TRUE):
  1. `exec_allowlist` parses correctly in all three states: absent (nil pointer), explicit empty list, and populated list — nil and empty are distinct values after JSON decoding
  2. `base_dir` is rejected at daemon startup with a clear error message when set to a non-absolute path
  3. `base_dir` is path-cleaned at config load time so downstream handlers receive a canonical value without trailing slashes
  4. All existing config files without the new fields continue to load without errors (backward compatibility)
**Plans**: TBD

### Phase 9: Command Allowlist
**Goal**: `ssh_exec` enforces per-host command prefix restrictions when `exec_allowlist` is configured
**Depends on**: Phase 8
**Requirements**: ALWL-02, ALWL-03
**Success Criteria** (what must be TRUE):
  1. When `exec_allowlist` is absent from a host config, `ssh_exec` runs any command — no change to existing behavior
  2. When `exec_allowlist` is an explicit empty list, `ssh_exec` rejects every command with `isError: true`
  3. When `exec_allowlist` contains prefixes, `ssh_exec` rejects commands that do not start with any listed prefix; the error message lists the configured prefixes so Claude can self-correct
  4. Two hosts with different allowlists each enforce only their own list independently
**Plans**: TBD

### Phase 10: BaseDir Path Sandbox
**Goal**: File operations and exec `cwd` on hosts with `base_dir` set are confined to that directory tree
**Depends on**: Phase 8
**Requirements**: BDIR-01, BDIR-02, BDIR-03
**Success Criteria** (what must be TRUE):
  1. `ssh_read_file`, `ssh_write_file`, `ssh_list_dir`, `ssh_upload_file`, and `ssh_download_file` return `isError: true` for any path that lexically resolves outside `base_dir` (including traversal sequences like `../`)
  2. `ssh_exec` rejects requests where the `cwd` param resolves outside `base_dir` with `isError: true`
  3. When `base_dir` is absent or empty on a host, all file and exec operations behave identically to before — no restriction applied
  4. The symlink-blind-spot limitation is documented in the tool description (consistent with SAFE-01 precedent)
**Plans**: TBD

### Phase 11: Port Forwarding
**Goal**: Claude can create, list, and tear down local port forwards to reach remote services, with clean process lifecycle
**Depends on**: Phase 8
**Requirements**: FWRD-01, FWRD-02, FWRD-03, FWRD-04, FWRD-05, FWRD-06, FWRD-07
**Success Criteria** (what must be TRUE):
  1. `ssh_port_forward` with `local_port: 0` returns the OS-assigned port in the response and the tunnel is immediately connectable
  2. `ssh_port_forward` returns `isError: true` naming the owning host when the requested local port is already bound by any active forward
  3. `ssh_kill_forward` succeeds (no `isError`) even when the target forward does not exist, returning `not_found: true` in the response
  4. `ssh_list_forwards` lists all active forwards with `local_port`, `remote_host`, `remote_port`, `host`, and `duration_seconds`; an optional `host` param filters to one host
  5. When the `port_forward` capability is disabled in config, none of the three forward tools are registered with the MCP server
  6. All active forwards are killed during daemon graceful shutdown — no orphaned `ssh` processes persist after daemon exit
**Plans**: TBD

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1-7. (Prior milestones) | v1.0–v2.0 | — | Complete | 2026-06-03 |
| 8. Config Schema | v2.1 | 0/? | Not started | - |
| 9. Command Allowlist | v2.1 | 0/? | Not started | - |
| 10. BaseDir Path Sandbox | v2.1 | 0/? | Not started | - |
| 11. Port Forwarding | v2.1 | 0/? | Not started | - |
