package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
	"github.com/NickName-AM/claude-ssh-daemon/internal/ssh"
)

type readFileOutput struct {
	Content          string `json:"content"`
	Encoding         string `json:"encoding"`
	InjectionWarning string `json:"_injection_warning"`
}

func newFileReadTestServer(t *testing.T, exec *toolsMockExecutor, fileRead bool) *mcp.ClientSession {
	t.Helper()
	cfg := &config.Config{
		SSHSocket: "/tmp/test.sock",
		SSHUser:   "user",
		SSHHost:   "host",
		Capabilities: config.Capabilities{
			FileRead: fileRead,
		},
	}
	registry := singleHostRegistry(exec, cfg)
	return newTestServer(t, registry, cfg)
}

func newFileReadTestServerWithSafeguards(t *testing.T, exec *toolsMockExecutor, s config.Safeguards) *mcp.ClientSession {
	t.Helper()
	cfg := &config.Config{
		SSHSocket: "/tmp/test.sock",
		SSHUser:   "user",
		SSHHost:   "host",
		Capabilities: config.Capabilities{
			FileRead: true,
		},
		Safeguards: s,
	}
	registry := singleHostRegistry(exec, cfg)
	return newTestServer(t, registry, cfg)
}

// TestReadFileTextEncoding verifies that a text file (us-ascii encoding) is
// returned with encoding:"utf-8" and the raw content string.
func TestReadFileTextEncoding(t *testing.T) {
	mock := &toolsMockExecutor{
		encodingResult: "us-ascii",
		readContent:    []byte("hello world"),
	}
	cs := newFileReadTestServer(t, mock, true)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_read_file",
		Arguments: map[string]any{"path": "/remote/file.txt"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "text read must not set isError")

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var out readFileOutput
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.Equal(t, "utf-8", out.Encoding, "text file must use utf-8 encoding")
	require.Equal(t, "hello world", out.Content)
}

// TestReadFileBinaryEncoding verifies that a binary file is base64-encoded
// and returned with encoding:"base64". Round-trip decode must match original bytes.
func TestReadFileBinaryEncoding(t *testing.T) {
	originalBytes := []byte{0x00, 0xFF, 0x42, 0x01}
	mock := &toolsMockExecutor{
		encodingResult: "binary",
		readContent:    originalBytes,
	}
	cs := newFileReadTestServer(t, mock, true)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_read_file",
		Arguments: map[string]any{"path": "/remote/image.bin"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "binary read must not set isError")

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var out readFileOutput
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.Equal(t, "base64", out.Encoding, "binary file must use base64 encoding")

	decoded, decErr := base64.StdEncoding.DecodeString(out.Content)
	require.NoError(t, decErr, "returned content must be valid base64")
	require.Equal(t, originalBytes, decoded, "decoded bytes must match original")
}

// TestReadFileDetectEncodingError verifies that a DetectEncoding failure sets
// result.IsError == true.
func TestReadFileDetectEncodingError(t *testing.T) {
	mock := &toolsMockExecutor{
		encodingErr: errors.New("file command not found"),
	}
	cs := newFileReadTestServer(t, mock, true)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_read_file",
		Arguments: map[string]any{"path": "/remote/file.txt"},
	})
	require.NoError(t, err)
	require.True(t, result.IsError, "detect encoding error must set isError")
}

// TestReadFileReadError verifies that a ReadFile failure sets result.IsError == true.
func TestReadFileReadError(t *testing.T) {
	mock := &toolsMockExecutor{
		encodingResult: "utf-8",
		readErr:        errors.New("permission denied"),
	}
	cs := newFileReadTestServer(t, mock, true)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_read_file",
		Arguments: map[string]any{"path": "/remote/file.txt"},
	})
	require.NoError(t, err)
	require.True(t, result.IsError, "read error must set isError")
}

