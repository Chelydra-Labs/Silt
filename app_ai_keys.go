package main

// =========================================================================
// AI API key storage — OS keyring integration (#218, #761)
// =========================================================================
//
// Resolves, sets, copies, and migrates provider API keys between the OS
// keyring and plaintext config.yaml. Split from app_ai.go so provider-config
// CRUD stays cohesive while keyring I/O (and its lock-ordering docs) live in
// one place. Callers: GetAIProviderConfig / SetAIAPIKey / withAIPreflight /
// vault open (migrateAIKeysToKeyringLocked).
//
// Lock ordering: vaultMu before configMu. Keyring I/O runs without configMu
// when possible so a D-Bus timeout cannot stall unrelated config readers.
// Callers that already hold vaultMu must use the *Locked migrate/set helpers
// (RWMutex is not re-entrant).

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"silt/backend/keyring"
	"strings"
)

// keyringService is the OS-keyring service name under which Silt stores AI
// provider keys (#218). The "user" half is vault-scoped (see aiKeyringUser) so
// two vaults on one machine keep separate keys.
const keyringService = "Silt"

// aiKeyringUser derives the vault-scoped OS-keyring "user" identifier for one
// provider (#218). It is SHA-8(vaultPath):which — stable for a given vault on a
// given machine, distinct across vaults, and carries the provider kind. The
// vault PATH (not a content fingerprint) is the scope on purpose: when a vault
// moves to a new machine the keys do not travel with it (a documented tradeoff;
// the user re-enters), and on the original machine the keys survive an index
// rebuild or content change. Reads a.vaultPath; callers hold vaultMu (or call
// before the snapshot is released).
func (a *App) aiKeyringUser(which string) string {
	h := sha256.Sum256([]byte(filepath.Clean(a.vaultPath)))
	return fmt.Sprintf("ai:%x:%s", h[:8], which)
}

// aiUseKeyringLocked reports whether keyring storage is enabled. MUST be called
// under configMu. nil (unset) reads as the default-true.
func (a *App) aiUseKeyringLocked() bool {
	return a.cfg.AI.UseKeyring == nil || *a.cfg.AI.UseKeyring
}

// resolveAIKeyUnlocked resolves a provider API key without taking configMu.
// It does not take vaultMu either, but callers MAY hold vaultMu.RLock across
// the call (CopyAIAPIKey / setAIAPIKeyLocked do, so a slow keyring cannot race
// SwitchVault). Prefer releasing locks for pure read paths (GetAIProviderConfig)
// so keyring latency does not stall unrelated vault writers. Tries the OS
// keyring first when enabled + present, and falls back to the config value on
// not-found OR keyring-unavailable (headless Linux / locked session). The
// returned unavailable flag lets the caller surface a one-time warning.
func (a *App) resolveAIKeyUnlocked(user string, useKeyring bool, configKey string) (key string, unavailable bool) {
	if !useKeyring || a.keyringStore == nil {
		return strings.TrimSpace(configKey), false
	}
	k, err := a.keyringStore.Get(keyringService, user)
	if err == nil {
		return strings.TrimSpace(k), false
	}
	if errors.Is(err, keyring.ErrNotFound) {
		// Normal: key not in the keyring yet (or migrated out). Use config.
		return strings.TrimSpace(configKey), false
	}
	// ErrUnavailable or any other platform error: fall back to config and flag
	// it so the page can show "OS keyring unavailable; key stored in config".
	return strings.TrimSpace(configKey), true
}

// setAIAPIKeyLocked writes a provider API key. Caller MUST hold vaultMu.RLock
// for the whole call so SwitchVault/MoveVault cannot cut over mid-write.
// configMu is taken internally (R then W); do not hold configMu when calling.
// Used by SetAIAPIKey and CopyAIAPIKey (#641) to avoid RWMutex re-entrancy.
func (a *App) setAIAPIKeyLocked(which, key string) error {
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	key = strings.TrimSpace(key)
	// Read the toggle + derive the keyring user under configMu, then release
	// before any keyring I/O.
	a.configMu.RLock()
	useKeyring := a.aiUseKeyringLocked()
	user := a.aiKeyringUser(which)
	a.configMu.RUnlock()

	keyringStored := false
	if useKeyring && a.keyringStore != nil {
		if err := a.keyringStore.Set(keyringService, user, key); err == nil {
			keyringStored = true
		}
		// On ErrUnavailable (or any error) we fall through to config below.
	}

	a.configMu.Lock()
	defer a.configMu.Unlock()
	configKey := ""
	if !keyringStored {
		// Keyring off/unavailable: keep the key in config so the feature works.
		configKey = key
	}
	// keyringStored → blank config (the key lives off plaintext disk now).
	if which == "chat" {
		a.cfg.AI.Chat.APIKey = configKey
	} else {
		a.cfg.AI.Embedding.APIKey = configKey
	}
	if err := a.saveConfigTracked(a.cfg); err != nil {
		return err
	}
	// A key change may flip a 401 list-endpoint to success (or vice versa) —
	// drop the cached list so the next poll reflects the new credentials.
	a.invalidateAIModelCache(which)
	return nil
}

