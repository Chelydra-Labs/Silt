package main

// Phase 1 lifecycle-hardening tests (#451, #452).
//
// These cover the two latent defects closed before the first AI-capable plugin
// ships:
//   - #452: CloseVault/SwitchVault must drain in-flight AI calls before
//     teardown so a close can't strand an audit entry or leak into the next
//     vault.
//   - #451: ClearNetworkAudit/ClearAIAudit must not deadlock when the audit
//     writer exits between pointer capture and the channel send.
//
// The -race detector is the authoritative gate for these (TSAN is unavailable
// on the dev host — "unsupported VMA range" — but CI runs it); the tests below
// are deterministic hangs/deadlocks that surface with or without -race.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"silt/backend/ai"
	"silt/backend/vault"
)

// resetNetworkAuditState clears the package-level network audit log + writer so
// tests don't leak state. Mirrors resetAIAuditState.
func resetNetworkAuditState(t *testing.T) {
	t.Helper()
	networkAuditMu.Lock()
	networkAudit = nil
	networkAuditMu.Unlock()
	networkAuditWriterMu.Lock()
	networkAuditWriter = nil
	networkAuditWriterMu.Unlock()
}

// --- #451: Clear*Audit must not deadlock when the writer exits mid-call -----

// waitWithTimeout wraps wg.Wait so a deadlocked Clear/audit goroutine fails
// the test with a clear, fast diagnostic instead of wedging until Go's global
// 10-min timeout (which yields a generic "panic: test timed out" with no
// indication of WHICH goroutine hung).
func waitWithTimeout(t *testing.T, wg *sync.WaitGroup, timeout time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatalf("wg.Wait() deadlocked — a Clear/audit goroutine did not return within %s", timeout)
	}
}

// TestClearAIAudit_NoDeadlockWhenWriterStopsConcurrently exercises the exact
// race #451 describes: Clear captures a live writer pointer, stopAIAuditWriter
// nils the global + closes w.stop + drains + exits, then Clear sends on the
// buffered channel. The old blocking send would succeed (buffer has room) and
// then hang forever on <-op.done. The non-blocking send + w.stop fallback
// must complete promptly instead. We hammer the window from two goroutines;
// any single hang fails the test via the timeout guard.
func TestClearAIAudit_NoDeadlockWhenWriterStopsConcurrently(t *testing.T) {
	app := newTestApp(t)
	resetAIAuditState(t)

	const deadline = 3 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Goroutine A: ClearAIAudit in a tight loop.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			// An error here is fine — the writer may be mid-stop; the point
			// is that ClearAIAudit RETURNS rather than hanging.
			_ = app.ClearAIAudit()
		}
	}()

	// Goroutine B: cycle the writer up/down so Clear races a stopping writer.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			startAIAuditWriter(app.vaultPath)
			// Seed an entry so the writer has something to drain, widening the
			// window between capture and send.
			app.auditAI("deadlock-probe", "chat",
				"https://api.example.com/v1/chat", "m", "ok", nil)
			stopAIAuditWriter()
		}
	}()

	<-ctx.Done()
	close(stop)
	// Drain the writers so no goroutine lingers past the test. The Clear loop
	// may have stopped a fresh writer mid-flight; ensure a clean slate.
	stopAIAuditWriter()
	waitWithTimeout(t, &wg, 5*time.Second)
}

// TestClearNetworkAudit_NoDeadlockWhenWriterStopsConcurrently is the network
// twin of the AI test above — the same fix applies to both audit paths.
func TestClearNetworkAudit_NoDeadlockWhenWriterStopsConcurrently(t *testing.T) {
	app := newTestApp(t)
	resetNetworkAuditState(t)

	const deadline = 3 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = app.ClearNetworkAudit()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			startNetworkAuditWriter(app.vaultPath)
			app.auditNetwork("deadlock-probe", "GET",
				"https://api.example.com/v1/x", 200)
			stopNetworkAuditWriter()
		}
	}()

	<-ctx.Done()
	close(stop)
	stopNetworkAuditWriter()
	waitWithTimeout(t, &wg, 5*time.Second)
}

