# claude-ssh-daemon

A lightweight Go daemon that acts as a local intermediary for SSH connections. It exposes an MCP (Model Context Protocol) server over a Unix socket so Claude Code can run remote commands, read and write files, and use other SSH capabilities through a persistent SSH ControlMaster session that you manage.

The key idea: Claude never touches your SSH credentials or manages connection lifecycle. You bring up the ControlMaster session, the daemon proxies through it.

## Quick start

End-to-end setup in five steps.

**Step 1 — Build and install the binary**

```sh
go build ./cmd/claude-ssh-daemon
sudo cp claude-ssh-daemon /usr/local/bin/
```

**Step 2 — Start your SSH ControlMaster session**

The daemon never manages SSH connections. You start and maintain the session yourself. The `-S` socket path must match the `ssh_socket` value in your config.

```sh
ssh -M -S /tmp/ssh-ctrl-user@host.sock -fN user@host
```

**Step 3 — Write your config**

Create `~/.config/claude-ssh-daemon/config.json`. See the Config section below for the full example.

**Step 4 — Install the background service**

The daemon should run persistently so it is available whenever Claude Code needs it.

- **macOS (launchd):** Copy `contrib/com.claude-ssh-daemon.plist` to `~/Library/LaunchAgents/` and run:
  ```sh
  launchctl load ~/Library/LaunchAgents/com.claude-ssh-daemon.plist
  ```
  The plist has install instructions in its comments. View logs with:
  ```sh
  tail -f /tmp/claude-ssh-daemon.log
  ```

- **Linux (systemd):** Copy `contrib/claude-ssh-daemon.service` to `~/.config/systemd/user/` and run:
  ```sh
  systemctl --user daemon-reload
  systemctl --user enable --now claude-ssh-daemon
  ```
  The service file has install instructions in its comments. View logs with:
  ```sh
  journalctl --user -u claude-ssh-daemon -f
  ```

**Step 5 — Add the MCP entry to your Claude Code project**

See the Connect Claude Code section below for the ready-to-paste `.claude/mcp.json` block.

---

## What it can do

**SSH tools (v1 — all 7 shipped)**

| Tool | Type | Description |
|------|------|-------------|
| `ssh_connection_status` | read-only | Check whether the SSH ControlMaster socket is alive and get a re-establishment hint if it is not |
| `ssh_exec` | destructive | Execute a remote shell command via the SSH ControlMaster session |
| `ssh_read_file` | read-only | Read the contents of a remote file |
| `ssh_list_dir` | read-only | List the contents of a remote directory |
| `ssh_write_file` | destructive | Write or overwrite a remote file |
| `ssh_upload_file` | destructive | Upload a local file to the remote host |
| `ssh_download_file` | destructive | Download a remote file to the local machine |

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
- Disabled capabilities are never registered — they do not appear in `tools/list` at all

## Roadmap (v2)

Port forwarding (`ssh_port_forward`, `ssh_kill_forward`, `ssh_list_forwards`) is the next planned capability. It is not included in v1. See REQUIREMENTS.md §v2 Requirements for the full specification.

---

## Requirements

- Go 1.25+
- macOS or Linux
- An SSH ControlMaster session already running (you manage this); OpenSSH 6.0+ recommended (`-O check` requires OpenSSH 5.6+, 6.0+ is a safe documented floor)
- `socat` or `nc` (with `-U` flag) for the Claude Code stdio bridge: `brew install socat` (macOS) or `apt install socat` (Debian/Ubuntu)

## Build

```sh
go build ./cmd/claude-ssh-daemon
```

## Config

Create `~/.config/claude-ssh-daemon/config.json`:

```json
{
  "ssh_socket": "/tmp/ssh-ctrl-user@host.sock",
  "mcp_socket": "/tmp/claude-ssh-daemon.sock",
  "ssh_user": "ubuntu",
  "ssh_host": "my.server.com",
  "capabilities": {
    "exec": true,
    "file_read": true,
    "file_write": true,
    "port_forward": false
  }
}
```

`ssh_socket` is the path to your existing ControlMaster socket (`-S` argument you passed to `ssh -M`). `mcp_socket` is where the daemon will listen for Claude Code. `ssh_user` and `ssh_host` identify the remote target; SSH config aliases in `~/.ssh/config` are supported.

Capability toggles: only tools for enabled capabilities are registered. Disabled tools are invisible to Claude — they do not appear in `tools/list`.

## Run

```sh
./claude-ssh-daemon
```

The daemon logs to stderr in JSON format. It logs the socket path on startup and each connection/disconnection event. For persistent operation, use the service files in `contrib/` (see Quick start above).

## Connect Claude Code

Claude Code speaks MCP over stdio. The daemon listens on a Unix domain socket. A `socat` bridge connects the two — Claude Code launches `socat`, which forwards its stdin/stdout to the daemon's socket.

Add this to your project's `.claude/mcp.json`:

```json
{
  "mcpServers": {
    "claude-ssh-daemon": {
      "type": "stdio",
      "command": "socat",
      "args": ["-", "UNIX-CONNECT:/tmp/claude-ssh-daemon.sock"]
    }
  }
}
```

Replace `/tmp/claude-ssh-daemon.sock` with the `mcp_socket` value from your config.

**Alternative using `nc`:**

```json
{
  "mcpServers": {
    "claude-ssh-daemon": {
      "type": "stdio",
      "command": "nc",
      "args": ["-U", "/tmp/claude-ssh-daemon.sock"]
    }
  }
}
```

Note: Claude Code has no native Unix-socket transport type — the `stdio` + bridge pattern above is the correct and only approach. A `unix` or `socket` transport key is not supported by Claude Code.

The MCP socket is created mode 0600 (owner-only), so the `socat`/`nc` bridge is only reachable by the user who owns the daemon process.

## Project status

v1 is complete and usable. All 7 SSH tools work end-to-end over the ControlMaster socket. Port forwarding is the v2 roadmap item.
