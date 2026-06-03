package tools

import (
	"context"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
	"github.com/NickName-AM/claude-ssh-daemon/internal/ssh"
)

// ListDirInput holds the parameters for the ssh_list_dir tool.
type ListDirInput struct {
	Path string `json:"path" jsonschema:"absolute remote directory path"`
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
	Entries []DirEntry `json:"entries"`
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
// It calls e.ListDir, parses the ls -la output, and returns structured entries.
func listDirHandler(e ssh.SSHExecutor, cfg *config.Config) mcp.ToolHandlerFor[ListDirInput, ListDirOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ListDirInput) (*mcp.CallToolResult, ListDirOutput, error) {
		raw, err := e.ListDir(ctx, in.Path)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, ListDirOutput{}, nil
		}
		return nil, ListDirOutput{Entries: parseLsOutput(raw)}, nil
	}
}
