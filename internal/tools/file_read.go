package tools

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
	"github.com/NickName-AM/claude-ssh-daemon/internal/guard"
	"github.com/NickName-AM/claude-ssh-daemon/internal/ssh"
)

// ReadFileInput holds the parameters for the ssh_read_file tool.
type ReadFileInput struct {
	Path string `json:"path"           jsonschema:"absolute remote file path"`
	Host string `json:"host,omitempty" jsonschema:"named SSH host; omit to use default_host"`
}

// ReadFileOutput is the structured response for ssh_read_file.
// Encoding is "utf-8" for text files and "base64" for binary files (D-07).
type ReadFileOutput struct {
	Content          string `json:"content"`
	Encoding         string `json:"encoding"`
	InjectionWarning string `json:"_injection_warning,omitempty"`
}

// readFileHandler returns a ToolHandlerFor closure for the ssh_read_file tool.
// It detects encoding first (D-07), then reads the file, returning base64 for
// binary content or a plain utf-8 string for text content.
//
// GURD-02: text-file content is scanned for injection patterns after reading.
// Binary (base64) content is never scanned — base64 cannot carry meaningful
// injection text and scanning raw bytes would be misleading (Pitfall 1).
// Injection hits set InjectionWarning but never set IsError (GURD-01).
func readFileHandler(registry map[string]ssh.SSHExecutor, cfg *config.Config) mcp.ToolHandlerFor[ReadFileInput, ReadFileOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ReadFileInput) (*mcp.CallToolResult, ReadFileOutput, error) {
		// MHST-05/06/07: resolve the executor for the requested host.
		exec, hostName, errResult := resolveExecutor(registry, cfg, in.Host)
		if errResult != nil {
			return errResult, ReadFileOutput{}, nil
		}

		// BDIR-01: base_dir sandbox — reject paths that resolve outside the
		// configured base directory (lexical check, no symlink resolution; BDIR-03).
		if baseDir := cfg.Hosts[hostName].BaseDir; baseDir != "" {
			if !withinBaseDir(baseDir, in.Path) {
				return &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{
						Text: fmt.Sprintf("[host %s] path %q is outside base_dir %q", hostName, in.Path, baseDir),
					}},
				}, ReadFileOutput{}, nil
			}
		}

		enc, err := exec.DetectEncoding(ctx, in.Path)
		if err != nil {
			// MHST-08: prefix error with resolved host name.
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("[host %s] %s", hostName, err.Error())}},
			}, ReadFileOutput{}, nil
		}

		content, err := exec.ReadFile(ctx, in.Path)
		if err != nil {
			// MHST-08: prefix error with resolved host name.
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("[host %s] %s", hostName, err.Error())}},
			}, ReadFileOutput{}, nil
		}

		if enc == "binary" {
			// Binary content is returned base64-encoded. Never scan base64 output
			// for injection — it would produce misleading results (Pitfall 1).
			return nil, ReadFileOutput{
				Content:  base64.StdEncoding.EncodeToString(content),
				Encoding: "base64",
			}, nil
		}

		out := ReadFileOutput{
			Content:  string(content),
			Encoding: "utf-8",
		}

		// GURD-02: scan text content for injection patterns. Matched text is never
		// reflected in the warning — formatInjectionWarning uses category+count only
		// (GURD-01 invariant). Never set IsError for an injection hit.
		if !cfg.Safeguards.GuardDisabled {
			if r := guard.ScanWithPatterns(out.Content, cfg.Safeguards.CompiledPatterns); r.Matches != nil {
				out.InjectionWarning = formatInjectionWarning(r)
			}
		}

		return nil, out, nil
	}
}
