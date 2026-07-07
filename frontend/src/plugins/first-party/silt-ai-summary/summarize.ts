// Orchestrator for silt-ai-summary (#220, #222).
//
// Pure given a {@link SummarizeDeps} + a PluginContext: no event subscriptions,
// no timers, no DOM. The reactive shell (state.svelte.ts) owns debouncing
// saves, switching notes, and calling this; that separation keeps the
// cache→extract→diff→cache flow cleanly unit-testable with the LLM + pluginDb
// mocked.
//
// Flow:
//   1. Unconfigured gate — return 'unconfigured' WITHOUT calling ctx.ai.complete
//      (no network until configured, per #220).
//   2. Cache check — exact (page_id, content_hash) hit with a matching model
//      serves instantly, re-deriving the newItems diff from the stored
//      prior_snapshot.
//   3. Miss / model-change / Regenerate — extract (with the three-tier
//      fallback), diff against the page's latest prior extraction, and store.
//
// Errors are always returned in the outcome — never thrown — so the banner
// renders a non-blocking inline state instead of propagating a rejection.

import type { PluginContext } from '../../sdk'
import type { SummarizeDeps, SummaryOutcome, SummaryResult } from './types'
import {
  computeContentHash,
  getCachedSummary,
  latestSummaryForPage,
  migrateCache,
  putCachedSummary
} from './cache'
import { extractSummary } from './extract'
import { EMPTY_EXTRACTION, diffFacets } from './diff'

/** RFC3339 timestamp for "now". Factored so tests can inject a clock if needed. */
function nowIso(): string {
  return new Date().toISOString()
}

/** Run one summary resolution for a page. See module doc for the flow. */
export async function summarize(ctx: PluginContext, deps: SummarizeDeps): Promise<SummaryOutcome> {
  // No provider configured: surface the setup nudge without any network call.
  // The banner (Phase 3) renders a "Configure AI provider" link for this code.
  if (!deps.isConfigured) {
    return {
      ok: false,
      error: {
        code: 'unconfigured',
        message: 'No AI provider is configured. Add a chat model in Settings → AI Provider to generate summaries.'
      }
    }
  }

  // An empty note has nothing to summarize; serve a muted empty result so the
  // banner can render "Nothing to highlight" without an LLM call.
  if (!deps.cleanContent.trim()) {
    return emptyResult()
  }
  // Oversized notes are SKIPPED (not truncated/chunked — v1). A head+tail
  // slice would produce a partial, potentially misleading summary; chunking is
  // a documented future enhancement. The settings page advertises this as
  // "skipped" and the banner renders the 'oversized' code as an inline state
  // pointing the user at Max note size. extract.ts still caps the prompt via
  // truncateForPrompt as a defense-in-depth context-window guard for any
  // caller that bypasses this gate.
  if (deps.cleanContent.length > deps.settings.max_note_chars) {
    return {
      ok: false,
      error: {
        code: 'oversized',
        message: `This note is ${deps.cleanContent.length} characters; the limit is ${deps.settings.max_note_chars}. Raise "Max note size" in Settings → AI Summary or split the note.`
      }
    }
  }

  // Cache failures (a corrupt plugin.db, a locked connection, the migration
  // not having run) must NOT crash summarization — the cache is disposable
  // working memory. Each call degrades gracefully: a migration/read failure
  // is treated as a miss (proceed to extraction); a write failure is logged
  // and the result still returns. A diagnostics warn surfaces the cause.
  await safeCache(() => migrateCache(ctx), 'migrate')
  const hash = await computeContentHash(deps.cleanContent)

  // Cache hit (exact hash match + same model + not a forced Regenerate).
  if (!deps.force) {
    const cached = await safeCache(
      () => getCachedSummary(ctx, deps.pageId, hash),
      'read'
    )
    if (cached && cached.model === deps.configuredModel && cached.summary_length === deps.settings.summary_length) {
      const result: SummaryResult = {
        summary: cached.summary,
        tasks: cached.tasks,
        risks: cached.risks,
        decisions: cached.decisions,
        newItems: diffFacets(toExtraction(cached), cached.prior_snapshot),
        fromCache: true,
        model: cached.model,
        generatedAt: cached.generated_at
      }
      return { ok: true, result }
    }
  }

  // Miss / invalidate / regenerate. The prior snapshot is the page's latest
  // extraction (any hash) so the newItems diff stays stable across content
  // edits AND model switches (the diff is content-relative, not model-relative).
  const latest = await safeCache(() => latestSummaryForPage(ctx, deps.pageId), 'read')
  const prior = latest ? toExtraction(latest) : EMPTY_EXTRACTION

  const extracted = await extractSummary({
    complete: (messages, maxTokens) =>
      ctx.ai.complete({ messages, temperature: 0, maxTokens }),
    content: deps.cleanContent,
    settings: deps.settings
  })
  if (!extracted.ok) {
    return { ok: false, error: extracted.error }
  }

  const generatedAt = nowIso()
  await safeCache(
    () =>
      putCachedSummary(ctx, {
        page_id: deps.pageId,
        content_hash: hash,
        summary: extracted.extraction.summary,
        tasks: extracted.extraction.tasks,
        risks: extracted.extraction.risks,
        decisions: extracted.extraction.decisions,
        prior_snapshot: prior,
        model: extracted.model,
        summary_length: deps.settings.summary_length,
        generated_at: generatedAt
      }),
    'write'
  )

  return {
    ok: true,
    result: {
      summary: extracted.extraction.summary,
      tasks: extracted.extraction.tasks,
      risks: extracted.extraction.risks,
      decisions: extracted.extraction.decisions,
      newItems: diffFacets(extracted.extraction, prior),
      fromCache: false,
      model: extracted.model,
      generatedAt
    }
  }
}

function toExtraction(row: {
  summary: string
  tasks: string[]
  risks: string[]
  decisions: string[]
}) {
  return { summary: row.summary, tasks: row.tasks, risks: row.risks, decisions: row.decisions }
}

/** Run a cache operation, returning null on failure (read/migrate) or void
 *  (write). The cache is disposable working memory: a corrupt plugin.db or a
 *  stale connection must not crash summarization — degrade to a miss and let
 *  extraction proceed. The warn surfaces the cause for diagnostics. */
async function safeCache<T>(fn: () => Promise<T>, op: string): Promise<T | null> {
  try {
    return await fn()
  } catch (e) {
    // eslint-disable-next-line no-console
    console.warn(`[silt-ai-summary] cache ${op} failed (degrading — cache is disposable):`, e)
    return null
  }
}

function emptyResult(): SummaryOutcome {
  return {
    ok: true,
    result: {
      summary: '',
      tasks: [],
      risks: [],
      decisions: [],
      newItems: { tasks: [], risks: [], decisions: [] },
      fromCache: true,
      model: '',
      generatedAt: nowIso()
    }
  }
}