// CopyAIAPIKey migrates a provider's API key into the other role's slot
// entirely server-side, so the secret never crosses to the renderer. It backs
// the "Sync providers" toggle: switching sync on should make embedding share
// chat's existing key without forcing the user to re-enter it, and the frontend
// has no way to read the key value (GetAIProviderConfig exposes only HasKey).
//
// No-op (returns nil) when the source has no key, so toggling sync for a
// keyless provider does not error or clobber the destination. Resolves the
// source via the same keyring-first/config-fallback path as every other key
// read, then stores via setAIAPIKeyLocked under one continuous vaultMu.RLock
// so a concurrent SwitchVault/MoveVault cannot retarget the write to a
// different vault (#641).
func (a *App) CopyAIAPIKey(from, to string) error {
	if err := aiValidateWhich(from); err != nil {
		return err
	}
	if err := aiValidateWhich(to); err != nil {
		return err
	}
	if from == to {
		return nil
	}
	// Hold vaultMu.R for the whole copy: resolve + destination write must
	// observe the same vault identity. RWMutex is not re-entrant, so we call
	// setAIAPIKeyLocked (not public SetAIAPIKey). Keyring I/O under R is the
	// same tradeoff as SetAIAPIKey — blocks writers (switch/move), not readers.
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	a.configMu.RLock()
	useKeyring := a.aiUseKeyringLocked()
	fromUser := a.aiKeyringUser(from)
	fromConfigKey := aiConfigBlock(a.cfg.AI, from).APIKey
	a.configMu.RUnlock()

	key, _ := a.resolveAIKeyUnlocked(fromUser, useKeyring, fromConfigKey)
	if key == "" {
		return nil
	}
	return a.setAIAPIKeyLocked(to, key)
}

// SetUseKeyring toggles whether AI provider keys are stored in the OS keyring
// (default true) vs plaintext config.yaml (#218). When turning keyring ON with
// a keyring store present and a key already in config, it migrates that key into
// the keyring immediately so the user sees the plaintext value leave config
// without a restart. When turning it OFF, it does NOT move keys back to config
// (the user opted out of keyring storage; a subsequent SetAIAPIKey will land in
// config, and a key still in the keyring remains resolvable until cleared).
func (a *App) SetUseKeyring(on bool) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	a.configMu.Lock()
	a.cfg.AI.UseKeyring = boolPtrAI(on)
	err := a.saveConfigTracked(a.cfg)
	a.configMu.Unlock()
	if err != nil {
		return err
	}
	// Turning keyring on: opportunistically migrate any plaintext keys now so
	// the user sees them leave config.yaml without a restart. We already hold
	// vaultMu.R — use the locked variant (do not re-enter vaultMu).
	if on {
		a.migrateAIKeysToKeyringLocked()
	}
	return nil
}

// boolPtrAI is the *bool helper local to AI keyring/config toggles (the config
// package's unexported boolPtr isn't visible here).
func boolPtrAI(b bool) *bool { return &b }

// migrateAIKeysToKeyring moves any plaintext AI API keys found in config.yaml
// into the OS keyring on first run after upgrade (#218). Idempotent: once a key
// is in the keyring the config field is blanked, so a re-run finds nothing to
// migrate. Best-effort and silent — if the keyring is unavailable, the plaintext
// key is LEFT in config (the documented fallback; the provider page surfaces the
// unavailability).
//
// Acquires vaultMu.RLock. Callers that already hold vaultMu (R or exclusive)
// must use migrateAIKeysToKeyringLocked instead — RWMutex is not re-entrant for
// Lock→RLock, and initializeVaultServices runs under exclusive Lock (#654).
func (a *App) migrateAIKeysToKeyring() {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.migrateAIKeysToKeyringLocked()
}

// migrateAIKeysToKeyringLocked is the lock-held body of migrateAIKeysToKeyring.
// Caller MUST hold vaultMu (RLock or Lock). Does not acquire vaultMu.
// Keyring I/O runs under the caller's vaultMu hold so SwitchVault/MoveVault
// cutover cannot retarget the write; vault open/switch is rare and user-driven.
func (a *App) migrateAIKeysToKeyringLocked() {
	if a.keyringStore == nil {
		return
	}
	if a.vaultPath == "" {
		return
	}
	a.configMu.RLock()
	useKeyring := a.aiUseKeyringLocked()
	type pending struct{ which, user, key string }
	var todo []pending
	for _, which := range []string{"chat", "embedding"} {
		if k := strings.TrimSpace(aiConfigBlock(a.cfg.AI, which).APIKey); k != "" {
			todo = append(todo, pending{which, a.aiKeyringUser(which), k})
		}
	}
	a.configMu.RUnlock()
	if !useKeyring || len(todo) == 0 {
		return
	}
	// Keyring writes without configMu (may be slow on some platforms).
	migrated := map[string]bool{}
	for _, p := range todo {
		if err := a.keyringStore.Set(keyringService, p.user, p.key); err == nil {
			migrated[p.which] = true
		}
	}
	if len(migrated) == 0 {
		return
	}
	// Blank the migrated config fields and persist once.
	a.configMu.Lock()
	defer a.configMu.Unlock()
	changed := false
	for which := range migrated {
		if aiConfigBlock(a.cfg.AI, which).APIKey != "" {
			if which == "chat" {
				a.cfg.AI.Chat.APIKey = ""
			} else {
				a.cfg.AI.Embedding.APIKey = ""
			}
			changed = true
		}
	}
	if !changed {
		return
	}
	_ = a.saveConfigTracked(a.cfg)
}
