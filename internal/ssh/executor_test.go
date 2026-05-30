package ssh

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestShellEscape verifies that shellescape correctly wraps strings in single
// quotes and escapes embedded single quotes using the ' -> '\'' technique.
func TestShellEscape(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple path",
			input: "/home/user/file.txt",
			want:  "'/home/user/file.txt'",
		},
		{
			name:  "path with spaces",
			input: "/home/user/my file.txt",
			want:  "'/home/user/my file.txt'",
		},
		{
			name:  "single quote in string",
			input: "a'b",
			want:  "'a'\\''b'",
		},
		{
			name:  "multiple single quotes",
			input: "it's a test's string",
			want:  "'it'\\''s a test'\\''s string'",
		},
		{
			name:  "empty string",
			input: "",
			want:  "''",
		},
		{
			name:  "shell metacharacters",
			input: "$HOME && rm -rf /",
			want:  "'$HOME && rm -rf /'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellescape(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

// TestControlMasterExecutorCompileTimeAssertion verifies that ControlMasterExecutor
// satisfies the SSHExecutor interface at compile time.
// The var _ declaration in executor.go already enforces this; this test documents
// the assertion and will fail to compile if the interface is broken.
func TestControlMasterExecutorCompileTimeAssertion(t *testing.T) {
	// If this compiles, the assertion passes.
	var _ SSHExecutor = (*ControlMasterExecutor)(nil)
}

// TestRunRequestTimeoutClamping verifies the timeout clamping logic:
// - TimeoutSeconds <= 0 is clamped to 30 (default)
// - TimeoutSeconds > 600 is clamped to 600 (maximum)
// - Values within [1, 600] are returned unchanged.
func TestRunRequestTimeoutClamping(t *testing.T) {
	tests := []struct {
		name    string
		input   int
		wantMin int
		wantMax int
	}{
		{
			name:    "zero becomes default 30",
			input:   0,
			wantMin: 30,
			wantMax: 30,
		},
		{
			name:    "negative becomes default 30",
			input:   -1,
			wantMin: 30,
			wantMax: 30,
		},
		{
			name:    "positive within range is unchanged",
			input:   60,
			wantMin: 60,
			wantMax: 60,
		},
		{
			name:    "exactly 600 is not clamped",
			input:   600,
			wantMin: 600,
			wantMax: 600,
		},
		{
			name:    "over 600 is clamped to 600",
			input:   999,
			wantMin: 600,
			wantMax: 600,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampTimeout(tt.input)
			require.Equal(t, tt.wantMin, got)
			require.Equal(t, tt.wantMax, got)
		})
	}
}
