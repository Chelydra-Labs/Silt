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
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"silt/backend/ai"
	"silt/backend/config"
	"silt/backend/keyring"
	"silt/backend/plugins"
	"strings"
	"time"
)

// AI stream event name prefixes (#226). Owner-scoped full names append
// ":"+pluginID so concurrent plugin streams do not share a global bus (#635).
// Payload still includes plugin_id for debugging.
const (
	aiEventCompleteDelta     = "ai:complete:delta"
	aiEventCompleteDone      = "ai:complete:done"
	aiEventCompleteError     = "ai:complete:error"
	aiEventCompleteToolDelta = "ai:complete:tool-delta"
)

// aiStreamEventName returns the owner-scoped Wails event name for a stream
// event base + pluginID (#635).
func aiStreamEventName(base, pluginID string) string {
	if pluginID == "" {
		return base
	}
	return base + ":" + pluginID
}

// aiStreamBufferCap is the max number of unconsumed delta events buffered per
// stream before the producer aborts (backpressure). Generous for UI consumers
// that coalesce on rAF; tight enough to bound memory if a plugin stalls.
const aiStreamBufferCap = 256

// aiStreamReadyWait is how long the producer waits for PluginAIStreamReady
// before starting anyway. Covers the IPC round-trip for listener attach; if the
// client never acks (crashed plugin), the stream still proceeds rather than
// hanging until the provider timeout.
const aiStreamReadyWait = 2 * time.Second

// keyringService is the OS-keyring service name under which Silt stores AI
// provider keys (#218). The "user" half is vault-scoped (see aiKeyringUser) so
// two vaults on one machine keep separate keys.
const keyringService = "Silt"

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

// PluginAIChatMessage is one chat message crossing the plugin→service boundary.
// For multi-turn tool use (#595): assistant turns may carry tool_calls, and a
// tool result turn (role "tool") carries tool_call_id correlating it.
type PluginAIChatMessage struct {
	Role       string             `json:"role"`
	Content    string             `json:"content"`
	ToolCalls  []PluginAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
}

// PluginAIToolDef declares one tool a plugin exposes to the model (#595).
// Parameters is a raw JSON Schema object.
type PluginAIToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// PluginAIToolCall is one tool invocation the model requested (#595). Arguments
// is the raw JSON object bytes (unwrapped from OpenAI's stringified form).
type PluginAIToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// PluginAIToolChoice constrains tool selection (#595).
type PluginAIToolChoice struct {
	Mode     string `json:"mode"`                // auto|required|none|force
	ToolName string `json:"tool_name,omitempty"` // set when Mode == "force"
}

// PluginAICompleteInput is the plugin-side request envelope for a chat
// completion. When Stream is true, PluginAIComplete starts an async SSE stream
// and returns immediately with stream_id set; deltas arrive as Wails events
// (#226). When false (default), the buffered Sprint 20 path is used.
type PluginAICompleteInput struct {
	Messages        []PluginAIChatMessage `json:"messages"`
	Model           string                `json:"model,omitempty"`
	Temperature     *float64              `json:"temperature,omitempty"`
	MaxTokens       *int                  `json:"max_tokens,omitempty"`
	ReasoningEffort *string               `json:"reasoning_effort,omitempty"`
	Stream          bool                  `json:"stream,omitempty"`
	ResponseSchema  json.RawMessage       `json:"response_schema,omitempty"`
	Tools           []PluginAIToolDef     `json:"tools,omitempty"`
	ToolChoice      *PluginAIToolChoice   `json:"tool_choice,omitempty"`
}

