import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

// transition:fly / glass overlays may call element.animate under Svelte 5.
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

const paletteMocks = vi.hoisted(() => ({
  settings: {
    config: {
      hotkeys: {
        open_search: 'Ctrl+Shift+F',
        new_task: 'Ctrl+Shift+N'
      }
    }
  }
}))

vi.mock('../../../../settings/store.svelte', () => ({
  settings: paletteMocks.settings
}))

import TasksCommandPalette from './TasksCommandPalette.svelte'
import { getTaskHubState, resetTaskHubState, saveView } from '../state.svelte'
import type { SavedView } from '../state.svelte'

function makeProps(
  overrides: Partial<{
    open: boolean
    onClose: () => void
    onDisplayMode: (m: string) => void
    onGroupBy: (g: string) => void
    onSort: (s: string) => void
    onApplyView: (v: SavedView) => void
    onFindTask: () => void
    onAddTask: () => void
  }> = {}
) {
  return {
    open: true,
    onClose: vi.fn(),
    onDisplayMode: vi.fn(),
    onGroupBy: vi.fn(),
    onSort: vi.fn(),
    onApplyView: vi.fn(),
    onFindTask: vi.fn(),
    onAddTask: vi.fn(),
    ...overrides
  }
}

beforeEach(() => {
  cleanup()
  resetTaskHubState()
})
afterEach(cleanup)

