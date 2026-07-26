package main

import (
	"log"
	"os"
	"strings"

	"silt/backend/templates"
)

// teardownVaultServices closes and nils every vault-scoped service in the
// reverse order of initializeVaultServices. Shared by shutdown (app exit)
// and CloseVault (workspace switch) so the two paths can't drift. Safe to
// call when services are already nil (each close is guarded).
func (a *App) teardownVaultServices() {
	// Stop MCP before audit/DB teardown so in-flight tools fail cleanly (#687).
	a.stopMCPHost()
	// Stop the audit writers FIRST so they drain queued entries for the closing
	// vault before any service they depend on (just vaultPath at this point)
	// goes away. After this returns, every enqueued audit write is on disk.
	stopNetworkAuditWriter()
	stopAIAuditWriter()
	// Clear the in-memory audit slices so a subsequent vault open seeds from
	// the new vault's on-disk logs, not the closed vault's leftover entries.
	// Without this, the seed guard (len == 0) would skip reseeding and the
	// new vault would display the old vault's audit history (#446 hardening).
	networkAuditLog.reset()
	aiAuditLog.reset()
	if a.watcher != nil {
		// Drop every focus lease before tearing the watcher down so a clean
		// exit can't strand a file under fsnotify suppression (#38).
		a.watcher.ReleaseAllFocus()
		_ = a.watcher.Close()
		a.watcher = nil
	}
	if a.templateWatcher != nil {
		_ = a.templateWatcher.Close()
		a.templateWatcher = nil
	}
	if a.configWatcher != nil {
		_ = a.configWatcher.Close()
		a.configWatcher = nil
	}
	if a.tracker != nil {
		a.tracker.Stop()
		a.tracker = nil
	}
	// Close the read-only plugin handle too (it points at the closing index).
	a.pluginRODBMu.Lock()
	if a.pluginRODB != nil {
		_ = a.pluginRODB.Close()
		a.pluginRODB = nil
	}
	a.pluginRODBMu.Unlock()
	// Close every per-plugin DB pool (#213). These point at files under the
	// closing vault's .system/plugins/<id>/data/, so they must be released
	// before the vault path goes away (and before any folder removal on a
	// vault move — Windows file lock).
	a.closeAllPluginDBs()
	if a.db != nil {
		// Close runs PRAGMA wal_checkpoint(TRUNCATE) so the WAL is merged
		// into the main index file on a clean close (#29).
		_ = a.db.Close()
		a.db = nil
	}
	a.coordinator = nil
	a.vaultPath = ""
	// Drop Dev Mode menu enablement unless SILT_DEBUG keeps it on (#684).
	a.syncOpenDevToolsMenuItemEnabled(strings.EqualFold(os.Getenv("SILT_DEBUG"), "1"))
	// Session security aggregates are vault-scoped in practice; clear on
	// vault close so the next vault doesn't inherit denial badges (#518).
	if a.securityStats != nil {
		a.securityStats.clear()
	}
	// F4: clear the per-host grants store so a subsequent vault open starts
	// fresh (LoadGrants + seedFirstPartyGrants repopulate). The on-disk file
	// is untouched — it persists across vault sessions.
	a.configMu.Lock()
	a.grants = nil
	a.quarantinedLinks = nil
	a.configMu.Unlock()
	templates.ResetPluginRegistry()
}

// CloseVault tears down the active vault's services in the reverse order of
// initializeVaultServices (via the shared teardownVaultServices helper).
// After it returns, IsVaultInitialized is false so the UI re-shows the
// onboarding screen. It does NOT clear the saved settings.json path — the
// user can re-open the same vault via InitializeVault / a new selection.
// Idempotent: safe to call when no vault is open. DRAINS in-flight AI calls
// (PluginAIComplete/PluginAIEmbed) before teardown via the closing flag +
// vaultClosingWG, so a close can't strand an AI call that would otherwise
// append a stale audit entry or write into the next vault (#452). The drain
// is distinct from a.wg (which tracks every handler for shutdown) because
// CloseVault does a.wg.Add(1) itself, so a.wg.Wait() here would self-deadlock.
func (a *App) CloseVault() error {
	a.wg.Add(1)
	defer a.wg.Done()

	// Fast nil-check under the lock so the "nothing to close" decision can't
	// race a concurrent Initialize.
	a.vaultMu.Lock()
	if a.vaultPath == "" && a.db == nil {
		a.vaultMu.Unlock()
		return nil
	}
	// Set the closing flag under the exclusive lock so withAIPreflight's
	// RLock-hold check+Add becomes atomic w.r.t. this set. New AI calls now
	// reject (errVaultClosing) before issuing HTTP; calls already past
	// preflight are tracked in vaultClosingWG and drained next. Snapshot the
	// cancel func + vaultPath under the same hold so the reads after the
	// Unlock are correct-by-construction (not reliant on a cross-goroutine
	// happens-before argument that a future caller could break).
	a.closing = true
	cancel := a.vaultCtxCancel
	vp := a.vaultPath
	a.vaultMu.Unlock()

	// Cancel the vault-scoped AI context OUTSIDE the lock so every in-flight
	// HTTP call observes context.Canceled in milliseconds — the HTTP client
	// aborts the request, the call returns, vaultClosingWG.Done() fires, and
	// the Wait() below unblocks promptly. Without this the drain blocks for
	// up to the provider timeout (~60s on a slow local model) with no UI
	// feedback, risking a force-quit mid-teardown (#471). The closing flag
	// (set above) still rejects NEW calls; vaultCtx is re-created on the
	// next initializeVaultServices, so cancelling it here is safe.
	if cancel != nil {
		cancel()
	} else if vp != "" {
		// vaultPath is set but vaultCtxCancel is nil — the vault was opened
		// without going through initializeVaultServices (every production
		// path does). The drain below will fall back to the provider-timeout
		// bound (the pre-#471 behavior). Log so a future code path that
		// bypasses the initializer surfaces immediately rather than silently
		// regressing the close latency.
		log.Printf("CloseVault: vaultCtxCancel is nil with an open vault (%s) — vault-scoped AI cancellation skipped (initializeVaultServices did not run for this vault)", vp)
	}

	// Drain in-flight AI calls OUTSIDE the lock. They released vaultMu after
	// preflight (the HTTP call doesn't hold it), so the lock can't serialize
	// them — and holding it across the (now short) completion would block
	// every reader IPC. With vaultCtx cancelled above, the wait is bounded
	// by the HTTP client's context-observe latency (milliseconds), not the
	// provider timeout.
	a.vaultClosingWG.Wait()

	// Emit vault:closing BEFORE teardown so the frontend plugin loader can run
	// every plugin's onVaultClose hook (#106) while IPC is still live. The
	// event is best-effort: if no frontend is mounted (e.g. headless test),
	// the emit is a no-op (a.emit guards wailsApp == nil internally).
	a.emit("vault:closing", struct{}{})
	// Hold the write lock across the teardown so concurrent readers can't
	// dereference a service pointer mid-close.
	a.vaultMu.Lock()
	a.teardownVaultServices()
	a.closing = false
	a.vaultMu.Unlock()
	return nil
}
