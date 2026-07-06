package main

// =========================================================================
// AI provider bindings (#216)
// =========================================================================
//
// This file exposes the core AI service (backend/ai) over the Wails bridge.
// Two consumers reach it:
//
//   - Plugins, via PluginAIComplete / PluginAIEmbed. These are gated exactly
//     like PluginFetch: session token → requireGrant(CapAI) → rate limiter →
//     size cap → service call → audit. Plugins NEVER receive credentials; the
//     provider config + resolved key are snapshotted server-side and handed to
//     the service as a value.
//   - The first-party AI Provider settings page, via GetAIProviderConfig /
//     UpdateAIProviderConfig / SetAIAPIKey / ClearAIAPIKey / TestAIConnection.
//     These are NOT capability-gated (they are core cross-cutting settings, not
//     plugin calls) but they never return a raw key — GetAIProviderConfig emits
//     only a HasKey flag.
//
// Lock ordering follows the app.go invariant (vaultMu before configMu). The
// plugin bindings snapshot the provider config under the locks, RELEASE them,
// and only then perform the (potentially long) HTTP call so an LLM completion
// cannot hold vaultMu for 60s. The snapshot is a value copy, so no lock is
// needed during the call. Auditing happens after the call with no configMu/vaultMu
// held (auditAI uses its own mutex).

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"silt/backend/ai"
	"silt/backend/config"
	"silt/backend/keyring"
	"silt/backend/plugins"
	"strings"
)

// keyringService is the OS-keyring service name under which Silt stores AI
// provider keys (#218). The "user" half is vault-scoped (see aiKeyringUser) so
// two vaults on one machine keep separate keys.
const keyringService = "Silt"

// aiContext returns the app-lifecycle context for AI HTTP calls, falling back
// to context.Background() when the App wasn't initialized via startup() (tests).
// Production sets aiCtx in startup() and cancels it in shutdown() so in-flight
// completions/embeddings don't outlive the process.
func (a *App) aiContext() context.Context {
	if a.aiCtx != nil {
		return a.aiCtx
	}
	return context.Background()
}

// validateAIBaseURL rejects base URLs that are not http(s) so a file:/// or
// empty-scheme typo is caught with a clear message instead of a confusing
// transport error from the HTTP client.
func validateAIBaseURL(raw string) error {
	u := strings.TrimSpace(raw)
	if u == "" {
		return nil // empty is allowed (NormalizeAIConfig fills the local default)
	}
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return fmt.Errorf("base URL must start with http:// or https://")
	}
	return nil
}

// AIProviderPatch is the input to UpdateAIProviderConfig: one provider block
// MINUS the API key. The key is never part of a generic patch — it moves through
// the dedicated SetAIAPIKey / ClearAIAPIKey bindings so it can be routed to the
// OS keyring (#218) rather than plaintext config.
type AIProviderPatch struct {
	ProviderType    string   `json:"provider_type"`
	BaseURL         string   `json:"base_url"`
	Model           string   `json:"model"`
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxTokens       *int     `json:"max_tokens,omitempty"`
	ReasoningEffort *string  `json:"reasoning_effort,omitempty"`
	TimeoutMs       *int     `json:"timeout_ms,omitempty"`
	Dimensions      *int     `json:"dimensions,omitempty"`
}

// AIPublicProvider is the API-key-scrubbed view of one provider block returned
// to the frontend. HasKey is true when a key is stored (config in Phase 1;
// config-or-keyring in #218) so the page can show "key set" without ever
// receiving the secret.
type AIPublicProvider struct {
	ProviderType    string   `json:"provider_type"`
	BaseURL         string   `json:"base_url"`
	Model           string   `json:"model"`
	HasKey          bool     `json:"has_key"`
	Temperature     *float64 `json:"temperature,omitempty"`
	MaxTokens       *int     `json:"max_tokens,omitempty"`
	ReasoningEffort *string  `json:"reasoning_effort,omitempty"`
	TimeoutMs       *int     `json:"timeout_ms,omitempty"`
	Dimensions      *int     `json:"dimensions,omitempty"`
}

// AIPublicConfig is the AI Provider page's full read view: both provider blocks
// (key-scrubbed) plus the keyring toggle and availability state. KeyringAvailable
// is false when no keyring store is wired (tests); KeyringUnusableFor lists the
// provider kinds ("chat"/"embedding") whose keyring lookup reported unavailable
// (headless Linux / locked session) so the page can show a fallback warning.
type AIPublicConfig struct {
	Chat               AIPublicProvider `json:"chat"`
	Embedding          AIPublicProvider `json:"embedding"`
	UseKeyring         bool             `json:"use_keyring"`
	KeyringAvailable   bool             `json:"keyring_available"`
	KeyringUnusableFor []string         `json:"keyring_unusable_for,omitempty"`
}

