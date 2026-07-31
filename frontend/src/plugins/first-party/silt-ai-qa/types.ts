// Types for silt-ai-qa (#224–#228, Sprint 42).
//
// RetrievedPassage moved to the shared retrieval module (#597) and is
// re-exported here so existing QA imports (`from './types'`) keep working.

export type { RetrievedPassage } from '../../shared/retrieval/hybrid'

export interface QASettings {
  /** Notebooks to index. Empty = all notebooks. */
  notebook_scope: string[]
  /** Vector weight α in weighted RRF (0 = pure FTS, 1 = pure vector). */
  hybrid_weight: number
  /** Max fused passages returned to the RAG prompt. */
  top_k: number
  /** Drop fused hits below this RRF score (0 disables). */
  min_score: number
  /** Approx char budget for retrieved context in the prompt. */
  max_context_chars: number
  /** Debounce for incremental re-index after save (ms). */
  reindex_debounce_ms: number
  /** Human-readable reason the search index is stale; null when fresh. */
  stale_reason: string | null
  /** When true, re-score fused candidates by query–passage cosine similarity. */
  rerank_enabled: boolean
}

export interface ChunkRecord {
  chunkId: string
  blockId: string
  notebook: string
  section: string
  page: string
  lineNumber: number
  text: string
  contentHash: string
}

export interface Citation {
  index: number
  blockId: string
  notebook: string
  section: string
  page: string
  lineNumber: number
  snippet: string
}

export interface QAMessage {
  role: 'user' | 'assistant'
  content: string
  citations?: Citation[]
}

export type IndexStatus =
  'idle' | 'indexing' | 'ready' | 'error' | 'unconfigured'

export interface IndexProgress {
  status: IndexStatus
  done: number
  total: number
  message?: string
  model?: string
  dimensions?: number
  chunkCount?: number
  lastError?: string
}
