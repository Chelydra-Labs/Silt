package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"silt/backend/types"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// stubBridge lets each typed-properties tool test pin the exact return/error
// the bridge should yield, without dragging in a full fakeBridge. It embeds the
// shared fakeBridge so unrelated Bridge methods stay satisfied.
type stubBridge struct {
	*fakeBridge
	metaResult  PageMetadataResult
	metaErr     error
	propErr     error
	typeErr     error
	typeFlagged []string
	gotProp     setPropCall
	gotType     setTypeCall
}

func (s *stubBridge) GetPageMetadata(ctx context.Context, notebook, section, page string) (PageMetadataResult, error) {
	_ = ctx
	if s.metaErr != nil {
		return PageMetadataResult{}, s.metaErr
	}
	return s.metaResult, nil
}

func (s *stubBridge) SetPageProperty(ctx context.Context, notebook, section, page, property, value string) error {
	_ = ctx
	s.gotProp = setPropCall{notebook, section, page, property, value}
	return s.propErr
}

func (s *stubBridge) SetPageType(ctx context.Context, notebook, section, page, typeName string) ([]string, error) {
	_ = ctx
	s.gotType = setTypeCall{notebook, section, page, typeName}
	return s.typeFlagged, s.typeErr
}

// connectStubTools mirrors connectTools but lets a test pass a pre-built stub.
func connectStubTools(t *testing.T, bridge Bridge, cfg Config) (*mcpsdk.ClientSession, *MemoryAuditor) {
	t.Helper()
	aud := &MemoryAuditor{}
	env := &toolEnv{
		bridge: bridge,
		cfg:    func() Config { return cfg },
		audit:  aud,
		client: func(context.Context) string { return "test" },
	}
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "silt", Version: "test"}, nil)
	registerTools(srv, env)
	t1, t2 := mcpsdk.NewInMemoryTransports()
	if _, err := srv.Connect(context.Background(), t1, nil); err != nil {
		t.Fatal(err)
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := client.Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs, aud
}

func TestTool_GetPageMetadata_TypedPage(t *testing.T) {
	bridge := &stubBridge{fakeBridge: &fakeBridge{path: t.TempDir()}}
	bridge.metaResult = PageMetadataResult{
		Notebook: "Work",
		Section:  "",
		Page:     "Meeting",
		Type:     "meeting",
		Properties: []PropertyValue{
			{Name: "attendees", Type: "text", Value: "alice, bob", IsSet: true, Required: true},
			{Name: "status", Type: "select", Options: []string{"draft", "done"}},
		},
		Frontmatter: map[string]any{"type": "meeting", "attendees": "alice, bob"},
	}
	cs, aud := connectStubTools(t, bridge, Config{Enabled: true})
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "get_page_metadata",
		Arguments: map[string]any{"notebook": "Work", "section": "", "page": "Meeting"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("isError: %s", toolText(t, res))
	}
	var payload PageMetadataResult
	if err := json.Unmarshal([]byte(toolText(t, res)), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Type != "meeting" {
		t.Errorf("Type = %q want meeting", payload.Type)
	}
	if len(payload.Properties) != 2 || payload.Properties[0].Name != "attendees" {
		t.Errorf("Properties = %+v", payload.Properties)
	}
	if payload.Frontmatter["attendees"] != "alice, bob" {
		t.Errorf("Frontmatter = %+v", payload.Frontmatter)
	}
	if !auditHas(aud, "get_page_metadata", "ok") {
		t.Fatalf("missing ok audit: %+v", aud.Entries)
	}
}

func TestTool_GetPageMetadata_NoVault(t *testing.T) {
	env := &toolEnv{
		bridge: nil,
		cfg:    func() Config { return Config{Enabled: true} },
		audit:  &MemoryAuditor{},
		client: func(context.Context) string { return "test" },
	}
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "silt", Version: "test"}, nil)
	registerTools(srv, env)
	t1, t2 := mcpsdk.NewInMemoryTransports()
	if _, err := srv.Connect(context.Background(), t1, nil); err != nil {
		t.Fatal(err)
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := client.Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "get_page_metadata",
		Arguments: map[string]any{"notebook": "X", "section": "", "page": "Y"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected isError for no vault")
	}
	if !strings.Contains(toolText(t, res), "no vault") {
		t.Fatalf("text = %q", toolText(t, res))
	}
}

