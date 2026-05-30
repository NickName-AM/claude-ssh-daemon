# claude-ssh-daemon

A lightweight Go daemon that acts as a local intermediary for SSH connections. It exposes an MCP (Model Context Protocol) server over a Unix socket so Claude Code can run remote commands, read and write files, and use other SSH capabilities through a persistent SSH ControlMaster session that you manage.

The key idea: Claude never touches your SSH credentials or manages connection lifecycle. You bring up the ControlMaster session, the daemon proxies through it.

## What it can do right now

The daemon is in early development. Here is what is working today:

**Core daemon**
- Starts up and creates a Unix socket at the path you configure
- Loads config from `~/.config/claude-ssh-daemon/config.json`
- Exposes an MCP server over the socket using the official `go-sdk` (spec 2025-11-25)
- Accepts one client connection at a time (sequential, intentional)
- Handles `SIGTERM` and `SIGINT` for clean shutdown with a 5-second drain timeout
- Removes the socket file on exit so restarts do not hit `EADDRINUSE`

**Security**
- Socket created with mode 0600 (owner read/write only)
- Umask-before-listen pattern to close the race window between `listen()` and `chmod()` (mitigates CVE-2023-45145 class)
- Capability toggles in config (`exec`, `file_read`, `file_write`, `port_forward`) all default to off
- No tools are registered yet; the MCP server responds to connections but has an empty tool list

**Config**
- Reads `ssh_socket` (path to your ControlMaster socket) and `mcp_socket` (where the daemon listens)
- Unknown config fields are silently ignored so one config file will work across future versions
- Clear error messages when required fields are missing

## What is not built yet

- SSH command execution (`exec` tool)
- Remote file read/write (`file_read`, `file_write` tools)
- Port forwarding (`port_forward` tool)
- Claude Code MCP config wiring (the `mcp.json` entry that tells Claude where the socket is)
- launchd/systemd service templates for running the daemon as a background service

## Requirements

- Go 1.25+
- macOS or Linux
- An OpenSSH ControlMaster session already running (you manage this)

## Build

```sh
go build ./cmd/claude-ssh-daemon
```

## Config

Create `~/.config/claude-ssh-daemon/config.json`:

```json
{
  "ssh_socket": "/tmp/ssh-ctl-yourhost",
  "mcp_socket": "/tmp/claude-ssh-daemon.sock",
  "capabilities": {
    "exec": false,
    "file_read": false,
    "file_write": false,
    "port_forward": false
  }
}
```

`ssh_socket` is the path to your existing ControlMaster socket. `mcp_socket` is where the daemon will listen for Claude Code.

## Run

```sh
./claude-ssh-daemon
```

The daemon logs to stderr in JSON format. It will log the socket path on startup and each connection/disconnection event.

## Project status

Early. The daemon infrastructure is solid but no SSH tools are wired up yet. The next development phase adds the actual MCP tools that let Claude execute remote commands through the ControlMaster socket.
