package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"silt/backend/keyring"
	"silt/backend/parser"
	"silt/backend/types"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// bearerTransport injects the host bearer token into every outgoing request so
// a real streamable MCP client can pass the host authMiddleware. These tests
// drive the full loopback HTTP transport (no in-memory shortcuts) so the SDK
// input-processing layer, the auth gate, and the receiving middleware are all
// exercised end to end.
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	if t.base == nil {
		t.base = http.DefaultTransport
	}
	return t.base.RoundTrip(req)
}

// startHTTPHost brings up a real loopback HTTP host and a streamable MCP client
// connected to it over HTTP. The returned MemoryAuditor captures every outcome
// the middleware and handlers record.
func startHTTPHost(t *testing.T, bridge Bridge, cfg Config) (*Host, *MemoryAuditor, *mcpsdk.ClientSession) {
	t.Helper()
	aud := &MemoryAuditor{}
	h := NewHost(Options{Keyring: keyring.NewFake(), Auditor: aud, Version: "test"})
	if err := h.Start(bridge, cfg); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { h.Stop() })
	cs := dialHTTPClient(t, h)
	return h, aud, cs
}

func dialHTTPClient(t *testing.T, h *Host) *mcpsdk.ClientSession {
	t.Helper()
	transport := &mcpsdk.StreamableClientTransport{
		Endpoint:             h.Endpoint(),
		HTTPClient:           &http.Client{Transport: &bearerTransport{token: h.Token()}},
		DisableStandaloneSSE: true,
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "silt-test", Version: "test"}, nil)
	cs, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func countOutcome(aud *MemoryAuditor, tool, outcome string) int {
	n := 0
	for _, e := range aud.Entries {
		if e.Tool == tool && e.Outcome == outcome {
			n++
		}
	}
	return n
}

func anyEntryContains(aud *MemoryAuditor, substr string) bool {
	for _, e := range aud.Entries {
		if strings.Contains(e.Error, substr) {
			return true
		}
	}
	return false
}

// TestSchemaAudit_MissingRequired verifies the canonical rejected_schema path:
// a tools/call missing a required field is rejected by the SDK before the
// handler runs, so the middleware records exactly one rejected_schema and the
// handler records nothing. This is the new outcome introduced by Phase 6/#864.
func TestSchemaAudit_MissingRequired(t *testing.T) {
	bridge := &fakeBridge{path: t.TempDir()}
	cfg := Config{Enabled: true, HTTPEnabled: true, HTTPPort: freePort(t)}
	_, aud, cs := startHTTPHost(t, bridge, cfg)

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "search_blocks",
		Arguments: map[string]any{}, // missing required "query"
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for missing required arg")
	}
	if got := countOutcome(aud, "search_blocks", OutcomeRejectedSchema); got != 1 {
		t.Fatalf("rejected_schema count=%d want 1: %+v", got, aud.Entries)
	}
	// Handler never ran, so no ok/error entry must leak.
	if got := countOutcome(aud, "search_blocks", "error"); got != 0 {
		t.Fatalf("handler error leaked on schema rejection: %+v", aud.Entries)
	}
	if !anyEntryContains(aud, "required") {
		t.Fatalf("expected SDK validation message mentioning required: %+v", aud.Entries)
	}
}