func TestTool_GetPageMetadata_BridgeError(t *testing.T) {
	bridge := &stubBridge{
		fakeBridge: &fakeBridge{path: t.TempDir()},
		metaErr:    errors.New("page file not found: /x/Y.md"),
	}
	cs, _ := connectStubTools(t, bridge, Config{Enabled: true})
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "get_page_metadata",
		Arguments: map[string]any{"notebook": "X", "section": "", "page": "Missing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected isError for missing page")
	}
	if !strings.Contains(toolText(t, res), "page file not found") {
		t.Fatalf("expected bridge error in tool text, got %q", toolText(t, res))
	}
}

func TestTool_SetPageProperty_GrantDenied(t *testing.T) {
	bridge := &stubBridge{fakeBridge: &fakeBridge{path: t.TempDir()}}
	cs, aud := connectStubTools(t, bridge, Config{Enabled: true, WriteEnabled: false})

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "set_page_property",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "M",
			"property": "rating", "value": "4",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected isError when write grant off")
	}
	if !strings.Contains(toolText(t, res), "write tools are disabled") {
		t.Fatalf("text = %q", toolText(t, res))
	}
	if bridge.gotProp != (setPropCall{}) {
		t.Fatalf("bridge was called despite deny: %+v", bridge.gotProp)
	}
	if !auditHas(aud, "set_page_property", "denied") {
		t.Fatalf("missing denied audit: %+v", aud.Entries)
	}
}

func TestTool_SetPageProperty_Success(t *testing.T) {
	bridge := &stubBridge{fakeBridge: &fakeBridge{path: t.TempDir()}}
	cs, _ := connectStubTools(t, bridge, Config{Enabled: true, WriteEnabled: true})

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "set_page_property",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "M",
			"property": "rating", "value": "4",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("isError: %s", toolText(t, res))
	}
	want := setPropCall{Notebook: "Work", Section: "", Page: "M", Property: "rating", Value: "4"}
	if bridge.gotProp != want {
		t.Fatalf("bridge args = %+v want %+v", bridge.gotProp, want)
	}
}

// TestTool_SetPageProperty_BridgeErrorBecomesStructuredError is the wire-level
// half of the safety contract: when the bridge returns a schema-validation
// error (types.ValidationError), the tool surfaces it as a structured MCP error
// result whose StructuredContent is a machine-readable {ok:false,
// errors:[{property,message}]} body. Clients branch on .property instead of
// parsing text. The file-untouched half of the contract is exercised end-to-end
// in app_mcp_types_test.go (real App + file).
func TestTool_SetPageProperty_BridgeErrorBecomesStructuredError(t *testing.T) {
	bridge := &stubBridge{
		fakeBridge: &fakeBridge{path: t.TempDir()},
		propErr:    types.ValidationError{Field: "status", Message: `"bogus" is not one of the allowed options [available read]`},
	}
	cs, _ := connectStubTools(t, bridge, Config{Enabled: true, WriteEnabled: true})

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "set_page_property",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "M",
			"property": "status", "value": "bogus",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected isError for validation failure")
	}
	body := structuredBody(t, res)
	if body["ok"] != false {
		t.Errorf("structured ok = %v, want false", body["ok"])
	}
	errs, _ := body["errors"].([]any)
	if len(errs) != 1 {
		t.Fatalf("structured errors = %v, want 1 entry", errs)
	}
	first, _ := errs[0].(map[string]any)
	if first["property"] != "status" {
		t.Errorf("error property = %v, want status", first["property"])
	}
	if !strings.Contains(first["message"].(string), "not one of the allowed options") {
		t.Errorf("error message = %v, want it to mention the validation failure", first["message"])
	}
}

