# claude-ssh-daemon

## What This Is

A lightweight Go daemon that acts as a secure local intermediary for SSH connections. The daemon exposes an MCP (Model Context Protocol) server over a Unix socket, allowing Claude Code to run remote commands, read/write files, and access other SSH capabilities through a persistent SSH ControlMaster session that the user establishes and maintains. All tool responses are scanned for adversarial content injection; destructive operations are gated by default.

## Current Milestone: v2.1 Tunneling & Access Controls

**Goal:** Add local port forwarding and per-host access controls so Claude can reach remote services via tunnels and operators can lock down what Claude is allowed to execute or touch.

**Target features:**
- Port forwarding (local -L, ephemeral): `ssh_port_forward` binds local port → remote host:port; `ssh_kill_forward` tears it down; `ssh_list_forwards` lists active tunnels; all multi-host aware
- Command allowlist (per-host, prefix-match): `hosts[].exec_allowlist` list of allowed command prefixes; `ssh_exec` rejects commands not on the list when set
- Working dir restriction (per-host): `hosts[].base_dir` sandboxes file ops (read/write/list/upload/download) and exec cwd to a configured base directory

## Core Value

Claude can execute remote commands through a persistent SSH tunnel via native MCP tools, without managing SSH connection lifecycle or credentials itself.

## Requirements

### Validated

- ✓ All capabilities individually toggleable via JSON config — v1.0
- ✓ JSON config at `~/.config/claude-ssh-daemon/config.json` — v1.0
- ✓ Daemon exposes MCP server over a Unix socket for Claude Code integration — v1.0
- ✓ Daemon reuses a user-established SSH ControlMaster socket (no credential management) — v1.0
- ✓ Remote shell command execution via `ssh_exec` MCP tool — v1.0
- ✓ Remote file read/write/list/upload/download via MCP tools — v1.0
- ✓ Fail-fast error return when SSH connection is dropped — v1.0
- ✓ Daemon ships with launchd plist and systemd unit for background service — v1.0
- ✓ All MCP tool responses carry `_injection_warning` when injection detected (category+count, never matched text) — v1.1
- ✓ Detection engine scans exec stdout/stderr, file contents, dir entry names — v1.1
- ✓ Built-in patterns: XML tool tags, instruction-override, role hijacking, authority assertion — v1.1
- ✓ Custom regex patterns in `safeguards.patterns[]` compiled at startup — v1.1
- ✓ Guard enabled by default; `guard_disabled: true` disables; absent block safe — v1.1
- ✓ `ssh_write_file` and `ssh_upload_file` gate on `allow_overwrite: false` by default — v1.1
- ✓ `ssh_exec` gates on `allow_delete: false` for rm/unlink/truncate/shred/dd — v1.1
- ✓ Multi-host: named ControlMaster sockets in config, per-call `host` param routing, backward-compat auto-seed — v2.0

### Active
- [ ] Port forwarding (local -L, ephemeral): `ssh_port_forward`, `ssh_kill_forward`, `ssh_list_forwards` — v2.1
- [ ] Command allowlist (per-host, prefix-match): `hosts[].exec_allowlist` — v2.1
- [ ] Working dir restriction (per-host): `hosts[].base_dir` sandboxes file ops and exec cwd — v2.1

### Out of Scope

- Auto-reconnect / credential storage — user owns the SSH session lifecycle
- REST/HTTP API — Unix socket only (MCP server)
- OAuth/web auth — SSH key auth is the user's responsibility
- Package distribution (Homebrew, go install) — build from source only
- rsync-style directory sync — `ssh_exec` + rsync command covers this
- Database-specific tools — pure compositions of `ssh_exec`
- Output streaming / SSE — requires Streamable HTTP transport, not Unix socket STDIO
- sudo_exec as separate tool — sudo support belongs as a config flag

## Context

This project solves a specific friction point: Claude Code needs to run commands on a remote server, but opening a fresh SSH connection per command is slow, requires auth each time, and forces Claude to manage SSH credentials. The ControlMaster pattern (SSH multiplexing) eliminates this — the user runs `ssh -M -S /path/to/socket user@host` once, and the daemon routes all subsequent commands through that multiplexed connection.

The v1.1 milestone added a guard layer after recognizing that remote servers returning adversarial content (prompt injection) could manipulate Claude through tool responses. The guard scans all response surfaces and annotates suspicious content without blocking the response — except for SAFE-01/02 which block destructive operations by default.

**Current state:** ~7,500 LOC Go, 7 phases complete (v2.0 milestone complete). All 7 MCP tools support multi-host routing; `ssh_connection_status` reports all configured hosts; single-host configs auto-upgrade at startup with no config changes required.
**Tech stack:** Go, `github.com/modelcontextprotocol/go-sdk` v1.6.1, `golang.org/x/crypto/ssh` (future), `os/exec` for ControlMaster, `github.com/stretchr/testify` v1.10.0.

## Constraints

- **Language**: Go — single binary, low memory, strong stdlib for daemons and IPC
- **SSH approach**: ControlMaster socket only — daemon never stores or handles SSH credentials
- **API**: Unix socket (MCP protocol) — no TCP port, no network exposure
- **Platform**: macOS and Linux (user's machine + remote server)
- **Distribution**: Build from source only (personal tool, no package management)
- **Config**: JSON at `~/.config/claude-ssh-daemon/config.json`

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| MCP server (not REST) | Native Claude Code integration — tools appear in Claude's toolbox without wrappers | ✓ Good |
| Unix socket over TCP | No port conflicts, tighter OS-level access control, IPC-appropriate | ✓ Good |
| ControlMaster reuse | Keeps session management out of the daemon entirely — simpler, more secure | ✓ Good |
| Go over Python | Single binary, good daemon primitives, no runtime dependency on remote server | ✓ Good |
| Fail-fast on connection drop | Simpler state model — no queuing, no partial execution, user controls reconnect | ✓ Good |
| Feature toggles per-capability | Fine-grained control over what Claude can do remotely; safer for production servers | ✓ Good |
| Per-handler guard integration (not middleware) | go-sdk has no middleware intercept point; explicit wiring is more auditable | ✓ Good |
| category+count only in `_injection_warning` | Never echo matched adversarial text back to Claude — prevents reflection attacks | ✓ Good |
| Binary (base64) branch bypassed in scan | Scanning raw base64 bytes produces false positives; content is caller-supplied | ✓ Good |
| Per-entry scan in listDirHandler | Raw ls output contains permissions/dates that false-positive on pattern matches | ✓ Good |
| `allow_overwrite` / `allow_delete` default false | Fail-safe: destructive operations require explicit opt-in | ✓ Good |
| SAFE-01 via `test -e` (not noclobber) | Simpler; TOCTOU race accepted for single-user personal daemon | ⚠️ Revisit if multi-user |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd-complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-06-04 — Milestone v2.1 started (Tunneling & Access Controls)*
