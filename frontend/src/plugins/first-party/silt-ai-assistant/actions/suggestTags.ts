// Suggest tags from existing vocabulary + optional new hierarchical paths (#232).

import type { PluginAIChatMessage, PluginContext } from '../../../sdk'
import { systemPromptFor } from '../prompts/defaults'
import { createProposal } from '../proposal/model'
import { parseJsonObject } from '../text'
import type {
  AssistantSettings,
  Proposal,
  ScopeContext,
  TagSuggestion
} from '../types'
import { completeBuffered } from './runChat'

export async function loadTagVocabulary(
  ctx: PluginContext,
  limit = 500
): Promise<string[]> {
  const { rows } = await ctx.sqliteQuery(
    `SELECT DISTINCT raw_path AS tag
       FROM tags
      WHERE raw_path IS NOT NULL AND raw_path != ''
      ORDER BY raw_path
      LIMIT ?`,
    [limit]
  )
  return rows
    .map((r) => (typeof r.tag === 'string' ? r.tag : ''))
    .filter(Boolean)
}

/** Prefer completing existing hierarchical paths over new top-level tags. */
export function filterTagSuggestions(
  proposed: string[],
  vocab: string[],
  settings: AssistantSettings
): TagSuggestion[] {
  const vocabSet = new Set(vocab.map((v) => v.replace(/^#/, '').toLowerCase()))
  const out: TagSuggestion[] = []
  const seen = new Set<string>()

  for (const raw of proposed) {
    const tag = raw.replace(/^#/, '').trim().replace(/\s+/g, '-')
    if (!tag) continue
    const key = tag.toLowerCase()
    if (seen.has(key)) continue

    const exact = vocabSet.has(key)
    // When existing-only: only accept exact vocabulary matches (strict).
    if (settings.existing_vocab_only && !exact) continue

    seen.add(key)
    out.push({ tag, existing: exact })
    if (out.length >= settings.max_tag_suggestions) break
  }

  // Prefer existing tags first, then hierarchical depth.
  out.sort((a, b) => {
    if (a.existing !== b.existing) return a.existing ? -1 : 1
    const da = a.tag.split('/').length
    const db = b.tag.split('/').length
    return db - da
  })
  return out.slice(0, settings.max_tag_suggestions)
}

export function buildSuggestTagsMessages(
  input: string,
  vocab: string[],
  settings: AssistantSettings
): PluginAIChatMessage[] {
  const sample = vocab.slice(0, 200).join(', ')
  const system = systemPromptFor(
    'suggest-tags',
    settings.prompt_overrides['suggest-tags']
  )
  return [
    { role: 'system', content: system },
    {
      role: 'user',
      content: `Existing tag vocabulary (prefer these):\n${sample || '(empty)'}\n\nNote:\n${input}`
    }
  ]
}

export function parseSuggestedTags(raw: string): string[] {
  const obj = parseJsonObject(raw)
  if (!obj) return []
  const tags = obj.tags
  if (!Array.isArray(tags)) return []
  return tags
    .filter((t): t is string => typeof t === 'string')
    .map((t) => t.trim())
    .filter(Boolean)
}

export async function runSuggestTags(
  ctx: PluginContext,
  scope: ScopeContext,
  settings: AssistantSettings
): Promise<Proposal> {
  const vocab = await loadTagVocabulary(ctx)
  const messages = buildSuggestTagsMessages(scope.inputText, vocab, settings)
  let raw = (await completeBuffered(ctx, messages, 400)).content
  let proposed = parseSuggestedTags(raw)
  if (proposed.length === 0 && raw.trim()) {
    raw = (
      await completeBuffered(
        ctx,
        [
          ...messages,
          { role: 'assistant', content: raw },
          {
            role: 'user',
            content: 'Return ONLY JSON: {"tags":["path/here"]}'
          }
        ],
        400
      )
    ).content
    proposed = parseSuggestedTags(raw)
  }

  const tags = filterTagSuggestions(proposed, vocab, settings)
  const md = tags.map((t) => `#${t.tag}`).join(' ')
  return createProposal({
    actionId: 'suggest-tags',
    kind: 'apply-tags',
    scope,
    proposedMarkdown: md,
    tags,
    warning:
      tags.length === 0
        ? settings.existing_vocab_only
          ? 'No matching tags in your existing vocabulary.'
          : 'No tags suggested.'
        : scope.truncated
          ? `Input truncated to ${settings.max_input_chars} characters.`
          : undefined,
    status: 'ready'
  })
}