describe('TasksCommandPalette (#436)', () => {
  it('renders the combobox + listbox when open', async () => {
    render(TasksCommandPalette, { props: makeProps() })
    await tick()

    const input = screen.getByTestId('tasks-command-palette-input')
    expect(input).toBeInTheDocument()
    expect(input.getAttribute('role')).toBe('combobox')
    expect(input.getAttribute('aria-expanded')).toBe('true')
    expect(input.getAttribute('aria-controls')).toBe(
      'tasks-command-palette-listbox'
    )

    const list = screen.getByTestId('tasks-command-palette-list')
    expect(list.getAttribute('role')).toBe('listbox')

    // Core command families are present.
    expect(screen.getByText('Switch to List')).toBeInTheDocument()
    expect(screen.getByText('Switch to Board')).toBeInTheDocument()
    expect(screen.getByText('Group by Status')).toBeInTheDocument()
    expect(screen.getByText('Sort by Priority')).toBeInTheDocument()
    expect(screen.getByText('Find task…')).toBeInTheDocument()
    expect(screen.getByText('Add task…')).toBeInTheDocument()
  })

  it('does not render when open is false', async () => {
    render(TasksCommandPalette, { props: makeProps({ open: false }) })
    await tick()
    expect(
      screen.queryByTestId('tasks-command-palette')
    ).not.toBeInTheDocument()
  })

  it('filters commands by fuzzy includes match', async () => {
    render(TasksCommandPalette, { props: makeProps() })
    await tick()

    const input = screen.getByTestId('tasks-command-palette-input')
    await fireEvent.input(input, { target: { value: 'board' } })
    await tick()

    expect(screen.getByText('Switch to Board')).toBeInTheDocument()
    expect(screen.queryByText('Switch to List')).not.toBeInTheDocument()
    expect(screen.queryByText('Find task…')).not.toBeInTheDocument()
  })

  it('filters via subsequence match (e.g. "gbs" → Group by Status)', async () => {
    render(TasksCommandPalette, { props: makeProps() })
    await tick()

    const input = screen.getByTestId('tasks-command-palette-input')
    // "Group by Status" — subsequence g-b-s across words.
    await fireEvent.input(input, { target: { value: 'gbs' } })
    await tick()

    expect(screen.getByText('Group by Status')).toBeInTheDocument()
  })

  it('ArrowDown + Enter runs the selected command and closes', async () => {
    const onDisplayMode = vi.fn()
    const onClose = vi.fn()
    render(TasksCommandPalette, {
      props: makeProps({ onDisplayMode, onClose })
    })
    await tick()

    const panel = screen.getByTestId('tasks-command-palette')
    // Default selection is index 0 (Switch to List). ArrowDown → Board.
    await fireEvent.keyDown(panel, { key: 'ArrowDown' })
    await fireEvent.keyDown(panel, { key: 'Enter' })
    await tick()

    expect(onDisplayMode).toHaveBeenCalledWith('board')
    expect(onClose).toHaveBeenCalled()
  })

  it('Enter on filtered Group by runs onGroupBy', async () => {
    const onGroupBy = vi.fn()
    const onClose = vi.fn()
    render(TasksCommandPalette, {
      props: makeProps({ onGroupBy, onClose })
    })
    await tick()

    const input = screen.getByTestId('tasks-command-palette-input')
    await fireEvent.input(input, { target: { value: 'Group by Owner' } })
    await tick()

    const panel = screen.getByTestId('tasks-command-palette')
    await fireEvent.keyDown(panel, { key: 'Enter' })
    await tick()

    expect(onGroupBy).toHaveBeenCalledWith('owner')
    expect(onClose).toHaveBeenCalled()
  })

  it('Escape closes the palette', async () => {
    const onClose = vi.fn()
    render(TasksCommandPalette, { props: makeProps({ onClose }) })
    await tick()

    // Capture-phase window listener (same path as real Escape).
    window.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })
    )
    await tick()
    expect(onClose).toHaveBeenCalled()
  })

  it('lists saved views and activates on click', async () => {
    const view: SavedView = {
      id: 'view-1',
      name: 'Sprint 15',
      displayMode: 'board',
      groupBy: 'status',
      sort: 'priority'
    }
    saveView(view)
    expect(getTaskHubState().savedViews.some((v) => v.id === 'view-1')).toBe(
      true
    )

    const onApplyView = vi.fn()
    const onClose = vi.fn()
    render(TasksCommandPalette, {
      props: makeProps({ onApplyView, onClose })
    })
    await tick()

    const item = screen.getByText('Activate saved view: Sprint 15')
    await fireEvent.click(item)
    await tick()

    expect(onApplyView).toHaveBeenCalledWith(
      expect.objectContaining({ id: 'view-1', name: 'Sprint 15' })
    )
    expect(onClose).toHaveBeenCalled()
  })

  it('Find task / Add task invoke their callbacks', async () => {
    const onFindTask = vi.fn()
    const onAddTask = vi.fn()
    const onClose = vi.fn()
    render(TasksCommandPalette, {
      props: makeProps({ onFindTask, onAddTask, onClose })
    })
    await tick()

    await fireEvent.click(screen.getByText('Find task…'))
    expect(onFindTask).toHaveBeenCalled()
    expect(onClose).toHaveBeenCalledTimes(1)

    // Re-open for the second command (component stays mounted with open=true
    // in this unit test; onClose was called but open prop is still true).
    onClose.mockClear()
    await fireEvent.click(screen.getByText('Add task…'))
    expect(onAddTask).toHaveBeenCalled()
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('exposes aria-live region for the active selection', async () => {
    render(TasksCommandPalette, { props: makeProps() })
    await tick()

    const live = screen.getByTestId('tasks-command-palette-live')
    expect(live.getAttribute('aria-live')).toBe('polite')
    expect(live.textContent).toMatch(/Switch to List/)
  })

  it('sets aria-activedescendant on the combobox to the active option', async () => {
    render(TasksCommandPalette, { props: makeProps() })
    await tick()

    const input = screen.getByTestId('tasks-command-palette-input')
    // First command is mode-list.
    expect(input.getAttribute('aria-activedescendant')).toBe(
      'tasks-cmd-mode-list'
    )

    const panel = screen.getByTestId('tasks-command-palette')
    await fireEvent.keyDown(panel, { key: 'ArrowDown' })
    await tick()
    expect(input.getAttribute('aria-activedescendant')).toBe(
      'tasks-cmd-mode-board'
    )
  })
})
