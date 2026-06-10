package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
	"github.com/NickName-AM/claude-ssh-daemon/internal/ssh"
)

// UploadInput holds the parameters for the ssh_upload_file tool.
// LocalPath must be an absolute path (D-02).
type UploadInput struct {
	LocalPath  string `json:"local_path"   jsonschema:"absolute local file path to upload"`
	RemotePath string `json:"remote_path"  jsonschema:"absolute remote destination path"`
	Host       string `json:"host,omitempty" jsonschema:"named SSH host; omit to use default_host"`
}

// UploadOutput is the structured response for ssh_upload_file.
type UploadOutput struct {
	Uploaded   bool   `json:"uploaded"`
	RemotePath string `json:"remote_path"`
}

// DownloadInput holds the parameters for the ssh_download_file tool.
// LocalPath must be an absolute path (D-02).
type DownloadInput struct {
	RemotePath string `json:"remote_path"  jsonschema:"absolute remote file path to download"`
	LocalPath  string `json:"local_path"   jsonschema:"absolute local destination path"`
	Host       string `json:"host,omitempty" jsonschema:"named SSH host; omit to use default_host"`
}

// DownloadOutput is the structured response for ssh_download_file.
type DownloadOutput struct {
	Downloaded bool   `json:"downloaded"`
	LocalPath  string `json:"local_path"`
}

// uploadHandler returns a ToolHandlerFor closure for the ssh_upload_file tool.
// Rejects relative local paths before touching the executor (D-02, T-02-13).
func uploadHandler(registry map[string]ssh.SSHExecutor, cfg *config.Config) mcp.ToolHandlerFor[UploadInput, UploadOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in UploadInput) (*mcp.CallToolResult, UploadOutput, error) {
		// MHST-05/06/07: resolve executor before any SSH I/O.
		exec, hostName, errResult := resolveExecutor(registry, cfg, in.Host)
		if errResult != nil {
			return errResult, UploadOutput{}, nil
		}

		if !filepath.IsAbs(in.LocalPath) {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "local path must be absolute"}},
			}, UploadOutput{}, nil
		}

		// BDIR-01/T-10-07: base_dir sandbox guard on the remote destination path.
		// Guard fires before SAFE-01 (allow_overwrite) to fail fast without any
		// remote SSH I/O (D-07). withinBaseDir is purely lexical (BDIR-03).
		if baseDir := cfg.Hosts[hostName].BaseDir; baseDir != "" {
			if !withinBaseDir(baseDir, in.RemotePath) {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(
						"[host %s] path %q is outside base_dir %q",
						hostName, in.RemotePath, baseDir,
					)}},
				}, UploadOutput{}, nil
			}
		}

		// SAFE-01: check whether the remote destination already exists before
		// uploading. Same gate as writeFileHandler; POSIX single-quote escaped
		// to prevent shell injection (T-06-11).
		if !cfg.Safeguards.AllowOverwrite {
			escapedPath := strings.ReplaceAll(in.RemotePath, "'", "'\\''")
			checkResult, checkErr := exec.RunCommand(ctx, ssh.RunRequest{Command: "test -e '" + escapedPath + "'"})
			if checkErr != nil {
				// MHST-08: prefix SSH error with resolved host name.
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("[host %s] overwrite check failed: %s", hostName, checkErr.Error())}},
				}, UploadOutput{}, nil
			}
			if checkResult.ExitCode == 0 {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(
						"remote file %q already exists; set safeguards.allow_overwrite: true to enable overwrite",
						in.RemotePath,
					)}},
				}, UploadOutput{}, nil
			}
		}

		if err := exec.UploadFile(ctx, in.LocalPath, in.RemotePath); err != nil {
			// MHST-08: prefix error with resolved host name.
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("[host %s] %s", hostName, err.Error())}},
			}, UploadOutput{}, nil
		}
		return nil, UploadOutput{Uploaded: true, RemotePath: in.RemotePath}, nil
	}
}

// downloadHandler returns a ToolHandlerFor closure for the ssh_download_file tool.
// Rejects relative local paths before touching the executor (D-02, T-02-13).
func downloadHandler(registry map[string]ssh.SSHExecutor, cfg *config.Config) mcp.ToolHandlerFor[DownloadInput, DownloadOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in DownloadInput) (*mcp.CallToolResult, DownloadOutput, error) {
		// MHST-05/06/07: resolve executor before any SSH I/O.
		exec, hostName, errResult := resolveExecutor(registry, cfg, in.Host)
		if errResult != nil {
			return errResult, DownloadOutput{}, nil
		}

		if !filepath.IsAbs(in.LocalPath) {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "local path must be absolute"}},
			}, DownloadOutput{}, nil
		}

		// BDIR-01/T-10-07: base_dir sandbox guard on the remote source path.
		// Guard fires before SAFE-01 (allow_overwrite) to fail fast (D-07).
		// withinBaseDir is purely lexical (BDIR-03).
		if baseDir := cfg.Hosts[hostName].BaseDir; baseDir != "" {
			if !withinBaseDir(baseDir, in.RemotePath) {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(
						"[host %s] path %q is outside base_dir %q",
						hostName, in.RemotePath, baseDir,
					)}},
				}, DownloadOutput{}, nil
			}
		}

		// SAFE-01: check whether the local destination already exists before
		// downloading; os.WriteFile would otherwise silently overwrite it.
		if !cfg.Safeguards.AllowOverwrite {
			if _, statErr := os.Stat(in.LocalPath); statErr == nil {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(
						"local file %q already exists; set safeguards.allow_overwrite: true to enable overwrite",
						in.LocalPath,
					)}},
				}, DownloadOutput{}, nil
			}
		}

		if err := exec.DownloadFile(ctx, in.RemotePath, in.LocalPath); err != nil {
			// MHST-08: prefix error with resolved host name.
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("[host %s] %s", hostName, err.Error())}},
			}, DownloadOutput{}, nil
		}
		return nil, DownloadOutput{Downloaded: true, LocalPath: in.LocalPath}, nil
	}
}
