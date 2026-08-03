package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"silt/backend/config"
	"silt/backend/vault"
)

// Vault move / copy / switch and the move-rollback helper. The init / close /
// teardown lifecycle itself remains in app.go; this file holds the user-driven
// relocation flows (and their pickers).

// PickVaultDestination opens a native folder picker for a vault move/copy
// destination and returns the chosen path ("" on cancel). Shared by Move and
// Copy so the frontend can show its own confirmation modal between the pick
// and the commit, mirroring the delete flows (#141).
func (a *App) PickVaultDestination() (string, error) {
	return a.openDirectoryDialog("Select Destination for Silt Vault")
}

// CopyVault duplicates the active vault tree at destPath, EXCLUDING the
// reproducible SQLite index (rebuilt from markdown when the copy is first
// opened). The active vault is untouched: no settings change, no service
// teardown, no event. The copy is a separate workspace the user can switch
// to later. CopyVaultTree validates the destination and verifies every byte
// (size + SHA-256); on failure it cleans up the partial destination.
func (a *App) CopyVault(destPath string) (vault.CopyResult, error) {
	a.wg.Add(1)
	defer a.wg.Done()

	// Snapshot the active vault path under the read lock so the copy reads a
	// stable source even if a lifecycle transition is racing. The copy itself
	// (CopyVaultTree) is long and never touches the service pointers, so it
	// runs without holding the lock.
	a.vaultMu.RLock()
	src := a.vaultPath
	a.vaultMu.RUnlock()
	if src == "" {
		return vault.CopyResult{}, fmt.Errorf("no vault is currently open")
	}
	return vault.CopyVaultTree(src, destPath)
}