// TestReadFileAbsentWhenCapFileReadFalse verifies ssh_read_file is not in
// tools/list when capabilities.file_read is false (SECU-01).
func TestReadFileAbsentWhenCapFileReadFalse(t *testing.T) {
	cs := newFileReadTestServer(t, &toolsMockExecutor{}, false)

	toolsList, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)

	for _, tool := range toolsList.Tools {
		require.NotEqual(t, "ssh_read_file", tool.Name)
	}
}

// TestReadFilePresentWithReadOnlyHintWhenCapFileReadTrue verifies ssh_read_file
// appears with readOnlyHint:true when capabilities.file_read is true.
func TestReadFilePresentWithReadOnlyHintWhenCapFileReadTrue(t *testing.T) {
	cs := newFileReadTestServer(t, &toolsMockExecutor{}, true)

	toolsList, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)

	var found bool
	for _, tool := range toolsList.Tools {
		if tool.Name == "ssh_read_file" {
			found = true
			require.NotNil(t, tool.Annotations)
			require.True(t, tool.Annotations.ReadOnlyHint, "ssh_read_file must have readOnlyHint:true")
			break
		}
	}
	require.True(t, found, "ssh_read_file must appear when file_read capability is true")
}

// TestReadFileTextInjectionWarningSet verifies that when text-file content
// contains an injection pattern, _injection_warning contains the category
// (e.g., "xml_tool_tags"), IsError is false, and the matched text is not
// reflected in the warning (GURD-01).
func TestReadFileTextInjectionWarningSet(t *testing.T) {
	mock := &toolsMockExecutor{
		encodingResult: "us-ascii",
		readContent:    []byte("<tool_call>x</tool_call>"),
	}
	cs := newFileReadTestServerWithSafeguards(t, mock, config.Safeguards{GuardDisabled: false})

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_read_file",
		Arguments: map[string]any{"path": "/remote/file.txt"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "injection hit must NOT set isError (GURD-01 annotation only)")

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var out readFileOutput
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.NotEmpty(t, out.InjectionWarning, "_injection_warning must be set for injection content")
	require.Contains(t, out.InjectionWarning, "xml_tool_tags", "warning must name the matched category")
	require.NotContains(t, out.InjectionWarning, "tool_call", "matched tag text must NOT appear in warning (GURD-01)")
	require.NotContains(t, out.InjectionWarning, "<tool_call>", "matched tag text must NOT appear in warning (GURD-01)")
}

