import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

// Hoisted mocks: searchTasks is the ctx method the typeahead calls (wrapping
// the SearchBlocks binding); sqliteQuery resolves dep-label lookups so chips
// render text. The picker now goes through ctx.searchTasks, not the binding
// directly (AGENTS.md — no direct wailsjs imports from plugin code).
const mocks = vi.hoisted(() => ({
  searchTasks: vi.fn(),
  setTaskBlockedBy: vi.fn(),
  sqliteQuery: vi.fn()
}))

vi.mock('../../../../wailsjs/runtime/runtime.js', () => ({
  EventsOn: vi.fn(() => () => {})
}))

import DependencyPicker from './DependencyPicker.svelte'
import type { PluginContext } from '../../sdk'
import { v2CtxStubs } from '../../test-helpers'

function makeCtx(overrides: Partial<PluginContext> = {}): PluginContext {
  return {
    activeNotebook: 'Work',
    activeSection: 'Journal',
    activePage: 'Daily',
    today: '2026-07-01',
    ...v2CtxStubs,
    // Overrides come AFTER v2CtxStubs so the test's mocks win over the
    // no-op stubs (v2CtxStubs also defines these methods).
    sqliteQuery: mocks.sqliteQuery,
    setTaskBlockedBy: mocks.setTaskBlockedBy,
    searchTasks: mocks.searchTasks,
    ...overrides
  } as PluginContext
}

async function flush() {
  await tick()
  await new Promise((r) => setTimeout(r, 0))
}

