package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
)

type listDirOutput struct {
	Entries []struct {
		Name        string `json:"name"`
		Type        string `json:"type"`
		Size        int64  `json:"size"`
		Permissions string `json:"permissions"`
		Mtime       string `json:"mtime"`
	} `json:"entries"`
	InjectionWarning string `json:"_injection_warning"`
}

func newListDirTestServer(t *testing.T, exec *toolsMockExecutor, fileRead bool) *mcp.ClientSession {
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

func newListDirTestServerWithSafeguards(t *testing.T, exec *toolsMockExecutor, s config.Safeguards) *mcp.ClientSession {
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

// lsFixture is a representative ls -la output used across multiple tests.
const lsFixture = `total 16
drwxr-xr-x  2 user group 4096 Jan  2 03:04 .
drwxr-xr-x 10 user group 4096 Jan  2 03:04 ..
-rw-r--r--  1 user group 1024 Jan  2 03:04 file.txt
drwxr-xr-x  2 user group 4096 Jan  2 03:04 .config
lrwxrwxrwx  1 user group    7 Jan  2 03:04 link -> target
-rw-r--r--  1 user group  512 Jan  2 03:04 some file with spaces.txt
`

// TestParseLsLineFile verifies that a regular file line is parsed correctly.
func TestParseLsLineFile(t *testing.T) {
	entry, ok := parseLsLine("-rw-r--r-- 1 user group 1024 Jan 2 03:04 file.txt")
	require.True(t, ok)
	require.Equal(t, "file.txt", entry.Name)
	require.Equal(t, "file", entry.Type)
	require.Equal(t, int64(1024), entry.Size)
	require.Equal(t, "-rw-r--r--", entry.Permissions)
	require.NotEmpty(t, entry.Mtime)
}

// TestParseLsLineDir verifies that a directory line produces type "dir".
func TestParseLsLineDir(t *testing.T) {
	entry, ok := parseLsLine("drwxr-xr-x 2 user group 4096 Jan 2 03:04 .config")
	require.True(t, ok)
	require.Equal(t, "dir", entry.Type)
	require.Equal(t, ".config", entry.Name, "dotfile name must be preserved")
}

// TestParseLsLineSymlink verifies that a symlink line produces type "symlink"
// and that the " -> target" suffix is stripped from the name.
func TestParseLsLineSymlink(t *testing.T) {
	entry, ok := parseLsLine("lrwxrwxrwx 1 user group 7 Jan 2 03:04 link -> target")
	require.True(t, ok)
	require.Equal(t, "symlink", entry.Type)
	require.Equal(t, "link", entry.Name, "symlink name must not include -> target")
}

// TestParseLsLineDotfile verifies that a dotfile name preserves the leading dot.
func TestParseLsLineDotfile(t *testing.T) {
	entry, ok := parseLsLine("-rw-r--r-- 1 user group 256 Jan 2 03:04 .bashrc")
	require.True(t, ok)
	require.Equal(t, ".bashrc", entry.Name, "leading dot must be preserved")
}

// TestParseLsLineSpacedName verifies that filenames containing spaces are
// reconstructed correctly by joining fields[8:].
func TestParseLsLineSpacedName(t *testing.T) {
	entry, ok := parseLsLine("-rw-r--r-- 1 user group 512 Jan 2 03:04 some file with spaces.txt")
	require.True(t, ok)
	require.Equal(t, "some file with spaces.txt", entry.Name)
}

// TestParseLsLineTotalSkipped verifies that the "total N" header returns false.
func TestParseLsLineTotalSkipped(t *testing.T) {
	_, ok := parseLsLine("total 16")
	require.False(t, ok)
}

// TestParseLsLineTooFewFields verifies that malformed lines return false (T-02-10).
func TestParseLsLineTooFewFields(t *testing.T) {
	_, ok := parseLsLine("-rw-r--r-- 1 user group 512")
	require.False(t, ok)
}

// TestListDirHandlerSuccess verifies that listDirHandler returns parsed entries
// from a multi-line ls -la fixture.
func TestListDirHandlerSuccess(t *testing.T) {
	mock := &toolsMockExecutor{listResult: lsFixture}
	cs := newListDirTestServer(t, mock, true)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_list_dir",
		Arguments: map[string]any{"path": "/remote/dir"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "successful list must not set isError")

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var out listDirOutput
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.NotEmpty(t, out.Entries, "entries must not be empty")

	// Verify that file.txt is present with correct fields
	var fileEntry *struct {
		Name        string `json:"name"`
		Type        string `json:"type"`
		Size        int64  `json:"size"`
		Permissions string `json:"permissions"`
		Mtime       string `json:"mtime"`
	}
	for i := range out.Entries {
		if out.Entries[i].Name == "file.txt" {
			fileEntry = &out.Entries[i]
			break
		}
	}
	require.NotNil(t, fileEntry, "file.txt must appear in entries")
	require.Equal(t, "file", fileEntry.Type)
	require.Equal(t, int64(1024), fileEntry.Size)
	require.NotEmpty(t, fileEntry.Permissions)
	require.NotEmpty(t, fileEntry.Mtime)
}

// TestListDirHandlerError verifies that a ListDir failure sets result.IsError == true.
func TestListDirHandlerError(t *testing.T) {
	mock := &toolsMockExecutor{listErr: errors.New("no such directory")}
	cs := newListDirTestServer(t, mock, true)

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_list_dir",
		Arguments: map[string]any{"path": "/remote/missing"},
	})
	require.NoError(t, err)
	require.True(t, result.IsError, "list error must set isError")
}

