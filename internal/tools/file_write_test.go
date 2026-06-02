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
	}
	return newTestServer(t, exec, cfg)
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
