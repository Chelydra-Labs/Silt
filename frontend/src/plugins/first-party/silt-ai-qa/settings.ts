// Default settings + resolver for silt-ai-qa (#228).

import type { QASettings } from './types'

export const DEFAULT_SETTINGS: QASettings = {
  notebook_scope: [],
  auto_reembed: true,
  hybrid_weight: 0.6,
  top_k: 8,
  min_score: 0,
  max_context_chars: 12000,
  reindex_debounce_ms: 2000
}

export function resolveSettings(
  raw: Record<string, unknown> | undefined | null
): QASettings {
  if (!raw) return { ...DEFAULT_SETTINGS }
  const out: QASettings = { ...DEFAULT_SETTINGS }
  if (Array.isArray(raw.notebook_scope)) {
    out.notebook_scope = raw.notebook_scope.filter(
      (s): s is string => typeof s === 'string' && s.length > 0
    )
  }
  if (typeof raw.auto_reembed === 'boolean') out.auto_reembed = raw.auto_reembed
  if (
    typeof raw.hybrid_weight === 'number' &&
    raw.hybrid_weight >= 0 &&
    raw.hybrid_weight <= 1
  ) {
    out.hybrid_weight = raw.hybrid_weight
  }
  if (typeof raw.top_k === 'number' && raw.top_k >= 1 && raw.top_k <= 50) {
    out.top_k = Math.floor(raw.top_k)
  }
  if (typeof raw.min_score === 'number' && raw.min_score >= 0) {
    out.min_score = raw.min_score
  }
  if (
    typeof raw.max_context_chars === 'number' &&
    raw.max_context_chars >= 500
  ) {
    out.max_context_chars = Math.floor(raw.max_context_chars)
  }
  if (
    typeof raw.reindex_debounce_ms === 'number' &&
    raw.reindex_debounce_ms >= 0
  ) {
    out.reindex_debounce_ms = Math.floor(raw.reindex_debounce_ms)
  }
  return out
}
