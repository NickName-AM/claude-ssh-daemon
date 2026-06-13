// Package forward manages the lifecycle of ssh -L port-forward requests via the
// ControlMaster mux protocol (ssh -O forward / ssh -O cancel).
//
// Design change from subprocess-tracking model:
// The original implementation tracked an ssh -L subprocess per forward. This was
// wrong: when ssh is invoked with -S <socket>, it contacts the ControlMaster via
// the mux protocol, negotiates the port binding, then exits immediately with
// status 0. The ControlMaster process owns the listening port. Tracking the
// already-dead child meant KillAll() had nothing to kill and forwards survived
// daemon shutdown (root cause of T-11-UAT-3 failure).
//
// The correct model:
//   - Creation: ssh -O forward -S <socket> -L spec user@host (exits after mux req)
//   - Teardown: ssh -O cancel -S <socket> -L spec user@host (mux req to release port)
//   - Registry stores the parameters needed to reconstruct the cancel command.
package forward

import (
	"fmt"
	"net"
	"os/exec"
	"sync"
	"time"
)

// ForwardEntry holds the parameters for a single active ssh -L port forward.
// No subprocess handle is kept — the ControlMaster owns the port; teardown
// uses ssh -O cancel with the same parameters used for creation.
type ForwardEntry struct {
	Socket     string    // ControlMaster socket path (-S arg)
	User       string    // SSH user (user@host)
	LocalPort  int       // allocated local port
	RemoteHost string    // remote forwarding target host
	RemotePort int       // remote forwarding target port
	HostName   string    // config host name (registry key prefix)
	StartedAt  time.Time // when the forward was established
}

// Forwarder is a mutex-protected registry of active ForwardEntry instances.
// The map key format is "hostName:localPort" (D-04).
// Constructed once in daemon.Run and passed to tool handlers via closure (D-03).
type Forwarder struct {
	mu      sync.Mutex
	entries map[string]*ForwardEntry
}

// NewForwarder returns an initialised Forwarder with an empty entries map.
func NewForwarder() *Forwarder {
	return &Forwarder{entries: make(map[string]*ForwardEntry)}
}

// Key returns the canonical registry key for a host-port pair (D-04).
// Format: "hostName:localPort" — e.g. "web:8080".
func Key(hostName string, localPort int) string {
	return fmt.Sprintf("%s:%d", hostName, localPort)
}

// Has reports whether key exists in the registry.
// Used for duplicate-forward detection before starting a new forward (D-02).
func (f *Forwarder) Has(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.entries[key]
	return ok
}

// Store inserts entry under key.
// Callers MUST call Store only after pollReady succeeds so that a stale entry
// is never left in the registry on a failed readiness check (Pitfall 3).
func (f *Forwarder) Store(key string, entry *ForwardEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries[key] = entry
}

// Delete removes the entry for key from the registry.
// Callers can use this to evict cancelled entries to prevent unbounded
// accumulation in long-running daemon processes (WR-03).
func (f *Forwarder) Delete(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.entries, key)
}

// Snapshot returns a copied slice of all current entries for safe iteration.
// The returned slice is always non-nil — an empty registry yields []*ForwardEntry{},
// which marshals to JSON [] rather than null (Pitfall 7).
func (f *Forwarder) Snapshot() []*ForwardEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*ForwardEntry, 0, len(f.entries))
	for _, e := range f.entries {
		out = append(out, e)
	}
	return out
}

// CancelAll sends ssh -O cancel for every registered forward.
// Called by daemon.Run in the shutdown path (D-07).
// Errors are silently ignored — if the ControlMaster is already gone the
// forward is already dead; there is nothing useful to do.
//
// CancelAll replaces the old KillAll() which sent SIGKILL to subprocess
// handles that were already dead (T-11-UAT-3 root cause).
func (f *Forwarder) CancelAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, entry := range f.entries {
		_ = cancelForward(entry.Socket, entry.User, entry.HostName, entry.LocalPort, entry.RemoteHost, entry.RemotePort)
	}
}

// AllocatePort is the exported entry point for port allocation.
// Exposed so that internal/tools can declare a test seam:
//
//	var allocatePortFn = forward.AllocatePort
//
// Tests override allocatePortFn to return a fixed port without spawning ssh.
func AllocatePort() (int, error) { return allocatePort() }

