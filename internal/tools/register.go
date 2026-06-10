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
//
// registry is a map of host name → executor built from cfg.Hosts in daemon.Run.
// Tool descriptions, annotations, and capability gating are unchanged (D-06/D-07
// keep safeguards and capabilities global across all hosts).
func RegisterTools(server *mcp.Server, registry map[string]ssh.SSHExecutor, cfg *config.Config) {
	// Always registered — diagnostic tool, not an SSH operation (CONN-01, DMON-04).
	mcp.AddTool(server, &mcp.Tool{
		Name:        "ssh_connection_status",
		Description: "Check whether the SSH ControlMaster socket is alive and get a re-establishment hint if it is not",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, statusHandler(registry, cfg))

	if cfg.Capabilities.Exec {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "ssh_exec",
			Description: "Execute a remote shell command via the SSH ControlMaster session. When base_dir is configured for the host, paths are confined to that directory by lexical checking only; symlinks on the remote are not resolved and may point outside base_dir.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
		}, execHandler(registry, cfg))
	}

	if cfg.Capabilities.FileRead {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "ssh_read_file",
			Description: "Read the contents of a remote file. When base_dir is configured for the host, paths are confined to that directory by lexical checking only; symlinks on the remote are not resolved and may point outside base_dir.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, readFileHandler(registry, cfg))
		mcp.AddTool(server, &mcp.Tool{
			Name:        "ssh_list_dir",
			Description: "List the contents of a remote directory. When base_dir is configured for the host, paths are confined to that directory by lexical checking only; symlinks on the remote are not resolved and may point outside base_dir.",
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
		}, listDirHandler(registry, cfg))
	}

	if cfg.Capabilities.FileWrite {
		mcp.AddTool(server, &mcp.Tool{
			Name:        "ssh_write_file",
			Description: "Write or overwrite a remote file. When base_dir is configured for the host, paths are confined to that directory by lexical checking only; symlinks on the remote are not resolved and may point outside base_dir.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
		}, writeFileHandler(registry, cfg))
		mcp.AddTool(server, &mcp.Tool{
			Name:        "ssh_upload_file",
			Description: "Upload a local file to the remote host. When base_dir is configured for the host, paths are confined to that directory by lexical checking only; symlinks on the remote are not resolved and may point outside base_dir.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
		}, uploadHandler(registry, cfg))
		mcp.AddTool(server, &mcp.Tool{
			Name:        "ssh_download_file",
			Description: "Download a remote file to the local machine. When base_dir is configured for the host, paths are confined to that directory by lexical checking only; symlinks on the remote are not resolved and may point outside base_dir.",
			Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPtr(true)},
		}, downloadHandler(registry, cfg))
	}
}

// boolPtr returns a pointer to b. Required because DestructiveHint is *bool
// (not bool) in mcp.ToolAnnotations — plain `true` does not compile (Pitfall 1).
// Used by later plans that register destructive tools.
func boolPtr(b bool) *bool { return &b }
