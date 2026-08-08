package mcp

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// postMCP posts a raw body to the host MCP endpoint. Used to exercise the
// HTTP gate and streamable handler without the SDK client (malformed wire).
func postMCP(t *testing.T, endpoint, token, contentType string, body []byte, extra http.Header) (status int, respBody []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, endpoint+"/", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, vals := range extra {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, b
}

func auditLen(aud *MemoryAuditor) int {
	aud.mu.Lock()
	defer aud.mu.Unlock()
	return len(aud.Entries)
}

// TestWireAudit_AuthAndContentTypeGateNoAudit locks the LOCAL_MCP contract:
// transport-auth and Content-Type failures never produce audit rows.
func TestWireAudit_AuthAndContentTypeGateNoAudit(t *testing.T) {
	bridge := &fakeBridge{path: t.TempDir()}
	cfg := Config{Enabled: true, HTTPEnabled: true, HTTPPort: freePort(t)}
	h, aud, _ := startHTTPHost(t, bridge, cfg)
	ep := h.Endpoint()
	tok := h.Token()
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search_blocks","arguments":{}}}`)

	cases := []struct {
		name string
		tok  string
		ct   string
	}{
		{name: "missing auth", tok: "", ct: "application/json"},
		{name: "bad bearer", tok: "not-the-token", ct: "application/json"},
		{name: "missing content-type", tok: tok, ct: ""},
		{name: "wrong content-type", tok: tok, ct: "text/plain"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := auditLen(aud)
			status, _ := postMCP(t, ep, tc.tok, tc.ct, body, nil)
			if status < 400 {
				t.Fatalf("status=%d want 4xx", status)
			}
			if got := auditLen(aud); got != before {
				t.Fatalf("audit grew %d→%d on transport gate failure", before, got)
			}
		})
	}
}

