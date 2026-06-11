package forward

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewForwarderEmptySnapshot guards Pitfall 7: empty registry must return a
// non-nil slice that marshals to [] not null.
func TestNewForwarderEmptySnapshot(t *testing.T) {
	f := NewForwarder()
	snap := f.Snapshot()
	require.NotNil(t, snap, "Snapshot() must return a non-nil slice")
	require.Len(t, snap, 0, "Snapshot() on empty Forwarder must have length 0")
}

// TestKey verifies the canonical key format used as registry map key.
func TestKey(t *testing.T) {
	require.Equal(t, "web:8080", Key("web", 8080))
	require.Equal(t, "db.example.com:5432", Key("db.example.com", 5432))
}

// TestAllocatePort verifies that allocatePort returns a usable ephemeral port.
func TestAllocatePort(t *testing.T) {
	port1, err := allocatePort()
	require.NoError(t, err)
	require.Greater(t, port1, 0, "port must be > 0")

	port2, err := allocatePort()
	require.NoError(t, err)
	require.Greater(t, port2, 0, "port must be > 0")
}

// TestStoreAndHas verifies registry mutation and Has/Snapshot semantics.
func TestStoreAndHas(t *testing.T) {
	f := NewForwarder()
	key := Key("h", 5000)
	require.False(t, f.Has(key), "key must not exist before Store")
	entry := &ForwardEntry{
		Cmd:       exec.Command("true"),
		HostName:  "h",
		LocalPort: 5000,
	}
	f.Store(key, entry)
	require.True(t, f.Has(key), "key must exist after Store")
	snap := f.Snapshot()
	require.Len(t, snap, 1, "Snapshot must have exactly 1 entry after Store")
}

// TestKillAllNilSafe verifies that KillAll does not panic when Process is nil
// (subprocess was never started).
func TestKillAllNilSafe(t *testing.T) {
	f := NewForwarder()
	key := Key("nilhost", 9999)
	entry := &ForwardEntry{
		Cmd:       exec.Command("true"), // Process is nil — never Started
		HostName:  "nilhost",
		LocalPort: 9999,
	}
	f.Store(key, entry)
	// Must not panic even though entry.Cmd.Process is nil.
	f.KillAll()
}

// TestStatusRunningVsDead verifies Status() correctly distinguishes exited vs
// not-yet-exited processes using the atomic exited flag (D-06, CR-01).
func TestStatusRunningVsDead(t *testing.T) {
	// Dead: entry with exited flag set (mirrors what the reaper goroutine does).
	dead := &ForwardEntry{Cmd: exec.Command("true")}
	dead.exited.Store(true)
	require.Equal(t, "dead", Status(dead), "exited process must have status 'dead'")

	// Running: freshly created entry — exited flag is false.
	running := &ForwardEntry{Cmd: exec.Command("true")}
	require.Equal(t, "running", Status(running), "unstarted process must have status 'running'")
}
