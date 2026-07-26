package main

// Plugin audit logs (network + AI), unified on the generic auditLog[T] engine
// in plugin_audit_core.go. This file holds the per-kind entry types, the two
// auditLog instances, the App-facing IPC methods, the per-kind entry
// construction (auditNetwork / auditAI / auditAIEvent), and the AI-specific
// redaction machinery that has no network analog. The background-writer drain,
// the on-disk I/O, and the seed/parse paths all live in the shared core.
//
// Thin package-level wrappers (appendNetworkAuditLine, startNetworkAuditWriter,
// …) and type aliases (networkAuditOp, aiAuditWriterState, …) preserve the
// pre-refactor symbol names so callers (app.go lifecycle wiring) and the drain
// regression tests compile unchanged.

import (
	"encoding/json"
	"strings"
	"time"

	"silt/backend/ai"
)

// =========================================================================
// Network audit (#115)
// =========================================================================

// NetworkAuditEntry is one row of the plugin network audit log.
type NetworkAuditEntry struct {
	Plugin string `json:"plugin"`
	Host   string `json:"host"`
	Status int    `json:"status"`
	Method string `json:"method"`
	At     string `json:"at"` // RFC3339
}

func (e NetworkAuditEntry) auditAt() string     { return e.At }
func (e NetworkAuditEntry) auditPlugin() string { return e.Plugin }

// networkAuditLog is the shared engine instance for network.log.
var networkAuditLog = newAuditLog[NetworkAuditEntry]("network", "network.log")

// GetNetworkAudit returns the in-memory plugin network audit log (#115).
func (a *App) GetNetworkAudit() ([]NetworkAuditEntry, error) {
	return networkAuditLog.snapshot(), nil
}

// ClearNetworkAudit empties the in-memory audit log AND truncates the on-disk
// per-plugin network.log files so a clear is durable across restarts (#157).
// See auditLog.clear for the writer-drain / cross-vault guarantees.
func (a *App) ClearNetworkAudit() error {
	return networkAuditLog.clear(a)
}

// auditNetwork appends a {plugin, host, status, time} row. The body is NEVER
// logged — only the host + status so a user can see what a plugin is doing
// without leaking sensitive request/response payloads.
func (a *App) auditNetwork(pluginID, method, rawURL string, status int) {
	host := rawURL
	// Best-effort host extraction without a full URL parse (the URL was already
	// validated as http/https above).
	if i := strings.Index(rawURL, "://"); i >= 0 {
		rest := rawURL[i+3:]
		// Include the path (up to but not including query string) so the
		// audit log distinguishes GET /health from DELETE /data/all.
		if j := strings.IndexAny(rest, "?#"); j >= 0 {
			rest = rest[:j]
		}
		host = rest
	}
	networkAuditLog.append(a, NetworkAuditEntry{
		Plugin: pluginID,
		Host:   host,
		Status: status,
		Method: method,
		At:     time.Now().Format(time.RFC3339),
	})
}

// --- network audit lifecycle wrappers (names kept for app.go + tests) ------

func startNetworkAuditWriter(vaultPath string)            { networkAuditLog.startWriter(vaultPath) }
func stopNetworkAuditWriter()                             { networkAuditLog.stopWriter() }
func currentNetworkAuditWriter() *networkAuditWriterState { return networkAuditLog.currentWriter() }
func seedNetworkAuditFromDisk(vaultPath string)           { networkAuditLog.seedFromDisk(vaultPath) }
func appendNetworkAuditLine(vaultPath string, e *NetworkAuditEntry) {
	networkAuditLog.appendLine(vaultPath, e)
}
func parseNetworkLogLine(line string) (NetworkAuditEntry, bool) {
	return networkAuditLog.parseLine(line)
}

// Type aliases preserve the pre-refactor op/writer symbol names so the drain
// regression tests (app_lifecycle_drain_test.go, app_plugins_v2_test.go) that
// reference them compile unchanged.
type (
	networkAuditOp          = auditOp[NetworkAuditEntry]
	networkAuditWriterState = auditWriter[NetworkAuditEntry]
)

