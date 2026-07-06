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
	"fmt"
	"silt/backend/ai"
	"silt/backend/config"
	"silt/backend/plugins"
	"strings"
)

// AIProviderPatch is the input to UpdateAIProviderConfig: one provider block
// MINUS the API key. The key is never part of a generic patch — it moves through
// the dedicated SetAIAPIKey / ClearAIAPIKey bindings so it can be routed to the
// OS keyring (#218) rather than plaintext config.
type AIProviderPatch struct {
	ProviderType string   `json:"provider_type"`
	BaseURL      string   `json:"base_url"`
	Model        string   `json:"model"`
	Temperature  *float64 `json:"temperature,omitempty"`
	MaxTokens    *int     `json:"max_tokens,omitempty"`
	TimeoutMs    *int     `json:"timeout_ms,omitempty"`
	Dimensions   *int     `json:"dimensions,omitempty"`
}

// AIPublicProvider is the API-key-scrubbed view of one provider block returned
// to the frontend. HasKey is true when a key is stored (config in Phase 1;
// config-or-keyring in #218) so the page can show "key set" without ever
// receiving the secret.
type AIPublicProvider struct {
	ProviderType string   `json:"provider_type"`
	BaseURL      string   `json:"base_url"`
	Model        string   `json:"model"`
	HasKey       bool     `json:"has_key"`
	Temperature  *float64 `json:"temperature,omitempty"`
	MaxTokens    *int     `json:"max_tokens,omitempty"`
	TimeoutMs    *int     `json:"timeout_ms,omitempty"`
	Dimensions   *int     `json:"dimensions,omitempty"`
}

// AIPublicConfig is the AI Provider page's full read view: both provider blocks
// (key-scrubbed) plus the keyring toggle.
type AIPublicConfig struct {
	Chat       AIPublicProvider `json:"chat"`
	Embedding  AIPublicProvider `json:"embedding"`
	UseKeyring bool             `json:"use_keyring"`
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
	Messages    []PluginAIChatMessage `json:"messages"`
	Model       string                `json:"model,omitempty"`
	Temperature *float64              `json:"temperature,omitempty"`
	MaxTokens   *int                  `json:"max_tokens,omitempty"`
	Stream      bool                  `json:"stream,omitempty"`
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

// resolveAIKey returns the API key for a provider. Phase 1 (#216): read from
// config.yaml (the plaintext fallback). Phase 3 (#218) overrides this to consult
// the OS keyring first and fall back to config. MUST be called under configMu
// (reads a.cfg-derived values).
func (a *App) resolveAIKey(_ string, fallback string) (string, error) {
	return strings.TrimSpace(fallback), nil
}

// GetAIProviderConfig returns the AI provider configuration with API keys
// scrubbed (HasKey flags presence). Never returns the raw secret. (#217.)
func (a *App) GetAIProviderConfig() (AIPublicConfig, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return AIPublicConfig{}, fmt.Errorf("vault not loaded")
	}
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return a.aiPublicConfigLocked(), nil
}

// aiPublicConfigLocked builds the key-scrubbed public view. MUST be called under
// configMu.
func (a *App) aiPublicConfigLocked() AIPublicConfig {
	chatKey, _ := a.resolveAIKey("chat", a.cfg.AI.Chat.APIKey)
	embKey, _ := a.resolveAIKey("embedding", a.cfg.AI.Embedding.APIKey)
	useKeyring := a.cfg.AI.UseKeyring == nil || *a.cfg.AI.UseKeyring
	return AIPublicConfig{
		UseKeyring: useKeyring,
		Chat: AIPublicProvider{
			ProviderType: a.cfg.AI.Chat.ProviderType,
			BaseURL:      a.cfg.AI.Chat.BaseURL,
			Model:        a.cfg.AI.Chat.Model,
			HasKey:       chatKey != "",
			Temperature:  a.cfg.AI.Chat.Temperature,
			MaxTokens:    a.cfg.AI.Chat.MaxTokens,
			TimeoutMs:    a.cfg.AI.Chat.TimeoutMs,
		},
		Embedding: AIPublicProvider{
			ProviderType: a.cfg.AI.Embedding.ProviderType,
			BaseURL:      a.cfg.AI.Embedding.BaseURL,
			Model:        a.cfg.AI.Embedding.Model,
			HasKey:       embKey != "",
			Dimensions:   a.cfg.AI.Embedding.Dimensions,
			TimeoutMs:    a.cfg.AI.Embedding.TimeoutMs,
		},
	}
}

