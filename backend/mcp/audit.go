package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Size-cap mirrors plugin network/AI audit logs (1 MB, keep last N lines).
const (
	maxMCPAuditLogBytes = 1 * 1024 * 1024
	maxMCPAuditLogLines = 500
)

// AuditEntry is one MCP tool invocation. Args are redacted (keys + shapes only;
// never note bodies or full text).
type AuditEntry struct {
	TS       string         `json:"ts"`
	Client   string         `json:"client,omitempty"`
	Tool     string         `json:"tool"`
	Vault    string         `json:"vault"`   // path hash, not full path
	Outcome  string         `json:"outcome"` // "ok" | "error" | "denied" | "rejected" | "rejected_schema"
	Error    string         `json:"error,omitempty"`
	ArgsMeta map[string]any `json:"args,omitempty"`
}

// Auditor writes MCP audit lines. Production uses fileAuditor under
// <vault>/.system/logs/mcp-audit.jsonl; tests use MemoryAuditor.
type Auditor interface {
	Record(e AuditEntry)
	Close()
}

// MemoryAuditor stores entries in memory (tests).
type MemoryAuditor struct {
	mu      sync.Mutex
	Entries []AuditEntry
}

func (m *MemoryAuditor) Record(e AuditEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Entries = append(m.Entries, e)
}

func (m *MemoryAuditor) Close() {}

// fileAuditor appends JSON lines under the vault log dir.
type fileAuditor struct {
	mu   sync.Mutex
	path string
	f    *os.File
}

func newFileAuditor(vaultPath string) (*fileAuditor, error) {
	dir := filepath.Join(vaultPath, ".system", "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "mcp-audit.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &fileAuditor{path: path, f: f}, nil
}

func (a *fileAuditor) Record(e AuditEntry) {
	if e.TS == "" {
		e.TS = time.Now().UTC().Format(time.RFC3339Nano)
	}
	b, err := json.Marshal(e)
	if err != nil {
		log.Printf("mcp audit: marshal: %v", err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.f == nil {
		return
	}
	// Tail-rotate when the log exceeds the size cap (same class as plugin audit).
	// Known limitation: rotation is a non-atomic close→truncate→rewrite under
	// a.mu, so a crash between Close and reopen forfeits the in-flight entry
	// (and the tail being rewritten). Acceptable: this log is best-effort
	// observability, not a durability-critical store, so we intentionally do
	// not run a background rotator or fsync per entry.
	if info, err := a.f.Stat(); err == nil && info.Size() > maxMCPAuditLogBytes {
		_ = a.f.Close()
		a.f = nil
		truncateMCPAuditLog(a.path, maxMCPAuditLogLines)
		f, openErr := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if openErr != nil {
			log.Printf("mcp audit: reopen after rotate: %v", openErr)
			return
		}
		a.f = f
	}
	if _, err := a.f.Write(append(b, '\n')); err != nil {
		log.Printf("mcp audit: write: %v", err)
	}
}

func (a *fileAuditor) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.f != nil {
		_ = a.f.Close()
		a.f = nil
	}
}

// truncateMCPAuditLog keeps the last keepLines of path (best-effort).
func truncateMCPAuditLog(path string, keepLines int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) <= keepLines {
		return
	}
	kept := lines[len(lines)-keepLines:]
	_ = os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0o600)
}

// VaultPathHash returns a short stable hash of the vault path for audit rows
// (never log the full path in shared logs if avoidable).
func VaultPathHash(vaultPath string) string {
	if vaultPath == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(vaultPath))
	return hex.EncodeToString(sum[:8])
}

// RedactArgs builds a metadata map from tool args: string lengths, counts,
// and non-content identifiers (notebook/section/page/ids). Never includes
// free-text body fields.
func RedactArgs(args map[string]any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		switch k {
		case "text", "content", "markdown", "body", "query":
			if s, ok := v.(string); ok {
				out[k+"_len"] = len([]rune(s))
			} else {
				out[k+"_present"] = true
			}
		case "blocks":
			switch b := v.(type) {
			case []any:
				out["blocks_count"] = len(b)
			default:
				out["blocks_present"] = true
			}
		case "block_ids":
			// Non-secret UUIDs for forensic correlation with read_page.
			switch ids := v.(type) {
			case []string:
				out["block_ids"] = ids
				out["block_ids_count"] = len(ids)
			case []any:
				clean := make([]string, 0, len(ids))
				for _, id := range ids {
					if s, ok := id.(string); ok && s != "" {
						clean = append(clean, s)
					}
				}
				out["block_ids"] = clean
				out["block_ids_count"] = len(clean)
			default:
				out["block_ids_present"] = true
			}
		default:
			// Safe identifiers and numbers only.
			switch v.(type) {
			case string, float64, int, int64, bool, nil:
				out[k] = v
			default:
				out[k+"_type"] = "complex"
			}
		}
	}
	return out
}

