package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
	"github.com/NickName-AM/claude-ssh-daemon/internal/ssh"
)

// WriteFileInput holds the parameters for the ssh_write_file tool.
// Encoding is optional; when "base64", Content is decoded before writing (D-08).
type WriteFileInput struct {
	Path     string `json:"path"               jsonschema:"absolute remote file path"`
	Content  string `json:"content"            jsonschema:"file content; base64-encoded when encoding is base64"`
	Encoding string `json:"encoding,omitempty" jsonschema:"utf-8 (default) or base64"`
	Host     string `json:"host,omitempty"     jsonschema:"named SSH host; omit to use default_host"`
}

// WriteFileOutput is the structured response for ssh_write_file.
type WriteFileOutput struct {
	Written bool   `json:"written"`
	Path    string `json:"path"`
}

// writeFileHandler returns a ToolHandlerFor closure for the ssh_write_file tool.
// It decodes base64 content when requested (D-08), then pipes bytes to the executor.
func writeFileHandler(registry map[string]ssh.SSHExecutor, cfg *config.Config) mcp.ToolHandlerFor[WriteFileInput, WriteFileOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in WriteFileInput) (*mcp.CallToolResult, WriteFileOutput, error) {
		// MHST-05/06/07: resolve the executor for the requested host.
		// Resolve before SAFE-01 because the overwrite check uses the executor
		// (exec.RunCommand for test -e). Unknown host must fail before any SSH I/O.
		exec, hostName, errResult := resolveExecutor(registry, cfg, in.Host)
		if errResult != nil {
			return errResult, WriteFileOutput{}, nil
		}

		// SAFE-01: when allow_overwrite is false, check whether the remote target
		// already exists before performing any decode or write. The path is
		// POSIX single-quote escaped inline to prevent shell injection (T-06-11).
		// Note: two separate SSH round-trips (test -e then write) — a file created
		// between the check and the write will be silently overwritten (TOCTOU).
		// Acceptable for a single-user personal daemon; set allow_overwrite: true
		// explicitly if the target environment is shared.
		if !cfg.Safeguards.AllowOverwrite {
			escapedPath := strings.ReplaceAll(in.Path, "'", "'\\''")
			checkResult, checkErr := exec.RunCommand(ctx, ssh.RunRequest{Command: "test -e '" + escapedPath + "'"})
			if checkErr != nil {
				// MHST-08: prefix SSH error with resolved host name; policy error text unchanged.
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("[host %s] overwrite check failed: %s", hostName, checkErr.Error())}},
				}, WriteFileOutput{}, nil
			}
			if checkResult.ExitCode == 0 {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("file %q already exists; set safeguards.allow_overwrite: true to enable overwrite", in.Path)}},
				}, WriteFileOutput{}, nil
			}
		}

		var data []byte
		if in.Encoding == "base64" {
			decoded, err := base64.StdEncoding.DecodeString(in.Content)
			if err != nil {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: "invalid base64 content: " + err.Error()}},
				}, WriteFileOutput{}, nil
			}
			data = decoded
		} else {
			data = []byte(in.Content)
		}

		if err := exec.WriteFile(ctx, in.Path, data); err != nil {
			// MHST-08: prefix error with resolved host name.
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("[host %s] %s", hostName, err.Error())}},
			}, WriteFileOutput{}, nil
		}
		return nil, WriteFileOutput{Written: true, Path: in.Path}, nil
	}
}