// =========================================================================
// AI audit (#216)
// =========================================================================
//
// AI calls are proxied through the Go backend so the user can see what a plugin
// is doing without the plugin ever touching credentials. Mirrors the network
// audit's shape and guarantees: in-memory, capped, persisted to a per-plugin
// ai.log (one JSON object per line), NEVER logs message content or embedding
// vectors — only the plugin, the call kind, the model, the outcome status, and
// a token-usage summary.

// AIAuditEntry is one row of the plugin AI audit log. Content is NEVER recorded
// — only the endpoint host, model, outcome, and a token-usage summary. Kind may
// also be agent-side events (tool_call, staging_decision, …) with Detail holding
// redacted structured fields (#630).
type AIAuditEntry struct {
	Plugin string `json:"plugin"`
	Kind   string `json:"kind"`   // "chat" | "embed" | "tool_call" | "staging_decision" | …
	Host   string `json:"host"`   // provider host (no path/query), best-effort
	Model  string `json:"model"`  // model the call targeted
	Status string `json:"status"` // "ok" | normalized error kind | "error" | event outcome
	At     string `json:"at"`     // RFC3339
	// Usage summary (present only on success and when the provider returned it).
	PromptTokens     *int `json:"prompt_tokens,omitempty"`
	CompletionTokens *int `json:"completion_tokens,omitempty"`
	TotalTokens      *int `json:"total_tokens,omitempty"`
	// Detail is a redacted JSON object for structured agent events (#630).
	// Omitted for chat/embed rows.
	Detail json.RawMessage `json:"detail,omitempty"`
}

func (e AIAuditEntry) auditAt() string     { return e.At }
func (e AIAuditEntry) auditPlugin() string { return e.Plugin }

// aiAuditLog is the shared engine instance for ai.log.
var aiAuditLog = newAuditLog[AIAuditEntry]("ai", "ai.log")

// maxAIAuditDetailBytes caps serialized Detail JSON so a plugin cannot flood
// ai.log with huge tool argument dumps (#630).
const maxAIAuditDetailBytes = 2 * 1024

// allowedAIAuditDetailKeys is the closed set of structured agent/tool audit
// fields that may be persisted to ai.log (synced with the vault). Anything
// else — note, summary, details, freeform message bodies — is dropped so
// private vault text cannot ride an arbitrary eventJSON key (#630).
//
// INVARIANT: this set is closed against freeform-text keys BY DESIGN. It MUST
// never include keys whose values could carry user/authored text or paths
// (e.g. content, note, message, error, body, path, detail, args, query). Only
// short developer-shaped identifiers belong here; the redactor relies on this
// so a path or vault snippet embedded inside a longer string has nowhere to
// land. Add a key only if its value is a bounded scalar (id / label / count).
var allowedAIAuditDetailKeys = map[string]struct{}{
	"tool":         {},
	"tool_call_id": {},
	"status":       {},
	"outcome":      {},
	"staged":       {},
	"side":         {},
	"iteration":    {},
	"error_kind":   {},
	"plugin":       {},
	// Server-only backstop marker when detail JSON is oversized.
	"detail_truncated": {},
}

// redactAIAuditFields filters to the allowlisted metadata keys and coerces
// values to short safe scalars (no nested freeform blobs).
func redactAIAuditFields(fields map[string]any) map[string]any {
	if fields == nil {
		return nil
	}
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		lk := strings.ToLower(strings.TrimSpace(k))
		if _, ok := allowedAIAuditDetailKeys[lk]; !ok {
			continue
		}
		if safe, ok := coerceAIAuditScalar(v); ok {
			out[lk] = safe
		}
	}
	return out
}

// coerceAIAuditScalar keeps only bool / number / short non-path strings.
func coerceAIAuditScalar(v any) (any, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		if f, err := t.Float64(); err == nil {
			return f, true
		}
		return nil, false
	case string:
		s := t
		if looksLikeAbsolutePath(s) {
			return "[path]", true
		}
		if len(s) > 64 {
			s = s[:64] + "…"
		}
		return s, true
	default:
		return nil, false
	}
}

