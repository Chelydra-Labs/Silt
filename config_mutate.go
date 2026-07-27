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