// MoveVault relocates the active vault to destPath: copy + verify, then a
// cutover (teardown services → patch dest config.yaml path → persist
// settings.json → reinit at the new path) that reuses the existing
// close/open paths, with a verbatim rollback to the original path if reinit
// fails. The dest config.yaml's notebooks.path is updated to dest so the
// Settings → General "Workspace" row shows the new location. Emits vault:moved
// ({from, to}) so the frontend resets navigation and reloads its stores. If
// removeOld is true the original folder is deleted AFTER a successful
// cutover (non-fatal on failure: RemoveOldErr carries the message).
func (a *App) MoveVault(destPath string, removeOld bool) (vault.MoveVaultResult, error) {
	a.wg.Add(1)
	defer a.wg.Done()

	// Snapshot the active vault path under the read lock so the (long) copy
	// reads a stable source even if a lifecycle transition is racing.
	a.vaultMu.RLock()
	src := a.vaultPath
	a.vaultMu.RUnlock()
	if src == "" {
		return vault.MoveVaultResult{}, fmt.Errorf("no vault is currently open")
	}

	// 1. Copy + verify. On failure the primitive cleans up dest itself; the
	//    active vault and settings are untouched. No lock held — this is the
	//    slow phase and it never touches the service pointers.
	copyRes, err := vault.CopyVaultTree(src, destPath)
	if err != nil {
		return vault.MoveVaultResult{}, fmt.Errorf("move vault: %w", err)
	}
	dest := destPath
	// Snapshot the instant the copy+verify completed. Used before removeOld
	// to detect an external edit (e.g. an external editor) that landed in the source
	// during the cutover — deleting the source then would silently lose it.
	copyDoneAt := time.Now()

	// 2. Update the dest config.yaml notebooks.path so the Settings → General
	//    workspace row reflects the new location (matches ScaffoldVault's
	//    forward-slash convention). Best-effort: a failure here is logged but
	//    does not abort — the vault is fully usable with a stale display path.
	if cfg, cfgErr := config.Load(dest); cfgErr == nil {
		cfg.Notebooks.Path = filepath.ToSlash(dest)
		if err := config.Save(dest, cfg); err != nil {
			log.Printf("MoveVault: could not update dest config.yaml notebooks.path: %v", err)
		}
	}

	// 3. Cutover under the exclusive write lock: no reader can dereference a
	//    service pointer while the db / watcher are being torn down and
	//    rebuilt. The prior-settings snapshot is read UNDER this lock (not
	//    before it) so a concurrent settings.json writer (ApplyTheme) can't
	//    commit a change that the cutover then overwrites with a stale
	//    snapshot. Re-check a.vaultPath hasn't moved (defensive; the UI
	//    serializes lifecycle calls). rollbackMove also runs under this lock
	//    (it does not acquire it itself — RWMutex is not reentrant).
	cutoverErr := func() error {
		// First hold: snapshot prior settings, set the closing flag, and
		// capture the cancel func — all under the lock so withAIPreflight's
		// RLock-hold check+Add stays atomic w.r.t. closing=true (#452).
		a.vaultMu.Lock()
		if a.vaultPath != src {
			a.vaultMu.Unlock()
			return fmt.Errorf("vault changed during move (concurrent lifecycle transition)")
		}
		prior, err := vault.LoadSettings()
		if err != nil && !errors.Is(err, vault.ErrSettingsFingerprintMismatch) {
			a.vaultMu.Unlock()
			return fmt.Errorf("move vault: snapshot settings: %w", err)
		}
		// Drain in-flight AI calls BEFORE teardown, mirroring CloseVault/
		// SwitchVault (#452/#471). Without this, an AI call past preflight
		// survives teardownVaultServices (which stops the audit writer) and,
		// once initializeVaultServices' self-cleaning guard cancels its
		// vaultCtx, its audit entry lands in the wrong vault via the inline
		// fallback (auditAI reads a.vaultPath with no lock). Draining first
		// routes the call's audit through the still-running writer into the
		// SOURCE vault before teardown stops it.
		a.closing = true
		cancel := a.vaultCtxCancel
		a.vaultMu.Unlock()
		if cancel != nil {
			cancel()
		}
		a.vaultClosingWG.Wait()

		// Close the type + monitor watchers BEFORE taking the teardown Lock:
		// both Close() join their loop goroutines, whose handlers take vaultMu,
		// so closing them under the teardown Lock deadlocks (MB-1).
		a.stopWatchersOutsideLock()
		// Second hold: the teardown→save→reinit cutover. rollbackMove runs
		// under this lock (it does not acquire it itself — RWMutex is not
		// reentrant). initializeVaultServices resets closing=false at its top.
		a.vaultMu.Lock()
		defer a.vaultMu.Unlock()
		a.teardownVaultServices()
		if _, err := vault.UpdateSettings(func(s *vault.AppSettings) {
			s.VaultPath = dest
		}); err != nil {
			_ = a.rollbackMove(src, prior)
			return fmt.Errorf("move vault: save settings: %w", err)
		}
		if err := a.initializeVaultServices(dest); err != nil {
			if recoverErr := a.rollbackMove(src, prior); recoverErr != nil {
				return fmt.Errorf("move vault: init services at %s failed (%v); rollback to %s also failed (%v)", dest, err, src, recoverErr)
			}
			return fmt.Errorf("move vault: init services at %s failed — rolled back to %s (%v)", dest, src, err)
		}
		return nil
	}()
	if cutoverErr != nil {
		return vault.MoveVaultResult{}, cutoverErr
	}

	result := vault.MoveVaultResult{
		CopyResult: copyRes,
		From:       src,
		To:         dest,
	}

	// 4. Optional old-vault removal (non-fatal: the cutover already
	//    succeeded). First guard against an external edit to the source that
	//    landed after the copy snapshot — if so, keep the old folder in place
	//    rather than delete the user's unsaved-to-the-new-vault change. A
	//    permission/lock failure on the delete itself is also carried on
	//    RemoveOldErr + logged so it is never fully silent.
	if removeOld {
		if modified, mErr := vault.SourceModifiedAfter(src, copyDoneAt); mErr != nil {
			result.RemoveOldErr = fmt.Sprintf("could not verify source unchanged: %v", mErr)
			log.Printf("MoveVault: skip removeOld — source check failed for %s: %v", src, mErr)
		} else if modified {
			result.RemoveOldErr = "original vault was modified during the move; left in place"
			log.Printf("MoveVault: skip removeOld — %s modified after copy snapshot", src)
		} else if err := vault.RemoveOldVault(src); err != nil {
			result.RemoveOldErr = err.Error()
			log.Printf("MoveVault: failed to remove old vault at %s: %v", src, err)
		}
	}

	// 5. Notify the frontend to reset navigation + reload stores. If the
	//    optional old-vault removal didn't happen (source modified during
	//    cutover, or a delete permission/lock error), carry a warning so the
	//    user is told the original folder is still on disk — the move itself
	//    succeeded, so this is non-blocking (surfaced as a toast, not an error
	//    return).
	payload := map[string]string{
		"from": src,
		"to":   dest,
	}
	if result.RemoveOldErr != "" {
		payload["warning"] = "Vault moved, but the original folder could not be removed: " + result.RemoveOldErr
	}
	a.emit(EventVaultMoved, payload)
	return result, nil
}

