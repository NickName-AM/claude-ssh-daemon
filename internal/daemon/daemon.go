package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
	"github.com/NickName-AM/claude-ssh-daemon/internal/forward"
	"github.com/NickName-AM/claude-ssh-daemon/internal/ssh"
	"github.com/NickName-AM/claude-ssh-daemon/internal/tools"
)

// logger is the package-level structured JSON logger writing to stderr.
// Set once at startup; never recreated per-request (RESEARCH.md Pattern 7, D-11, D-12).
var logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))

// logCapabilities emits a "tool not registered" log line for each capability
// that is disabled in the config (SECU-01 registration enforcement).
// In Phase 1 no tools are registered regardless; this log is the observable
// signal that disabled capabilities are not wired in.
func logCapabilities(c config.Capabilities) {
	if !c.Exec {
		logger.Info("tool not registered", "capability", "exec", "reason", "disabled in config")
	}
	if !c.FileRead {
		logger.Info("tool not registered", "capability", "file_read", "reason", "disabled in config")
	}
	if !c.FileWrite {
		logger.Info("tool not registered", "capability", "file_write", "reason", "disabled in config")
	}
	if !c.PortForward {
		logger.Info("tool not registered", "capability", "port_forward", "reason", "disabled in config")
	}
}

// acceptLoop accepts connections from ln and serves each one sequentially via
// the MCP server. The loop runs in its own goroutine (Pitfall 6 — sequential
// loop must not block the shutdown path in Run).
//
// On accept error:
//   - net.ErrClosed means the listener was closed (shutdown path) — return nil.
//   - Any other error is logged and the loop exits.
//
// Each accepted connection is wrapped in an IOTransport, connected to the
// server via server.Connect (not the one-shot Run method), then drained with
// ss.Wait() before accepting the next connection (D-10 sequential accept).
//
// activeSess is set to the current ServerSession for the duration of ss.Wait()
// so that Run can call ss.Close() when the drain timeout fires (WR-01).
func acceptLoop(ctx context.Context, ln net.Listener, server *mcp.Server, activeSess *atomic.Pointer[mcp.ServerSession]) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			logger.Error("accept failed", "error", err)
			return
		}
		logger.Info("client connected")
		t := connTransport(conn)
		ss, err := server.Connect(ctx, t, nil)
		if err != nil {
			logger.Error("mcp connect failed", "error", err)
			conn.Close()
			continue
		}
		// Publish the active session so Run can close it on drain timeout.
		activeSess.Store(ss)
		// Block until this session ends — sequential per D-10.
		if err := ss.Wait(); err != nil {
			logger.Warn("session ended with error", "error", err)
		}
		activeSess.Store(nil)
		conn.Close() // always close the underlying connection after session ends
		logger.Info("client disconnected")
	}
}

// Run is the daemon entry point. It:
//  1. Creates the Unix socket at cfg.MCPSocket with mode 0600 (SECU-03).
//  2. Initializes the go-sdk MCP server with no tools registered (SECU-01, D-08).
//  3. Logs each disabled capability as "tool not registered" (SECU-01).
//  4. Logs "daemon started" with the socket path (startup log requirement).
//  5. Runs the accept loop in a goroutine (Pitfall 6 fix).
//  6. Blocks until ctx is cancelled (SIGTERM/SIGINT via signal.NotifyContext).
//  7. Closes the listener to unblock Accept().
//  8. Waits for the accept loop to exit, bounded by a 5s drain timeout (DMON-02).
//  9. Removes the socket file (DMON-02).
// 10. Returns nil.
func Run(ctx context.Context, cfg *config.Config) error {
	ln, err := createSocket(cfg.MCPSocket)
	if err != nil {
		return fmt.Errorf("create socket: %w", err)
	}

	// Initialize MCP server with no tools registered — empty tools/list is the
	// intended Phase 1 behavior; tools are added in Phase 2 (D-08, SECU-01).
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "claude-ssh-daemon",
		Version: "0.1.0",
	}, nil)

	// Log capability status for each feature toggle (SECU-01 enforcement signal).
	logCapabilities(cfg.Capabilities)

	// Startup log — includes socket path per §Specific Ideas.
	logger.Info("daemon started", "mcp_socket", cfg.MCPSocket)

	// Register MCP tools — must happen before acceptLoop starts (Pitfall 3).
	// Build executor registry from cfg.Hosts (populated by config.Validate()).
	// daemon.go owns the dependency injection root — one ControlMasterExecutor per host.
	registry := make(map[string]ssh.SSHExecutor, len(cfg.Hosts))
	for name, h := range cfg.Hosts {
		registry[name] = &ssh.ControlMasterExecutor{
			Socket: h.Socket,
			User:   h.User,
			Host:   h.Host,
		}
	}
	fwd := forward.NewForwarder()
	tools.RegisterTools(server, registry, cfg, fwd)

	// activeSess holds the session currently blocked in ss.Wait(), if any.
	// The accept loop stores/clears it around each session so that the drain
	// timeout path below can call ss.Close() to unblock it (WR-01).
	var activeSess atomic.Pointer[mcp.ServerSession]

	// Run the accept loop in a goroutine so the main goroutine can service
	// ctx.Done() without being blocked behind ss.Wait() (Pitfall 6).
	done := make(chan struct{})
	go func() {
		defer close(done)
		acceptLoop(ctx, ln, server, &activeSess)
	}()

	// Block until shutdown signal (SIGTERM/SIGINT via signal.NotifyContext).
	<-ctx.Done()
	logger.Info("shutdown signal received")

	// Cancel all active port forwards before closing the listener (D-07, T-11-08).
	// CancelAll sends ssh -O cancel for each registered forward so the ControlMaster
	// releases the listening port. Must precede ln.Close() so the daemon is still
	// running when the cancel commands are issued (Pitfall 6 ordering).
	fwd.CancelAll()

	// Close the listener to unblock any pending Accept() call.
	ln.Close()

	// Wait for the accept loop goroutine to exit, bounded by 5s (DMON-02).
	drainCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case <-done:
	case <-drainCtx.Done():
		// Close the active session to unblock ss.Wait() in the accept loop.
		// Without this, the goroutine would remain blocked until the client
		// disconnects, even though Run has returned (WR-01).
		if ss := activeSess.Load(); ss != nil {
			ss.Close()
		}
		logger.Warn("drain timeout exceeded; closing active session")
		// WR-001: wait for the accept goroutine to exit after ss.Close() unblocks
		// ss.Wait(). Without this bounded wait, the goroutine (activeSess.Store(nil),
		// conn.Close(), logger.Info) runs concurrently with or after Run() returns,
		// producing log lines in incorrect order and leaking the goroutine relative
		// to the Run() call lifetime.
		postCtx, postCancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer postCancel()
		select {
		case <-done:
		case <-postCtx.Done():
			logger.Warn("accept goroutine did not exit after session close")
		}
	}

	// Remove the socket file so subsequent daemon starts do not fail with
	// EADDRINUSE. Ignore "not found" (already removed); log other errors.
	removeErr := os.Remove(cfg.MCPSocket)
	socketRemoved := removeErr == nil || os.IsNotExist(removeErr)
	if removeErr != nil && !os.IsNotExist(removeErr) {
		logger.Error("failed to remove socket", "error", removeErr)
	}

	logger.Info("shutdown complete", "socket_removed", socketRemoved)
	return nil
}
