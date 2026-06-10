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

type writeFileOutput struct {
	Written bool   `json:"written"`
	Path    string `json:"path"`
}

func newFileWriteTestServer(t *testing.T, exec *toolsMockExecutor, fileWrite bool) *mcp.ClientSession {
	t.Helper()
	cfg := &config.Config{
		SSHSocket: "/tmp/test.sock",
		SSHUser:   "user",
		SSHHost:   "host",
		Capabilities: config.Capabilities{
			FileWrite: fileWrite,
		},
		// AllowOverwrite=true so these helpers test the write path itself,
		// not the SAFE-01 existence gate. Gate behavior is tested separately.
		Safeguards: config.Safeguards{
			AllowOverwrite: true,
		},
	}
	registry := singleHostRegistry(exec, cfg)
	return newTestServer(t, registry, cfg)
}

// TestWriteFileUTF8 verifies that utf-8 content is passed to WriteFile as-is.
func TestWriteFileUTF8(t *testing.T) {
	mock := &toolsMockExecutor{}
	cs := newFileWriteTestServer(t, mock, true)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_write_file",
		Arguments: map[string]any{
			"path":    "/remote/file.txt",
			"content": "hello world",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "utf-8 write must not set isError")

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var out writeFileOutput
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.True(t, out.Written)
	require.Equal(t, "/remote/file.txt", out.Path)
	require.Equal(t, []byte("hello world"), mock.writtenContent, "bytes passed to WriteFile must match the input string")
}

// TestWriteFileBase64 verifies that base64 content is decoded before being
// passed to WriteFile (D-08, symmetric with read).
func TestWriteFileBase64(t *testing.T) {
	originalBytes := []byte{0x00, 0xFF, 0x42, 0xAB}
	encoded := base64.StdEncoding.EncodeToString(originalBytes)

	mock := &toolsMockExecutor{}
	cs := newFileWriteTestServer(t, mock, true)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_write_file",
		Arguments: map[string]any{
			"path":     "/remote/image.bin",
			"content":  encoded,
			"encoding": "base64",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "base64 write must not set isError")

	require.Equal(t, originalBytes, mock.writtenContent, "decoded bytes must match original")
}

// TestWriteFileInvalidBase64 verifies that malformed base64 sets result.IsError == true
// without calling the executor (T-02-14).
func TestWriteFileInvalidBase64(t *testing.T) {
	mock := &toolsMockExecutor{}
	cs := newFileWriteTestServer(t, mock, true)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_write_file",
		Arguments: map[string]any{
			"path":     "/remote/file.bin",
			"content":  "not!valid!base64!!!",
			"encoding": "base64",
		},
	})
	require.NoError(t, err)
	require.True(t, result.IsError, "invalid base64 must set isError")
	require.Nil(t, mock.writtenContent, "WriteFile must not be called on invalid base64")
}

// TestWriteFileWriteError verifies that a WriteFile executor failure sets
// result.IsError == true.
func TestWriteFileWriteError(t *testing.T) {
	mock := &toolsMockExecutor{writeErr: errors.New("disk full")}
	cs := newFileWriteTestServer(t, mock, true)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_write_file",
		Arguments: map[string]any{
			"path":    "/remote/file.txt",
			"content": "data",
		},
	})
	require.NoError(t, err)
	require.True(t, result.IsError, "write error must set isError")
}

// TestWriteFileOverwriteBlockedWhenFileExists verifies SAFE-01: when allow_overwrite
// is false and the remote file already exists (RunCommand ExitCode==0), the handler
// returns isError:true naming the file path and the safeguards.allow_overwrite key,
// and WriteFile is NOT called.
func TestWriteFileOverwriteBlockedWhenFileExists(t *testing.T) {
	mock := &toolsMockExecutor{
		runResult: ssh.RunResult{ExitCode: 0}, // test -e returns 0 → file exists
	}
	cfg := &config.Config{
		SSHSocket: "/tmp/test.sock",
		SSHUser:   "user",
		SSHHost:   "host",
		Capabilities: config.Capabilities{
			FileWrite: true,
		},
		Safeguards: config.Safeguards{
			AllowOverwrite: false,
		},
	}
	cs := newTestServer(t, singleHostRegistry(mock, cfg), cfg)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_write_file",
		Arguments: map[string]any{
			"path":    "/remote/important.txt",
			"content": "new data",
		},
	})
	require.NoError(t, err)
	require.True(t, result.IsError, "existing file with allow_overwrite=false must set isError")

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, "/remote/important.txt", "error must name the file path")
	require.Contains(t, text.Text, "safeguards.allow_overwrite", "error must name the config key")
	require.Nil(t, mock.writtenContent, "WriteFile must not be called when file exists and allow_overwrite is false")
}

