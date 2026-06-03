package tools

import (
	"context"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
	"github.com/NickName-AM/claude-ssh-daemon/internal/ssh"
)

// UploadInput holds the parameters for the ssh_upload_file tool.
// LocalPath must be an absolute path (D-02).
type UploadInput struct {
	LocalPath  string `json:"local_path"  jsonschema:"absolute local file path to upload"`
	RemotePath string `json:"remote_path" jsonschema:"absolute remote destination path"`
}

// UploadOutput is the structured response for ssh_upload_file.
type UploadOutput struct {
	Uploaded   bool   `json:"uploaded"`
	RemotePath string `json:"remote_path"`
}

// DownloadInput holds the parameters for the ssh_download_file tool.
// LocalPath must be an absolute path (D-02).
type DownloadInput struct {
	RemotePath string `json:"remote_path" jsonschema:"absolute remote file path to download"`
	LocalPath  string `json:"local_path"  jsonschema:"absolute local destination path"`
}

// DownloadOutput is the structured response for ssh_download_file.
type DownloadOutput struct {
	Downloaded bool   `json:"downloaded"`
	LocalPath  string `json:"local_path"`
}

// uploadHandler returns a ToolHandlerFor closure for the ssh_upload_file tool.
// Rejects relative local paths before touching the executor (D-02, T-02-13).
func uploadHandler(e ssh.SSHExecutor, cfg *config.Config) mcp.ToolHandlerFor[UploadInput, UploadOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in UploadInput) (*mcp.CallToolResult, UploadOutput, error) {
		if !filepath.IsAbs(in.LocalPath) {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "local path must be absolute"}},
			}, UploadOutput{}, nil
		}

		if err := e.UploadFile(ctx, in.LocalPath, in.RemotePath); err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, UploadOutput{}, nil
		}
		return nil, UploadOutput{Uploaded: true, RemotePath: in.RemotePath}, nil
	}
}

// downloadHandler returns a ToolHandlerFor closure for the ssh_download_file tool.
// Rejects relative local paths before touching the executor (D-02, T-02-13).
func downloadHandler(e ssh.SSHExecutor, cfg *config.Config) mcp.ToolHandlerFor[DownloadInput, DownloadOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in DownloadInput) (*mcp.CallToolResult, DownloadOutput, error) {
		if !filepath.IsAbs(in.LocalPath) {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "local path must be absolute"}},
			}, DownloadOutput{}, nil
		}

		if err := e.DownloadFile(ctx, in.RemotePath, in.LocalPath); err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, DownloadOutput{}, nil
		}
		return nil, DownloadOutput{Downloaded: true, LocalPath: in.LocalPath}, nil
	}
}
