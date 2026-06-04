// Package ssh provides the SSHExecutor interface and ControlMasterExecutor
// implementation wrapping os/exec ssh -S <socket> for all remote operations.
// A non-zero remote exit code is NOT an executor error — only broken pipe,
// missing binary, or dead socket returns a non-nil error.
package ssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// SSHExecutor abstracts all SSH subprocess calls.
// Each method constructs and runs one ssh subprocess via the ControlMaster socket.
// A non-zero remote exit code is NOT an executor error — only broken pipe,
// missing binary, or dead socket returns a non-nil error.
type SSHExecutor interface {
	// RunCommand runs a remote command. Returns stdout, stderr, exit code,
	// duration, and whether it timed out. A non-zero exit code is NOT an error.
	// Dead socket returns an error.
	RunCommand(ctx context.Context, req RunRequest) (RunResult, error)

	// ReadFile reads a remote file as bytes. Caller does binary detection first.
	ReadFile(ctx context.Context, remotePath string) ([]byte, error)

	// DetectEncoding runs 'file --mime-encoding remotePath' on the remote host.
	// Returns "binary" or an IANA charset name.
	DetectEncoding(ctx context.Context, remotePath string) (string, error)

	// WriteFile pipes content to 'cat > remotePath' on the remote host.
	WriteFile(ctx context.Context, remotePath string, content []byte) error

	// ListDir runs 'ls -la remotePath' and returns raw output for parsing.
	ListDir(ctx context.Context, remotePath string) (string, error)

	// UploadFile opens localPath and pipes bytes to 'cat > remotePath'.
	UploadFile(ctx context.Context, localPath, remotePath string) error

	// DownloadFile reads 'cat remotePath' stdout and writes to localPath.
	DownloadFile(ctx context.Context, remotePath, localPath string) error

	// CheckSocket runs 'ssh -S socket -O check user@host'.
	// Returns nil if alive, error if dead.
	CheckSocket(ctx context.Context) error
}

// RunRequest holds parameters for a remote command invocation.
type RunRequest struct {
	Command        string
	Cwd            string
	TimeoutSeconds int
	Env            map[string]string
}

// RunResult holds the result of a remote command invocation.
type RunResult struct {
	Stdout     string
	Stderr     string
	ExitCode   int
	DurationMs int64
	TimedOut   bool
}

// ControlMasterExecutor implements SSHExecutor using os/exec ssh -S <socket>.
type ControlMasterExecutor struct {
	Socket string // path to ControlMaster socket
	User   string
	Host   string
}

// Compile-time assertion: ControlMasterExecutor satisfies SSHExecutor.
var _ SSHExecutor = (*ControlMasterExecutor)(nil)

// posixVarName matches valid POSIX shell variable names.
// Env keys that do not match are rejected to prevent shell command injection
// (CR-001: key names were previously passed verbatim into the remote command string).
var posixVarName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// clampTimeout clamps TimeoutSeconds to [1, 600], defaulting to 30 when <= 0.
// Extracted as a testable helper.
func clampTimeout(t int) int {
	if t <= 0 {
		return 30
	}
	if t > 600 {
		return 600
	}
	return t
}

// RunCommand runs a remote command via the ControlMaster socket.
// Non-zero exit codes are returned as RunResult.ExitCode with err=nil (EXEC-03).
// Dead socket or subprocess failure returns a non-nil error.
func (e *ControlMasterExecutor) RunCommand(ctx context.Context, req RunRequest) (RunResult, error) {
	timeout := clampTimeout(req.TimeoutSeconds)
	cmdCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	// Build remote command: wrap in cd if cwd is specified.
	remote := req.Command
	if req.Cwd != "" {
		remote = fmt.Sprintf("cd %s && %s", shellescape(req.Cwd), req.Command)
	}
	// Prepend env vars inline: VAR=val cmd (POSIX-portable; no -o SendEnv needed).
	// CR-001: validate every key against the POSIX variable-name pattern before
	// building the command string; keys containing shell metacharacters would be
	// injected verbatim, bypassing the allow_delete safeguard entirely.
	if len(req.Env) > 0 {
		var envParts []string
		for k, v := range req.Env {
			if !posixVarName.MatchString(k) {
				return RunResult{}, fmt.Errorf(
					"invalid env key %q: must match [A-Za-z_][A-Za-z0-9_]*", k)
			}
			envParts = append(envParts, fmt.Sprintf("%s=%s", k, shellescape(v)))
		}
		remote = strings.Join(envParts, " ") + " " + remote
	}

	// CRITICAL: args as separate slice elements — never concatenate into sh -c (STATE.md pitfall).
	args := []string{"-S", e.Socket, "-o", "BatchMode=yes", e.User + "@" + e.Host, remote}
	cmd := exec.CommandContext(cmdCtx, "ssh", args...)

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	start := time.Now()
	stdout, err := cmd.Output()
	elapsed := time.Since(start).Milliseconds()

	timedOut := ctx.Err() == context.DeadlineExceeded || cmdCtx.Err() == context.DeadlineExceeded
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
			err = nil // non-zero exit is NOT an executor error (EXEC-03)
		} else {
			return RunResult{}, fmt.Errorf("ssh subprocess error (socket dead?): %w", err)
		}
	}
	return RunResult{
		Stdout:     string(stdout),
		Stderr:     stderrBuf.String(),
		ExitCode:   exitCode,
		DurationMs: elapsed,
		TimedOut:   timedOut,
	}, nil
}

