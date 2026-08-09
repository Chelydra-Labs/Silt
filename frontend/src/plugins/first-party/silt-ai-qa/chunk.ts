// Block-aware chunking + content hash for silt-ai-qa (#224).
// Re-exports shared helpers.

export {
  computeContentHash,
  chunksFromBlocks,
  hashesEqual,
  type BlockInput,
  type ChunkRecord
} from '../../shared/retrieval/chunk'
