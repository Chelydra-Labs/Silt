// Contracts for the shared saved-view bookkeeping helpers (#863).
// stripSystemFlag is the persist primitive both consumers' settings layers
// compose on top of.
import { describe, it, expect } from 'vitest'
import { stripSystemFlag, type SavedViewBase } from './viewState'

interface V extends SavedViewBase {
  mode: string
}

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