// PluginAIChatMessage is one chat message crossing the plugin→service boundary.
type PluginAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// PluginAICompleteInput is the plugin-side request envelope for a chat
// completion. Stream is accepted (and forwarded as false) so the signature is
// additive when Sprint 22 delivers streaming.
type PluginAICompleteInput struct {
	Messages        []PluginAIChatMessage `json:"messages"`
	Model           string                `json:"model,omitempty"`
	Temperature     *float64              `json:"temperature,omitempty"`
	MaxTokens       *int                  `json:"max_tokens,omitempty"`
	ReasoningEffort *string               `json:"reasoning_effort,omitempty"`
	Stream          bool                  `json:"stream,omitempty"`
}

// PluginAIEmbedInput is the plugin-side request envelope for an embedding batch.
type PluginAIEmbedInput struct {
	Texts      []string `json:"texts"`
	Model      string   `json:"model,omitempty"`
	Dimensions *int     `json:"dimensions,omitempty"`
}

// AIProbeResult is the outcome of a Test Connection call. On failure, Kind is
// the normalized ai.AIErrorKind and Message is the provider's trimmed error body.
type AIProbeResult struct {
	OK      bool   `json:"ok"`
	Kind    string `json:"kind,omitempty"`
	Message string `json:"message,omitempty"`
}

// aiChatKind / aiEmbedKind are the audit "kind" tags written to the AI audit log.
const (
	aiChatKind = "chat"
	aiEmbKind  = "embed"
)

// aiValidateWhich rejects anything that is not exactly "chat" or "embedding" so
// a typo cannot index into a map or branch wrongly downstream.
func aiValidateWhich(which string) error {
	if which != "chat" && which != "embedding" {
		return fmt.Errorf(`which must be "chat" or "embedding"`)
	}
	return nil
}

// aiConfigBlock returns a copy of the requested provider block. MUST be called
// under configMu.
func aiConfigBlock(cfg config.AIConfig, which string) config.AIProviderConfig {
	if which == "embedding" {
		return cfg.Embedding
	}
	return cfg.Chat
}

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

// resolveAIKeyUnlocked resolves a provider API key with NO configMu/vaultMu held
// (the caller has already snapshotted what it needs — including the precomputed
// keyring user key — and released the locks, so a slow/unavailable keyring
// cannot stall config or vault access). It tries the OS keyring first when
// enabled + present, and falls back to the config value on not-found OR on
// keyring-unavailable (headless Linux / locked session). The returned
// unavailable flag lets the caller surface a one-time warning.
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

// GetAIProviderConfig returns the AI provider configuration with API keys
// scrubbed (HasKey flags presence). Never returns the raw secret. (#217.)
// GetAIProviderConfig returns the AI provider configuration with API keys
// scrubbed (HasKey flags presence, resolved across BOTH the OS keyring and
// config.yaml). Never returns the raw secret. The keyring lookup happens with
// no locks held so an unavailable keyring cannot stall the call. (#217.)
func (a *App) GetAIProviderConfig() (AIPublicConfig, error) {
	a.vaultMu.RLock()
	if a.vaultPath == "" {
		a.vaultMu.RUnlock()
		return AIPublicConfig{}, fmt.Errorf("vault not loaded")
	}
	a.configMu.RLock()
	chat := a.cfg.AI.Chat
	emb := a.cfg.AI.Embedding
	useKeyring := a.aiUseKeyringLocked()
	chatUser := a.aiKeyringUser("chat")
	embUser := a.aiKeyringUser("embedding")
	a.configMu.RUnlock()
	a.vaultMu.RUnlock()

	// Resolve whether a key is present (keyring OR config) with no locks held.
	chatKey, chatUnavail := a.resolveAIKeyUnlocked(chatUser, useKeyring, chat.APIKey)
	embKey, embUnavail := a.resolveAIKeyUnlocked(embUser, useKeyring, emb.APIKey)
	keyringAvailable := a.keyringStore != nil && a.keyringStore.Available()
	return AIPublicConfig{
		UseKeyring:         useKeyring,
		KeyringAvailable:   keyringAvailable,
		KeyringUnusableFor: aiUnusableList(chatUnavail, embUnavail),
		Chat: AIPublicProvider{
			ProviderType:    chat.ProviderType,
			BaseURL:         chat.BaseURL,
			Model:           chat.Model,
			HasKey:          chatKey != "",
			Temperature:     chat.Temperature,
			MaxTokens:       chat.MaxTokens,
			ReasoningEffort: chat.ReasoningEffort,
			TimeoutMs:       chat.TimeoutMs,
		},
		Embedding: AIPublicProvider{
			ProviderType: emb.ProviderType,
			BaseURL:      emb.BaseURL,
			Model:        emb.Model,
			HasKey:       embKey != "",
			Dimensions:   emb.Dimensions,
			TimeoutMs:    emb.TimeoutMs,
		},
	}, nil
}