// TestWriteFileAllowedWhenFileAbsent verifies SAFE-01: when allow_overwrite is false
// but the remote file does not exist (RunCommand ExitCode==1), the write proceeds.
func TestWriteFileAllowedWhenFileAbsent(t *testing.T) {
	mock := &toolsMockExecutor{
		runResult: ssh.RunResult{ExitCode: 1}, // test -e returns 1 → file absent
	}
	cfg := &config.Config{
		SSHSocket: "/tmp/test.sock",
		SSHUser:   "user",
		SSHHost:   "host",
		Capabilities: config.Capabilities{
			FileWrite: true,
		},
		Safeguards: config.Safeguards{
			AllowOverwrite: false,
		},
	}
	cs := newTestServer(t, singleHostRegistry(mock, cfg), cfg)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_write_file",
		Arguments: map[string]any{
			"path":    "/remote/new.txt",
			"content": "hello",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "absent file must allow write")

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var out writeFileOutput
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.True(t, out.Written, "Written must be true when file is absent")
}

// TestWriteFileAllowOverwriteTrue verifies SAFE-01: when allow_overwrite is true,
// the existence check is skipped entirely and the write proceeds regardless.
func TestWriteFileAllowOverwriteTrue(t *testing.T) {
	// runResult.ExitCode=0 simulates that the file would exist,
	// but with AllowOverwrite=true the check must never be consulted.
	mock := &toolsMockExecutor{
		runResult: ssh.RunResult{ExitCode: 0},
	}
	cfg := &config.Config{
		SSHSocket: "/tmp/test.sock",
		SSHUser:   "user",
		SSHHost:   "host",
		Capabilities: config.Capabilities{
			FileWrite: true,
		},
		Safeguards: config.Safeguards{
			AllowOverwrite: true,
		},
	}
	cs := newTestServer(t, singleHostRegistry(mock, cfg), cfg)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_write_file",
		Arguments: map[string]any{
			"path":    "/remote/file.txt",
			"content": "overwrite content",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "allow_overwrite=true must not set isError")

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var out writeFileOutput
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.True(t, out.Written, "Written must be true when allow_overwrite is true")
}

// TestWriteFileOverwriteCheckError verifies SAFE-01: when the RunCommand existence
// check itself returns an error, the handler returns isError:true with "overwrite check failed".
func TestWriteFileOverwriteCheckError(t *testing.T) {
	mock := &toolsMockExecutor{
		runErr: errors.New("ControlMaster socket dead"),
	}
	cfg := &config.Config{
		SSHSocket: "/tmp/test.sock",
		SSHUser:   "user",
		SSHHost:   "host",
		Capabilities: config.Capabilities{
			FileWrite: true,
		},
		Safeguards: config.Safeguards{
			AllowOverwrite: false,
		},
	}
	cs := newTestServer(t, singleHostRegistry(mock, cfg), cfg)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_write_file",
		Arguments: map[string]any{
			"path":    "/remote/file.txt",
			"content": "data",
		},
	})
	require.NoError(t, err)
	require.True(t, result.IsError, "RunCommand error must set isError")

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, "overwrite check failed", "error must contain 'overwrite check failed'")
	require.Nil(t, mock.writtenContent, "WriteFile must not be called when check errors")
}

// TestWriteFileAbsentWhenCapFileWriteFalse verifies that ssh_write_file is absent
// from tools/list when capabilities.file_write is false (SECU-01).
func TestWriteFileAbsentWhenCapFileWriteFalse(t *testing.T) {
	cs := newFileWriteTestServer(t, &toolsMockExecutor{}, false)

	toolsList, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)

	for _, tool := range toolsList.Tools {
		require.NotEqual(t, "ssh_write_file", tool.Name)
	}
}

