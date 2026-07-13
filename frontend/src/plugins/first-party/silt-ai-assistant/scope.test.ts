import { describe, expect, it } from 'vitest'
import { resolveBlockForSelection } from './scope'

describe('resolveBlockForSelection', () => {
  const blocks = [
    { id: 'a', clean_content: 'Hello world' },
    { id: 'b', clean_content: 'Another paragraph with more text' },
    { id: 'c', clean_content: 'shared word here' }
  ]

  it('matches full block exactly', () => {
    const r = resolveBlockForSelection(blocks, 'Hello world')
    expect(r).toEqual({
      id: 'a',
      blockText: 'Hello world',
      fullBlock: true
    })
  })

  it('matches unique containment as partial', () => {
    const r = resolveBlockForSelection(blocks, 'paragraph with more')
    expect(r?.id).toBe('b')
    expect(r?.fullBlock).toBe(false)
  })

  it('returns null when nothing matches', () => {
    expect(resolveBlockForSelection(blocks, 'zzzz')).toBeNull()
  })
})
