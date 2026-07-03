import { describe, it, expect } from 'vitest'
import { VIEW_CYCLE, nextView } from './viewCycle'

describe('VIEW_CYCLE', () => {
  it('contains notes → tags → calendar → tasks → kanban (#370 adds Tasks)', () => {
    // Tasks was inserted between Calendar and Kanban (#370) so the cycle
    // visits the date-scoped agenda and the vault-scoped undated-aware
    // task view adjacent — both are "what's on the plate right now"
    // surfaces, but only Tasks surfaces undated tasks.
    expect([...VIEW_CYCLE]).toEqual([
      'notes',
      'tags',
      'calendar',
      'tasks',
      'kanban'
    ])
  })
})

describe('nextView', () => {
  it('cycles notes → tags → calendar → tasks → kanban → notes (#370)', () => {
    expect(nextView('notes')).toBe('tags')
    expect(nextView('tags')).toBe('calendar')
    expect(nextView('calendar')).toBe('tasks')
    expect(nextView('tasks')).toBe('kanban')
    expect(nextView('kanban')).toBe('notes')
  })

  it("'agenda' is no longer in the cycle — falls back to notes (#322)", () => {
    // The Agenda view was merged into Calendar; routing an activeView of
    // 'agenda' through the cycle should reset to 'notes' rather than skip
    // through Calendar. The unified Calendar already exposes the Agenda
    // layout via its mode toggle.
    expect(nextView('agenda')).toBe('notes')
  })

  it('wraps from the last view back to the first', () => {
    expect(nextView('kanban')).toBe('notes')
  })

  it("returns 'notes' when current is not in the cycle (e.g. a plugin view)", () => {
    expect(nextView('silt-custom-view')).toBe('notes')
  })

  it("returns 'notes' when current is empty", () => {
    expect(nextView('')).toBe('notes')
  })
})
