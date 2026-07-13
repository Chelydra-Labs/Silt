// Action catalog for Writing Assistant (#229 spike).

import type { ActionId, ActionMeta, AssistantSettings } from './types'

export const ACTION_CATALOG: ActionMeta[] = [
  {
    id: 'draft-expand',
    label: 'Draft / Expand',
    description: 'Turn a short description into an outline or draft.',
    slashLabel: 'Draft / Expand',
    slashDescription: 'Draft or expand from selection or description',
    icon: 'edit_note',
    needsChat: true,
    needsEmbed: false,
    acceptsInstruction: true,
    prefersSelection: false
  },
  {
    id: 'rewrite-succinct',
    label: 'Rewrite succinct',
    description: 'Condense verbose text into note-friendly bullets.',
    slashLabel: 'Rewrite succinct',
    slashDescription: 'Condense selection into succinct notes',
    icon: 'compress',
    needsChat: true,
    needsEmbed: false,
    acceptsInstruction: false,
    prefersSelection: true
  },
  {
    id: 'improve-clarity',
    label: 'Improve clarity',
    description: 'Improve grammar and readability without changing meaning.',
    slashLabel: 'Improve clarity',
    slashDescription: 'Clarify selection while preserving meaning',
    icon: 'spellcheck',
    needsChat: true,
    needsEmbed: false,
    acceptsInstruction: false,
    prefersSelection: true
  },
  {
    id: 'extract-tasks',
    label: 'Extract action items',
    description: 'Propose GFM tasks from commitments in the note.',
    slashLabel: 'Extract tasks',
    slashDescription: 'Extract action items as proposed tasks',
    icon: 'checklist',
    needsChat: true,
    needsEmbed: false,
    acceptsInstruction: false,
    prefersSelection: false
  },
  {
    id: 'suggest-tags',
    label: 'Suggest tags',
    description: 'Propose tags from your existing vocabulary.',
    slashLabel: 'Suggest tags',
    slashDescription: 'Suggest tags for this note',
    icon: 'sell',
    needsChat: true,
    needsEmbed: false,
    acceptsInstruction: false,
    prefersSelection: false
  },
  {
    id: 'suggest-related',
    label: 'Suggest related notes',
    description: 'Find semantically related blocks and propose links.',
    slashLabel: 'Suggest related',
    slashDescription: 'Suggest related notes via embeddings',
    icon: 'hub',
    needsChat: false,
    needsEmbed: true,
    acceptsInstruction: false,
    prefersSelection: false
  }
]

export function actionById(id: ActionId): ActionMeta | undefined {
  return ACTION_CATALOG.find((a) => a.id === id)
}

/** Actions enabled in settings (defaults true when key missing). */
export function enabledActions(settings: AssistantSettings): ActionMeta[] {
  return ACTION_CATALOG.filter((a) => settings.actions_enabled[a.id] !== false)
}

export function isActionEnabled(
  settings: AssistantSettings,
  id: ActionId
): boolean {
  return settings.actions_enabled[id] !== false
}