// aiUnusableList returns the provider kinds whose keyring lookup reported
// unavailable, for the settings page's warning banner. nil/empty when both are
// fine.
func aiUnusableList(chat, emb bool) []string {
	var out []string
	if chat {
		out = append(out, "chat")
	}
	if emb {
		out = append(out, "embedding")
	}
	return out
}

// UpdateAIProviderConfig applies a non-key patch to one provider block and
// persists atomically. The existing API key is preserved (the patch type has no
// key field), and the AI section is re-normalized. Mirrors UpdatePluginSetting's
// atomic read-modify-write under vaultMu(R) + configMu(W). (#217.)
func (a *App) UpdateAIProviderConfig(which string, patch AIProviderPatch) error {
	if err := aiValidateWhich(which); err != nil {
		return err
	}
	if err := validateAIBaseURL(patch.BaseURL); err != nil {
		return err
	}
	// Reject unknown reasoning_effort values at the gate so they can't persist
	// and later surface as a provider 400. Empty/nil means "unset" and is allowed.
	if patch.ReasoningEffort != nil {
		if re := strings.TrimSpace(*patch.ReasoningEffort); re != "" && !config.IsValidAIReasoningEffort(re) {
			return fmt.Errorf("invalid reasoning_effort %q: must be one of none, minimal, low, medium, high, xhigh, max", *patch.ReasoningEffort)
		}
	}
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()

	// Build the new block from the patch, preserving the live key (the patch
	// never carries one).
	patched := config.AIProviderConfig{
		ProviderType:    patch.ProviderType,
		BaseURL:         patch.BaseURL,
		Model:           patch.Model,
		Temperature:     patch.Temperature,
		MaxTokens:       patch.MaxTokens,
		ReasoningEffort: patch.ReasoningEffort,
		TimeoutMs:       patch.TimeoutMs,
		Dimensions:      patch.Dimensions,
	}
	if which == "chat" {
		patched.APIKey = a.cfg.AI.Chat.APIKey
		a.cfg.AI.Chat = patched
	} else {
		patched.APIKey = a.cfg.AI.Embedding.APIKey
		a.cfg.AI.Embedding = patched
	}
	a.cfg.AI = config.NormalizeAIConfig(a.cfg.AI)

	if a.configWatcher != nil {
		a.configWatcher.RegisterSelfWrite()
	}
	return config.Save(a.vaultPath, a.cfg)
}

// SetAIAPIKey stores a provider API key. When the use_keyring toggle is on AND
// a keyring store is wired AND it accepts the write, the key is stored ONLY in
// the OS keyring (and blanked from plaintext config.yaml). If the keyring is off
// or unavailable, the key falls back to config.yaml so the feature still works
// on a headless/locked machine. Trims surrounding whitespace. The keyring write
// happens with no locks held so a D-Bus timeout cannot stall config access.
// (#218.)
func (a *App) SetAIAPIKey(which, key string) error {
	if err := aiValidateWhich(which); err != nil {
		return err
	}
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
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
	if a.configWatcher != nil {
		a.configWatcher.RegisterSelfWrite()
	}
	return config.Save(a.vaultPath, a.cfg)
}