// TestClearAIAudit_WStopFallbackUsesWriterVaultNotCurrentVaultPath is the
// cross-vault regression guard for the <-w.stop fallback. When the writer exits
// before processing a clear-op, the fallback must truncate the WRITER's vault
// (w.vaultPath — the vault the clear was enqueued for), never a.vaultPath (which
// a concurrent SwitchVault may have already moved to a different vault).
//
// The test forces the fallback deterministically: it stops the writer (closes
// w.stop) WITHOUT niling the global pointer, so ClearAIAudit still captures a
// non-nil writer and takes the writer path. The writer has exited, so op.done
// never closes and the select fires <-w.stop every time. Before the fix, the
// fallback truncated a.vaultPath (vaultB — the wrong vault); after the fix it
// waits on <-w.done and truncates w.vaultPath (vaultA — the correct vault).
func TestClearAIAudit_WStopFallbackUsesWriterVaultNotCurrentVaultPath(t *testing.T) {
	app := newTestApp(t)
	resetAIAuditState(t)

	vaultA := t.TempDir() // the writer's vault (w.vaultPath)
	vaultB := t.TempDir() // a.vaultPath after a simulated switch

	entry := &AIAuditEntry{
		Plugin: "test-plugin", Kind: "chat", Host: "example.com",
		Model: "m", Status: "ok", At: "2026-01-01T00:00:00Z",
	}
	appendAIAuditLine(vaultA, entry)
	appendAIAuditLine(vaultB, entry)

	startAIAuditWriter(vaultA)
	w := currentAIAuditWriter()
	if w == nil {
		t.Fatal("startAIAuditWriter did not set the global writer")
	}

	// Simulate the post-switch state: a.vaultPath points to vaultB while the
	// writer is still for vaultA.
	app.vaultMu.Lock()
	app.vaultPath = vaultB
	app.vaultMu.Unlock()

	// Stop the writer by closing w.stop directly (NOT via stopAIAuditWriter,
	// which would nil the global). The writer drains + exits. ClearAIAudit will
	// still see the non-nil global and take the writer path — hitting <-w.stop
	// every time (the writer can't process the op, it already exited).
	close(w.stop)
	<-w.done

	// Re-seed vaultA so the fallback truncation has something to clear.
	appendAIAuditLine(vaultA, entry)

	if err := app.ClearAIAudit(); err != nil {
		t.Fatalf("ClearAIAudit: %v", err)
	}

	// vaultA (the writer's vault) must be cleared.
	if data, _ := os.ReadFile(filepath.Join(vaultA, ".system", "plugins", "test-plugin", "ai.log")); len(data) > 0 {
		t.Errorf("vaultA ai.log should be empty after clear (w.vaultPath), got %q", data)
	}
	// vaultB (a.vaultPath — the wrong vault) must survive. Before the fix this
	// was truncated, destroying the new vault's freshly-seeded audit history.
	if data, _ := os.ReadFile(filepath.Join(vaultB, ".system", "plugins", "test-plugin", "ai.log")); len(data) == 0 {
		t.Fatal("vaultB ai.log was truncated by ClearAIAudit — <-w.stop fallback used a.vaultPath instead of w.vaultPath (cross-vault data loss)")
	}

	// Cleanup: nil the global manually (w.stop is already closed; calling
	// stopAIAuditWriter would double-close it).
	aiAuditWriterMu.Lock()
	aiAuditWriter = nil
	aiAuditWriterMu.Unlock()
}

