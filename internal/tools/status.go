package tools

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
	"github.com/NickName-AM/claude-ssh-daemon/internal/ssh"
)

// StatusInput has no input parameters — ssh_connection_status takes no arguments.
type StatusInput struct{}

// StatusOutput is the structured response for ssh_connection_status.
// connected:   true if the ControlMaster socket is alive.
// socket_path: the configured SSH socket path (helps user identify which to fix).
// hint:        non-empty when connected is false; contains the re-establishment command.
type StatusOutput struct {
	Connected  bool   `json:"connected"`
	SocketPath string `json:"socket_path"`
	Hint       string `json:"hint"`
}

// statusHandler returns a ToolHandlerFor closure for the ssh_connection_status tool.
// It calls e.CheckSocket to probe the ControlMaster socket liveness.
//
// CONN-01 / DMON-04: a dead socket is a normal, expected diagnostic answer.
// The handler MUST return IsError=false for a dead socket — only set IsError for
// unexpected tool failures (not for the expected "socket is dead" outcome).
func statusHandler(e ssh.SSHExecutor, cfg *config.Config) mcp.ToolHandlerFor[StatusInput, StatusOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ StatusInput) (*mcp.CallToolResult, StatusOutput, error) {
		err := e.CheckSocket(ctx)
		if err == nil {
			// Socket is alive — return connected:true.
			out := StatusOutput{
				Connected:  true,
				SocketPath: cfg.SSHSocket,
				Hint:       "",
			}
			// Return nil *CallToolResult so SDK auto-populates Content from out.
			return nil, out, nil
		}

		// Socket is dead — return connected:false with a re-establishment hint.
		// Do NOT set IsError:true — this is a diagnostic result, not a tool failure.
		// The hint already contains the re-establishment command (from CheckSocket).
		out := StatusOutput{
			Connected:  false,
			SocketPath: cfg.SSHSocket,
			Hint:       err.Error(),
		}

		// Marshal manually so we can return a *CallToolResult with IsError=false explicitly.
		// Returning nil *CallToolResult would also work (SDK defaults IsError to false),
		// but being explicit guards against future SDK behavior changes.
		b, marshalErr := json.Marshal(out)
		if marshalErr != nil {
			// This should never happen for a simple struct; surface as tool error if it does.
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: marshalErr.Error()}},
			}, StatusOutput{}, nil
		}
		return &mcp.CallToolResult{
			IsError: false,
			Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
		}, StatusOutput{}, nil
	}
}
