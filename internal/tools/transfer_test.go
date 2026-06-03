package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
	"github.com/NickName-AM/claude-ssh-daemon/internal/ssh"
)

type uploadOutput struct {
	Uploaded   bool   `json:"uploaded"`
	RemotePath string `json:"remote_path"`
}

type downloadOutput struct {
	Downloaded bool   `json:"downloaded"`
	LocalPath  string `json:"local_path"`
}

func newFileWriteFullTestServer(t *testing.T, exec *toolsMockExecutor, fileWrite bool) *mcp.ClientSession {
	t.Helper()
	cfg := &config.Config{
		SSHSocket: "/tmp/test.sock",
		SSHUser:   "user",
		SSHHost:   "host",
		Capabilities: config.Capabilities{
			FileWrite: fileWrite,
		},
		// AllowOverwrite: true keeps existing upload/download tests focused on
		// the upload/download path rather than the SAFE-01 gate.
		Safeguards: config.Safeguards{AllowOverwrite: true},
	}
	return newTestServer(t, exec, cfg)
}

func newTransferSafeguardsServer(t *testing.T, exec *toolsMockExecutor) *mcp.ClientSession {
	t.Helper()
	cfg := &config.Config{
		SSHSocket: "/tmp/test.sock",
		SSHUser:   "user",
		SSHHost:   "host",
		Capabilities: config.Capabilities{
			FileWrite: true,
		},
		Safeguards: config.Safeguards{AllowOverwrite: false},
	}
	return newTestServer(t, exec, cfg)
}

// TestUploadAbsolutePath verifies that an absolute local_path calls UploadFile
// and returns uploaded:true.
func TestUploadAbsolutePath(t *testing.T) {
	mock := &toolsMockExecutor{}
	cs := newFileWriteFullTestServer(t, mock, true)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_upload_file",
		Arguments: map[string]any{
			"local_path":  "/home/user/file.txt",
			"remote_path": "/remote/dest.txt",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "upload with absolute path must not set isError")

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var out uploadOutput
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.True(t, out.Uploaded)
	require.Equal(t, "/remote/dest.txt", out.RemotePath)
	require.True(t, mock.uploadCalled, "UploadFile must have been called")
}

// TestUploadRelativePathRejected verifies that a relative local_path is rejected
// with IsError:true and the executor is NOT called (D-02, T-02-13).
func TestUploadRelativePathRejected(t *testing.T) {
	mock := &toolsMockExecutor{}
	cs := newFileWriteFullTestServer(t, mock, true)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_upload_file",
		Arguments: map[string]any{
			"local_path":  "relative/file.txt",
			"remote_path": "/remote/dest.txt",
		},
	})
	require.NoError(t, err)
	require.True(t, result.IsError, "relative local_path must set isError")
	require.False(t, mock.uploadCalled, "UploadFile must NOT be called for relative path")
}

// TestUploadError verifies that an UploadFile executor failure sets IsError:true.
func TestUploadError(t *testing.T) {
	mock := &toolsMockExecutor{uploadErr: errors.New("connection reset")}
	cs := newFileWriteFullTestServer(t, mock, true)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_upload_file",
		Arguments: map[string]any{
			"local_path":  "/home/user/file.txt",
			"remote_path": "/remote/dest.txt",
		},
	})
	require.NoError(t, err)
	require.True(t, result.IsError, "upload error must set isError")
}

// TestDownloadAbsolutePath verifies that an absolute local_path calls DownloadFile
// and returns downloaded:true.
func TestDownloadAbsolutePath(t *testing.T) {
	mock := &toolsMockExecutor{}
	cs := newFileWriteFullTestServer(t, mock, true)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_download_file",
		Arguments: map[string]any{
			"remote_path": "/remote/file.txt",
			"local_path":  "/home/user/dest.txt",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "download with absolute path must not set isError")

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var out downloadOutput
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.True(t, out.Downloaded)
	require.Equal(t, "/home/user/dest.txt", out.LocalPath)
	require.True(t, mock.downloadCalled, "DownloadFile must have been called")
}

