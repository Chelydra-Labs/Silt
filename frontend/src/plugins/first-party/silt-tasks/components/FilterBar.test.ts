import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

// transition:fly (used on each popover) calls element.animate(); polyfill so
// the import graph resolves under jsdom. Mirrors the pattern in the other
// silt-tasks component tests.
if (!Element.prototype.animate) {
  Element.prototype.animate = function () {
    return {
      cancel() {},
      finish() {},
      play() {},
      pause() {},
      reverse() {},
      addEventListener() {},
      removeEventListener() {},
      onfinish: null,
      oncancel: null
    } as unknown as Animation
  }
}

import FilterBar from './FilterBar.svelte'
import type { PluginContext } from '../../../sdk'
import type { TaskFilters, GroupBy, SortMode, Scope } from '../state.svelte'

interface FilterBarProps {
  filters: TaskFilters
  owners: string[]
  tags: string[]
  onFiltersChange: (f: TaskFilters) => void
  onTextChange: (text: string) => void
  groupBy: GroupBy
  onGroupByChange: (g: GroupBy) => void
  sort: SortMode
  onSortChange: (s: SortMode) => void
  scope: Scope
  onScopeChange: (s: Scope) => void
  isScopeDisabled: (s: string) => boolean
  scopeCrumb: string
  scopeUserOverride: boolean
  onResetScope: () => void
  totalCount: number
}

function makeProps(overrides: Partial<FilterBarProps> = {}): FilterBarProps {
  return {
    filters: { owners: [], priorities: [], dueDate: '', tags: [] },
    owners: [],
    tags: [],
    onFiltersChange: vi.fn(),
    onTextChange: vi.fn(),
    groupBy: 'status',
    onGroupByChange: vi.fn(),
    sort: 'manual',
    onSortChange: vi.fn(),
    scope: 'vault',
    onScopeChange: vi.fn(),
    isScopeDisabled: () => false,
    scopeCrumb: '',
    scopeUserOverride: false,
    onResetScope: vi.fn(),
    totalCount: 0,
    ...overrides
  }
}

// Void PluginContext import so the type is referenced (keeps the import honest
// if future assertions need a ctx); FilterBar itself takes no ctx.
type _Ctx = PluginContext

beforeEach(cleanup)
afterEach(cleanup)

