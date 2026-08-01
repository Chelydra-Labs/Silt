// Default settings + resolver for silt-ai-qa (#228, #614, #617, #620).

import type { QASettings } from './types'

export const DEFAULT_SETTINGS: QASettings = {
  notebook_scope: [],
  hybrid_weight: 0.6,
  top_k: 10,
  min_score: 0,
  max_context_chars: 24000,
  reindex_debounce_ms: 2000,
  stale_reason: null,
  rerank_enabled: false
}

export function resolveSettings(
  raw: Record<string, unknown> | undefined | null
): QASettings {
  if (!raw) return { ...DEFAULT_SETTINGS }
  const out: QASettings = { ...DEFAULT_SETTINGS }
  // Legacy auto_reembed is ignored — indexing is always on (#850).
  if (Array.isArray(raw.notebook_scope)) {
    out.notebook_scope = raw.notebook_scope.filter(
      (s): s is string => typeof s === 'string' && s.length > 0
    )
  }
  if (
    typeof raw.hybrid_weight === 'number' &&
    raw.hybrid_weight >= 0 &&
    raw.hybrid_weight <= 1
  ) {
    out.hybrid_weight = raw.hybrid_weight
  }
  if (typeof raw.top_k === 'number' && raw.top_k >= 1 && raw.top_k <= 100) {
    out.top_k = Math.floor(raw.top_k)
  }
  if (typeof raw.min_score === 'number' && raw.min_score >= 0) {
    out.min_score = raw.min_score
  }
  if (
    typeof raw.max_context_chars === 'number' &&
    raw.max_context_chars >= 1000
  ) {
    out.max_context_chars = Math.floor(raw.max_context_chars)
  }
  if (
    typeof raw.reindex_debounce_ms === 'number' &&
    raw.reindex_debounce_ms >= 0
  ) {
    out.reindex_debounce_ms = Math.floor(raw.reindex_debounce_ms)
  }
  if (raw.stale_reason === null) {
    out.stale_reason = null
  } else if (typeof raw.stale_reason === 'string') {
    out.stale_reason = raw.stale_reason.length > 0 ? raw.stale_reason : null
  }
  if (typeof raw.rerank_enabled === 'boolean') {
    out.rerank_enabled = raw.rerank_enabled
  }
  return out
}
