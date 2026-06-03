package tools

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/NickName-AM/claude-ssh-daemon/internal/guard"
)

// formatInjectionWarning builds a human-readable summary of detected injection
// signals from the guard result. It uses only the category label and match count
// from each Match — matched text is NEVER read or reflected (GURD-01 invariant).
//
// Example output: "potential injection: xml_tool_tags(2), authority_tags(1)"
func formatInjectionWarning(r guard.Result) string {
	parts := make([]string, 0, len(r.Matches))
	for _, m := range r.Matches {
		parts = append(parts, fmt.Sprintf("%s(%d)", m.Category, m.Count))
	}
	return "potential injection: " + strings.Join(parts, ", ")
}

// destructiveCommands is the set of command basenames that require
// allow_delete=true before execution (SAFE-02).
var destructiveCommands = map[string]struct{}{
	"rm":       {},
	"unlink":   {},
	"truncate": {},
	"shred":    {},
	"dd":       {},
}

// isDestructiveCommand reports whether cmd starts with a destructive command
// basename. It uses filepath.Base to handle path-prefixed commands such as
// /bin/rm (SAFE-02 Pitfall 6). Returns the matched basename and true when
// destructive, or ("", false) otherwise.
//
// Scope: first-token check only. Shell wrappers such as "sudo rm", "bash -c rm",
// or "sh -c rm" are NOT blocked — only commands where rm/unlink/truncate/shred/dd
// appears directly as the first word are caught.
func isDestructiveCommand(cmd string) (string, bool) {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return "", false
	}
	base := filepath.Base(fields[0])
	if _, ok := destructiveCommands[base]; ok {
		return base, true
	}
	return "", false
}
