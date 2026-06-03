package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHostConfigJSONTags verifies the HostConfig type exists with the correct JSON tags.
func TestHostConfigJSONTags(t *testing.T) {
	h := HostConfig{
		Socket: "/tmp/ssh.sock",
		User:   "ubuntu",
		Host:   "example.com",
	}
	data, err := json.Marshal(h)
	require.NoError(t, err)
	require.Contains(t, string(data), `"socket"`)
	require.Contains(t, string(data), `"user"`)
	require.Contains(t, string(data), `"host"`)

	// Verify field names via reflection to ensure JSON tags are correct.
	rt := reflect.TypeOf(HostConfig{})
	socketField, ok := rt.FieldByName("Socket")
	require.True(t, ok, "HostConfig must have Socket field")
	require.Equal(t, "socket", socketField.Tag.Get("json"), "Socket field must have json:\"socket\" tag")
	userField, ok := rt.FieldByName("User")
	require.True(t, ok, "HostConfig must have User field")
	require.Equal(t, "user", userField.Tag.Get("json"), "User field must have json:\"user\" tag")
	hostField, ok := rt.FieldByName("Host")
	require.True(t, ok, "HostConfig must have Host field")
	require.Equal(t, "host", hostField.Tag.Get("json"), "Host field must have json:\"host\" tag")
}

// TestConfigHostsAndDefaultHostFields verifies the Config struct has the new v2.0 fields.
func TestConfigHostsAndDefaultHostFields(t *testing.T) {
	rt := reflect.TypeOf(Config{})
	hostsField, ok := rt.FieldByName("Hosts")
	require.True(t, ok, "Config must have Hosts field")
	require.Equal(t, "hosts,omitempty", hostsField.Tag.Get("json"), "Hosts must have json:\"hosts,omitempty\" tag")

	defaultHostField, ok := rt.FieldByName("DefaultHost")
	require.True(t, ok, "Config must have DefaultHost field")
	require.Equal(t, "default_host,omitempty", defaultHostField.Tag.Get("json"), "DefaultHost must have json:\"default_host,omitempty\" tag")
}

// TestConfigLegacyFieldsRetained verifies SSHSocket, SSHUser, SSHHost remain on Config.
func TestConfigLegacyFieldsRetained(t *testing.T) {
	rt := reflect.TypeOf(Config{})
	for _, name := range []string{"SSHSocket", "SSHUser", "SSHHost"} {
		_, ok := rt.FieldByName(name)
		require.True(t, ok, "Config must retain legacy field %s for backward compat", name)
	}
}