describe('DependencyPicker (#303)', () => {
  beforeEach(() => {
    mocks.searchTasks.mockReset()
    mocks.setTaskBlockedBy.mockReset().mockResolvedValue(true)
    // Label resolution: refreshDeps runs a single SELECT id, clean_content
    // FROM blocks WHERE id IN (...) for the whole blocked-by set. Echo each
    // queried id back with a label so the chips render text.
    mocks.sqliteQuery
      .mockReset()
      .mockImplementation(async (_sql: string, params?: unknown[]) => {
        const ids = (params as string[]) ?? []
        const rows = ids.map((id) => ({ id, clean_content: 'Resolved label' }))
        return { rows, truncated: false }
      })
  })

  afterEach(() => cleanup())

  it('renders existing dependencies as chips with remove buttons', async () => {
    mocks.sqliteQuery.mockImplementation(
      async (_sql: string, params?: unknown[]) => {
        const ids = (params as string[]) ?? []
        const rows = ids.map((id) => ({ id, clean_content: 'Prereq task' }))
        return { rows, truncated: false }
      }
    )
    render(DependencyPicker, {
      cardId: 'task-1',
      blockedBy: ['dep-1'],
      ctx: makeCtx()
    })
    await flush()

    expect(screen.getByText('Prereq task')).toBeInTheDocument()
    expect(
      screen.getByLabelText('Remove dependency Prereq task')
    ).toBeInTheDocument()
  })

  it('shows the empty-state message when there are no prerequisites', async () => {
    render(DependencyPicker, {
      cardId: 'task-1',
      blockedBy: [],
      ctx: makeCtx()
    })
    await flush()

    expect(screen.getByText('No prerequisites.')).toBeInTheDocument()
  })

  it('removing a dependency calls setTaskBlockedBy with the filtered list', async () => {
    mocks.sqliteQuery.mockImplementation(async () => ({
      rows: [{ id: 'dep-a', clean_content: 'Dep A' }],
      truncated: false
    }))
    render(DependencyPicker, {
      cardId: 'task-1',
      blockedBy: ['dep-a'],
      ctx: makeCtx()
    })
    await flush()

    await fireEvent.click(screen.getByLabelText('Remove dependency Dep A'))
    await flush()

    expect(mocks.setTaskBlockedBy).toHaveBeenCalledWith('task-1', [])
  })

  it('searching renders matching tasks and Enter adds the highlighted result', async () => {
    mocks.sqliteQuery.mockImplementation(async () => ({
      rows: [{ clean_content: 'X' }],
      truncated: false
    }))
    mocks.searchTasks.mockResolvedValue([
      {
        id: 'dep-found',
        clean_content: 'Found task',
        notebook: 'Work',
        section: 'Journal',
        page: 'Daily'
      }
    ])
    render(DependencyPicker, {
      cardId: 'task-1',
      blockedBy: [],
      ctx: makeCtx()
    })
    await flush()

    const input = screen.getByLabelText(
      'Search tasks to add as dependencies'
    ) as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'found' } })
    // The search is debounced; flush past the timer.
    await new Promise((r) => setTimeout(r, 220))
    await flush()

    // The result is rendered in the listbox.
    expect(
      screen.getByRole('option', { name: /Found task/ })
    ).toBeInTheDocument()
    await fireEvent.keyDown(input, { key: 'Enter' })
    await flush()

    expect(mocks.setTaskBlockedBy).toHaveBeenCalledWith('task-1', ['dep-found'])
  })

  it('filters out self and already-added deps from search results', async () => {
    mocks.sqliteQuery.mockImplementation(async () => ({
      rows: [{ clean_content: 'Existing' }],
      truncated: false
    }))
    mocks.searchTasks.mockResolvedValue([
      {
        id: 'task-1',
        clean_content: 'Self',
        notebook: 'W',
        section: '',
        page: 'P'
      },
      {
        id: 'dep-existing',
        clean_content: 'Existing',
        notebook: 'W',
        section: '',
        page: 'P'
      },
      {
        id: 'dep-new',
        clean_content: 'New candidate',
        notebook: 'W',
        section: '',
        page: 'P'
      }
    ])
    render(DependencyPicker, {
      cardId: 'task-1',
      blockedBy: ['dep-existing'],
      ctx: makeCtx()
    })
    await flush()

    const input = screen.getByLabelText(
      'Search tasks to add as dependencies'
    ) as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'can' } })
    await new Promise((r) => setTimeout(r, 220))
    await flush()

    // Only the new candidate appears; self and the existing dep are filtered.
    expect(
      screen.getByRole('option', { name: /New candidate/ })
    ).toBeInTheDocument()
    expect(screen.queryByText('Self')).toBeNull()
  })

  it('surfaces a circular-dependency error inline when the backend rejects', async () => {
    mocks.sqliteQuery.mockImplementation(async () => ({
      rows: [{ clean_content: 'X' }],
      truncated: false
    }))
    mocks.searchTasks.mockResolvedValue([
      {
        id: 'dep-cycle',
        clean_content: 'Cycle',
        notebook: 'W',
        section: '',
        page: 'P'
      }
    ])
    mocks.setTaskBlockedBy.mockRejectedValueOnce(
      new Error('adding this dependency would create a circular dependency')
    )
    render(DependencyPicker, {
      cardId: 'task-1',
      blockedBy: [],
      ctx: makeCtx()
    })
    await flush()

    const input = screen.getByLabelText(
      'Search tasks to add as dependencies'
    ) as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'cyc' } })
    await new Promise((r) => setTimeout(r, 220))
    await flush()
    // Enter adds the highlighted result; the backend rejects with a cycle.
    await fireEvent.keyDown(input, { key: 'Enter' })
    // The add is optimistic; the cycle error surfaces after the backend
    // rejection settles + re-renders. findByText waits for it.
    expect(
      await screen.findByText(/Cannot add: would create a circular dependency/)
    ).toBeInTheDocument()
  })

  it('portals the results listbox out of the scroll container so it is not clipped (#376)', async () => {
    mocks.searchTasks.mockResolvedValue([
      {
        id: 'dep-portaled',
        clean_content: 'Portaled result',
        notebook: 'Work',
        section: 'Journal',
        page: 'Daily'
      }
    ])
    const { container } = render(DependencyPicker, {
      cardId: 'task-1',
      blockedBy: [],
      ctx: makeCtx()
    })
    await flush()

    const input = screen.getByLabelText(
      'Search tasks to add as dependencies'
    ) as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'port' } })
    // The search is debounced (180ms); flush past the timer.
    await new Promise((r) => setTimeout(r, 220))
    await flush()

    // The option renders into document.body via the shared <Popover> portal —
    // NOT inside the picker's container, so it escapes CardDetailPanel's
    // overflow-y-auto clip. Mirrors the recurrence dropdown's escape test.
    const option = await screen.findByRole('option', {
      name: /Portaled result/
    })
    expect(document.body.contains(option)).toBe(true)
    expect(container.contains(option)).toBe(false)
  })

  it('restores focus to the search input after adding a dependency (a11y)', async () => {
    mocks.searchTasks.mockResolvedValue([
      {
        id: 'dep-focus',
        clean_content: 'Focus result',
        notebook: 'Work',
        section: 'Journal',
        page: 'Daily'
      }
    ])
    render(DependencyPicker, {
      cardId: 'task-1',
      blockedBy: [],
      ctx: makeCtx()
    })
    await flush()

    const input = screen.getByLabelText(
      'Search tasks to add as dependencies'
    ) as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'foc' } })
    await new Promise((r) => setTimeout(r, 220))
    await flush()

    // Realistic interaction: the result button receives focus (mouse or
    // keyboard activation), then is clicked. addDep clears `results`
    // synchronously, which unmounts that focused button — without the
    // restore-to-input, focus would strand on document.body.
    const option = screen.getByRole('option', { name: /Focus result/ })
    const optionButton = option.querySelector('button') as HTMLButtonElement
    optionButton.focus()
    await fireEvent.click(optionButton)
    await flush()

    expect(mocks.setTaskBlockedBy).toHaveBeenCalledWith('task-1', ['dep-focus'])
    // Focus came back to the input, not document.body.
    expect(document.activeElement).toBe(input)
  })
})
