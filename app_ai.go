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
	"errors"
	"fmt"
	"silt/backend/ai"
	"silt/backend/config"
	"strings"
)

// errVaultClosing is the sentinel returned by withAIPreflight when a vault
// close/switch is in progress. The call is rejected before any HTTP so it can
// neither strand an audit entry against a torn-down vault nor leak into the
// next vault (#452). Kept as the errors.Is target; the return site hands back
// a vaultClosingError() (*IPCError carrying CodeVaultClosing, #478) that wraps
// this sentinel so errors.Is(err, errVaultClosing) still holds. The plugin
// surfaces it as a transient rejection (normalizeAIError → unknown); the UI is
// unmounting during a close, so it is rarely observed.
var errVaultClosing = errors.New("vault is closing; AI call rejected")

// vaultClosingError returns the close-in-progress rejection as an *IPCError
// carrying the stable CodeVaultClosing (#478) that also satisfies
// errors.Is(err, errVaultClosing) via the wrapped sentinel.
func vaultClosingError() *IPCError {
	return wrapSentinelAsIPCError(CodeVaultClosing, errVaultClosing.Error(), errVaultClosing)
}

// aiContext returns the vault-scoped context for AI HTTP calls, falling
// back through the app-lifecycle context (aiCtx) to context.Background()
// when the App wasn't initialized via startup() (tests). Production sets
// vaultCtx in initializeVaultServices (a child of aiCtx) and cancels it in
// CloseVault/SwitchVault so in-flight completions/embeddings abort promptly
// on close/switch instead of running to their 60s timeout (#471); aiCtx is
// itself cancelled in shutdown() so in-flight calls also don't outlive the
// process.
func (a *App) aiContext() context.Context {
	if a.vaultCtx != nil {
		return a.vaultCtx
	}
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
	ProviderType    ai.AIProviderType `json:"provider_type"`
	BaseURL         string            `json:"base_url"`
	Model           string            `json:"model"`
	Temperature     *float64          `json:"temperature,omitempty"`
	MaxTokens       *int              `json:"max_tokens,omitempty"`
	ReasoningEffort *string           `json:"reasoning_effort,omitempty"`
	TimeoutMs       *int              `json:"timeout_ms,omitempty"`
	Dimensions      *int              `json:"dimensions,omitempty"`
}

// AIPublicProvider is the API-key-scrubbed view of one provider block returned
// to the frontend. HasKey is true when a key is stored (config in Phase 1;
// config-or-keyring in #218) so the page can show "key set" without ever
// receiving the secret.
type AIPublicProvider struct {
	ProviderType    ai.AIProviderType `json:"provider_type"`
	BaseURL         string            `json:"base_url"`
	Model           string            `json:"model"`
	HasKey          bool              `json:"has_key"`
	Temperature     *float64          `json:"temperature,omitempty"`
	MaxTokens       *int              `json:"max_tokens,omitempty"`
	ReasoningEffort *string           `json:"reasoning_effort,omitempty"`
	TimeoutMs       *int              `json:"timeout_ms,omitempty"`
	Dimensions      *int              `json:"dimensions,omitempty"`
}

// AIPublicConfig is the AI settings page's full read view: both provider blocks
// (key-scrubbed), product feature flags (#632), plus the keyring toggle and
// availability state. KeyringAvailable is false when no keyring store is wired
// (tests); KeyringUnusableFor lists the provider kinds ("chat"/"embedding")
// whose keyring lookup reported unavailable so the page can show a fallback warning.
type AIPublicConfig struct {
	Chat               AIPublicProvider        `json:"chat"`
	Embedding          AIPublicProvider        `json:"embedding"`
	Features           config.AIFeaturesConfig `json:"features"`
	UseKeyring         bool                    `json:"use_keyring"`
	KeyringAvailable   bool                    `json:"keyring_available"`
	KeyringUnusableFor []string                `json:"keyring_unusable_for,omitempty"`
}

// AIFeaturesPatch is the input to UpdateAIFeatures (#632).
type AIFeaturesPatch struct {
	Enabled          *bool `json:"enabled,omitempty"`
	RAGEnabled       *bool `json:"rag_enabled,omitempty"`
	SummariesEnabled *bool `json:"summaries_enabled,omitempty"`
}

// AIProbeResult is the outcome of a Test Connection call. On failure, Kind is
// the normalized ai.AIErrorKind and Message is the provider's trimmed error body.
type AIProbeResult struct {
	OK      bool   `json:"ok"`
	Kind    string `json:"kind,omitempty"`
	Message string `json:"message,omitempty"`
}

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

