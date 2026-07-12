import { describe, expect, it } from 'vitest'
import { rankByEmbedding } from './suggestRelated'

describe('suggest related ranking', () => {
  it('ranks by cosine similarity', () => {
    const candidates = [
      { id: 'a', text: 'alpha' },
      { id: 'b', text: 'beta' },
      { id: 'c', text: 'gamma' }
    ]
    const query = [1, 0, 0]
    const embeddings = [
      [0.9, 0.1, 0],
      [0.1, 0.9, 0],
      [0, 0, 1]
    ]
    const ranked = rankByEmbedding(query, candidates, embeddings, 2)
    expect(ranked[0].blockId).toBe('a')
    expect(ranked.length).toBeLessThanOrEqual(2)
  })
})
