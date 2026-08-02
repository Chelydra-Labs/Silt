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
	a.emit(EventTypesChanged, struct{}{})
	log.Printf("types: DeleteType → removed %q", id)
	return nil
}

// ReloadTypes forces a re-scan of the types directory + cache flush. Used by
// the type watcher's onChange callback (external edit detected) and available
// as a manual refresh. Emits types:changed.
func (a *App) ReloadTypes() error {
	a.wg.Add(1)
	defer a.wg.Done()
	types.InvalidateTypesCache()
	a.emit(EventTypesChanged, struct{}{})
	return nil
}
