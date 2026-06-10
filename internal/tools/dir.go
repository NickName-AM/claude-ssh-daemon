package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
	"github.com/NickName-AM/claude-ssh-daemon/internal/guard"
	"github.com/NickName-AM/claude-ssh-daemon/internal/ssh"
)

// ListDirInput holds the parameters for the ssh_list_dir tool.
type ListDirInput struct {
	Path string `json:"path"           jsonschema:"absolute remote directory path"`
	Host string `json:"host,omitempty" jsonschema:"named SSH host; omit to use default_host"`
}

// DirEntry represents a single entry returned by ssh_list_dir (D-05, D-06).
type DirEntry struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Size        int64  `json:"size"`
	Permissions string `json:"permissions"`
	Mtime       string `json:"mtime"`
}

// ListDirOutput is the structured response for ssh_list_dir.
type ListDirOutput struct {
	Entries          []DirEntry `json:"entries"`
	InjectionWarning string     `json:"_injection_warning,omitempty"`
}

// parseLsLine parses a single line from ls -la output into a DirEntry.
// Returns (entry, true) on success and (DirEntry{}, false) for lines with
// too few fields or the "total N" header line.
func parseLsLine(line string) (DirEntry, bool) {
	fields := strings.Fields(line)
	// Minimum: perms nlinks user group size month day time name = 9 fields
	if len(fields) < 9 {
		return DirEntry{}, false
	}

	perms := fields[0]
	// Skip "total N" header
	if strings.HasPrefix(perms, "total") {
		return DirEntry{}, false
	}
	// Permissions field must start with a file-type character
	if len(perms) == 0 {
		return DirEntry{}, false
	}

	entryType := "file"
	switch perms[0] {
	case 'd':
		entryType = "dir"
	case 'l':
		entryType = "symlink"
	}

	size, _ := strconv.ParseInt(fields[4], 10, 64)
	mtime := fields[5] + " " + fields[6] + " " + fields[7]

	name := strings.Join(fields[8:], " ")
	// For symlinks, strip " -> target" suffix (D-05)
	if entryType == "symlink" {
		if idx := strings.Index(name, " -> "); idx != -1 {
			name = name[:idx]
		}
	}

	return DirEntry{
		Name:        name,
		Type:        entryType,
		Size:        size,
		Permissions: perms,
		Mtime:       mtime,
	}, true
}

// parseLsOutput parses the full ls -la output string into a slice of DirEntry.
// Skips the "total N" header line; includes . and .. entries as returned by ls -la (D-06).
func parseLsOutput(raw string) []DirEntry {
	var entries []DirEntry
	for _, line := range strings.Split(raw, "\n") {
		entry, ok := parseLsLine(line)
		if ok {
			entries = append(entries, entry)
		}
	}
	if entries == nil {
		entries = []DirEntry{}
	}
	return entries
}

// listDirHandler returns a ToolHandlerFor closure for the ssh_list_dir tool.
// It calls exec.ListDir, parses the ls -la output, and returns structured entries.
//
// GURD-02: each entry's Name field is scanned for injection patterns. The raw
// ls -la string is never scanned — permission strings and timestamps would produce
// false positives (Pitfall 2). On the first match, InjectionWarning is set via
// formatInjectionWarning and scanning stops (one warning is sufficient). Injection
// hits never set IsError (GURD-01 annotation only).
func listDirHandler(registry map[string]ssh.SSHExecutor, cfg *config.Config) mcp.ToolHandlerFor[ListDirInput, ListDirOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListDirInput) (*mcp.CallToolResult, ListDirOutput, error) {
		// MHST-05/06/07: resolve the executor for the requested host.
		exec, hostName, errResult := resolveExecutor(registry, cfg, in.Host)
		if errResult != nil {
			return errResult, ListDirOutput{}, nil
		}

		// BDIR-01: base_dir sandbox — reject paths that resolve outside the
		// configured base directory (lexical check, no symlink resolution; BDIR-03).
		if baseDir := cfg.Hosts[hostName].BaseDir; baseDir != "" {
			if !withinBaseDir(baseDir, in.Path) {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{
						Text: fmt.Sprintf("[host %s] path %q is outside base_dir %q", hostName, in.Path, baseDir),
					}},
				}, ListDirOutput{}, nil
			}
		}

		raw, err := exec.ListDir(ctx, in.Path)
		if err != nil {
			// MHST-08: prefix error with resolved host name.
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("[host %s] %s", hostName, err.Error())}},
			}, ListDirOutput{}, nil
		}

		out := ListDirOutput{Entries: parseLsOutput(raw)}

		// GURD-02: scan each entry Name individually. Scanning entry.Name (not the
		// raw ls -la string) prevents permission strings and timestamps from
		// triggering false positives (Pitfall 2). Break on first hit — one warning
		// is sufficient. Matched text is never reflected (GURD-01 invariant).
		if !cfg.Safeguards.GuardDisabled {
			for _, entry := range out.Entries {
				if r := guard.ScanWithPatterns(entry.Name, cfg.Safeguards.CompiledPatterns); r.Matches != nil {
					out.InjectionWarning = formatInjectionWarning(r)
					break
				}
			}
		}

		return nil, out, nil
	}
}
