package config

import (
	"strings"

	"silt/backend/ai"
	"silt/backend/mcp"
)

// AIConfig holds the shared chat-LLM and embedding-model provider configuration
// (Sprint 20) plus product-level feature enablement (#632). Chat and embedding
// providers are INDEPENDENT: a user may run a local Ollama chat model and a
// cloud embedding endpoint, or any other combination.
type AIConfig struct {
	Chat      AIProviderConfig `yaml:"chat" json:"chat"`
	Embedding AIProviderConfig `yaml:"embedding" json:"embedding"`
	// Features is the user-facing AI product switch (#632): one master enable
	// plus dependent RAG / summaries toggles. First-party AI plugins load from
	// these flags, not from independent Plugins-tab toggles.
	Features AIFeaturesConfig `yaml:"features" json:"features"`
	// LocalMCP is the vault-scoped local Model Context Protocol host (#687).
	// Default off. When enabled with a vault open, Silt serves tools on
	// loopback HTTP and via `silt mcp` stdio proxy.
	LocalMCP mcp.Config `yaml:"local_mcp,omitempty" json:"local_mcp"`
	// UseKeyring, when true (default), stores provider API keys in the OS
	// credential store instead of plaintext config.yaml (#218). Tri-state so
	// "unset" stays distinguishable from "explicitly false" through the Load →
	// normalize path. nil reads as true downstream.
	UseKeyring *bool `yaml:"use_keyring,omitempty" json:"use_keyring,omitempty"`
}

// AIFeaturesConfig is the product-level AI enablement model (#632).
// Defaults are all false (opt-in). NormalizeAIConfig clamps dependents when
// master is off so impossible combinations cannot persist.
type AIFeaturesConfig struct {
	// Enabled is the master "Enable AI" switch: agent drawer, writing assistant.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// RAGEnabled gates semantic search / Q&A index and agent retrieval tools.
	// Requires Enabled; embedding readiness is enforced in the UI/loader, not here.
	RAGEnabled bool `yaml:"rag_enabled" json:"rag_enabled"`
	// SummariesEnabled gates the note-summary banner. Requires Enabled.
	SummariesEnabled bool `yaml:"summaries_enabled" json:"summaries_enabled"`
}

// First-party AI plugin IDs driven by AIFeaturesConfig (not plugins.disabled).
const (
	AIPluginAgent     = "silt-ai-agent"
	AIPluginQA        = "silt-ai-qa"
	AIPluginAssistant = "silt-ai-assistant"
	AIPluginSummary   = "silt-ai-summary"
)

// IsFirstPartyAIPlugin reports whether id is one of the four AI capability modules.
func IsFirstPartyAIPlugin(id string) bool {
	switch id {
	case AIPluginAgent, AIPluginQA, AIPluginAssistant, AIPluginSummary:
		return true
	default:
		return false
	}
}

// AIPluginLoadEnabled reports whether a first-party AI plugin should register
// a session under the current feature flags. Non-AI ids return false.
func AIPluginLoadEnabled(features AIFeaturesConfig, pluginID string) bool {
	switch pluginID {
	case AIPluginAgent, AIPluginAssistant:
		return features.Enabled
	case AIPluginQA:
		return features.Enabled && features.RAGEnabled
	case AIPluginSummary:
		return features.Enabled && features.SummariesEnabled
	default:
		return false
	}
}

// AIProviderConfig is one provider endpoint (chat OR embedding). It is the unit
// the AI Provider settings page edits. APIKey carries the yaml tag so the value
// persists to config.yaml (the migration/fallback slot when the OS keyring is
// unavailable), but the json tag is "-" so a GetSystemConfig / GetAIProviderConfig
// round-trip can NEVER leak the secret to plugin JS or the frontend. The
// dedicated SetAIAPIKey binding is the only write path; a full SaveSystemConfig
// round-trip preserves the existing key server-side (see SaveSystemConfig).
type AIProviderConfig struct {
	ProviderType    ai.AIProviderType `yaml:"provider_type,omitempty" json:"provider_type"`                 // ai.ProviderLocal | ai.ProviderOpenAICompatible | ai.ProviderGoogle | ai.ProviderAnthropic
	BaseURL         string            `yaml:"base_url,omitempty" json:"base_url"`                           // e.g. http://localhost:11434 (Ollama) or https://openrouter.ai/api/v1
	APIKey          string            `yaml:"api_key,omitempty" json:"-"`                                   // NEVER serialized to JS
	Model           string            `yaml:"model,omitempty" json:"model"`                                 // e.g. qwen3:30b-a3b, nomic-embed-text
	Temperature     *float64          `yaml:"temperature,omitempty" json:"temperature,omitempty"`           // chat only
	MaxTokens       *int              `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty"`             // chat only
	ReasoningEffort *string           `yaml:"reasoning_effort,omitempty" json:"reasoning_effort,omitempty"` // chat only: "none"|"minimal"|"low"|"medium"|"high"|"xhigh"|"max"
	TimeoutMs       *int              `yaml:"timeout_ms,omitempty" json:"timeout_ms,omitempty"`             // per-call; default 60000
	Dimensions      *int              `yaml:"dimensions,omitempty" json:"dimensions,omitempty"`             // embeddings only (truncation)
}

// AI provider type discriminators live in backend/ai (the canonical
// ai.AIProviderType enum). config imports that type rather than re-declaring
// the literals so the dispatcher and the persisted config cannot drift — see
// ai.ProviderLocal / ProviderOpenAICompatible / ProviderGoogle /
// ProviderAnthropic. "local" targets an Ollama/llama.cpp instance on the same
// machine (no key expected by default); "openai-compatible" targets a
// cloud/local OpenAI-compatible endpoint (OpenRouter, LM Studio, OpenAI,
// llama-server) where a Bearer token is expected. "google" and "anthropic"
// target the providers' native first-party APIs (#479), bypassing the OpenAI
// compat shape for better stability + structured-output support.

