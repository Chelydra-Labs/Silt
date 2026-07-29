package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"silt/backend/ai"
)

// resetAIAuditState clears the package-level in-memory log + stops the writer
// so tests don't leak state into each other. stopAIAuditWriter is idempotent
// (no-op if the writer isn't running) and drains queued ops before exiting,
// so no audit data is lost and no goroutine is orphaned.
func resetAIAuditState(t *testing.T) {
	t.Helper()
	stopAIAuditWriter()
	aiAuditLog.reset()
}

// withAIAuditWriter starts the background writer for app.vaultPath and stops
// it (draining) on test cleanup. Mirrors withNetworkAuditWriter.
func withAIAuditWriter(t *testing.T, app *App) {
	t.Helper()
	startAIAuditWriter(app.vaultPath)
	t.Cleanup(stopAIAuditWriter)
}

// syncAIAuditWriter enqueues a no-op done op on the writer's channel and waits
// for it to be processed, so a test can deterministically observe that every
// prior auditAI call has landed on disk before stopping the writer.
func syncAIAuditWriter(t *testing.T, w *aiAuditWriterState) {
	t.Helper()
	if w == nil {
		t.Fatal("syncAIAuditWriter: writer is nil")
	}
	op := &aiAuditOp{done: make(chan struct{})}
	w.ch <- op
	<-op.done
}

// TestSeedAIAuditFromDisk writes 2 ai.log files under a temp vault with
// JSON-line AIAuditEntry records and asserts seedAIAuditFromDisk loads them
// sorted by At.
func TestSeedAIAuditFromDisk(t *testing.T) {
	app := newTestApp(t)
	resetAIAuditState(t)

	// Two plugins, two ai.log files, entries with out-of-order timestamps
	// across files so we exercise the cross-plugin sort-by-At.
	mustWriteAILog := func(plugin, content string) {
		t.Helper()
		dir := filepath.Join(app.vaultPath, ".system", "plugins", plugin)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "ai.log"), []byte(content), 0o644); err != nil {
			t.Fatalf("write ai.log: %v", err)
		}
	}
	mustWriteAILog("p1",
		`{"plugin":"p1","kind":"chat","host":"api.one.com/v1","model":"m","status":"ok","at":"2026-07-06T10:01:00Z"}`+"\n")
	mustWriteAILog("p2",
		`{"plugin":"p2","kind":"embed","host":"api.two.com/v1","model":"e","status":"ok","at":"2026-07-06T10:00:00Z"}`+"\n")

	seedAIAuditFromDisk(app.vaultPath)

	entries, err := app.GetAIAudit()
	if err != nil {
		t.Fatalf("GetAIAudit: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 seeded entries, got %d", len(entries))
	}
	// Sorted by At ascending: p2 (10:00) before p1 (10:01).
	if entries[0].Plugin != "p2" || entries[1].Plugin != "p1" {
		t.Errorf("seeded order = %q then %q; want p2 (older At) before p1", entries[0].Plugin, entries[1].Plugin)
	}
}

