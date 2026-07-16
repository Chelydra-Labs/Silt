package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"silt/backend/ai"
	"sort"
	"strings"
	"sync"
	"time"
)

// maxPluginAuditLogBytes bounds a single per-plugin on-disk audit log
// (network.log / ai.log) so it cannot grow unbounded across a long session.
// When exceeded, the log is truncated to the most recent
// maxPluginAuditLogLines.
const (
	maxPluginAuditLogBytes = 1 * 1024 * 1024 // 1 MB
	maxPluginAuditLogLines = 200             // keep-lines on truncation
)

// networkAuditMu guards the in-memory network audit log. The log is a simple
// append-only slice of {plugin, host, status, time} entries, surfaced in
// Settings → Plugins so a user can see what a networked plugin is doing (#115).
var (
	networkAuditMu sync.Mutex
	networkAudit   []NetworkAuditEntry
)

// NetworkAuditEntry is one row of the plugin network audit log.
type NetworkAuditEntry struct {
	Plugin string `json:"plugin"`
	Host   string `json:"host"`
	Status int    `json:"status"`
	Method string `json:"method"`
	At     string `json:"at"` // RFC3339
}

// truncateNetworkLog reads the log file, keeps the last n lines, and rewrites
// it. Best-effort — errors are silently ignored (the audit log is not a
// security boundary, just a diagnostic aid).
func truncateNetworkLog(path string, keepLines int) {
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

// appendNetworkAuditLine writes one entry to the per-plugin on-disk log file.
// Extracted from auditNetwork's pre-#235 inline path; the I/O is identical.
// Best-effort — errors are logged, never surfaced (the audit log is
// diagnostic).
//
// Concurrency: NOT goroutine-safe — callers must serialize. In production the
// background writer goroutine (networkAuditWriterState.process) is the sole
// caller; in the inline fallback path (tests, pre-init) auditNetwork holds
// networkAuditMu. Mirrors the pre-#235 contract.
//
// #254: the on-disk format is a single-line JSON object per entry (one
// json.Marshal + trailing newline). JSON is self-describing, survives column
// re-ordering, and is parseable by standard tooling (jq, SIEM ingest).
func appendNetworkAuditLine(vaultPath string, entry *NetworkAuditEntry) {
	if vaultPath == "" {
		return
	}
	logPath := filepath.Join(vaultPath, ".system", "plugins", entry.Plugin, "network.log")
	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("appendNetworkAuditLine: json.Marshal failed: %v", err)
		return
	}
	_ = os.MkdirAll(filepath.Dir(logPath), 0o700)
	if info, err := os.Stat(logPath); err == nil && info.Size() > maxPluginAuditLogBytes {
		truncateNetworkLog(logPath, maxPluginAuditLogLines)
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		_, _ = f.Write(append(data, '\n'))
		_ = f.Close()
	}
}

// clearNetworkAuditFiles empties every per-plugin on-disk network.log under
// the vault's .system/plugins/ tree. Extracted from ClearNetworkAudit's
// pre-#235 inline path; best-effort (errors silently ignored).
func clearNetworkAuditFiles(vaultPath string) {
	if vaultPath == "" {
		return
	}
	pluginsDir := filepath.Join(vaultPath, ".system", "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		logPath := filepath.Join(pluginsDir, e.Name(), "network.log")
		if _, err := os.Stat(logPath); err == nil {
			_ = os.WriteFile(logPath, []byte{}, 0o600)
		}
	}
}

// GetNetworkAudit returns the in-memory plugin network audit log (#115).
func (a *App) GetNetworkAudit() ([]NetworkAuditEntry, error) {
	networkAuditMu.Lock()
	defer networkAuditMu.Unlock()
	out := make([]NetworkAuditEntry, len(networkAudit))
	copy(out, networkAudit)
	return out, nil
}