// GetAIProviderConfig returns the AI provider configuration with API keys
// scrubbed (HasKey flags presence, resolved across BOTH the OS keyring and
// config.yaml). Never returns the raw secret. The keyring lookup happens with
// no locks held so an unavailable keyring cannot stall the call. (#217.)
// Keyring helpers live in app_ai_keys.go.
func (a *App) GetAIProviderConfig() (AIPublicConfig, error) {
	a.vaultMu.RLock()
	if a.vaultPath == "" {
		a.vaultMu.RUnlock()
		return AIPublicConfig{}, fmt.Errorf("vault not loaded")
	}
	a.configMu.RLock()
	chat := a.cfg.AI.Chat
	emb := a.cfg.AI.Embedding
	features := a.cfg.AI.Features
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
		Features:           features,
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

// UpdateAIFeatures applies a product-level AI enablement patch (#632) and
// persists atomically. Dependents are clamped when master is off. Live a.cfg
// is only updated after a successful save so a failed persist does not leave
// memory and disk diverged until restart.
func (a *App) UpdateAIFeatures(patch AIFeaturesPatch) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()

	// Candidate copy: mutate offline, then publish only if write succeeds.
	next := a.cfg
	f := next.AI.Features
	if patch.Enabled != nil {
		f.Enabled = *patch.Enabled
	}
	if patch.RAGEnabled != nil {
		f.RAGEnabled = *patch.RAGEnabled
	}
	if patch.SummariesEnabled != nil {
		f.SummariesEnabled = *patch.SummariesEnabled
	}
	next.AI.Features = f
	wasOn := a.cfg.AI.Features.Enabled
	next.AI = config.NormalizeAIConfig(next.AI)
	if err := a.saveConfigTracked(next); err != nil {
		return err
	}
	a.cfg = next
	// Master AI just turned off: cancel any in-flight provider streams so they
	// stop consuming tokens/cost immediately instead of running to their
	// per-call timeout. Frontend teardown calls PluginAICancelStream, but that
	// binding rejects once the AI capability grant is revoked on the next
	// session check, so shutdown must not depend on it (#632).
	if wasOn && !next.AI.Features.Enabled {
		a.cancelAllAIStreams()
	}
	return nil
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

	if err := a.saveConfigTracked(a.cfg); err != nil {
		return err
	}
	// A provider-type / base-URL / model change means the cached model list
	// may no longer match the live endpoint — drop it so the next ListModels
	// re-polls.
	a.invalidateAIModelCache(which)
	return nil
}

// SetAIAPIKey stores a provider API key. When the use_keyring toggle is on AND
// a keyring store is wired AND it accepts the write, the key is stored ONLY in
// the OS keyring (and blanked from plaintext config.yaml). If the keyring is off
// or unavailable, the key falls back to config.yaml so the feature still works
// on a headless/locked machine. Trims surrounding whitespace. The keyring write
// runs under vaultMu.RLock (blocks Switch/Move cutover, not other readers) so a
// concurrent vault switch cannot retarget the write. configMu is released
// during keyring I/O so a D-Bus timeout cannot stall unrelated config readers.
// Implementation: setAIAPIKeyLocked in app_ai_keys.go. (#218.)
func (a *App) SetAIAPIKey(which, key string) error {
	if err := aiValidateWhich(which); err != nil {
		return err
	}
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	return a.setAIAPIKeyLocked(which, key)
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
	if err := a.saveConfigTracked(a.cfg); err != nil {
		return err
	}
	// Cleared key → the cached list (polled under the old key) may no longer
	// be authoritatively reachable. Drop it.
	a.invalidateAIModelCache(which)
	return nil
}

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

// ListModels polls the configured provider's model-list endpoint and returns
// the available models for the Settings dropdown. When force is false and a
// cached result exists, the cache is served without a network call (so tab-open
// after a prior poll shows the dropdown instantly). When force is false and NO
// cache entry exists, an empty list is returned (no network call) — cold start
// shows the free-text fallback until the user clicks "Refresh models", which
// passes force=true to poll and populate the cache. which selects chat vs
// embedding so the Google filter can pick generateContent vs embedContent
// models. The snapshot is taken with locks released before the (potentially
// slow) HTTP list call, mirroring TestAIConnection.
func (a *App) ListModels(which string, force bool) ([]ai.AIModel, error) {
	if err := aiValidateWhich(which); err != nil {
		return nil, err
	}
	// Serve from cache when present; cold-start (no cache, no force) returns
	// empty so the dropdown falls back to free-text without a surprise poll.
	if !force {
		a.aiModelCacheMu.Lock()
		cached, ok := a.aiModelCache[which]
		a.aiModelCacheMu.Unlock()
		if ok {
			return cached, nil
		}
		return nil, nil
	}
	provider, err := a.snapshotAIProvider(which)
	if err != nil {
		return nil, err
	}
	models, err := ai.ListModels(a.aiContext(), provider, which)
	if err != nil {
		return nil, err
	}
	// Cache the successful result. A failed poll leaves the previous cache
	// intact so a transient list-endpoint error doesn't wipe a good dropdown.
	a.aiModelCacheMu.Lock()
	a.aiModelCache[which] = models
	a.aiModelCacheMu.Unlock()
	return models, nil
}

// invalidateAIModelCache drops the cached model list for one provider block.
// Called when the provider type, base URL, model, or key changes so the
// dropdown doesn't show stale models from a different endpoint. Safe to call
// when no cache entry exists (no-op).
func (a *App) invalidateAIModelCache(which string) {
	a.aiModelCacheMu.Lock()
	delete(a.aiModelCache, which)
	a.aiModelCacheMu.Unlock()
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
