package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"silt/backend/keyring"
	"silt/backend/mcp"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// runMCPCLI is the `silt mcp` entrypoint. It dials the running Silt instance's
// loopback MCP HTTP endpoint (bearer from OS keyring) and proxies stdio
// JSON-RPC. Logs go to stderr only — never stdout.
//
// Usage: silt mcp [--url URL]
func runMCPCLI(args []string) int {
	// Ensure log package never touches stdout.
	log.SetOutput(os.Stderr)

	url := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--url":
			if i+1 < len(args) {
				url = args[i+1]
				i++
			}
		case "-h", "--help":
			fmt.Fprint(os.Stderr, `silt mcp — stdio proxy to the running Silt local MCP host

Requires Silt running with Local MCP enabled (Settings → AI) and a vault open.
Auth token is read from the OS keyring (never printed).

Options:
  --url URL   Override endpoint (default: discover via health on default port)
`)
			return 0
		}
	}

	token, err := loadMCPToken()
	if err != nil || token == "" {
		fmt.Fprintln(os.Stderr, "silt mcp: no auth token in OS keyring — enable Local MCP in Silt Settings first")
		return 1
	}

	if url == "" {
		url = discoverMCPEndpoint()
	}
	if url == "" {
		fmt.Fprintln(os.Stderr, "silt mcp: could not reach a running Silt MCP host — is Silt open with Local MCP enabled?")
		return 1
	}

	// Prefer Streamable HTTP client transport to the running instance.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "silt-mcp-stdio", Version: appVersion}, nil)
	transport := &mcpsdk.StreamableClientTransport{
		Endpoint: url,
		HTTPClient: &http.Client{
			Timeout: 0, // long-lived
			Transport: &bearerRoundTripper{
				base:  http.DefaultTransport,
				token: token,
			},
		},
	}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "silt mcp: connect %s: %v\n", url, err)
		return 1
	}
	defer session.Close()

	// Bridge: read JSON-RPC from stdin, forward via session, write to stdout.
	// The SDK's stdio server path is for hosting; here we are a thin proxy.
	// For v1 we run an in-process server that re-exports tools by calling the
	// remote session — simpler and keeps stdout pure JSON-RPC.
	return runStdioProxy(ctx, session)
}

type bearerRoundTripper struct {
	base  http.RoundTripper
	token string
}

func (b *bearerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Never attach the keyring bearer to a non-loopback host (endpoint-file
	// tampering / misconfiguration must not exfiltrate the token).
	if req.URL == nil || !mcp.IsLoopbackEndpoint(req.URL.String()) {
		return nil, fmt.Errorf("silt mcp: refusing to send auth to non-loopback host")
	}
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	if r.Header.Get("Content-Type") == "" && (r.Method == http.MethodPost || r.Method == http.MethodPut) {
		r.Header.Set("Content-Type", "application/json")
	}
	base := b.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

func loadMCPToken() (string, error) {
	// Env override for tests / advanced users (never log).
	if t := strings.TrimSpace(os.Getenv("SILT_MCP_TOKEN")); t != "" {
		return t, nil
	}
	return keyring.Default().Get(mcp.KeyringService, mcp.KeyringUser)
}

func discoverMCPEndpoint() string {
	// Prefer keyring-pinned endpoint (not spoofable via endpoint file alone),
	// then endpoint file only when it matches the pin (or pin unavailable),
	// then the default port.
	kr := keyring.Default()
	pinned := mcp.LoadPinnedEndpoint(kr)
	candidates := make([]string, 0, 3)
	if pinned != "" {
		candidates = append(candidates, pinned)
	}
	if ep := mcp.ReadEndpointFile(); ep != "" {
		ep = strings.TrimRight(ep, "/")
		// Ignore a file that disagrees with the pin — same-user spoof defense.
		if pinned == "" || pinned == ep {
			candidates = append(candidates, ep)
		}
	}
	candidates = append(candidates, fmt.Sprintf("http://127.0.0.1:%d", mcp.DefaultHTTPPort))
	client := &http.Client{Timeout: 800 * time.Millisecond}
	seen := map[string]bool{}
	for _, base := range candidates {
		if base == "" || seen[base] {
			continue
		}
		seen[base] = true
		if !mcp.IsLoopbackEndpoint(base) {
			continue
		}
		req, err := http.NewRequest(http.MethodGet, strings.TrimRight(base, "/")+"/health", nil)
		if err != nil {
			continue
		}
		// Health is unauthenticated (loopback discovery only).
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()
		if resp.StatusCode == 200 && strings.Contains(string(body), "silt-mcp") {
			return base
		}
	}
	return ""
}

// runStdioProxy hosts a local stdio MCP server whose tools forward to the
// remote session (tool list + call). Keeps the client config as `silt mcp`.
//
// Tools are registered from the remote list at connect time. If the host tool
// surface changes mid-session (e.g. write grant toggled), clients should
// reconnect — see docs/LOCAL_MCP.md.
func runStdioProxy(ctx context.Context, remote *mcpsdk.ClientSession) int {
	local := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "silt", Version: appVersion}, nil)

	// Mirror remote tools at startup (client discovers via tools/list).
	tools, err := remote.ListTools(ctx, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "silt mcp: list tools: %v\n", err)
		return 1
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

	if err := local.Run(ctx, &mcpsdk.StdioTransport{}); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "silt mcp: stdio: %v\n", err)
		return 1
	}
	return 0
}
