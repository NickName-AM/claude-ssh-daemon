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
		// ssh_exec is registered by plan 02-02.
		// mcp.AddTool(server, &mcp.Tool{Name: "ssh_exec", ...}, execHandler(exec))
	}

	if cfg.Capabilities.FileRead {
		// ssh_read_file and ssh_list_dir are registered by plan 02-03.
		// mcp.AddTool(server, &mcp.Tool{Name: "ssh_read_file", ...}, readFileHandler(exec))
		// mcp.AddTool(server, &mcp.Tool{Name: "ssh_list_dir", ...}, listDirHandler(exec))
	}

	if cfg.Capabilities.FileWrite {
		// ssh_write_file, ssh_upload_file, and ssh_download_file are registered by plan 02-04.
		// mcp.AddTool(server, &mcp.Tool{Name: "ssh_write_file",     ...}, writeFileHandler(exec))
		// mcp.AddTool(server, &mcp.Tool{Name: "ssh_upload_file",    ...}, uploadHandler(exec))
		// mcp.AddTool(server, &mcp.Tool{Name: "ssh_download_file",  ...}, downloadHandler(exec))
	}
}

// boolPtr returns a pointer to b. Required because DestructiveHint is *bool
// (not bool) in mcp.ToolAnnotations — plain `true` does not compile (Pitfall 1).
// Used by later plans that register destructive tools.
func boolPtr(b bool) *bool { return &b }