// ClearNetworkAudit empties the in-memory audit log AND truncates the on-disk
// per-plugin network.log files so a clear is durable across restarts (#157).
// When the background writer is running, the on-disk truncation is enqueued
// and processed in FIFO order with concurrent auditNetwork appends so a
// fetch that fires after the clear click cannot interleave a line into a file
// we just emptied. When the writer is not running (tests, pre-initialize),
// the truncation runs inline — the pre-#235 behavior.
func (a *App) ClearNetworkAudit() error {
	// Snapshot vaultPath under vaultMu BEFORE acquiring networkAuditMu so the
	// w==nil inline path never reads a.vaultPath unsynchronized (a concurrent
	// SwitchVault/ImportVault may flip it). The writer branches use w.vaultPath
	// (the vault the writer was created for) instead — see the <-w.stop branch.
	a.vaultMu.RLock()
	vaultPathSnapshot := a.vaultPath
	a.vaultMu.RUnlock()
	networkAuditMu.Lock()
	networkAudit = nil
	w := currentNetworkAuditWriter()
	if w == nil {
		// Writer not running: truncate inline (pre-#235 path).
		clearNetworkAuditFiles(vaultPathSnapshot)
		networkAuditMu.Unlock()
		return nil
	}
	// The clear op MUST be enqueued while holding networkAuditMu so it is
	// ordered relative to concurrent auditNetwork appends — both producers
	// send on w.ch under this lock, so without it a concurrent append could
	// enqueue AFTER our networkAudit = nil but BEFORE our clear op, and the
	// writer would append-then-truncate (deleting a post-clear entry from
	// disk — a #157 restart-persistence regression).
	//
	// The send is NON-BLOCKING. A blocking send here can deadlock:
	// currentNetworkAuditWriter may have captured a pointer to a writer whose
	// goroutine exits before the send completes (stopNetworkAuditWriter nils
	// the global pointer, closes w.stop, the writer drains its queue and
	// exits). Because the channel is buffered (256 slots), the send would
	// STILL succeed after the goroutine is gone — and then <-op.done would
	// block forever with no goroutine to process it (#451).
	op := &networkAuditOp{clear: true, done: make(chan struct{})}
	select {
	case w.ch <- op:
		networkAuditMu.Unlock()
		// Wait for the writer to process the clear, but ALSO watch w.stop. If
		// the writer exits before processing the op (or if the select
		// spuriously picks w.stop when both are ready), we must still guarantee
		// the truncation runs against the WRITER's vault — never a.vaultPath,
		// which a concurrent SwitchVault may have already moved to a different
		// vault (#452 cross-vault data-loss regression). Waiting on w.done
		// ensures the writer's drain completes first (our op is in the buffer
		// and the drain processes every buffered op before returning).
		select {
		case <-op.done:
		case <-w.stop:
			<-w.done
			clearNetworkAuditFiles(w.vaultPath)
		}
	default:
		// Queue full (astronomically rare). Truncate the WRITER's vault — not
		// a.vaultPath — so a concurrent switch can't redirect the truncation
		// to the new vault. Same FIFO trade-off as ClearAIAudit's default
		// branch (see comment there).
		clearNetworkAuditFiles(w.vaultPath)
		networkAuditMu.Unlock()
	}
	return nil
}

// seedNetworkAuditFromDisk reads every on-disk network.log file under the
// vault's .system/plugins/ tree and seeds the in-memory audit log so entries
// survive a restart (#157). Called once during initializeVaultServices. The
// on-disk format is one JSON object per line (#254). The in-memory log is
// capped at 500 entries (most recent).
func seedNetworkAuditFromDisk(vaultPath string) {
	if vaultPath == "" {
		return
	}
	pluginsDir := filepath.Join(vaultPath, ".system", "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return
	}
	var seeded []NetworkAuditEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		logPath := filepath.Join(pluginsDir, e.Name(), "network.log")
		data, err := os.ReadFile(logPath)
		if err != nil || len(data) == 0 {
			continue
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		for _, line := range lines {
			entry, ok := parseNetworkLogLine(line)
			if ok {
				seeded = append(seeded, entry)
			}
		}
	}
	// Sort by timestamp (oldest first) so we can trim to the last 500.
	sort.Slice(seeded, func(i, j int) bool {
		return seeded[i].At < seeded[j].At
	})
	if len(seeded) > 500 {
		seeded = seeded[len(seeded)-500:]
	}
	networkAuditMu.Lock()
	// Only seed if the in-memory log is empty (don't overwrite entries that
	// may have been added between vault open and this call).
	if len(networkAudit) == 0 {
		networkAudit = seeded
	}
	networkAuditMu.Unlock()
}

