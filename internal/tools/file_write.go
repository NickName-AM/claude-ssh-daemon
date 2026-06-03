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
}

// WriteFileOutput is the structured response for ssh_write_file.
type WriteFileOutput struct {
	Written bool   `json:"written"`
	Path    string `json:"path"`
}

// writeFileHandler returns a ToolHandlerFor closure for the ssh_write_file tool.
// It decodes base64 content when requested (D-08), then pipes bytes to the executor.
func writeFileHandler(e ssh.SSHExecutor, cfg *config.Config) mcp.ToolHandlerFor[WriteFileInput, WriteFileOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in WriteFileInput) (*mcp.CallToolResult, WriteFileOutput, error) {
		// SAFE-01: when allow_overwrite is false, check whether the remote target
		// already exists before performing any decode or write. The path is
		// POSIX single-quote escaped inline to prevent shell injection (T-06-11).
		if !cfg.Safeguards.AllowOverwrite {
			escapedPath := strings.ReplaceAll(in.Path, "'", "'\\''")
			checkResult, checkErr := e.RunCommand(ctx, ssh.RunRequest{Command: "test -e '" + escapedPath + "'"})
			if checkErr != nil {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: "overwrite check failed: " + checkErr.Error()}},
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

		if err := e.WriteFile(ctx, in.Path, data); err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, WriteFileOutput{}, nil
		}
		return nil, WriteFileOutput{Written: true, Path: in.Path}, nil
	}
}