// TestReadFileBinaryInjectionWarningAbsent verifies that binary (base64) content
// containing injection bytes is never scanned and _injection_warning is absent.
// Binary content is always base64-encoded and scanning raw bytes would be
// misleading (Pitfall 1).
func TestReadFileBinaryInjectionWarningAbsent(t *testing.T) {
	// Bytes that contain "<tool_call>" if interpreted as ASCII.
	mock := &toolsMockExecutor{
		encodingResult: "binary",
		readContent:    []byte("<tool_call>evil</tool_call>"),
	}
	cs := newFileReadTestServerWithSafeguards(t, mock, config.Safeguards{GuardDisabled: false})

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_read_file",
		Arguments: map[string]any{"path": "/remote/image.bin"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var out readFileOutput
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.Equal(t, "base64", out.Encoding, "binary file must use base64 encoding")
	require.Empty(t, out.InjectionWarning, "_injection_warning must be absent for binary content")
}

// TestReadFileCleanTextNoInjectionWarning verifies that clean text content
// produces no _injection_warning (omitempty must suppress the field).
func TestReadFileCleanTextNoInjectionWarning(t *testing.T) {
	mock := &toolsMockExecutor{
		encodingResult: "us-ascii",
		readContent:    []byte("hello world, this is clean content"),
	}
	cs := newFileReadTestServerWithSafeguards(t, mock, config.Safeguards{GuardDisabled: false})

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_read_file",
		Arguments: map[string]any{"path": "/remote/clean.txt"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var out readFileOutput
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.Empty(t, out.InjectionWarning, "_injection_warning must be absent for clean content")
}

// TestReadFileGuardDisabledNoWarning verifies that when GuardDisabled=true,
// _injection_warning is absent even when text content contains injection patterns.
func TestReadFileGuardDisabledNoWarning(t *testing.T) {
	mock := &toolsMockExecutor{
		encodingResult: "us-ascii",
		readContent:    []byte("<tool_call>x</tool_call>"),
	}
	cs := newFileReadTestServerWithSafeguards(t, mock, config.Safeguards{GuardDisabled: true})

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_read_file",
		Arguments: map[string]any{"path": "/remote/file.txt"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var out readFileOutput
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.Empty(t, out.InjectionWarning, "_injection_warning must be absent when guard is disabled")
}

// newBaseDirServer builds a test server with BaseDir set on the "default" host.
// cfg.Hosts is built directly (not via singleHostRegistry) to preserve the BaseDir
// field, which singleHostRegistry overwrites (mirrors newExecAllowlistServer pattern).
func newBaseDirServer(t *testing.T, exec ssh.SSHExecutor, baseDir string) *mcp.ClientSession {
	t.Helper()
	cfg := &config.Config{
		MCPSocket:   "/tmp/mcp.sock",
		DefaultHost: "default",
		Hosts: map[string]config.HostConfig{
			"default": {
				Socket:  "/tmp/test.sock",
				User:    "user",
				Host:    "host",
				BaseDir: baseDir,
			},
		},
		Capabilities: config.Capabilities{FileRead: true, FileWrite: true},
		Safeguards:   config.Safeguards{AllowOverwrite: true},
	}
	registry := map[string]ssh.SSHExecutor{"default": exec}
	return newTestServer(t, registry, cfg)
}

// TestReadFileBaseDirOutsidePathRejected verifies that readFileHandler returns
// isError:true when the requested path is outside base_dir (BDIR-01, D-03).
func TestReadFileBaseDirOutsidePathRejected(t *testing.T) {
	mock := &toolsMockExecutor{encodingResult: "us-ascii", readContent: []byte("secret")}
	cs := newBaseDirServer(t, mock, "/srv/app")

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_read_file",
		Arguments: map[string]any{"path": "/etc/passwd"},
	})
	require.NoError(t, err)
	require.True(t, result.IsError, "path outside base_dir must set isError (BDIR-01)")

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, "[host default]", "error must include host name prefix")
	require.Contains(t, text.Text, "outside base_dir", "error must describe the violation")
	require.Contains(t, text.Text, "/srv/app", "error must include the configured base_dir value (D-03)")
}

// TestReadFileBaseDirTraversalRejected verifies that a path using "../" traversal
// that lexically resolves outside base_dir is rejected (BDIR-01).
func TestReadFileBaseDirTraversalRejected(t *testing.T) {
	mock := &toolsMockExecutor{encodingResult: "us-ascii", readContent: []byte("secret")}
	cs := newBaseDirServer(t, mock, "/srv/app")

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_read_file",
		Arguments: map[string]any{"path": "/srv/app/../etc/passwd"},
	})
	require.NoError(t, err)
	require.True(t, result.IsError, "traversal path outside base_dir must set isError")
}

// TestReadFileBaseDirInsidePathPassThrough verifies that a path inside base_dir
// proceeds to the executor without error (BDIR-01 positive case).
func TestReadFileBaseDirInsidePathPassThrough(t *testing.T) {
	mock := &toolsMockExecutor{encodingResult: "us-ascii", readContent: []byte("ok")}
	cs := newBaseDirServer(t, mock, "/srv/app")

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_read_file",
		Arguments: map[string]any{"path": "/srv/app/conf.txt"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "in-sandbox path must not be rejected")
}

// TestReadFileBaseDirEmptyNoCheck verifies that when base_dir is empty,
// readFileHandler proceeds without any sandbox check (unchanged behavior).
func TestReadFileBaseDirEmptyNoCheck(t *testing.T) {
	mock := &toolsMockExecutor{encodingResult: "us-ascii", readContent: []byte("ok")}
	cs := newBaseDirServer(t, mock, "")

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_read_file",
		Arguments: map[string]any{"path": "/etc/passwd"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "empty base_dir must not block any path")
}
