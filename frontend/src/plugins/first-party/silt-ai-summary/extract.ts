// Extraction core for silt-ai-summary (#220): prompt building, tolerant JSON
// parsing, and the LLM call with a three-tier fallback.
//
// Design decision D1 (PLAN.md): prompt-only JSON + tolerant parsing + a
// summary-only fallback, rather than passing response_format/json_schema
// through the AI service. Prompt-only works universally across OpenAI-
// compatible servers (Ollama's response_format json_schema has a known
// silent-ignore bug), and the fallback chain guarantees a usable result even
// when a weak model emits prose-wrapped or partial JSON. The AI service
// surface stays unchanged; a future enhancement can add optional
// response_format pass-through without disturbing this plugin.
//
// All functions here are pure given their inputs (extractSummary takes a ctx
// only to call ctx.ai.complete) so they unit-test cleanly with the LLM mocked.

import type { PluginAIChatMessage, PluginAIError } from '../../sdk'
import type { SummaryError, SummaryExtraction, SummarySettings } from './types'
import { maxTokensForLength } from './settings'

/** The JSON Schema for the structured summary extraction. Passed to native
 *  providers (Google, Anthropic) via responseSchema so the model returns a
 *  JSON object conforming to this shape directly — no prompt-only heuristics,
 *  no markdown fences. The OpenAI-compatible path ignores the schema and
 *  falls back to prompt-only JSON + tolerant parsing (D1). */
const SUMMARY_SCHEMA = {
  type: 'object',
  properties: {
    summary: { type: 'string', description: 'A concise summary of the note' },
    tasks: {
      type: 'array',
      items: { type: 'string' },
      description: 'Actionable to-dos the note mentions'
    },
    risks: {
      type: 'array',
      items: { type: 'string' },
      description: 'Risks, blockers, or concerns raised'
    },
    decisions: {
      type: 'array',
      items: { type: 'string' },
      description: 'Decisions made or confirmed'
    }
  },
  required: ['summary', 'tasks', 'risks', 'decisions']
} as const

/** Mid-truncate a note that exceeds the size budget: keep a head and a tail
 *  with a visible ellipsis seam, preserving the opening (context) and the
 *  closing (actions/decisions often live at the end of meeting notes). Notes
 *  at or under the limit pass through untouched. */
export function truncateForPrompt(content: string, maxChars: number): string {
  if (content.length <= maxChars) return content
  // Reserve 1/4 for the tail so the closing decisions aren't lost.
  const tail = Math.floor(maxChars / 4)
  const head = maxChars - tail - 1 // -1 for the ellipsis marker
  return content.slice(0, head) + '…' + content.slice(content.length - tail)
}

/** Build the structured-extraction prompt. The schema is described as TEXT
 *  (not a JSON-schema object) because prompt-only JSON is the universal
 *  denominator across OpenAI-compatible servers (D1). */
export function buildSummaryPrompt(
  content: string,
  settings: SummarySettings
): PluginAIChatMessage[] {
  const lengthHint =
    settings.summary_length === 'short'
      ? '2 concise sentences'
      : settings.summary_length === 'long'
        ? '3–4 sentences'
        : '2–3 sentences'
  const system = `You are a note-summarization assistant. Read the user's note and return a JSON object capturing what matters.

Return ONLY a single JSON object — no markdown fences, no commentary before or after. The object MUST have exactly these keys:
{
  "summary": string,            // ${lengthHint} summarizing the note's substance
  "tasks": string[],            // actionable to-dos the note mentions (empty array if none)
  "risks": string[],            // risks, blockers, or concerns raised (empty array if none)
  "decisions": string[]         // decisions made or confirmed (empty array if none)
}

Rules:
- Every array MUST be present, even when empty (use []).
- Each array item is a short, self-contained phrase (not a paragraph).
- If the note has no tasks/risks/decisions, return [] for that key — never omit it.
- Quote the note's own wording where possible; do not invent content not in the note.
- Output valid JSON only.`

  return [
    { role: 'system', content: system },
    {
      role: 'user',
      content: truncateForPrompt(content, settings.max_note_chars)
    }
  ]
}

/** The prose-only fallback prompt, used when structured extraction fails twice.
 *  Asks only for a summary so the banner still has something useful to show. */
function buildProseSummaryPrompt(
  content: string,
  settings: SummarySettings
): PluginAIChatMessage[] {
  const lengthHint =
    settings.summary_length === 'short'
      ? '2 concise sentences'
      : settings.summary_length === 'long'
        ? '3–4 sentences'
        : '2–3 sentences'
  return [
    {
      role: 'system',
      content: `Summarize the user's note in ${lengthHint}, in plain prose. Return only the summary text — no JSON, no preamble.`
    },
    {
      role: 'user',
      content: truncateForPrompt(content, settings.max_note_chars)
    }
  ]
}

/** Strip ```json / ``` fences and surrounding prose, then locate the first
 *  balanced `{`…`}` object. Returns the substring (or '' if no balanced
 *  object is found) so JSON.parse can attempt it. */
