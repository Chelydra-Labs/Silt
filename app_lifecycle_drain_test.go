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
	wg.Wait()
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
	wg.Wait()
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

// TestCloseVault_DrainsInFlightAICall is the regression test from #452: an AI
// call in flight must be drained (and audited into the CLOSING vault) before
// teardown, then a reopened vault must NOT inherit the entry.
func TestCloseVault_DrainsInFlightAICall(t *testing.T) {
	app := newTestApp(t)
	resetAIAuditState(t)

	requestReceived := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestReceived)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "test",
			"choices": []map[string]any{{"message": map[string]any{"content": "pong"}}},
		})
	}))
	defer srv.Close()
	pointAIProviderAt(t, app, "chat", srv.URL, "test")

	tok, err := app.RegisterPluginSession("silt-kanban")
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}

	// Kick off the AI call — it blocks at the HTTP server, having already
	// released vaultMu after preflight and registered with vaultClosingWG.
	callDone := make(chan error, 1)
	go func() {
		_, err := app.PluginAIComplete("silt-kanban", tok, PluginAICompleteInput{
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

	// CloseVault must block on the drain while the AI call is in flight.
	closeReturned := make(chan error, 1)
	go func() { closeReturned <- app.CloseVault() }()
	select {
	case <-closeReturned:
		t.Fatal("CloseVault returned while an AI call was still in flight — drain missing")
	case <-time.After(200 * time.Millisecond):
		// Expected: CloseVault is blocked on vaultClosingWG.Wait().
	}

	// Release the HTTP call. The audit lands against the closing vault, Done()
	// fires, the drain completes, and CloseVault proceeds to teardown.
	close(release)

	select {
	case err := <-closeReturned:
		if err != nil {
			t.Fatalf("CloseVault: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("CloseVault did not return after the AI call completed — drain deadlocked")
	}

	// The in-flight call must have completed (no error) — it was drained, not
	// cancelled by the close (only shutdown cancels aiCtx).
	if err := <-callDone; err != nil {
		t.Fatalf("in-flight PluginAIComplete failed: %v", err)
	}

	// Teardown ran: services are nil.
	app.vaultMu.RLock()
	dbNil := app.db == nil
	pathEmpty := app.vaultPath == ""
	closingFlag := app.closing
	app.vaultMu.RUnlock()
	if !dbNil || !pathEmpty {
		t.Fatalf("teardown did not run: db=%v vaultPath=%q", !dbNil, app.vaultPath)
	}
	if closingFlag {
		t.Errorf("closing flag left set after CloseVault — would reject all future AI calls")
	}

	// The audit entry landed in the CLOSING vault's ai.log (drained before
	// teardown), proving the call was not lost.
	oldLog := filepath.Join(oldVault, ".system", "plugins", "silt-kanban", "ai.log")
	if data, _ := os.ReadFile(oldLog); len(data) == 0 {
		t.Errorf("closing vault's ai.log should contain the drained audit entry; file is empty")
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

	tok, err := app.RegisterPluginSession("silt-kanban")
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

	_, err = app.PluginAIComplete("silt-kanban", tok, PluginAICompleteInput{
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
// the switch path (#452 names "close/switch"). SwitchVault shares
// teardownVaultServices, so the same drain applies before the cutover.
func TestSwitchVault_DrainsInFlightAICall(t *testing.T) {
	app := newTestApp(t)
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
	pointAIProviderAt(t, app, "chat", srv.URL, "test")

	tok, err := app.RegisterPluginSession("silt-kanban")
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}

	callDone := make(chan error, 1)
	go func() {
		_, err := app.PluginAIComplete("silt-kanban", tok, PluginAICompleteInput{
			Messages: []PluginAIChatMessage{{Role: "user", Content: "ping"}},
		})
		callDone <- err
	}()

	select {
	case <-requestReceived:
	case <-time.After(5 * time.Second):
		t.Fatal("PluginAIComplete did not reach the HTTP server in time")
	}

	// SwitchVault must block on the drain while the AI call is in flight.
	switchReturned := make(chan error, 1)
	go func() { switchReturned <- app.SwitchVault(secondVault) }()
	select {
	case err := <-switchReturned:
		t.Fatalf("SwitchVault returned (err=%v) while an AI call was in flight — drain missing", err)
	case <-time.After(200 * time.Millisecond):
		// Expected: SwitchVault is blocked on vaultClosingWG.Wait().
	}

	close(release)

	select {
	case err := <-switchReturned:
		if err != nil {
			t.Fatalf("SwitchVault: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("SwitchVault did not return after the AI call completed — drain deadlocked")
	}

	if err := <-callDone; err != nil {
		t.Fatalf("in-flight PluginAIComplete failed: %v", err)
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

	// The drained audit entry landed in the FIRST vault's ai.log (the call
	// ran against the first vault before the cutover), not the second's.
	firstLog := filepath.Join(firstVault, ".system", "plugins", "silt-kanban", "ai.log")
	if data, _ := os.ReadFile(firstLog); len(data) == 0 {
		t.Errorf("first vault's ai.log should contain the drained audit entry; file is empty")
	}
	secondLog := filepath.Join(secondVault, ".system", "plugins", "silt-kanban", "ai.log")
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