// parseNetworkLogLine parses one JSON-format line from a network.log file
// into a NetworkAuditEntry. Lines are single-line JSON objects (#254).
// Returns ok=false on any parse failure (best-effort).
func parseNetworkLogLine(line string) (NetworkAuditEntry, bool) {
	var entry NetworkAuditEntry
	if err := json.Unmarshal([]byte(line), &entry); err == nil && entry.At != "" {
		return entry, true
	}
	return NetworkAuditEntry{}, false
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
	entry := NetworkAuditEntry{
		Plugin: pluginID,
		Host:   host,
		Status: status,
		Method: method,
		At:     time.Now().Format(time.RFC3339),
	}
	networkAuditMu.Lock()
	networkAudit = append(networkAudit, entry)
	// Bound the in-memory log to the last 500 entries so it does not grow
	// unbounded.
	if len(networkAudit) > 500 {
		networkAudit = networkAudit[len(networkAudit)-500:]
	}
	// Decouple the on-disk write from the lock: enqueue onto the background
	// writer's channel (non-blocking — the 256-slot buffer handles burst rates
	// far beyond any plugin's allotment). If the writer is not running, fall
	// back to inline I/O so behavior is identical for tests that don't start
	// the writer (#235).
	w := currentNetworkAuditWriter()
	if w != nil {
		select {
		case w.ch <- &networkAuditOp{entry: &entry}:
		default:
			log.Printf("auditNetwork: writer queue full; dropping on-disk write for plugin %q", pluginID)
		}
	} else {
		appendNetworkAuditLine(a.vaultPath, &entry)
	}
	networkAuditMu.Unlock()
}

// --- Background audit-log writer (#235) ----------------------------------
//
// The writer drains on-disk audit writes off the networkAuditMu lock so
// concurrent PluginFetch calls don't serialize on per-plugin file I/O. A
// single goroutine processes the channel in FIFO order, preserving the
// "no interleaved line in a file we just emptied" invariant ClearNetworkAudit
// relies on.

// networkAuditOp is one operation queued to the background writer.
type networkAuditOp struct {
	entry *NetworkAuditEntry // non-nil = append to on-disk log
	clear bool               // true = truncate on-disk logs
	done  chan struct{}      // optional: closed when this op is fully processed
}

// networkAuditWriterState is the background writer's mutable state. It is
// non-nil while the writer goroutine is running.
type networkAuditWriterState struct {
	ch        chan *networkAuditOp
	stop      chan struct{}
	done      chan struct{} // closed when the goroutine has exited
	vaultPath string
}

var (
	networkAuditWriterMu sync.Mutex // guards networkAuditWriter
	networkAuditWriter   *networkAuditWriterState
)

// currentNetworkAuditWriter returns the active writer state, or nil if the
// writer is not running. Thread-safe; callers may use the returned pointer
// without holding networkAuditWriterMu (the state struct is never mutated
// after creation; only the package-level pointer is swapped).
func currentNetworkAuditWriter() *networkAuditWriterState {
	networkAuditWriterMu.Lock()
	defer networkAuditWriterMu.Unlock()
	return networkAuditWriter
}

