// Contracts for the shared saved-view bookkeeping helpers (#863).
// mergeViewById + stripSystemFlag are the load/persist primitives both
// consumers' settings layers compose on top of.
import { describe, it, expect } from 'vitest'
import { mergeViewById, stripSystemFlag, type SavedViewBase } from './viewState'

interface V extends SavedViewBase {
  mode: string
}

describe('mergeViewById', () => {
  it('dedupes by id across lists (later lists win)', () => {
    const a: V[] = [{ id: '1', name: 'one', mode: 'list' }]
    const b: V[] = [
      { id: '1', name: 'one-updated', mode: 'board' },
      { id: '2', name: 'two', mode: 'list' }
    ]
    expect(mergeViewById(a, b)).toEqual([
      { id: '1', name: 'one-updated', mode: 'board' },
      { id: '2', name: 'two', mode: 'list' }
    ])
  })

  it('preserves first-seen order when no collision', () => {
    const a: V[] = [{ id: 'sys', name: 'S', mode: 'list', system: true }]
    const b: V[] = [{ id: 'u1', name: 'U', mode: 'board' }]
    expect(mergeViewById(a, b).map((v) => v.id)).toEqual(['sys', 'u1'])
  })

  it('handles zero lists', () => {
    expect(mergeViewById()).toEqual([])
  })
})

describe('stripSystemFlag', () => {
  it('drops system views and removes the system marker from the rest', () => {
    const views: V[] = [
      { id: 'sys-1', name: 'S', mode: 'list', system: true },
      { id: 'u1', name: 'U', mode: 'board', system: false },
      { id: 'u2', name: 'U2', mode: 'list' }
    ]
    const out = stripSystemFlag(views)
    expect(out).toEqual([
      { id: 'u1', name: 'U', mode: 'board' },
      { id: 'u2', name: 'U2', mode: 'list' }
    ])
    // The system key is gone from every emitted record.
    for (const r of out) {
      expect('system' in r).toBe(false)
    }
  })

  it('emits an empty array when only system views are present', () => {
    expect(
      stripSystemFlag([{ id: 'sys', name: 'S', mode: 'list', system: true }])
    ).toEqual([])
  })
})