// ReadFile reads a remote file as raw bytes via stdout capture.
func (e *ControlMasterExecutor) ReadFile(ctx context.Context, remotePath string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ssh",
		"-S", e.Socket, "-o", "BatchMode=yes",
		e.User+"@"+e.Host,
		"cat "+shellescape(remotePath),
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("read remote file %s: %w", remotePath, err)
	}
	return out, nil
}

// DetectEncoding runs 'file --mime-encoding remotePath' on the remote host.
// Returns "binary" or an IANA charset name (e.g., "us-ascii", "utf-8").
func (e *ControlMasterExecutor) DetectEncoding(ctx context.Context, remotePath string) (string, error) {
	cmd := exec.CommandContext(ctx, "ssh",
		"-S", e.Socket, "-o", "BatchMode=yes",
		e.User+"@"+e.Host,
		"file --mime-encoding "+shellescape(remotePath),
	)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("file --mime-encoding: %w", err)
	}
	// Output format: "/path/to/file: us-ascii\n"
	parts := strings.SplitN(strings.TrimSpace(string(out)), ": ", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("unexpected file --mime-encoding output: %q", out)
	}
	return strings.TrimSpace(parts[1]), nil
}

// WriteFile pipes content to 'cat > remotePath' on the remote host.
func (e *ControlMasterExecutor) WriteFile(ctx context.Context, remotePath string, content []byte) error {
	cmd := exec.CommandContext(ctx, "ssh",
		"-S", e.Socket, "-o", "BatchMode=yes",
		e.User+"@"+e.Host,
		"cat > "+shellescape(remotePath),
	)
	cmd.Stdin = bytes.NewReader(content)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("write remote file %s: %w", remotePath, err)
	}
	return nil
}

// ListDir runs 'ls -la remotePath' and returns the raw output for parsing.
func (e *ControlMasterExecutor) ListDir(ctx context.Context, remotePath string) (string, error) {
	cmd := exec.CommandContext(ctx, "ssh",
		"-S", e.Socket, "-o", "BatchMode=yes",
		e.User+"@"+e.Host,
		"ls -la "+shellescape(remotePath),
	)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("list remote dir %s: %w", remotePath, err)
	}
	return string(out), nil
}

// UploadFile opens localPath and pipes its bytes to 'cat > remotePath' on the remote host.
// D-02: localPath must be an absolute path; enforcement is the caller's responsibility.
func (e *ControlMasterExecutor) UploadFile(ctx context.Context, localPath, remotePath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local file: %w", err)
	}
	defer f.Close()

	cmd := exec.CommandContext(ctx, "ssh",
		"-S", e.Socket, "-o", "BatchMode=yes",
		e.User+"@"+e.Host,
		"cat > "+shellescape(remotePath),
	)
	cmd.Stdin = f
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("upload to %s: %w", remotePath, err)
	}
	return nil
}

// DownloadFile reads 'cat remotePath' stdout and writes to localPath.
// D-02: localPath must be an absolute path; enforcement is the caller's responsibility.
func (e *ControlMasterExecutor) DownloadFile(ctx context.Context, remotePath, localPath string) error {
	cmd := exec.CommandContext(ctx, "ssh",
		"-S", e.Socket, "-o", "BatchMode=yes",
		e.User+"@"+e.Host,
		"cat "+shellescape(remotePath),
	)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("download %s: %w", remotePath, err)
	}
	return os.WriteFile(localPath, out, 0600)
}

// CheckSocket runs 'ssh -S socket -O check user@host'.
// Returns nil if the ControlMaster is alive, or a descriptive error with a
// re-establishment hint if the socket is dead or absent (DMON-04, CONN-01).
func (e *ControlMasterExecutor) CheckSocket(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "ssh",
		"-S", e.Socket,
		"-O", "check",
		e.User+"@"+e.Host,
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ControlMaster socket %s is not alive: run 'ssh -M -S %s %s@%s' to re-establish",
			e.Socket, e.Socket, e.User, e.Host)
	}
	return nil
}

// shellescape wraps s in single quotes, escaping any embedded single quotes.
// Used to prevent path injection when s appears in the remote command string.
// Example: a'b -> 'a'\''b'
func shellescape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