// TestClearAIAudit_DurableClearStillRunsThroughWriterWhenAlive is the
// regression guard the #451 fix could regress: if the non-blocking send
// accidentally always took the default (inline) branch, a clear would still
// truncate correctly in isolation, but it would NOT honor FIFO ordering
// against a queued append — the append would resurrect after the inline
// truncation. Enqueue an append, then clear, then stop (drain), and assert the
// pre-clear append did NOT survive: only the writer-path (FIFO clear-after-
// append) produces an empty file.
func TestClearAIAudit_DurableClearStillRunsThroughWriterWhenAlive(t *testing.T) {
	app := newTestApp(t)
	resetAIAuditState(t)

	startAIAuditWriter(app.vaultPath)
	t.Cleanup(stopAIAuditWriter)

	// Queue an append (writer alive). It lands on disk via the writer.
	app.auditAI("fifo-probe", "chat", "https://api.example.com/v1/chat", "m", "ok", nil)
	// Then clear. With the writer alive this MUST enqueue a clear op that the
	// writer processes AFTER the append (FIFO) — truncating the just-written
	// line. An accidental inline clear (default branch) would race the
	// writer's append and could leave the line on disk.
	if err := app.ClearAIAudit(); err != nil {
		t.Fatalf("ClearAIAudit: %v", err)
	}
	// Drain so the on-disk state is final before we read it.
	syncAIAuditWriter(t, currentAIAuditWriter())

	logPath := filepath.Join(app.vaultPath, ".system", "plugins", "fifo-probe", "ai.log")
	data, _ := os.ReadFile(logPath)
	if len(data) != 0 {
		t.Errorf("ai.log should be empty after clear drained the queued append; got %q", string(data))
	}
}

// --- #452: CloseVault drains in-flight AI calls before teardown -------------

