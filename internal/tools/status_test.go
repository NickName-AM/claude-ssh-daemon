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

// toolsMockExecutor implements ssh.SSHExecutor for tools package tests.
// Each method returns its corresponding Result/Err field.
type toolsMockExecutor struct {
	runResult      ssh.RunResult
	runErr         error
	readContent    []byte
	readErr        error
	encodingResult string
	encodingErr    error
	writeErr       error
	writtenContent []byte // captures content passed to WriteFile
	listResult     string
	listErr        error
	uploadErr      error
	uploadCalled   bool // tracks whether UploadFile was invoked
	downloadErr    error
	downloadCalled bool // tracks whether DownloadFile was invoked
	checkErr       error
}

func (m *toolsMockExecutor) RunCommand(_ context.Context, _ ssh.RunRequest) (ssh.RunResult, error) {
	return m.runResult, m.runErr
}
func (m *toolsMockExecutor) ReadFile(_ context.Context, _ string) ([]byte, error) {
	return m.readContent, m.readErr
}
func (m *toolsMockExecutor) DetectEncoding(_ context.Context, _ string) (string, error) {
	return m.encodingResult, m.encodingErr
}
func (m *toolsMockExecutor) WriteFile(_ context.Context, _ string, content []byte) error {
	m.writtenContent = content
	return m.writeErr
}
func (m *toolsMockExecutor) ListDir(_ context.Context, _ string) (string, error) {
	return m.listResult, m.listErr
}
func (m *toolsMockExecutor) UploadFile(_ context.Context, _, _ string) error {
	m.uploadCalled = true
	return m.uploadErr
}
func (m *toolsMockExecutor) DownloadFile(_ context.Context, _, _ string) error {
	m.downloadCalled = true
	return m.downloadErr
}
func (m *toolsMockExecutor) CheckSocket(_ context.Context) error {
	return m.checkErr
}

// statusOutput mirrors the JSON fields of StatusOutput for assertion.
type statusOutput struct {
	Connected  bool   `json:"connected"`
	SocketPath string `json:"socket_path"`
	Hint       string `json:"hint"`
}

// newTestServer builds a test MCP server with tools registered, connects a
// client via InMemoryTransport, and returns both the client session and a
// cleanup function. Pattern from internal/daemon/daemon_test.go.
func newTestServer(t *testing.T, exec ssh.SSHExecutor, cfg *config.Config) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	RegisterTools(server, exec, cfg)

	cTransport, sTransport := mcp.NewInMemoryTransports()

	ss, err := server.Connect(ctx, sTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { ss.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "testclient"}, nil)
	cs, err := client.Connect(ctx, cTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { cs.Close() })

	return cs
}

// TestSSHConnectionStatusToolRegistered verifies that ssh_connection_status
// appears in tools/list with readOnlyHint true when capabilities are all false.
// The tool must always be registered regardless of capability toggles (diagnostic).
func TestSSHConnectionStatusToolRegistered(t *testing.T) {
	cfg := &config.Config{
		SSHSocket: "/tmp/test.sock",
		SSHUser:   "user",
		SSHHost:   "host",
	}
	cs := newTestServer(t, &toolsMockExecutor{}, cfg)

	tools, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)

	var found bool
	for _, tool := range tools.Tools {
		if tool.Name == "ssh_connection_status" {
			found = true
			require.NotNil(t, tool.Annotations, "annotations must not be nil")
			require.True(t, tool.Annotations.ReadOnlyHint, "readOnlyHint must be true")
			break
		}
	}
	require.True(t, found, "ssh_connection_status must be in tools/list")
}

// TestSSHConnectionStatusLiveSocket verifies that when CheckSocket returns nil
// (socket alive), the tool returns connected:true, the correct socket_path,
// and result.IsError == false.
func TestSSHConnectionStatusLiveSocket(t *testing.T) {
	cfg := &config.Config{
		SSHSocket: "/tmp/test-live.sock",
		SSHUser:   "user",
		SSHHost:   "host",
	}
	// CheckErr is nil — socket is alive.
	cs := newTestServer(t, &toolsMockExecutor{checkErr: nil}, cfg)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_connection_status",
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "live socket must NOT set isError")

	require.NotEmpty(t, result.Content, "result must have content")
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "content[0] must be *mcp.TextContent")

	var out statusOutput
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.True(t, out.Connected, "connected must be true for live socket")
	require.Equal(t, cfg.SSHSocket, out.SocketPath, "socket_path must match config")
}

// TestSSHConnectionStatusDeadSocket verifies that when CheckSocket returns an error
// (socket dead), the tool returns connected:false, a non-empty hint containing
// the socket path, and result.IsError == false (CONN-01: dead socket is a normal
// diagnostic answer, not a tool failure).
func TestSSHConnectionStatusDeadSocket(t *testing.T) {
	cfg := &config.Config{
		SSHSocket: "/tmp/test-dead.sock",
		SSHUser:   "user",
		SSHHost:   "host",
	}
	// CheckErr set — socket is dead.
	deadErr := errors.New("ControlMaster socket /tmp/test-dead.sock is not alive: run 'ssh -M -S /tmp/test-dead.sock user@host' to re-establish")
	cs := newTestServer(t, &toolsMockExecutor{checkErr: deadErr}, cfg)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_connection_status",
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "dead socket must NOT set isError (CONN-01)")

	require.NotEmpty(t, result.Content, "result must have content")
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "content[0] must be *mcp.TextContent")

	var out statusOutput
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.False(t, out.Connected, "connected must be false for dead socket")
	require.Equal(t, cfg.SSHSocket, out.SocketPath, "socket_path must match config")
	require.Contains(t, out.Hint, cfg.SSHSocket, "hint must contain the socket path")

	// StructuredContent must mirror the text content — returning StatusOutput{}
	// (empty) instead of the populated out would leave these fields blank.
	// InMemoryTransport round-trips through JSON so StructuredContent arrives as map[string]any.
	sc, ok := result.StructuredContent.(map[string]any)
	require.True(t, ok, "StructuredContent must be map[string]any after transport round-trip")
	require.Equal(t, false, sc["connected"], "structuredContent.connected must be false")
	require.Equal(t, cfg.SSHSocket, sc["socket_path"], "structuredContent.socket_path must match config")
	hint, _ := sc["hint"].(string)
	require.Contains(t, hint, cfg.SSHSocket, "structuredContent.hint must contain the socket path")
}

// TestRegisterToolsCapabilityGating verifies that with all capabilities false,
// only ssh_connection_status is registered (not ssh_exec, ssh_read_file, etc.).
func TestRegisterToolsCapabilityGating(t *testing.T) {
	cfg := &config.Config{
		SSHSocket: "/tmp/test.sock",
		SSHUser:   "user",
		SSHHost:   "host",
		// All capabilities default to false.
	}
	cs := newTestServer(t, &toolsMockExecutor{}, cfg)

	toolsList, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)

	// Only ssh_connection_status should be present when all caps are false.
	require.Len(t, toolsList.Tools, 1, "only ssh_connection_status should be registered")
	require.Equal(t, "ssh_connection_status", toolsList.Tools[0].Name)
}
