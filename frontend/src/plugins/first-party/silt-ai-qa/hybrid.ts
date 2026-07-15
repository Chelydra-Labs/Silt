// Hybrid FTS5 + vector retrieval with weighted RRF (#225).
//
// The fusion primitives now live in the shared retrieval module (#597) so the
// AI agent and QA share one source of truth. This file re-exports them for the
// QA plugin's own modules + tests; no behavioural change.

export { fuseHybrid, trimToBudget } from '../../shared/retrieval/hybrid'
export type { RankedHit } from '../../shared/retrieval/hybrid'
