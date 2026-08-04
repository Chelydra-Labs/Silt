package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"silt/backend/types"
	"silt/backend/vault"
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
	// Derive the on-disk id before arming self-write suppression. The type
	// editor always sends id:"" and types.SaveType fills it via TypeIDFromName
	// — arming <typesDir>/.yaml would miss the real <id>.yaml write and the
	// watcher would fire a redundant InvalidateTypesCache + reproject.
	saveID := strings.TrimSpace(td.ID)
	if saveID == "" {
		saveID = types.TypeIDFromName(td.Name)
	}
	typePath := filepath.Join(a.typesDir(), saveID+".yaml")
	if a.typeWatcher != nil {
		// Path- and content-scoped: arm the exact bytes SaveType will write
		// (ID is yaml:"-" so SerializeType is independent of the derived
		// id). Only this file's events are deferred; on debounce the watcher
		// reads the file and compares — a match is a confirmed self-write
		// (suppressed), a mismatch means an external/sync edit landed on top
		// of our save and onChange fires anyway. A coincident edit to another
		// type still fires (path-scoped).
		a.typeWatcher.RegisterSelfWrite(typePath, types.SerializeType(&td))
	}
	if a.tracker != nil {
		a.tracker.RegisterWrite(typePath)
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
	typePath := filepath.Join(a.typesDir(), id+".yaml")
	if a.typeWatcher != nil {
		// nil expected arms a delete: the watcher treats a missing file as
		// a confirmed self-write (suppressed) and a post-delete recreate as
		// an external edit (reload fires).
		a.typeWatcher.RegisterSelfWrite(typePath, nil)
	}
	if a.tracker != nil {
		a.tracker.RegisterWrite(typePath)
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

// RestoreExampleTypes re-seeds the shipped example note types (Book, Meeting)
// into <vault>/.system/types/ when absent — the quick unblock for a user who has
// no types and hit the empty-state dead-end. Idempotent: a type whose id already
// exists is left untouched so the user's edits survive a repeat call. Returns
// the ids of the types it actually created (empty if all already existed).
// Mirrors SaveType's write path (validate + atomic write, self-write
// suppression) but batches emit + re-project across the whole set.
func (a *App) RestoreExampleTypes(ctx context.Context) ([]string, error) {
	_ = ctx
	a.vaultMu.Lock()
	defer a.vaultMu.Unlock()
	a.wg.Add(1)
	defer a.wg.Done()
	if a.vaultPath == "" {
		return nil, fmt.Errorf("vault not loaded")
	}
	// Existing ids guard the per-type write so a repeat call never overwrites
	// the user's edits. CachedListTypes is mtime-aware and, under the exclusive
	// lock, no concurrent writer can be mid-flight.
	existing := map[string]bool{}
	if res, err := types.CachedListTypes(a.typesDir()); err == nil {
		for _, t := range res.Types {
			existing[t.ID] = true
		}
	}
	var created []string
	for _, td := range vault.ExampleTypes() {
		if existing[td.ID] {
			continue
		}
		typePath := filepath.Join(a.typesDir(), td.ID+".yaml")
		// Path-scoped per file so a coincident external edit to another type
		// is not dropped while we restore the examples. Content-scoped so an
		// external edit landing on top of one of these writes still reloads.
		if a.typeWatcher != nil {
			a.typeWatcher.RegisterSelfWrite(typePath, types.SerializeType(td))
		}
		if a.tracker != nil {
			a.tracker.RegisterWrite(typePath)
		}
		if err := types.SaveType(a.typesDir(), td); err != nil {
			if a.typeWatcher != nil {
				a.typeWatcher.UnregisterSelfWrite()
			}
			log.Printf("types: RestoreExampleTypes(%q) failed: %v", td.ID, err)
			// Surface any types already written this batch so the picker /
			// dashboard do not stay stale until an unrelated refresh.
			if len(created) > 0 {
				types.InvalidateTypesCache()
				a.reprojectAllTypedPages()
				a.emit(EventTypesChanged, struct{}{})
			}
			return created, fmt.Errorf("restore example type %q: %w", td.ID, err)
		}
		created = append(created, td.ID)
	}
	if len(created) > 0 {
		types.InvalidateTypesCache()
		// Re-project once for the batch (same rationale as SaveType: the
		// self-write window suppressed the watcher's onChange).
		a.reprojectAllTypedPages()
		a.emit(EventTypesChanged, struct{}{})
	}
	log.Printf("types: RestoreExampleTypes → created %d %v", len(created), created)
	return created, nil
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