// TestCloseVault_DrainsInFlightAICall is the regression test from #452/#471:
// an AI call in flight must be CANCELLED (via vaultCtx) when CloseVault
// begins, so the drain completes in milliseconds — not the provider timeout —
// and the aborted call's audit entry still lands in the CLOSING vault (not the
// next one). Before #471 the drain blocked until the call completed naturally
// (~60s on a slow local model) because only shutdown cancelled aiCtx.
func TestCloseVault_DrainsInFlightAICall(t *testing.T) {
	app := newTestApp(t)
	// newTestApp bypasses startup/initializeVaultServices, so wire the
	// vault-scoped AI context explicitly — this is the surface #471 adds and
	// the cancel the test must observe.
	app.vaultCtx, app.vaultCtxCancel = context.WithCancel(context.Background())
	resetAIAuditState(t)

	requestReceived := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestReceived)
		// Block until released. CloseVault's vaultCtxCancel must abort the
		// CLIENT before this unblocks — proving the drain didn't wait for the
		// (simulated) slow provider to respond.
		<-release
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "test",
			"choices": []map[string]any{{"message": map[string]any{"content": "pong"}}},
		})
	}))
	defer srv.Close()
	// The handler blocks at <-release; it must be unblocked before srv.Close()
	// (deferred above) returns, otherwise httptest hangs. close(release) runs
	// after the drain assertions below; if an early t.Fatalf fires, the
	// deferred srv.Close would hang — so defer the release here as a safety net.
	defer close(release)
	pointAIProviderAt(t, app, "chat", srv.URL, "test")

	tok, err := app.RegisterPluginSession("silt-tasks")
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}

	// Kick off the AI call — it blocks at the HTTP server, having already
	// released vaultMu after preflight and registered with vaultClosingWG.
	callDone := make(chan error, 1)
	go func() {
		_, err := app.PluginAIComplete("silt-tasks", tok, PluginAICompleteInput{
			Messages: []PluginAIChatMessage{{Role: "user", Content: "ping"}},
		})
		callDone <- err
	}()

	select {
	case <-requestReceived:
	case <-time.After(5 * time.Second):
		t.Fatal("PluginAIComplete did not reach the HTTP server in time")
	}

	oldVault := app.vaultPath

	// CloseVault must cancel vaultCtx → the HTTP client aborts → Done() fires →
	// the drain unblocks → CloseVault returns. This must happen in well under
	// the provider timeout; assert < 1s as the #471 regression guard. The
	// server handler is STILL blocked at <-release (we have not closed it), so
	// the only way CloseVault returns fast is the cancel path.
	start := time.Now()
	closeReturned := make(chan error, 1)
	go func() { closeReturned <- app.CloseVault() }()
	select {
	case err := <-closeReturned:
		if err != nil {
			t.Fatalf("CloseVault: %v", err)
		}
		elapsed := time.Since(start)
		if elapsed >= time.Second {
			t.Fatalf("CloseVault took %s — vaultCtx cancel did not abort the in-flight call (expected < 1s)", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CloseVault did not return — vaultCtx cancel failed to unblock the drain")
	}

	// The in-flight call must have been ABORTED (non-nil error) by the cancel,
	// not completed naturally. The ai service normalizes a cancelled transport
	// into *AIError{Kind: ErrUnreachable}; assert err != nil (the call did not
	// succeed) rather than a specific cancellation type.
	if err := <-callDone; err == nil {
		t.Fatal("in-flight PluginAIComplete succeeded — vaultCtx cancel did not abort the HTTP call")
	}

	// Teardown ran: services are nil.
	app.vaultMu.RLock()
	dbNil := app.db == nil
	pathEmpty := app.vaultPath == ""
	closingFlag := app.closing
	app.vaultMu.RUnlock()
	if !dbNil || !pathEmpty {
		t.Fatalf("teardown did not run: db=%v vaultPath=%q", app.db, app.vaultPath)
	}
	if closingFlag {
		t.Errorf("closing flag left set after CloseVault — would reject all future AI calls")
	}

	// The audit entry landed in the CLOSING vault's ai.log. auditAI runs
	// unconditionally after the call returns (cancelled or not), so the entry
	// is recorded with a non-"ok" status — proving the call was not lost even
	// though it was aborted.
	oldLog := filepath.Join(oldVault, ".system", "plugins", "silt-tasks", "ai.log")
	if data, _ := os.ReadFile(oldLog); len(data) == 0 {
		t.Errorf("closing vault's ai.log should contain the aborted call's audit entry; file is empty")
	}

	// Reopen a DIFFERENT vault and assert the old entry did not seed into it
	// (no cross-vault leak). seedAIAuditFromDisk reads only the new vault's
	// plugins/*/ai.log, which is empty on a fresh scaffold.
	newVault := t.TempDir()
	if err := scaffoldVaultForTest(t, newVault); err != nil {
		t.Fatalf("scaffold new vault: %v", err)
	}
	app.vaultMu.Lock()
	if err := app.initializeVaultServices(newVault); err != nil {
		app.vaultMu.Unlock()
		t.Fatalf("initializeVaultServices(newVault): %v", err)
	}
	app.vaultMu.Unlock()
	t.Cleanup(func() {
		_ = app.CloseVault()
	})

	entries, _ := app.GetAIAudit()
	if len(entries) != 0 {
		t.Errorf("reopened vault inherited %d audit entries from the closed vault (cross-vault leak): %+v", len(entries), entries)
	}
}

// TestCloseVault_DrainCompletesFastWhenCallInFlight is the headline #471
// regression guard: a close must NOT block for the provider timeout (~60s on a
// slow local model) when an AI call is in flight. The vaultCtx cancel must
// abort the HTTP client in milliseconds. This test simulates a pathologically
// slow provider (a server that would hold the connection for 30s) and asserts
// CloseVault returns in well under that — if the cancel regresses, this test
// fails at the 5s timeout instead of making a developer wait 30s.
func TestCloseVault_DrainCompletesFastWhenCallInFlight(t *testing.T) {
	app := newTestApp(t)
	app.vaultCtx, app.vaultCtxCancel = context.WithCancel(context.Background())
	resetAIAuditState(t)

	requestReceived := make(chan struct{})
	// slowRelease never fires during the test — the server simulates a
	// provider that takes 30s to respond. The cancel must abort the CLIENT
	// long before that.
	slowRelease := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestReceived)
		select {
		case <-slowRelease:
		case <-time.After(30 * time.Second):
		}
	}))
	defer srv.Close()
	defer close(slowRelease) // unblock the handler before srv.Close() returns
	pointAIProviderAt(t, app, "chat", srv.URL, "test")

	tok, err := app.RegisterPluginSession("silt-tasks")
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}

	callDone := make(chan error, 1)
	go func() {
		_, err := app.PluginAIComplete("silt-tasks", tok, PluginAICompleteInput{
			Messages: []PluginAIChatMessage{{Role: "user", Content: "ping"}},
		})
		callDone <- err
	}()

	select {
	case <-requestReceived:
	case <-time.After(5 * time.Second):
		t.Fatal("PluginAIComplete did not reach the HTTP server in time")
	}

	// The headline assertion: CloseVault must return in < 1s despite the
	// server holding the connection for 30s. A regression that drops the
	// vaultCtx cancel makes this block until the test's 5s timeout fires.
	start := time.Now()
	if err := app.CloseVault(); err != nil {
		t.Fatalf("CloseVault: %v", err)
	}
	elapsed := time.Since(start)
	if elapsed >= time.Second {
		t.Fatalf("CloseVault took %s with an in-flight call against a 30s-slow provider — vaultCtx cancel regressed (expected < 1s)", elapsed)
	}

	// The call must have been aborted, not completed.
	if err := <-callDone; err == nil {
		t.Fatal("in-flight PluginAIComplete succeeded against a 30s-slow provider — the call was not cancelled")
	}
}

