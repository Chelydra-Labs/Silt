import { describe, it, expect } from 'vitest'
import { VIEW_CYCLE, nextView } from './viewCycle'

describe('VIEW_CYCLE', () => {
  it('contains notes → tags → tasks (#429 collapses Calendar+Kanban into Tasks)', () => {
    // Phase 10 retired the standalone Calendar and Kanban plugins; both are
    // now display modes of the unified silt-tasks hub, so the activity bar
    // exposes only notes / tags / tasks.
    expect([...VIEW_CYCLE]).toEqual(['notes', 'tags', 'tasks'])
  })
})

describe('nextView', () => {
  it('cycles notes → tags → tasks → notes (#429)', () => {
    expect(nextView('notes')).toBe('tags')
    expect(nextView('tags')).toBe('tasks')
    expect(nextView('tasks')).toBe('notes')
  })

  it('wraps from the last view back to the first', () => {
    expect(nextView('tasks')).toBe('notes')
  })

  it("returns 'notes' when current is not in the cycle (e.g. a plugin view)", () => {
    expect(nextView('silt-custom-view')).toBe('notes')
  })

  it("returns 'notes' when current is empty", () => {
    expect(nextView('')).toBe('notes')
  })
})