func looksLikeAbsolutePath(s string) bool {
	if len(s) < 3 {
		return false
	}
	// Windows drive path (C:\… or C:/…).
	if (s[0] >= 'A' && s[0] <= 'Z' || s[0] >= 'a' && s[0] <= 'z') && s[1] == ':' {
		return true
	}
	// UNC.
	if strings.HasPrefix(s, "\\\\") {
		return true
	}
	// POSIX absolute: any /-prefixed string with a path separator (covers
	// /home, /mnt, /media, /srv, /opt, /root, /data, …). Vault-relative
	// "notebook/section" has no leading slash and is left alone.
	if s[0] == '/' && strings.Contains(s[1:], "/") {
		return true
	}
	return false
}

// auditAIEvent appends a structured agent event (tool_call, staging_decision, …)
// with redacted fields to the AI audit log (#630).
func (a *App) auditAIEvent(pluginID, kind string, fields map[string]any) {
	redacted := redactAIAuditFields(fields)
	status := "ok"
	if s, ok := redacted["status"].(string); ok && s != "" {
		status = s
	} else if s, ok := redacted["outcome"].(string); ok && s != "" {
		status = s
	}
	var detail json.RawMessage
	if len(redacted) > 0 {
		raw, err := json.Marshal(redacted)
		if err == nil {
			// Byte-cap must leave valid JSON; mid-object truncation breaks the
			// frontend Recent AI activity parser. Per-field truncation usually
			// keeps us under the cap; this is a hard backstop only.
			if len(raw) > maxAIAuditDetailBytes {
				raw = []byte(`{"detail_truncated":true}`)
			}
			detail = raw
		}
	}
	aiAuditLog.append(a, AIAuditEntry{
		Plugin: pluginID,
		Kind:   kind,
		Status: status,
		At:     time.Now().Format(time.RFC3339),
		Detail: detail,
	})
}

// auditAI appends one AI audit entry. host is the provider endpoint (already
// validated as http/https upstream); only the host[:port]/path prefix is kept so
// query strings (which some providers use for routing) are not logged. status is
// "ok" on success or the normalized ai.AIErrorKind on failure; usage is the
// provider's token summary when available.
func (a *App) auditAI(pluginID, kind, host, model, status string, usage *ai.AIUsage) {
	// Trim to host + path (drop query/fragment) so a routing query string is
	// not recorded. Best-effort, mirroring auditNetwork's host extraction.
	h := host
	if i := strings.Index(host, "://"); i >= 0 {
		rest := host[i+3:]
		if j := strings.IndexAny(rest, "?#"); j >= 0 {
			rest = rest[:j]
		}
		h = rest
	}
	entry := AIAuditEntry{
		Plugin: pluginID,
		Kind:   kind,
		Host:   h,
		Model:  model,
		Status: status,
		At:     time.Now().Format(time.RFC3339),
	}
	if usage != nil {
		entry.PromptTokens = usage.PromptTokens
		entry.CompletionTokens = usage.CompletionTokens
		entry.TotalTokens = usage.TotalTokens
	}
	aiAuditLog.append(a, entry)
}

// GetAIAudit returns a copy of the in-memory AI audit log (#216).
func (a *App) GetAIAudit() ([]AIAuditEntry, error) {
	return aiAuditLog.snapshot(), nil
}

// ClearAIAudit empties the in-memory AI audit log AND truncates the on-disk
// per-plugin ai.log files so a clear is durable across restarts (#446, mirrors
// ClearNetworkAudit's #157 contract for network.log). See auditLog.clear for
// the writer-drain / cross-vault guarantees.
func (a *App) ClearAIAudit() error {
	return aiAuditLog.clear(a)
}

// --- AI audit lifecycle wrappers (names kept for app.go + tests) -----------

func startAIAuditWriter(vaultPath string)                 { aiAuditLog.startWriter(vaultPath) }
func stopAIAuditWriter()                                  { aiAuditLog.stopWriter() }
func currentAIAuditWriter() *aiAuditWriterState           { return aiAuditLog.currentWriter() }
func seedAIAuditFromDisk(vaultPath string)                { aiAuditLog.seedFromDisk(vaultPath) }
func appendAIAuditLine(vaultPath string, e *AIAuditEntry) { aiAuditLog.appendLine(vaultPath, e) }

// Type aliases preserve the pre-refactor AI op/writer symbol names.
type (
	aiAuditOp          = auditOp[AIAuditEntry]
	aiAuditWriterState = auditWriter[AIAuditEntry]
)
