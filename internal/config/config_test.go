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
// missing ssh_host, valid config, and all multi-host validation paths.
func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		// Legacy (single-host) path — existing cases
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
			name: "valid legacy config",
			cfg:  Config{SSHSocket: "/tmp/ssh.sock", MCPSocket: "/tmp/mcp.sock", SSHUser: "user", SSHHost: "host"},
		},
		// mcp_socket always checked first regardless of host style
		{
			name: "missing mcp_socket with multi-host block",
			cfg: Config{
				Hosts: map[string]HostConfig{
					"web": {Socket: "/tmp/web.sock", User: "ubuntu", Host: "web.example.com"},
				},
				DefaultHost: "web",
			},
			wantErr: "config: mcp_socket is required",
		},
		// Multi-host path — new cases (MHST-03, MHST-04, Open Question 3)
		{
			name: "empty string host map key is rejected",
			cfg: Config{
				MCPSocket: "/tmp/mcp.sock",
				Hosts: map[string]HostConfig{
					"": {Socket: "/tmp/web.sock", User: "ubuntu", Host: "web.example.com"},
				},
				DefaultHost: "",
			},
			wantErr: "config: host name (map key) must not be empty",
		},
		{
			name: "non-empty hosts missing default_host",
			cfg: Config{
				MCPSocket: "/tmp/mcp.sock",
				Hosts: map[string]HostConfig{
					"web": {Socket: "/tmp/web.sock", User: "ubuntu", Host: "web.example.com"},
				},
			},
			wantErr: "config: hosts is non-empty but default_host is absent",
		},
		{
			name: "default_host names missing key",
			cfg: Config{
				MCPSocket: "/tmp/mcp.sock",
				Hosts: map[string]HostConfig{
					"web": {Socket: "/tmp/web.sock", User: "ubuntu", Host: "web.example.com"},
				},
				DefaultHost: "db",
			},
			wantErr: `config: default_host "db" is not a key in hosts`,
		},
		{
			name: "per-host empty socket",
			cfg: Config{
				MCPSocket: "/tmp/mcp.sock",
				Hosts: map[string]HostConfig{
					"web": {User: "ubuntu", Host: "web.example.com"}, // socket missing
				},
				DefaultHost: "web",
			},
			wantErr: `config: hosts["web"].socket is required`,
		},
		{
			name: "per-host empty user",
			cfg: Config{
				MCPSocket: "/tmp/mcp.sock",
				Hosts: map[string]HostConfig{
					"web": {Socket: "/tmp/web.sock", Host: "web.example.com"}, // user missing
				},
				DefaultHost: "web",
			},
			wantErr: `config: hosts["web"].user is required`,
		},
		{
			name: "per-host empty host",
			cfg: Config{
				MCPSocket: "/tmp/mcp.sock",
				Hosts: map[string]HostConfig{
					"web": {Socket: "/tmp/web.sock", User: "ubuntu"}, // host missing
				},
				DefaultHost: "web",
			},
			wantErr: `config: hosts["web"].host is required`,
		},
		{
			name: "valid multi-host config",
			cfg: Config{
				MCPSocket: "/tmp/mcp.sock",
				Hosts: map[string]HostConfig{
					"web": {Socket: "/tmp/web.sock", User: "ubuntu", Host: "web.example.com"},
					"db":  {Socket: "/tmp/db.sock", User: "ubuntu", Host: "db.example.com"},
				},
				DefaultHost: "web",
			},
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

// TestValidateLegacyAutoSeed verifies that Validate() populates cfg.Hosts and
// cfg.DefaultHost when only legacy fields are present (MHST-03, D-03).
func TestValidateLegacyAutoSeed(t *testing.T) {
	t.Run("legacy auto-seed populates hosts and default_host", func(t *testing.T) {
		cfg := Config{
			SSHSocket: "/tmp/ssh.sock",
			MCPSocket: "/tmp/mcp.sock",
			SSHUser:   "ubuntu",
			SSHHost:   "my.server.com",
		}
		require.NoError(t, cfg.Validate())
		require.Equal(t, "default", cfg.DefaultHost, "DefaultHost must be auto-set to 'default'")
		require.NotNil(t, cfg.Hosts, "Hosts must be non-nil after auto-seed")
		require.Contains(t, cfg.Hosts, "default", "Hosts must contain 'default' key after auto-seed")
		require.Equal(t, "/tmp/ssh.sock", cfg.Hosts["default"].Socket)
		require.Equal(t, "ubuntu", cfg.Hosts["default"].User)
		require.Equal(t, "my.server.com", cfg.Hosts["default"].Host)
	})

	t.Run("explicit empty hosts map triggers auto-seed (Pitfall 7)", func(t *testing.T) {
		// JSON "hosts": {} decodes to a non-nil but empty map; len==0 must still trigger auto-seed.
		cfg := Config{
			SSHSocket: "/tmp/ssh.sock",
			MCPSocket: "/tmp/mcp.sock",
			SSHUser:   "ubuntu",
			SSHHost:   "my.server.com",
			Hosts:     map[string]HostConfig{}, // explicitly empty, not nil
		}
		require.NoError(t, cfg.Validate())
		require.Equal(t, "default", cfg.DefaultHost)
		require.Contains(t, cfg.Hosts, "default")
	})
}

// TestMultiHostLoadFromPath verifies JSON round-trip parsing of multi-host config.
func TestMultiHostLoadFromPath(t *testing.T) {
	t.Run("multi-host JSON parses correctly and validates", func(t *testing.T) {
		data := `{
			"mcp_socket": "/tmp/claude-ssh.sock",
			"default_host": "web",
			"hosts": {
				"web": {"socket": "/tmp/ssh-web.sock", "user": "ubuntu", "host": "web.example.com"},
				"db":  {"socket": "/tmp/ssh-db.sock",  "user": "ubuntu", "host": "db.example.com"}
			},
			"capabilities": {"exec": true}
		}`
		path := writeTemp(t, data)
		cfg, err := loadFromPath(path)
		require.NoError(t, err)
		require.Equal(t, "web", cfg.DefaultHost)
		require.Len(t, cfg.Hosts, 2)
		require.Equal(t, "/tmp/ssh-web.sock", cfg.Hosts["web"].Socket)
		require.Equal(t, "ubuntu", cfg.Hosts["web"].User)
		require.Equal(t, "web.example.com", cfg.Hosts["web"].Host)
		require.Equal(t, "/tmp/ssh-db.sock", cfg.Hosts["db"].Socket)
		require.Equal(t, "ubuntu", cfg.Hosts["db"].User)
		require.Equal(t, "db.example.com", cfg.Hosts["db"].Host)
		require.True(t, cfg.Capabilities.Exec)
	})

	t.Run("legacy single-host JSON auto-seeds to hosts default", func(t *testing.T) {
		data := `{
			"ssh_socket": "/tmp/ssh-ctrl.sock",
			"mcp_socket": "/tmp/claude-ssh.sock",
			"ssh_user": "ubuntu",
			"ssh_host": "my.server.com"
		}`
		path := writeTemp(t, data)
		cfg, err := loadFromPath(path)
		require.NoError(t, err)
		require.Equal(t, "default", cfg.DefaultHost)
		require.Equal(t, "/tmp/ssh-ctrl.sock", cfg.Hosts["default"].Socket)
		require.Equal(t, "ubuntu", cfg.Hosts["default"].User)
		require.Equal(t, "my.server.com", cfg.Hosts["default"].Host)
	})

	t.Run("explicit empty hosts JSON triggers auto-seed", func(t *testing.T) {
		data := `{
			"ssh_socket": "/tmp/ssh.sock",
			"mcp_socket": "/tmp/mcp.sock",
			"ssh_user": "ubuntu",
			"ssh_host": "my.server.com",
			"hosts": {}
		}`
		path := writeTemp(t, data)
		cfg, err := loadFromPath(path)
		require.NoError(t, err)
		require.Equal(t, "default", cfg.DefaultHost)
		require.Contains(t, cfg.Hosts, "default")
	})
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

	// Track whether the directory existed before the test to restore state on cleanup.
	dirExistedBefore := true
	if _, statErr := os.Stat(configDir); os.IsNotExist(statErr) {
		dirExistedBefore = false
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
	t.Cleanup(func() {
		os.Remove(configPath)
		if !dirExistedBefore {
			os.Remove(configDir) // best-effort; leaves directory only if non-empty
		}
	})

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

// TestExecAllowlistThreeStates verifies the three-state semantics of ExecAllowlist:
// absent JSON key → nil pointer (allow-all, ALWL-01), explicit [] → non-nil empty
// slice (deny-all), and populated list → non-nil slice with entries.
// JSON round-trip via writeTemp+loadFromPath is required because struct literals
// cannot reproduce the JSON decoder's nil vs non-nil pointer distinction.
func TestExecAllowlistThreeStates(t *testing.T) {
	base := func(allowlistJSON string) string {
		allowlistField := ""
		if allowlistJSON != "" {
			allowlistField = `"exec_allowlist": ` + allowlistJSON + ","
		}
		return `{
			"mcp_socket": "/tmp/claude-ssh.sock",
			"default_host": "web",
			"hosts": {
				"web": {"socket": "/tmp/ssh-web.sock", "user": "ubuntu", "host": "web.example.com",
				        ` + allowlistField + `
				        "base_dir": ""}
			}
		}`
	}

	t.Run("absent exec_allowlist key: ExecAllowlist is nil (allow-all)", func(t *testing.T) {
		cfg, err := loadFromPath(writeTemp(t, base("")))
		require.NoError(t, err)
		require.Nil(t, cfg.Hosts["web"].ExecAllowlist,
			"absent exec_allowlist must decode to nil pointer (allow-all signal, ALWL-01)")
	})

	t.Run("explicit empty exec_allowlist: ExecAllowlist is non-nil with len 0 (deny-all)", func(t *testing.T) {
		cfg, err := loadFromPath(writeTemp(t, base("[]")))
		require.NoError(t, err)
		require.NotNil(t, cfg.Hosts["web"].ExecAllowlist,
			"explicit [] must decode to non-nil pointer (deny-all signal)")
		require.Len(t, *cfg.Hosts["web"].ExecAllowlist, 0,
			"pointer must point to a zero-length slice")
	})

	t.Run("populated exec_allowlist: ExecAllowlist is non-nil with correct entry", func(t *testing.T) {
		cfg, err := loadFromPath(writeTemp(t, base(`["git "]`)))
		require.NoError(t, err)
		require.NotNil(t, cfg.Hosts["web"].ExecAllowlist,
			"populated list must decode to non-nil pointer")
		require.Len(t, *cfg.Hosts["web"].ExecAllowlist, 1,
			"pointer must point to a one-element slice")
		require.Equal(t, "git ", (*cfg.Hosts["web"].ExecAllowlist)[0],
			"list element must preserve exact value including trailing space")
	})

	t.Run("exec_allowlist with empty string entry is rejected (CR-01 security fix)", func(t *testing.T) {
		_, err := loadFromPath(writeTemp(t, base(`[""]`)))
		require.EqualError(t, err,
			`config: hosts["web"].exec_allowlist[0] must not be empty (empty string is a prefix of every command)`)
	})
}

// TestExecAllowlistPerHostIndependence verifies that two hosts with different
// exec_allowlist values each retain their own pointer after Validate (ALWL-04).
func TestExecAllowlistPerHostIndependence(t *testing.T) {
	data := `{
		"mcp_socket": "/tmp/claude-ssh.sock",
		"default_host": "web",
		"hosts": {
			"web": {"socket": "/tmp/ssh-web.sock", "user": "ubuntu", "host": "web.example.com",
			        "exec_allowlist": ["git "]},
			"db":  {"socket": "/tmp/ssh-db.sock",  "user": "ubuntu", "host": "db.example.com"}
		}
	}`
	cfg, err := loadFromPath(writeTemp(t, data))
	require.NoError(t, err)

	require.NotNil(t, cfg.Hosts["web"].ExecAllowlist,
		"web host with exec_allowlist must have non-nil pointer")
	require.Len(t, *cfg.Hosts["web"].ExecAllowlist, 1)

	require.Nil(t, cfg.Hosts["db"].ExecAllowlist,
		"db host without exec_allowlist must have nil pointer (ALWL-04 independence)")
}

// TestBaseDirValidation covers base_dir validation in Validate(): non-absolute rejection
// and path.Clean normalisation of trailing slashes and double slashes (BDIR-04).
func TestBaseDirValidation(t *testing.T) {
	makeHostCfg := func(baseDir string) Config {
		return Config{
			MCPSocket: "/tmp/mcp.sock",
			Hosts: map[string]HostConfig{
				"web": {Socket: "/tmp/web.sock", User: "ubuntu", Host: "web.example.com", BaseDir: baseDir},
			},
			DefaultHost: "web",
		}
	}

	t.Run("non-absolute base_dir is rejected with exact error", func(t *testing.T) {
		cfg := makeHostCfg("relative/path")
		err := cfg.Validate()
		require.EqualError(t, err, `config: hosts["web"].base_dir must be an absolute path, got "relative/path"`)
	})

	t.Run("trailing slash is cleaned: /home/ubuntu/ → /home/ubuntu", func(t *testing.T) {
		cfg := makeHostCfg("/home/ubuntu/")
		require.NoError(t, cfg.Validate())
		require.Equal(t, "/home/ubuntu", cfg.Hosts["web"].BaseDir,
			"trailing slash must be stripped by path.Clean")
	})

	t.Run("double slash and trailing slash cleaned: /home/ubuntu//x/ → /home/ubuntu/x", func(t *testing.T) {
		cfg := makeHostCfg("/home/ubuntu//x/")
		require.NoError(t, cfg.Validate())
		require.Equal(t, "/home/ubuntu/x", cfg.Hosts["web"].BaseDir,
			"double slash and trailing slash must be normalised by path.Clean")
	})

	t.Run("empty base_dir is accepted and left empty", func(t *testing.T) {
		cfg := makeHostCfg("")
		require.NoError(t, cfg.Validate())
		require.Equal(t, "", cfg.Hosts["web"].BaseDir)
	})

	t.Run("canonical absolute path is accepted unchanged", func(t *testing.T) {
		cfg := makeHostCfg("/home/ubuntu")
		require.NoError(t, cfg.Validate())
		require.Equal(t, "/home/ubuntu", cfg.Hosts["web"].BaseDir)
	})
}

// TestLegacyBackwardCompatNewFields verifies that a legacy auto-seeded config
// and a multi-host config that omit exec_allowlist and base_dir both load without
// errors and default the new fields to nil and "" respectively.
func TestLegacyBackwardCompatNewFields(t *testing.T) {
	t.Run("legacy auto-seed: ExecAllowlist is nil and BaseDir is empty", func(t *testing.T) {
		cfg := Config{
			SSHSocket: "/tmp/ssh.sock",
			MCPSocket: "/tmp/mcp.sock",
			SSHUser:   "ubuntu",
			SSHHost:   "my.server.com",
		}
		require.NoError(t, cfg.Validate())
		require.Nil(t, cfg.Hosts["default"].ExecAllowlist,
			"auto-seeded host must have nil ExecAllowlist (absent means allow-all)")
		require.Equal(t, "", cfg.Hosts["default"].BaseDir,
			"auto-seeded host must have empty BaseDir")
	})

	t.Run("multi-host config without new fields loads with no error", func(t *testing.T) {
		data := `{
			"mcp_socket": "/tmp/mcp.sock",
			"default_host": "web",
			"hosts": {
				"web": {"socket": "/tmp/ssh-web.sock", "user": "ubuntu", "host": "web.example.com"},
				"db":  {"socket": "/tmp/ssh-db.sock",  "user": "ubuntu", "host": "db.example.com"}
			}
		}`
		cfg, err := loadFromPath(writeTemp(t, data))
		require.NoError(t, err)
		require.Nil(t, cfg.Hosts["web"].ExecAllowlist)
		require.Equal(t, "", cfg.Hosts["web"].BaseDir)
		require.Nil(t, cfg.Hosts["db"].ExecAllowlist)
		require.Equal(t, "", cfg.Hosts["db"].BaseDir)
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