// TestCloseVault_NewAICallRejectedWhileClosing verifies the closing flag gates
// new preflight: once CloseVault has set closing, a concurrent PluginAIComplete
// returns errVaultClosing WITHOUT issuing HTTP.
func TestCloseVault_NewAICallRejectedWhileClosing(t *testing.T) {
	app := newTestApp(t)
	resetAIAuditState(t)

	httpHit := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case httpHit <- struct{}{}:
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "x"}}},
		})
	}))
	defer srv.Close()
	pointAIProviderAt(t, app, "chat", srv.URL, "test")

	tok, err := app.RegisterPluginSession("silt-tasks")
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}

	// Simulate the "closing" window: set the flag under the lock, exactly as
	// CloseVault does before its drain.
	app.vaultMu.Lock()
	app.closing = true
	app.vaultMu.Unlock()
	t.Cleanup(func() {
		app.vaultMu.Lock()
		app.closing = false
		app.vaultMu.Unlock()
	})

	_, err = app.PluginAIComplete("silt-tasks", tok, PluginAICompleteInput{
		Messages: []PluginAIChatMessage{{Role: "user", Content: "ping"}},
	})
	if !errors.Is(err, errVaultClosing) {
		t.Fatalf("want errVaultClosing, got %v", err)
	}
	select {
	case <-httpHit:
		t.Fatal("AI HTTP server was contacted even though the vault is closing")
	default:
		// Expected: preflight rejected before HTTP.
	}
}

