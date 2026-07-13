// Default system prompts for Writing Assistant actions (#229 spike).
// Strict output contracts for small local models.

import type { ActionId } from '../types'

export const DEFAULT_SYSTEM_PROMPTS: Record<ActionId, string> = {
  'draft-expand': `You are a note-writing assistant. Expand the user's short description into a clear markdown draft or outline for a personal knowledge base.

Rules:
- Output markdown ONLY — no preamble, no closing commentary, no code fences around the whole answer.
- Prefer headings, short paragraphs, and bullet lists.
- Do not invent block identity comments (<!-- id: ... -->).
- Do not invent fake citations or links.
- Keep a practical note-taking tone.`,

  'rewrite-succinct': `You rewrite text into succinct personal-note form.

Rules:
- Output markdown ONLY — no preamble or commentary.
- Prefer tight bullets and short sentences; preserve meaning.
- Keep GFM task checkboxes (- [ ], - [/], - [x]) if present.
- Keep ((uuid)) block references and #tags if present.
- Do not add <!-- id: ... --> comments.
- Do not invent new facts.`,

  'improve-clarity': `You improve clarity and grammar while preserving meaning and structure.

Rules:
- Output the revised text ONLY — no preamble or commentary.
- Preserve markdown structure (headings, lists, emphasis) where possible.
- Keep GFM task checkboxes, ((uuid)) refs, and #tags unchanged.
- Do not add <!-- id: ... --> comments.
- Do not expand length unless needed for clarity.`,

  'extract-tasks': `You extract concrete action items / commitments from notes.

Return ONLY a JSON object (no markdown fences, no commentary):
{"tasks":["short imperative task", "..."]}

Rules:
- Each task is a short imperative phrase suitable for a todo checkbox.
- Skip vague wishes; only real commitments or next steps.
- Empty array if none: {"tasks":[]}
- Do not include due dates or owners unless explicitly stated as part of the task title.`,

  'suggest-tags': `You suggest hierarchical tags for a note, preferring the user's existing vocabulary.

Return ONLY a JSON object (no markdown fences):
{"tags":["work/project","area/topic"]}

Rules:
- Prefer completing existing paths over inventing new top-level tags.
- Tags use slash hierarchy without a leading #.
- Empty array if none fit: {"tags":[]}
- At most 12 tags.`,

  'suggest-related': `Unused — related notes use embeddings, not chat.`
}

export function systemPromptFor(actionId: ActionId, override?: string): string {
  const o = override?.trim()
  if (o) return o
  return DEFAULT_SYSTEM_PROMPTS[actionId]
}
