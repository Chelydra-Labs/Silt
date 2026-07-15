import { describe, expect, it } from 'vitest'
import { getEmbeddingCapabilities } from './modelCapabilities'

describe('getEmbeddingCapabilities', () => {
  it('detects gemini-embedding Matryoshka support', () => {
    const c = getEmbeddingCapabilities('gemini-embedding-001')
    expect(c.supportsTruncation).toBe(true)
    expect(c.nativeDimensions).toBe(3072)
    expect(c.recommendedPresets).toContain(768)
  })

  it('detects text-embedding-3 families', () => {
    expect(getEmbeddingCapabilities('text-embedding-3-small').supportsTruncation).toBe(
      true
    )
    expect(getEmbeddingCapabilities('text-embedding-3-large').nativeDimensions).toBe(
      3072
    )
  })

  it('marks ada-002 and bge as fixed', () => {
    expect(getEmbeddingCapabilities('text-embedding-ada-002').supportsTruncation).toBe(
      false
    )
    expect(getEmbeddingCapabilities('bge-micro-v2').supportsTruncation).toBe(false)
  })

  it('detects nomic, e5, gte, mxbai', () => {
    expect(getEmbeddingCapabilities('nomic-embed-text').supportsTruncation).toBe(true)
    expect(getEmbeddingCapabilities('e5-small').supportsTruncation).toBe(false)
    expect(getEmbeddingCapabilities('gte-base').supportsTruncation).toBe(false)
    expect(getEmbeddingCapabilities('mxbai-embed-large').nativeDimensions).toBe(1024)
  })

  it('returns unknown for unrecognized models', () => {
    const c = getEmbeddingCapabilities('my-custom-embed-v9')
    expect(c.supportsTruncation).toBeUndefined()
  })

  it('handles empty model name', () => {
    expect(getEmbeddingCapabilities('').supportsTruncation).toBeUndefined()
  })
})
