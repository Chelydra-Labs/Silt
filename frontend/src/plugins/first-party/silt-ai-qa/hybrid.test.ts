import { describe, expect, it } from 'vitest'
import { fuseHybrid, trimToBudget } from './hybrid'
import type { RetrievedPassage } from './types'

describe('fuseHybrid (weighted RRF)', () => {
  it('dedupes by block id and ranks fused hits', () => {
    const vec = [
      { blockId: 'a', text: 'alpha' },
      { blockId: 'b', text: 'beta' }
    ]
    const fts = [
      { blockId: 'b', text: 'beta fts' },
      { blockId: 'c', text: 'gamma' }
    ]
    const out = fuseHybrid(vec, fts, { hybridWeight: 0.5, topK: 10 })
    const ids = out.map((p) => p.blockId)
    expect(new Set(ids).size).toBe(ids.length)
    expect(ids).toContain('a')
    expect(ids).toContain('b')
    expect(ids).toContain('c')
    // b appears in both lists → highest fused score
    expect(out[0].blockId).toBe('b')
    expect(out[0].citeIndex).toBe(1)
  })

  it('pure vector when hybridWeight=1', () => {
    const out = fuseHybrid(
      [{ blockId: 'v1', text: 'v' }],
      [{ blockId: 'f1', text: 'f' }],
      { hybridWeight: 1, topK: 5 }
    )
    expect(out.map((p) => p.blockId)).toEqual(['v1'])
  })

  it('pure FTS when hybridWeight=0', () => {
    const out = fuseHybrid(
      [{ blockId: 'v1', text: 'v' }],
      [{ blockId: 'f1', text: 'f' }],
      { hybridWeight: 0, topK: 5 }
    )
    expect(out.map((p) => p.blockId)).toEqual(['f1'])
  })

  it('respects minScore', () => {
    const out = fuseHybrid([{ blockId: 'a', text: 'a' }], [], {
      hybridWeight: 1,
      topK: 5,
      minScore: 999
    })
    expect(out).toEqual([])
  })
})

describe('trimToBudget', () => {
  it('drops lowest-score passages first', () => {
    const passages: RetrievedPassage[] = [
      {
        blockId: 'a',
        notebook: 'n',
        section: 's',
        page: 'p',
        lineNumber: 1,
        text: 'aaaa',
        score: 1,
        citeIndex: 1
      },
      {
        blockId: 'b',
        notebook: 'n',
        section: 's',
        page: 'p',
        lineNumber: 2,
        text: 'bbbbbbbb',
        score: 0.5,
        citeIndex: 2
      }
    ]
    const trimmed = trimToBudget(passages, 6)
    expect(trimmed).toHaveLength(1)
    expect(trimmed[0].blockId).toBe('a')
    expect(trimmed[0].citeIndex).toBe(1)
  })
})
