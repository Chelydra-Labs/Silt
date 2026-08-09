package mcp

import (
	"context"
	"fmt"
	"io"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// StdioProxyOptions configures the local stdio MCP server that forwards tools
// to a remote ClientSession (silt mcp CLI and in-process audit tests).
type StdioProxyOptions struct {
	Name    string // e.g. "silt" or "silt-stdio-test"
	Version string
	// ReadCloser/WriteCloser match IOTransport (CLI: os.Stdin/Stdout; tests: pipes).
	Reader io.ReadCloser
	Writer io.WriteCloser
}

// RunStdioProxy lists remote tools once at connect, registers forwarders on a
// local server, and runs until ctx cancel or transport EOF.
// Uses IOTransport only (never process StdioTransport) so tests can pass pipes
// without closing real stdout (go-sdk#548).
func RunStdioProxy(ctx context.Context, remote *mcpsdk.ClientSession, opts StdioProxyOptions) error {
	if opts.Reader == nil || opts.Writer == nil {
		return fmt.Errorf("stdio proxy: Reader and Writer are required")
	}
	name := opts.Name
	if name == "" {
		name = "silt"
	}
	version := opts.Version
	if version == "" {
		version = "dev"
	}

	local := mcpsdk.NewServer(&mcpsdk.Implementation{Name: name, Version: version}, nil)

	tools, err := remote.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}
	for _, t := range tools.Tools {
		tool := t // capture
		local.AddTool(tool, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			// Always forward the registered tool name — the local list is the
			// filter. Clients that need a refreshed surface reconnect (docs).
			params := &mcpsdk.CallToolParams{
				Name:      tool.Name,
				Arguments: req.Params.Arguments,
			}
			return remote.CallTool(ctx, params)
		})
	}

	return local.Run(ctx, &mcpsdk.IOTransport{
		Reader: opts.Reader,
		Writer: opts.Writer,
	})
}