// TestSwitchVault_DrainsInFlightAICall mirrors the CloseVault drain test for
// the switch path (#452 names "close/switch", #471 adds vaultCtx cancel to
// both). SwitchVault shares teardownVaultServices, so the same cancel-then-drain
// applies before the cutover. The in-flight call must be ABORTED (not drained
// to completion) and its audit must land in the FIRST vault, not the second.
func TestSwitchVault_DrainsInFlightAICall(t *testing.T) {
	app := newTestApp(t)
	// newTestApp bypasses startup/initializeVaultServices — wire the
	// vault-scoped AI context explicitly (the surface #471 adds).
	app.vaultCtx, app.vaultCtxCancel = context.WithCancel(context.Background())
	resetAIAuditState(t)
	firstVault := app.vaultPath

	// Second vault: a valid Silt vault (has .system) for SwitchVault to target.
	secondVault := t.TempDir()
	if err := scaffoldVaultForTest(t, secondVault); err != nil {
		t.Fatalf("scaffold second vault: %v", err)
	}

	requestReceived := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestReceived)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "pong"}}},
		})
	}))
	defer srv.Close()
	defer close(release) // unblock the handler before srv.Close() returns
	pointAIProviderAt(t, app, "chat", srv.URL, "test")

	tok, err := app.RegisterPluginSession("silt-tasks")
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}

	callDone := make(chan error, 1)
	go func() {
		_, err := app.PluginAIComplete("silt-tasks", tok, PluginAICompleteInput{
			Messages: []PluginAIChatMessage{{Role: "user", Content: "ping"}},
		})
		callDone <- err
	}()

	select {
	case <-requestReceived:
	case <-time.After(5 * time.Second):
		t.Fatal("PluginAIComplete did not reach the HTTP server in time")
	}

	// SwitchVault must cancel vaultCtx → the HTTP client aborts → Done() fires →
	// the drain unblocks → the cutover proceeds. Assert < 1s as the #471
	// regression guard. The server handler is still blocked at <-release, so
	// only the cancel path can make SwitchVault return fast.
	start := time.Now()
	switchReturned := make(chan error, 1)
	go func() { switchReturned <- app.SwitchVault(secondVault) }()
	select {
	case err := <-switchReturned:
		if err != nil {
			t.Fatalf("SwitchVault: %v", err)
		}
		if elapsed := time.Since(start); elapsed >= time.Second {
			t.Fatalf("SwitchVault took %s — vaultCtx cancel did not abort the in-flight call (expected < 1s)", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SwitchVault did not return — vaultCtx cancel failed to unblock the drain")
	}

	// The in-flight call must have been ABORTED (non-nil error) by the cancel.
	if err := <-callDone; err == nil {
		t.Fatal("in-flight PluginAIComplete succeeded — vaultCtx cancel did not abort the HTTP call")
	}

	// The cutover landed on the second vault.
	app.vaultMu.RLock()
	finalPath := app.vaultPath
	closingFlag := app.closing
	app.vaultMu.RUnlock()
	if finalPath != secondVault {
		t.Fatalf("vaultPath = %q, want %q", finalPath, secondVault)
	}
	if closingFlag {
		t.Errorf("closing flag left set after SwitchVault — new vault would reject AI calls")
	}
	t.Cleanup(func() {
		_ = app.CloseVault()
	})

	// The aborted call's audit entry landed in the FIRST vault's ai.log (the
	// call ran against the first vault before the cutover), not the second's.
	// auditAI runs unconditionally after the call returns (cancelled or not).
	firstLog := filepath.Join(firstVault, ".system", "plugins", "silt-tasks", "ai.log")
	if data, _ := os.ReadFile(firstLog); len(data) == 0 {
		t.Errorf("first vault's ai.log should contain the aborted call's audit entry; file is empty")
	}
	secondLog := filepath.Join(secondVault, ".system", "plugins", "silt-tasks", "ai.log")
	if data, _ := os.ReadFile(secondLog); len(data) != 0 {
		t.Errorf("second vault's ai.log should be empty (no leak); got %q", string(data))
	}
}

// scaffoldVaultForTest creates a valid Silt vault layout at path so
// SwitchVault/initializeVaultServices accept it. Uses the canonical scaffolder
// so the new vault has a real config.yaml + .system tree (initializeVaultServices
// runs a full index scan and config load).
func scaffoldVaultForTest(t *testing.T, path string) error {
	t.Helper()
	return vault.ScaffoldVault(path)
}

// compile-time guard: ensure ai.CompleteResult is referenced so the import
// stays used if future tests drop the explicit reference above.
var _ = ai.CompleteResult{}

// TestWithAIPreflight_EnforcesDrainContract is #473's structural test: the
// wrapper bundles the closing-check + vaultClosingWG.Add(1) under one RLock
// hold and returns a done func the caller defers. This test pins both halves
// of the contract:
//   - SUCCESS path: done is non-nil and balances the Add (a subsequent
//     vaultClosingWG.Wait() returns promptly once done is called).
//   - CLOSING path: once closing=true, the wrapper returns errVaultClosing with
//     a nil done — no Add ran, so there is nothing to balance (a non-nil done
//     here would be a double-Done panic waiting to happen, and a missing Add
//     would under-count the drain).
//
// A future IPC handler that drops the done defer would surface here as a
// Wait() deadlock (the Add is never balanced).
func TestWithAIPreflight_EnforcesDrainContract(t *testing.T) {
	app := newTestApp(t)
	resetAIAuditState(t)

	tok, err := app.RegisterPluginSession("silt-tasks")
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}

	// --- SUCCESS path: done is non-nil and balances the Add ---
	provider, _, done, err := app.withAIPreflight("silt-tasks", tok, "chat")
	if err != nil {
		t.Fatalf("withAIPreflight success path: %v", err)
	}
	if provider.BaseURL == "" && provider.Model == "" {
		// The default scaffold config has no AI provider, so the snapshot may
		// be empty — that's fine; preflight still succeeds and registers the
		// drain. What matters is the done func.
	}
	if done == nil {
		t.Fatal("withAIPreflight returned nil done on the success path — the drain Add ran but the caller has no way to balance it (drain-contract regression)")
	}

	// Before done is called, the WG has one outstanding Add. A Wait() here
	// would block; prove it via a timed Wait that MUST complete only after
	// done fires.
	waitReturned := make(chan struct{})
	go func() {
		app.vaultClosingWG.Wait()
		close(waitReturned)
	}()
	// Give the Wait goroutine a moment to observe the non-zero counter. A
	// 50ms wait is generous for goroutine scheduling; the Wait blocks on the
	// counter, so it cannot return early.
	select {
	case <-waitReturned:
		t.Fatal("vaultClosingWG.Wait() returned before done() — the Add was not outstanding (drain-contract regression)")
	case <-time.After(50 * time.Millisecond):
		// Expected: Wait is blocked on the unbalanced Add.
	}

	// Balance the Add. The Wait goroutine must now complete promptly.
	done()
	select {
	case <-waitReturned:
	case <-time.After(time.Second):
		t.Fatal("vaultClosingWG.Wait() did not return after done() — the done func does not balance the Add")
	}

	// --- CLOSING path: done is nil, no Add ran ---
	app.vaultMu.Lock()
	app.closing = true
	app.vaultMu.Unlock()
	t.Cleanup(func() {
		app.vaultMu.Lock()
		app.closing = false
		app.vaultMu.Unlock()
	})

	_, _, doneClosing, errClosing := app.withAIPreflight("silt-tasks", tok, "chat")
	if !errors.Is(errClosing, errVaultClosing) {
		t.Fatalf("withAIPreflight closing path: want errVaultClosing, got %v", errClosing)
	}
	if doneClosing != nil {
		t.Fatal("withAIPreflight returned a non-nil done on the closing (error) path — calling it would double-Done or the Add never ran (drain-contract regression)")
	}

	// The closing-path rejection must NOT have touched the WG: a Wait() must
	// still return immediately (the only Add was the success-path one, already
	// balanced above).
	waitReturned2 := make(chan struct{})
	go func() {
		app.vaultClosingWG.Wait()
		close(waitReturned2)
	}()
	select {
	case <-waitReturned2:
		// Expected: WG is balanced; Wait returns at once.
	case <-time.After(time.Second):
		t.Fatal("vaultClosingWG.Wait() blocked after the closing-path call — the rejection path leaked an unbalanced Add")
	}
}