// rollbackMove restores the active vault to originalPath after a failed
// cutover: persists the prior settings (verbatim, preserving theme/mode) and
// reinitializes services at the original path. The leftover verified copy is
// intentionally left in place — deleting it during error handling would risk
// data loss. Caller MUST hold vaultMu (it does not acquire it; RWMutex is not
// reentrant). Returns the reinit error, if any.
func (a *App) rollbackMove(originalPath string, prior *vault.AppSettings) error {
	// Never (re)initialize services against an empty path: absClean("") would
	// resolve to the working directory and pollute it with a .system/index.
	if originalPath == "" {
		return nil
	}
	_ = vault.SaveSettings(prior)
	return a.initializeVaultServices(originalPath)
}

// SwitchVault points Silt at an existing vault folder (e.g. one created by
// CopyVault) without a picker or scaffolding: teardown the active vault,
// persist settings.json to the new path, and reinit services there. The path
// must already contain a .system folder (CopyVault/MoveVault both produce
// valid vaults). Emits vault:moved so the frontend resets navigation.
func (a *App) SwitchVault(path string) error {
	a.wg.Add(1)
	defer a.wg.Done()

	if path == "" {
		return fmt.Errorf("empty vault path")
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("resolve vault path: %w", err)
	}
	if _, err := os.Stat(filepath.Join(abs, ".system")); err != nil {
		return fmt.Errorf("not a Silt vault (no .system folder): %s", path)
	}

	// Cutover under the exclusive write lock so concurrent readers can't race
	// the teardown/reinit. The prior-settings snapshot is read UNDER this lock
	// (not before it) so a concurrent settings.json writer (ApplyTheme) can't
	// commit a change that the cutover overwrites with a stale snapshot.
	// activePath is captured under the lock (before teardown nils it) for
	// rollback. rollbackMove runs under this same lock.
	//
	// The drain mirrors CloseVault: set closing, release the lock, wait for
	// in-flight AI calls (which don't hold vaultMu during HTTP), then re-lock
	// for the teardown→reinit cutover. Without it, the same #452 race would
	// strand an AI call into the newly-switched vault. Lifecycle transitions
	// are frontend-serialized (the onboarding flow is modal), so no second
	// transition enters the brief window between closing=true/Unlock and the
	// re-Lock for teardown; if that serialization ever changes, add a
	// closing-flag re-check or a vault-identity guard after the Wait.
	switchErr := func() error {
		a.vaultMu.Lock()
		activePath := a.vaultPath
		prior, _ := vault.LoadSettings()
		a.closing = true
		// Snapshot the cancel func under the lock so the post-Unlock read is
		// correct-by-construction (mirrors CloseVault).
		cancel := a.vaultCtxCancel
		a.vaultMu.Unlock()

		// Cancel the vault-scoped AI context so in-flight HTTP calls abort
		// promptly (context.Canceled) instead of blocking the drain for up
		// to the provider timeout (#471). Mirrors CloseVault.
		if cancel != nil {
			cancel()
		}
		a.vaultClosingWG.Wait()

		// Close the type + monitor watchers BEFORE taking the teardown Lock:
		// both Close() join their loop goroutines, whose handlers take vaultMu,
		// so closing them under the teardown Lock deadlocks (MB-1).
		a.stopWatchersOutsideLock()
		a.vaultMu.Lock()
		defer a.vaultMu.Unlock()
		a.teardownVaultServices()

		if _, err := vault.UpdateSettings(func(s *vault.AppSettings) {
			s.VaultPath = abs
		}); err != nil {
			if prior != nil {
				_ = a.rollbackMove(activePath, prior)
			}
			a.closing = false
			return fmt.Errorf("switch vault: save settings: %w", err)
		}
		// initializeVaultServices resets closing=false at the top, but if it
		// fails partway, mirror the UpdateSettings failure path (line 1041)
		// so the flag can never be left stuck on an init failure.
		if err := a.initializeVaultServices(abs); err != nil {
			if prior != nil {
				_ = a.rollbackMove(activePath, prior)
			}
			a.closing = false
			return fmt.Errorf("switch vault: init services: %w", err)
		}
		return nil
	}()
	if switchErr != nil {
		return switchErr
	}
	a.emit(EventVaultMoved, map[string]string{
		"from": "",
		"to":   abs,
	})
	return nil
}