describe('FilterBar facet search (#462)', () => {
  it('does NOT render the owner search input when owners <= 10', async () => {
    render(FilterBar, {
      props: makeProps({ owners: ['A', 'B', 'C'] })
    })
    // Open the owner chip popover.
    await fireEvent.click(screen.getByRole('button', { name: /Owner/ }))
    await tick()
    expect(screen.queryByTestId('owner-facet-search')).toBeNull()
  })

  it('renders + filters the owner list when owners > 10', async () => {
    const owners = [
      'Alice',
      'Bob',
      'Carol',
      'Dave',
      'Eve',
      'Frank',
      'Grace',
      'Heidi',
      'Ivan',
      'Judy',
      'Mallory'
    ] // 11 items — exceeds the threshold
    render(FilterBar, { props: makeProps({ owners }) })

    await fireEvent.click(screen.getByRole('button', { name: /Owner/ }))
    await tick()
    const search = screen.getByTestId('owner-facet-search')
    expect(search).toBeTruthy()

    // Before filtering, every owner checkbox label is present.
    for (const o of owners) {
      expect(screen.getByText(o)).toBeTruthy()
    }

    // Type a query — only matching owners remain.
    await fireEvent.input(search, { target: { value: 'ali' } })
    expect(screen.getByText('Alice')).toBeTruthy()
    expect(screen.queryByText('Bob')).toBeNull()
    expect(screen.queryByText('Mallory')).toBeNull()
  })

  it('shows "No matches" when the owner search yields nothing', async () => {
    const owners = Array.from({ length: 11 }, (_, i) => `owner-${i}`)
    render(FilterBar, { props: makeProps({ owners }) })

    await fireEvent.click(screen.getByRole('button', { name: /Owner/ }))
    await tick()
    const search = screen.getByTestId('owner-facet-search')
    await fireEvent.input(search, { target: { value: 'zzz-no-such-owner' } })
    expect(screen.getByText('No matches')).toBeTruthy()
  })

  it('renders the tag search input when tags > 10', async () => {
    const tags = Array.from({ length: 11 }, (_, i) => `tag-${i}`)
    render(FilterBar, { props: makeProps({ tags }) })

    await fireEvent.click(screen.getByRole('button', { name: /Tags/ }))
    await tick()
    expect(screen.getByTestId('tag-facet-search')).toBeTruthy()
  })

  it('ArrowDown in the search field moves focus to the first option (#462)', async () => {
    const owners = [
      'Alice',
      'Bob',
      'Carol',
      'Dave',
      'Eve',
      'Frank',
      'Grace',
      'Heidi',
      'Ivan',
      'Judy',
      'Mallory'
    ] // 11 — exceeds the threshold
    render(FilterBar, { props: makeProps({ owners }) })

    await fireEvent.click(screen.getByRole('button', { name: /Owner/ }))
    await tick()
    const search = screen.getByTestId('owner-facet-search')
    search.focus()
    expect(document.activeElement).toBe(search)

    // ArrowDown bridges from the search field into the list (the listbox's own
    // keydown handler only covers listbox descendants, so the search input — a
    // sibling — needs this bridge).
    await fireEvent.keyDown(search, { key: 'ArrowDown' })
    // First visible option's checkbox now holds focus.
    const firstCheckbox = screen.getByLabelText('Alice', { exact: false })
    expect(document.activeElement).toBe(firstCheckbox)
  })

  it('keeps Home and End as native caret keys in facet search inputs', async () => {
    const owners = Array.from({ length: 11 }, (_, i) => `owner-${i}`)
    render(FilterBar, { props: makeProps({ owners }) })

    await fireEvent.click(screen.getByRole('button', { name: /Owner/ }))
    await tick()
    const search = screen.getByTestId('owner-facet-search')
    search.focus()

    await fireEvent.keyDown(search, { key: 'Home' })
    expect(document.activeElement).toBe(search)
    await fireEvent.keyDown(search, { key: 'End' })
    expect(document.activeElement).toBe(search)
  })
})

describe('FilterBar popover focus', () => {
  it('moves focus into a single-select popover and returns it on Escape', async () => {
    render(FilterBar, { props: makeProps() })

    const trigger = screen.getByTestId('tasks-hub-scope-toggle')
    expect(trigger).toHaveAttribute('aria-controls', 'tasks-filter-scope')
    await fireEvent.click(trigger)
    await tick()

    expect(
      screen.getByRole('listbox', { name: 'Filter by scope' })
    ).toBeTruthy()
    expect(document.activeElement).toBe(
      screen.getByTestId('scope-option-vault')
    )

    await fireEvent.keyDown(window, { key: 'Escape' })
    await tick()
    expect(document.activeElement).toBe(trigger)
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
  })

  it('returns focus after choosing a single-select option', async () => {
    const onGroupByChange = vi.fn()
    render(FilterBar, { props: makeProps({ onGroupByChange }) })

    const trigger = screen.getByTestId('tasks-hub-group-by-toggle')
    await fireEvent.click(trigger)
    await tick()
    await fireEvent.click(screen.getByTestId('group-option-owner'))
    await tick()

    expect(onGroupByChange).toHaveBeenCalledWith('owner')
    expect(document.activeElement).toBe(trigger)
  })

  it('keeps the click-away backdrop out of the accessibility and tab order', async () => {
    render(FilterBar, { props: makeProps() })

    const trigger = screen.getByTestId('tasks-hub-scope-toggle')
    await fireEvent.click(trigger)
    await tick()

    const backdrop = screen.getByTestId('tasks-filter-backdrop')
    expect(backdrop.tagName).toBe('DIV')
    expect(backdrop).toHaveAttribute('role', 'presentation')
    expect(backdrop).toHaveAttribute('aria-hidden', 'true')
    expect(backdrop).toHaveAttribute('tabindex', '-1')

    await fireEvent.click(backdrop)
    await tick()
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
    expect(document.activeElement).toBe(trigger)
  })
})

