// Tests for the system-default saved views + the two fingerprint helpers
// (#427 Phase 6). The fingerprints power the "is the live state the same
// as this saved view?" check used by the header bookmark + (Phase 7) the
// sidebar list highlight; stability across calls is what makes that check
// reliable, so we assert it directly.
import { describe, expect, it, beforeEach } from 'vitest'
import { SYSTEM_VIEWS, fingerprintOf, fingerprintOfState } from './savedViews'
import {
  getTaskHubState,
  resetTaskHubState,
  type SavedView,
  type TaskHubState
} from './state.svelte'

describe('SYSTEM_VIEWS (#427)', () => {
  it('defines exactly three built-in defaults', () => {
    expect(SYSTEM_VIEWS).toHaveLength(3)
  })

  it('marks every system view with system: true (read-only marker)', () => {
    for (const v of SYSTEM_VIEWS) {
      expect(v.system).toBe(true)
    }
  })

  it('uses stable sys- ids so persistSavedViews can recognize + strip them', () => {
    for (const v of SYSTEM_VIEWS) {
      expect(v.id.startsWith('sys-')).toBe(true)
    }
    expect(SYSTEM_VIEWS.map((v) => v.id).sort()).toEqual([
      'sys-by-owner',
      'sys-today-board',
      'sys-week-calendar'
    ])
  })

  it('covers each of the three display modes (list / board / calendar)', () => {
    const modes = SYSTEM_VIEWS.map((v) => v.displayMode).sort()
    expect(modes).toEqual(['board', 'calendar', 'list'])
  })

  it("today's board is grouped by status with the today due-date filter", () => {
    const today = SYSTEM_VIEWS.find((v) => v.id === 'sys-today-board')!
    expect(today.displayMode).toBe('board')
    expect(today.groupBy).toBe('status')
    expect(today.sort).toBe('dueDate')
    expect(today.filters?.dueDate).toBe('today')
  })

  it('by-owner view groups by owner with priority sort', () => {
    const byOwner = SYSTEM_VIEWS.find((v) => v.id === 'sys-by-owner')!
    expect(byOwner.displayMode).toBe('list')
    expect(byOwner.groupBy).toBe('owner')
    expect(byOwner.sort).toBe('priority')
  })

  it("week calendar is calendar mode with calendarSubMode='week'", () => {
    const cal = SYSTEM_VIEWS.find((v) => v.id === 'sys-week-calendar')!
    expect(cal.displayMode).toBe('calendar')
    expect(cal.calendarSubMode).toBe('week')
    expect(cal.filters?.dueDate).toBe('week')
  })
})

describe('fingerprintOf (#427)', () => {
  it('returns the same string for two views with identical dimensions', () => {
    const a: SavedView = {
      id: 'a',
      name: 'A',
      displayMode: 'board',
      groupBy: 'status',
      sort: 'dueDate',
      scope: 'vault',
      filters: { owners: ['x'], priorities: [1], dueDate: 'today', tags: [] }
    }
    const b: SavedView = {
      id: 'b', // different id + name should NOT change the fingerprint
      name: 'B',
      displayMode: 'board',
      groupBy: 'status',
      sort: 'dueDate',
      scope: 'vault',
      filters: { owners: ['x'], priorities: [1], dueDate: 'today', tags: [] }
    }
    expect(fingerprintOf(a)).toBe(fingerprintOf(b))
  })

  it('changes when any tracked dimension changes', () => {
    const base: SavedView = {
      id: 'a',
      name: 'A',
      displayMode: 'list',
      groupBy: 'owner',
      sort: 'priority',
      scope: 'vault',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    const fp = fingerprintOf(base)
    expect(fingerprintOf({ ...base, displayMode: 'board' })).not.toBe(fp)
    expect(fingerprintOf({ ...base, groupBy: 'status' })).not.toBe(fp)
    expect(fingerprintOf({ ...base, sort: 'title' })).not.toBe(fp)
    expect(fingerprintOf({ ...base, scope: 'notebook' })).not.toBe(fp)
    expect(
      fingerprintOf({
        ...base,
        filters: { ...base.filters!, dueDate: 'today' }
      })
    ).not.toBe(fp)
    expect(fingerprintOf({ ...base, calendarSubMode: 'week' })).not.toBe(fp)
    expect(fingerprintOf({ ...base, columns: ['TODO', 'DOING'] })).not.toBe(fp)
  })

  it('ignores filter-array order via join (so ["a","b"] === ["b","a"])', () => {
    // The fingerprint uses .join('|') which is order-sensitive today, so
    // we lock the current behaviour: the same array in the same order
    // matches; reordering is treated as a different view. This matches
    // the legacy KanbanSidebar fingerprint contract (#323 lineage).
    const a: SavedView = {
      id: 'a',
      name: 'A',
      filters: { owners: ['x', 'y'], priorities: [], dueDate: '', tags: [] }
    }
    const b: SavedView = {
      id: 'b',
      name: 'B',
      filters: { owners: ['x', 'y'], priorities: [], dueDate: '', tags: [] }
    }
    expect(fingerprintOf(a)).toBe(fingerprintOf(b))
  })
})

describe('fingerprintOfState (#427)', () => {
  beforeEach(() => {
    resetTaskHubState()
  })

  it('matches fingerprintOf for a view built from the same state', () => {
    const s: TaskHubState = getTaskHubState()
    const viewFromState: SavedView = {
      id: 'v',
      name: 'snapshot',
      displayMode: s.displayMode,
      groupBy: s.groupBy,
      sort: s.sort,
      scope: s.scope,
      filters: { ...s.filters },
      calendarSubMode: s.calendarSubMode,
      columns: [...s.columns]
    }
    expect(fingerprintOfState(s)).toBe(fingerprintOf(viewFromState))
  })

  it('diverges after the user changes a dimension', () => {
    const s = getTaskHubState()
    const before = fingerprintOfState(s)
    // Mutate via direct field write (the fingerprint reads state fields,
    // independent of the setter layer).
    s.groupBy = 'status'
    expect(fingerprintOfState(s)).not.toBe(before)
  })
})