// DefaultAIBaseURL is the conventional local endpoint (Ollama's default port).
// Used as the default base URL when providerType is "local" and none is set.
const DefaultAIBaseURL = "http://localhost:11434"

// DefaultGoogleBaseURL is the Google AI Studio (generativelanguage) endpoint.
// The native generateContent / batchEmbedContents / listModels paths are rooted
// under /v1beta/.
const DefaultGoogleBaseURL = "https://generativelanguage.googleapis.com"

// DefaultAnthropicBaseURL is the Anthropic Messages API endpoint.
const DefaultAnthropicBaseURL = "https://api.anthropic.com"

// DefaultAITimeoutMs is the per-call timeout when AIProviderConfig.TimeoutMs is
// unset. Generous (LLM completions are slow) but bounded so a dead endpoint
// cannot hang a plugin call forever.
const DefaultAITimeoutMs = 60000

// validAIReasoningEfforts is the set of reasoning_effort values accepted across
// OpenAI-compatible providers (OpenAI, Ollama, vLLM, OpenRouter, …). "none"
// means "do not send the parameter"; the others ramp the reasoning budget. Kept
// as the single source of truth so the binding layer can reject a typo at the
// gate (instead of forwarding it to a provider for a 400) and NormalizeAIConfig
// can drop a stale/unknown value from a hand-edited config.yaml.
var validAIReasoningEfforts = map[string]bool{
	"none": true, "minimal": true, "low": true,
	"medium": true, "high": true, "xhigh": true, "max": true,
}

// IsValidAIReasoningEffort reports whether s is a recognized reasoning_effort
// value. Callers pass a already-trimmed value. Used by the AI binding layer
// (UpdateAIProviderConfig / PluginAIComplete) to reject unknown values early.
func IsValidAIReasoningEffort(s string) bool {
	return validAIReasoningEfforts[s]
}

// NormalizeAIConfig applies the AI provider normalization rules and feature
// dependency clamps. Exported so the dedicated UpdateAIProviderConfig binding
// can normalize a single patch the same way the full-config normalize path does.
func NormalizeAIConfig(ai AIConfig) AIConfig {
	if ai.UseKeyring == nil {
		ai.UseKeyring = boolPtr(true)
	}
	ai.Chat = normalizeAIProvider(ai.Chat, true)
	ai.Embedding = normalizeAIProvider(ai.Embedding, false)
	// Dependents cannot outlive the master switch (#632).
	if !ai.Features.Enabled {
		ai.Features.RAGEnabled = false
		ai.Features.SummariesEnabled = false
	}
	ai.LocalMCP = mcp.NormalizeConfig(ai.LocalMCP)
	return ai
}

// normalizeAIProvider coerces one provider block into a canonical form. isChat
// distinguishes the chat block (Temperature/MaxTokens apply) from the embedding
// block (Dimensions applies); the unused advanced knob on the wrong block is
// dropped so a stale value cannot drift in config.yaml.
func normalizeAIProvider(p AIProviderConfig, isChat bool) AIProviderConfig {
	p.ProviderType = ai.AIProviderType(strings.TrimSpace(string(p.ProviderType)))
	switch p.ProviderType {
	case ai.ProviderLocal, ai.ProviderOpenAICompatible, ai.ProviderGoogle, ai.ProviderAnthropic:
		// known type — keep as-is
	default:
		// Unknown/empty → local (the safest default — nothing leaves the
		// machine, no key expected).
		p.ProviderType = ai.ProviderLocal
	}
	p.BaseURL = strings.TrimSpace(p.BaseURL)
	if p.BaseURL == "" {
		switch p.ProviderType {
		case ai.ProviderLocal:
			p.BaseURL = DefaultAIBaseURL
		case ai.ProviderGoogle:
			p.BaseURL = DefaultGoogleBaseURL
		case ai.ProviderAnthropic:
			p.BaseURL = DefaultAnthropicBaseURL
		}
	}
	p.Model = strings.TrimSpace(p.Model)
	p.APIKey = strings.TrimSpace(p.APIKey)
	// Validate reasoning_effort against the documented enum so a stale or
	// hand-typed unknown value is dropped rather than forwarded to a provider
	// for a 400. Applies to chat only; normalize drops it for embeddings below.
	if p.ReasoningEffort != nil {
		re := strings.TrimSpace(*p.ReasoningEffort)
		if IsValidAIReasoningEffort(re) {
			p.ReasoningEffort = &re
		} else {
			p.ReasoningEffort = nil
		}
	}
	// Drop advanced knobs that don't apply to this block so a user who flips
	// chat↔embedding in the UI doesn't leave a stale value behind.
	if isChat {
		p.Dimensions = nil
	} else {
		p.Temperature = nil
		p.MaxTokens = nil
		p.ReasoningEffort = nil
	}
	// Bound the per-call timeout. A negative value is nonsensical; an
	// absurdly large value would let a dead endpoint hang a plugin call. nil
	// is left to the service to default at call time.
	if p.TimeoutMs != nil {
		t := *p.TimeoutMs
		if t < 0 {
			t = 0
		}
		if t > 300000 { // 5 min hard cap
			t = 300000
		}
		p.TimeoutMs = intPtr(t)
	}
	// Dimensions must be positive when set (a 0/negative would truncate to
	// nothing). Left as a pointer so "unset" stays distinct from a deliberate
	// model-native value.
	if p.Dimensions != nil && *p.Dimensions <= 0 {
		p.Dimensions = nil
	}
	return p
}