func TestTool_SetPageType_GrantDenied(t *testing.T) {
	bridge := &stubBridge{fakeBridge: &fakeBridge{path: t.TempDir()}}
	cs, aud := connectStubTools(t, bridge, Config{Enabled: true, WriteEnabled: false})

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "set_page_type",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "M", "type": "meeting",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected isError when write grant off")
	}
	if bridge.gotType != (setTypeCall{}) {
		t.Fatalf("bridge was called despite deny: %+v", bridge.gotType)
	}
	if !auditHas(aud, "set_page_type", "denied") {
		t.Fatalf("missing denied audit: %+v", aud.Entries)
	}
}

func TestTool_SetPageType_Success(t *testing.T) {
	bridge := &stubBridge{fakeBridge: &fakeBridge{path: t.TempDir()}}
	cs, _ := connectStubTools(t, bridge, Config{Enabled: true, WriteEnabled: true})

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "set_page_type",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "M", "type": "meeting",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("isError: %s", toolText(t, res))
	}
	want := setTypeCall{Notebook: "Work", Section: "", Page: "M", Type: "meeting"}
	if bridge.gotType != want {
		t.Fatalf("bridge args = %+v want %+v", bridge.gotType, want)
	}
	// Plain assign with no flagged values omits the `flagged` key entirely.
	body := structuredBody(t, res)
	if body["ok"] != true {
		t.Errorf("structured ok = %v, want true", body["ok"])
	}
	if _, present := body["flagged"]; present {
		t.Errorf("structured response included flagged on a clean assign: %+v", body)
	}
}

// TestTool_SetPageType_FlaggedSurfaced verifies that when the bridge returns a
// non-empty keep-and-flag list (existing values that do not fit the new schema),
// the tool surfaces it as `flagged` in the success response — clients use this
// to warn the user that some values need attention even though the write
// succeeded.
func TestTool_SetPageType_FlaggedSurfaced(t *testing.T) {
	bridge := &stubBridge{
		fakeBridge:  &fakeBridge{path: t.TempDir()},
		typeFlagged: []string{"status"},
	}
	cs, _ := connectStubTools(t, bridge, Config{Enabled: true, WriteEnabled: true})

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "set_page_type",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "M", "type": "meeting",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("flagged-values is a success, not an error: %s", toolText(t, res))
	}
	body := structuredBody(t, res)
	if body["ok"] != true {
		t.Errorf("structured ok = %v, want true (flagged is not an error)", body["ok"])
	}
	flagged, _ := body["flagged"].([]any)
	if len(flagged) != 1 || flagged[0] != "status" {
		t.Errorf("flagged = %v, want [status]", flagged)
	}
}

func TestTool_SetPageType_EmptyTypeClears(t *testing.T) {
	bridge := &stubBridge{fakeBridge: &fakeBridge{path: t.TempDir()}}
	cs, _ := connectStubTools(t, bridge, Config{Enabled: true, WriteEnabled: true})

	// Empty type string is the clear sentinel — must reach the bridge verbatim,
	// not be filtered out by the tool layer.
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "set_page_type",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "M", "type": "",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("isError: %s", toolText(t, res))
	}
	if bridge.gotType.Type != "" {
		t.Fatalf("expected empty type forwarded, got %q", bridge.gotType.Type)
	}
}

func TestTool_SetPageType_BridgeErrorBecomesStructuredError(t *testing.T) {
	bridge := &stubBridge{
		fakeBridge: &fakeBridge{path: t.TempDir()},
		typeErr:    types.ValidationError{Field: "*", Message: `unknown type "no-such"`},
	}
	cs, _ := connectStubTools(t, bridge, Config{Enabled: true, WriteEnabled: true})

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "set_page_type",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "M", "type": "no-such",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected isError for unknown type")
	}
	body := structuredBody(t, res)
	if body["ok"] != false {
		t.Errorf("structured ok = %v, want false", body["ok"])
	}
	errs, _ := body["errors"].([]any)
	first, _ := errs[0].(map[string]any)
	// set_page_type targets the type field as a whole, so the structured
	// property is the "*" sentinel rather than a named property.
	if first["property"] != "*" {
		t.Errorf("error property = %v, want *", first["property"])
	}
	if !strings.Contains(first["message"].(string), "unknown type") {
		t.Errorf("error message = %v, want it to mention unknown type", first["message"])
	}
}

