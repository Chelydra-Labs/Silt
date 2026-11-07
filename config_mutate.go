package main

import (
	"fmt"
	"reflect"

	"silt/backend/config"
)

// mutateConfig is the only navigation/config write primitive. It holds the
// lifecycle read lock and config write lock through the atomic save, so every
// caller merges against the latest in-memory config rather than persisting a
// stale frontend snapshot.
func (a *App) mutateConfig(mut func(*config.SystemConfig) error) error {
	_, _, err := a.mutateConfigWithResult(mut)
	return err
}

// mutateConfigWithResult is the serialized navigation/config mutation path.
// The returned snapshot is only valid when changed is true and is emitted by
// callers after the config and lifecycle locks have been released.
func (a *App) mutateConfigWithResult(mut func(*config.SystemConfig) error) (config.SystemConfig, bool, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return config.SystemConfig{}, false, fmt.Errorf("vault not loaded")
	}

	a.configMu.Lock()
	defer a.configMu.Unlock()
	next := config.Clone(a.cfg)
	if err := mut(&next); err != nil {
		return config.SystemConfig{}, false, err
	}
	next = config.Normalize(next)
	if reflect.DeepEqual(next, a.cfg) {
		return config.SystemConfig{}, false, nil
	}
	if err := a.saveConfigTracked(next); err != nil {
		return config.SystemConfig{}, false, err
	}
	a.cfg = next
	return next, true, nil
}

// mutateConfigFresh applies a targeted single-field delta after re-reading
// config.yaml under configMu. Targeted setters use this so an external edit to
// an unrelated section that landed after the last config.Load is merged into
// the save rather than clobbered by stale memory, shrinking the loss window to
// the lock-hold duration. The mutator returns changed=false for idempotent
// no-ops, which skips the write. A re-read failure refuses loudly — overwriting
// a corrupt file with stale memory would destroy the user's only signal.
//
// Deliberate exceptions: whole-config writers (SaveSystemConfig), and writers
// whose in-memory state must win over disk (backend-owned navigation/registry
// reconciliation, vault-init migration).
func (a *App) mutateConfigFresh(mut func(*config.SystemConfig) (changed bool, err error)) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	return a.mutateConfigFreshLocked(mut)
}

// mutateConfigFreshLocked is the lifecycle-locked form of mutateConfigFresh,
// used by operations that already hold vaultMu.RLock (e.g. SetAIAPIKey and
// ClearAIAPIKey, which hold vaultMu.RLock across keyring I/O to prevent vault
// cutover mid-write). Re-entering an RWMutex read lock is unsafe when a lifecycle
// writer is queued, so those callers use this variant.
func (a *App) mutateConfigFreshLocked(mut func(*config.SystemConfig) (changed bool, err error)) error {
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}

	a.configMu.Lock()
	defer a.configMu.Unlock()
	fresh, loadErr := config.Load(a.vaultPath)
	if loadErr != nil {
		return fmt.Errorf("cannot read config.yaml before update: %w", loadErr)
	}
	changed, err := mut(&fresh)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	a.cfg = fresh
	return a.saveConfigTracked(a.cfg)
}

// mutateConfigLocked is the lifecycle-locked form used by content mutations
// that already hold vaultMu.RLock. Re-entering an RWMutex read lock is unsafe
// when a lifecycle writer is queued, so those callers use this variant.
func (a *App) mutateConfigLocked(mut func(*config.SystemConfig) error) error {
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}

	a.configMu.Lock()
	defer a.configMu.Unlock()
	next := config.Clone(a.cfg)
	if err := mut(&next); err != nil {
		return err
	}
	next = config.Normalize(next)
	if reflect.DeepEqual(next, a.cfg) {
		return nil
	}
	if err := a.saveConfigTracked(next); err != nil {
		return err
	}
	a.cfg = next
	return nil
}