// ClearAIAPIKey removes a provider API key from BOTH stores (the OS keyring and
// config.yaml) so a clear is durable regardless of where the key lived. The
// keyring delete is best-effort (ErrNotFound is the normal "nothing to delete"
// case and is ignored). (#218.)
func (a *App) ClearAIAPIKey(which string) error {
	if err := aiValidateWhich(which); err != nil {
		return err
	}
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	user := a.aiKeyringUser(which)
	// Best-effort keyring delete with no locks held: ErrNotFound is the normal
	// "nothing to delete" case and any other error is ignored, since config is
	// cleared below regardless.
	if a.keyringStore != nil {
		_ = a.keyringStore.Delete(keyringService, user)
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	if which == "chat" {
		a.cfg.AI.Chat.APIKey = ""
	} else {
		a.cfg.AI.Embedding.APIKey = ""
	}
	if a.configWatcher != nil {
		a.configWatcher.RegisterSelfWrite()
	}
	return config.Save(a.vaultPath, a.cfg)
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
	if a.configWatcher != nil {
		a.configWatcher.RegisterSelfWrite()
	}
	err := config.Save(a.vaultPath, a.cfg)
	a.configMu.Unlock()
	if err != nil {
		return err
	}
	// Turning keyring on: opportunistically migrate any plaintext keys now so
	// the user sees them leave config.yaml without a restart.
	if on {
		a.migrateAIKeysToKeyring()
	}
	return nil
}

// boolPtrAI is the *bool helper local to this file (the config package's
// unexported boolPtr isn't visible here).
func boolPtrAI(b bool) *bool { return &b }
func (a *App) TestAIConnection(which string) (AIProbeResult, error) {
	if err := aiValidateWhich(which); err != nil {
		return AIProbeResult{}, err
	}
	provider, err := a.snapshotAIProvider(which)
	if err != nil {
		return AIProbeResult{}, err
	}
	// No configMu/vaultMu held during the HTTP probe.
	if err := ai.Probe(a.aiContext(), provider, which == "chat"); err != nil {
		if e, ok := err.(*ai.AIError); ok {
			return AIProbeResult{OK: false, Kind: string(e.Kind), Message: e.Message}, nil
		}
		return AIProbeResult{OK: false, Message: err.Error()}, nil
	}
	return AIProbeResult{OK: true}, nil
}

// snapshotAIProvider reads one provider block, resolves its key (keyring-first,
// config fallback), and returns it as a value. Both locks are released before
// the keyring lookup so an unavailable keyring cannot stall the call, and the
// returned value is used by the caller for HTTP with NO locks held.
func (a *App) snapshotAIProvider(which string) (ai.AIProvider, error) {
	a.vaultMu.RLock()
	if a.vaultPath == "" {
		a.vaultMu.RUnlock()
		return ai.AIProvider{}, fmt.Errorf("vault not loaded")
	}
	a.configMu.RLock()
	p := aiConfigBlock(a.cfg.AI, which)
	useKeyring := a.aiUseKeyringLocked()
	user := a.aiKeyringUser(which)
	a.configMu.RUnlock()
	a.vaultMu.RUnlock()
	key, _ := a.resolveAIKeyUnlocked(user, useKeyring, p.APIKey)
	return ai.AIProvider{
		ProviderType:    p.ProviderType,
		BaseURL:         p.BaseURL,
		APIKey:          key,
		Model:           p.Model,
		ReasoningEffort: p.ReasoningEffort,
		TimeoutMs:       p.TimeoutMs,
	}, nil
}

// aiPreflightPlugin runs the plugin-side gates (session, capability, rate limit)
// and returns the resolved provider snapshot + effective model. Locks are
// released before the keyring lookup and before the caller does any HTTP.
func (a *App) aiPreflightPlugin(pluginID, sessionToken, which string) (ai.AIProvider, string, error) {
	a.vaultMu.RLock()
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		a.vaultMu.RUnlock()
		return ai.AIProvider{}, "", err
	}
	if err := a.requireGrant(pluginID, plugins.CapAI); err != nil {
		a.vaultMu.RUnlock()
		return ai.AIProvider{}, "", err
	}
	if a.rateLimiter != nil && !a.rateLimiter.allow(a.vaultPath, pluginID) {
		a.vaultMu.RUnlock()
		rps, burst := resolvePluginRatelimit(a.vaultPath, pluginID)
		return ai.AIProvider{}, "", fmt.Errorf("plugin %q AI rate limit exceeded (max %.1f rps, burst %d); retry after a short delay", pluginID, rps, burst)
	}
	a.configMu.RLock()
	p := aiConfigBlock(a.cfg.AI, which)
	useKeyring := a.aiUseKeyringLocked()
	user := a.aiKeyringUser(which)
	a.configMu.RUnlock()
	a.vaultMu.RUnlock()
	key, _ := a.resolveAIKeyUnlocked(user, useKeyring, p.APIKey)
	return ai.AIProvider{
		ProviderType:    p.ProviderType,
		BaseURL:         p.BaseURL,
		APIKey:          key,
		Model:           p.Model,
		ReasoningEffort: p.ReasoningEffort,
		TimeoutMs:       p.TimeoutMs,
	}, p.Model, nil
}