// startNetworkAuditWriter launches the background audit-log writer goroutine
// for the given vault. Idempotent — a second call while the writer is running
// is a no-op. The writer drains on-disk audit writes off the networkAuditMu
// lock so concurrent PluginFetch calls don't serialize on file I/O (#235).
func startNetworkAuditWriter(vaultPath string) {
	networkAuditWriterMu.Lock()
	defer networkAuditWriterMu.Unlock()
	if networkAuditWriter != nil {
		return
	}
	w := &networkAuditWriterState{
		ch:        make(chan *networkAuditOp, 256),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
		vaultPath: vaultPath,
	}
	networkAuditWriter = w
	go w.run()
}

// stopNetworkAuditWriter signals the writer to drain remaining ops and exit,
// then blocks until the goroutine is done. Idempotent. Guarantees no queued
// entry is lost on vault close (#157 persistent-audit contract).
func stopNetworkAuditWriter() {
	networkAuditWriterMu.Lock()
	w := networkAuditWriter
	if w == nil {
		networkAuditWriterMu.Unlock()
		return
	}
	networkAuditWriter = nil
	networkAuditWriterMu.Unlock()
	close(w.stop)
	<-w.done
}

// run is the writer goroutine body. It processes ops in FIFO order until
// stop is closed, then drains every remaining queued op before exiting so
// no entry is lost on shutdown.
func (w *networkAuditWriterState) run() {
	defer close(w.done)
	for {
		select {
		case op := <-w.ch:
			w.process(op)
		case <-w.stop:
			// Drain every queued op before exiting so no entry is lost.
			for {
				select {
				case op := <-w.ch:
					w.process(op)
				default:
					return
				}
			}
		}
	}
}

// process handles one op. Entry appends to the per-plugin on-disk log; clear
// empties every on-disk log. If done is non-nil it is closed after the op
// completes so the caller (ClearNetworkAudit) can synchronize.
func (w *networkAuditWriterState) process(op *networkAuditOp) {
	if op.entry != nil {
		appendNetworkAuditLine(w.vaultPath, op.entry)
	}
	if op.clear {
		clearNetworkAuditFiles(w.vaultPath)
	}
	if op.done != nil {
		close(op.done)
	}
}

// =========================================================================
// AI audit (#216)
// =========================================================================
//
// AI calls are proxied through the Go backend for the same reason network
// fetches are: so the user can see what a plugin is doing without the plugin
// ever touching credentials. The AI audit mirrors the network audit's shape and
// guarantees: in-memory, capped at the last 500 entries, persisted to a
// per-plugin ai.log on disk (one JSON object per line, same format as
// network.log), and NEVER logs message content or embedding vectors — only the
// plugin, the call kind (chat/embed), the model, the outcome status, and a
// token-usage summary. Surfaced in Settings → Plugins alongside the network
// audit so a user audits AI traffic in the same place.

var (
	aiAuditMu sync.Mutex
	aiAudit   []AIAuditEntry
)

// maxAIAuditEntries bounds the in-memory AI audit log (mirrors the network
// audit's 500-entry cap).
const maxAIAuditEntries = 500

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
	entry := AIAuditEntry{
		Plugin: pluginID,
		Kind:   kind,
		Status: status,
		At:     time.Now().Format(time.RFC3339),
		Detail: detail,
	}
	a.appendAIAuditEntry(entry)
}

// appendAIAuditEntry is the shared in-memory + on-disk append path for chat,
// embed, and structured agent events.
func (a *App) appendAIAuditEntry(entry AIAuditEntry) {
	a.vaultMu.RLock()
	vaultPathSnapshot := a.vaultPath
	a.vaultMu.RUnlock()
	aiAuditMu.Lock()
	aiAudit = append(aiAudit, entry)
	if len(aiAudit) > maxAIAuditEntries {
		aiAudit = aiAudit[len(aiAudit)-maxAIAuditEntries:]
	}
	w := currentAIAuditWriter()
	if w != nil {
		select {
		case w.ch <- &aiAuditOp{entry: &entry}:
		default:
			log.Printf("auditAI: writer queue full; dropping on-disk write for plugin %q", entry.Plugin)
		}
	} else {
		appendAIAuditLine(vaultPathSnapshot, &entry)
	}
	aiAuditMu.Unlock()
}