// PluginAIEmbedInput is the plugin-side request envelope for an embedding batch.
type PluginAIEmbedInput struct {
	Texts      []string `json:"texts"`
	Model      string   `json:"model,omitempty"`
	Dimensions *int     `json:"dimensions,omitempty"`
	// TaskType is Google-specific (RETRIEVAL_DOCUMENT / RETRIEVAL_QUERY); empty omits.
	TaskType string `json:"task_type,omitempty"`
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
// (#218.)
func (a *App) SetAIAPIKey(which, key string) error {
	if err := aiValidateWhich(which); err != nil {
		return err
	}
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	return a.setAIAPIKeyLocked(which, key)
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

// withAIPreflight runs the plugin-side gates (session, capability, rate limit)
// and returns the resolved provider snapshot, the effective model, and a done
// func the caller MUST defer. Locks are released before the keyring lookup and
// before the caller does any HTTP.
//
// Drain contract (#473): the closing-check + vaultClosingWG.Add(1) below share
// ONE RLock hold, so they are atomic w.r.t. CloseVault/SwitchVault's
// closing=true + Wait (taken under the write lock) — no TOCTOU window where a
// call slips through after the drain returns (#452). Returning the balancing
// Done() as part of the same result as the Add makes the contract STRUCTURAL:
// a caller that uses this wrapper cannot forget the Add (it's bundled in) and
// cannot forget the Done (the returned func must be deferred or the vet/linter
// flags the unused result). On error, done is nil — no Add ran, nothing to
// balance. The Done() is safe to call at most once (sync.WaitGroup semantics).
//
// Nothing after the Add may return an error, or it would leak an unbalanced
// increment; the remaining steps (keyring resolve + struct return) cannot fail
// (the key resolve ignores its error and the return is a value copy), so the
// returned done is always balanced by exactly one caller defer.
func (a *App) withAIPreflight(pluginID, sessionToken, which string) (ai.AIProvider, string, func(), error) {
	a.vaultMu.RLock()
	// Reject new AI calls once a vault close/switch has begun. The check and
	// the vaultClosingWG.Add below share this RLock hold, so they are atomic
	// w.r.t. CloseVault/SwitchVault's closing=true + Wait (taken under the
	// write lock) — no TOCTOU window where a call slips through after the
	// drain returns (#452).
	if a.closing {
		a.vaultMu.RUnlock()
		return ai.AIProvider{}, "", nil, vaultClosingError()
	}
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		a.vaultMu.RUnlock()
		return ai.AIProvider{}, "", nil, err
	}
	if err := a.requireGrant(pluginID, plugins.CapAI); err != nil {
		a.vaultMu.RUnlock()
		return ai.AIProvider{}, "", nil, err
	}
	if a.rateLimiter != nil && !a.rateLimiter.allow(a.vaultPath, pluginID) {
		a.vaultMu.RUnlock()
		a.recordRateLimited(pluginID)
		rps, burst := resolvePluginRatelimit(a.vaultPath, pluginID)
		return ai.AIProvider{}, "", nil, fmt.Errorf("plugin %q AI rate limit exceeded (max %.1f rps, burst %d); retry after a short delay", pluginID, rps, burst)
	}
	a.configMu.RLock()
	p := aiConfigBlock(a.cfg.AI, which)
	useKeyring := a.aiUseKeyringLocked()
	user := a.aiKeyringUser(which)
	a.configMu.RUnlock()
	// All gates passed: this call will issue HTTP and outlive the RLock. Add
	// to the vault-close drain so CloseVault/SwitchVault wait for it before
	// teardown (#452). The returned done func balances this Add; the caller
	// defers it on the success path.
	a.vaultClosingWG.Add(1)
	a.vaultMu.RUnlock()
	key, _ := a.resolveAIKeyUnlocked(user, useKeyring, p.APIKey)
	provider := ai.AIProvider{
		ProviderType:    p.ProviderType,
		BaseURL:         p.BaseURL,
		APIKey:          key,
		Model:           p.Model,
		ReasoningEffort: p.ReasoningEffort,
		TimeoutMs:       p.TimeoutMs,
	}
	return provider, p.Model, func() { a.vaultClosingWG.Done() }, nil
}

// PluginAIComplete performs a chat completion on behalf of a plugin. Gated by
// the ai capability; rate-limited and audit-logged exactly like PluginFetch.
// Credentials are read server-side and never returned to the caller. (#216.)
//
// When input.Stream is true (#226), the call returns immediately with
// stream_id set and pushes deltas via ai:complete:delta / done / error events.
// Cancel with PluginAICancelStream. Audit records one start+final status row
// (not per-token).
func (a *App) PluginAIComplete(pluginID, sessionToken string, input PluginAICompleteInput) (ai.CompleteResult, error) {
	// Tracked by a.wg so shutdown's a.wg.Wait() drains in-flight AI calls
	// before teardownVaultServices clears the audit state. Unlike PluginFetch
	// (which holds vaultMu.RLock for its whole body), PluginAIComplete releases
	// vaultMu after withAIPreflight so a long LLM call doesn't hold the vault
	// lock — which means vaultMu alone can't serialize the call against a close.
	a.wg.Add(1)
	// Validate reasoning_effort BEFORE withAIPreflight so an invalid call
	// doesn't consume a rate-limit slot or snapshot the provider config.
	if input.ReasoningEffort != nil {
		if re := strings.TrimSpace(*input.ReasoningEffort); re != "" && !config.IsValidAIReasoningEffort(re) {
			a.wg.Done()
			return ai.CompleteResult{}, &ai.AIError{Kind: ai.ErrBadRequest, Message: fmt.Sprintf("invalid reasoning_effort %q: must be one of none, minimal, low, medium, high, xhigh, max", *input.ReasoningEffort)}
		}
	}
	// Validate tools + tool_choice at the boundary so a malformed request
	// fails fast (bad-request) before snapshotting config or rate-limiting.
	// Providers normalize inconsistently; this keeps one source of truth.
	if verr := validateAITools(input.Tools, input.ToolChoice); verr != nil {
		a.wg.Done()
		return ai.CompleteResult{}, &ai.AIError{Kind: ai.ErrBadRequest, Message: verr.Error()}
	}
	provider, configuredModel, drainDone, err := a.withAIPreflight(pluginID, sessionToken, "chat")
	if err != nil {
		a.wg.Done()
		return ai.CompleteResult{}, err
	}
	// Effective model for auditing: the per-call override, else the configured one.
	effectiveModel := input.Model
	if effectiveModel == "" {
		effectiveModel = configuredModel
	}
	messages := make([]ai.ChatMessage, len(input.Messages))
	for i, m := range input.Messages {
		messages[i] = ai.ChatMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCalls:  toAIToolCalls(m.ToolCalls),
			ToolCallID: m.ToolCallID,
		}
	}
	req := ai.CompleteRequest{
		Provider:        provider,
		Messages:        messages,
		Model:           input.Model,
		Temperature:     input.Temperature,
		MaxTokens:       input.MaxTokens,
		ReasoningEffort: input.ReasoningEffort,
		ResponseSchema:  input.ResponseSchema,
		Tools:           toAIToolDefs(input.Tools),
		ToolChoice:      toAIToolChoice(input.ToolChoice),
	}

	if input.Stream {
		return a.startAIStream(pluginID, provider, effectiveModel, req, drainDone)
	}

	defer a.wg.Done()
	// Preflight registered this call with the vault-close drain (the Add ran
	// inside withAIPreflight on this success path). Balance it now that HTTP +
	// audit will run, so CloseVault/SwitchVault's drain can't under-count.
	defer drainDone()
	result, callErr := ai.Complete(a.aiContext(), req)
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

// startAIStream launches an async CompleteStream and returns stream_id immediately.
// The caller's a.wg.Add(1) is balanced when the stream goroutine finishes.
// drainDone is deferred inside the goroutine so vault-close waits for the stream.
func (a *App) startAIStream(pluginID string, provider ai.AIProvider, effectiveModel string, req ai.CompleteRequest, drainDone func()) (ai.CompleteResult, error) {
	// Owner-scoped stream events are named ":<pluginID>"; an empty id would
	// fall back to the global unscoped bus (#635). Preflight validates this, but
	// assert structurally so a future refactor cannot silently regress it.
	if pluginID == "" {
		a.wg.Done()
		drainDone()
		return ai.CompleteResult{}, &ai.AIError{Kind: ai.ErrBadRequest, Message: "plugin_id is required for a streamed completion"}
	}
	streamID, err := newAIStreamID()
	if err != nil {
		a.wg.Done()
		drainDone()
		return ai.CompleteResult{}, &ai.AIError{Kind: ai.ErrUnknown, Message: fmt.Sprintf("allocate stream id: %v", err)}
	}
	// Child of vault/app AI context so close/shutdown cancels the HTTP body.
	streamCtx, streamCancel := context.WithCancel(a.aiContext())

	ready := make(chan struct{})
	a.aiStreamsMu.Lock()
	if a.aiStreams == nil {
		a.aiStreams = make(map[string]*aiStreamSession)
	}
	a.aiStreams[streamID] = &aiStreamSession{
		pluginID: pluginID,
		cancel:   streamCancel,
		ready:    ready,
	}
	a.aiStreamsMu.Unlock()

	// Buffered channel for backpressure between SSE reader and event emit.
	// Producer aborts if the buffer fills (consumer not keeping up).
	deltaCh := make(chan string, aiStreamBufferCap)
	toolDeltaCh := make(chan ai.ToolCallDelta, aiStreamBufferCap)

	// Audit stream start (one row); terminal status is audited when the
	// goroutine finishes (#226 — not per-token).
	a.auditAI(pluginID, aiChatKind, provider.BaseURL, effectiveModel, "stream-start", nil)

	go func() {
		defer a.wg.Done()
		defer drainDone()
		defer streamCancel()
		defer func() {
			a.aiStreamsMu.Lock()
			delete(a.aiStreams, streamID)
			a.aiStreamsMu.Unlock()
		}()

		// Wait for the frontend to attach Events.On listeners (PluginAIStreamReady)
		// before starting the upstream request. Immediate failures (native
		// provider reject, empty model) would otherwise emit done/error before
		// createAIStream installs handlers, leaving the client hung (PR #540).
		select {
		case <-ready:
		case <-time.After(aiStreamReadyWait):
		case <-streamCtx.Done():
			a.auditAI(pluginID, aiChatKind, provider.BaseURL, effectiveModel, "cancelled", nil)
			a.emit(aiStreamEventName(aiEventCompleteError, pluginID), map[string]any{
				"stream_id": streamID,
				"plugin_id": pluginID,
				"kind":      string(ai.ErrCanceled),
				"message":   "stream cancelled before start",
			})
			return
		}

		// Fan-out deltas to Wails events on a separate goroutine so the SSE
		// parser only blocks on the bounded channel (backpressure), not on IPC.
		// Event names are owner-scoped by pluginID (#635).
		emitDone := make(chan struct{})
		go func() {
			defer close(emitDone)
			idx := 0
			for delta := range deltaCh {
				a.emit(aiStreamEventName(aiEventCompleteDelta, pluginID), map[string]any{
					"stream_id": streamID,
					"plugin_id": pluginID,
					"delta":     delta,
					"index":     idx,
				})
				idx++
			}
		}()

		// Fan-out tool-call fragments to a parallel event so the chat UX can
		// surface in-progress tool invocations live (#595).
		emitToolDone := make(chan struct{})
		go func() {
			defer close(emitToolDone)
			for frag := range toolDeltaCh {
				a.emit(aiStreamEventName(aiEventCompleteToolDelta, pluginID), map[string]any{
					"stream_id":          streamID,
					"plugin_id":          pluginID,
					"index":              frag.Index,
					"id":                 frag.ID,
					"name":               frag.Name,
					"arguments_fragment": frag.ArgumentsFragment,
				})
			}
		}()

		// Natural backpressure: block until the emit goroutine drains a slot
		// or the stream is cancelled. A default arm would turn a momentary
		// full buffer into a hard abort mid-answer (PR #540 review).
		result, callErr := ai.CompleteStream(streamCtx, req, func(delta string) error {
			select {
			case deltaCh <- delta:
				return nil
			case <-streamCtx.Done():
				return streamCtx.Err()
			}
		}, func(frag ai.ToolCallDelta) error {
			select {
			case toolDeltaCh <- frag:
				return nil
			case <-streamCtx.Done():
				return streamCtx.Err()
			}
		})
		close(deltaCh)
		close(toolDeltaCh)
		<-emitDone
		<-emitToolDone

		status := "ok"
		if callErr != nil {
			status = aiErrKind(callErr)
			// Cancellation is a first-class terminal status for audit.
			if streamCtx.Err() != nil && (errors.Is(callErr, context.Canceled) || strings.Contains(callErr.Error(), "cancel")) {
				status = "cancelled"
			}
			a.auditAI(pluginID, aiChatKind, provider.BaseURL, effectiveModel, status, nil)
			kind, msg := "unknown", callErr.Error()
			if e, ok := callErr.(*ai.AIError); ok {
				kind, msg = string(e.Kind), e.Message
			}
			a.emit(aiStreamEventName(aiEventCompleteError, pluginID), map[string]any{
				"stream_id": streamID,
				"plugin_id": pluginID,
				"kind":      kind,
				"message":   msg,
			})
			return
		}
		a.auditAI(pluginID, aiChatKind, provider.BaseURL, effectiveModel, status, result.Usage)
		payload := map[string]any{
			"stream_id": streamID,
			"plugin_id": pluginID,
			"content":   result.Content,
			"model":     result.Model,
		}
		if len(result.ToolCalls) > 0 {
			payload["tool_calls"] = result.ToolCalls
		}
		if result.Usage != nil {
			payload["usage"] = result.Usage
		}
		a.emit(aiStreamEventName(aiEventCompleteDone, pluginID), payload)
	}()

	return ai.CompleteResult{StreamID: streamID, Model: effectiveModel}, nil
}

// PluginAICancelStream aborts an in-flight streamed completion started by
// PluginAIComplete(stream=true). The plugin must own the stream (pluginID match).
// Idempotent: cancelling an unknown/finished stream is a no-op success.
func (a *App) PluginAICancelStream(pluginID, sessionToken, streamID string) error {
	if err := a.requirePluginSession(pluginID, sessionToken); err != nil {
		return err
	}
	if err := a.requireGrant(pluginID, plugins.CapAI); err != nil {
		return err
	}
	streamID = strings.TrimSpace(streamID)
	if streamID == "" {
		return &ai.AIError{Kind: ai.ErrBadRequest, Message: "stream_id is required"}
	}
	a.aiStreamsMu.Lock()
	sess, ok := a.aiStreams[streamID]
	if ok && sess.pluginID == pluginID {
		// Leave the map entry; the stream goroutine removes it on exit.
		cancel := sess.cancel
		// Unblock a producer still waiting on ready so it observes cancel.
		if sess.ready != nil {
			sess.readyOnce.Do(func() { close(sess.ready) })
		}
		a.aiStreamsMu.Unlock()
		cancel()
		return nil
	}
	a.aiStreamsMu.Unlock()
	return nil
}

// cancelAllAIStreams aborts every in-flight streamed completion. Used when AI
// is turned off so active provider requests stop immediately instead of
// running to their per-call timeout. Mirrors PluginAICancelStream's per-stream
// teardown (close the ready gate, then cancel) but for the whole map. The
// ready gate is closed so a producer still blocked on the ready handshake
// observes the cancel (#632).
func (a *App) cancelAllAIStreams() {
	a.aiStreamsMu.Lock()
	sessions := make([]*aiStreamSession, 0, len(a.aiStreams))
	for _, s := range a.aiStreams {
		sessions = append(sessions, s)
	}
	a.aiStreamsMu.Unlock()
	// Cancel outside the lock: each stream's goroutine re-acquires aiStreamsMu
	// in its cleanup defer to delete itself, so holding it across cancel() would
	// self-deadlock.
	for _, s := range sessions {
		if s.ready != nil {
			s.readyOnce.Do(func() { close(s.ready) })
		}
		s.cancel()
	}
}

// PluginAIStreamReady signals that the frontend has attached Events.On
// listeners for streamID and is ready to receive deltas/terminal events.
// Must be called after PluginAIComplete(stream=true) returns stream_id.
// Idempotent; unknown streams are a no-op success.
func (a *App) PluginAIStreamReady(pluginID, sessionToken, streamID string) error {
	if err := a.requirePluginSession(pluginID, sessionToken); err != nil {
		return err
	}
	if err := a.requireGrant(pluginID, plugins.CapAI); err != nil {
		return err
	}
	streamID = strings.TrimSpace(streamID)
	if streamID == "" {
		return &ai.AIError{Kind: ai.ErrBadRequest, Message: "stream_id is required"}
	}
	a.aiStreamsMu.Lock()
	sess, ok := a.aiStreams[streamID]
	if ok && sess.pluginID == pluginID && sess.ready != nil {
		sess.readyOnce.Do(func() { close(sess.ready) })
	}
	a.aiStreamsMu.Unlock()
	return nil
}

func newAIStreamID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// requirePluginSession validates the session token maps to pluginID. Shared by
// stream cancel and other AI bindings that need identity without full preflight.
func (a *App) requirePluginSession(pluginID, sessionToken string) error {
	a.pluginSessionsMu.RLock()
	owner, ok := a.pluginSessions[sessionToken]
	a.pluginSessionsMu.RUnlock()
	if !ok || owner != pluginID {
		return fmt.Errorf("invalid plugin session")
	}
	return nil
}

// maxPluginAIAuditEventJSONBytes rejects oversized event payloads before
// json.Unmarshal so CapAI plugins cannot force multi-megabyte allocations
// (the on-disk Detail cap is only 2 KiB after filtering).
const maxPluginAIAuditEventJSONBytes = 8 * 1024

// maxPluginAIAuditKindLen bounds the kind string stored on each log row.
const maxPluginAIAuditKindLen = 64

// PluginAIAuditEvent appends a structured agent/tool/staging audit row to the
// AI audit log (#630). Gated by CapAI + session. eventJSON is a JSON object;
// only allowlisted metadata keys are retained (never freeform note bodies).
func (a *App) PluginAIAuditEvent(pluginID, sessionToken, eventJSON string) error {
	if err := a.requirePluginSession(pluginID, sessionToken); err != nil {
		return err
	}
	if err := a.requireGrant(pluginID, plugins.CapAI); err != nil {
		return err
	}
	eventJSON = strings.TrimSpace(eventJSON)
	if eventJSON == "" {
		return &ai.AIError{Kind: ai.ErrBadRequest, Message: "event JSON is required"}
	}
	if len(eventJSON) > maxPluginAIAuditEventJSONBytes {
		return &ai.AIError{
			Kind:    ai.ErrBadRequest,
			Message: fmt.Sprintf("event JSON exceeds %d-byte cap", maxPluginAIAuditEventJSONBytes),
		}
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(eventJSON), &fields); err != nil {
		return &ai.AIError{Kind: ai.ErrBadRequest, Message: fmt.Sprintf("invalid event JSON: %v", err)}
	}
	kind, _ := fields["kind"].(string)
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "event"
	}
	if len(kind) > maxPluginAIAuditKindLen {
		return &ai.AIError{
			Kind:    ai.ErrBadRequest,
			Message: fmt.Sprintf("event kind exceeds %d characters", maxPluginAIAuditKindLen),
		}
	}
	// Drop the kind key from detail fields; it is stored on the entry.
	delete(fields, "kind")
	a.auditAIEvent(pluginID, kind, fields)
	return nil
}