// TestSeedAIAuditFromDisk_DoesNotClobber asserts the seed only assigns when
// the in-memory log is empty (mirrors the network seed guard).
func TestSeedAIAuditFromDisk_DoesNotClobber(t *testing.T) {
	app := newTestApp(t)
	resetAIAuditState(t)

	// Pre-populate in-memory with one entry under the mutex.
	aiAuditLog.mu.Lock()
	aiAuditLog.entries = []AIAuditEntry{{Plugin: "pre-existing", Kind: "chat", At: "2026-07-06T09:00:00Z"}}
	aiAuditLog.mu.Unlock()

	// Write a disk ai.log that would otherwise seed.
	dir := filepath.Join(app.vaultPath, ".system", "plugins", "fromdisk")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ai.log"),
		[]byte(`{"plugin":"fromdisk","kind":"chat","at":"2026-07-06T10:00:00Z"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	seedAIAuditFromDisk(app.vaultPath)

	entries, _ := app.GetAIAudit()
	if len(entries) != 1 || entries[0].Plugin != "pre-existing" {
		t.Errorf("seed clobbered pre-existing in-memory entries; got %+v", entries)
	}
}

// TestStartAIAuditWriter_Idempotent mirrors TestAuditWriter_IdempotentStartStop:
// double-start is a no-op, double-stop is safe, start-stop-start runs again.
func TestStartAIAuditWriter_Idempotent(t *testing.T) {
	app := newTestApp(t)
	resetAIAuditState(t)

	startAIAuditWriter(app.vaultPath)
	startAIAuditWriter(app.vaultPath) // second is a no-op
	stopAIAuditWriter()
	stopAIAuditWriter()               // double-stop must not panic or block
	startAIAuditWriter(app.vaultPath) // restart works
	stopAIAuditWriter()
}

// TestAuditAI_WriterDrainsToDisk asserts an auditAI call with the writer
// running produces a JSON-line entry on disk after the writer is stopped.
func TestAuditAI_WriterDrainsToDisk(t *testing.T) {
	app := newTestApp(t)
	resetAIAuditState(t)

	startAIAuditWriter(app.vaultPath)
	app.auditAI("writer-drain", "chat", "https://api.example.com/v1/chat", "gpt-x", "ok", nil)
	stopAIAuditWriter() // blocks until the queued op is processed

	logPath := filepath.Join(app.vaultPath, ".system", "plugins", "writer-drain", "ai.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read ai.log: %v", err)
	}
	if !strings.Contains(string(data), `"plugin":"writer-drain"`) {
		t.Errorf("ai.log missing plugin entry; got %q", string(data))
	}
	if !strings.Contains(string(data), `"kind":"chat"`) {
		t.Errorf("ai.log missing kind=chat; got %q", string(data))
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Errorf("ai.log not newline-terminated; got %q", string(data))
	}
}

// TestAuditAI_InlineFallbackWithoutWriter asserts that without the writer
// (pre-init / test path), auditAI writes the entry inline.
func TestAuditAI_InlineFallbackWithoutWriter(t *testing.T) {
	app := newTestApp(t)
	resetAIAuditState(t)
	// Deliberately do NOT start the writer — exercises the inline fallback.

	app.auditAI("inline-fallback", "embed", "https://api.example.com/v1/embeddings", "embed-3", "ok", nil)

	logPath := filepath.Join(app.vaultPath, ".system", "plugins", "inline-fallback", "ai.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read ai.log: %v", err)
	}
	if !strings.Contains(string(data), `"plugin":"inline-fallback"`) {
		t.Errorf("inline ai.log missing entry; got %q", string(data))
	}
	if !strings.Contains(string(data), `"kind":"embed"`) {
		t.Errorf("inline ai.log missing kind=embed; got %q", string(data))
	}
}

// TestClearAIAudit_DurableClearWithWriter asserts ClearAIAudit empties BOTH
// in-memory state AND on-disk ai.log files when the writer is running.
func TestClearAIAudit_DurableClearWithWriter(t *testing.T) {
	app := newTestApp(t)
	resetAIAuditState(t)

	startAIAuditWriter(app.vaultPath)
	app.auditAI("clear-dur", "chat", "https://api.example.com/v1/chat", "gpt-x", "ok", nil)
	// Wait for the audit to land on disk before issuing the clear.
	syncAIAuditWriter(t, currentAIAuditWriter())

	if err := app.ClearAIAudit(); err != nil {
		t.Fatalf("ClearAIAudit: %v", err)
	}
	stopAIAuditWriter()

	logPath := filepath.Join(app.vaultPath, ".system", "plugins", "clear-dur", "ai.log")
	data, _ := os.ReadFile(logPath)
	if len(data) != 0 {
		t.Errorf("on-disk ai.log should be empty after ClearAIAudit, got %q", string(data))
	}
	entries, _ := app.GetAIAudit()
	if len(entries) != 0 {
		t.Errorf("in-memory audit should be empty after ClearAIAudit, got %d entries", len(entries))
	}
}

// TestClearAIAudit_InlineClearWithoutWriter mirrors
// TestClearNetworkAudit_TruncatesOnDiskFiles: without the writer the clear
// runs inline and truncates the on-disk file.
func TestClearAIAudit_InlineClearWithoutWriter(t *testing.T) {
	app := newTestApp(t)
	resetAIAuditState(t)
	// No writer — clear must run inline.

	dir := filepath.Join(app.vaultPath, ".system", "plugins", "clear-inline")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	logPath := filepath.Join(dir, "ai.log")
	if err := os.WriteFile(logPath,
		[]byte(`{"plugin":"clear-inline","kind":"chat","at":"2026-07-06T10:00:00Z"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := app.ClearAIAudit(); err != nil {
		t.Fatalf("ClearAIAudit: %v", err)
	}

	data, _ := os.ReadFile(logPath)
	if len(data) != 0 {
		t.Errorf("on-disk ai.log should be empty after inline ClearAIAudit, got %q", string(data))
	}
}

// TestStopAIAuditWriter_DrainsQueuedOps mirrors TestAuditWriter_DrainsOnShutdown:
// the writer MUST process every queued op before exiting so no audit data is
// lost on vault close.
func TestStopAIAuditWriter_DrainsQueuedOps(t *testing.T) {
	app := newTestApp(t)
	resetAIAuditState(t)

	startAIAuditWriter(app.vaultPath)
	const n = 50
	for i := 0; i < n; i++ {
		app.auditAI("ai-drain", "chat",
			fmt.Sprintf("https://api.example.com/v1/chat/%d", i), "gpt-x", "ok", nil)
	}
	// A final clear op: must run as part of the drain (no-loss shutdown).
	if err := app.ClearAIAudit(); err != nil {
		t.Fatalf("ClearAIAudit: %v", err)
	}
	stopAIAuditWriter()

	// After the drain + clear, the on-disk ai.log must be empty (the queued
	// entries landed, then the clear truncated them).
	logPath := filepath.Join(app.vaultPath, ".system", "plugins", "ai-drain", "ai.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read ai.log: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("ai.log should be empty after drain+clear, got %q (drain dropped ops OR clear did not run)", string(data))
	}
}

// TestAuditAI_UsageFieldsPreservedOnDisk asserts token-usage fields survive
// the JSON round-trip onto disk.
func TestAuditAI_UsageFieldsPreservedOnDisk(t *testing.T) {
	app := newTestApp(t)
	resetAIAuditState(t)

	prompt, completion, total := 42, 7, 49
	usage := &ai.AIUsage{PromptTokens: &prompt, CompletionTokens: &completion, TotalTokens: &total}
	app.auditAI("usage-plugin", "chat", "https://api.example.com/v1/chat", "gpt-x", "ok", usage)

	logPath := filepath.Join(app.vaultPath, ".system", "plugins", "usage-plugin", "ai.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read ai.log: %v", err)
	}
	s := string(data)
	for _, want := range []string{
		`"prompt_tokens":42`,
		`"completion_tokens":7`,
		`"total_tokens":49`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("ai.log missing %s; got %q", want, s)
		}
	}
}

