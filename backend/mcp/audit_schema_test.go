package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"silt/backend/types"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// redactIsSafe reports an error if raw appears anywhere in the redacted meta
// or error text — the invariant for input-derived content on the rejected_schema
// path.
func assertNothingLeaks(t *testing.T, aud *MemoryAuditor, raw ...string) {
	t.Helper()
	for _, e := range aud.Entries {
		if e.Outcome != OutcomeRejectedSchema {
			continue
		}
		metaJSON, _ := json.Marshal(e.ArgsMeta)
		blob := string(metaJSON) + " " + e.Error
		for _, needle := range raw {
			if strings.Contains(blob, needle) {
				t.Fatalf("input-derived %q leaked in rejected_schema row:\nerror=%q\nmeta=%s", needle, e.Error, metaJSON)
			}
		}
	}
}

// TestRedactSchemaArgs_Allowlist pins the allowlist redaction as a fast unit
// test (no transport): identifier string values persist; everything else —
// `value`, body fields, numbers, unknown client keys — is reduced to a shape
// marker, and unknown keys are aggregated without their names.
func TestRedactSchemaArgs_Allowlist(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		want  map[string]any
		leaks []string
	}{
		{
			name: "identifiers persisted as strings",
			raw:  `{"notebook":"Work","section":"A/B","page":"Home","property":"status","type":"meeting"}`,
			want: map[string]any{
				"notebook": "Work", "section": "A/B", "page": "Home",
				"property": "status", "type": "meeting",
			},
		},
		{
			name:  "property value content redacted to length",
			raw:   `{"notebook":"N","section":"","page":"P","property":"status","value":"top-secret-value"}`,
			want:  map[string]any{"notebook": "N", "section": "", "page": "P", "property": "status", "value_len": 16},
			leaks: []string{"top-secret-value"},
		},
		{
			name:  "property value wrong-type records presence only",
			raw:   `{"notebook":"N","section":"","page":"P","property":"status","value":99}`,
			want:  map[string]any{"notebook": "N", "section": "", "page": "P", "property": "status", "value_present": true},
			leaks: []string{"99"},
		},
		{
			name:  "unknown string key aggregated without name or value",
			raw:   `{"notebook":"N","mystery":"leak-me-unknown"}`,
			want:  map[string]any{"notebook": "N", "unknown_string_args": 1},
			leaks: []string{"mystery", "leak-me-unknown"},
		},
		{
			name:  "unknown non-string key aggregated",
			raw:   `{"notebook":"N","weird":[1,2,3],"more":{"x":1}}`,
			want:  map[string]any{"notebook": "N", "unknown_other_args": 2},
			leaks: []string{"weird", "more"},
		},
		{
			name:  "wrong-typed identifier records type not value",
			raw:   `{"notebook":123,"page":"P"}`,
			want:  map[string]any{"notebook_type": "number", "page": "P"},
			leaks: []string{"123"},
		},
		{
			name:  "blocks counted, nested body never reached",
			raw:   `{"notebook":"N","section":"","page":"P","blocks":[{"type":"NOTE","text":"nested-secret"}]}`,
			want:  map[string]any{"notebook": "N", "section": "", "page": "P", "blocks_count": 1},
			leaks: []string{"nested-secret"},
		},
		{
			name:  "query body field redacted to length",
			raw:   `{"query":"free-text-search-secret"}`,
			want:  map[string]any{"query_len": 23},
			leaks: []string{"free-text-search-secret"},
		},
		{
			// heading is free-form: must match RedactArgs 120-rune cap on the
			// rejected_schema path (schemaIDKeys alone would store it verbatim).
			name: "long heading capped like handler path",
			raw:  `{"notebook":"N","page":"P","heading":"` + strings.Repeat("H", 200) + `"}`,
			want: map[string]any{
				"notebook": "N", "page": "P",
				"heading":     strings.Repeat("H", maxHeadingAuditRunes) + "…",
				"heading_len": 200,
			},
			// Full 200-H run must not appear; truncated prefix is intentional.
			leaks: []string{strings.Repeat("H", 200)},
		},
		{
			name: "short heading persisted verbatim",
			raw:  `{"heading":"Meeting::Notes"}`,
			want: map[string]any{"heading": "Meeting::Notes"},
		},
		{
			name:  "malformed JSON records presence + bytes only",
			raw:   `{not valid json`,
			want:  map[string]any{"args_present": true, "args_bytes": 15},
			leaks: []string{"not valid json"},
		},
		{
			name: "empty and null yield nil",
			raw:  `null`,
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactSchemaArgs(json.RawMessage(tc.raw))
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(tc.want)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("redactSchemaArgs(%s)\n got %s\nwant %s", tc.raw, gotJSON, wantJSON)
			}
			for _, leak := range tc.leaks {
				if strings.Contains(string(gotJSON), leak) {
					t.Fatalf("leaked %q in %s", leak, gotJSON)
				}
			}
		})
	}
}