// TestWriteFilePresentWithDestructiveHintWhenCapFileWriteTrue verifies that
// ssh_write_file appears with destructiveHint:true when file_write is enabled.
func TestWriteFilePresentWithDestructiveHintWhenCapFileWriteTrue(t *testing.T) {
	cs := newFileWriteTestServer(t, &toolsMockExecutor{}, true)

	toolsList, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)

	var found bool
	for _, tool := range toolsList.Tools {
		if tool.Name == "ssh_write_file" {
			found = true
			require.NotNil(t, tool.Annotations)
			require.NotNil(t, tool.Annotations.DestructiveHint)
			require.True(t, *tool.Annotations.DestructiveHint)
			break
		}
	}
	require.True(t, found, "ssh_write_file must appear when file_write capability is true")
}

// TestWriteFileBaseDirOutsidePathRejected verifies that writeFileHandler returns
// isError:true when the requested path is outside base_dir, and that the rejection
// fires BEFORE the allow_overwrite check (D-07, BDIR-01, T-10-06).
func TestWriteFileBaseDirOutsidePathRejected(t *testing.T) {
	// runResult.ExitCode=0 would normally allow overwrite; we verify the guard
	// fires before RunCommand is called — runCalled must remain false.
	mock := &toolsMockExecutor{runResult: ssh.RunResult{ExitCode: 0}}
	cs := newBaseDirServer(t, mock, "/srv/app")

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_write_file",
		Arguments: map[string]any{
			"path":    "/etc/x",
			"content": "evil",
		},
	})
	require.NoError(t, err)
	require.True(t, result.IsError, "path outside base_dir must set isError (BDIR-01)")

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, "[host default]", "error must include host name prefix")
	require.Contains(t, text.Text, "outside base_dir", "error must describe the violation")
	require.Contains(t, text.Text, "/srv/app", "error must include the configured base_dir value (D-03)")
	require.False(t, mock.runCalled, "allow_overwrite RunCommand must NOT be called when base_dir guard fires (D-07)")
	require.Nil(t, mock.writtenContent, "WriteFile must not be called when base_dir guard fires")
}

// TestWriteFileBaseDirTraversalRejected verifies that a traversal path that
// lexically resolves outside base_dir is rejected before any SSH I/O (BDIR-01, D-07).
func TestWriteFileBaseDirTraversalRejected(t *testing.T) {
	mock := &toolsMockExecutor{runResult: ssh.RunResult{ExitCode: 1}}
	cs := newBaseDirServer(t, mock, "/srv/app")

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_write_file",
		Arguments: map[string]any{
			"path":    "/srv/app/../etc/x",
			"content": "evil",
		},
	})
	require.NoError(t, err)
	require.True(t, result.IsError, "traversal path outside base_dir must set isError")
	require.False(t, mock.runCalled, "RunCommand must not be called when traversal guard fires")
}

// TestWriteFileBaseDirInsidePathPassThrough verifies that a path inside base_dir
// proceeds to the write operation (BDIR-01 positive case).
func TestWriteFileBaseDirInsidePathPassThrough(t *testing.T) {
	// ExitCode=1 → file absent, so write proceeds even with allow_overwrite=false.
	// newBaseDirServer sets AllowOverwrite=true so no RunCommand is needed.
	mock := &toolsMockExecutor{}
	cs := newBaseDirServer(t, mock, "/srv/app")

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_write_file",
		Arguments: map[string]any{
			"path":    "/srv/app/new.txt",
			"content": "hello",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "in-sandbox path must not be rejected")
	require.NotNil(t, mock.writtenContent, "WriteFile must be called for in-sandbox path")
}

// TestWriteFileBaseDirEmptyNoCheck verifies that when base_dir is empty,
// writeFileHandler proceeds without any sandbox check (unchanged behavior).
func TestWriteFileBaseDirEmptyNoCheck(t *testing.T) {
	mock := &toolsMockExecutor{}
	cs := newBaseDirServer(t, mock, "")

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "ssh_write_file",
		Arguments: map[string]any{
			"path":    "/etc/anywhere",
			"content": "data",
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "empty base_dir must not block any path")
}