// TestInitializeVaultServices_CancelsPriorVaultCtx pins the MoveVault/
// rollbackMove re-init path (#471 follow-up): initializeVaultServices must
// cancel any prior vaultCtx before overwriting it. teardownVaultServices does
// NOT cancel vaultCtx, and MoveVault/rollbackMove reach initializeVaultServices
// via teardown → init WITHOUT a proactive cancel (unlike CloseVault/SwitchVault,
// which cancel before the drain wait). Without the self-cleaning guard at the
// top of initializeVaultServices, every vault move / failed-move rollback would
// orphan the old context in aiCtx.children until shutdown.
func TestInitializeVaultServices_CancelsPriorVaultCtx(t *testing.T) {
	app := newTestApp(t)
	// Capture the vault path before teardown nils it (newTestApp scaffolds a
	// real vault on disk that survives teardown — teardown closes the db but
	// leaves the files, so re-init on the same path works).
	vaultPath := app.vaultPath
	// Simulate a prior init's vaultCtx (newTestApp builds the App literal
	// directly, so vaultCtx is nil until the first initializeVaultServices).
	app.vaultCtx, app.vaultCtxCancel = context.WithCancel(context.Background())
	prior := app.vaultCtx

	// Sanity: the prior context is live before re-init.
	select {
	case <-prior.Done():
		t.Fatal("prior vaultCtx already done before re-init")
	default:
	}

	// Re-init via the MoveVault/rollback shape: teardown (does NOT cancel
	// vaultCtx) then initializeVaultServices (the guard must cancel prior).
	app.vaultMu.Lock()
	app.teardownVaultServices()
	if err := app.initializeVaultServices(vaultPath); err != nil {
		app.vaultMu.Unlock()
		t.Fatalf("initializeVaultServices: %v", err)
	}
	app.vaultMu.Unlock()
	t.Cleanup(func() { _ = app.CloseVault() })

	// The guard cancelled the prior context before overwriting it. If the guard
	// regresses, the prior context stays live (orphaned) and this times out.
	select {
	case <-prior.Done():
		// Expected: prior was cancelled by the re-init guard.
	case <-time.After(time.Second):
		t.Fatal("prior vaultCtx was not cancelled by re-init — MoveVault/rollback would orphan it in aiCtx.children")
	}

	// A fresh context took its place (not the orphaned one).
	app.vaultMu.RLock()
	fresh := app.vaultCtx
	app.vaultMu.RUnlock()
	if fresh == nil || fresh == prior {
		t.Fatal("re-init did not mint a fresh vaultCtx (nil or same pointer as prior)")
	}
}