// PluginAIEmbed computes embeddings for a batch of texts on behalf of a plugin.
// Gated by the ai capability; rate-limited and audit-logged exactly like
// PluginFetch. Credentials are read server-side and never returned to the
// caller. (#216.)
func (a *App) PluginAIEmbed(pluginID, sessionToken string, input PluginAIEmbedInput) (ai.EmbedResult, error) {
	// Tracked by a.wg — same rationale as PluginAIComplete (see above).
	a.wg.Add(1)
	defer a.wg.Done()
	provider, configuredModel, drainDone, err := a.withAIPreflight(pluginID, sessionToken, "embedding")
	if err != nil {
		return ai.EmbedResult{}, err
	}
	// Preflight registered this call with the vault-close drain (the Add ran
	// inside withAIPreflight on this success path); balance it via the returned
	// done func. See PluginAIComplete.
	defer drainDone()
	effectiveModel := input.Model
	if effectiveModel == "" {
		effectiveModel = configuredModel
	}
	result, callErr := ai.Embed(a.aiContext(), ai.EmbedRequest{
		Provider:   provider,
		Texts:      input.Texts,
		Model:      input.Model,
		Dimensions: input.Dimensions,
		TaskType:   input.TaskType,
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

// toAIToolDefs maps the plugin-facing tool defs into the ai package's
// normalized ToolDef. The shapes are identical; this keeps the conversion in
// one place so future divergence is intentional, not accidental.
func toAIToolDefs(in []PluginAIToolDef) []ai.ToolDef {
	if len(in) == 0 {
		return nil
	}
	out := make([]ai.ToolDef, len(in))
	for i, t := range in {
		out[i] = ai.ToolDef{Name: t.Name, Description: t.Description, Parameters: t.Parameters}
	}
	return out
}

// toAIToolCalls maps plugin-facing tool calls into the ai package's ToolCall.
func toAIToolCalls(in []PluginAIToolCall) []ai.ToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]ai.ToolCall, len(in))
	for i, c := range in {
		out[i] = ai.ToolCall{ID: c.ID, Name: c.Name, Arguments: c.Arguments}
	}
	return out
}