export function extractJsonObject(raw: string): string {
  let s = String(raw ?? '').trim()
  // Strip a leading ``` fence (with optional language tag) + trailing fence.
  if (s.startsWith('```')) {
    s = s.replace(/^```[a-zA-Z0-9]*\s*\n?/, '').replace(/\n?```\s*$/, '')
  }
  const start = s.indexOf('{')
  if (start < 0) return ''
  // Scan for the matching close, respecting nested braces + string literals so
  // a `}` inside a string value doesn't fool the brace counter.
  let depth = 0
  let inStr = false
  let esc = false
  for (let i = start; i < s.length; i++) {
    const c = s[i]
    if (inStr) {
      if (esc) esc = false
      else if (c === '\\') esc = true
      else if (c === '"') inStr = false
      continue
    }
    if (c === '"') inStr = true
    else if (c === '{') depth++
    else if (c === '}') {
      depth--
      if (depth === 0) return s.slice(start, i + 1)
    }
  }
  return ''
}

/** Coerce a parsed value into a string[] per the schema: a bare string becomes
 *  a single-element array; non-strings are dropped; empties are dropped. */
function coerceStringArray(v: unknown): string[] {
  if (typeof v === 'string') return v.trim() ? [v.trim()] : []
  if (!Array.isArray(v)) return []
  return v
    .map((x) => (typeof x === 'string' ? x.trim() : ''))
    .filter((x) => x.length > 0)
}

/** Tolerantly parse the model output into a SummaryExtraction, or return null
 *  when no usable object can be recovered. Degrades gracefully: if facets fail
 *  validation but a summary is present, the facets default to empty (the
 *  banner still shows the summary). Returns null only when there is no
 *  summary at all. */
export function parseSummary(raw: string): SummaryExtraction | null {
  const jsonText = extractJsonObject(raw)
  if (!jsonText) return null
  let obj: unknown
  try {
    obj = JSON.parse(jsonText)
  } catch {
    return null
  }
  if (!obj || typeof obj !== 'object') return null
  const o = obj as Record<string, unknown>
  const summary = typeof o.summary === 'string' ? o.summary.trim() : ''
  if (!summary) return null
  return {
    summary,
    tasks: coerceStringArray(o.tasks),
    risks: coerceStringArray(o.risks),
    decisions: coerceStringArray(o.decisions)
  }
}

/** Map a normalized AI rejection (PluginAIError) to a SummaryError the banner
 *  can branch on. Every provider-side failure collapses to 'provider-error'
 *  (the banner offers Retry); the 'unconfigured' code is reserved for the
 *  orchestrator's pre-call gate. */
export function providerError(e: unknown): SummaryError {
  const err = e as Partial<PluginAIError> & { message?: string }
  const message = err?.message || 'The AI provider request failed.'
  return { code: 'provider-error', message }
}

/** The result of an extraction attempt: either a usable extraction (with the
 *  model id that produced it) or a typed error. Never throws — the orchestrator
 *  relies on this to render non-blocking inline states. */
export type ExtractResult =
  | { ok: true; extraction: SummaryExtraction; model: string }
  | { ok: false; error: SummaryError }

/** Run the structured extraction with a three-tier fallback:
 *  1. Ask for JSON, parse it.
 *  2. On parse failure, retry once (same prompt — transient malformed output).
 *  3. On a second parse failure, re-prompt for PROSE ONLY and wrap the result
 *     as an empty-facets extraction so the banner still has a summary.
 *  A provider rejection at any tier short-circuits to a typed error. */
export async function extractSummary(args: {
  complete: (
    messages: PluginAIChatMessage[],
    maxTokens: number,
    options?: { responseSchema?: Record<string, unknown> }
  ) => Promise<{ content: string; model: string }>
  content: string
  settings: SummarySettings
}): Promise<ExtractResult> {
  const { complete, content, settings } = args
  const messages = buildSummaryPrompt(content, settings)
  const maxTokens = maxTokensForLength(settings.summary_length)

  let res: { content: string; model: string }
  try {
    // Pass the summary schema so native providers (Google, Anthropic) enforce
    // structured output. OpenAI-compat ignores it (D1 prompt-only fallback).
    res = await complete(messages, maxTokens, { responseSchema: SUMMARY_SCHEMA })
  } catch (e) {
    return { ok: false, error: providerError(e) }
  }
  let parsed = parseSummary(res.content)
  if (!parsed) {
    // Tier 2: one retry with the same structured prompt.
    try {
      res = await complete(messages, maxTokens, { responseSchema: SUMMARY_SCHEMA })
      parsed = parseSummary(res.content)
    } catch (e) {
      return { ok: false, error: providerError(e) }
    }
  }
  if (parsed) {
    return { ok: true, extraction: parsed, model: res.model }
  }
  // Tier 3: summary-only prose fallback.
  try {
    const prose = await complete(
      buildProseSummaryPrompt(content, settings),
      maxTokens
    )
    const summary = prose.content.trim()
    if (!summary) {
      return {
        ok: false,
        error: {
          code: 'provider-error',
          message: 'The model returned an empty summary.'
        }
      }
    }
    return {
      ok: true,
      extraction: { summary, tasks: [], risks: [], decisions: [] },
      model: prose.model
    }
  } catch (e) {
    return { ok: false, error: providerError(e) }
  }
}
