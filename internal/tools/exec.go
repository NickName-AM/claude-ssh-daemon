package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NickName-AM/claude-ssh-daemon/internal/ssh"
)

// ExecInput holds the parameters for the ssh_exec tool.
// Command is required; cwd, timeout_seconds, and env are optional (EXEC-02).
type ExecInput struct {
	Command        string            `json:"command"          jsonschema:"the shell command to run on the remote host"`
	Cwd            string            `json:"cwd,omitempty"    jsonschema:"remote working directory; the command is run as 'cd <cwd> && <command>'"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty" jsonschema:"timeout in seconds (default 30, max 600)"`
	Env            map[string]string `json:"env,omitempty"    jsonschema:"additional environment variables to set before running the command"`
}

// ExecOutput is the structured response for the ssh_exec tool (EXEC-01).
// All seven fields are always present in the JSON output.
type ExecOutput struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	TimedOut   bool   `json:"timed_out"`
	Command    string `json:"command"`
	Cwd        string `json:"cwd"`
}

// execHandler returns a ToolHandlerFor closure for the ssh_exec tool.
// It maps ExecInput to ssh.RunRequest, calls RunCommand, and maps the result
// back to ExecOutput.
//
// EXEC-03 error boundary:
//   - Non-zero exit code → IsError=false; exit_code carries the value.
//   - Dead socket / subprocess failure (RunCommand returns non-nil error) → IsError=true.
func execHandler(e ssh.SSHExecutor) mcp.ToolHandlerFor[ExecInput, ExecOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ExecInput) (*mcp.CallToolResult, ExecOutput, error) {
		result, err := e.RunCommand(ctx, ssh.RunRequest{
			Command:        in.Command,
			Cwd:            in.Cwd,
			TimeoutSeconds: in.TimeoutSeconds,
			Env:            in.Env,
		})
		if err != nil {
			// Dead socket or subprocess failure — isError: true (EXEC-03).
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, ExecOutput{}, nil
		}

		out := ExecOutput{
			Stdout:     result.Stdout,
			Stderr:     result.Stderr,
			ExitCode:   result.ExitCode,
			DurationMs: result.DurationMs,
			TimedOut:   result.TimedOut,
			Command:    in.Command,
			Cwd:        in.Cwd,
		}
		// Return nil *CallToolResult so SDK auto-populates Content from out (EXEC-01).
		// Non-zero ExitCode is a normal result — IsError remains false (EXEC-03).
		return nil, out, nil
	}
}
