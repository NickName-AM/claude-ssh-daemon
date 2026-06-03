// Package config provides JSON config loading with tilde expansion,
// required-field validation, and capability toggles that default to false.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const defaultConfigPath = "~/.config/claude-ssh-daemon/config.json"

// Capabilities holds per-feature toggle flags. All fields default to false
// when absent from the JSON config (Go zero-value for bool).
type Capabilities struct {
	Exec        bool `json:"exec"`
	FileRead    bool `json:"file_read"`
	FileWrite   bool `json:"file_write"`
	PortForward bool `json:"port_forward"`
}

// Safeguards holds guard-layer configuration flags and user-supplied custom
// detection patterns. All bool fields default to false when absent from the JSON
// config (Go zero-value for bool). GuardDisabled false means the guard is ON.
type Safeguards struct {
	GuardDisabled  bool     `json:"guard_disabled"`
	AllowOverwrite bool     `json:"allow_overwrite"`
	AllowDelete    bool     `json:"allow_delete"`
	Patterns       []string `json:"patterns"`
	// CompiledPatterns is runtime state set by Validate(); nil when Patterns is
	// empty or the safeguards block is absent.
	CompiledPatterns []*regexp.Regexp `json:"-"`
}

// HostConfig holds the connection parameters for a single named SSH host.
// Field names match ssh.ControlMasterExecutor exactly (Socket, User, Host)
// so registry construction in daemon.Run maps 1:1 with no impedance mismatch.
type HostConfig struct {
	Socket string `json:"socket"`
	User   string `json:"user"`
	Host   string `json:"host"`
}

// Config holds the daemon configuration loaded from the JSON config file.
// Phase 1 fields: ssh_socket, mcp_socket, capabilities.
// Phase 2 fields: ssh_user, ssh_host (D-03, required).
// v2.0 multi-host additions: hosts map + default_host (D-01, D-02, D-03).
// Legacy single-host fields (ssh_socket/ssh_user/ssh_host) are retained for
// backward-compat JSON parsing; when a hosts block is provided they are silently
// ignored (D-02). When absent, Validate() auto-seeds hosts["default"] from them
// (D-03). Direct access to SSHSocket/SSHUser/SSHHost after Validate() is
// deprecated — read from cfg.Hosts instead.
type Config struct {
	// Legacy single-host fields (retained for backward-compat JSON parsing; D-02, D-03)
	SSHSocket string `json:"ssh_socket"`
	SSHUser   string `json:"ssh_user"` // added Phase 2 (D-03)
	SSHHost   string `json:"ssh_host"` // added Phase 2 (D-03)
	// v2.0 multi-host fields (D-01)
	Hosts       map[string]HostConfig `json:"hosts,omitempty"`
	DefaultHost string                `json:"default_host,omitempty"`
	// Unchanged
	MCPSocket    string       `json:"mcp_socket"`
	Capabilities Capabilities `json:"capabilities"`
	Safeguards   Safeguards   `json:"safeguards"`
}

// Load reads the config from the default path (~/.config/claude-ssh-daemon/config.json),
// applying tilde expansion via os.UserHomeDir(). Returns a validated Config or an error.
func Load() (*Config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	return loadFromPath(path)
}

// loadFromPath reads and validates the config from the given path.
// Tests in the same package (package config) call this directly to avoid
// relying on the tilde-expanded default path returned by os.UserHomeDir.
func loadFromPath(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	var cfg Config
	dec := json.NewDecoder(f)
	// Do NOT call dec.DisallowUnknownFields() — unknown fields must be silently
	// ignored so a single config file spans all phases (Open Question A2, PATTERNS.md).
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, cfg.Validate()
}

// Validate checks required fields and resolves multi-host configuration.
//
// Check order (Pitfall 4 — mcp_socket always first):
//  1. mcp_socket — required regardless of host style
//  2. Multi-host resolution — len(c.Hosts) == 0 (NOT c.Hosts == nil, Pitfall 7):
//     - Empty/absent: auto-seed hosts["default"] from legacy fields (MHST-03, D-03)
//     - Non-empty: validate per-host fields and default_host (MHST-04, D-05)
//  3. Safeguards.Patterns compilation (unchanged; runs after host resolution)
//
// After Validate() returns nil, c.Hosts is guaranteed non-empty and c.DefaultHost
// names a valid key in c.Hosts. All downstream code reads from c.Hosts.
func (c *Config) Validate() error {
	// 1. mcp_socket is always required, regardless of host style (Pitfall 4).
	if c.MCPSocket == "" {
		return errors.New("config: mcp_socket is required")
	}

	// 2. Multi-host resolution: use len() not == nil to handle "hosts": {} (Pitfall 7).
	if len(c.Hosts) == 0 {
		// MHST-03: auto-seed from legacy fields. Validate each is present.
		if c.SSHSocket == "" {
			return errors.New("config: ssh_socket is required")
		}
		if c.SSHUser == "" {
			return errors.New("config: ssh_user is required")
		}
		if c.SSHHost == "" {
			return errors.New("config: ssh_host is required")
		}
		c.Hosts = map[string]HostConfig{
			"default": {Socket: c.SSHSocket, User: c.SSHUser, Host: c.SSHHost},
		}
		c.DefaultHost = "default"
	} else {
		// Hosts block is authoritative (D-02). Validate per-host field completeness
		// in sorted key order for deterministic error messages (Open Question 3).
		keys := make([]string, 0, len(c.Hosts))
		for k := range c.Hosts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, name := range keys {
			h := c.Hosts[name]
			if h.Socket == "" {
				return fmt.Errorf("config: hosts[%q].socket is required", name)
			}
			if h.User == "" {
				return fmt.Errorf("config: hosts[%q].user is required", name)
			}
			if h.Host == "" {
				return fmt.Errorf("config: hosts[%q].host is required", name)
			}
		}
		// MHST-04: fail fast if default_host is absent or names a missing key (D-05).
		if c.DefaultHost == "" {
			return errors.New("config: hosts is non-empty but default_host is absent")
		}
		if _, ok := c.Hosts[c.DefaultHost]; !ok {
			return fmt.Errorf("config: default_host %q is not a key in hosts", c.DefaultHost)
		}
	}

	// 3. Compile Safeguards patterns (unchanged; runs after host resolution).
	for i, pat := range c.Safeguards.Patterns {
		re, err := regexp.Compile(pat)
		if err != nil {
			return fmt.Errorf("config: safeguards.patterns[%d] invalid regex %q: %w", i, pat, err)
		}
		c.Safeguards.CompiledPatterns = append(c.Safeguards.CompiledPatterns, re)
	}
	return nil
}

// configPath resolves the default config file path, expanding a leading ~/
// using os.UserHomeDir(). The ~ is a shell feature, not an OS feature —
// Go's stdlib does not expand it automatically (see RESEARCH.md Pitfall 5).
func configPath() (string, error) {
	p := defaultConfigPath
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("get home dir: %w", err)
		}
		p = filepath.Join(home, p[2:])
	}
	return p, nil
}
