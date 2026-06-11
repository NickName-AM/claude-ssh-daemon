package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NickName-AM/claude-ssh-daemon/internal/config"
	"github.com/NickName-AM/claude-ssh-daemon/internal/forward"
	"github.com/NickName-AM/claude-ssh-daemon/internal/ssh"
)

// allocatePortFn is overridable in tests to make the duplicate-check branch deterministic.
var allocatePortFn = forward.AllocatePort

// ForwardPortInput holds the parameters for the ssh_forward_port tool.
// RemoteHost and RemotePort are required. Host is optional (omit for default_host).
type ForwardPortInput struct {
	RemoteHost string `json:"remote_host"  jsonschema:"remote host to forward traffic to (e.g. db.internal)"`
	RemotePort int    `json:"remote_port"  jsonschema:"remote port to forward traffic to (e.g. 5432)"`
	Host       string `json:"host,omitempty" jsonschema:"named SSH host; omit to use default_host"`
}

// ForwardPortOutput is the structured response for the ssh_forward_port tool (D-09).
// All five fields are always present in the JSON output.
type ForwardPortOutput struct {
	LocalPort  int    `json:"local_port"`
	RemoteHost string `json:"remote_host"`
	RemotePort int    `json:"remote_port"`
	Host       string `json:"host"`
	Status     string `json:"status"`
}

// ForwardListEntry describes a single active (or dead) port forward (D-10).
type ForwardListEntry struct {
	LocalPort  int    `json:"local_port"`
	RemoteHost string `json:"remote_host"`
	RemotePort int    `json:"remote_port"`
	Host       string `json:"host"`
	Status     string `json:"status"`
}

// ListForwardsOutput is the structured response for the ssh_list_forwards tool (D-10).
type ListForwardsOutput struct {
	Forwards []ForwardListEntry `json:"forwards"`
}

// forwardPortHandler returns a ToolHandlerFor closure for the ssh_forward_port tool.
// Sequence:
//  1. resolveExecutor — validates host exists; connection params come from cfg.Hosts.
//  2. Read h.Socket, h.User, h.Host from cfg.Hosts[hostName].
//  3. allocatePortFn() — allocate a free local port (test seam).
//  4. Duplicate check (D-02) — if key already in registry, return IsError true.
//  5. StartForward — launch ssh -L subprocess.
//  6. PollReady — wait up to 500ms for the port to become reachable (Pitfall 3).
//  7. On success — Store entry and return D-09 response shape with status "running".
func forwardPortHandler(registry map[string]ssh.SSHExecutor, cfg *config.Config, fwd *forward.Forwarder) mcp.ToolHandlerFor[ForwardPortInput, ForwardPortOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ForwardPortInput) (*mcp.CallToolResult, ForwardPortOutput, error) {
		// Step 1: resolve the executor for the requested host.
		// The executor itself is unused — connection params come from cfg.Hosts.
		_, hostName, errResult := resolveExecutor(registry, cfg, in.Host)
		if errResult != nil {
			return errResult, ForwardPortOutput{}, nil
		}

		// Validate required fields before allocating resources (CR-02).
		if in.RemoteHost == "" {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{
					Text: "remote_host is required and must not be empty",
				}},
			}, ForwardPortOutput{}, nil
		}
		if in.RemotePort < 1 || in.RemotePort > 65535 {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{
					Text: fmt.Sprintf("remote_port must be in [1, 65535], got %d", in.RemotePort),
				}},
			}, ForwardPortOutput{}, nil
		}

		// Step 2: read connection params from the config.
		h := cfg.Hosts[hostName]

		// Step 3: allocate a free local port via the test seam.
		localPort, err := allocatePortFn()
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{
					Text: fmt.Sprintf("[host %s] failed to allocate local port: %s", hostName, err.Error()),
				}},
			}, ForwardPortOutput{}, nil
		}

		// Step 4: duplicate check (D-02). With port-0 allocation a fresh port is
		// essentially never a duplicate in production; the check is reachable
		// deterministically in tests via the allocatePortFn seam.
		key := forward.Key(hostName, localPort)
		if fwd.Has(key) {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{
					Text: fmt.Sprintf("[host %s] port %d is already forwarded on host %s", hostName, localPort, hostName),
				}},
			}, ForwardPortOutput{}, nil
		}

		// Step 5: launch the ssh -L subprocess.
		entry, err := forward.StartForward(h.Socket, h.User, h.Host, localPort, in.RemoteHost, in.RemotePort)
		if err != nil {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{
					Text: fmt.Sprintf("[host %s] failed to start ssh forward: %s", hostName, err.Error()),
				}},
			}, ForwardPortOutput{}, nil
		}

		// Step 6: poll until the local port becomes reachable (500ms budget).
		// Do NOT Store on failure — a stale entry would block future forwards (Pitfall 3, T-11-07).
		if err := forward.PollReady(localPort); err != nil {
			// Check whether ssh already exited on its own before we kill it (WR-01).
			// The check must happen before Kill() because Kill() is non-blocking —
			// the reaper goroutine may not have set exited by the time we check after.
			alreadyExited := forward.HasExited(entry)
			if entry.Cmd.Process != nil {
				_ = entry.Cmd.Process.Kill()
			}
			hint := ""
			if alreadyExited {
				hint = fmt.Sprintf(" (ssh process exited immediately — ControlMaster socket %s may be dead)", h.Socket)
			}
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{
					Text: fmt.Sprintf("[host %s] %s%s", hostName, err.Error(), hint),
				}},
			}, ForwardPortOutput{}, nil
		}

		// Step 7: store the entry only after successful readiness check (Pitfall 3, T-11-07).
		// Populate the remaining fields now that we know the forward is live.
		entry.LocalPort = localPort
		entry.RemoteHost = in.RemoteHost
		entry.RemotePort = in.RemotePort
		entry.HostName = hostName
		entry.StartedAt = time.Now()
		fwd.Store(key, entry)

		// Return nil *CallToolResult so SDK auto-populates Content from out.
		// Status is always "running" at creation time (D-09).
		return nil, ForwardPortOutput{
			LocalPort:  localPort,
			RemoteHost: in.RemoteHost,
			RemotePort: in.RemotePort,
			Host:       hostName,
			Status:     "running",
		}, nil
	}
}

// listForwardsHandler returns a ToolHandlerFor closure for the ssh_list_forwards tool (D-10).
// Takes a snapshot of the Forwarder and maps entries to ForwardListEntry with running/dead status.
// The Forwards slice is always non-nil so it JSON-marshals to [] not null (Pitfall 7, T-11-09).
func listForwardsHandler(fwd *forward.Forwarder) mcp.ToolHandlerFor[struct{}, ListForwardsOutput] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ListForwardsOutput, error) {
		entries := fwd.Snapshot()
		// Pitfall 7: use make([]ForwardListEntry, 0) so an empty list marshals to
		// JSON [] rather than null.
		out := make([]ForwardListEntry, 0)
		for _, e := range entries {
			out = append(out, ForwardListEntry{
				LocalPort:  e.LocalPort,
				RemoteHost: e.RemoteHost,
				RemotePort: e.RemotePort,
				Host:       e.HostName,
				Status:     forward.Status(e),
			})
		}
		return nil, ListForwardsOutput{Forwards: out}, nil
	}
}