// auditAI appends one AI audit entry. host is the provider endpoint (already
// validated as http/https upstream); only the host[:port]/path prefix is kept so
// query strings (which some providers use for routing) are not logged. status is
// "ok" on success or the normalized ai.AIErrorKind on failure; usage is the
// provider's token summary when available. The mutex is the only lock taken —
// this is safe to call with no vaultMu/configMu held (the HTTP call has already
// returned by the time we audit).
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
	// Snapshot + append via shared path (vaultMu → aiAuditMu ordering).
	a.appendAIAuditEntry(entry)
}

// GetAIAudit returns a copy of the in-memory AI audit log (#216).
func (a *App) GetAIAudit() ([]AIAuditEntry, error) {
	aiAuditMu.Lock()
	defer aiAuditMu.Unlock()
	out := make([]AIAuditEntry, len(aiAudit))
	copy(out, aiAudit)
	return out, nil
}

// ClearAIAudit empties the in-memory AI audit log AND truncates the on-disk
// per-plugin ai.log files so a clear is durable across restarts (#446, mirrors
// ClearNetworkAudit's #157 contract for network.log). When the background
// writer is running, the on-disk truncation is enqueued and processed in FIFO
// order with concurrent auditAI appends so a fetch that fires after the clear
// click cannot interleave a line into a file we just emptied. When the writer
// is not running (tests, pre-initialize), the truncation runs inline.
func (a *App) ClearAIAudit() error {
	// Snapshot vaultPath under vaultMu BEFORE acquiring aiAuditMu so the
	// w==nil inline path never reads a.vaultPath unsynchronized (a concurrent
	// SwitchVault/ImportVault may flip it). The writer branches use w.vaultPath
	// instead — see the <-w.stop branch.
	a.vaultMu.RLock()
	vaultPathSnapshot := a.vaultPath
	a.vaultMu.RUnlock()
	aiAuditMu.Lock()
	aiAudit = nil
	w := currentAIAuditWriter()
	if w == nil {
		// Writer not running: truncate inline.
		clearAIAuditFiles(vaultPathSnapshot)
		aiAuditMu.Unlock()
		return nil
	}
	// The clear op MUST be enqueued while holding aiAuditMu so it is ordered
	// relative to concurrent auditAI appends (FIFO clear-vs-append invariant
	// — see ClearNetworkAudit for the full rationale).
	//
	// The send is NON-BLOCKING to avoid a latent deadlock: a blocking send
	// can succeed on the buffered channel even after the writer goroutine has
	// exited (stopAIAuditWriter nils the global pointer, closes w.stop, the
	// writer drains and exits), leaving <-op.done blocked forever (#451).
	op := &aiAuditOp{clear: true, done: make(chan struct{})}
	select {
	case w.ch <- op:
		aiAuditMu.Unlock()
		// Wait for the writer to process the clear, but ALSO watch w.stop. If
		// the writer exits before processing the op (or the select picks
		// w.stop when both are ready), we must still guarantee the truncation
		// runs against the WRITER's vault — never a.vaultPath, which a
		// concurrent SwitchVault may have moved to a different vault (#452
		// cross-vault data-loss regression). Waiting on w.done ensures the
		// writer's drain completes first (our op is in the buffer and the
		// drain processes every buffered op before returning).
		select {
		case <-op.done:
		case <-w.stop:
			<-w.done
			clearAIAuditFiles(w.vaultPath)
		}
	default:
		// Queue full (256-slot saturation — astronomically rare: needs a burst
		// of 256+ AI calls with the writer stalled, e.g. slow disk). We
		// truncate the WRITER's vault inline — not a.vaultPath — so a
		// concurrent switch can't redirect the truncation to the new vault.
		//
		// KNOWN FIFO TRADE-OFF: the writer goroutine appends queued entries to
		// disk WITHOUT holding aiAuditMu, so an entry op already in the queue
		// when this truncation runs can be re-appended by the writer AFTER the
		// truncation, resurrecting a pre-clear line in the on-disk ai.log. The
		// in-memory log (cleared above under the lock) is always correct; only
		// the on-disk diagnostic file can carry a stale tail. Accepted because
		// the path is near-unreachable and the data is diagnostic.
		clearAIAuditFiles(w.vaultPath)
		aiAuditMu.Unlock()
	}
	return nil
}

