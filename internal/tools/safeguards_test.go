package tools

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/NickName-AM/claude-ssh-daemon/internal/guard"
)

// TestIsDestructiveCommandTable tests isDestructiveCommand with a variety of inputs.
func TestIsDestructiveCommandTable(t *testing.T) {
	cases := []struct {
		cmd      string
		wantName string
		wantOk   bool
	}{
		{"rm -rf /tmp/x", "rm", true},
		{"/bin/rm file", "rm", true},
		{"unlink /tmp/foo", "unlink", true},
		{"truncate -s 0 /var/log/syslog", "truncate", true},
		{"shred -u secret.txt", "shred", true},
		{"dd if=/dev/zero of=/tmp/out", "dd", true},
		{"ls -la", "", false},
		{"cat /etc/passwd", "", false},
		{"", "", false},
		{"   ", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.cmd, func(t *testing.T) {
			name, ok := isDestructiveCommand(tc.cmd)
			require.Equal(t, tc.wantOk, ok, "ok mismatch for %q", tc.cmd)
			require.Equal(t, tc.wantName, name, "name mismatch for %q", tc.cmd)
		})
	}
}

// TestFormatInjectionWarningContainsCategoryAndCount tests that formatInjectionWarning
// builds the expected string from category and count only, never from matched text.
func TestFormatInjectionWarningContainsCategoryAndCount(t *testing.T) {
	r := guard.Result{
		Matches: []guard.Match{
			{Category: "xml_tool_tags", Count: 2},
			{Category: "authority_tags", Count: 1},
		},
	}
	warning := formatInjectionWarning(r)
	require.Contains(t, warning, "xml_tool_tags(2)", "must include category+count for xml_tool_tags")
	require.Contains(t, warning, "authority_tags(1)", "must include category+count for authority_tags")
	require.Contains(t, warning, "potential injection:", "must start with 'potential injection:'")
	// Security: matched text must never appear.
	require.NotContains(t, warning, "do evil", "matched text must not appear in warning")
	require.NotContains(t, warning, "tool_call", "matched text must not appear in warning")
}