// PluginAIComplete performs a chat completion on behalf of a plugin. Gated by
// the ai capability; rate-limited and audit-logged exactly like PluginFetch.
// Credentials are read server-side and never returned to the caller. (#216.)
func (a *App) PluginAIComplete(pluginID, sessionToken string, input PluginAICompleteInput) (ai.CompleteResult, error) {
	// Validate reasoning_effort BEFORE aiPreflightPlugin so an invalid call
	// doesn't consume a rate-limit slot or snapshot the provider config.
	if input.ReasoningEffort != nil {
		if re := strings.TrimSpace(*input.ReasoningEffort); re != "" && !config.IsValidAIReasoningEffort(re) {
			return ai.CompleteResult{}, &ai.AIError{Kind: ai.ErrBadRequest, Message: fmt.Sprintf("invalid reasoning_effort %q: must be one of none, minimal, low, medium, high, xhigh, max", *input.ReasoningEffort)}
		}
	}
	provider, configuredModel, err := a.aiPreflightPlugin(pluginID, sessionToken, "chat")
	if err != nil {
		return ai.CompleteResult{}, err
	}
	// Effective model for auditing: the per-call override, else the configured one.
	effectiveModel := input.Model
	if effectiveModel == "" {
		effectiveModel = configuredModel
	}
	messages := make([]ai.ChatMessage, len(input.Messages))
	for i, m := range input.Messages {
		messages[i] = ai.ChatMessage{Role: m.Role, Content: m.Content}
	}
	result, callErr := ai.Complete(a.aiContext(), ai.CompleteRequest{
		Provider:        provider,
		Messages:        messages,
		Model:           input.Model,
		Temperature:     input.Temperature,
		MaxTokens:       input.MaxTokens,
		ReasoningEffort: input.ReasoningEffort,
		Stream:          false, // Sprint 22 delivers streaming; signature is additive
	})
	status := "ok"
	if callErr != nil {
		status = aiErrKind(callErr)
	}
	a.auditAI(pluginID, aiChatKind, provider.BaseURL, effectiveModel, status, result.Usage)
	if callErr != nil {
		return ai.CompleteResult{}, callErr
	}
	return result, nil
}

// PluginAIEmbed computes embeddings for a batch of texts on behalf of a plugin.
// Gated by the ai capability; rate-limited and audit-logged exactly like
// PluginFetch. Credentials are read server-side and never returned to the
// caller. (#216.)
func (a *App) PluginAIEmbed(pluginID, sessionToken string, input PluginAIEmbedInput) (ai.EmbedResult, error) {
	provider, configuredModel, err := a.aiPreflightPlugin(pluginID, sessionToken, "embedding")
	if err != nil {
		return ai.EmbedResult{}, err
	}
	effectiveModel := input.Model
	if effectiveModel == "" {
		effectiveModel = configuredModel
	}
	result, callErr := ai.Embed(a.aiContext(), ai.EmbedRequest{
		Provider:   provider,
		Texts:      input.Texts,
		Model:      input.Model,
		Dimensions: input.Dimensions,
	})
	status := "ok"
	if callErr != nil {
		status = aiErrKind(callErr)
	}
	a.auditAI(pluginID, aiEmbKind, provider.BaseURL, effectiveModel, status, result.Usage)
	if callErr != nil {
		return ai.EmbedResult{}, callErr
	}
	return result, nil
}

// aiErrKind reduces an error from the service to its audit "kind" tag, falling
// back to a generic "error" for non-AIError failures.
func aiErrKind(err error) string {
	if e, ok := err.(*ai.AIError); ok {
		return string(e.Kind)
	}
	return "error"
}

// migrateAIKeysToKeyring moves any plaintext AI API keys found in config.yaml
// into the OS keyring on first run after upgrade (#218). Idempotent: once a key
// is in the keyring the config field is blanked, so a re-run finds nothing to
// migrate. Best-effort and silent — if the keyring is unavailable, the plaintext
// key is LEFT in config (the documented fallback; the provider page surfaces the
// unavailability). No locks are held during keyring writes so a D-Bus timeout
// cannot stall startup. Called from initializeVaultServices.
func (a *App) migrateAIKeysToKeyring() {
	if a.keyringStore == nil {
		return
	}
	// Snapshot any plaintext keys + the toggle + the keyring user keys.
	a.vaultMu.RLock()
	if a.vaultPath == "" {
		a.vaultMu.RUnlock()
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
	a.vaultMu.RUnlock()
	if !useKeyring || len(todo) == 0 {
		return
	}
	// Keyring writes with no locks held.
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
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
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
	if a.configWatcher != nil {
		a.configWatcher.RegisterSelfWrite()
	}
	_ = config.Save(a.vaultPath, a.cfg)
}