// TestValidate covers missing ssh_socket, missing mcp_socket, missing ssh_user,
// missing ssh_host, and valid config.
func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name:    "missing ssh_socket",
			cfg:     Config{MCPSocket: "/tmp/mcp.sock"},
			wantErr: "config: ssh_socket is required",
		},
		{
			name:    "missing mcp_socket",
			cfg:     Config{SSHSocket: "/tmp/ssh.sock"},
			wantErr: "config: mcp_socket is required",
		},
		{
			name:    "missing ssh_user",
			cfg:     Config{SSHSocket: "/tmp/ssh.sock", MCPSocket: "/tmp/mcp.sock", SSHHost: "host"},
			wantErr: "config: ssh_user is required",
		},
		{
			name:    "missing ssh_host",
			cfg:     Config{SSHSocket: "/tmp/ssh.sock", MCPSocket: "/tmp/mcp.sock", SSHUser: "user"},
			wantErr: "config: ssh_host is required",
		},
		{
			name: "valid config",
			cfg:  Config{SSHSocket: "/tmp/ssh.sock", MCPSocket: "/tmp/mcp.sock", SSHUser: "user", SSHHost: "host"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestLoadFromPath verifies JSON parsing, struct field mapping, capability defaults,
// and that field values with tilde are NOT expanded (only the config FILE PATH is expanded).
func TestLoadFromPath(t *testing.T) {
	t.Run("valid config with capability true and omitted capability", func(t *testing.T) {
		data := `{
			"ssh_socket": "/tmp/ssh-ctrl.sock",
			"mcp_socket": "/tmp/mcp.sock",
			"ssh_user": "ubuntu",
			"ssh_host": "my.server.com",
			"capabilities": {
				"exec": true
			}
		}`
		path := writeTemp(t, data)
		cfg, err := loadFromPath(path)
		require.NoError(t, err)
		require.Equal(t, "/tmp/ssh-ctrl.sock", cfg.SSHSocket)
		require.Equal(t, "/tmp/mcp.sock", cfg.MCPSocket)
		require.True(t, cfg.Capabilities.Exec, "exec should be true")
		require.False(t, cfg.Capabilities.FileRead, "file_read should default to false")
		require.False(t, cfg.Capabilities.FileWrite, "file_write should default to false")
		require.False(t, cfg.Capabilities.PortForward, "port_forward should default to false")
	})

	t.Run("all capabilities set", func(t *testing.T) {
		data := `{
			"ssh_socket": "/tmp/ssh.sock",
			"mcp_socket": "/tmp/mcp.sock",
			"ssh_user": "ubuntu",
			"ssh_host": "my.server.com",
			"capabilities": {
				"exec": true,
				"file_read": true,
				"file_write": true,
				"port_forward": true
			}
		}`
		path := writeTemp(t, data)
		cfg, err := loadFromPath(path)
		require.NoError(t, err)
		require.True(t, cfg.Capabilities.Exec)
		require.True(t, cfg.Capabilities.FileRead)
		require.True(t, cfg.Capabilities.FileWrite)
		require.True(t, cfg.Capabilities.PortForward)
	})

	t.Run("missing ssh_socket returns field-specific error", func(t *testing.T) {
		data := `{"mcp_socket": "/tmp/mcp.sock"}`
		path := writeTemp(t, data)
		_, err := loadFromPath(path)
		require.EqualError(t, err, "config: ssh_socket is required")
	})

	t.Run("missing mcp_socket returns field-specific error", func(t *testing.T) {
		data := `{"ssh_socket": "/tmp/ssh.sock"}`
		path := writeTemp(t, data)
		_, err := loadFromPath(path)
		require.EqualError(t, err, "config: mcp_socket is required")
	})

	t.Run("file not found returns wrapped open error", func(t *testing.T) {
		_, err := loadFromPath("/nonexistent/path/config.json")
		require.Error(t, err)
		require.Contains(t, err.Error(), "open config:")
	})

	t.Run("invalid JSON returns wrapped parse error", func(t *testing.T) {
		path := writeTemp(t, `{not valid json}`)
		_, err := loadFromPath(path)
		require.Error(t, err)
		require.Contains(t, err.Error(), "parse config:")
	})

	t.Run("unknown fields are silently ignored (forward compat)", func(t *testing.T) {
		data := `{
			"ssh_socket": "/tmp/ssh.sock",
			"mcp_socket": "/tmp/mcp.sock",
			"ssh_user": "ubuntu",
			"ssh_host": "my.server.com",
			"future_field": "ignored",
			"another_unknown": 42
		}`
		path := writeTemp(t, data)
		cfg, err := loadFromPath(path)
		require.NoError(t, err)
		require.Equal(t, "/tmp/ssh.sock", cfg.SSHSocket)
	})
}

// TestTildeValueNotExpanded asserts that a literal ~/... value in the mcp_socket JSON
// field is preserved verbatim — only the config FILE PATH itself is tilde-expanded,
// never the field values inside the config.
func TestTildeValueNotExpanded(t *testing.T) {
	data := `{
		"ssh_socket": "/tmp/ssh.sock",
		"mcp_socket": "~/.config/claude-ssh-daemon/mcp.sock",
		"ssh_user": "ubuntu",
		"ssh_host": "my.server.com"
	}`
	path := writeTemp(t, data)
	cfg, err := loadFromPath(path)
	require.NoError(t, err)
	require.Equal(t, "~/.config/claude-ssh-daemon/mcp.sock", cfg.MCPSocket,
		"field values with tilde must NOT be expanded — only the config file path is expanded")
}

// TestLoadUsesDefaultPath is an integration test verifying Load() correctly
// resolves tilde expansion. We write a valid config to the default path and
// call Load(). This test is skipped if the default directory cannot be created.
func TestLoadUsesDefaultPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir:", err)
	}
	configDir := filepath.Join(home, ".config", "claude-ssh-daemon")
	configPath := filepath.Join(configDir, "config.json")

	// Skip if config file already exists — don't overwrite user's real config.
	if _, err := os.Stat(configPath); err == nil {
		t.Skip("real config file exists at", configPath, "— skipping to avoid overwrite")
	}

	// Create the config directory and a temporary config file.
	require.NoError(t, os.MkdirAll(configDir, 0o755))
	data, err := json.Marshal(Config{
		SSHSocket: "/tmp/ssh-ctrl.sock",
		MCPSocket: "/tmp/mcp.sock",
		SSHUser:   "ubuntu",
		SSHHost:   "my.server.com",
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, data, 0o600))
	t.Cleanup(func() { os.Remove(configPath) })

	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "/tmp/ssh-ctrl.sock", cfg.SSHSocket)
}