// TestSchemaAudit_WrongType verifies a wrong-typed argument (number where a
// string is expected) is classified as rejected_schema, distinct from a
// missing field. The SDK rejects it during input validation before the handler.
// Identifier-shaped values (notebook/page) are preserved in the redacted meta
// by design, same as handler-level audit; body fields stay redacted.
func TestSchemaAudit_WrongType(t *testing.T) {
	bridge := &fakeBridge{path: t.TempDir()}
	cfg := Config{Enabled: true, HTTPEnabled: true, HTTPPort: freePort(t)}
	_, aud, cs := startHTTPHost(t, bridge, cfg)

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "read_page",
		Arguments: map[string]any{
			"notebook": 123, // number where string is expected
			"section":  "",
			"page":     "Home",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for wrong type")
	}
	if got := countOutcome(aud, "read_page", OutcomeRejectedSchema); got != 1 {
		t.Fatalf("rejected_schema count=%d want 1: %+v", got, aud.Entries)
	}
	// The handler must not have run on a schema-rejected call.
	if got := countOutcome(aud, "read_page", "ok") + countOutcome(aud, "read_page", "error"); got != 0 {
		t.Fatalf("handler ran despite schema rejection: %+v", aud.Entries)
	}
	// A body field, if present in the rejected args, must still be redacted
	// (length only, never content) — confirming redaction applies on the
	// rejected_schema path the same way it does for handler-level audit.
	res2, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "update_blocks",
		Arguments: map[string]any{
			"notebook": 123, // wrong type → schema reject
			"section":  "",
			"page":     "P",
			"blocks": []any{
				map[string]any{"type": "NOTE", "text": "top-secret-body"},
			},
		},
	})
	if err != nil {
		t.Fatalf("CallTool update_blocks: %v", err)
	}
	if !res2.IsError {
		t.Fatal("expected IsError for wrong type on update_blocks")
	}
	if got := countOutcome(aud, "update_blocks", OutcomeRejectedSchema); got != 1 {
		t.Fatalf("rejected_schema count=%d want 1: %+v", got, aud.Entries)
	}
	for _, e := range aud.Entries {
		if e.Tool != "update_blocks" || e.Outcome != OutcomeRejectedSchema {
			continue
		}
		raw, _ := json.Marshal(e.ArgsMeta)
		if strings.Contains(string(raw), "top-secret-body") {
			t.Fatalf("block body leaked into rejected_schema meta: %s", raw)
		}
		if !strings.Contains(string(raw), "blocks_count") {
			t.Fatalf("expected blocks_count in redacted meta: %s", raw)
		}
	}
}

// TestSchemaAudit_ValidCallNotRecorded confirms a well-formed call reaches the
// handler and audits ok exactly once, with zero rejected_schema entries — i.e.
// the middleware does not double-audit successful handler outcomes.
func TestSchemaAudit_ValidCallNotRecorded(t *testing.T) {
	bridge := &fakeBridge{
		path: t.TempDir(),
		search: parser.SearchResult{
			Results: []parser.TaskResult{{ID: "b1", Notebook: "Work", Page: "Home", Snippet: "hi"}},
			Total:   1,
		},
	}
	cfg := Config{Enabled: true, HTTPEnabled: true, HTTPPort: freePort(t)}
	_, aud, cs := startHTTPHost(t, bridge, cfg)

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "search_blocks",
		Arguments: map[string]any{"query": "hi"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error: %s", toolText(t, res))
	}
	if got := countOutcome(aud, "search_blocks", "ok"); got != 1 {
		t.Fatalf("ok count=%d want 1: %+v", got, aud.Entries)
	}
	if got := countOutcome(aud, "search_blocks", OutcomeRejectedSchema); got != 0 {
		t.Fatalf("rejected_schema leaked on valid call: %+v", aud.Entries)
	}
}

// TestSchemaAudit_AuthDenialNotDoubleAudited verifies an authorization denial
// (write tool called without the write grant) audits as denied exactly once and
// is NOT misclassified as rejected_schema — the handler owns that outcome.
func TestSchemaAudit_AuthDenialNotDoubleAudited(t *testing.T) {
	bridge := &fakeBridge{path: t.TempDir()}
	cfg := Config{Enabled: true, HTTPEnabled: true, HTTPPort: freePort(t), WriteEnabled: false}
	_, aud, cs := startHTTPHost(t, bridge, cfg)

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "create_page",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "X",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for denied write")
	}
	if got := countOutcome(aud, "create_page", "denied"); got != 1 {
		t.Fatalf("denied count=%d want 1: %+v", got, aud.Entries)
	}
	if got := countOutcome(aud, "create_page", OutcomeRejectedSchema); got != 0 {
		t.Fatalf("denial misclassified as rejected_schema: %+v", aud.Entries)
	}
	if bridge.createN != 0 {
		t.Fatalf("bridge create ran despite deny: %d", bridge.createN)
	}
}