describe('FilterBar stale chip (#440)', () => {
  it('toggles filters.stale true then false', async () => {
    const onFiltersChange = vi.fn()
    const { rerender } = render(FilterBar, {
      props: makeProps({ onFiltersChange })
    })

    const chip = screen.getByTestId('filter-stale-toggle')
    await fireEvent.click(chip)
    expect(onFiltersChange).toHaveBeenCalledWith(
      expect.objectContaining({ stale: true })
    )

    await rerender(
      makeProps({
        onFiltersChange,
        filters: {
          owners: [],
          priorities: [],
          dueDate: '',
          tags: [],
          stale: true
        }
      })
    )
    await fireEvent.click(screen.getByTestId('filter-stale-toggle'))
    expect(onFiltersChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ stale: false })
    )
  })

  it('exposes 30-day stale semantics via title', () => {
    render(FilterBar, { props: makeProps() })
    const chip = screen.getByTestId('filter-stale-toggle')
    expect(chip.getAttribute('title') ?? '').toMatch(/30 days/i)
  })
})

describe('FilterBar shortlist chip (#1092)', () => {
  it('toggles filters.shortlist true then false', async () => {
    const onFiltersChange = vi.fn()
    const { rerender } = render(FilterBar, {
      props: makeProps({ onFiltersChange })
    })

    const chip = screen.getByTestId('filter-shortlist-toggle')
    expect(chip).toHaveAttribute('aria-pressed', 'false')
    await fireEvent.click(chip)
    expect(onFiltersChange).toHaveBeenCalledWith(
      expect.objectContaining({ shortlist: true })
    )

    await rerender(
      makeProps({
        onFiltersChange,
        filters: {
          owners: [],
          priorities: [],
          dueDate: '',
          tags: [],
          shortlist: true
        }
      })
    )
    const activeChip = screen.getByTestId('filter-shortlist-toggle')
    expect(activeChip).toHaveAttribute('aria-pressed', 'true')
    await fireEvent.click(activeChip)
    expect(onFiltersChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ shortlist: false })
    )
  })

  it('exposes rank semantics via title', () => {
    render(FilterBar, { props: makeProps() })
    const chip = screen.getByTestId('filter-shortlist-toggle')
    expect(chip.getAttribute('title') ?? '').toMatch(/0–3/)
    expect(chip.getAttribute('title') ?? '').toMatch(/0 is the top rank/)
  })
})

describe('FilterBar aria-live announcer', () => {
  it('announces the active-filter count and updates on change', async () => {
    // The decomposition moved counts to SmartLists and trimmed its region to
    // counts-only, so the active-filter announcement must live here. This pins
    // that contract (AGENTS.md: aria-live for dynamic updates).
    const { rerender } = render(FilterBar, { props: makeProps() })
    await tick()
    let region = document.querySelector('[aria-live="polite"]')
    expect(region?.textContent?.trim()).toBe('No active filters')

    // Two facets selected (owner + priority) -> activeCount 2.
    await rerender(
      makeProps({
        filters: {
          owners: ['Alice'],
          priorities: [1],
          dueDate: '',
          tags: []
        }
      })
    )
    await tick()
    region = document.querySelector('[aria-live="polite"]')
    expect(region?.textContent?.trim()).toBe('2 active filters')
  })
})