// TestAIAudit_SizeCapTruncation mirrors TestAuditWriter_TruncatesOversizedLog:
// when an ai.log exceeds the 1 MB cap, the next append truncates it to the last
// maxPluginAuditLogLines (200) lines.
func TestAIAudit_SizeCapTruncation(t *testing.T) {
	app := newTestApp(t)
	resetAIAuditState(t)
	pluginID := "ai-trunc"
	dir := filepath.Join(app.vaultPath, ".system", "plugins", pluginID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	logPath := filepath.Join(dir, "ai.log")

	// Pre-write an ai.log over the 1 MB threshold so the next append triggers
	// truncation. ~20000 JSON lines × ~60 bytes ≈ 1.2 MB.
	var sb strings.Builder
	for i := 0; i < 20000; i++ {
		// Each line is a valid AIAuditEntry so a future seed would parse it.
		fmt.Fprintf(&sb, `{"plugin":"ai-trunc","kind":"chat","host":"api.example.com/v1","model":"old","status":"ok","at":"2026-07-06T10:00:%02dZ"}`+"\n", i%60)
	}
	if err := os.WriteFile(logPath, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write oversized ai.log: %v", err)
	}

	// Inline path (no writer) so truncation happens synchronously inside
	// appendAIAuditLine before we read the file back.
	app.auditAI(pluginID, "chat", "https://api.example.com/v1/chat", "trigger", "ok", nil)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read ai.log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	// Truncation keeps the last 200 lines, then the trigger append adds 1.
	if len(lines) > maxPluginAuditLogLines+1 {
		t.Errorf("ai.log has %d lines after truncation, want ≤ %d (200 kept + 1 trigger)",
			len(lines), maxPluginAuditLogLines+1)
	}
	// The trigger entry (most recent) must be present.
	if !strings.Contains(string(data), `"model":"trigger"`) {
		t.Errorf("trigger entry missing after truncation:\n%s", string(data))
	}
}

// TestAuditAI_ConcurrentAppendsNoSerialize is a smoke test mirroring
// TestAuditNetwork_ConcurrentFetchDoesNotSerialize: concurrent auditAI calls
// with the writer running must all land in-memory (the synchronous part) and
// on disk after stop (the drained part).
func TestAuditAI_ConcurrentAppendsNoSerialize(t *testing.T) {
	app := newTestApp(t)
	resetAIAuditState(t)

	startAIAuditWriter(app.vaultPath)
	const n = 64
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			app.auditAI(fmt.Sprintf("async-%d", i), "chat",
				fmt.Sprintf("https://api.example.com/v1/%d", i), "gpt-x", "ok", nil)
		}(i)
	}
	wg.Wait()
	stopAIAuditWriter()

	// All N entries must land in-memory.
	entries, _ := app.GetAIAudit()
	if len(entries) != n {
		t.Errorf("in-memory AI audit has %d entries, want %d", len(entries), n)
	}
}

