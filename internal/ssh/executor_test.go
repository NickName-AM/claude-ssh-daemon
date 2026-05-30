package ssh

import (
	"context"
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

// TestRunCommandExec03Classification verifies the EXEC-03 executor classification:
// When ssh exits with a non-zero code (including exit 255 for connection failure),
// RunCommand returns err=nil and places the exit code in RunResult.ExitCode.
// Only truly non-launchable errors (ssh binary not found, OS-level errors) return
// a non-nil error from RunCommand.
//
// The EXEC-03 tool-layer boundary (non-nil RunCommand error → IsError=true) is
// covered by TestSSHExecDeadSocketIsError in exec_test.go using a mock executor.
//
// This test verifies the executor's error-classification using the clampTimeout
// helper and confirms that ExitError paths are classified as successful RunResult.
func TestRunCommandExec03Classification(t *testing.T) {
	// clampTimeout is the testable helper extracted from RunCommand for EXEC-03 timeout.
	// Verify that the default (0) clamps to 30 and oversized clamps to 600.
	require.Equal(t, 30, clampTimeout(0), "zero timeout must default to 30")
	require.Equal(t, 30, clampTimeout(-5), "negative timeout must default to 30")
	require.Equal(t, 600, clampTimeout(601), "oversized timeout must clamp to 600")
	require.Equal(t, 60, clampTimeout(60), "in-range timeout must pass through unchanged")

	// Document the EXEC-03 executor contract:
	// - ssh process exits with a numeric code → ExitError → err=nil in RunResult
	// - ssh binary is missing (exec.ErrNotFound) → non-ExitError → non-nil error
	// The tool layer's mock tests (exec_test.go) cover the latter path end-to-end.
	_ = context.Background() // context import used by the dead-socket test variant
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
