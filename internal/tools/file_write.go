package tools

import (
	"context"
	"encoding/base64"

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