// TestTool_SetPageProperty_IOErrorIsPlainToolErr pins the classification fix:
// a NON-validation bridge error (page missing, vault not loaded, disk/DB
// failure) must surface as a plain toolErr (text-only), NOT a structured
// {ok:false, errors:[...]} body — otherwise a never-attempted write would
// masquerade as a value rejection. The tool-untouched guarantee only holds for
// real ValidationErrors; everything else is an IO/transient failure.
func TestTool_SetPageProperty_IOErrorIsPlainToolErr(t *testing.T) {
	bridge := &stubBridge{
		fakeBridge: &fakeBridge{path: t.TempDir()},
		propErr:    errors.New("page file not found: /x/M.md"),
	}
	cs, aud := connectStubTools(t, bridge, Config{Enabled: true, WriteEnabled: true})

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "set_page_property",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "M",
			"property": "rating", "value": "4",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected isError for IO failure")
	}
	// Plain toolErr carries no StructuredContent — that is what distinguishes a
	// transient/IO failure from a schema-validation rejection on the wire.
	if res.StructuredContent != nil {
		t.Errorf("IO error must not carry a structured validation body: %+v", res.StructuredContent)
	}
	if !strings.Contains(toolText(t, res), "page file not found") {
		t.Fatalf("expected IO error text verbatim, got %q", toolText(t, res))
	}
	// IO failures audit as "error", validation rejections as "rejected" — the
	// distinction lets operators tell client mistakes from server problems.
	if !auditHas(aud, "set_page_property", "error") {
		t.Fatalf("missing error audit: %+v", aud.Entries)
	}
	if auditHas(aud, "set_page_property", "rejected") {
		t.Fatalf("IO failure must not audit as rejected: %+v", aud.Entries)
	}
}

// TestTool_SetPageType_IOErrorIsPlainToolErr mirrors the property test for the
// type-assign path: a non-validation error stays a plain toolErr.
func TestTool_SetPageType_IOErrorIsPlainToolErr(t *testing.T) {
	bridge := &stubBridge{
		fakeBridge: &fakeBridge{path: t.TempDir()},
		typeErr:    errors.New("vault not loaded"),
	}
	cs, aud := connectStubTools(t, bridge, Config{Enabled: true, WriteEnabled: true})

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "set_page_type",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "M", "type": "meeting",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected isError for IO failure")
	}
	if res.StructuredContent != nil {
		t.Errorf("IO error must not carry a structured validation body: %+v", res.StructuredContent)
	}
	if !strings.Contains(toolText(t, res), "vault not loaded") {
		t.Fatalf("expected IO error text verbatim, got %q", toolText(t, res))
	}
	if !auditHas(aud, "set_page_type", "error") {
		t.Fatalf("missing error audit: %+v", aud.Entries)
	}
	if auditHas(aud, "set_page_type", "rejected") {
		t.Fatalf("IO failure must not audit as rejected: %+v", aud.Entries)
	}
}

// structuredBody extracts the StructuredContent map from a CallToolResult.
// Both success and validation-error results populate StructuredContent, so the
// tool tests branch on its shape rather than parsing the TextContent body.
func structuredBody(t *testing.T, res *mcpsdk.CallToolResult) map[string]any {
	t.Helper()
	if res == nil || res.StructuredContent == nil {
		t.Fatalf("no StructuredContent on result: %+v", res)
	}
	// The SDK marshals StructuredContent to JSON on the way out and back on
	// the way in, so the client-side type is json.RawMessage -> decoded map.
	switch v := res.StructuredContent.(type) {
	case map[string]any:
		return v
	case json.RawMessage:
		var m map[string]any
		if err := json.Unmarshal(v, &m); err != nil {
			t.Fatalf("unmarshal StructuredContent: %v", err)
		}
		return m
	}
	t.Fatalf("unexpected StructuredContent type %T: %+v", res.StructuredContent, res.StructuredContent)
	return nil
}

func auditHas(aud *MemoryAuditor, tool, outcome string) bool {
	for _, e := range aud.Entries {
		if e.Tool == tool && e.Outcome == outcome {
			return true
		}
	}
	return false
}