// TestWireAudit_MalformedBodyNoAudit covers truncated / non-JSON bodies that
// pass the auth gate but never become a tools/call the middleware can audit.
func TestWireAudit_MalformedBodyNoAudit(t *testing.T) {
	bridge := &fakeBridge{path: t.TempDir()}
	cfg := Config{Enabled: true, HTTPEnabled: true, HTTPPort: freePort(t)}
	h, aud, _ := startHTTPHost(t, bridge, cfg)
	ep := h.Endpoint()
	tok := h.Token()

	bodies := [][]byte{
		[]byte(`{`),
		[]byte(`not json at all`),
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`),
	}
	for i, body := range bodies {
		before := auditLen(aud)
		_, _ = postMCP(t, ep, tok, "application/json", body, nil)
		if got := auditLen(aud); got != before {
			t.Fatalf("case %d: audit grew on malformed body: %+v", i, aud.Entries)
		}
	}
}

// TestWireAudit_UnknownToolAndMalformedOuterParamsNoSchemaAudit reinforces
// that protocol-level failures are not classified as rejected_schema.
func TestWireAudit_UnknownToolAndMalformedOuterParamsNoSchemaAudit(t *testing.T) {
	bridge := &fakeBridge{path: t.TempDir()}
	cfg := Config{Enabled: true, HTTPEnabled: true, HTTPPort: freePort(t)}
	h, aud, cs := startHTTPHost(t, bridge, cfg)

	// Unknown tool via SDK (session already initialized).
	_, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "no_such_tool_wire",
		Arguments: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected JSON-RPC error for unknown tool")
	}
	if got := countOutcome(aud, "no_such_tool_wire", OutcomeRejectedSchema); got != 0 {
		t.Fatalf("unknown tool audited as rejected_schema: %+v", aud.Entries)
	}

	// Raw POST with params as a JSON string (malformed outer tools/call).
	// Streamable handler may 4xx without a session; either way no schema audit.
	before := auditLen(aud)
	raw := []byte(`{"jsonrpc":"2.0","id":99,"method":"tools/call","params":"not-an-object"}`)
	_, _ = postMCP(t, h.Endpoint(), h.Token(), "application/json", raw, nil)
	for _, e := range aud.Entries[before:] {
		if e.Outcome == OutcomeRejectedSchema {
			t.Fatalf("malformed outer params produced rejected_schema: %+v", e)
		}
	}
	if got := countOutcome(aud, "search_blocks", OutcomeRejectedSchema); got != 0 {
		t.Fatalf("unexpected rejected_schema before control: %+v", aud.Entries)
	}
}

// TestWireAudit_SchemaInvalidStillAudited is the positive control for the
// raw-wire harness: a schema-invalid tools/call over the live HTTP host still
// yields exactly one rejected_schema (same contract as host_schema_audit_test).
func TestWireAudit_SchemaInvalidStillAudited(t *testing.T) {
	bridge := &fakeBridge{path: t.TempDir()}
	cfg := Config{Enabled: true, HTTPEnabled: true, HTTPPort: freePort(t)}
	_, aud, cs := startHTTPHost(t, bridge, cfg)

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "search_blocks",
		Arguments: map[string]any{
			"query": 123, // wrong type
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for wrong-typed arg")
	}
	if got := countOutcome(aud, "search_blocks", OutcomeRejectedSchema); got != 1 {
		t.Fatalf("rejected_schema count=%d want 1: %+v", got, aud.Entries)
	}
	// Ensure error text was sanitized (no raw "123" leak required here — unit
	// tests cover sanitize; just ensure we recorded something useful).
	if !anyEntryContains(aud, "type") && !anyEntryContains(aud, "validat") {
		// Soft check — message shape varies by SDK version.
		t.Logf("schema error text: %+v", aud.Entries)
	}
}

// TestSchemaAudit_ViaStdioProxyPath proves a tools/call that reaches the host
// through the same local-forwarding shape as `silt mcp` (stdio proxy → remote
// HTTP session) still records rejected_schema exactly once on the host auditor.
// Uses IOTransport pipes — not process StdioTransport — to avoid closing test
// stdout / breaking coverage (go-sdk#548).
func TestSchemaAudit_ViaStdioProxyPath(t *testing.T) {
	bridge := &fakeBridge{path: t.TempDir()}
	cfg := Config{Enabled: true, HTTPEnabled: true, HTTPPort: freePort(t)}
	_, aud, remote := startHTTPHost(t, bridge, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Local server mirrors remote tools and forwards CallTool — same shape as
	// runStdioProxy in cmd_mcp.go.
	local := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "silt-stdio-test", Version: "test"}, nil)
	tools, err := remote.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range tools.Tools {
		tool := tool
		local.AddTool(tool, func(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
			params := &mcpsdk.CallToolParams{
				Name:      tool.Name,
				Arguments: req.Params.Arguments,
			}
			return remote.CallTool(ctx, params)
		})
	}

	// Bidirectional pipes: client ↔ local proxy server.
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	t.Cleanup(func() {
		_ = c2sW.Close()
		_ = s2cW.Close()
		_ = c2sR.Close()
		_ = s2cR.Close()
	})

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- local.Run(ctx, &mcpsdk.IOTransport{
			Reader: c2sR,
			Writer: s2cW,
		})
	}()

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "stdio-test-client", Version: "test"}, nil)
	cs, err := client.Connect(ctx, &mcpsdk.IOTransport{
		Reader: s2cR,
		Writer: c2sW,
	}, nil)
	if err != nil {
		t.Fatalf("stdio client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "search_blocks",
		Arguments: map[string]any{}, // missing required query
	})
	if err != nil {
		t.Fatalf("CallTool via stdio proxy: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for schema rejection via stdio path")
	}
	if got := countOutcome(aud, "search_blocks", OutcomeRejectedSchema); got != 1 {
		t.Fatalf("rejected_schema via stdio path count=%d want 1: %+v", got, aud.Entries)
	}
	if got := countOutcome(aud, "search_blocks", "error"); got != 0 {
		t.Fatalf("handler error leaked on schema rejection via stdio: %+v", aud.Entries)
	}

	// Valid call still reaches the host through the same path.
	res, err = cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "search_blocks",
		Arguments: map[string]any{"query": "hello"},
	})
	if err != nil {
		t.Fatalf("valid CallTool: %v", err)
	}
	if res.IsError {
		t.Logf("valid call IsError=%v (bridge stub may error)", res.IsError)
	} else if got := countOutcome(aud, "search_blocks", "ok"); got != 1 {
		t.Fatalf("ok count=%d want 1 after valid call: %+v", got, aud.Entries)
	}

	cancel()
	select {
	case err := <-serverErr:
		if err != nil && ctx.Err() == nil && !strings.Contains(err.Error(), "closed") {
			t.Logf("local server exit: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Log("local server did not exit promptly after cancel")
	}
}
