//go:build darwin || linux

// Package daemon implements the Unix socket lifecycle, MCP server wiring,
// sequential accept loop, and graceful shutdown for claude-ssh-daemon.
package daemon

import (
	"fmt"
	"net"
	"os"
	"syscall"
)

// createSocket creates a Unix domain socket at the given path with mode 0600
// and no exploitable race window (CVE-2023-45145 mitigation, SECU-03).
//
// The sequence is:
//  1. Remove any stale socket file from a prior run (EADDRINUSE prevention).
//  2. Set umask to 0o777 BEFORE net.Listen — the socket file is created with
//     mode 000, closing the window where another local user could connect
//     between listen() and chmod().
//  3. Restore the umask IMMEDIATELY after net.Listen — do NOT defer.
//  4. os.Chmod(path, 0o600) sets the final owner-read+write-only permission.
func createSocket(path string) (net.Listener, error) {
	// Remove stale socket from a previous run. Ignore "not found" errors;
	// any other error means we genuinely cannot proceed.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove stale socket: %w", err)
	}

	// Set umask to 0777 BEFORE net.Listen so the socket file is created with
	// mode 000. This closes the race window between listen() and chmod().
	// Do NOT use defer — restore must happen on the very next line.
	origUmask := syscall.Umask(0o777)
	ln, err := net.Listen("unix", path)
	syscall.Umask(origUmask) // Restore immediately; NOT deferred.
	if err != nil {
		return nil, fmt.Errorf("listen unix: %w", err)
	}

	// Set final permissions: owner read+write only (SECU-03).
	// Socket was created with mode 000; chmod 0600 here has no race window.
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, fmt.Errorf("chmod socket: %w", err)
	}
	return ln, nil
}