// appendAIAuditLine writes one entry to the per-plugin on-disk ai.log file.
// Mirrors appendNetworkAuditLine; the I/O is identical. Best-effort — errors
// are logged, never surfaced (the audit log is diagnostic).
//
// Concurrency: NOT goroutine-safe — callers must serialize. In production the
// background writer goroutine (aiAuditWriterState.process) is the sole caller;
// in the inline fallback path (tests, pre-init) auditAI holds aiAuditMu.
//
// The on-disk format is a single-line JSON object per entry (one json.Marshal
// + trailing newline), matching network.log so the same tooling (jq, SIEM
// ingest) parses both. Size-capped via the path-parametric truncateNetworkLog
// (reused — there is no AI-specific truncate).
func appendAIAuditLine(vaultPath string, entry *AIAuditEntry) {
	if vaultPath == "" {
		return
	}
	logPath := filepath.Join(vaultPath, ".system", "plugins", entry.Plugin, "ai.log")
	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("appendAIAuditLine: json.Marshal failed: %v", err)
		return
	}
	_ = os.MkdirAll(filepath.Dir(logPath), 0o700)
	if info, err := os.Stat(logPath); err == nil && info.Size() > maxPluginAuditLogBytes {
		truncateNetworkLog(logPath, maxPluginAuditLogLines)
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		_, _ = f.Write(append(data, '\n'))
		_ = f.Close()
	}
}

// clearAIAuditFiles empties every per-plugin on-disk ai.log under the vault's
// .system/plugins/ tree. Mirrors clearNetworkAuditFiles; best-effort (errors
// silently ignored).
func clearAIAuditFiles(vaultPath string) {
	if vaultPath == "" {
		return
	}
	pluginsDir := filepath.Join(vaultPath, ".system", "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		logPath := filepath.Join(pluginsDir, e.Name(), "ai.log")
		if _, err := os.Stat(logPath); err == nil {
			_ = os.WriteFile(logPath, []byte{}, 0o600)
		}
	}
}

// parseAILogLine parses one JSON-format line from an ai.log file into an
// AIAuditEntry. Mirrors parseNetworkLogLine. Returns ok=false on any parse
// failure (best-effort).
func parseAILogLine(line string) (AIAuditEntry, bool) {
	var entry AIAuditEntry
	if err := json.Unmarshal([]byte(line), &entry); err == nil && entry.At != "" {
		return entry, true
	}
	return AIAuditEntry{}, false
}

// seedAIAuditFromDisk reads every on-disk ai.log file under the vault's
// .system/plugins/ tree and seeds the in-memory AI audit log so entries survive
// a restart (#446). Mirrors seedNetworkAuditFromDisk. Called once during
// initializeVaultServices. The on-disk format is one JSON object per line. The
// in-memory log is capped at 500 entries (most recent).
func seedAIAuditFromDisk(vaultPath string) {
	if vaultPath == "" {
		return
	}
	pluginsDir := filepath.Join(vaultPath, ".system", "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return
	}
	var seeded []AIAuditEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		logPath := filepath.Join(pluginsDir, e.Name(), "ai.log")
		data, err := os.ReadFile(logPath)
		if err != nil || len(data) == 0 {
			continue
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		for _, line := range lines {
			entry, ok := parseAILogLine(line)
			if ok {
				seeded = append(seeded, entry)
			}
		}
	}
	// Sort by timestamp (oldest first) so we can trim to the last 500.
	sort.Slice(seeded, func(i, j int) bool {
		return seeded[i].At < seeded[j].At
	})
	if len(seeded) > 500 {
		seeded = seeded[len(seeded)-500:]
	}
	aiAuditMu.Lock()
	// Only seed if the in-memory log is empty (don't overwrite entries that
	// may have been added between vault open and this call).
	if len(aiAudit) == 0 {
		aiAudit = seeded
	}
	aiAuditMu.Unlock()
}

