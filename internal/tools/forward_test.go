package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
	"github.com/NickName-AM/claude-ssh-daemon/internal/forward"
	"github.com/NickName-AM/claude-ssh-daemon/internal/ssh"
)

// TestListForwardsEmptyReturnsArray verifies that listForwardsHandler with an
// empty Forwarder returns a non-nil empty Forwards slice that JSON-marshals to
// [] rather than null (Pitfall 7, T-11-09).
func TestListForwardsEmptyReturnsArray(t *testing.T) {
	handler := listForwardsHandler(forward.NewForwarder())
	result, out, err := handler(context.Background(), nil, struct{}{})
	require.NoError(t, err, "listForwardsHandler must not return a non-nil error")
	require.Nil(t, result, "listForwardsHandler on empty forwarder must return nil *CallToolResult (no error)")
	require.NotNil(t, out.Forwards, "Forwards must be non-nil so it marshals to JSON [] not null")
	require.Len(t, out.Forwards, 0, "empty Forwarder must yield 0 forwards")

	// Confirm JSON serialisation: the slice must render as [] not null.
	b, err := json.Marshal(out)
	require.NoError(t, err)
	require.Contains(t, string(b), `"forwards":[]`, "Forwards must JSON-marshal to []")
}

// TestForwardPortDuplicate verifies the D-02 duplicate-check branch: when the
// Forwarder already holds a key matching the host+port that allocatePortFn
// returns, forwardPortHandler must return IsError true and must NOT start any
// subprocess (the duplicate check at step 4 precedes StartForward at step 5).
func TestForwardPortDuplicate(t *testing.T) {
	const fixedPort = 54321
	const hostName = "web"

	// Override allocatePortFn for the duration of this test so the handler
	// always computes the same key (forward.Key(hostName, fixedPort)).
	orig := allocatePortFn
	allocatePortFn = func() (int, error) { return fixedPort, nil }
	t.Cleanup(func() { allocatePortFn = orig })

	// Build config with PortForward enabled and one named host.
	cfg := &config.Config{
		MCPSocket:   "/tmp/test-mcp.sock",
		DefaultHost: hostName,
		Capabilities: config.Capabilities{
			PortForward: true,
		},
		Hosts: map[string]config.HostConfig{
			hostName: {Socket: "/tmp/ssh-web.sock", User: "ubuntu", Host: "web.example.com"},
		},
	}
	registry := map[string]ssh.SSHExecutor{
		hostName: &toolsMockExecutor{},
	}

	// Pre-seed the Forwarder so the key forward.Key(hostName, fixedPort) already exists.
	fwd := forward.NewForwarder()
	fwd.Store(forward.Key(hostName, fixedPort), &forward.ForwardEntry{
		Socket:    "/tmp/ssh-web.sock",
		User:      "ubuntu",
		LocalPort: fixedPort,
		HostName:  hostName,
	})

	handler := forwardPortHandler(registry, cfg, fwd)
	result, _, err := handler(context.Background(), nil, ForwardPortInput{
		RemoteHost: "db.internal",
		RemotePort: 5432,
		Host:       hostName,
	})

	require.NoError(t, err, "handler must not return a non-nil error on duplicate")
	require.NotNil(t, result, "handler must return a non-nil *CallToolResult on duplicate")
	require.True(t, result.IsError, "IsError must be true on duplicate key (D-02)")

	// The error text must mention both the port and the host.
	require.NotEmpty(t, result.Content)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok, "content[0] must be *mcp.TextContent")
	require.Contains(t, text.Text, "54321", "error text must mention the duplicate port")
	require.Contains(t, text.Text, hostName, "error text must mention the host name")
}

// TestForwardPortUnknownHost verifies that forwardPortHandler returns IsError
// true when the requested host is not in the registry (resolveExecutor miss path).
func TestForwardPortUnknownHost(t *testing.T) {
	cfg := &config.Config{
		MCPSocket:   "/tmp/test-mcp.sock",
		DefaultHost: "web",
		Capabilities: config.Capabilities{
			PortForward: true,
		},
		Hosts: map[string]config.HostConfig{
			"web": {Socket: "/tmp/ssh-web.sock", User: "ubuntu", Host: "web.example.com"},
		},
	}
	registry := map[string]ssh.SSHExecutor{
		"web": &toolsMockExecutor{},
	}

	handler := forwardPortHandler(registry, cfg, forward.NewForwarder())
	result, _, err := handler(context.Background(), nil, ForwardPortInput{
		Host:       "no-such-host",
		RemoteHost: "x",
		RemotePort: 1,
	})

	require.NoError(t, err, "handler must not return a non-nil error on unknown host")
	require.NotNil(t, result, "handler must return a non-nil *CallToolResult on unknown host")
	require.True(t, result.IsError, "IsError must be true for unknown host")
}
