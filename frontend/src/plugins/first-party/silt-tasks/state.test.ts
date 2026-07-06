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
  setScope,
  narrowScopeTo,
  clearScopeOverride,
  setFilters,
  clearFilters,
  setFocusDate,
  clearFocusDate,
  setActiveFilter,
  clearActiveFilter,
  saveView,
  applySavedView,
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
      expect(s.groupBy).toBe('none')
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

  // ── savedViews (new; #323 saved-board lineage, generalized) ───────────

  describe('saveView() / applySavedView()', () => {
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

    it('applySavedView restores scope + filters + groupBy and flips override', () => {
      const filters: TaskFilters = {
        owners: ['bob'],
        priorities: [1],
        dueDate: '',
        tags: []
      }
      applySavedView({
        id: 'v1',
        name: 'n/a',
        scope: 'page',
        filters,
        groupBy: 'status'
      })
      const s = getTaskHubState()
      expect(s.scope).toBe('page')
      expect(s.filters.owners).toEqual(['bob'])
      expect(s.groupBy).toBe('status')
      // Clicking a saved view is user intent → override flipped.
      expect(s.scopeUserOverride).toBe(true)
    })

    it('applySavedView without scope leaves scope + override untouched', () => {
      setScope('section') // override = true
      applySavedView({ id: 'v1', name: 'filters only', groupBy: 'owner' })
      const s = getTaskHubState()
      expect(s.scope).toBe('section')
      expect(s.scopeUserOverride).toBe(true)
      expect(s.groupBy).toBe('owner')
    })
  })

  // ── reset (single source of truth) ────────────────────────────────────

  describe('resetTaskHubState()', () => {
    it('clears every field — scope/filters/focus/group/display/savedViews', () => {
      setDisplayMode('board')
      setGroupBy('status')
      setScope('page') // flips override
      setFilters({
        owners: ['a'],
        priorities: [1],
        dueDate: 'today',
        tags: ['x']
      })
      setFocusDate('2026-06-16')
      setActiveFilter('overdue')
      saveView({ id: 'v1', name: 'V' })

      resetTaskHubState()

      const s = getTaskHubState()
      expect(s.displayMode).toBe('list')
      expect(s.groupBy).toBe('none')
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
    })
  })
})
