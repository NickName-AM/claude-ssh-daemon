# Requirements: v2.1 Tunneling & Access Controls

**Milestone:** v2.1 — Tunneling & Access Controls
**Core Value:** Claude can execute remote commands through a persistent SSH tunnel via native MCP tools, without managing SSH connection lifecycle or credentials itself.
**Last updated:** 2026-06-04

---

## v2.1 Requirements

### Port Forwarding (FWRD)

- [ ] **FWRD-01**: User can create a local port forward via `ssh_port_forward` (`local_port`, `remote_host`, `remote_port`, optional `host`); `local_port: 0` auto-assigns an OS-chosen port and the response includes the actual bound port
- [ ] **FWRD-02**: `ssh_port_forward` returns `isError: true` if `local_port` is already in use (by any host), naming the port and the host that owns it
- [ ] **FWRD-03**: `ssh_port_forward` returns `isError: true` if the ControlMaster socket for the target host is dead, reusing the existing `CheckSocket` error format
- [ ] **FWRD-04**: User can tear down an active forward via `ssh_kill_forward` (`local_port`, optional `host`); returns success even if the forward is already gone (idempotent — `not_found: true` in response, no `isError`)
- [ ] **FWRD-05**: User can list all active forwards via `ssh_list_forwards` (optional `host` filter); each entry includes `local_port`, `remote_host`, `remote_port`, `host`, `duration_seconds`
- [ ] **FWRD-06**: Port forwarding respects the `port_forward` capability toggle; when disabled, forward tools are not registered with the MCP server
- [ ] **FWRD-07**: Active forwards are killed during daemon graceful shutdown; no orphaned `ssh` processes persist after daemon exit

### Command Allowlist (ALWL)

- [ ] **ALWL-01**: When `hosts[].exec_allowlist` is absent from config, `ssh_exec` allows all commands — backward-compatible default, no behavior change for existing configs
- [ ] **ALWL-02**: When `exec_allowlist` is an explicit empty list (`[]`), `ssh_exec` rejects all commands with `isError: true`
- [ ] **ALWL-03**: When `exec_allowlist` is a non-empty list, `ssh_exec` rejects any command that does not have a full-string prefix match against any entry; error message includes the list of configured prefixes
- [ ] **ALWL-04**: `exec_allowlist` is per-host; different hosts can have independent lists or no list

### Base Dir Restriction (BDIR)

- [x] **BDIR-01**: When `hosts[].base_dir` is set, `ssh_read_file`, `ssh_write_file`, `ssh_list_dir`, `ssh_upload_file`, `ssh_download_file` return `isError: true` for any path that resolves outside `base_dir` via lexical `path.Clean` + trailing-slash prefix check
- [x] **BDIR-02**: When `base_dir` is set, `ssh_exec` rejects requests where `cwd` resolves outside `base_dir`
- [x] **BDIR-03**: Path validation is lexical (`path.Clean` + `strings.HasPrefix` with trailing-slash normalization); remote symlinks are not resolved — limitation documented in tool description, consistent with SAFE-01 precedent
- [ ] **BDIR-04**: `base_dir` is per-host; absent or empty means no restriction; a non-empty value must be an absolute path, validated and cleaned at daemon startup

---

## Future Requirements (Deferred)

- Remote port forwarding (`ssh -R`) — no identified Claude use case yet
- Dynamic / SOCKS5 forwarding (`ssh -D`) — different lifecycle, deferred
- Persistent forwards across daemon restart — `ssh_port_forward` is ephemeral by design in v2.1
- Wildcard or regex allowlist entries — prefix-match covers identified use cases
- Per-host `allow_forward` capability toggle — global `port_forward` capability is sufficient for v2.1
- `golang.org/x/crypto/ssh` native channel API — `NewControlClientConn` is now in v0.52.0 but deferred to v2.2+

---

## Out of Scope

- `base_dir` applied to exec command strings — that is the allowlist's responsibility
- `base_dir` applied to stdout/stderr content — that is the guard's responsibility
- Bandwidth or connection-count stats in forward listing — not needed
- Allowlist enforcement against shell wrappers (`bash -c 'rm ...'`) — document as known limitation; operators needing hard enforcement should use `ForceCommand` in remote `sshd_config`

---

## Traceability

| REQ-ID | Phase | Status |
|--------|-------|--------|
| ALWL-01 | Phase 8 | Pending |
| ALWL-04 | Phase 8 | Pending |
| BDIR-04 | Phase 8 | Pending |
| ALWL-02 | Phase 9 | Pending |
| ALWL-03 | Phase 9 | Pending |
| BDIR-01 | Phase 10 | Complete |
| BDIR-02 | Phase 10 | Complete |
| BDIR-03 | Phase 10 | Complete |
| FWRD-01 | Phase 11 | Pending |
| FWRD-02 | Phase 11 | Pending |
| FWRD-03 | Phase 11 | Pending |
| FWRD-04 | Phase 11 | Pending |
| FWRD-05 | Phase 11 | Pending |
| FWRD-06 | Phase 11 | Pending |
| FWRD-07 | Phase 11 | Pending |