// TestTeardownVaultServices_ClearsInMemoryAudit is the regression test for the
// cross-vault audit leak: teardownVaultServices must nil both in-memory audit
// slices (aiAudit + networkAudit) after stopping the writers, so a subsequent
// vault open seeds from the new vault's on-disk logs rather than the closed
// vault's leftover entries. Without the clear, the seed guard (len == 0) skips
// reseeding and the new vault displays the old vault's history (#446).
func TestTeardownVaultServices_ClearsInMemoryAudit(t *testing.T) {
	// A bare App with only vaultPath set survives teardownVaultServices: every
	// service field it touches (db, watcher, etc.) is nil-guarded, and
	// closeAllPluginDBs is a no-op on a nil pluginDBs map.
	app := &App{vaultPath: t.TempDir()}
	resetAIAuditState(t)
	// Also reset network audit state for a clean baseline.
	networkAuditLog.reset()

	startNetworkAuditWriter(app.vaultPath)
	startAIAuditWriter(app.vaultPath)

	// Populate both in-memory logs + on-disk files via the audit paths.
	app.auditNetwork("p-net", "GET", "https://api.example.com/v1", 200)
	app.auditAI("p-ai", "chat", "https://api.example.com/v1", "m", "ok", nil)

	netEntries, _ := app.GetNetworkAudit()
	aiEntries, _ := app.GetAIAudit()
	if len(netEntries) != 1 {
		t.Fatalf("network audit setup: want 1 entry, got %d", len(netEntries))
	}
	if len(aiEntries) != 1 {
		t.Fatalf("AI audit setup: want 1 entry, got %d", len(aiEntries))
	}

	// Simulate a vault close/switch: teardown must drain the writers AND clear
	// the in-memory slices so the next open's seed is not skipped.
	app.teardownVaultServices()

	networkAuditLog.mu.Lock()
	aiAuditLog.mu.Lock()
	netLeaked := networkAuditLog.entries
	aiLeaked := aiAuditLog.entries
	aiAuditLog.mu.Unlock()
	networkAuditLog.mu.Unlock()

	if netLeaked != nil {
		t.Errorf("network audit leaked across teardown: %d entries survived", len(netLeaked))
	}
	if aiLeaked != nil {
		t.Errorf("AI audit leaked across teardown: %d entries survived", len(aiLeaked))
	}
	if currentAIAuditWriter() != nil || currentNetworkAuditWriter() != nil {
		t.Errorf("audit writers still running after teardown")
	}
}

