# claude-ssh-daemon

A lightweight Go daemon that acts as a local intermediary for SSH connections. It exposes an MCP (Model Context Protocol) server over a Unix socket so Claude Code can run remote commands, read and write files, and use other SSH capabilities through a persistent SSH ControlMaster session that you manage.

The key idea: Claude never touches your SSH credentials or manages connection lifecycle. You bring up the ControlMaster session, the daemon proxies through it.

## Quick start

**Step 1: Build and install the binary**

```sh
go build ./cmd/claude-ssh-daemon
sudo cp claude-ssh-daemon /usr/local/bin/
```

**Step 2: Start your SSH ControlMaster session**

The daemon never manages SSH connections. You start and maintain the session yourself. The `-S` socket path must match the `ssh_socket` value in your config.

```sh
ssh -M -S /tmp/ssh-ctrl-user@host.sock -fN user@host
```

**Step 3: Write your config**

Create `~/.config/claude-ssh-daemon/config.json`. See the Config section below for the full example.

**Step 4: Install the background service**

The daemon should run persistently so it is available whenever Claude Code needs it.

- **macOS (launchd):** Copy the plist, substitute your username (launchd does not expand `~` in paths), then load the service:
  ```sh
  cp contrib/com.claude-ssh-daemon.plist ~/Library/LaunchAgents/
  sed -i '' "s/YOUR_USERNAME/$(id -un)/g" ~/Library/LaunchAgents/com.claude-ssh-daemon.plist
  launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.claude-ssh-daemon.plist
  ```
  View logs with:
  ```sh
  tail -f ~/Library/Logs/claude-ssh-daemon.log
  ```
  To unload:
  ```sh
  launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/com.claude-ssh-daemon.plist
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

**Step 5: Register the MCP server with Claude Code**

Claude Code spawns MCP servers as subprocesses using a restricted PATH that typically does not include Homebrew (`/opt/homebrew/bin`) on macOS. You must use the **full path** to `socat`.

Find the path first:

```sh
which socat
# macOS Apple Silicon (Homebrew): /opt/homebrew/bin/socat
# macOS Intel (Homebrew):         /usr/local/bin/socat
# Linux (apt/system):             /usr/bin/socat
```

Then register using that path (replace the path and socket path as needed):

```sh
claude mcp add claude-ssh-daemon /opt/homebrew/bin/socat -- - UNIX-CONNECT:/tmp/claude-ssh-daemon.sock
```

This registers the server globally (all projects). Verify it connected:

```sh
# In any Claude Code session:
/mcp
```

You should see `claude-ssh-daemon` listed as connected with its tools.

---

## What it can do

**SSH tools (v2.1 -- all 9 shipped)**

| Tool | Capability | Type | Description |
|------|-----------|------|-------------|
| `ssh_connection_status` | always on | read-only | Check whether the SSH ControlMaster socket is alive and get a re-establishment hint if it is not |
| `ssh_exec` | `exec` | destructive | Execute a remote shell command via the SSH ControlMaster session |
| `ssh_read_file` | `file_read` | read-only | Read the contents of a remote file |
| `ssh_list_dir` | `file_read` | read-only | List the contents of a remote directory |
| `ssh_write_file` | `file_write` | destructive | Write or overwrite a remote file |
| `ssh_upload_file` | `file_write` | destructive | Upload a local file to the remote host |
| `ssh_download_file` | `file_write` | destructive | Download a remote file to the local machine |
| `ssh_forward_port` | `port_forward` | destructive | Create a local SSH port forward via the ControlMaster session (auto-allocates a free local port) |
| `ssh_list_forwards` | `port_forward` | read-only | List all active port forwards managed by this daemon instance, with running/dead status |

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
- Capability toggles in config (`exec`, `file_read`, `file_write`, `port_forward`) all default to off; disabled tools are never registered
- Safeguards layer: prompt-injection scanning on all tool output (on by default), overwrite protection for `ssh_write_file` (opt-in), destructive command blocking for `ssh_exec` (opt-in)
- Per-host `base_dir`: lexically confines all file and exec-cwd operations to a directory subtree (opt-in per host)
- Per-host `exec_allowlist`: restricts `ssh_exec` to a set of command prefixes (opt-in per host)

## Requirements

- Go 1.23+
- macOS or Linux
- An SSH ControlMaster session already running (you manage this); OpenSSH 6.0+ recommended (`-O check` requires OpenSSH 5.6+, 6.0+ is a safe documented floor)
- `socat` for the Claude Code stdio bridge: `brew install socat` (macOS) or `apt install socat` (Debian/Ubuntu). Use the **full absolute path** to `socat` in your MCP config — Claude Code spawns servers with a restricted PATH and may not find socat by name alone (see Step 5 above).

## Build

```sh
go build ./cmd/claude-ssh-daemon
```

## Config

Create `~/.config/claude-ssh-daemon/config.json`.

**Single-host (legacy, still fully supported):**

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

**Multi-host:**

```json
{
  "mcp_socket": "/tmp/claude-ssh-daemon.sock",
  "default_host": "prod",
  "hosts": {
    "prod": {
      "socket": "/tmp/ssh-ctrl-ubuntu@prod.sock",
      "user": "ubuntu",
      "host": "prod.example.com",
      "base_dir": "/srv/app",
      "exec_allowlist": ["git ", "make ", "npm "]
    },
    "staging": {
      "socket": "/tmp/ssh-ctrl-ubuntu@staging.sock",
      "user": "ubuntu",
      "host": "staging.example.com"
    }
  },
  "capabilities": {
    "exec": true,
    "file_read": true,
    "file_write": true,
    "port_forward": false
  }
}
```

With multi-host config every tool accepts an optional `host` parameter. Omit it to target `default_host`. Each host needs its own ControlMaster session running against its `socket` path.

**Per-host optional fields:**

| Field | Default | Effect |
|-------|---------|--------|
| `base_dir` | `""` (unset) | Absolute path. When set, all file operations (`ssh_read_file`, `ssh_write_file`, `ssh_list_dir`, `ssh_upload_file`, `ssh_download_file`) and `ssh_exec` cwd are confined to this directory tree by lexical path checking. Paths that resolve outside are rejected with `isError: true`. Symlinks on the remote are not resolved and may point outside `base_dir`. |
| `exec_allowlist` | `null` (allow-all) | JSON array of command prefixes. When `null` or absent: all commands are allowed. When set to `[]` (empty array): all commands are denied. When set to `["git ", "make "]`: only commands whose first token matches a listed prefix are allowed. |

**Safeguards (optional):**

```json
{
  "safeguards": {
    "guard_disabled": false,
    "allow_overwrite": false,
    "allow_delete": false,
    "patterns": []
  }
}
```

| Field | Default | Effect |
|-------|---------|--------|
| `guard_disabled` | `false` | When false, stdout/stderr from every tool is scanned for prompt-injection patterns. A warning is appended to the result but the operation is not blocked. |
| `allow_overwrite` | `false` | When false, `ssh_write_file` refuses to write to paths that already exist on the remote. |
| `allow_delete` | `false` | When false, `ssh_exec` blocks commands whose first token is `rm`, `unlink`, `truncate`, `shred`, or `dd`. |
| `patterns` | `[]` | Additional regex strings appended to the built-in injection-detection ruleset. |

Capability toggles: only tools for enabled capabilities are registered. Disabled tools are invisible to Claude — they do not appear in `tools/list`.

## Run

```sh
./claude-ssh-daemon
```

The daemon logs to stderr in JSON format. It logs the socket path on startup and each connection/disconnection event. For persistent operation, use the service files in `contrib/` (see Quick start above).

## Connect Claude Code

Claude Code speaks MCP over stdio. The daemon listens on a Unix domain socket. A `socat` bridge connects the two -- Claude Code launches `socat`, which forwards its stdin/stdout to the daemon's socket.

**Important:** Claude Code spawns MCP server processes with a restricted PATH that typically does not include Homebrew prefixes on macOS. Always use the **absolute path** to `socat`. Find it with `which socat`.

**Global registration (recommended):**

```sh
# macOS Apple Silicon — adjust path for your system (see `which socat`)
claude mcp add claude-ssh-daemon /opt/homebrew/bin/socat -- - UNIX-CONNECT:/tmp/claude-ssh-daemon.sock
```

Replace `/tmp/claude-ssh-daemon.sock` with the `mcp_socket` value from your config. This makes the server available in all Claude Code projects.

**Per-project registration:**

Add a `.claude/mcp.json` file in your project directory:

```json
{
  "mcpServers": {
    "claude-ssh-daemon": {
      "type": "stdio",
      "command": "/opt/homebrew/bin/socat",
      "args": ["-", "UNIX-CONNECT:/tmp/claude-ssh-daemon.sock"]
    }
  }
}
```

Adjust the `command` path for your platform (`/usr/local/bin/socat` on Intel Mac, `/usr/bin/socat` on Linux).

Note: Claude Code has no native Unix-socket transport type -- the `stdio` + `socat` bridge is the correct and only approach. A `unix` or `socket` transport key is not supported.

The MCP socket is created mode 0600 (owner-only), so the `socat` bridge is only reachable by the user who owns the daemon process.

## Example Claude Code prompts

Once the daemon is running and registered, you can talk to your remote hosts naturally. Here are some prompts that work well:

**Explore and understand a remote service**
```
Check the status of nginx on my prod server — is it running, what config is it using, and are there any recent errors in the logs?
```

**Tail logs and diagnose issues**
```
Read the last 100 lines of /var/log/app/error.log on prod and summarize any recurring errors or patterns you see.
```

**Deploy a change**
```
Upload my local ./dist/app.js to /srv/app/dist/app.js on the staging host, then restart the Node process with: systemctl --user restart myapp
```

**Database access via port forwarding**
```
Forward the Postgres port from db.internal:5432 so I can connect to it locally. Tell me the local port when it's ready.
```
After the forward is established, you can then connect with `psql -h 127.0.0.1 -p <local_port> -U myuser mydb`.

**Inspect and edit a config file**
```
Read /etc/nginx/sites-available/mysite on prod. The upstream timeout is too low — update it to 60s and reload nginx.
```

**Multi-host workflow**
```
Compare the app version currently deployed on staging vs prod — read /srv/app/package.json on each host and tell me if they differ.
```

**Check connection health before starting work**
```
Check the SSH connection status for all my configured hosts before we start.
```

---

## Project status

v2.1 is complete. All 9 SSH tools work end-to-end: 7 core tools (connection check, exec, file read/write/list/upload/download) plus 2 port-forwarding tools (`ssh_forward_port`, `ssh_list_forwards`). Multi-host config, `base_dir` sandboxing, `exec_allowlist`, and safeguards (prompt-injection scanning, overwrite protection, delete blocking) are all shipped.
