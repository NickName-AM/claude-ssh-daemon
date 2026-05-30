package daemon

import (
	"net"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connTransport wraps a net.Conn as an MCP IOTransport.
//
// net.Conn satisfies both io.ReadCloser and io.WriteCloser.
// IOTransport.Reader and IOTransport.Writer are exported fields in go-sdk v1.6.1.
// Do NOT implement a custom Transport interface — IOTransport already handles
// newline-delimited JSON and the concurrent read goroutine internally
// (see RESEARCH.md §Don't Hand-Roll).
func connTransport(conn net.Conn) *mcp.IOTransport {
	return &mcp.IOTransport{Reader: conn, Writer: conn}
}
