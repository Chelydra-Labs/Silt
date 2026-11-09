import { describe, expect, it } from 'vitest'
import { coerceTaskRow, type TaskDetail } from './types'

// shortlist_rank coercion (#1092): the rank is nullable with 0 as a REAL
// top rank, so coercion must preserve null (unranked) and 0 distinctly —
// coercing null to 0 (or 0 to null) would collapse "top focus" into
// "unranked".
describe('coerceTaskRow — shortlist_rank (#1092)', () => {
  it('preserves null as null (unranked, never coerced to 0)', () => {
    const row = coerceTaskRow({ id: 'a', shortlist_rank: null })
    expect(row.shortlist_rank).toBeNull()
  })

  it('preserves 0 as 0 (top rank, a real value)', () => {
    const row = coerceTaskRow({ id: 'b', shortlist_rank: 0 })
    expect(row.shortlist_rank).toBe(0)
  })

  it('keeps ranks 1-3 as numbers', () => {
    for (const n of [1, 2, 3]) {
      const row = coerceTaskRow({ id: 'c', shortlist_rank: n })
      expect(row.shortlist_rank).toBe(n)
    }
  })

  it('treats an absent field (SQL row never set) as null', () => {
    const row = coerceTaskRow({ id: 'd' }) as TaskDetail
    expect(row.shortlist_rank == null ? null : row.shortlist_rank).toBeNull()
  })
})