// TestMoveVault_DrainsInFlightAICall pins the #452/#471 drain on the MoveVault
// cutover — the third lifecycle path. MoveVault used to call teardown→init
// directly with no closing flag, no vaultCtxCancel, and no vaultClosingWG.Wait,
// so an AI call past preflight survived teardown (which stops the audit
// writer); once initializeVaultServices' self-cleaning guard cancelled its
// context, its audit entry landed in the wrong vault via the inline fallback.
// The fix mirrors CloseVault/SwitchVault: drain (closing + cancel + Wait)
// BEFORE teardown so the call's audit routes through the still-running writer
// into the SOURCE vault. This test asserts the audit lands in src, not dest.
func TestMoveVault_DrainsInFlightAICall(t *testing.T) {
	// newMoveTestApp runs the full initializeVaultServices (so vaultCtx +
	// the audit writer are wired as in production, unlike newTestApp).
	app, src := newMoveTestApp(t)
	resetAIAuditState(t)

	dest := filepath.Join(t.TempDir(), "moved")

	requestReceived := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestReceived)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"content": "pong"}}},
		})
	}))
	defer srv.Close()
	defer close(release) // unblock the handler before srv.Close() returns
	pointAIProviderAt(t, app, "chat", srv.URL, "test")

	tok, err := app.RegisterPluginSession("silt-tasks")
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}

	callDone := make(chan error, 1)
	go func() {
		_, err := app.PluginAIComplete("silt-tasks", tok, PluginAICompleteInput{
			Messages: []PluginAIChatMessage{{Role: "user", Content: "ping"}},
		})
		callDone <- err
	}()

	select {
	case <-requestReceived:
	case <-time.After(5 * time.Second):
		t.Fatal("PluginAIComplete did not reach the HTTP server in time")
	}

	// MoveVault must drain (cancel vaultCtx + Wait) before teardown, so it
	// returns in well under the provider timeout once the in-flight call is
	// aborted — not blocked until the (simulated) slow provider responds.
	start := time.Now()
	moveDone := make(chan error, 1)
	go func() {
		_, err := app.MoveVault(dest, false)
		moveDone <- err
	}()
	select {
	case err := <-moveDone:
		if err != nil {
			t.Fatalf("MoveVault: %v", err)
		}
		if elapsed := time.Since(start); elapsed >= time.Second {
			t.Fatalf("MoveVault took %s — the drain did not cancel the in-flight call (expected < 1s)", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("MoveVault did not return — drain missing or deadlocked")
	}

	// The in-flight call must have been aborted (non-nil error) by the drain's
	// vaultCtx cancel, not completed naturally.
	if err := <-callDone; err == nil {
		t.Fatal("in-flight PluginAIComplete succeeded — the drain did not cancel the HTTP call")
	}

	// The aborted call's audit entry landed in the SOURCE vault's ai.log: the
	// drain ran before teardown, so the still-running writer (w.vaultPath=src)
	// processed it. dest's ai.log was copied BEFORE the entry existed (MoveVault
	// copies the tree before the cutover), so it must stay empty — proving no
	// cross-vault leak (the #452 class this hardens).
	srcLog := filepath.Join(src, ".system", "plugins", "silt-tasks", "ai.log")
	if data, _ := os.ReadFile(srcLog); len(data) == 0 {
		t.Errorf("source vault's ai.log should contain the drained audit entry; file is empty")
	}
	destLog := filepath.Join(dest, ".system", "plugins", "silt-tasks", "ai.log")
	if data, _ := os.ReadFile(destLog); len(data) != 0 {
		t.Errorf("dest vault's ai.log should be empty (no cross-vault leak); got %q", string(data))
	}
}
