// Defaults + resolver for silt-ai-assistant settings (#233).

import type { ActionId, AssistantSettings } from './types'
import { ACTION_CATALOG } from './catalog'

const ALL_ACTION_IDS = ACTION_CATALOG.map((a) => a.id)

function defaultActionsEnabled(): Record<ActionId, boolean> {
  const out = {} as Record<ActionId, boolean>
  for (const id of ALL_ACTION_IDS) out[id] = true
  return out
}

export const DEFAULT_SETTINGS: AssistantSettings = {
  actions_enabled: defaultActionsEnabled(),
  existing_vocab_only: true,
  max_tag_suggestions: 8,
  max_input_chars: 12000,
  prompt_overrides: {},
  related_candidate_limit: 40,
  max_related_suggestions: 5
}

export function resolveSettings(
  raw: Record<string, unknown> | undefined | null
): AssistantSettings {
  const out: AssistantSettings = {
    ...DEFAULT_SETTINGS,
    actions_enabled: { ...DEFAULT_SETTINGS.actions_enabled },
    prompt_overrides: {}
  }
  if (!raw) return out

  if (raw.actions_enabled && typeof raw.actions_enabled === 'object') {
    const ae = raw.actions_enabled as Record<string, unknown>
    for (const id of ALL_ACTION_IDS) {
      if (typeof ae[id] === 'boolean') out.actions_enabled[id] = ae[id]
    }
  }
  if (typeof raw.existing_vocab_only === 'boolean') {
    out.existing_vocab_only = raw.existing_vocab_only
  }
  if (
    typeof raw.max_tag_suggestions === 'number' &&
    raw.max_tag_suggestions > 0
  ) {
    out.max_tag_suggestions = Math.min(50, Math.floor(raw.max_tag_suggestions))
  }
  if (typeof raw.max_input_chars === 'number' && raw.max_input_chars > 0) {
    out.max_input_chars = Math.floor(raw.max_input_chars)
  }
  if (raw.prompt_overrides && typeof raw.prompt_overrides === 'object') {
    const po = raw.prompt_overrides as Record<string, unknown>
    for (const id of ALL_ACTION_IDS) {
      if (typeof po[id] === 'string' && po[id].trim()) {
        out.prompt_overrides[id] = po[id]
      }
    }
  }
  if (
    typeof raw.related_candidate_limit === 'number' &&
    raw.related_candidate_limit > 0
  ) {
    out.related_candidate_limit = Math.min(
      200,
      Math.floor(raw.related_candidate_limit)
    )
  }
  if (
    typeof raw.max_related_suggestions === 'number' &&
    raw.max_related_suggestions > 0
  ) {
    out.max_related_suggestions = Math.min(
      20,
      Math.floor(raw.max_related_suggestions)
    )
  }
  return out
}