// TestDownloadRelativePathRejected verifies that a relative local_path is rejected
// with IsError:true and the executor is NOT called (D-02, T-02-13).
func TestDownloadRelativePathRejected(t *testing.T) {
	mock := &toolsMockExecutor{}
	cs := newFileWriteFullTestServer(t, mock, true)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_download_file",
		Arguments: map[string]any{
			"remote_path": "/remote/file.txt",
			"local_path":  "relative/dest.txt",
		},
	})
	require.NoError(t, err)
	require.True(t, result.IsError, "relative local_path must set isError")
	require.False(t, mock.downloadCalled, "DownloadFile must NOT be called for relative path")
}

// TestDownloadError verifies that a DownloadFile executor failure sets IsError:true.
func TestDownloadError(t *testing.T) {
	mock := &toolsMockExecutor{downloadErr: errors.New("no such file")}
	cs := newFileWriteFullTestServer(t, mock, true)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_download_file",
		Arguments: map[string]any{
			"remote_path": "/remote/file.txt",
			"local_path":  "/home/user/dest.txt",
		},
	})
	require.NoError(t, err)
	require.True(t, result.IsError, "download error must set isError")
}

// TestTransferToolsAbsentWhenCapFileWriteFalse verifies all three write tools
// are absent when capabilities.file_write is false (SECU-01).
func TestTransferToolsAbsentWhenCapFileWriteFalse(t *testing.T) {
	cs := newFileWriteFullTestServer(t, &toolsMockExecutor{}, false)

	toolsList, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)

	writeTools := map[string]bool{"ssh_write_file": true, "ssh_upload_file": true, "ssh_download_file": true}
	for _, tool := range toolsList.Tools {
		require.False(t, writeTools[tool.Name], "%s must not appear when file_write is false", tool.Name)
	}
}

// TestTransferToolsPresentWithDestructiveHintWhenCapFileWriteTrue verifies that
// ssh_upload_file and ssh_download_file appear with destructiveHint:true.
func TestTransferToolsPresentWithDestructiveHintWhenCapFileWriteTrue(t *testing.T) {
	cs := newFileWriteFullTestServer(t, &toolsMockExecutor{}, true)

	toolsList, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)

	want := map[string]bool{"ssh_upload_file": false, "ssh_download_file": false}
	for _, tool := range toolsList.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
			require.NotNil(t, tool.Annotations)
			require.NotNil(t, tool.Annotations.DestructiveHint)
			require.True(t, *tool.Annotations.DestructiveHint, "%s must have destructiveHint:true", tool.Name)
		}
	}
	for name, found := range want {
		require.True(t, found, "%s must appear when file_write capability is true", name)
	}
}

// TestFullSevenToolSurface verifies that exactly the 7 expected tools are
// registered when exec, file_read, and file_write capabilities are all enabled.
func TestFullSevenToolSurface(t *testing.T) {
	cfg := &config.Config{
		SSHSocket: "/tmp/test.sock",
		SSHUser:   "user",
		SSHHost:   "host",
		Capabilities: config.Capabilities{
			Exec:      true,
			FileRead:  true,
			FileWrite: true,
		},
	}
	cs := newTestServer(t, &toolsMockExecutor{}, cfg)

	toolsList, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)

	expected := map[string]bool{
		"ssh_connection_status": false,
		"ssh_exec":              false,
		"ssh_read_file":         false,
		"ssh_list_dir":          false,
		"ssh_write_file":        false,
		"ssh_upload_file":       false,
		"ssh_download_file":     false,
	}
	for _, tool := range toolsList.Tools {
		expected[tool.Name] = true
	}
	for name, found := range expected {
		require.True(t, found, "%s must be present in the 7-tool surface", name)
	}
	require.Len(t, toolsList.Tools, 7, "exactly 7 tools must be registered when all capabilities enabled")
}