func TestRedactAIAuditFields_AllowlistOnly(t *testing.T) {
	in := map[string]any{
		"tool":         "search_notes",
		"status":       "ok",
		"staged":       true,
		"note":         "private note text",
		"summary":      "should not leak",
		"details":      "more private",
		"content":      "secret body",
		"tool_call_id": "call_1",
		"outcome":      "confirmed",
		"side":         "vector",
		// #811: block_id is the one vault-content-adjacent key allowed — a block
		// UUID is a fixed-shape identifier with no room for prose, so agent
		// write-tool mutations become traceable without leaking body text.
		"block_id": "7c2a3f1e-1111-2222-3333-444455556666",
		// Freeform-text and path-bearing keys MUST still be dropped.
		"args": "/home/user/vault/secret.md",
		"path": "C:\\Users\\someone\\vault\\page.md",
	}
	out := redactAIAuditFields(in)
	if out["tool"] != "search_notes" {
		t.Errorf("tool = %v", out["tool"])
	}
	if out["status"] != "ok" {
		t.Errorf("status = %v", out["status"])
	}
	if out["staged"] != true {
		t.Errorf("staged = %v", out["staged"])
	}
	if out["tool_call_id"] != "call_1" {
		t.Errorf("tool_call_id = %v", out["tool_call_id"])
	}
	if out["outcome"] != "confirmed" {
		t.Errorf("outcome = %v", out["outcome"])
	}
	if out["side"] != "vector" {
		t.Errorf("side = %v", out["side"])
	}
	if out["block_id"] != "7c2a3f1e-1111-2222-3333-444455556666" {
		t.Errorf("block_id = %v, want the UUID preserved", out["block_id"])
	}
	for _, banned := range []string{"note", "summary", "details", "content", "args", "path"} {
		if _, ok := out[banned]; ok {
			t.Errorf("%s must be dropped, got %v", banned, out[banned])
		}
	}
}

// TestRedactAIAuditFields_BlockIdNotAPath is the #811 regression: a block id
// is a UUID, never a path, so it survives redaction unchanged; a same-shaped
// value under a banned key (content/path) is dropped regardless of its value.
func TestRedactAIAuditFields_BlockIdNotAPath(t *testing.T) {
	const uuid = "7c2a3f1e-1111-2222-3333-444455556666"
	out := redactAIAuditFields(map[string]any{
		"block_id": uuid,
		"content":  uuid, // banned key — must drop even though the value is a UUID
	})
	if out["block_id"] != uuid {
		t.Errorf("block_id = %v, want %q", out["block_id"], uuid)
	}
	if _, ok := out["content"]; ok {
		t.Errorf("content must be dropped regardless of value, got %v", out["content"])
	}
}

