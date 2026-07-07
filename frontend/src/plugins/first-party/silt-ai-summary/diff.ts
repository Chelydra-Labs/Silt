// "New items" diff for silt-ai-summary (#220).
//
// An item is "new" when it appears in the current extraction but not in the
// prior snapshot. Matching is normalized (case-insensitive, trimmed,
// whitespace-collapsed) so the model re-phrasing "Ship the API" as
// "ship the api" doesn't double-flag it; the ORIGINAL wording is returned so
// the banner shows what the model actually said. Exact normalized match is the
// v1 strategy — trigram fuzzy matching is a documented future refinement if
// re-flagging near-duplicates becomes noisy.

import type { FacetDiff, SummaryExtraction } from './types'

/** Normalize a facet item for matching: lowercase, trim, collapse internal
 *  whitespace runs to a single space. */
export function normalizeItem(s: string): string {
  return String(s ?? '')
    .toLowerCase()
    .trim()
    .replace(/\s+/g, ' ')
}

/** Items in `current` whose normalized form is absent from `prior`. Returns
 *  the ORIGINAL (un-normalized) strings, deduplicated by normalized key (first
 *  occurrence wins) so a model emitting a duplicate doesn't show it twice. */
export function newItems(current: string[], prior: string[]): string[] {
  const priorSet = new Set(prior.map(normalizeItem))
  const seen = new Set<string>()
  const out: string[] = []
  for (const item of current) {
    const key = normalizeItem(item)
    if (!key) continue // skip empties
    if (priorSet.has(key)) continue
    if (seen.has(key)) continue
    seen.add(key)
    out.push(item)
  }
  return out
}

/** Diff every facet at once. The orchestrator threads this against the prior
 *  snapshot stored alongside the cache row (#222). */
export function diffFacets(
  current: SummaryExtraction,
  prior: SummaryExtraction
): FacetDiff {
  return {
    tasks: newItems(current.tasks, prior.tasks),
    risks: newItems(current.risks, prior.risks),
    decisions: newItems(current.decisions, prior.decisions)
  }
}

export const EMPTY_EXTRACTION: SummaryExtraction = {
  summary: '',
  tasks: [],
  risks: [],
  decisions: []
}
