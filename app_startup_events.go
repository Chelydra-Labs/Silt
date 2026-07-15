package main

import (
	"fmt"

	"silt/backend/vault"
)

// Startup-event replay + the F20 / F4 user-acknowledgement bindings. These
// run before / just after the frontend mounts its Events.On listeners, so the
// queue + emit helpers (in wails_runtime.go) bridge the race.

// GetStartupEvents returns events queued during ServiceStartup (before the
// frontend mounted its Events.On listeners) and clears the queue. In Wails v3,
// ServiceStartup fires before the webview exists, so emits like
// vault:init-error and settings:fingerprint-mismatch are lost; emitOrQueue
// stashed them for this retrieval. The frontend calls this on mount (after
// MarkFrontendReady) and dispatches each entry through the same handler its
// Events.On listener would have used.
func (a *App) GetStartupEvents() []startupEvent {
	a.startupEventsMu.Lock()
	defer a.startupEventsMu.Unlock()
	out := a.startupEvents
	a.startupEvents = nil
	return out
}

// MarkFrontendReady signals that the frontend has finished registering its
// Events.On listeners. After this, emitOrQueue stops queueing (events reliably
// reach the live listeners). Call once on mount, before GetStartupEvents.
// Idempotent.
func (a *App) MarkFrontendReady() {
	a.startupEventsMu.Lock()
	a.frontendReady = true
	a.startupEventsMu.Unlock()
}

// ConfirmSettingsChange is the F20 user-ack binding. When the frontend detects
// a settings:fingerprint-mismatch event (the trust-anchor fields in
// settings.json changed since last launch), it shows a confirmation dialog;
// if the user confirms the change was intentional, it calls this binding,
// which updates the on-disk fingerprint to match the current values so the
// next launch proceeds without a prompt. A user who rejects the dialog can
// manually fix settings.json; the mismatch persists across relaunches until
// either the values are restored or the user confirms.
func (a *App) ConfirmSettingsChange() error {
	if _, err := vault.ConfirmSettingsChange(); err != nil {
		return fmt.Errorf("confirm settings change: %w", err)
	}
	return nil
}

// ConfirmGrantsMigration is the F4 user-ack binding. When the frontend detects
// a grants:migration-required event (the vault's legacy config.yaml carries a
// grants block this host has never seen), it shows a one-time confirmation
// dialog. If the user confirms, this binding:
//  1. Merges the legacy grants into the per-host store (preserving any grants
//     the host already has — e.g. first-party seeds).
//  2. Persists the merged store to grants.json.
//  3. Rewrites config.yaml WITHOUT the grants block (the field is already
//     gone from the struct, so a normalize + Save strips it from disk).
//
// If the user denies, the host store keeps its first-party seeds only; every
// third-party plugin re-prompts on first use (the safe default).
func (a *App) ConfirmGrantsMigration(legacyGrants map[string]map[string]string) error {
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	// Merge legacy grants into the host store. First-party IDs are skipped —
	// they are always seeded implicitly and the user never granted them
	// manually.
	if a.grants == nil {
		a.grants = vault.GrantsStore{}
	}
	for pid, caps := range legacyGrants {
		if isFirstPartyPlugin(pid) {
			continue
		}
		if a.grants[pid] == nil {
			a.grants[pid] = map[string]string{}
		}
		for cap, qual := range caps {
			a.grants[pid][cap] = qual
		}
	}
	if err := vault.SaveGrants(a.grants); err != nil {
		return fmt.Errorf("persist migrated grants: %w", err)
	}
	// Rewrite config.yaml so the legacy grants block is stripped from disk.
	// The struct no longer has a Grants field, so a round-trip through Save
	// drops it. saveConfigTracked arms the self-write window and clears it if
	// the save fails.
	if err := a.saveConfigTracked(a.cfg); err != nil {
		return fmt.Errorf("strip legacy grants from config.yaml: %w", err)
	}
	a.emitPluginsChanged()
	return nil
}

// DeclineGrantsMigration is the F4 user-decline binding. When the user
// dismisses the grants-migration dialog, this strips the legacy grants:
// block from config.yaml so the dialog does NOT re-fire on the next launch.
// The host store keeps its first-party seeds only; every third-party plugin
// re-prompts on first use (the safe default). The user's third-party grants
// are lost — they chose not to migrate.
func (a *App) DeclineGrantsMigration() error {
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	// config.Save drops the grants field (it's gone from the struct), so
	// the on-disk file no longer carries the legacy block. saveConfigTracked
	// arms the self-write window and clears it if the save fails.
	if err := a.saveConfigTracked(a.cfg); err != nil {
		return fmt.Errorf("strip legacy grants from config.yaml: %w", err)
	}
	return nil
}
