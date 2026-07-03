import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

// Hoisted mocks: SearchBlocks is the Wails binding the typeahead calls; the
// sqliteQuery stub resolves dep-label lookups so chips can render text.
const mocks = vi.hoisted(() => ({
  SearchBlocks: vi.fn(),
  setTaskBlockedBy: vi.fn(),
  sqliteQuery: vi.fn()
}))

vi.mock('../../../../wailsjs/go/main/App.js', () => ({
  SearchBlocks: mocks.SearchBlocks
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
    // no-op stubs (v2CtxStubs also defines setTaskBlockedBy/sqliteQuery).
    sqliteQuery: mocks.sqliteQuery,
    setTaskBlockedBy: mocks.setTaskBlockedBy,
    ...overrides
  } as PluginContext
}

async function flush() {
  await tick()
  await new Promise((r) => setTimeout(r, 0))
}

describe('DependencyPicker (#303)', () => {
  beforeEach(() => {
    mocks.SearchBlocks.mockReset()
    mocks.setTaskBlockedBy.mockReset().mockResolvedValue(true)
    // Label resolution: return a clean_content for the queried uuid.
    mocks.sqliteQuery.mockReset().mockImplementation(async () => ({
      rows: [{ clean_content: 'Resolved label' }],
      truncated: false
    }))
  })

  afterEach(() => cleanup())

  it('renders existing dependencies as chips with remove buttons', async () => {
    mocks.sqliteQuery.mockImplementation(async () => ({
      rows: [{ clean_content: 'Prereq task' }],
      truncated: false
    }))
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
      rows: [{ clean_content: 'Dep A' }],
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
    mocks.SearchBlocks.mockResolvedValue([
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
    mocks.SearchBlocks.mockResolvedValue([
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
    mocks.SearchBlocks.mockResolvedValue([
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
})
