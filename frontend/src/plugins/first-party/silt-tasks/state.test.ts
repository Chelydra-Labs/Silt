// Direct unit tests for the unified Tasks hub state (#419 phase 4).
// Ports the behavioral assertions from focusState.test.ts (#322) and
// kanbanSharedState.test.ts (#323) against the new unified store, and
// adds coverage for the new displayMode / groupBy / savedViews surfaces.
// The scopeUserOverride invariant (#124) is re-asserted here because it
// is load-bearing — a regression would silently break the auto-narrow.
import { describe, expect, it, beforeEach, vi } from 'vitest'
import type { TaskFilters } from './state.svelte'
import {
  getTaskHubState,
  setDisplayMode,
  setGroupBy,
  setSort,
  setScope,
  narrowScopeTo,
  clearScopeOverride,
  setFilters,
  setColumns,
  clearFilters,
  setFocusDate,
  clearFocusDate,
  setActiveFilter,
  clearActiveFilter,
  saveView,
  applySavedView,
  deleteSavedView,
  clearActiveSavedView,
  reorderSavedViews,
  MAX_USER_SAVED_VIEWS,
  resetTaskHubState
} from './state.svelte'

describe('silt-tasks unified state (#419)', () => {
  beforeEach(() => {
    resetTaskHubState()
  })

  describe('getTaskHubState() — defaults', () => {
    it('returns the default state after reset', () => {
      const s = getTaskHubState()
      expect(s.displayMode).toBe('list')
      // #423: the default grouping is 'dueDate' so the unmodified list
      // experience (time-horizon buckets) survives the grouping engine
      // landing. The default within-group sort is also 'dueDate'.
      expect(s.groupBy).toBe('dueDate')
      expect(s.sort).toBe('dueDate')
      expect(s.scope).toBe('vault')
      expect(s.scopeUserOverride).toBe(false)
      expect(s.filters).toEqual({
        owners: [],
        priorities: [],
        dueDate: '',
        tags: []
      })
      expect(s.focusDate).toBe('')
      expect(s.activeFilter).toBe('all')
      expect(s.savedViews).toEqual([])
      // #427 active-view tracking defaults: no view active, no dirty flag.
      expect(s.activeSavedViewId).toBe('')
      expect(s.savedViewsDirty).toBe(false)
      expect(s.columns).toEqual(['TODO', 'DOING', 'DONE'])
    })
  })

  // ── displayMode / groupBy (new unified surfaces) ──────────────────────

  describe('setDisplayMode()', () => {
    it('switches between list and board', () => {
      setDisplayMode('board')
      expect(getTaskHubState().displayMode).toBe('board')
      setDisplayMode('list')
      expect(getTaskHubState().displayMode).toBe('list')
    })
  })

  describe('setGroupBy()', () => {
    it('writes the grouping dimension', () => {
      setGroupBy('status')
      expect(getTaskHubState().groupBy).toBe('status')
    })

    it('accepts the high-cardinality dimensions added in #423', () => {
      setGroupBy('tag')
      expect(getTaskHubState().groupBy).toBe('tag')
      setGroupBy('notebook')
      expect(getTaskHubState().groupBy).toBe('notebook')
      setGroupBy('section')
      expect(getTaskHubState().groupBy).toBe('section')
      setGroupBy('page')
      expect(getTaskHubState().groupBy).toBe('page')
    })
  })

  describe('setSort()', () => {
    it('writes the within-group sort mode', () => {
      setSort('priority')
      expect(getTaskHubState().sort).toBe('priority')
    })

    it('sort defaults to dueDate after reset', () => {
      setSort('title')
      resetTaskHubState()
      expect(getTaskHubState().sort).toBe('dueDate')
    })
  })

  // ── scope + scopeUserOverride invariant (#124, ported verbatim) ──────

  describe('setScope() — user-initiated scope change', () => {
    it('mutates scope AND flips scopeUserOverride', () => {
      setScope('notebook')
      const s = getTaskHubState()
      expect(s.scope).toBe('notebook')
      expect(s.scopeUserOverride).toBe(true)
    })

    it('a second setScope re-flips the override (no-op semantics)', () => {
      setScope('notebook')
      setScope('section')
      const s = getTaskHubState()
      expect(s.scope).toBe('section')
      expect(s.scopeUserOverride).toBe(true)
    })
  })

  describe('narrowScopeTo() — navigation auto-narrow', () => {
    it('mutates scope WITHOUT flipping scopeUserOverride', () => {
      narrowScopeTo('page')
      const s = getTaskHubState()
      expect(s.scope).toBe('page')
      expect(s.scopeUserOverride).toBe(false)
    })

    it('is a no-op when scopeUserOverride is true (#124 invariant)', () => {
      setScope('notebook') // flips override
      narrowScopeTo('vault') // nav would normally narrow to vault
      const s = getTaskHubState()
      // Scope stays 'notebook' (the user's pick), override stays true.
      expect(s.scope).toBe('notebook')
      expect(s.scopeUserOverride).toBe(true)
    })
  })

  describe('clearScopeOverride() — Follow affordance', () => {
    it('clears scopeUserOverride so nav can re-narrow', () => {
      setScope('section')
      clearScopeOverride()
      const s = getTaskHubState()
      expect(s.scope).toBe('section') // scope unchanged
      expect(s.scopeUserOverride).toBe(false)
    })

    it('a subsequent narrowScopeTo actually mutates again', () => {
      setScope('section')
      clearScopeOverride()
      narrowScopeTo('page')
      expect(getTaskHubState().scope).toBe('page')
    })
  })

  // ── filters (ported from kanbanSharedState) ───────────────────────────

  describe('setFilters() / clearFilters()', () => {
    it('setFilters replaces the filters object entirely', () => {
      setFilters({
        owners: ['alice'],
        priorities: [1, 2],
        dueDate: 'overdue',
        tags: ['backend']
      })
      const s = getTaskHubState()
      expect(s.filters.owners).toEqual(['alice'])
      expect(s.filters.priorities).toEqual([1, 2])
      expect(s.filters.dueDate).toBe('overdue')
      expect(s.filters.tags).toEqual(['backend'])
    })

    it('clearFilters resets to empty defaults', () => {
      setFilters({
        owners: ['alice'],
        priorities: [1],
        dueDate: 'today',
        tags: ['x']
      })
      clearFilters()
      expect(getTaskHubState().filters).toEqual({
        owners: [],
        priorities: [],
        dueDate: '',
        tags: []
      })
    })
  })

  // ── focusDate + activeFilter (ported from focusState) ─────────────────

  describe('setFocusDate() / clearFocusDate()', () => {
    it('setFocusDate writes the YYYY-MM-DD value', () => {
      setFocusDate('2026-06-16')
      expect(getTaskHubState().focusDate).toBe('2026-06-16')
    })

    it('clearFocusDate resets to empty string', () => {
      setFocusDate('2026-06-16')
      clearFocusDate()
      expect(getTaskHubState().focusDate).toBe('')
    })

    it('setFocusDate dispatches "calendar:focus-date" on window', () => {
      const handler = vi.fn()
      window.addEventListener('calendar:focus-date', handler)
      setFocusDate('2026-06-20')
      expect(handler).toHaveBeenCalledTimes(1)
      const detail = (handler.mock.calls[0][0] as CustomEvent).detail
      expect(detail.date).toBe('2026-06-20')
      window.removeEventListener('calendar:focus-date', handler)
    })

    it('clearFocusDate also dispatches the event with empty detail', () => {
      const handler = vi.fn()
      setFocusDate('2026-06-16')
      window.addEventListener('calendar:focus-date', handler)
      clearFocusDate()
      expect(handler).toHaveBeenCalledTimes(1)
      const detail = (handler.mock.calls[0][0] as CustomEvent).detail
      expect(detail.date).toBe('')
      window.removeEventListener('calendar:focus-date', handler)
    })
  })

  describe('setActiveFilter() / clearActiveFilter()', () => {
    it('setActiveFilter writes the filter value', () => {
      setActiveFilter('today')
      expect(getTaskHubState().activeFilter).toBe('today')
    })

    it('clearActiveFilter resets to "all"', () => {
      setActiveFilter('overdue')
      clearActiveFilter()
      expect(getTaskHubState().activeFilter).toBe('all')
    })
  })

  // ── savedViews (#427 — generalized from #323 saved boards) ───────────

  describe('saveView() — upsert + cap', () => {
    it('saveView adds a named view to the list', () => {
      saveView({ id: 'v1', name: 'My View', scope: 'notebook' })
      const views = getTaskHubState().savedViews
      expect(views).toHaveLength(1)
      expect(views[0].name).toBe('My View')
    })

    it('saveView replaces an existing view with the same id', () => {
      saveView({ id: 'v1', name: 'Old', scope: 'vault' })
      saveView({ id: 'v1', name: 'New', scope: 'page' })
      const views = getTaskHubState().savedViews
      expect(views).toHaveLength(1)
      expect(views[0].name).toBe('New')
      expect(views[0].scope).toBe('page')
    })

    it(`drops the LAST user view when exceeding the ${MAX_USER_SAVED_VIEWS}-cap`, () => {
      // Seed MAX user views.
      for (let i = 0; i < MAX_USER_SAVED_VIEWS; i++) {
        saveView({ id: `u${i}`, name: `V${i}` })
      }
      expect(getTaskHubState().savedViews).toHaveLength(MAX_USER_SAVED_VIEWS)
      // Adding one more should evict the last user view (u49) and append
      // the new one — staying at the cap, not exceeding it.
      saveView({ id: 'u-new', name: 'New' })
      const views = getTaskHubState().savedViews
      expect(views).toHaveLength(MAX_USER_SAVED_VIEWS)
      expect(views.find((v) => v.id === 'u-new')).toBeDefined()
      // The last user view is gone.
      expect(
        views.find((v) => v.id === `u${MAX_USER_SAVED_VIEWS - 1}`)
      ).toBeUndefined()
    })

    it('does NOT count system views toward the user cap', () => {
      // Seed the cap fully with system-flagged views.
      for (let i = 0; i < MAX_USER_SAVED_VIEWS; i++) {
        saveView({ id: `s${i}`, name: `Sys${i}`, system: true })
      }
      // Adding a real user view still goes in — system views don't
      // consume the user-view budget.
      saveView({ id: 'u1', name: 'User' })
      const views = getTaskHubState().savedViews
      expect(views.find((v) => v.id === 'u1')).toBeDefined()
      // No system view was evicted to make room.
      expect(views.filter((v) => v.system)).toHaveLength(MAX_USER_SAVED_VIEWS)
    })
  })

  describe('applySavedView() — restore all dimensions + mark active', () => {
    it('restores displayMode/groupBy/sort/scope/filters/calendarSubMode/columns', () => {
      const filters: TaskFilters = {
        owners: ['bob'],
        priorities: [1],
        dueDate: 'today',
        tags: ['x']
      }
      applySavedView({
        id: 'v1',
        name: 'n/a',
        displayMode: 'board',
        groupBy: 'status',
        sort: 'priority',
        scope: 'page',
        filters,
        calendarSubMode: 'week',
        columns: ['TODO', 'DOING', 'DONE', 'BLOCKED']
      })
      const s = getTaskHubState()
      expect(s.displayMode).toBe('board')
      expect(s.groupBy).toBe('status')
      expect(s.sort).toBe('priority')
      expect(s.scope).toBe('page')
      expect(s.filters.owners).toEqual(['bob'])
      expect(s.filters.dueDate).toBe('today')
      expect(s.calendarSubMode).toBe('week')
      expect(s.columns).toEqual(['TODO', 'DOING', 'DONE', 'BLOCKED'])
      // Clicking a saved view is user intent → override flipped.
      expect(s.scopeUserOverride).toBe(true)
    })

    it('sets activeSavedViewId and clears the dirty flag', () => {
      const s0 = getTaskHubState()
      s0.savedViewsDirty = true
      applySavedView({ id: 'v1', name: 'n/a', groupBy: 'owner' })
      const s = getTaskHubState()
      expect(s.activeSavedViewId).toBe('v1')
      expect(s.savedViewsDirty).toBe(false)
    })

    it('partial view leaves unspecified dimensions untouched', () => {
      setScope('section') // override = true, displayMode stays default
      applySavedView({ id: 'v1', name: 'filters only', groupBy: 'owner' })
      const s = getTaskHubState()
      expect(s.scope).toBe('section')
      expect(s.scopeUserOverride).toBe(true)
      expect(s.groupBy).toBe('owner')
    })
  })

  describe('deleteSavedView() — read-only system + clear-on-active', () => {
    it('removes a user view by id', () => {
      saveView({ id: 'v1', name: 'V1' })
      saveView({ id: 'v2', name: 'V2' })
      deleteSavedView('v1')
      const ids = getTaskHubState().savedViews.map((v) => v.id)
      expect(ids).toEqual(['v2'])
    })

    it('refuses to delete a system view (read-only)', () => {
      saveView({ id: 'sys-x', name: 'Sys', system: true })
      deleteSavedView('sys-x')
      const v = getTaskHubState().savedViews.find((x) => x.id === 'sys-x')
      expect(v).toBeDefined()
    })

    it('clears activeSavedViewId when the active view is deleted', () => {
      saveView({ id: 'v1', name: 'V1' })
      applySavedView({ id: 'v1', name: 'V1' })
      expect(getTaskHubState().activeSavedViewId).toBe('v1')
      deleteSavedView('v1')
      expect(getTaskHubState().activeSavedViewId).toBe('')
    })

    it('is a no-op for an unknown id', () => {
      saveView({ id: 'v1', name: 'V1' })
      deleteSavedView('does-not-exist')
      expect(getTaskHubState().savedViews).toHaveLength(1)
    })
  })

  describe('clearActiveSavedView() — unpin without deleting', () => {
    it('clears activeSavedViewId and the dirty flag', () => {
      saveView({ id: 'v1', name: 'V1' })
      applySavedView({ id: 'v1', name: 'V1' })
      const s = getTaskHubState()
      s.savedViewsDirty = true
      clearActiveSavedView()
      const after = getTaskHubState()
      expect(after.activeSavedViewId).toBe('')
      expect(after.savedViewsDirty).toBe(false)
      // The view itself is not deleted.
      expect(after.savedViews.find((v) => v.id === 'v1')).toBeDefined()
    })
  })

  describe('reorderSavedViews() — user-view reorder (#470)', () => {
    it('moves a view before the target when before=true', () => {
      saveView({ id: 'u1', name: 'A' })
      saveView({ id: 'u2', name: 'B' })
      saveView({ id: 'u3', name: 'C' })
      reorderSavedViews('u3', 'u1', true)
      const ids = getTaskHubState().savedViews.map((v) => v.id)
      expect(ids).toEqual(['u3', 'u1', 'u2'])
    })

    it('moves a view after the target when before=false', () => {
      saveView({ id: 'u1', name: 'A' })
      saveView({ id: 'u2', name: 'B' })
      saveView({ id: 'u3', name: 'C' })
      reorderSavedViews('u1', 'u2', false)
      const ids = getTaskHubState().savedViews.map((v) => v.id)
      expect(ids).toEqual(['u2', 'u1', 'u3'])
    })

    it('refuses to reorder system views (source or target)', () => {
      saveView({ id: 'sys-x', name: 'Sys', system: true })
      saveView({ id: 'u1', name: 'A' })
      reorderSavedViews('u1', 'sys-x', true)
      let ids = getTaskHubState().savedViews.map((v) => v.id)
      expect(ids).toEqual(['sys-x', 'u1'])
      reorderSavedViews('sys-x', 'u1', true)
      ids = getTaskHubState().savedViews.map((v) => v.id)
      expect(ids).toEqual(['sys-x', 'u1'])
    })

    it('is a no-op for identical ids or missing ids', () => {
      saveView({ id: 'u1', name: 'A' })
      saveView({ id: 'u2', name: 'B' })
      reorderSavedViews('u1', 'u1', true)
      expect(getTaskHubState().savedViews.map((v) => v.id)).toEqual([
        'u1',
        'u2'
      ])
      reorderSavedViews('u1', 'missing', true)
      expect(getTaskHubState().savedViews.map((v) => v.id)).toEqual([
        'u1',
        'u2'
      ])
    })
  })

  describe('savedViewsDirty — modified-tracking', () => {
    it('does NOT flip dirty when no saved view is active', () => {
      setDisplayMode('board')
      setGroupBy('status')
      setSort('priority')
      setScope('notebook')
      setFilters({ owners: ['x'], priorities: [], dueDate: '', tags: [] })
      setColumns(['TODO'])
      expect(getTaskHubState().savedViewsDirty).toBe(false)
    })

    it('flips dirty when ANY dimension changes while a view is active', () => {
      applySavedView({ id: 'v1', name: 'V1', displayMode: 'list' })
      expect(getTaskHubState().savedViewsDirty).toBe(false)

      setDisplayMode('board')
      expect(getTaskHubState().savedViewsDirty).toBe(true)

      // applySavedView clears the dirty flag (the state matches the view again).
      applySavedView({ id: 'v1', name: 'V1', displayMode: 'board' })
      expect(getTaskHubState().savedViewsDirty).toBe(false)

      // Every dimension setter flips the flag while a view is active.
      setGroupBy('status')
      expect(getTaskHubState().savedViewsDirty).toBe(true)
      applySavedView({ id: 'v1', name: 'V1', groupBy: 'status' })

      setSort('title')
      expect(getTaskHubState().savedViewsDirty).toBe(true)
      applySavedView({ id: 'v1', name: 'V1', sort: 'title' })

      setScope('page')
      expect(getTaskHubState().savedViewsDirty).toBe(true)
      applySavedView({ id: 'v1', name: 'V1', scope: 'page' })

      setFilters({ owners: ['a'], priorities: [], dueDate: '', tags: [] })
      expect(getTaskHubState().savedViewsDirty).toBe(true)
      applySavedView({
        id: 'v1',
        name: 'V1',
        filters: { owners: ['a'], priorities: [], dueDate: '', tags: [] }
      })

      setColumns(['TODO', 'DOING'])
      expect(getTaskHubState().savedViewsDirty).toBe(true)
    })

    it('narrowScopeTo flips dirty only when it actually changes scope', () => {
      applySavedView({ id: 'v1', name: 'V1', scope: 'vault' })
      // Same scope → no change → no dirty flip.
      narrowScopeTo('vault')
      expect(getTaskHubState().savedViewsDirty).toBe(false)
      // But narrowScopeTo is a no-op once the user has explicitly picked
      // a scope (#124 invariant), so this scenario can't arise in
      // practice — the test just locks the contract.
    })
  })

  // ── reset (single source of truth) ────────────────────────────────────

  describe('resetTaskHubState()', () => {
    it('clears every field — scope/filters/focus/group/display/savedViews', () => {
      setDisplayMode('board')
      setGroupBy('status')
      setSort('title')
      setScope('page') // flips override
      setFilters({
        owners: ['a'],
        priorities: [1],
        dueDate: 'today',
        tags: ['x']
      })
      setFocusDate('2026-06-16')
      setActiveFilter('overdue')
      setColumns(['TODO', 'DOING', 'DONE', 'BLOCKED'])
      saveView({ id: 'v1', name: 'V' })
      applySavedView({ id: 'v1', name: 'V' })
      // Now an active view + dirty flag should be set; verify the reset
      // clears them too (#427 active-view tracking).
      setGroupBy('owner')
      expect(getTaskHubState().activeSavedViewId).toBe('v1')
      expect(getTaskHubState().savedViewsDirty).toBe(true)

      resetTaskHubState()

      const s = getTaskHubState()
      expect(s.displayMode).toBe('list')
      expect(s.groupBy).toBe('dueDate')
      expect(s.sort).toBe('dueDate')
      expect(s.scope).toBe('vault')
      expect(s.scopeUserOverride).toBe(false)
      expect(s.filters).toEqual({
        owners: [],
        priorities: [],
        dueDate: '',
        tags: []
      })
      expect(s.focusDate).toBe('')
      expect(s.activeFilter).toBe('all')
      expect(s.columns).toEqual(['TODO', 'DOING', 'DONE'])
      expect(s.savedViews).toEqual([])
      expect(s.activeSavedViewId).toBe('')
      expect(s.savedViewsDirty).toBe(false)
    })
  })
})
