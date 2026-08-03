package main

import (
	"fmt"
	"log"
	"path/filepath"

	"silt/backend/types"
)

// typesDir returns the on-disk user-type directory <vault>/.system/types/,
// mirroring templatesDir. Returns "" when no vault is open (the type manager
// is unavailable pre-vault).
func (a *App) typesDir() string {
	if a.vaultPath == "" {
		return ""
	}
	return filepath.Join(a.vaultPath, ".system", "types")
}

// ListTypes enumerates every valid user-defined type in <vault>/.system/types/
// plus any per-file load errors (so the type manager can name a broken file).
// Works before a vault is open (returns an empty result). Uses the mtime-aware
// cache so repeated calls during type-picker rendering are cheap.
func (a *App) ListTypes() (*types.ListTypesResult, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	return types.CachedListTypes(a.typesDir())
}

// GetType resolves a single type by id via an O(1) direct file lookup and
// returns its full schema. Returns a user-facing error when the id is absent.
func (a *App) GetType(id string) (types.TypeDef, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	if id == "" {
		return types.TypeDef{}, fmt.Errorf("type id is required")
	}
	td, err := types.GetType(a.typesDir(), id)
	if err != nil {
		return types.TypeDef{}, err
	}
	return *td, nil
}

// ResolveTypeID resolves a frontmatter `type:` reference (the canonical id, a
// case-insensitive id, or the display Name) to its canonical id, so a page may
// store `type: Book` by name while the index/UI work with the lowercased id.
// Returns a user-facing error when nothing matches.
func (a *App) ResolveTypeID(ref string) (string, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	return types.ResolveTypeID(a.typesDir(), ref)
}

// SaveType validates td, derives an id when one is not set, and writes the
// canonical form atomically to <vault>/.system/types/<id>.yaml. The type
// watcher's self-write window is armed before the write (and cleared on
// failure) so the resulting fsnotify events do not trigger a redundant reload.
// Emits types:changed so the type manager and any open typed page re-resolve.
// Mirrors App.SaveUserTemplate.
func (a *App) SaveType(td types.TypeDef) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	if a.typeWatcher != nil {
		a.typeWatcher.RegisterSelfWrite()
	}
	if a.tracker != nil {
		a.tracker.RegisterWrite(filepath.Join(a.typesDir(), td.ID+".yaml"))
	}
	if err := types.SaveType(a.typesDir(), &td); err != nil {
		if a.typeWatcher != nil {
			a.typeWatcher.UnregisterSelfWrite()
		}
		log.Printf("types: SaveType(%q) failed: %v", td.ID, err)
		return err
	}
	types.InvalidateTypesCache()
	// Re-project now: the type watcher suppresses fsnotify events for this
	// method's own atomic write (RegisterSelfWrite above), so its onChange —
	// the only other caller of reprojectAllTypedPages — never fires for in-app
	// schema edits, and typed pages' projections would drift until restart.
	// Safe under the held RLock: reprojectAllTypedPages only reads a.db /
	// a.vaultPath (handle-based DB locking) and does not re-acquire vaultMu.
	a.reprojectAllTypedPages()
	a.emit(EventTypesChanged, struct{}{})
	log.Printf("types: SaveType → saved %q", td.ID)
	return nil
}

// DeleteType removes the on-disk type with the given id. Idempotent (deleting
// an already-deleted type is a no-op success). Emits types:changed. Mirrors
// App.DeleteUserTemplate.
func (a *App) DeleteType(id string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	if a.typeWatcher != nil {
		a.typeWatcher.RegisterSelfWrite()
	}
	if a.tracker != nil {
		a.tracker.RegisterWrite(filepath.Join(a.typesDir(), id+".yaml"))
	}
	if err := types.DeleteType(a.typesDir(), id); err != nil {
		if a.typeWatcher != nil {
			a.typeWatcher.UnregisterSelfWrite()
		}
		log.Printf("types: DeleteType(%q) failed: %v", id, err)
		return err
	}
	types.InvalidateTypesCache()
	// Re-project now: the type watcher suppresses fsnotify events for this
	// method's own atomic write (RegisterSelfWrite above), so its onChange —
	// the only other caller of reprojectAllTypedPages — never fires for in-app
	// schema edits, and typed pages' projections would drift until restart.
	// Safe under the held RLock: reprojectAllTypedPages only reads a.db /
	// a.vaultPath (handle-based DB locking) and does not re-acquire vaultMu.
	a.reprojectAllTypedPages()
	a.emit(EventTypesChanged, struct{}{})
	log.Printf("types: DeleteType → removed %q", id)
	return nil
}

// ReloadTypes forces a re-scan of the types directory + cache flush, exposed as
// the manual type-refresh IPC. Re-projects every typed page so the dashboard
// reflects schema changes that arrived between the watcher's last onChange and
// this call (e.g. the watcher was briefly disabled, or types were touched while
// Silt was starting). The watcher's own onChange path re-projects independently
// (vault_init.go), so this covers the manual-refresh gap. Returns nil when no
// vault is open (nothing to reload).
func (a *App) ReloadTypes() error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.wg.Add(1)
	defer a.wg.Done()
	if a.vaultPath == "" {
		return nil
	}
	types.InvalidateTypesCache()
	// Mirror SaveType/DeleteType: re-project under the held RLock so manual
	// refresh reaches typed pages immediately (the watcher path is the only
	// other caller, so without this the dashboard drifts until a page mutation
	// or restart). Safe under RLock: reprojectAllTypedPages only reads a.db /
	// a.vaultPath and does not re-acquire vaultMu.
	a.reprojectAllTypedPages()
	a.emit(EventTypesChanged, struct{}{})
	return nil
}

// PluginListTypes is the read-only SDK wrapper for ListTypes. Type schemas are
// treated like template definitions — visible to every loaded plugin without a
// capability grant (a plugin building a typed view needs the schema to
// interpret frontmatter it already sees via read-files). Session-token verified
// so an SDK-less caller cannot probe the type roster by impersonating another
// plugin.
func (a *App) PluginListTypes(pluginID, sessionToken string) (*types.ListTypesResult, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return nil, err
	}
	return a.ListTypes()
}

// PluginGetType is the read-only SDK wrapper for GetType. Same grant posture as
// PluginListTypes (schema read is non-privileged). Session-token verified.
func (a *App) PluginGetType(pluginID, sessionToken, id string) (types.TypeDef, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return types.TypeDef{}, err
	}
	return a.GetType(id)
}

// PluginSaveType is the SDK wrapper for SaveType, gated under CapContentMutate
// because editing a type schema rewrites a vault asset (a .system/types/*.yaml
// file) and reshapes every typed page's projection. Mirrors the existing
// wrapPluginMutate gating the task setters use. Session-token verified.
func (a *App) PluginSaveType(pluginID, sessionToken string, td types.TypeDef) (bool, error) {
	return a.wrapPluginMutate(pluginID, sessionToken, func() error {
		return a.SaveType(td)
	})
}

// PluginDeleteType is the SDK wrapper for DeleteType, gated under
// CapContentMutate (same rationale as PluginSaveType — removing a vault asset).
// Session-token verified.
func (a *App) PluginDeleteType(pluginID, sessionToken, id string) (bool, error) {
	return a.wrapPluginMutate(pluginID, sessionToken, func() error {
		return a.DeleteType(id)
	})
}
