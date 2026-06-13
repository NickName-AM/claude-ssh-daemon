package forward

import (
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
		Socket:    "/tmp/ssh-h.sock",
		User:      "ubuntu",
		HostName:  "h",
		LocalPort: 5000,
	}
	f.Store(key, entry)
	require.True(t, f.Has(key), "key must exist after Store")
	snap := f.Snapshot()
	require.Len(t, snap, 1, "Snapshot must have exactly 1 entry after Store")
}

// TestCancelAllNoSocket verifies that CancelAll does not panic when the
// ControlMaster socket does not exist. The error from ssh -O cancel is
// silently ignored — if the ControlMaster is gone the forward is already dead.
func TestCancelAllNoSocket(t *testing.T) {
	f := NewForwarder()
	key := Key("nohost", 9999)
	entry := &ForwardEntry{
		Socket:     "/tmp/no-such-ctrl.sock",
		User:       "ubuntu",
		HostName:   "nohost",
		LocalPort:  9999,
		RemoteHost: "db.internal",
		RemotePort: 5432,
	}
	f.Store(key, entry)
	// Must not panic even though the socket does not exist.
	f.CancelAll()
}

// TestDeleteRemovesEntry verifies that Delete evicts the entry from the registry.
func TestDeleteRemovesEntry(t *testing.T) {
	f := NewForwarder()
	key := Key("h", 6000)
	entry := &ForwardEntry{HostName: "h", LocalPort: 6000}
	f.Store(key, entry)
	require.True(t, f.Has(key))
	f.Delete(key)
	require.False(t, f.Has(key), "entry must be gone after Delete")
	snap := f.Snapshot()
	require.Len(t, snap, 0, "Snapshot must be empty after Delete")
}
