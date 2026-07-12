import { describe, expect, it } from 'vitest'
import { chunksFromBlocks, hashesEqual } from './chunk'

describe('chunksFromBlocks', () => {
  it('creates one chunk per non-empty block with stable hash', async () => {
    const chunks = await chunksFromBlocks([
      {
        id: 'b1',
        notebook: 'N',
        section: 'S',
        page: 'P',
        line_number: 3,
        clean_content: 'Hello world'
      },
      {
        id: 'b2',
        notebook: 'N',
        section: 'S',
        page: 'P',
        clean_content: '   '
      }
    ])
    expect(chunks).toHaveLength(1)
    expect(chunks[0].blockId).toBe('b1')
    expect(chunks[0].chunkId).toBe('b1')
    expect(chunks[0].text).toBe('Hello world')
    expect(chunks[0].contentHash).toMatch(/^[a-f0-9]{64}$/)
    expect(chunks[0].lineNumber).toBe(3)
  })
})

describe('hashesEqual', () => {
  it('detects hash diffs', () => {
    expect(
      hashesEqual(
        [{ chunkId: 'a', contentHash: '1' }],
        [{ chunkId: 'a', contentHash: '1' }]
      )
    ).toBe(true)
    expect(
      hashesEqual(
        [{ chunkId: 'a', contentHash: '1' }],
        [{ chunkId: 'a', contentHash: '2' }]
      )
    ).toBe(false)
  })
})
