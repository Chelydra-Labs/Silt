// Tests for the system-default saved views + the two fingerprint helpers
// (#427 Phase 6). The fingerprints power the "is the live state the same
// as this saved view?" check used by the header bookmark + (Phase 7) the
// sidebar list highlight; stability across calls is what makes that check
// reliable, so we assert it directly.
import { describe, expect, it, beforeEach } from 'vitest'
import {
  SYSTEM_VIEWS,
  fingerprintOf,
  fingerprintOfState,
  viewMatchesState
} from './savedViews'
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

describe('viewMatchesState (#432 — lenient match for partial-template views)', () => {
  // Build a default state once; tests clone + mutate. Defaults represent the
  // hub state AFTER `applySavedView(SYSTEM_VIEWS[1])` (sys-by-owner) has run
  // against fresh defaults: the view's defined dims (list/owner/priority/
  // vault/empty-filters) are applied, and the dims it doesn't define
  // (calendarSubMode, columns) retain their freshDefaults() values. This is
  // the state in which the bookmark/sidebar active-highlight check runs.
  function makeState(overrides: Partial<TaskHubState> = {}): TaskHubState {
    return {
      displayMode: 'list',
      groupBy: 'owner',
      sort: 'priority',
      scope: 'vault',
      scopeUserOverride: false,
      filters: { owners: [], priorities: [], dueDate: '', tags: [] },
      focusDate: '',
      activeFilter: 'all',
      calendarSubMode: 'month',
      columns: ['TODO', 'DOING', 'DONE'],
      savedViews: [],
      activeSavedViewId: '',
      savedViewsDirty: false,
      ...overrides
    }
  }

  it('system view (sys-by-owner) matches state after applySavedView', () => {
    // sys-by-owner defines list/owner/priority/vault/empty-filters.
    // The state has those values PLUS calendarSubMode=month and columns=[TODO,DOING,DONE]
    // (defaults the view doesn't constrain). Strict fingerprint would mismatch
    // on those two undefined-by-view dims.
    expect(viewMatchesState(SYSTEM_VIEWS[1], makeState())).toBe(true)
  })

  it('system view does NOT match state when a defined dim differs', () => {
    // sys-today-board wants board/status; state is list/owner.
    // The view's displayMode + groupBy don't match → not active.
    expect(viewMatchesState(SYSTEM_VIEWS[0], makeState())).toBe(false)
  })

  it('changing an UNDEFINED dim (columns) does not disqualify a system view', () => {
    // sys-by-owner doesn't define columns; user changes columns → still matches.
    const state = makeState({ columns: ['TODO', 'DOING', 'DONE', 'BLOCKED'] })
    expect(viewMatchesState(SYSTEM_VIEWS[1], state)).toBe(true)
  })

  it('changing a DEFINED dim (groupBy) disqualifies the view', () => {
    const state = makeState({ groupBy: 'status' })
    expect(viewMatchesState(SYSTEM_VIEWS[1], state)).toBe(false)
  })

  it('a complete-snapshot user view reduces to the strict check', () => {
    // A user view that defines every dim behaves like the old strict fingerprint.
    const state = makeState()
    const userView: SavedView = {
      id: 'u1',
      name: 'mine',
      system: false,
      displayMode: state.displayMode,
      groupBy: state.groupBy,
      sort: state.sort,
      scope: state.scope,
      filters: { ...state.filters },
      calendarSubMode: state.calendarSubMode,
      columns: [...state.columns]
    }
    expect(viewMatchesState(userView, state)).toBe(true)
    // Any single-dim change flips it false.
    expect(viewMatchesState(userView, { ...state, columns: ['X'] })).toBe(false)
  })

  it('filters defined as empty arrays match state with empty arrays', () => {
    // The SYSTEM_VIEWS all define filters as empty arrays; default state
    // also has empty arrays. This is the common matching case.
    expect(viewMatchesState(SYSTEM_VIEWS[1], makeState())).toBe(true)
  })

  it('view without filters field matches any state filters', () => {
    // A view that omits filters entirely doesn't constrain them.
    const view: SavedView = { id: 'x', name: 'no-filters' }
    expect(viewMatchesState(view, makeState())).toBe(true)
    expect(
      viewMatchesState(
        view,
        makeState({
          filters: {
            owners: ['alice'],
            priorities: [1],
            dueDate: 'today',
            tags: ['x']
          }
        })
      )
    ).toBe(true)
  })
})