// TestRedactAIAuditFields_BlockIdMustBeUUID is the #811 hardening regression:
// block_id reaches the redactor from model-supplied args (update_block/
// update_task pass the caller's id verbatim), so it must match a UUID shape —
// prose, paths, and fragments placed there are dropped, never persisted to the
// synced ai.log.
func TestRedactAIAuditFields_BlockIdMustBeUUID(t *testing.T) {
	const uuid = "7c2a3f1e-1111-2222-3333-444455556666"
	for _, bad := range []string{
		"not a uuid, just prose",
		"/home/user/vault/secret.md",                 // POSIX path
		"C:\\Users\\someone\\vault\\page.md",         // Windows path
		"7c2a3f1e",                                   // truncated fragment
		"7c2a3f1e-1111-2222-3333-444455556666-extra", // overlong
		"",
	} {
		out := redactAIAuditFields(map[string]any{"block_id": bad})
		if _, ok := out["block_id"]; ok {
			t.Errorf("block_id=%q must be dropped (not a UUID), got %v", bad, out["block_id"])
		}
	}
	// A valid UUID survives unchanged.
	out := redactAIAuditFields(map[string]any{"block_id": uuid})
	if out["block_id"] != uuid {
		t.Errorf("block_id UUID = %v, want %q preserved", out["block_id"], uuid)
	}
	// A non-string block_id (e.g. a number) is also dropped.
	outNum := redactAIAuditFields(map[string]any{"block_id": 12345})
	if _, ok := outNum["block_id"]; ok {
		t.Errorf("non-string block_id must be dropped, got %v", outNum["block_id"])
	}
}

func TestPluginAIAuditEvent_RejectsOversizedJSON(t *testing.T) {
	app := newTestApp(t)
	resetAIAuditState(t)
	app.configMu.Lock()
	app.cfg.Plugins.Disabled = nil
	app.cfg.AI.Features.Enabled = true
	app.configMu.Unlock()
	tok, err := app.RegisterPluginSession("silt-ai-agent")
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	huge := `{"kind":"tool_call","status":"ok","pad":"` + strings.Repeat("x", maxPluginAIAuditEventJSONBytes) + `"}`
	err = app.PluginAIAuditEvent("silt-ai-agent", tok, huge)
	if err == nil {
		t.Fatal("expected error for oversized event JSON")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error = %v, want exceeds cap", err)
	}
}

func TestPluginAIAuditEvent_AppendsRedacted(t *testing.T) {
	app := newTestApp(t)
	resetAIAuditState(t)
	app.configMu.Lock()
	app.cfg.Plugins.Disabled = nil
	app.cfg.AI.Features.Enabled = true
	app.configMu.Unlock()
	// First-party AI plugin exercises CapAI + feature gate + allowlist path.
	tok, err := app.RegisterPluginSession("silt-ai-agent")
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	err = app.PluginAIAuditEvent("silt-ai-agent", tok, `{"kind":"tool_call","tool":"search_notes","note":"private note text","content":"nope","status":"ok"}`)
	if err != nil {
		t.Fatalf("PluginAIAuditEvent: %v", err)
	}
	entries, err := app.GetAIAudit()
	if err != nil {
		t.Fatalf("GetAIAudit: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	if entries[0].Kind != "tool_call" {
		t.Errorf("kind = %q", entries[0].Kind)
	}
	if entries[0].Status != "ok" {
		t.Errorf("status = %q", entries[0].Status)
	}
	if strings.Contains(string(entries[0].Detail), "nope") {
		t.Errorf("detail leaked content: %s", entries[0].Detail)
	}
	if strings.Contains(string(entries[0].Detail), "private note") {
		t.Errorf("detail leaked note: %s", entries[0].Detail)
	}
	if !strings.Contains(string(entries[0].Detail), "search_notes") {
		t.Errorf("detail missing tool: %s", entries[0].Detail)
	}
}

// Stale session after AI master off: PluginAIAuditEvent must deny CapAI even
// when the session token is still registered (#632).
func TestPluginAIAuditEvent_RejectsWhenAIFeaturesOff(t *testing.T) {
	app := newTestApp(t)
	resetAIAuditState(t)
	app.configMu.Lock()
	app.cfg.Plugins.Disabled = nil
	app.cfg.AI.Features.Enabled = true
	app.configMu.Unlock()
	tok, err := app.RegisterPluginSession("silt-ai-agent")
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	app.configMu.Lock()
	app.cfg.AI.Features.Enabled = false
	app.configMu.Unlock()
	err = app.PluginAIAuditEvent("silt-ai-agent", tok, `{"kind":"tool_call","status":"ok"}`)
	if err == nil {
		t.Fatal("expected CapAI denial when Features.Enabled=false")
	}
}
