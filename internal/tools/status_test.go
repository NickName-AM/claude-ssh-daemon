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
	runCalled      bool   // tracks whether RunCommand was invoked
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
	m.runCalled = true
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

// newTestServer builds a test MCP server with tools registered, connects a
// client via InMemoryTransport, and returns the client session.
// registry is a map[string]ssh.SSHExecutor built by each per-tool helper.
// Pattern from internal/daemon/daemon_test.go; updated for multi-host (Phase 7).
func newTestServer(t *testing.T, registry map[string]ssh.SSHExecutor, cfg *config.Config) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()

	server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)
	RegisterTools(server, registry, cfg)

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

// singleHostRegistry builds a single-host registry and corresponding cfg.Hosts
// for backward-compat single-host tests (default routing, MHST-06).
//
// CONTRACT: this helper mutates cfg.Hosts and cfg.DefaultHost in place.
// Callers must not read those fields with pre-call expectations after calling
// this helper, and must not pass a cfg whose Hosts or DefaultHost fields are
// checked elsewhere (WR-003: silent mutation can cause confusing test failures).
func singleHostRegistry(exec ssh.SSHExecutor, cfg *config.Config) map[string]ssh.SSHExecutor {
	cfg.Hosts = map[string]config.HostConfig{
		"default": {Socket: cfg.SSHSocket, User: cfg.SSHUser, Host: cfg.SSHHost},
	}
	cfg.DefaultHost = "default"
	return map[string]ssh.SSHExecutor{"default": exec}
}

// hostStatusOut mirrors HostStatus JSON for assertion in status tests.
type hostStatusOut struct {
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	Socket    string `json:"socket"`
	Hint      string `json:"hint"`
}

// statusOutputV2 mirrors StatusOutput JSON for assertion in status tests (D-09).
type statusOutputV2 struct {
	DefaultHost string          `json:"default_host"`
	Hosts       []hostStatusOut `json:"hosts"`
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
	registry := singleHostRegistry(&toolsMockExecutor{}, cfg)
	cs := newTestServer(t, registry, cfg)

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

// TestSSHConnectionStatusSingleHostLiveSocket verifies that when CheckSocket returns
// nil (socket alive), the tool returns connected:true for the single host and
// result.IsError == false. Uses the v2.0 schema (D-09).
func TestSSHConnectionStatusSingleHostLiveSocket(t *testing.T) {
	cfg := &config.Config{
		SSHSocket: "/tmp/test-live.sock",
		SSHUser:   "user",
		SSHHost:   "host",
	}
	registry := singleHostRegistry(&toolsMockExecutor{checkErr: nil}, cfg)
	cs := newTestServer(t, registry, cfg)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_connection_status",
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "live socket must NOT set isError")

	require.NotEmpty(t, result.Content, "result must have content")
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "content[0] must be *mcp.TextContent")

	var out statusOutputV2
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.Equal(t, "default", out.DefaultHost, "default_host must be 'default'")
	require.Len(t, out.Hosts, 1, "single-host config must produce one host entry")
	require.Equal(t, "default", out.Hosts[0].Name)
	require.True(t, out.Hosts[0].Connected, "connected must be true for live socket")
	require.Equal(t, cfg.SSHSocket, out.Hosts[0].Socket, "socket must match config")
}

// TestSSHConnectionStatusSingleHostDeadSocket verifies that when CheckSocket
// returns an error, the tool returns connected:false with a non-empty hint and
// result.IsError == false (CONN-01: dead socket is a normal diagnostic answer).
// Uses the v2.0 schema (D-09).
func TestSSHConnectionStatusSingleHostDeadSocket(t *testing.T) {
	cfg := &config.Config{
		SSHSocket: "/tmp/test-dead.sock",
		SSHUser:   "user",
		SSHHost:   "host",
	}
	deadErr := errors.New("ControlMaster socket /tmp/test-dead.sock is not alive: run 'ssh -M -S /tmp/test-dead.sock user@host' to re-establish")
	registry := singleHostRegistry(&toolsMockExecutor{checkErr: deadErr}, cfg)
	cs := newTestServer(t, registry, cfg)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_connection_status",
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "dead socket must NOT set isError (CONN-01)")

	require.NotEmpty(t, result.Content, "result must have content")
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "content[0] must be *mcp.TextContent")

	var out statusOutputV2
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.Equal(t, "default", out.DefaultHost)
	require.Len(t, out.Hosts, 1)
	require.Equal(t, "default", out.Hosts[0].Name)
	require.False(t, out.Hosts[0].Connected, "connected must be false for dead socket")
	require.Equal(t, cfg.SSHSocket, out.Hosts[0].Socket)
	require.Contains(t, out.Hosts[0].Hint, cfg.SSHSocket, "hint must contain the socket path")
}

// TestSSHConnectionStatusMultiHostSortedOutput verifies that when multiple hosts
// are configured, the status response lists them in sorted order with per-host
// connected/socket/hint fields (MHST-09).
func TestSSHConnectionStatusMultiHostSortedOutput(t *testing.T) {
	webSock := "/tmp/ssh-web.sock"
	dbSock := "/tmp/ssh-db.sock"

	webMock := &toolsMockExecutor{checkErr: nil}
	dbDeadErr := errors.New("ControlMaster socket /tmp/ssh-db.sock is not alive: run 'ssh ...' to re-establish")
	dbMock := &toolsMockExecutor{checkErr: dbDeadErr}

	registry := map[string]ssh.SSHExecutor{
		"web": webMock,
		"db":  dbMock,
	}
	cfg := &config.Config{
		MCPSocket:   "/tmp/mcp.sock",
		DefaultHost: "web",
		Hosts: map[string]config.HostConfig{
			"web": {Socket: webSock, User: "ubuntu", Host: "web.example.com"},
			"db":  {Socket: dbSock, User: "ubuntu", Host: "db.example.com"},
		},
	}

	cs := newTestServer(t, registry, cfg)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_connection_status",
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "multi-host status must not set isError even with dead hosts (CONN-01)")

	require.NotEmpty(t, result.Content)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var out statusOutputV2
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))

	// DefaultHost must be "web" (the configured default)
	require.Equal(t, "web", out.DefaultHost)

	// Hosts must be in sorted order: db before web
	require.Len(t, out.Hosts, 2, "must have exactly 2 host entries")
	require.Equal(t, "db", out.Hosts[0].Name, "sorted: db must come before web")
	require.Equal(t, "web", out.Hosts[1].Name, "sorted: web must come after db")

	// db is dead
	require.False(t, out.Hosts[0].Connected, "db must be connected:false")
	require.NotEmpty(t, out.Hosts[0].Hint, "dead db must have a non-empty hint")
	require.Equal(t, dbSock, out.Hosts[0].Socket, "db socket must match config")

	// web is live
	require.True(t, out.Hosts[1].Connected, "web must be connected:true")
	require.Empty(t, out.Hosts[1].Hint, "live web must have empty hint")
	require.Equal(t, webSock, out.Hosts[1].Socket, "web socket must match config")
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
	registry := singleHostRegistry(&toolsMockExecutor{}, cfg)
	cs := newTestServer(t, registry, cfg)

	toolsList, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)

	// Only ssh_connection_status should be present when all caps are false.
	require.Len(t, toolsList.Tools, 1, "only ssh_connection_status should be registered")
	require.Equal(t, "ssh_connection_status", toolsList.Tools[0].Name)
}
