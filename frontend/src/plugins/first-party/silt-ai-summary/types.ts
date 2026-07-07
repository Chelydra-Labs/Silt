// Shared types for the silt-ai-summary plugin (#220–#223).
//
// The plugin extracts a structured summary from a note via the user-configured
// LLM and caches it by content hash. These types are the contract between the
// pure logic modules (extract / diff / cache / summarize) and the reactive
// shell (state.svelte.ts / the banner component).

/** The structured extraction the LLM is asked to produce. */
export interface SummaryExtraction {
  summary: string
  tasks: string[]
  risks: string[]
  decisions: string[]
}

/** A snapshot of which facets to surface + tuning knobs. Stored in
 *  config.yaml under plugins.plugin_settings.silt-ai-summary (snake_case keys,
 *  matching the rest of the config schema). */
export interface SummarySettings {
  auto_on_open: boolean
  on_demand_only: boolean
  summary_length: 'short' | 'medium' | 'long'
  facets: FacetToggles
  regenerate_debounce_ms: number
  max_note_chars: number
  /** Dismissed banners keyed by `pageId:contentHash` so a meaningfully-changed
   *  note re-shows its banner. */
  dismissed_notes: string[]
}

export interface FacetToggles {
  tasks: boolean
  risks: boolean
  decisions: boolean
}

/** The "new since last generation" diff per facet. Each entry is the model's
 *  original (un-normalized) wording for an item present now but absent in the
 *  prior snapshot. */
export interface FacetDiff {
  tasks: string[]
  risks: string[]
  decisions: string[]
}

/** A fully-resolved summary ready to render. */
export interface SummaryResult {
  summary: string
  tasks: string[]
  risks: string[]
  decisions: string[]
  newItems: FacetDiff
  /** True when served from the content-hash cache (no LLM call this open). */
  fromCache: boolean
  /** The model that produced the extraction; drives cache invalidation when
   *  the user switches models. */
  model: string
  /** RFC3339 generation timestamp. */
  generatedAt: string
}

export type SummaryErrorCode =
  | 'unconfigured' // no provider ready — banner shows the setup nudge
  | 'provider-error' // the call ran but the endpoint errored
  | 'oversized' // note exceeds max_note_chars and chunking is not implemented
  | 'unknown'

export interface SummaryError {
  code: SummaryErrorCode
  message: string
}

/** The discriminated outcome the orchestrator returns. Errors are always
 *  surfaced here — never thrown — so the banner can render a non-blocking
 *  inline state instead of propagating a rejection to the UI thread. */
export type SummaryOutcome = { ok: true; result: SummaryResult } | { ok: false; error: SummaryError }

/** The orchestrator call signature. Pure given these inputs — no event
 *  subscriptions, no timers, no DOM. The reactive shell (state.svelte.ts) is
 *  responsible for debouncing saves, switching notes, and calling this. */
export interface SummarizeDeps {
  /** Stable page identity: `${notebook}/${section}/${page}`. Renames miss the
   *  cache (acceptable — re-summarize on next open). */
  pageId: string
  cleanContent: string
  settings: SummarySettings
  /** The currently-configured chat model id, for cache invalidation + the
   *  configured gate. Empty string when no model is configured. */
  configuredModel: string
  /** False until a provider is configured — when false, summarize returns an
   *  'unconfigured' outcome WITHOUT calling ctx.ai.complete (no network until
   *  configured, per #220). */
  isConfigured: boolean
  /** True forces a cache bypass (Regenerate). */
  force?: boolean
}
