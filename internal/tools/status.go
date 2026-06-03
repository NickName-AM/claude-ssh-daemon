package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
	"github.com/NickName-AM/claude-ssh-daemon/internal/ssh"
)

// StatusInput has no input parameters — ssh_connection_status is exempt from
// the optional host parameter (D-08). It always checks all configured hosts.
type StatusInput struct{}

// HostStatus represents the connection status of a single named SSH host (D-09).
type HostStatus struct {
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	Socket    string `json:"socket"`
	Hint      string `json:"hint,omitempty"`
}

// StatusOutput is the structured response for ssh_connection_status (D-09).
// DefaultHost names the host used when tools omit the host parameter.
// Hosts contains per-host connection status in sorted order.
// Old top-level connected/socket_path/hint fields are removed (D-10).
type StatusOutput struct {
	DefaultHost string       `json:"default_host"`
	Hosts       []HostStatus `json:"hosts"`
}

// statusHandler returns a ToolHandlerFor closure for the ssh_connection_status tool.
// It checks all configured hosts in sorted order and returns per-host connection status.
//
// CONN-01 / DMON-04: a dead socket is a normal, expected diagnostic answer.
// The handler MUST return IsError=false for a dead socket — only set IsError for
// unexpected tool failures (not for the expected "socket is dead" outcome).
// D-08: ssh_connection_status takes no host parameter — it always checks all hosts.
func statusHandler(registry map[string]ssh.SSHExecutor, cfg *config.Config) mcp.ToolHandlerFor[StatusInput, StatusOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ StatusInput) (*mcp.CallToolResult, StatusOutput, error) {
		names := sortedKeys(registry)
		statuses := make([]HostStatus, 0, len(names))
		for _, name := range names {
			exec := registry[name]
			hs := HostStatus{Name: name, Socket: cfg.Hosts[name].Socket}
			if err := exec.CheckSocket(ctx); err == nil {
				hs.Connected = true
			} else {
				hs.Hint = err.Error()
			}
			statuses = append(statuses, hs)
		}
		// Return nil *CallToolResult so SDK auto-populates Content from StatusOutput.
		// No manual JSON marshal needed (unlike the old v1.x dead-socket path — Pitfall 1).
		return nil, StatusOutput{DefaultHost: cfg.DefaultHost, Hosts: statuses}, nil
	}
}
