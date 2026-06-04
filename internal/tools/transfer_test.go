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
	registry := singleHostRegistry(exec, cfg)
	return newTestServer(t, registry, cfg)
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
	registry := singleHostRegistry(exec, cfg)
	return newTestServer(t, registry, cfg)
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
	mock := &toolsMockExecutor{}
	cs := newTestServer(t, singleHostRegistry(mock, cfg), cfg)

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
	cs := newTestServer(t, singleHostRegistry(mock, cfg), cfg)

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

// newMultiHostTransferServer builds a test server with a two-host registry
// (web + db) for multi-host routing tests on transfer handlers (MHST-05 through
// MHST-08). AllowOverwrite:true skips the existence-check gate so routing tests
// focus purely on host resolution and not safeguard behaviour (WR-004).
func newMultiHostTransferServer(t *testing.T, webMock, dbMock *toolsMockExecutor) (*mcp.ClientSession, *config.Config) {
	t.Helper()
	cfg := &config.Config{
		MCPSocket:   "/tmp/mcp.sock",
		DefaultHost: "web",
		Hosts: map[string]config.HostConfig{
			"web": {Socket: "/tmp/ssh-web.sock", User: "ubuntu", Host: "web.example.com"},
			"db":  {Socket: "/tmp/ssh-db.sock", User: "ubuntu", Host: "db.example.com"},
		},
		Capabilities: config.Capabilities{FileWrite: true},
		Safeguards:   config.Safeguards{AllowOverwrite: true},
	}
	registry := map[string]ssh.SSHExecutor{
		"web": webMock,
		"db":  dbMock,
	}
	return newTestServer(t, registry, cfg), cfg
}

// TestSSHUploadKnownHostRouting verifies that ssh_upload_file with "host":"web"
// routes to the web executor and NOT the db executor (MHST-05).
func TestSSHUploadKnownHostRouting(t *testing.T) {
	webMock := &toolsMockExecutor{}
	dbMock := &toolsMockExecutor{}
	cs, _ := newMultiHostTransferServer(t, webMock, dbMock)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_upload_file",
		Arguments: map[string]any{
			"local_path":  "/home/user/file.txt",
			"remote_path": "/remote/dest.txt",
			"host":        "web",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "known host upload must not set isError (MHST-05)")
	require.True(t, webMock.uploadCalled, "web mock must have been called (MHST-05)")
	require.False(t, dbMock.uploadCalled, "db mock must NOT have been called")
}

// TestSSHUploadDefaultRouting verifies that omitting the host parameter on
// ssh_upload_file routes to the default_host executor (MHST-06).
func TestSSHUploadDefaultRouting(t *testing.T) {
	webMock := &toolsMockExecutor{}
	dbMock := &toolsMockExecutor{}
	cs, _ := newMultiHostTransferServer(t, webMock, dbMock)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_upload_file",
		Arguments: map[string]any{
			"local_path":  "/home/user/file.txt",
			"remote_path": "/remote/dest.txt",
			// No "host" field — should route to default_host ("web").
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "default routing upload must not set isError (MHST-06)")
	require.True(t, webMock.uploadCalled, "web (default) mock must have been called (MHST-06)")
	require.False(t, dbMock.uploadCalled, "db mock must NOT have been called for default routing")
}

// TestSSHUploadUnknownHostReturnsIsError verifies that ssh_upload_file with an
// unknown host returns IsError=true with the host name and the sorted list of
// configured hosts (MHST-07).
func TestSSHUploadUnknownHostReturnsIsError(t *testing.T) {
	webMock := &toolsMockExecutor{}
	dbMock := &toolsMockExecutor{}
	cs, _ := newMultiHostTransferServer(t, webMock, dbMock)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_upload_file",
		Arguments: map[string]any{
			"local_path":  "/home/user/file.txt",
			"remote_path": "/remote/dest.txt",
			"host":        "nope",
		},
	})
	require.NoError(t, err)
	require.True(t, result.IsError, "unknown host must set isError (MHST-07)")

	require.NotEmpty(t, result.Content)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, `unknown host "nope"`, "error must name the requested host")
	require.Contains(t, text.Text, "db", "error must list configured hosts")
	require.Contains(t, text.Text, "web", "error must list configured hosts")
	require.False(t, webMock.uploadCalled, "executor must not be called for unknown host")
	require.False(t, dbMock.uploadCalled, "executor must not be called for unknown host")
}