describe('FilterBar consolidated clusters (#1058)', () => {
  it('groups organize and facet controls into explicit wrapping clusters and merges scope crumb into the Scope chip', () => {
    render(FilterBar, {
      props: makeProps({
        scope: 'notebook',
        scopeCrumb: 'Notebook · Work',
        scopeUserOverride: true,
        totalCount: 12
      })
    })

    const bar = screen.getByTestId('tasks-filter-bar')
    const organizeCluster = screen.getByTestId('tasks-filter-cluster-organize')
    const facetsCluster = screen.getByTestId('tasks-filter-cluster-facets')

    expect(bar).toContainElement(organizeCluster)
    expect(bar).toContainElement(facetsCluster)

    const scopeBtn = screen.getByTestId('tasks-hub-scope-toggle')
    const followBtn = screen.getByRole('button', {
      name: 'Reset Tasks scope to follow navigation'
    })
    const groupBtn = screen.getByTestId('tasks-hub-group-by-toggle')
    const sortBtn = screen.getByTestId('tasks-hub-sort-toggle')

    expect(organizeCluster).toContainElement(scopeBtn)
    expect(organizeCluster).toContainElement(followBtn)
    expect(organizeCluster).toContainElement(groupBtn)
    expect(organizeCluster).toContainElement(sortBtn)
    expect(scopeBtn).toHaveTextContent('Scope: Notebook · Work')

    expect(facetsCluster).toContainElement(
      screen.getByRole('button', { name: /Owner/ })
    )
    expect(facetsCluster).toContainElement(
      screen.getByRole('button', { name: /Priority/ })
    )
    expect(facetsCluster).toContainElement(
      screen.getByRole('button', { name: /Due date/ })
    )
    expect(facetsCluster).toContainElement(
      screen.getByTestId('filter-stale-toggle')
    )
    expect(facetsCluster).toContainElement(
      screen.getByRole('button', { name: /Tags/ })
    )

    // Duplicate "{scopeCrumb} · {totalCount} tasks" readout is removed from FilterBar.
    expect(bar.textContent).not.toContain('12 tasks')
  })
})

describe('FilterBar text narrowing', () => {
  it('renders a labelled, keyboard-reachable Find task input', () => {
    render(FilterBar, { props: makeProps() })
    const input = screen.getByLabelText('Find tasks')
    expect(input).toHaveAttribute('type', 'search')
    expect(input).toHaveAttribute('placeholder', 'Find task…')
    expect(input).toHaveAttribute('id', 'tasks-hub-text-filter')
    input.focus()
    expect(document.activeElement).toBe(input)
  })

  it('debounces typing into a single onTextChange commit', async () => {
    const onTextChange = vi.fn()
    render(FilterBar, { props: makeProps({ onTextChange }) })
    const input = screen.getByLabelText('Find tasks')
    await fireEvent.input(input, { target: { value: 'postgres' } })
    // Draft is local until the trailing debounce fires.
    expect(onTextChange).not.toHaveBeenCalled()
    await vi.waitFor(
      () => expect(onTextChange).toHaveBeenCalledWith('postgres'),
      { timeout: 1000 }
    )
    expect(onTextChange).toHaveBeenCalledTimes(1)
  })

  it('clears the text immediately via the clear button', async () => {
    const onTextChange = vi.fn()
    render(FilterBar, {
      props: makeProps({ onTextChange, filters: emptyTextFilters('pg') })
    })
    expect(screen.getByLabelText('Find tasks')).toHaveValue('pg')
    await fireEvent.click(
      screen.getByRole('button', { name: 'Clear task search' })
    )
    expect(onTextChange).toHaveBeenCalledWith('')
    expect(screen.getByLabelText('Find tasks')).toHaveValue('')
    expect(
      screen.queryByRole('button', { name: 'Clear task search' })
    ).toBeNull()
  })

  it('refocuses the input after the clear button removes itself', async () => {
    render(FilterBar, {
      props: makeProps({ filters: emptyTextFilters('pg') })
    })
    const clear = screen.getByRole('button', { name: 'Clear task search' })
    clear.focus()
    expect(document.activeElement).toBe(clear)

    await fireEvent.click(clear)
    await tick()
    expect(document.activeElement).toBe(screen.getByLabelText('Find tasks'))
  })

  it('syncs an external clear (Clear all) back into the draft', async () => {
    const { rerender } = render(FilterBar, {
      props: makeProps({ filters: emptyTextFilters('pg') })
    })
    await rerender(makeProps({ filters: emptyTextFilters('') }))
    await tick()
    expect(screen.getByLabelText('Find tasks')).toHaveValue('')
  })

  it('counts an active search as an active filter', async () => {
    render(FilterBar, {
      props: makeProps({ filters: emptyTextFilters('postgres') })
    })
    await tick()
    const region = document.querySelector('[aria-live="polite"]')
    expect(region?.textContent?.trim()).toBe('1 active filter')
  })

  it('does not count text that normalizes to no FTS term', async () => {
    // "a" and punctuation-only queries produce no MATCH predicate, so
    // claiming an active filter would promise narrowing that never happens.
    render(FilterBar, {
      props: makeProps({ filters: emptyTextFilters('a * ((') })
    })
    await tick()
    const region = document.querySelector('[aria-live="polite"]')
    expect(region?.textContent?.trim()).toBe('No active filters')
    expect(
      screen.queryByRole('button', { name: /Clear all/ })
    ).not.toBeInTheDocument()
  })
})