// allocatePort finds a free TCP port on 127.0.0.1 using kernel port-0 assignment.
// The listener is closed immediately after reading the port so that ssh can bind it.
//
// TOCTOU: the port is released before ssh claims it. A concurrent process could
// claim the port in this window. Race is accepted per SAFE-01 precedent (D-08).
// If another process claims the port, ssh -O forward will fail and pollReady will
// surface the error within the 500ms budget.
func allocatePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("allocate local port: %w", err)
	}
	// TCPListener.Addr() always returns *net.TCPAddr — type assertion is safe here.
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port, nil
}

// CreateForward is the exported entry point for establishing an ssh -L forward.
// See createForward for full documentation.
func CreateForward(socket, user, host string, localPort int, remoteHost string, remotePort int) (*ForwardEntry, error) {
	return createForward(socket, user, host, localPort, remoteHost, remotePort)
}

// createForward establishes a port forward via the ControlMaster mux protocol.
//
// It runs: ssh -O forward -S <socket> -L localPort:remoteHost:remotePort user@host
//
// The ssh process exits immediately after the mux negotiation succeeds (with
// exit status 0). The ControlMaster process takes ownership of the listening
// port. createForward waits for the ssh process to exit and returns a
// ForwardEntry with the parameters needed to cancel the forward later.
//
// On failure (non-zero exit), the ControlMaster rejected the forward request.
// No port will be listening, so no cleanup is needed.
func createForward(socket, user, host string, localPort int, remoteHost string, remotePort int) (*ForwardEntry, error) {
	lSpec := fmt.Sprintf("%d:%s:%d", localPort, remoteHost, remotePort)
	cmd := exec.Command("ssh",
		"-O", "forward",
		"-S", socket,
		"-L", lSpec,
		user+"@"+host,
	)

	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ssh -O forward: %w (output: %s)", err, string(out))
	}

	return &ForwardEntry{
		Socket:     socket,
		User:       user,
		RemoteHost: remoteHost,
		RemotePort: remotePort,
		// HostName and LocalPort are set by the caller after Store (tools/forward.go).
	}, nil
}

// CancelForward is the exported entry point for cancelling a single forward.
// See cancelForward for full documentation.
func CancelForward(socket, user, host string, localPort int, remoteHost string, remotePort int) error {
	return cancelForward(socket, user, host, localPort, remoteHost, remotePort)
}

// cancelForward releases a port forward via the ControlMaster mux protocol.
//
// It runs: ssh -O cancel -S <socket> -L localPort:remoteHost:remotePort user@host
//
// The ssh process exits immediately after the mux negotiation (with exit
// status 0 on success). The ControlMaster releases the listening port.
func cancelForward(socket, user, host string, localPort int, remoteHost string, remotePort int) error {
	lSpec := fmt.Sprintf("%d:%s:%d", localPort, remoteHost, remotePort)
	cmd := exec.Command("ssh",
		"-O", "cancel",
		"-S", socket,
		"-L", lSpec,
		user+"@"+host,
	)

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ssh -O cancel: %w (output: %s)", err, string(out))
	}
	return nil
}

// PollReady is the exported entry point for readiness polling.
// See pollReady for full documentation.
func PollReady(localPort int) error { return pollReady(localPort) }

// pollReady polls the local port until ssh binds it or the attempt budget expires.
// Polls 10 times with 50ms sleep before each retry (500ms total budget) (D-13).
// The sleep is placed before the dial on iterations 1–9 (not after iteration 9)
// so the worst-case latency is exactly 9×50ms = 450ms before the final attempt,
// avoiding an unnecessary trailing sleep on the last failed iteration (WR-02).
// Returns a non-nil error if the port never becomes reachable.
func pollReady(localPort int) error {
	addr := fmt.Sprintf("127.0.0.1:%d", localPort)
	for i := 0; i < 10; i++ {
		if i > 0 {
			time.Sleep(50 * time.Millisecond)
		}
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			conn.Close()
			return nil
		}
	}
	return fmt.Errorf("ssh forward on port %d did not become reachable within 500ms", localPort)
}
