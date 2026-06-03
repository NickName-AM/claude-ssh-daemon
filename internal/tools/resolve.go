package tools

import (
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
	"github.com/NickName-AM/claude-ssh-daemon/internal/ssh"
)

// resolveExecutor looks up the executor for hostParam (or cfg.DefaultHost if
// hostParam is empty). Returns the executor, the resolved host name (always
// non-empty on success — Pitfall 6), and nil error result on success.
//
// On registry miss, returns (nil, "", errResult) where errResult is a pre-built
// *mcp.CallToolResult with IsError=true listing the configured host names in
// sorted order (MHST-07). Callers return early with:
//
//	return errResult, ZeroOutput{}, nil
func resolveExecutor(
	registry map[string]ssh.SSHExecutor,
	cfg *config.Config,
	hostParam string,
) (ssh.SSHExecutor, string, *mcp.CallToolResult) {
	name := hostParam
	if name == "" {
		// MHST-06: empty host param defaults to cfg.DefaultHost.
		name = cfg.DefaultHost
	}

	exec, ok := registry[name]
	if !ok {
		// MHST-07: list configured names so the user knows what is available.
		// Use sortedKeys(registry) for deterministic error messages (Pitfall 2).
		return nil, "", &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{
				Text: fmt.Sprintf(
					"unknown host %q; configured hosts: %s",
					hostParam,
					strings.Join(sortedKeys(registry), ", "),
				),
			}},
		}
	}

	// Return the executor and the resolved name. The resolved name is always
	// non-empty here because cfg.DefaultHost is set by config.Validate() (Pitfall 6).
	return exec, name, nil
}

// sortedKeys returns the keys of m in ascending lexicographic order.
// Used by resolveExecutor for deterministic error message host lists,
// and by statusHandler for deterministic status output ordering (Pitfall 2).
func sortedKeys(m map[string]ssh.SSHExecutor) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
