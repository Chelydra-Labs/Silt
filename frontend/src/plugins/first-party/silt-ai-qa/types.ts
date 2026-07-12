// Types for silt-ai-qa (#224–#228).

export interface QASettings {
  /** Notebooks to index. Empty = all notebooks. */
  notebook_scope: string[]
  /** Re-embed on editor:save / block:changed when true. */
  auto_reembed: boolean
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

export interface RetrievedPassage {
  blockId: string
  notebook: string
  section: string
  page: string
  lineNumber: number
  text: string
  score: number
  /** 1-based citation marker used in the prompt ([1], [2], …). */
  citeIndex: number
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