// TestSanitizeSchemaErr_StripsInputContent pins the error-text sanitizer: the
// SDK echoes offending values and unknown key names — both must be stripped,
// while schema-derived parts (property paths, declared types, missing-property
// names) are preserved.
func TestSanitizeSchemaErr_StripsInputContent(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		want  string
		leaks []string
	}{
		{
			name:  "type-mismatch value echoed is redacted",
			in:    `validating "arguments": validating root: validating /properties/value: type: 99 has type "integer", want "string"`,
			want:  `validating "arguments": validating root: validating /properties/value: type: <redacted> has type "integer", want "string"`,
			leaks: []string{"99"},
		},
		{
			name:  "string value echoed is redacted",
			in:    `validating /properties/blocks: type: not-an-array has type "string", want one of "null, array"`,
			want:  `validating /properties/blocks: type: <redacted> has type "string", want one of "null, array"`,
			leaks: []string{"not-an-array"},
		},
		{
			name:  "value containing the delimiter is fully redacted",
			in:    `validating /properties/value: type: x has type y has type "string", want "string"`,
			want:  `validating /properties/value: type: <redacted> has type "string", want "string"`,
			leaks: []string{"x has type y"},
		},
		{
			name:  "additional property key names redacted",
			in:    `validating "arguments": validating root: unexpected additional properties ["mystery"]`,
			want:  `validating "arguments": validating root: unexpected additional properties (redacted)`,
			leaks: []string{"mystery"},
		},
		{
			name: "missing-property names (schema-declared) preserved",
			in:   `validating "arguments": validating root: required: missing properties: ["query"]`,
			want: `validating "arguments": validating root: required: missing properties: ["query"]`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeSchemaErr(tc.in)
			if got != tc.want {
				t.Fatalf("sanitizeSchemaErr(%q)\n got %q\nwant %q", tc.in, got, tc.want)
			}
			for _, leak := range tc.leaks {
				if strings.Contains(got, leak) {
					t.Fatalf("leaked %q in sanitized error: %q", leak, got)
				}
			}
		})
	}
}

