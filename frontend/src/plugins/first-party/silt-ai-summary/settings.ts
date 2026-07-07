// Default settings + resolver for silt-ai-summary (#223).
//
// Stored in <vault>/.system/config.yaml under
// plugins.plugin_settings.silt-ai-summary (snake_case keys, matching the
// config schema convention). resolveSettings deep-merges the user's stored
// values over the defaults so a setting added in a later release is present
// even on vaults that pre-date it.

import type { SummarySettings } from './types'

export const DEFAULT_SETTINGS: SummarySettings = {
  auto_on_open: true,
  on_demand_only: false,
  summary_length: 'medium',
  facets: { tasks: true, risks: true, decisions: true },
  regenerate_debounce_ms: 3000,
  // Notes larger than this are skipped in v1 (chunking is a documented future
  // enhancement — see README). 12k chars ≈ a 2–3k-token prompt headroom under
  // the typical 4k context window of a local 7B model.
  max_note_chars: 12000,
  dismissed_notes: []
}

/** Merge stored settings over defaults. Stored values win per-key; nested
 *  `facets` is merged field-by-field so toggling one facet doesn't blank the
 *  others on an upgrade. Unknown keys are ignored (forward-compat). */
export function resolveSettings(raw: Record<string, unknown> | undefined | null): SummarySettings {
  if (!raw) return { ...DEFAULT_SETTINGS, facets: { ...DEFAULT_SETTINGS.facets } }
  const out: SummarySettings = { ...DEFAULT_SETTINGS, facets: { ...DEFAULT_SETTINGS.facets } }
  if (typeof raw.auto_on_open === 'boolean') out.auto_on_open = raw.auto_on_open
  if (typeof raw.on_demand_only === 'boolean') out.on_demand_only = raw.on_demand_only
  if (raw.summary_length === 'short' || raw.summary_length === 'medium' || raw.summary_length === 'long') {
    out.summary_length = raw.summary_length
  }
  if (raw.facets && typeof raw.facets === 'object') {
    const f = raw.facets as Record<string, unknown>
    if (typeof f.tasks === 'boolean') out.facets.tasks = f.tasks
    if (typeof f.risks === 'boolean') out.facets.risks = f.risks
    if (typeof f.decisions === 'boolean') out.facets.decisions = f.decisions
  }
  if (typeof raw.regenerate_debounce_ms === 'number' && raw.regenerate_debounce_ms >= 0) {
    out.regenerate_debounce_ms = raw.regenerate_debounce_ms
  }
  if (typeof raw.max_note_chars === 'number' && raw.max_note_chars > 0) {
    out.max_note_chars = raw.max_note_chars
  }
  if (Array.isArray(raw.dismissed_notes)) {
    out.dismissed_notes = raw.dismissed_notes.filter((s): s is string => typeof s === 'string')
  }
  return out
}

/** Approximate output-token budget per summary_length. Generous enough for the
 *  JSON envelope (summary + up to several facet items) without wasting compute
 *  on a wall of text. Used by extract.ts to size the completion. */
export function maxTokensForLength(length: SummarySettings['summary_length']): number {
  switch (length) {
    case 'short':
      return 300
    case 'long':
      return 1200
    case 'medium':
    default:
      return 600
  }
}