// TestSchemaAudit_SemanticRejectionNotDoubleAudited verifies a handler-level
// schema-validation rejection (bridge returns types.ValidationError) audits as
// rejected exactly once and is NOT also recorded as rejected_schema. This pins
// the distinction: rejected = a value the bridge turned down;
// rejected_schema = a call the SDK never let through to the handler.
func TestSchemaAudit_SemanticRejectionNotDoubleAudited(t *testing.T) {
	bridge := &stubBridge{
		fakeBridge: &fakeBridge{path: t.TempDir()},
		propErr:    types.ValidationError{Field: "status", Message: `"bogus" is not allowed`},
	}
	cfg := Config{Enabled: true, HTTPEnabled: true, HTTPPort: freePort(t), WriteEnabled: true}
	_, aud, cs := startHTTPHost(t, bridge, cfg)

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "set_page_property",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "M",
			"property": "status", "value": "bogus",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for semantic rejection")
	}
	if got := countOutcome(aud, "set_page_property", "rejected"); got != 1 {
		t.Fatalf("rejected count=%d want 1: %+v", got, aud.Entries)
	}
	if got := countOutcome(aud, "set_page_property", OutcomeRejectedSchema); got != 0 {
		t.Fatalf("semantic rejection misclassified as rejected_schema: %+v", aud.Entries)
	}
}

// TestSchemaAudit_HandlerErrorNotDoubleAudited verifies a non-validation
// handler error (bridge I/O / transient failure) audits as error exactly once
// and is NOT classified as rejected_schema.
func TestSchemaAudit_HandlerErrorNotDoubleAudited(t *testing.T) {
	bridge := &stubBridge{
		fakeBridge: &fakeBridge{path: t.TempDir()},
		metaErr:    errors.New("page file not found: /x/Y.md"),
	}
	cfg := Config{Enabled: true, HTTPEnabled: true, HTTPPort: freePort(t)}
	_, aud, cs := startHTTPHost(t, bridge, cfg)

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "get_page_metadata",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "Y",
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for handler error")
	}
	if got := countOutcome(aud, "get_page_metadata", "error"); got != 1 {
		t.Fatalf("error count=%d want 1: %+v", got, aud.Entries)
	}
	if got := countOutcome(aud, "get_page_metadata", OutcomeRejectedSchema); got != 0 {
		t.Fatalf("handler error misclassified as rejected_schema: %+v", aud.Entries)
	}
}

// TestSchemaAudit_UnknownToolIsJSONRPCError verifies an unknown tool name yields
// a JSON-RPC error (err != nil), NOT a rejected_schema entry. Unknown tools and
// malformed outer params are protocol problems, deliberately excluded from the
// rejected_schema classification.
func TestSchemaAudit_UnknownToolIsJSONRPCError(t *testing.T) {
	bridge := &fakeBridge{path: t.TempDir()}
	cfg := Config{Enabled: true, HTTPEnabled: true, HTTPPort: freePort(t)}
	_, aud, cs := startHTTPHost(t, bridge, cfg)

	_, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "no_such_tool",
		Arguments: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected JSON-RPC error for unknown tool")
	}
	if got := countOutcome(aud, "no_such_tool", OutcomeRejectedSchema); got != 0 {
		t.Fatalf("unknown tool misclassified as rejected_schema: %+v", aud.Entries)
	}
}