// --- Background AI audit-log writer (#446) --------------------------------
//
// The writer drains on-disk AI audit writes off the aiAuditMu lock so
// concurrent auditAI calls don't serialize on per-plugin file I/O. A single
// goroutine processes the channel in FIFO order, preserving the "no
// interleaved line in a file we just emptied" invariant ClearAIAudit relies
// on. Mirrors the network writer (#235) exactly.

// aiAuditOp is one operation queued to the background writer.
type aiAuditOp struct {
	entry *AIAuditEntry // non-nil = append to on-disk log
	clear bool          // true = truncate on-disk logs
	done  chan struct{} // optional: closed when this op is fully processed
}

// aiAuditWriterState is the background writer's mutable state. It is non-nil
// while the writer goroutine is running.
type aiAuditWriterState struct {
	ch        chan *aiAuditOp
	stop      chan struct{}
	done      chan struct{} // closed when the goroutine has exited
	vaultPath string
}

var (
	aiAuditWriterMu sync.Mutex // guards aiAuditWriter
	aiAuditWriter   *aiAuditWriterState
)

// currentAIAuditWriter returns the active writer state, or nil if the writer
// is not running. Thread-safe; callers may use the returned pointer without
// holding aiAuditWriterMu (the state struct is never mutated after creation;
// only the package-level pointer is swapped).
func currentAIAuditWriter() *aiAuditWriterState {
	aiAuditWriterMu.Lock()
	defer aiAuditWriterMu.Unlock()
	return aiAuditWriter
}

// startAIAuditWriter launches the background AI audit-log writer goroutine for
// the given vault. Idempotent — a second call while the writer is running is a
// no-op. Mirrors startNetworkAuditWriter.
func startAIAuditWriter(vaultPath string) {
	aiAuditWriterMu.Lock()
	defer aiAuditWriterMu.Unlock()
	if aiAuditWriter != nil {
		return
	}
	w := &aiAuditWriterState{
		ch:        make(chan *aiAuditOp, 256),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
		vaultPath: vaultPath,
	}
	aiAuditWriter = w
	go w.run()
}

// stopAIAuditWriter signals the writer to drain remaining ops and exit, then
// blocks until the goroutine is done. Idempotent. Guarantees no queued entry
// is lost on vault close (#446 persistent-audit contract). Mirrors
// stopNetworkAuditWriter.
func stopAIAuditWriter() {
	aiAuditWriterMu.Lock()
	w := aiAuditWriter
	if w == nil {
		aiAuditWriterMu.Unlock()
		return
	}
	aiAuditWriter = nil
	aiAuditWriterMu.Unlock()
	close(w.stop)
	<-w.done
}

// run is the writer goroutine body. It processes ops in FIFO order until stop
// is closed, then drains every remaining queued op before exiting so no entry
// is lost on shutdown.
func (w *aiAuditWriterState) run() {
	defer close(w.done)
	for {
		select {
		case op := <-w.ch:
			w.process(op)
		case <-w.stop:
			// Drain every queued op before exiting so no entry is lost.
			for {
				select {
				case op := <-w.ch:
					w.process(op)
				default:
					return
				}
			}
		}
	}
}

// process handles one op. Entry appends to the per-plugin on-disk ai.log;
// clear empties every on-disk ai.log. If done is non-nil it is closed after
// the op completes so the caller (ClearAIAudit) can synchronize.
func (w *aiAuditWriterState) process(op *aiAuditOp) {
	if op.entry != nil {
		appendAIAuditLine(w.vaultPath, op.entry)
	}
	if op.clear {
		clearAIAuditFiles(w.vaultPath)
	}
	if op.done != nil {
		close(op.done)
	}
}