// TestSSHUploadHostPrefixedOnExecutorError verifies that when the resolved
// executor returns an error for ssh_upload_file, the error is prefixed with
// "[host <name>]" (MHST-08).
func TestSSHUploadHostPrefixedOnExecutorError(t *testing.T) {
	webMock := &toolsMockExecutor{
		uploadErr: errors.New("socket dead: no such file or directory"),
	}
	dbMock := &toolsMockExecutor{}
	cs, _ := newMultiHostTransferServer(t, webMock, dbMock)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_upload_file",
		Arguments: map[string]any{
			"local_path":  "/home/user/file.txt",
			"remote_path": "/remote/dest.txt",
			"host":        "web",
		},
	})
	require.NoError(t, err)
	require.True(t, result.IsError, "executor error must set isError")

	require.NotEmpty(t, result.Content)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, "[host web]", "error must be prefixed with [host web] (MHST-08)")
	require.Contains(t, text.Text, "socket dead", "error must contain the original error message")
}

// TestSSHDownloadKnownHostRouting verifies that ssh_download_file with "host":"db"
// routes to the db executor and NOT the web executor (MHST-05).
func TestSSHDownloadKnownHostRouting(t *testing.T) {
	webMock := &toolsMockExecutor{}
	dbMock := &toolsMockExecutor{}
	cs, _ := newMultiHostTransferServer(t, webMock, dbMock)

	absentPath := t.TempDir() + "/downloaded.txt"
	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_download_file",
		Arguments: map[string]any{
			"remote_path": "/remote/file.txt",
			"local_path":  absentPath,
			"host":        "db",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "known host download must not set isError (MHST-05)")
	require.True(t, dbMock.downloadCalled, "db mock must have been called (MHST-05)")
	require.False(t, webMock.downloadCalled, "web mock must NOT have been called")
}

// TestSSHDownloadDefaultRouting verifies that omitting the host parameter on
// ssh_download_file routes to the default_host executor (MHST-06).
func TestSSHDownloadDefaultRouting(t *testing.T) {
	webMock := &toolsMockExecutor{}
	dbMock := &toolsMockExecutor{}
	cs, _ := newMultiHostTransferServer(t, webMock, dbMock)

	absentPath := t.TempDir() + "/downloaded.txt"
	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_download_file",
		Arguments: map[string]any{
			"remote_path": "/remote/file.txt",
			"local_path":  absentPath,
			// No "host" field — should route to default_host ("web").
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "default routing download must not set isError (MHST-06)")
	require.True(t, webMock.downloadCalled, "web (default) mock must have been called (MHST-06)")
	require.False(t, dbMock.downloadCalled, "db mock must NOT have been called for default routing")
}

// TestSSHDownloadUnknownHostReturnsIsError verifies that ssh_download_file with
// an unknown host returns IsError=true with the host name and the sorted list of
// configured hosts (MHST-07).
func TestSSHDownloadUnknownHostReturnsIsError(t *testing.T) {
	webMock := &toolsMockExecutor{}
	dbMock := &toolsMockExecutor{}
	cs, _ := newMultiHostTransferServer(t, webMock, dbMock)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_download_file",
		Arguments: map[string]any{
			"remote_path": "/remote/file.txt",
			"local_path":  "/tmp/dest.txt",
			"host":        "nope",
		},
	})
	require.NoError(t, err)
	require.True(t, result.IsError, "unknown host must set isError (MHST-07)")

	require.NotEmpty(t, result.Content)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, `unknown host "nope"`, "error must name the requested host")
	require.Contains(t, text.Text, "db", "error must list configured hosts")
	require.Contains(t, text.Text, "web", "error must list configured hosts")
	require.False(t, webMock.downloadCalled, "executor must not be called for unknown host")
	require.False(t, dbMock.downloadCalled, "executor must not be called for unknown host")
}

// TestSSHDownloadHostPrefixedOnExecutorError verifies that when the resolved
// executor returns an error for ssh_download_file, the error is prefixed with
// "[host <name>]" (MHST-08).
func TestSSHDownloadHostPrefixedOnExecutorError(t *testing.T) {
	webMock := &toolsMockExecutor{
		downloadErr: errors.New("socket dead: no such file or directory"),
	}
	dbMock := &toolsMockExecutor{}
	cs, _ := newMultiHostTransferServer(t, webMock, dbMock)

	absentPath := t.TempDir() + "/dest.txt"
	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_download_file",
		Arguments: map[string]any{
			"remote_path": "/remote/file.txt",
			"local_path":  absentPath,
			"host":        "web",
		},
	})
	require.NoError(t, err)
	require.True(t, result.IsError, "executor error must set isError")

	require.NotEmpty(t, result.Content)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, "[host web]", "error must be prefixed with [host web] (MHST-08)")
	require.Contains(t, text.Text, "socket dead", "error must contain the original error message")
}
