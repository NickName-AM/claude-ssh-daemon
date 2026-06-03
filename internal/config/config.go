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

// Config holds the daemon configuration loaded from the JSON config file.
// Phase 1 fields: ssh_socket, mcp_socket, capabilities.
// Phase 2 fields: ssh_user, ssh_host (D-03, required).
type Config struct {
	SSHSocket    string       `json:"ssh_socket"`
	MCPSocket    string       `json:"mcp_socket"`
	SSHUser      string       `json:"ssh_user"`   // added Phase 2 (D-03)
	SSHHost      string       `json:"ssh_host"`   // added Phase 2 (D-03)
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

// Validate checks that required fields are present. Returns a field-specific
// error message per D-03 and CONTEXT.md §Specific Ideas.
// ssh_socket is checked first, then mcp_socket, then ssh_user, then ssh_host.
func (c *Config) Validate() error {
	if c.SSHSocket == "" {
		return errors.New("config: ssh_socket is required")
	}
	if c.MCPSocket == "" {
		return errors.New("config: mcp_socket is required")
	}
	// Phase 2 additions (D-03): both ssh_user and ssh_host are required.
	if c.SSHUser == "" {
		return errors.New("config: ssh_user is required")
	}
	if c.SSHHost == "" {
		return errors.New("config: ssh_host is required")
	}
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
