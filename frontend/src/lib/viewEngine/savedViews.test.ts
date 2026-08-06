// Generic contracts for the shared partial-snapshot saved-view matcher (#863).
// Mirrors the lenient-match semantics extracted from
// silt-tasks/savedViews.test.ts: a view only constrains the dims it defines.
import { describe, it, expect } from 'vitest'
import { arrayEqual, defaultValuesEqual, viewMatchesState } from './savedViews'

describe('arrayEqual', () => {
  it('true for same length + same elements in order', () => {
    expect(arrayEqual([1, 2, 3], [1, 2, 3])).toBe(true)
  })
  it('false for length or element mismatch', () => {
    expect(arrayEqual([1, 2], [1, 2, 3])).toBe(false)
    expect(arrayEqual([1, 2, 3], [1, 2, 4])).toBe(false)
  })
  it('empty arrays are equal', () => {
    expect(arrayEqual([], [])).toBe(true)
  })
})

describe('defaultValuesEqual', () => {
  it('strict === for scalars', () => {
    expect(defaultValuesEqual('a', 'a')).toBe(true)
    expect(defaultValuesEqual(1, 1)).toBe(true)
    expect(defaultValuesEqual('a', 'b')).toBe(false)
  })
  it('shallow array equality for arrays', () => {
    expect(defaultValuesEqual(['x'], ['x'])).toBe(true)
    expect(defaultValuesEqual(['x'], ['y'])).toBe(false)
  })
  it('reference equality for objects (callers override per-dim)', () => {
    const o = { a: 1 }
    expect(defaultValuesEqual(o, o)).toBe(true)
    expect(defaultValuesEqual({ a: 1 }, { a: 1 })).toBe(false)
  })
})

describe('viewMatchesState — partial template matching', () => {
  const dims = ['mode', 'groupBy', 'tags'] as const

  it('matches when every defined dim equals the state', () => {
    const view = { mode: 'list', groupBy: 'owner' }
    const state = { mode: 'list', groupBy: 'owner', tags: ['x'] }
    expect(viewMatchesState(view, state, dims)).toBe(true)
  })

  it('mismatches when any defined dim differs', () => {
    const view = { mode: 'list', groupBy: 'owner' }
    const state = { mode: 'board', groupBy: 'owner' }
    expect(viewMatchesState(view, state, dims)).toBe(false)
  })

  it('undefined-by-view dims do not disqualify (system-template contract)', () => {
    // View says nothing about `tags`; state with any tags still matches.
    const view = { mode: 'list' }
    const state = { mode: 'list', tags: ['a', 'b', 'c'] }
    expect(viewMatchesState(view, state, dims)).toBe(true)
  })

  it('a view that defines every dim reduces to the strict check', () => {
    const view = { mode: 'list', groupBy: 'owner', tags: ['x'] }
    const state = { mode: 'list', groupBy: 'owner', tags: ['x'] }
    expect(viewMatchesState(view, state, dims)).toBe(true)
    expect(viewMatchesState(view, { ...state, tags: ['x', 'y'] }, dims)).toBe(
      false
    )
  })

  it('array dims use shallow element-wise equality', () => {
    const view = { tags: ['a', 'b'] }
    expect(viewMatchesState(view, { tags: ['a', 'b'] }, dims)).toBe(true)
    expect(viewMatchesState(view, { tags: ['b', 'a'] }, dims)).toBe(false)
  })

  it('equalsBy override applies only to the named dim', () => {
    // columns comparator treats wipLimit null/undefined as equal.
    const colEqual = (a: unknown, b: unknown): boolean => {
      const aa = a as { name: string; wip?: number | null }[]
      const bb = b as { name: string; wip?: number | null }[]
      if (aa.length !== bb.length) return false
      return aa.every(
        (c, i) =>
          c.name === bb[i].name && (c.wip ?? null) === (bb[i].wip ?? null)
      )
    }
    const dims2 = ['mode', 'columns'] as const
    const view = { mode: 'list', columns: [{ name: 'TODO' }, { name: 'DONE' }] }
    const state = {
      mode: 'list',
      columns: [{ name: 'TODO', wip: null }, { name: 'DONE' }]
    }
    expect(
      viewMatchesState(view, state, dims2, (d) =>
        d === 'columns' ? colEqual : undefined
      )
    ).toBe(true)
  })
})
