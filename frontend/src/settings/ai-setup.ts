// Shared "is the AI chat provider ready" predicate (#450).
//
// Two surfaces nudge the user toward AI provider setup: the Plugins-tab badge
// on AI-capable plugin cards and the AI Provider tab's in-tab banner. They
// previously disagreed (badge: just !chatModel; in-tab: both providers still
// on local defaults + no keys), so a user could click the badge and land on a
// tab showing no nudge (or vice versa). This helper is the single source of
// truth both consume so the two stay coherent.
//
// Semantic: "the chat provider cannot serve a completion right now." That is
// what an AI plugin (e.g. silt-ai-summary) actually needs; embedding is a
// separate concern and does not gate the badge.
//
// Data asymmetry: the AI Provider tab reads GetAIProviderConfig (which exposes
// has_key), but the Plugins tab reads the SystemConfig view where API keys are
// scrubbed (json:"-"). The predicate therefore treats an UNKNOWN key state
// (has_key omitted) as non-blocking — the Plugins badge nudges only on a missing
// model, and a genuinely-missing key surfaces as a retryable 'unauthorized'
// error at call time (shown inline by the banner). This keeps the click-the-
// badge-lands-on-a-nudge guarantee without forcing the Plugins tab to make an
// extra async binding call per card.

export interface AIProviderReadiness {
  /** "local" (Ollama/llama.cpp, keyless) or "openai-compatible" (needs a key). */
  provider_type?: string
  /** The chosen chat model id. */
  model?: string
  /** Whether a key is configured. Omit when the caller cannot see keys
   *  (SystemConfig view); the predicate then does not nudge on the key. */
  has_key?: boolean
}

/** True when the chat provider is not ready to serve a completion. */
export function aiProviderNeedsSetup(
  chat: AIProviderReadiness | null | undefined
): boolean {
  if (!chat) return true
  if (!chat.model) return true
  // Local providers run keyless. Cloud/openai-compatible endpoints need a key —
  // but only nudge when we KNOW it's absent (has_key === false). An unknown key
  // state does not false-fire (the Plugins-tab badge is model-gated).
  if (chat.provider_type !== 'local' && chat.has_key === false) return true
  return false
}

/**
 * True when the embedding provider is not ready (#224). Same readiness rules
 * as chat; kept as a named export so Q&A index build and chat complete can
 * gate independently.
 */
export function embeddingProviderNeedsSetup(
  embedding: AIProviderReadiness | null | undefined
): boolean {
  return aiProviderNeedsSetup(embedding)
}
