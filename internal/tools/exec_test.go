package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
	"github.com/NickName-AM/claude-ssh-daemon/internal/ssh"
)

// execOutput mirrors the JSON fields of ExecOutput for assertion.
type execOutput struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	TimedOut   bool   `json:"timed_out"`
	Command    string `json:"command"`
	Cwd        string `json:"cwd"`
}

func newExecTestServer(t *testing.T, exec ssh.SSHExecutor, capExec bool) *mcp.ClientSession {
	t.Helper()
	cfg := &config.Config{
		SSHSocket: "/tmp/test.sock",
		SSHUser:   "user",
		SSHHost:   "host",
		Capabilities: config.Capabilities{
			Exec: capExec,
		},
	}
	return newTestServer(t, exec, cfg)
}

// TestSSHExecSuccessExitZero verifies that execHandler returns IsError=false and
// correct fields (stdout, exit_code=0, command echoed) for a successful command.
func TestSSHExecSuccessExitZero(t *testing.T) {
	mock := &toolsMockExecutor{
		runResult: ssh.RunResult{
			Stdout:     "hello\n",
			Stderr:     "",
			ExitCode:   0,
			DurationMs: 42,
		},
	}
	cs := newExecTestServer(t, mock, true)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_exec",
		Arguments: map[string]any{"command": "echo hello"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "exit 0 must NOT set isError")

	require.NotEmpty(t, result.Content)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "content[0] must be *mcp.TextContent")

	var out execOutput
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.Equal(t, "hello\n", out.Stdout)
	require.Equal(t, 0, out.ExitCode)
	require.Equal(t, "echo hello", out.Command, "command must be echoed back")
}

// TestSSHExecNonZeroExitIsNotError proves EXEC-03:
// when the remote command exits non-zero, result.IsError must be false
// and exit_code must carry the non-zero value.
func TestSSHExecNonZeroExitIsNotError(t *testing.T) {
	mock := &toolsMockExecutor{
		runResult: ssh.RunResult{
			Stdout:   "",
			Stderr:   "no match\n",
			ExitCode: 1,
		},
	}
	cs := newExecTestServer(t, mock, true)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_exec",
		Arguments: map[string]any{"command": "grep nothing /dev/null"},
	})
	require.NoError(t, err)
	// EXEC-03: non-zero exit is NOT isError — the command ran, it just failed.
	require.False(t, result.IsError, "non-zero exit must NOT set isError (EXEC-03)")

	require.NotEmpty(t, result.Content)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "content[0] must be *mcp.TextContent")

	var out execOutput
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.Equal(t, 1, out.ExitCode, "exit_code must be 1 (EXEC-03)")
	require.Equal(t, "grep nothing /dev/null", out.Command, "command must be echoed back")
}

// TestSSHExecDeadSocketIsError proves EXEC-03:
// when the executor returns a non-nil error (dead socket / subprocess failure),
// result.IsError must be true.
func TestSSHExecDeadSocketIsError(t *testing.T) {
	mock := &toolsMockExecutor{
		runErr: errors.New("ssh subprocess error (socket dead?): no such file or directory"),
	}
	cs := newExecTestServer(t, mock, true)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_exec",
		Arguments: map[string]any{"command": "echo hello"},
	})
	require.NoError(t, err)
	// EXEC-03: dead socket IS isError — tool-layer failure.
	require.True(t, result.IsError, "dead socket must set isError (EXEC-03)")

	require.NotEmpty(t, result.Content)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "content[0] must be *mcp.TextContent")
	require.NotEmpty(t, text.Text, "error message must not be empty")
}

// TestSSHExecEchoesCwdField verifies that the cwd input field is reflected
// in the ExecOutput.Cwd field (EXEC-01).
func TestSSHExecEchoesCwdField(t *testing.T) {
	mock := &toolsMockExecutor{
		runResult: ssh.RunResult{
			Stdout:   "/home/ubuntu\n",
			ExitCode: 0,
		},
	}
	cs := newExecTestServer(t, mock, true)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_exec",
		Arguments: map[string]any{
			"command": "pwd",
			"cwd":     "/home/ubuntu",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var out execOutput
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.Equal(t, "pwd", out.Command)
	require.Equal(t, "/home/ubuntu", out.Cwd, "cwd must be echoed back (EXEC-01)")
}

// TestSSHExecAbsentWhenCapExecFalse verifies that ssh_exec does NOT appear in
// tools/list when capabilities.exec is false (SECU-01/SECU-02).
func TestSSHExecAbsentWhenCapExecFalse(t *testing.T) {
	cs := newExecTestServer(t, &toolsMockExecutor{}, false)

	toolsList, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)

	for _, tool := range toolsList.Tools {
		require.NotEqual(t, "ssh_exec", tool.Name, "ssh_exec must NOT appear when exec capability is false")
	}
}

// TestSSHExecPresentWithDestructiveHintWhenCapExecTrue verifies that ssh_exec
// appears in tools/list with destructiveHint:true when capabilities.exec is true.
func TestSSHExecPresentWithDestructiveHintWhenCapExecTrue(t *testing.T) {
	cs := newExecTestServer(t, &toolsMockExecutor{}, true)

	toolsList, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)

	var found bool
	for _, tool := range toolsList.Tools {
		if tool.Name == "ssh_exec" {
			found = true
			require.NotNil(t, tool.Annotations, "annotations must not be nil")
			require.NotNil(t, tool.Annotations.DestructiveHint, "destructiveHint must be set (*bool, not nil)")
			require.True(t, *tool.Annotations.DestructiveHint, "destructiveHint must be true")
			break
		}
	}
	require.True(t, found, "ssh_exec must appear in tools/list when exec capability is true")
}