// schemaIDKeys are the structural identifier keys whose string value is safe to
// persist verbatim in a rejected_schema audit row. They are short, non-content
// identifiers (vault path and property/type names) — the same keys handler-level
// audit treats as safe. A non-string value in one of these slots is reduced to
// a type marker so a wrong-typed value is never logged.
var schemaIDKeys = map[string]bool{
	"notebook": true,
	"section":  true,
	"page":     true,
	"property": true,
	"type":     true,
}

// schemaKnownKeys are the remaining keys declared by a tool input schema. Their
// names are safe to record (schema-declared, not client content) but their
// values are content-shaped, so only a length/count/presence marker is kept.
// Any key outside both schemaIDKeys and schemaKnownKeys is client-supplied
// (unknown) and aggregated without its name, so a malicious key cannot smuggle
// content into the audit log as a metadata key.
var schemaKnownKeys = map[string]bool{
	"query":    true,
	"offset":   true,
	"limit":    true,
	"tag":      true,
	"date":     true,
	"markdown": true,
	"blocks":   true,
	"value":    true,
}

// redactSchemaArgs decodes raw wire arguments from a rejected tools/call into a
// strictly-allowlisted metadata map. Only schemaIDKeys string values are
// persisted; every other value (notably `value`, body text, numbers, and
// unknown client-supplied keys) is reduced to a shape marker — presence,
// length, or count — never the content. This is stricter than RedactArgs
// because the input is untrusted client data the SDK rejected, not a
// handler-built map of known fields. Malformed JSON yields presence + byte
// length (never the raw bytes).
func redactSchemaArgs(raw json.RawMessage) map[string]any {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return map[string]any{"args_present": true, "args_bytes": len(raw)}
	}
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m)+2)
	var unknownStrings, unknownOthers int
	for k, v := range m {
		switch {
		case schemaIDKeys[k]:
			if s, ok := v.(string); ok {
				out[k] = s
			} else if v != nil {
				out[k+"_type"] = jsonTypeName(v)
			}
		case schemaKnownKeys[k]:
			applyShapeMarker(out, k, v)
		default:
			// Unknown client-supplied key: aggregate without the name.
			if _, ok := v.(string); ok {
				unknownStrings++
			} else if v != nil {
				unknownOthers++
			}
		}
	}
	if unknownStrings > 0 {
		out["unknown_string_args"] = unknownStrings
	}
	if unknownOthers > 0 {
		out["unknown_other_args"] = unknownOthers
	}
	return out
}

// applyShapeMarker records a length/count/presence marker for k without its
// value. Used for schemaKnownKeys (content-shaped but schema-declared).
func applyShapeMarker(out map[string]any, k string, v any) {
	switch v.(type) {
	case string:
		out[k+"_len"] = len([]rune(v.(string)))
	case []any:
		out[k+"_count"] = len(v.([]any))
	case map[string]any:
		out[k+"_present"] = true
	case nil:
		// omit null
	case float64, int, int64, bool:
		out[k+"_present"] = true
	default:
		out[k+"_type"] = jsonTypeName(v)
	}
}

// jsonTypeName returns the JSON-schema type name for a decoded value.
func jsonTypeName(v any) string {
	switch v.(type) {
	case bool:
		return "boolean"
	case float64, int, int64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case nil:
		return "null"
	default:
		return "unknown"
	}
}

// sanitizeSchemaErr strips input-derived content from an SDK schema-validation
// error before it is persisted. The SDK echoes the offending VALUE in
// type-mismatch messages (`type: 99 has type "integer"`) and client-supplied
// KEY names in additional-property messages; both are input-derived and must
// not be logged. Schema-derived parts (the "validating arguments" prefix,
// property paths, declared type names, and missing-property names) are kept
// because they are not client content and carry the forensic reason for the
// rejection. A length cap bounds any unforeseen echo in future SDK versions.
var (
	schemaErrValueEcho = regexp.MustCompile(`type: .* has type `)
	schemaErrAddlProps = regexp.MustCompile(`unexpected additional properties \[[^\]]*\]`)
)

const maxSchemaErrLen = 200

func sanitizeSchemaErr(s string) string {
	// Greedy match from "type:" to the last " has type " on the line: redacts
	// the echoed value even if the value itself contains " has type ".
	s = schemaErrValueEcho.ReplaceAllString(s, `type: <redacted> has type `)
	// Drop client-supplied additional-property key names; the args meta already
	// records their presence via the unknown_* aggregates.
	s = schemaErrAddlProps.ReplaceAllString(s, `unexpected additional properties (redacted)`)
	if r := []rune(s); len(r) > maxSchemaErrLen {
		s = string(r[:maxSchemaErrLen]) + "..."
	}
	return s
}
