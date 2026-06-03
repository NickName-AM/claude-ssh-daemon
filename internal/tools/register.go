// Package tools registers all MCP tool handlers on the go-sdk *mcp.Server.
// Tool registration is capability-gated: disabled capabilities are not wired in.
package tools

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
	"github.com/NickName-AM/claude-ssh-daemon/internal/ssh"
)

// RegisterTools registers all MCP tool handlers on server.
// Tools for disabled capabilities are not registered (SECU-01).
// ssh_connection_status is always registered — it is a diagnostic tool, not
// capability-gated (CONN-01, RESEARCH.md open question A2 resolved here).
// Must be called BEFORE the accept loop starts in daemon.Run (Pitfall 3).
func RegisterTools(server *mcp.Server, exec ssh.SSHExecutor, cfg *config.Config) {
	// Always registered — diagnostic tool, not an SSH operation (CONN-01, DMON-04).
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ssh_connection_status",
		Description: "Check whether the SSH ControlMaster socket is alive and get a re-establishment hint if it is not",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, statusHandler(exec, cfg))

	if cfg.Capabilities.Exec {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "ssh_exec",
			Description: "Execute a remote shell command via the SSH ControlMaster session",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
		}, execHandler(exec, cfg))
	}

	if cfg.Capabilities.FileRead {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "ssh_read_file",
			Description: "Read the contents of a remote file",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, readFileHandler(exec, cfg))
		mcp.AddTool(server, &mcp.Tool{
			Name:        "ssh_list_dir",
			Description: "List the contents of a remote directory",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, listDirHandler(exec, cfg))
	}

	if cfg.Capabilities.FileWrite {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "ssh_write_file",
			Description: "Write or overwrite a remote file",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
		}, writeFileHandler(exec, cfg))
		mcp.AddTool(server, &mcp.Tool{
			Name:        "ssh_upload_file",
			Description: "Upload a local file to the remote host",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
		}, uploadHandler(exec, cfg))
		mcp.AddTool(server, &mcp.Tool{
			Name:        "ssh_download_file",
			Description: "Download a remote file to the local machine",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
		}, downloadHandler(exec, cfg))
	}
}

// boolPtr returns a pointer to b. Required because DestructiveHint is *bool
// (not bool) in mcp.ToolAnnotations — plain `true` does not compile (Pitfall 1).
// Used by later plans that register destructive tools.
func boolPtr(b bool) *bool { return &b }
