// Package forward manages the lifecycle of ssh -L port-forward subprocesses.
// It provides a mutex-protected registry of active forwards (Forwarder), the
// ForwardEntry type that tracks per-forward state, and stdlib helpers for port
// allocation, subprocess launch, and readiness polling.
package forward

import (
	"fmt"
	"net"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// ForwardEntry holds the state for a single active ssh -L port forward (D-05).
type ForwardEntry struct {
	Cmd        *exec.Cmd
	LocalPort  int
	RemoteHost string
	RemotePort int
	HostName   string
	StartedAt  time.Time
	exited     atomic.Bool // set to true by the reaper goroutine after cmd.Wait() returns
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
// Callers can use this to evict dead entries (status "dead") to prevent
// unbounded accumulation in long-running daemon processes (WR-03).
func (f *Forwarder) Delete(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.entries, key)
}

// Snapshot returns a copied slice of all current entries for safe iteration.
// The returned slice is always non-nil — an empty registry yields []ForwardEntry{},
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

// KillAll sends SIGKILL to every live forward process.
// Called by daemon.Run in the shutdown path (D-07).
// Kill() is a non-blocking signal send — it does not wait for exit.
// Do NOT call cmd.Wait() here; each forward has its own background Wait goroutine
// (started in startForward) that reaps the process and sets entry.exited.
func (f *Forwarder) KillAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, entry := range f.entries {
		if entry.Cmd.Process != nil && !entry.exited.Load() {
			_ = entry.Cmd.Process.Kill()
		}
	}
}

// HasExited reports whether the ssh subprocess has already exited.
// The reaper goroutine sets this atomically after cmd.Wait() returns.
// Use HasExited instead of reading cmd.ProcessState directly to avoid data races.
func HasExited(entry *ForwardEntry) bool {
	return entry.exited.Load()
}

// Status returns "dead" if the process has exited (the reaper goroutine has
// set entry.exited after cmd.Wait() returns), or "running" otherwise (D-06, D-10).
func Status(entry *ForwardEntry) string {
	if entry.exited.Load() {
		return "dead"
	}
	return "running"
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
// If another process claims the port, ssh -L will fail and pollReady will surface
// the error within the 500ms budget.
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

// StartForward is the exported entry point for launching an ssh -L subprocess.
// See startForward for full documentation.
func StartForward(socket, user, host string, localPort int, remoteHost string, remotePort int) (*ForwardEntry, error) {
	return startForward(socket, user, host, localPort, remoteHost, remotePort)
}

// startForward launches ssh -L as a long-lived background process via os/exec.
// The subprocess uses -S (ControlMaster socket), -N (no remote command), and
// BatchMode=yes to prevent interactive prompts (D-11).
//
// Orphan prevention (D-12) is handled by setSysProcAttr, which is implemented
// in platform-specific files:
//   - forward_linux.go:   Pdeathsig: syscall.SIGTERM (kernel parent-death signal)
//   - forward_other.go:   Setpgid: true (child in own process group; KillAll signals it)
//
// Pdeathsig is a Linux/FreeBSD-only syscall field; referencing it on macOS causes
// a compile error (Pitfall 1). Build-constrained files are the only correct approach
// (the runtime.GOOS check is not sufficient because the compiler evaluates struct
// literals at compile time regardless of the enclosing branch).
//
// After a successful Start, a background goroutine calls cmd.Wait() so the
// process is reaped when it exits. entry.exited is set atomically to avoid
// data races on ProcessState (CR-01, Pitfall 4).
func startForward(socket, user, host string, localPort int, remoteHost string, remotePort int) (*ForwardEntry, error) {
	lSpec := fmt.Sprintf("%d:%s:%d", localPort, remoteHost, remotePort)
	cmd := exec.Command("ssh",
		"-L", lSpec,
		"-S", socket,
		"-N",
		"-o", "BatchMode=yes",
		user+"@"+host,
	)

	// Platform-specific orphan prevention (D-12).
	// Implemented in forward_linux.go (Pdeathsig) and forward_other.go (Setpgid).
	setSysProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ssh forward: %w", err)
	}

	entry := &ForwardEntry{
		Cmd: cmd,
	}

	// Reap the subprocess when it exits so it does not become a zombie.
	// entry.exited is set after Wait returns, providing a race-free signal
	// for Status() and KillAll() (CR-01).
	go func() {
		_ = cmd.Wait()
		entry.exited.Store(true)
	}()

	return entry, nil
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