// UpdateAIProviderConfig applies a non-key patch to one provider block and
// persists atomically. The existing API key is preserved (the patch type has no
// key field), and the AI section is re-normalized. Mirrors UpdatePluginSetting's
// atomic read-modify-write under vaultMu(R) + configMu(W). (#217.)
func (a *App) UpdateAIProviderConfig(which string, patch AIProviderPatch) error {
	if err := aiValidateWhich(which); err != nil {
		return err
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
		ProviderType: patch.ProviderType,
		BaseURL:      patch.BaseURL,
		Model:        patch.Model,
		Temperature:  patch.Temperature,
		MaxTokens:    patch.MaxTokens,
		TimeoutMs:    patch.TimeoutMs,
		Dimensions:   patch.Dimensions,
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

// SetAIAPIKey stores a provider API key. Phase 1 (#216): writes to config.yaml.
// Phase 3 (#218) overrides the storage target to honor the use_keyring toggle
// (keyring when on + available, config otherwise). Trims surrounding whitespace.
func (a *App) SetAIAPIKey(which, key string) error {
	if err := aiValidateWhich(which); err != nil {
		return err
	}
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	key = strings.TrimSpace(key)
	if which == "chat" {
		a.cfg.AI.Chat.APIKey = key
	} else {
		a.cfg.AI.Embedding.APIKey = key
	}
	if a.configWatcher != nil {
		a.configWatcher.RegisterSelfWrite()
	}
	return config.Save(a.vaultPath, a.cfg)
}

// ClearAIAPIKey removes a provider API key. Phase 1 (#216): blanks it in
// config.yaml. Phase 3 (#218) also deletes it from the keyring.
func (a *App) ClearAIAPIKey(which string) error {
	if err := aiValidateWhich(which); err != nil {
		return err
	}
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
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

// TestAIConnection runs the AI Provider page's connection probe for one block
// (chat: a 1-token completion; embedding: a single short embed). NOT capability-
// gated — it is a core settings action, not a plugin call. Snapshots the
// resolved provider under the locks, releases them, then runs the probe so a
// slow/dead endpoint cannot hold vaultMu. (#217.)
func (a *App) TestAIConnection(which string) (AIProbeResult, error) {
	if err := aiValidateWhich(which); err != nil {
		return AIProbeResult{}, err
	}
	provider, err := a.snapshotAIProvider(which)
	if err != nil {
		return AIProbeResult{}, err
	}
	// No configMu/vaultMu held during the HTTP probe.
	if err := ai.Probe(context.Background(), provider, which == "chat"); err != nil {
		if e, ok := err.(*ai.AIError); ok {
			return AIProbeResult{OK: false, Kind: string(e.Kind), Message: e.Message}, nil
		}
		return AIProbeResult{OK: false, Message: err.Error()}, nil
	}
	return AIProbeResult{OK: true}, nil
}

// snapshotAIProvider reads + resolves one provider block under the locks and
// returns it as a value. Both locks are released before the caller performs any
// HTTP. MUST be the only place the plugin/test-connection paths read provider
// config so the "no locks during HTTP" invariant has one source of truth.
func (a *App) snapshotAIProvider(which string) (ai.AIProvider, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return ai.AIProvider{}, fmt.Errorf("vault not loaded")
	}
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	p := aiConfigBlock(a.cfg.AI, which)
	key, err := a.resolveAIKey(which, p.APIKey)
	if err != nil {
		return ai.AIProvider{}, err
	}
	return ai.AIProvider{
		ProviderType: p.ProviderType,
		BaseURL:      p.BaseURL,
		APIKey:       key,
		Model:        p.Model,
		TimeoutMs:    p.TimeoutMs,
	}, nil
}

// aiPreflightPlugin runs the plugin-side gates (session, capability, rate limit)
// and returns the resolved provider snapshot + effective model. Locks are
// released before the caller does any HTTP, exactly like snapshotAIProvider but
// with the plugin privilege gates prepended.
func (a *App) aiPreflightPlugin(pluginID, sessionToken, which string) (ai.AIProvider, string, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return ai.AIProvider{}, "", err
	}
	if err := a.requireGrant(pluginID, plugins.CapAI); err != nil {
		return ai.AIProvider{}, "", err
	}
	if a.rateLimiter != nil && !a.rateLimiter.allow(a.vaultPath, pluginID) {
		rps, burst := resolvePluginRatelimit(a.vaultPath, pluginID)
		return ai.AIProvider{}, "", fmt.Errorf("plugin %q AI rate limit exceeded (max %.1f rps, burst %d); retry after a short delay", pluginID, rps, burst)
	}
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	p := aiConfigBlock(a.cfg.AI, which)
	key, err := a.resolveAIKey(which, p.APIKey)
	if err != nil {
		return ai.AIProvider{}, "", err
	}
	return ai.AIProvider{
		ProviderType: p.ProviderType,
		BaseURL:      p.BaseURL,
		APIKey:       key,
		Model:        p.Model,
		TimeoutMs:    p.TimeoutMs,
	}, p.Model, nil
}

// PluginAIComplete performs a chat completion on behalf of a plugin. Gated by
// the ai capability; rate-limited and audit-logged exactly like PluginFetch.
// Credentials are read server-side and never returned to the caller. (#216.)
func (a *App) PluginAIComplete(pluginID, sessionToken string, input PluginAICompleteInput) (ai.CompleteResult, error) {
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
	result, callErr := ai.Complete(context.Background(), ai.CompleteRequest{
		Provider:    provider,
		Messages:    messages,
		Model:       input.Model,
		Temperature: input.Temperature,
		MaxTokens:   input.MaxTokens,
		Stream:      false, // Sprint 22 delivers streaming; signature is additive
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
	result, callErr := ai.Embed(context.Background(), ai.EmbedRequest{
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