// toAIToolChoice maps the plugin-facing tool choice into the ai package's
// pointer-based ToolChoice (nil stays nil → provider default).
func toAIToolChoice(in *PluginAIToolChoice) *ai.ToolChoice {
	if in == nil {
		return nil
	}
	return &ai.ToolChoice{Mode: in.Mode, ToolName: in.ToolName}
}

// validateAITools rejects malformed tool definitions and tool_choice at the
// plugin boundary. Tool names must be non-empty; tool_choice.mode must be a
// known value; a "force" choice must name a declared tool. Providers each
// normalize tool_choice in their own shape and only partially coerce bad
// input, so this keeps a single source of truth before rate-limiting or HTTP.
func validateAITools(tools []PluginAIToolDef, choice *PluginAIToolChoice) error {
	seen := make(map[string]struct{}, len(tools))
	for _, t := range tools {
		if strings.TrimSpace(t.Name) == "" {
			return fmt.Errorf("tool definitions must each carry a non-empty name")
		}
		if _, dup := seen[t.Name]; dup {
			return fmt.Errorf("duplicate tool name %q", t.Name)
		}
		seen[t.Name] = struct{}{}
		// parameters must be a JSON Schema object. Reject missing, scalar,
		// and array shapes that providers would forward verbatim and then
		// reject or misparse.
		if len(t.Parameters) == 0 {
			return fmt.Errorf("tool %q parameters must be a JSON Schema object", t.Name)
		}
		var params map[string]any
		if err := json.Unmarshal(t.Parameters, &params); err != nil {
			return fmt.Errorf("tool %q parameters must be a JSON object: %w", t.Name, err)
		}
		if pt, ok := params["type"].(string); ok && pt != "object" {
			return fmt.Errorf("tool %q parameters must be type \"object\", got %q", t.Name, pt)
		}
	}
	if choice == nil {
		return nil
	}
	switch choice.Mode {
	case ai.ToolChoiceAuto, ai.ToolChoiceRequired, ai.ToolChoiceNone, ai.ToolChoiceForce:
	default:
		return fmt.Errorf("tool_choice.mode %q must be one of auto, required, none, force", choice.Mode)
	}
	if choice.Mode == ai.ToolChoiceForce {
		if strings.TrimSpace(choice.ToolName) == "" {
			return fmt.Errorf(`tool_choice.mode "force" requires tool_name`)
		}
		for _, t := range tools {
			if t.Name == choice.ToolName {
				return nil
			}
		}
		return fmt.Errorf("tool_choice forces unknown tool %q", choice.ToolName)
	}
	return nil
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
	_ = a.saveConfigTracked(a.cfg)
}
