// Package main is the entry point for the claude-ssh-daemon binary.
// It installs signal handling for SIGTERM/SIGINT, loads the JSON config,
// and delegates to daemon.Run.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
	"github.com/NickName-AM/claude-ssh-daemon/internal/daemon"
)

func main() {
	// Install SIGTERM/SIGINT handler. Cancelling ctx drives the daemon's
	// graceful shutdown sequence (DMON-02). defer stop() releases the
	// os/signal registration when main returns.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Load config from ~/.config/claude-ssh-daemon/config.json (DMON-01).
	// On any error, log and exit non-zero — do not start with a broken config.
	cfg, err := config.Load()
	if err != nil {
		slog.Error("config load failed", "error", err)
		os.Exit(1)
	}

	// Run the daemon; it blocks until ctx is cancelled or an unrecoverable
	// error occurs. Non-zero exit on any error returned.
	if err := daemon.Run(ctx, cfg); err != nil {
		slog.Error("daemon exited with error", "error", err)
		os.Exit(1)
	}
}
