// Agent tool #602 — get_related_notes.
//
// Semantic "more like this": given a source block, embed its content and rank
// other blocks by cosine similarity. The agent does not maintain a vec0 index
// today (Phase 4 noted this), so the primary path is on-demand: gather a
// candidate pool (recent + FTS-recalled), embed the misses via ctx.ai.embed,
// and rank by cosine similarity computed in JS. Each computed vector is
// cached in plugin.db keyed by (block_id, content_hash) so subsequent runs
// reuse the work — a real vec0 + vec_distance_cosine index can replace the
// cache table later without changing this tool's contract.
//
// The shared ranking pipeline lives in ./_embedding; this file is the thin
// tool wrapper (param validation, source lookup, output formatting).

import type { PluginContext } from '../../../sdk'
import { asString } from '../../../../lib/asString'
import type { ToolResult } from '../tool-registry'
import { breadcrumb, clampInt } from './_util'
import { embedOne, gatherCandidates, rankCandidates } from './_embedding'

export const getRelatedNotesToolDef = {
  name: 'get_related_notes',
  description:
    'Find blocks semantically related to a source block. Embeds the source ' +
    'and ranks candidates by cosine similarity. Returns block_id, score, ' +
    'snippet, and location for each. The source block is excluded from ' +
    'results.',
  parameters: {
    type: 'object',
    required: ['block_id'],
    properties: {
      block_id: { type: 'string', description: 'Source block UUID.' },
      top_k: {
        type: 'integer',
        description: 'Max results to return (default 10).',
        minimum: 1,
        maximum: 50
      },
      min_score: {
        type: 'number',
        description:
          'Minimum cosine similarity in [0, 1] to include a result (default 0.5).',
        minimum: 0,
        maximum: 1
      }
    }
  }
}

export async function handleGetRelatedNotes(
  ctx: PluginContext,
  args: Record<string, unknown>
): Promise<ToolResult> {
  const sourceId = asString(args.block_id).trim()
  if (!sourceId) {
    return { content: '', error: 'block_id must not be empty' }
  }
  const topK = clampInt(args.top_k, 10, 1, 50)
  const minScore =
    typeof args.min_score === 'number' && Number.isFinite(args.min_score)
      ? Math.min(1, Math.max(0, args.min_score))
      : 0.5

  // 1. Read the source block's clean_content.
  const sourceRes = await fetchSourceContent(ctx, sourceId)
  if ('error' in sourceRes) {
    return { content: '', error: sourceRes.error }
  }
  const sourceText = sourceRes.text

  // 2. Embed the source as a retrieval query.
  const queryVec = await embedOne(ctx, sourceText, 'RETRIEVAL_QUERY')
  if (!queryVec || queryVec.length === 0) {
    return {
      content: '',
      error: 'embedding the source block failed (no vector returned)'
    }
  }

  // 3. Gather candidates (recent + FTS-recalled, source excluded).
  const candidates = await gatherCandidates(
    ctx,
    new Set([sourceId]),
    sourceText
  )
  if (candidates.length === 0) {
    return { content: 'No other blocks found to compare against.' }
  }

  // 4. Resolve vectors, score, filter, sort, take top_k.
  const top = await rankCandidates(ctx, queryVec, candidates, {
    minScore,
    topK
  })

  if (top.length === 0) {
    return {
      content: `No related blocks met the min_score=${minScore.toFixed(2)} threshold.`
    }
  }

  const lines = top.map((s, i) => {
    const snippet =
      s.block.clean_content.length > 200
        ? `${s.block.clean_content.slice(0, 200)}…`
        : s.block.clean_content
    return [
      `[${i + 1}] block ${s.block.id}`,
      `    score: ${s.score.toFixed(4)}`,
      `    location: ${breadcrumb(
        s.block.notebook,
        s.block.section,
        s.block.page
      )}`,
      `    ${snippet.replace(/\n/g, '\n    ')}`
    ].join('\n')
  })
  return {
    content: `${top.length} related block(s):\n\n${lines.join('\n\n')}`
  }
}

// --- helpers --------------------------------------------------------------

/** Fetch the source block's clean_content (single-row SELECT). */
async function fetchSourceContent(
  ctx: PluginContext,
  id: string
): Promise<{ error: string } | { text: string }> {
  const { rows } = await ctx.sqliteQuery(
    'SELECT clean_content FROM blocks WHERE id = ?',
    [id]
  )
  const row = rows[0]
  if (!row) {
    return { error: `block ${id} not found` }
  }
  const text = asString(row.clean_content).trim()
  if (!text) {
    return { error: `block ${id} has no content to compare` }
  }
  return { text }
}