// TestUploadSafe01BlocksWhenRemoteExists verifies that ssh_upload_file returns
// IsError=true when allow_overwrite is false and the remote target exists
// (RunCommand test -e exits 0). UploadFile must NOT be called.
func TestUploadSafe01BlocksWhenRemoteExists(t *testing.T) {
	mock := &toolsMockExecutor{runResult: ssh.RunResult{ExitCode: 0}}
	cs := newTransferSafeguardsServer(t, mock)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_upload_file",
		Arguments: map[string]any{
			"local_path":  "/home/user/file.txt",
			"remote_path": "/remote/dest.txt",
		},
	})
	require.NoError(t, err)
	require.True(t, result.IsError, "upload must be blocked when remote target exists (SAFE-01)")
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, "/remote/dest.txt", "error must name the remote path")
	require.Contains(t, text.Text, "safeguards.allow_overwrite", "error must name the config key")
	require.False(t, mock.uploadCalled, "UploadFile must NOT be called when gate blocks")
}

// TestUploadSafe01AllowsWhenRemoteAbsent verifies that upload proceeds when
// allow_overwrite is false and the remote target is absent (test -e exits 1).
func TestUploadSafe01AllowsWhenRemoteAbsent(t *testing.T) {
	mock := &toolsMockExecutor{runResult: ssh.RunResult{ExitCode: 1}}
	cs := newTransferSafeguardsServer(t, mock)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_upload_file",
		Arguments: map[string]any{
			"local_path":  "/home/user/file.txt",
			"remote_path": "/remote/new.txt",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "upload must proceed when remote target is absent")
	require.True(t, mock.uploadCalled, "UploadFile must be called when file is absent")
}

// TestUploadSafe01AllowOverwriteTrue verifies that upload skips the existence
// check and proceeds when allow_overwrite is true, even when test -e would return 0.
func TestUploadSafe01AllowOverwriteTrue(t *testing.T) {
	mock := &toolsMockExecutor{runResult: ssh.RunResult{ExitCode: 0}}
	cfg := &config.Config{
		SSHSocket:    "/tmp/test.sock",
		SSHUser:      "user",
		SSHHost:      "host",
		Capabilities: config.Capabilities{FileWrite: true},
		Safeguards:   config.Safeguards{AllowOverwrite: true},
	}
	cs := newTestServer(t, mock, cfg)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_upload_file",
		Arguments: map[string]any{
			"local_path":  "/home/user/file.txt",
			"remote_path": "/remote/dest.txt",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "upload must proceed when allow_overwrite is true")
	require.True(t, mock.uploadCalled, "UploadFile must be called when allow_overwrite is true")
}

// TestDownloadSafe01BlocksWhenLocalExists verifies that ssh_download_file returns
// IsError=true when allow_overwrite is false and the local destination already
// exists. DownloadFile must NOT be called.
func TestDownloadSafe01BlocksWhenLocalExists(t *testing.T) {
	// Use a path that reliably exists on any machine.
	existingPath := t.TempDir() + "/existing.txt"
	require.NoError(t, os.WriteFile(existingPath, []byte("x"), 0o600))

	mock := &toolsMockExecutor{}
	cs := newTransferSafeguardsServer(t, mock)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_download_file",
		Arguments: map[string]any{
			"remote_path": "/remote/file.txt",
			"local_path":  existingPath,
		},
	})
	require.NoError(t, err)
	require.True(t, result.IsError, "download must be blocked when local target exists (SAFE-01)")
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, existingPath, "error must name the local path")
	require.Contains(t, text.Text, "safeguards.allow_overwrite", "error must name the config key")
	require.False(t, mock.downloadCalled, "DownloadFile must NOT be called when gate blocks")
}

// TestDownloadSafe01AllowsWhenLocalAbsent verifies that download proceeds when
// allow_overwrite is false and the local destination does not exist.
func TestDownloadSafe01AllowsWhenLocalAbsent(t *testing.T) {
	absentPath := t.TempDir() + "/nonexistent.txt"

	mock := &toolsMockExecutor{}
	cs := newTransferSafeguardsServer(t, mock)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_download_file",
		Arguments: map[string]any{
			"remote_path": "/remote/file.txt",
			"local_path":  absentPath,
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "download must proceed when local target is absent")
	require.True(t, mock.downloadCalled, "DownloadFile must be called when file is absent")
}
