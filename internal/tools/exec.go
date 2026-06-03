package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
	"github.com/NickName-AM/claude-ssh-daemon/internal/guard"
	"github.com/NickName-AM/claude-ssh-daemon/internal/ssh"
)

// ExecInput holds the parameters for the ssh_exec tool.
// Command is required; cwd, timeout_seconds, env, and host are optional (EXEC-02).
type ExecInput struct {
	Command        string            `json:"command"          jsonschema:"the shell command to run on the remote host"`
	Cwd            string            `json:"cwd,omitempty"    jsonschema:"remote working directory; the command is run as 'cd <cwd> && <command>'"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty" jsonschema:"timeout in seconds (default 30, max 600)"`
	Env            map[string]string `json:"env,omitempty"    jsonschema:"additional environment variables to set before running the command"`
	Host           string            `json:"host,omitempty"   jsonschema:"named SSH host; omit to use default_host"`
}

// ExecOutput is the structured response for the ssh_exec tool (EXEC-01).
// The first seven fields are always present in the JSON output.
// _injection_warning is omitted when no injection patterns are detected (GURD-01).
type ExecOutput struct {
	Stdout           string `json:"stdout"`
	Stderr           string `json:"stderr"`
	ExitCode         int    `json:"exit_code"`
	DurationMs       int64  `json:"duration_ms"`
	TimedOut         bool   `json:"timed_out"`
	Command          string `json:"command"`
	Cwd              string `json:"cwd"`
	InjectionWarning string `json:"_injection_warning,omitempty"`
}

// execHandler returns a ToolHandlerFor closure for the ssh_exec tool.
// It maps ExecInput to ssh.RunRequest, calls RunCommand, and maps the result
// back to ExecOutput.
//
// EXEC-03 error boundary:
//   - Non-zero exit code → IsError=false; exit_code carries the value.
//   - Dead socket / subprocess failure (RunCommand returns non-nil error) → IsError=true.
func execHandler(registry map[string]ssh.SSHExecutor, cfg *config.Config) mcp.ToolHandlerFor[ExecInput, ExecOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ExecInput) (*mcp.CallToolResult, ExecOutput, error) {
		// SAFE-02: block destructive commands before resolving executor (policy gate
		// fires before any SSH I/O — consistent with pre-executor gate ordering).
		if !cfg.Safeguards.AllowDelete {
			if name, ok := isDestructiveCommand(in.Command); ok {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{
						Text: fmt.Sprintf("command %q is blocked: matches destructive pattern; set safeguards.allow_delete: true to allow", name),
					}},
				}, ExecOutput{}, nil
			}
		}

		// MHST-05/06/07: resolve the executor for the requested host.
		exec, hostName, errResult := resolveExecutor(registry, cfg, in.Host)
		if errResult != nil {
			return errResult, ExecOutput{}, nil
		}

		result, err := exec.RunCommand(ctx, ssh.RunRequest{
			Command:        in.Command,
			Cwd:            in.Cwd,
			TimeoutSeconds: in.TimeoutSeconds,
			Env:            in.Env,
		})
		if err != nil {
			// Dead socket or subprocess failure — isError: true (EXEC-03).
			// MHST-08: prefix error with resolved host name.
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("[host %s] %s", hostName, err.Error())}},
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

		// GURD-01/02: scan stdout then stderr; first hit wins. Never set IsError —
		// injection annotation is advisory only (Pitfall 5). Matched text is never
		// reflected; formatInjectionWarning uses only category+count (GURD-01).
		if !cfg.Safeguards.GuardDisabled {
			if r := guard.ScanWithPatterns(out.Stdout, cfg.Safeguards.CompiledPatterns); r.Matches != nil {
				out.InjectionWarning = formatInjectionWarning(r)
			} else if r := guard.ScanWithPatterns(out.Stderr, cfg.Safeguards.CompiledPatterns); r.Matches != nil {
				out.InjectionWarning = formatInjectionWarning(r)
			}
		}

		// Return nil *CallToolResult so SDK auto-populates Content from out (EXEC-01).
		// Non-zero ExitCode is a normal result — IsError remains false (EXEC-03).
		return nil, out, nil
	}
}
