package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
)

// TestCreateSocketPermissions verifies that:
//  1. createSocket creates a listener and the socket file has mode 0600 (SECU-03).
//  2. Calling createSocket a second time on the same (stale) path succeeds —
//     the stale-socket removal prevents EADDRINUSE (Pitfall 1).
func TestCreateSocketPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.sock")

	// First call: socket is created fresh.
	ln, err := createSocket(path)
	require.NoError(t, err)
	defer ln.Close()

	// Stat the socket file and assert mode 0600 (SECU-03).
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
		"socket must have mode 0600 (owner read+write only)")

	// Close the first listener to leave a stale socket file on disk.
	ln.Close()

	// Second call on the same path: must succeed (stale-socket removal, no EADDRINUSE).
	ln2, err := createSocket(path)
	require.NoError(t, err, "createSocket on stale path must succeed (stale-socket removal)")
	defer ln2.Close()
}

// TestMCPServerEmptyToolList verifies that a tool-less mcp.NewServer returns an
// empty tools list to a connected client (SECU-01 — no tools registered in Phase 1).
//
// Uses mcp.NewInMemoryTransports() per CLAUDE.md §Testing and RESEARCH.md Pattern 8.
// Does NOT spawn the daemon binary.
func TestMCPServerEmptyToolList(t *testing.T) {
	ctx := context.Background()

	// Initialize a server with no tools registered — Phase 1 behavior (D-08).
	server := mcp.NewServer(&mcp.Implementation{Name: "test"}, nil)

	// NewInMemoryTransports returns two paired transports backed by net.Pipe.
	// Server must Connect before client (the client drives initialization).
	cTransport, sTransport := mcp.NewInMemoryTransports()

	ss, err := server.Connect(ctx, sTransport, nil)
	require.NoError(t, err)
	defer ss.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "testclient"}, nil)
	cs, err := client.Connect(ctx, cTransport, nil)
	require.NoError(t, err)
	defer cs.Close()

	// Verify the tools list is empty — Phase 1 server has no registered tools.
	tools, err := cs.ListTools(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, tools.Tools, "Phase 1 server should have no registered tools")
}

// TestRunRemovesSocketOnShutdown verifies that Run creates the socket file and
// removes it after the context is cancelled (DMON-02: graceful shutdown with
// socket removal on exit).
func TestRunRemovesSocketOnShutdown(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "mcp.sock")

	cfg := &config.Config{
		SSHSocket: "/tmp/ssh.sock",
		MCPSocket: sockPath,
		// All capabilities default to false.
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Run the daemon in a goroutine; capture its return error.
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, cfg)
	}()

	// Wait until the socket file appears (daemon is accepting connections).
	require.Eventually(t, func() bool {
		_, err := os.Stat(sockPath)
		return err == nil
	}, 2*time.Second, 20*time.Millisecond, "socket file must appear within 2s of daemon start")

	// Cancel the context to trigger graceful shutdown.
	cancel()

	// Run must return within 6 seconds (5s drain + margin).
	select {
	case err := <-errCh:
		require.NoError(t, err, "Run should return nil on clean shutdown")
	case <-time.After(6 * time.Second):
		t.Fatal("Run did not return within 6s after context cancellation (DMON-02)")
	}

	// The socket file must be gone after shutdown (DMON-02 socket removal).
	_, statErr := os.Stat(sockPath)
	require.True(t, os.IsNotExist(statErr),
		"socket file must not exist after daemon shutdown (DMON-02)")
}
