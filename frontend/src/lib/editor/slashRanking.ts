// Deterministic ranking for the slash command palette (#585).
//
// The previous inline scorer produced surprising results: every label/id prefix
// match tied at the same score and was broken by registry order (alphabetical),
// so `/b` highlighted "Background color" ahead of "Bold"; and `desc.includes(q)`
// pulled in any description containing the substring, so short or stopword-like
// queries (`the`, `insert`) returned large, undifferentiated lists.
//
// This module replaces that with an explicit, tested priority ladder and a
// deterministic tiebreak, independent of registry insertion order.

import type { SlashCommand } from './slash-registry'

// Stopwords that never qualify a description-substring match. Without this,
// `the` matches every "…the selection/…" description and swamps the list.
const STOPWORDS = new Set([
  'the',
  'a',
  'an',
  'of',
  'for',
  'and',
  'or',
  'to',
  'in',
  'on',
  'at',
  'by',
  'with',
  'from',
  'into',
  'your'
])

// Curated priority for the inline-formatting commands so an ambiguous short
// prefix resolves to the most common intent — `/b` → Bold, not Background
// color. Higher wins among otherwise-equal matches. Only the commands where
// ambiguity actually matters need an entry; everything else falls through to
// the label-length / index tiebreaks.
const PRIORITY: Record<string, number> = {
  bold: 100,
  italic: 90,
  underline: 80,
  strike: 70,
  code: 60,
  highlight: 50
}

// Description matching is gated behind this minimum length so one- and
// two-character queries stay label/id-only.
const MIN_DESC_QUERY = 3

// Match tiers, highest first. A command lands in the first tier it satisfies.
const TIER_EXACT = 100
const TIER_PREFIX = 90
const TIER_WORD_BOUNDARY = 80
const TIER_SUBSTRING = 70
const TIER_DESCRIPTION = 20

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

// True when some whitespace/punctuation-delimited word in `haystack` starts
// with `q` (e.g. q='color' → 'Background color'). Catches compound labels the
// prefix tier misses.
function wordBoundaryMatch(haystack: string, q: string): boolean {
  if (!q) return false
  return new RegExp(`\\b${escapeRegExp(q)}`, 'i').test(haystack)
}

function tierOf(cmd: SlashCommand, q: string): number {
  const label = cmd.label.toLowerCase()
  const id = cmd.id.toLowerCase()
  if (label === q || id === q) return TIER_EXACT
  if (label.startsWith(q) || id.startsWith(q)) return TIER_PREFIX
  if (wordBoundaryMatch(label, q) || wordBoundaryMatch(id, q))
    return TIER_WORD_BOUNDARY
  if (label.includes(q) || id.includes(q)) return TIER_SUBSTRING
  if (
    q.length >= MIN_DESC_QUERY &&
    !STOPWORDS.has(q) &&
    cmd.description &&
    cmd.description.toLowerCase().includes(q)
  ) {
    return TIER_DESCRIPTION
  }
  return -1
}

/**
 * Rank commands for a slash-palette query. Pure and deterministic: the output
 * order depends only on the inputs, never on registry insertion order. An empty
 * query returns the commands unchanged (the palette's grouped display order).
 */
export function rankSlashCommands(
  commands: SlashCommand[],
  query: string
): SlashCommand[] {
  const q = query.toLowerCase().trim()
  if (!q) return commands

  const scored = commands
    .map((cmd, index) => ({ cmd, tier: tierOf(cmd, q), index }))
    .filter((item) => item.tier >= 0)

  scored.sort((a, b) => {
    if (b.tier !== a.tier) return b.tier - a.tier
    const pa = PRIORITY[a.cmd.id] ?? 0
    const pb = PRIORITY[b.cmd.id] ?? 0
    if (pb !== pa) return pb - pa
    // Shorter label first — for a short prefix a shorter label is the more
    // specific match (Bold before Background color even without the curated
    // map, as a belt-and-braces fallback).
    const lenDiff = a.cmd.label.length - b.cmd.label.length
    if (lenDiff !== 0) return lenDiff
    return a.index - b.index
  })

  return scored.map((item) => item.cmd)
}
