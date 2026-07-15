// Embedding model capability registry (#619).
// Maps model name patterns to Matryoshka truncation support so Index Density
// can show presets, a fixed badge, or a caveat for unknown models.

export interface EmbeddingCapabilities {
  /** true = truncatable; false = fixed; undefined = unknown. */
  supportsTruncation?: boolean
  nativeDimensions?: number
  recommendedPresets?: number[]
  notes?: string
}

interface RegistryEntry {
  pattern: RegExp
  caps: EmbeddingCapabilities
}

const REGISTRY: RegistryEntry[] = [
  {
    pattern: /^gemini-embedding/i,
    caps: {
      supportsTruncation: true,
      nativeDimensions: 3072,
      recommendedPresets: [768, 1024],
      notes: 'Matryoshka Representation Learning'
    }
  },
  {
    pattern: /^text-embedding-3-small$/i,
    caps: {
      supportsTruncation: true,
      nativeDimensions: 1536,
      recommendedPresets: [512, 768]
    }
  },
  {
    pattern: /^text-embedding-3-large$/i,
    caps: {
      supportsTruncation: true,
      nativeDimensions: 3072,
      recommendedPresets: [768, 1024, 1536]
    }
  },
  {
    pattern: /^text-embedding-ada-002$/i,
    caps: { supportsTruncation: false, nativeDimensions: 1536 }
  },
  {
    pattern: /^nomic-embed/i,
    caps: {
      supportsTruncation: true,
      nativeDimensions: 768,
      recommendedPresets: [256, 512]
    }
  },
  {
    pattern: /^bge-/i,
    caps: { supportsTruncation: false }
  },
  {
    pattern: /^(e5-|gte-)/i,
    caps: { supportsTruncation: false }
  },
  {
    pattern: /^mxbai-embed/i,
    caps: { supportsTruncation: false, nativeDimensions: 1024 }
  }
]

/** Look up embedding capabilities for a model name (case-insensitive patterns). */
export function getEmbeddingCapabilities(
  modelName: string
): EmbeddingCapabilities {
  const name = (modelName ?? '').trim()
  if (!name) return { supportsTruncation: undefined }
  for (const entry of REGISTRY) {
    if (entry.pattern.test(name)) return { ...entry.caps }
  }
  return { supportsTruncation: undefined }
}
