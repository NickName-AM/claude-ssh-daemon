package tools

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
	"github.com/NickName-AM/claude-ssh-daemon/internal/ssh"
)

// TestSortedKeys verifies that sortedKeys returns map keys in ascending
// lexicographic order regardless of map iteration order.
func TestSortedKeys(t *testing.T) {
	registry := map[string]ssh.SSHExecutor{
		"web":     &toolsMockExecutor{},
		"db":      &toolsMockExecutor{},
		"api":     &toolsMockExecutor{},
		"staging": &toolsMockExecutor{},
	}
	keys := sortedKeys(registry)
	require.Equal(t, []string{"api", "db", "staging", "web"}, keys, "keys must be in ascending lexicographic order")
}

// TestSortedKeysSingle verifies sortedKeys with a single-entry map.
func TestSortedKeysSingle(t *testing.T) {
	registry := map[string]ssh.SSHExecutor{
		"default": &toolsMockExecutor{},
	}
	keys := sortedKeys(registry)
	require.Equal(t, []string{"default"}, keys)
}

// TestSortedKeysEmpty verifies sortedKeys with an empty map returns an empty slice.
func TestSortedKeysEmpty(t *testing.T) {
	registry := map[string]ssh.SSHExecutor{}
	keys := sortedKeys(registry)
	require.Empty(t, keys, "empty registry must return empty slice")
}

// resolveTestCfg builds a minimal *config.Config for resolver tests.
// It sets Hosts entries matching the registry keys and sets defaultHost.
// Tests bypass JSON loading and Validate() directly.
func resolveTestCfg(registry map[string]ssh.SSHExecutor, defaultHost string) *config.Config {
	hosts := make(map[string]config.HostConfig, len(registry))
	for name := range registry {
		hosts[name] = config.HostConfig{
			Socket: "/tmp/" + name + ".sock",
			User:   "user",
			Host:   name + ".example.com",
		}
	}
	return &config.Config{
		MCPSocket:   "/tmp/mcp.sock",
		Hosts:       hosts,
		DefaultHost: defaultHost,
	}
}

// TestResolveExecutorDefaultRouting verifies that an empty hostParam routes to
// cfg.DefaultHost (MHST-06). Returns the correct executor and non-empty resolved name.
func TestResolveExecutorDefaultRouting(t *testing.T) {
	defaultMock := &toolsMockExecutor{}
	webMock := &toolsMockExecutor{}
	registry := map[string]ssh.SSHExecutor{
		"default": defaultMock,
		"web":     webMock,
	}
	cfg := resolveTestCfg(registry, "default")

	exec, hostName, errResult := resolveExecutor(registry, cfg, "")
	require.Nil(t, errResult, "empty hostParam must not return an error result")
	require.Equal(t, "default", hostName, "resolved name must be cfg.DefaultHost")
	require.Equal(t, defaultMock, exec, "must return the default executor")
}

// TestResolveExecutorNamedHost verifies that a non-empty hostParam resolves to
// the named host's executor (MHST-05). Returns the executor and the resolved name.
func TestResolveExecutorNamedHost(t *testing.T) {
	defaultMock := &toolsMockExecutor{}
	webMock := &toolsMockExecutor{}
	registry := map[string]ssh.SSHExecutor{
		"default": defaultMock,
		"web":     webMock,
	}
	cfg := resolveTestCfg(registry, "default")

	exec, hostName, errResult := resolveExecutor(registry, cfg, "web")
	require.Nil(t, errResult, "known host must not return an error result")
	require.Equal(t, "web", hostName, "resolved name must be the requested host name")
	require.Equal(t, webMock, exec, "must return the web executor")
}

// TestResolveExecutorUnknownHost verifies that an unknown hostParam returns a
// non-nil IsError result listing the configured host names in sorted order (MHST-07).
// The error text must contain the requested host name and the sorted host list.
func TestResolveExecutorUnknownHost(t *testing.T) {
	registry := map[string]ssh.SSHExecutor{
		"web": &toolsMockExecutor{},
		"db":  &toolsMockExecutor{},
	}
	cfg := resolveTestCfg(registry, "web")

	exec, hostName, errResult := resolveExecutor(registry, cfg, "nope")
	require.Nil(t, exec, "unknown host must return nil executor")
	require.Empty(t, hostName, "unknown host must return empty resolved name")
	require.NotNil(t, errResult, "unknown host must return a non-nil error result")
	require.True(t, errResult.IsError, "unknown host error result must have IsError=true")

	require.NotEmpty(t, errResult.Content, "error result must have content")
	text, ok := errResult.Content[0].(*mcp.TextContent)
	require.True(t, ok, "content[0] must be *mcp.TextContent")

	require.Contains(t, text.Text, `unknown host "nope"`, "error text must quote the requested host name")
	require.Contains(t, text.Text, "db", "error text must list configured host names")
	require.Contains(t, text.Text, "web", "error text must list configured host names")

	// Verify sorted order: db comes before web alphabetically
	dbIdx := indexOfStr(text.Text, "db")
	webIdx := indexOfStr(text.Text, "web")
	require.Less(t, dbIdx, webIdx, "host list in error must be in sorted order (db before web)")
}

// TestResolveExecutorErrorTextFormat verifies the exact format string:
// unknown host %q; configured hosts: %s
func TestResolveExecutorErrorTextFormat(t *testing.T) {
	registry := map[string]ssh.SSHExecutor{
		"alpha": &toolsMockExecutor{},
		"beta":  &toolsMockExecutor{},
	}
	cfg := resolveTestCfg(registry, "alpha")

	_, _, errResult := resolveExecutor(registry, cfg, "missing")
	require.NotNil(t, errResult)

	text, ok := errResult.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	// Must contain literal: unknown host "missing"
	require.Contains(t, text.Text, `unknown host "missing"`)
	// Must contain sorted host list: alpha, beta
	require.Contains(t, text.Text, "alpha, beta")
}

// TestResolveExecutorReturnedNameIsAlwaysNonEmpty verifies Pitfall 6:
// the resolved name returned on success is always the actual name, never "".
// An empty in.Host resolves to cfg.DefaultHost — the returned name is non-empty.
func TestResolveExecutorReturnedNameIsAlwaysNonEmpty(t *testing.T) {
	registry := map[string]ssh.SSHExecutor{
		"default": &toolsMockExecutor{},
	}
	cfg := resolveTestCfg(registry, "default")

	// Empty host param → resolved to "default" (non-empty)
	_, hostName, errResult := resolveExecutor(registry, cfg, "")
	require.Nil(t, errResult)
	require.NotEmpty(t, hostName, "resolved name must be non-empty even when hostParam is empty (Pitfall 6)")
	require.Equal(t, "default", hostName)
}

// indexOfStr returns the byte index of substr in s, or -1 if not found.
// Used to assert ordering of substrings within error messages.
func indexOfStr(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