// TestSanitizeSchemaErr_LengthCap bounds any unforeseen echo in future SDK
// versions: an oversized error is truncated rather than persisted whole.
func TestSanitizeSchemaErr_LengthCap(t *testing.T) {
	long := strings.Repeat("a", maxSchemaErrLen+50)
	got := sanitizeSchemaErr(long)
	if len([]rune(got)) > maxSchemaErrLen+3 {
		t.Fatalf("sanitized error not capped: len=%d", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected truncation marker, got: %q", got)
	}
}

// TestSchemaAudit_RejectDoesNotLeakPropertyValue is the end-to-end regression
// for the review finding: a set_page_property whose sibling `page` field has an
// invalid shape (number where string is expected) is rejected by the SDK while
// `value` carries content, and the rejected_schema audit row must not persist
// that content in EITHER the args meta or the sanitized error text. Driven
// through the real loopback HTTP transport so the full SDK input-processing +
// middleware stack runs.
func TestSchemaAudit_RejectDoesNotLeakPropertyValue(t *testing.T) {
	bridge := &stubBridge{
		fakeBridge: &fakeBridge{path: t.TempDir()},
		// Force a semantic ValidationError if the handler ever ran (it must not,
		// because the SDK rejects the wrong-typed sibling first). The sentinel
		// message proves non-execution without coupling to the bridge shape.
		propErr: types.ValidationError{Field: "value", Message: "SHOULD_NOT_AUDIT_THIS"},
	}
	cfg := Config{Enabled: true, HTTPEnabled: true, HTTPPort: freePort(t), WriteEnabled: true}
	_, aud, cs := startHTTPHost(t, bridge, cfg)

	const secret = "top-secret-property-value"
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "set_page_property",
		Arguments: map[string]any{
			"notebook": "Work",
			"section":  "",
			"page":     123, // wrong-typed sibling → SDK rejects before handler
			"property": "status",
			"value":    secret, // valid string content carried by the rejected call
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for invalid sibling shape")
	}
	if got := countOutcome(aud, "set_page_property", OutcomeRejectedSchema); got != 1 {
		t.Fatalf("rejected_schema count=%d want 1: %+v", got, aud.Entries)
	}
	// The handler must not have run, so no semantic-rejection audit either.
	if got := countOutcome(aud, "set_page_property", "rejected"); got != 0 {
		t.Fatalf("handler ran despite sibling-shape rejection: %+v", aud.Entries)
	}
	// The property value content and the sentinel must not appear anywhere in
	// the audit row (neither args meta nor the sanitized error text).
	assertNothingLeaks(t, aud, secret, "SHOULD_NOT_AUDIT_THIS", "123")
	// Structural identifiers that the call DID supply validly are preserved
	// (forensic traceability); the wrong-typed sibling records a type, not a
	// value; value records presence/length only.
	for _, e := range aud.Entries {
		if e.Tool != "set_page_property" || e.Outcome != OutcomeRejectedSchema {
			continue
		}
		if e.ArgsMeta["notebook"] != "Work" || e.ArgsMeta["property"] != "status" {
			t.Fatalf("expected valid identifier values preserved, got: %+v", e.ArgsMeta)
		}
		if e.ArgsMeta["page"] != nil {
			t.Fatalf("wrong-typed sibling page must not persist a value: %+v", e.ArgsMeta)
		}
		if e.ArgsMeta["page_type"] == nil {
			t.Fatalf("expected page_type marker for wrong-typed sibling: %+v", e.ArgsMeta)
		}
		if e.ArgsMeta["value"] != nil {
			t.Fatalf("value content must not be persisted verbatim: %+v", e.ArgsMeta)
		}
		if _, ok := e.ArgsMeta["value_len"]; !ok {
			t.Fatalf("expected value_len marker: %+v", e.ArgsMeta)
		}
	}
}

// TestSchemaAudit_UnknownArgsAggregated is the end-to-end regression for the
// "unknown string args" finding: client-supplied keys not declared by any tool
// schema must not have their names or values persisted.
func TestSchemaAudit_UnknownArgsAggregated(t *testing.T) {
	bridge := &fakeBridge{path: t.TempDir()}
	cfg := Config{Enabled: true, HTTPEnabled: true, HTTPPort: freePort(t)}
	_, aud, cs := startHTTPHost(t, bridge, cfg)

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "read_page",
		Arguments: map[string]any{
			"notebook": "N", "section": "", "page": "P",
			"password_hint": "should-not-leak", // unknown key with content-like name
		},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for unknown additional property")
	}
	if got := countOutcome(aud, "read_page", OutcomeRejectedSchema); got != 1 {
		t.Fatalf("rejected_schema count=%d want 1: %+v", got, aud.Entries)
	}
	// Neither the unknown key name nor its value may appear.
	assertNothingLeaks(t, aud, "password_hint", "should-not-leak")
	for _, e := range aud.Entries {
		if e.Tool != "read_page" || e.Outcome != OutcomeRejectedSchema {
			continue
		}
		if n, ok := e.ArgsMeta["unknown_string_args"].(int); !ok || n < 1 {
			t.Fatalf("expected unknown_string_args≥1, got: %+v", e.ArgsMeta)
		}
	}
}