describe('FilterBar "Clear all" and the transient search', () => {
  it('cancels an in-flight debounce so the cleared text cannot reappear', async () => {
    const onTextChange = vi.fn()
    const onFiltersChange = vi.fn()
    render(FilterBar, {
      props: makeProps({
        onTextChange,
        onFiltersChange,
        filters: { owners: ['Alice'], priorities: [], dueDate: '', tags: [] }
      })
    })
    const input = screen.getByLabelText('Find tasks')
    await fireEvent.input(input, { target: { value: 'postgres' } })
    // Facet active → Clear all is visible while the text commit is pending.
    await fireEvent.click(screen.getByRole('button', { name: /Clear all/ }))

    expect(onTextChange).toHaveBeenCalledWith('')
    expect(onFiltersChange).toHaveBeenCalledWith(
      expect.objectContaining({ owners: [] })
    )
    expect(screen.getByLabelText('Find tasks')).toHaveValue('')

    // The pending trailing commit was cancelled — the draft never lands.
    await new Promise((r) => setTimeout(r, 300))
    expect(onTextChange).not.toHaveBeenCalledWith('postgres')
    expect(onTextChange).toHaveBeenCalledTimes(1)
  })

  it('clears a search-only view through the transient path (no saved-view dirty)', async () => {
    const onTextChange = vi.fn()
    const onFiltersChange = vi.fn()
    render(FilterBar, {
      props: makeProps({
        onTextChange,
        onFiltersChange,
        filters: emptyTextFilters('pg')
      })
    })
    await fireEvent.click(screen.getByRole('button', { name: /Clear all/ }))

    // facet setters never run — setFilters would mark the active view dirty.
    expect(onFiltersChange).not.toHaveBeenCalled()
    expect(onTextChange).toHaveBeenCalledWith('')
    expect(screen.getByLabelText('Find tasks')).toHaveValue('')
  })

  it('still clears non-text facets through onFiltersChange', async () => {
    const onFiltersChange = vi.fn()
    render(FilterBar, {
      props: makeProps({
        onFiltersChange,
        filters: {
          owners: ['Alice'],
          priorities: [1],
          dueDate: 'overdue',
          tags: ['x'],
          stale: true
        }
      })
    })
    await fireEvent.click(screen.getByRole('button', { name: /Clear all/ }))
    expect(onFiltersChange).toHaveBeenCalledTimes(1)
    const cleared = onFiltersChange.mock.calls[0][0] as TaskFilters
    // Exact-key canary (mirrors the TasksHub snapshot-shape guard): the
    // emitted reset must be the canonical default shape, so the next
    // filter field cannot survive "Clear all" or fall off the reset.
    expect(Object.keys(cleared).sort()).toEqual([
      'dueDate',
      'owners',
      'priorities',
      'shortlist',
      'tags',
      'text'
    ])
    expect(cleared).toEqual({
      owners: [],
      priorities: [],
      dueDate: '',
      tags: [],
      shortlist: false,
      text: ''
    })
  })
})

function emptyTextFilters(text: string): TaskFilters {
  return { owners: [], priorities: [], dueDate: '', tags: [], text }
}

// Keep the PluginContext type import referenced (no-unused).
export type __KeepCtxImport = _Ctx