// TestSchemaAudit_AuditStoreFailureIsBestEffort verifies the middleware's
// rejected_schema recording is best-effort: if the auditor panics (simulating a
// catastrophic audit-store failure), the SDK's own error result still reaches
// the client. The middleware is the sole audit path for pre-handler rejections,
// so an observability hiccup must not regress the call outcome.
func TestSchemaAudit_AuditStoreFailureIsBestEffort(t *testing.T) {
	h := NewHost(Options{Keyring: keyring.NewFake(), Auditor: panicAuditor{}, Version: "test"})
	bridge := &fakeBridge{path: t.TempDir()}
	if err := h.Start(bridge, Config{Enabled: true, HTTPEnabled: true, HTTPPort: freePort(t)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer h.Stop()
	cs := dialHTTPClient(t, h)

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "search_blocks",
		Arguments: map[string]any{}, // missing required → rejected_schema path
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	// The SDK error result survives the auditor panic; IsError reflects the
	// schema rejection, not a propagated server-side failure.
	if !res.IsError {
		t.Fatal("expected IsError (SDK validation result) despite auditor panic")
	}
}

// TestSchemaAudit_PersistsToFile verifies the real fileAuditor I/O path writes a
// rejected_schema entry to mcp-audit.jsonl (the host creates a fileAuditor when
// no Options.Auditor is supplied).
func TestSchemaAudit_PersistsToFile(t *testing.T) {
	vault := t.TempDir()
	h := NewHost(Options{Keyring: keyring.NewFake(), Version: "test"}) // no Auditor → fileAuditor
	bridge := &fakeBridge{path: vault}
	if err := h.Start(bridge, Config{Enabled: true, HTTPEnabled: true, HTTPPort: freePort(t)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer h.Stop()
	cs := dialHTTPClient(t, h)

	_, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "search_blocks",
		Arguments: map[string]any{}, // missing required
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	// Give the synchronous Record append a moment to flush, then read the file.
	path := filepath.Join(vault, ".system", "logs", "mcp-audit.jsonl")
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read audit file: %v", readErr)
	}
	if !strings.Contains(string(data), OutcomeRejectedSchema) {
		t.Fatalf("expected rejected_schema entry in audit file, got:\n%s", data)
	}
	if !strings.Contains(string(data), "search_blocks") {
		t.Fatalf("expected tool name in audit file, got:\n%s", data)
	}
}

// TestSchemaAudit_Concurrent verifies the middleware is safe under concurrent
// tools/call (run with -race): each schema-rejecting call records exactly one
// rejected_schema entry, with no drops, duplicates, or races on the auditor.
func TestSchemaAudit_Concurrent(t *testing.T) {
	bridge := &fakeBridge{
		path:   t.TempDir(),
		search: parser.SearchResult{Results: []parser.TaskResult{{ID: "b1"}}},
	}
	cfg := Config{Enabled: true, HTTPEnabled: true, HTTPPort: freePort(t)}
	_, aud, cs := startHTTPHost(t, bridge, cfg)

	const badN = 16
	const goodN = 8
	var wg sync.WaitGroup
	var badOK, goodOK int32
	for i := 0; i < badN; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
				Name:      "search_blocks",
				Arguments: map[string]any{}, // missing required → rejected_schema
			})
			if err == nil && res != nil && res.IsError {
				atomic.AddInt32(&badOK, 1)
			}
		}()
	}
	for i := 0; i < goodN; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
				Name:      "search_blocks",
				Arguments: map[string]any{"query": "hi"},
			})
			if err == nil && res != nil && !res.IsError {
				atomic.AddInt32(&goodOK, 1)
			}
		}()
	}
	wg.Wait()

	if got := countOutcome(aud, "search_blocks", OutcomeRejectedSchema); got != badN {
		t.Fatalf("rejected_schema count=%d want %d: %+v", got, badN, aud.Entries)
	}
	if got := countOutcome(aud, "search_blocks", "ok"); got != goodN {
		t.Fatalf("ok count=%d want %d: %+v", got, goodN, aud.Entries)
	}
	if int(badOK) != badN {
		t.Fatalf("bad call IsError count=%d want %d", badOK, badN)
	}
	if int(goodOK) != goodN {
		t.Fatalf("good call success count=%d want %d", goodOK, goodN)
	}
}

// panicAuditor simulates a catastrophic audit-store failure for the
// best-effort recording test. Only Record is exercised on the hot path.
type panicAuditor struct{}

func (panicAuditor) Record(AuditEntry) { panic("simulated audit store failure") }
func (panicAuditor) Close()            {}