// TestSafeguards covers GURD-04, GURD-05, SAFE-03, SAFE-04: absent block zero values,
// flag parsing, valid pattern compilation, invalid regex errors, and compilation when disabled.
func TestSafeguards(t *testing.T) {
	base := `{
		"ssh_socket": "/tmp/ssh.sock",
		"mcp_socket": "/tmp/mcp.sock",
		"ssh_user":   "ubuntu",
		"ssh_host":   "my.server.com"
	}`

	t.Run("absent safeguards block: all zero values", func(t *testing.T) {
		cfg, err := loadFromPath(writeTemp(t, base))
		require.NoError(t, err)
		require.False(t, cfg.Safeguards.GuardDisabled)
		require.False(t, cfg.Safeguards.AllowOverwrite)
		require.False(t, cfg.Safeguards.AllowDelete)
		require.Nil(t, cfg.Safeguards.Patterns)
		require.Nil(t, cfg.Safeguards.CompiledPatterns)
	})

	t.Run("guard_disabled true is parsed", func(t *testing.T) {
		data := `{
			"ssh_socket": "/tmp/ssh.sock", "mcp_socket": "/tmp/mcp.sock",
			"ssh_user": "ubuntu", "ssh_host": "my.server.com",
			"safeguards": {"guard_disabled": true}
		}`
		cfg, err := loadFromPath(writeTemp(t, data))
		require.NoError(t, err)
		require.True(t, cfg.Safeguards.GuardDisabled)
	})

	t.Run("allow_overwrite true is parsed", func(t *testing.T) {
		data := `{
			"ssh_socket": "/tmp/ssh.sock", "mcp_socket": "/tmp/mcp.sock",
			"ssh_user": "ubuntu", "ssh_host": "my.server.com",
			"safeguards": {"allow_overwrite": true}
		}`
		cfg, err := loadFromPath(writeTemp(t, data))
		require.NoError(t, err)
		require.True(t, cfg.Safeguards.AllowOverwrite)
	})

	t.Run("allow_delete true is parsed", func(t *testing.T) {
		data := `{
			"ssh_socket": "/tmp/ssh.sock", "mcp_socket": "/tmp/mcp.sock",
			"ssh_user": "ubuntu", "ssh_host": "my.server.com",
			"safeguards": {"allow_delete": true}
		}`
		cfg, err := loadFromPath(writeTemp(t, data))
		require.NoError(t, err)
		require.True(t, cfg.Safeguards.AllowDelete)
	})

	t.Run("valid patterns are compiled", func(t *testing.T) {
		data := `{
			"ssh_socket": "/tmp/ssh.sock", "mcp_socket": "/tmp/mcp.sock",
			"ssh_user": "ubuntu", "ssh_host": "my.server.com",
			"safeguards": {"patterns": ["foo", "bar\\d+"]}
		}`
		cfg, err := loadFromPath(writeTemp(t, data))
		require.NoError(t, err)
		require.Len(t, cfg.Safeguards.CompiledPatterns, 2)
	})

	t.Run("invalid regex at index 0 returns error with index and quoted pattern", func(t *testing.T) {
		data := `{
			"ssh_socket": "/tmp/ssh.sock", "mcp_socket": "/tmp/mcp.sock",
			"ssh_user": "ubuntu", "ssh_host": "my.server.com",
			"safeguards": {"patterns": ["(?invalid"]}
		}`
		_, err := loadFromPath(writeTemp(t, data))
		require.ErrorContains(t, err, "safeguards.patterns[0]")
		require.ErrorContains(t, err, `"(?invalid"`)
	})

	t.Run("invalid regex still errors when guard_disabled is true", func(t *testing.T) {
		data := `{
			"ssh_socket": "/tmp/ssh.sock", "mcp_socket": "/tmp/mcp.sock",
			"ssh_user": "ubuntu", "ssh_host": "my.server.com",
			"safeguards": {"guard_disabled": true, "patterns": ["(?invalid"]}
		}`
		_, err := loadFromPath(writeTemp(t, data))
		require.Error(t, err, "compilation must run even when guard is disabled")
	})
}

// writeTemp writes content to a temporary file and returns its path.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "config-*.json")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	return f.Name()
}
