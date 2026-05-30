// Package daemon integration tests exercise the full transport path over a real
// Unix socket: daemon.Run starts in-process, an mcp.IOTransport wraps the raw
// net.Conn, and a real mcp.Client connects and makes MCP calls.
//
// These tests complement daemon_test.go (which uses mcp.NewInMemoryTransports)
// by validating socket permissions, the real transport path, and the clean-
// shutdown contract over an actual filesystem socket — behaviors that
// InMemoryTransport cannot exercise.
//
// IMPORTANT: Tests do NOT spawn the compiled daemon binary.  Per CLAUDE.md
// §Testing, daemon.Run is called in-process so tests remain fast, hermetic,
// and free of binary-path fragility.
//
// The link between these tests and the binary's shutdown is explicit:
// signal.NotifyContext in main.go translates SIGTERM/SIGINT into context
// cancellation, which is exactly what cancel() does here.  Asserting that
// Run returns promptly on cancel() therefore validates the binary's SIGTERM
// behaviour without spawning it.
package daemon

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
)

// startDaemon is a helper that builds a minimal *config.Config, launches
// daemon.Run in a goroutine, waits until the socket file appears (meaning the
// daemon is ready to accept connections), and returns the config plus the error
// channel.  The caller is responsible for cancelling ctx to trigger shutdown
// and draining errCh.
func startDaemon(t *testing.T, ctx context.Context) (*config.Config, <-chan error) {
	t.Helper()

	cfg := &config.Config{
		// Use a single-char socket name: t.TempDir() embeds the full test name,
		// and on macOS the resulting path can exceed the 104-byte sun_path limit.
		MCPSocket: filepath.Join(t.TempDir(), "s"),
		SSHSocket: "/tmp/ssh.sock", // not used in Phase 1; required by Validate()
		// All capabilities default to false.
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, cfg)
	}()

	// Wait until the socket file exists before returning so callers can
	// dial immediately after startDaemon returns.
	require.Eventually(t, func() bool {
		_, err := os.Stat(cfg.MCPSocket)
		return err == nil
	}, 2*time.Second, 10*time.Millisecond,
		"socket file must appear within 2s of daemon start")

	return cfg, errCh
}

// TestEndToEndConnectionStatusTool connects a real mcp.Client to the daemon over
// the real Unix socket and asserts that ssh_connection_status appears in the
// tools list (CONN-01, DMON-04 end-to-end). Also verifies mode 0600 (SECU-03).
//
// Previously named TestEndToEndEmptyToolList (Phase 1). Updated in Phase 2 because
// RegisterTools now always registers ssh_connection_status regardless of capabilities.
func TestEndToEndConnectionStatusTool(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, errCh := startDaemon(t, ctx)

	// SECU-03: assert the live socket mode is 0600.
	info, err := os.Stat(cfg.MCPSocket)
	require.NoError(t, err, "socket file must exist after daemon start")
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
		"socket must have mode 0600 (SECU-03)")

	// Dial the real Unix socket.
	conn, err := net.Dial("unix", cfg.MCPSocket)
	require.NoError(t, err, "net.Dial must succeed")
	defer conn.Close()

	// Wrap the raw net.Conn in an MCP IOTransport and connect a real client.
	// signal.NotifyContext in main.go translates OS signals into ctx cancellation;
	// the same ctx is passed to daemon.Run and here to the client — so shutdown
	// on ctx.Done() is the exact in-process equivalent of delivering SIGTERM.
	client := mcp.NewClient(&mcp.Implementation{Name: "itest"}, nil)
	cs, err := client.Connect(ctx, &mcp.IOTransport{Reader: conn, Writer: conn}, nil)
	require.NoError(t, err, "client.Connect must succeed")
	defer cs.Close()

	// Phase 2: ssh_connection_status is always registered (CONN-01, DMON-04).
	// With all capabilities false, it must be the only tool.
	tools, err := cs.ListTools(ctx, nil)
	require.NoError(t, err, "ListTools must succeed")
	require.Len(t, tools.Tools, 1, "only ssh_connection_status should be registered when all caps false")
	require.Equal(t, "ssh_connection_status", tools.Tools[0].Name)
	require.NotNil(t, tools.Tools[0].Annotations, "annotations must not be nil")
	require.True(t, tools.Tools[0].Annotations.ReadOnlyHint, "readOnlyHint must be true (SECU-02)")

	// Close the client session cleanly before cancelling the daemon.
	_ = cs.Close()

	// Cancel the daemon context and drain the error channel.
	cancel()
	select {
	case runErr := <-errCh:
		require.NoError(t, runErr, "Run must return nil on clean shutdown")
	case <-time.After(6 * time.Second):
		t.Fatal("Run did not return within 6s after context cancellation")
	}
}

// TestSigtermEquivalentCleanShutdown verifies that cancelling the daemon's
// context (the in-process equivalent of delivering SIGTERM via
// signal.NotifyContext in main.go) causes Run to return within the drain
// window (≤5s) with no error, and that the socket file is removed (DMON-02).
//
// This test covers the binary's shutdown behaviour without spawning it because
// signal.NotifyContext converts SIGTERM/SIGINT into exactly this ctx
// cancellation — main.go passes the same ctx to daemon.Run.
func TestSigtermEquivalentCleanShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg, errCh := startDaemon(t, ctx)

	// Cancel the context — in-process equivalent of SIGTERM (see note above).
	cancel()

	// Run must return within 6s (5s drain timeout + 1s margin per DMON-02).
	select {
	case runErr := <-errCh:
		require.NoError(t, runErr, "Run must return nil on clean shutdown (DMON-02)")
	case <-time.After(6 * time.Second):
		t.Fatal("Run did not return within 6s after context cancellation (DMON-02 timeout exceeded)")
	}

	// The socket file must be removed after daemon shutdown (DMON-02).
	_, statErr := os.Stat(cfg.MCPSocket)
	require.True(t, os.IsNotExist(statErr),
		"socket file must not exist after daemon shutdown (DMON-02)")
}