// TestListDirAbsentWhenCapFileReadFalse verifies that ssh_list_dir is absent
// from tools/list when capabilities.file_read is false (SECU-01).
func TestListDirAbsentWhenCapFileReadFalse(t *testing.T) {
	cs := newListDirTestServer(t, &toolsMockExecutor{}, false)

	toolsList, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)

	for _, tool := range toolsList.Tools {
		require.NotEqual(t, "ssh_list_dir", tool.Name)
	}
}

// TestListDirPresentWithReadOnlyHintWhenCapFileReadTrue verifies that ssh_list_dir
// appears with readOnlyHint:true when capabilities.file_read is true (SECU-02).
func TestListDirPresentWithReadOnlyHintWhenCapFileReadTrue(t *testing.T) {
	cs := newListDirTestServer(t, &toolsMockExecutor{}, true)

	toolsList, err := cs.ListTools(context.Background(), nil)
	require.NoError(t, err)

	var found bool
	for _, tool := range toolsList.Tools {
		if tool.Name == "ssh_list_dir" {
			found = true
			require.NotNil(t, tool.Annotations)
			require.True(t, tool.Annotations.ReadOnlyHint, "ssh_list_dir must have readOnlyHint:true")
			break
		}
	}
	require.True(t, found, "ssh_list_dir must appear when file_read capability is true")
}

// lsInjectionFixture is an ls -la listing that includes a file with an
// injection-pattern name. Used to verify per-entry-name scanning.
const lsInjectionFixture = `total 8
drwxr-xr-x  2 user group 4096 Jan  2 03:04 .
drwxr-xr-x 10 user group 4096 Jan  2 03:04 ..
-rw-r--r--  1 user group  512 Jan  2 03:04 <tool_call>
-rw-r--r--  1 user group  256 Jan  2 03:04 normal.txt
`

// TestListDirEntryNameInjectionWarningSet verifies that when a directory entry
// name contains an injection pattern, _injection_warning contains the category
// ("xml_tool_tags"), IsError is false, and no permission/timestamp text appears
// in the warning (GURD-02, per-entry-name scan, not raw ls output).
func TestListDirEntryNameInjectionWarningSet(t *testing.T) {
	mock := &toolsMockExecutor{listResult: lsInjectionFixture}
	cs := newListDirTestServerWithSafeguards(t, mock, config.Safeguards{GuardDisabled: false})

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_list_dir",
		Arguments: map[string]any{"path": "/remote/dir"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "injection hit must NOT set isError (GURD-01 annotation only)")

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var out listDirOutput
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.NotEmpty(t, out.InjectionWarning, "_injection_warning must be set for injection entry name")
	require.Contains(t, out.InjectionWarning, "xml_tool_tags", "warning must name the matched category")
	// Warning must not contain permission strings, dates, or raw ls fields.
	require.NotContains(t, out.InjectionWarning, "rw-r--r--", "permission strings must not appear in warning")
	require.NotContains(t, out.InjectionWarning, "Jan", "date fields must not appear in warning")
}

// TestListDirCleanListingNoInjectionWarning verifies that a realistic ls -la
// fixture with permission strings, dates, and normal filenames does not produce
// a false positive _injection_warning (per-entry-name scan avoids raw output).
func TestListDirCleanListingNoInjectionWarning(t *testing.T) {
	mock := &toolsMockExecutor{listResult: lsFixture}
	cs := newListDirTestServerWithSafeguards(t, mock, config.Safeguards{GuardDisabled: false})

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_list_dir",
		Arguments: map[string]any{"path": "/remote/dir"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var out listDirOutput
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.Empty(t, out.InjectionWarning, "_injection_warning must be absent for clean filenames (no false positive on permissions/dates)")
}

// TestListDirGuardDisabledNoWarning verifies that GuardDisabled=true suppresses
// _injection_warning even when an entry name contains an injection pattern.
func TestListDirGuardDisabledNoWarning(t *testing.T) {
	mock := &toolsMockExecutor{listResult: lsInjectionFixture}
	cs := newListDirTestServerWithSafeguards(t, mock, config.Safeguards{GuardDisabled: true})

	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "ssh_list_dir",
		Arguments: map[string]any{"path": "/remote/dir"},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)

	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	var out listDirOutput
	require.NoError(t, json.Unmarshal([]byte(text.Text), &out))
	require.Empty(t, out.InjectionWarning, "_injection_warning must be absent when guard is disabled")
}
