import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

// The hub persists via the settings store (which wraps the wails
// UpdatePluginSetting binding) and reads the synchronous config snapshot —
// same mock seam Kanban.test.ts uses.
const mocks = vi.hoisted(() => ({
  sqliteQuery: vi.fn(),
  updatePluginSetting: vi.fn().mockResolvedValue(true),
  settings: {
    config: {
      plugins: {
        active: [],
        disabled: [],
        plugin_settings: {} as Record<string, Record<string, unknown>>
      }
    }
  },
  blockChangedCallbacks: [] as Array<() => void>
}))

vi.mock('../../../settings/store.svelte', () => ({
  settings: mocks.settings,
  updatePluginSetting: mocks.updatePluginSetting
}))

vi.mock('../../../../wailsjs/runtime/runtime.js', () => ({
  EventsOn: vi.fn(() => () => {})
}))

import TasksHub from './TasksHub.svelte'
import type {
  PluginContext,
  PluginManifest,
  PluginEventName,
  PluginEventPayload
} from '../../sdk'
import { v2CtxStubs } from '../../test-helpers'
import { getTaskHubState, resetTaskHubState } from './state.svelte'

// jsdom polyfills: ListView pulls in TaskEditDrawer/TaskSubEditorModal, whose
// transition:fly + TipTap need Element.animate / elementFromPoint / Range rects.
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
if (typeof document !== 'undefined' && !document.elementFromPoint) {
  document.elementFromPoint = () => document.body
}
if (
  typeof window !== 'undefined' &&
  window.Range &&
  !Range.prototype.getClientRects
) {
  const zeroRect: DOMRect = {
    x: 0,
    y: 0,
    left: 0,
    top: 0,
    right: 0,
    bottom: 0,
    width: 0,
    height: 0,
    toJSON() {
      return this
    }
  }
  Range.prototype.getClientRects = (() => [
    zeroRect
  ]) as unknown as typeof Range.prototype.getClientRects
  Range.prototype.getBoundingClientRect = () => zeroRect
}

function makeCtx(): PluginContext {
  return {
    ...v2CtxStubs,
    activeNotebook: '',
    activeSection: '',
    activePage: '',
    today: '2026-07-06',
    sqliteQuery: mocks.sqliteQuery,
    mutateBlock: vi.fn(),
    updateBlockState: vi.fn(),
    updateTaskMeta: vi.fn(),
    getPluginSettings: vi.fn(() => Promise.resolve({})),
    on: <E extends PluginEventName>(
      event: E,
      cb: (payload: PluginEventPayload<E>) => void
    ) => {
      if (event === 'block:changed') {
        const cbAny = cb as unknown as () => void
        mocks.blockChangedCallbacks.push(cbAny)
        return () => {
          const i = mocks.blockChangedCallbacks.indexOf(cbAny)
          if (i >= 0) mocks.blockChangedCallbacks.splice(i, 1)
        }
      }
      return () => {}
    }
  }
}

const MANIFEST: PluginManifest = {
  id: 'silt-tasks',
  name: 'Tasks',
  version: '1.0.0'
}

async function flush() {
  await tick()
  await new Promise((r) => setTimeout(r, 0))
}

describe('Tasks hub shell (#424)', () => {
  beforeEach(() => {
    mocks.sqliteQuery.mockReset()
    // Default: every query returns an empty set so List renders its empty
    // state without drawer/sub-editor side effects.
    mocks.sqliteQuery.mockResolvedValue({ rows: [], truncated: false })
    mocks.updatePluginSetting.mockReset().mockResolvedValue(true)
    mocks.settings.config.plugins.plugin_settings = {}
    mocks.blockChangedCallbacks.length = 0
    resetTaskHubState()
  })

  afterEach(() => {
    cleanup()
  })

  it('renders the header, mode switcher, filter row, and scope breadcrumb', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    // Title from the manifest name.
    expect(screen.getByText('Tasks')).toBeInTheDocument()
    // Mode segmented control is a radiogroup with three radios.
    expect(screen.getByTestId('tasks-hub-mode-switcher')).toBeInTheDocument()
    expect(
      screen.getByRole('radio', { name: /List mode/i })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('radio', { name: /Board mode/i })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('radio', { name: /Calendar mode/i })
    ).toBeInTheDocument()
    // Scope breadcrumb radiogroup.
    expect(
      screen.getByRole('radiogroup', { name: 'Tasks scope' })
    ).toBeInTheDocument()
    // Shared FilterBar chip row.
    expect(screen.getByRole('button', { name: /Owner/i })).toBeInTheDocument()
  })

  it('defaults to List mode and renders the list renderer', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const listRadio = screen.getByRole('radio', { name: /List mode/i })
    expect(listRadio.getAttribute('aria-checked')).toBe('true')
    // ListView's root + empty state are present; stubs are not.
    expect(document.querySelector('[data-tasks-view]')).toBeTruthy()
    expect(screen.queryByTestId('tasks-board-stub')).toBeNull()
    expect(screen.queryByTestId('tasks-calendar-stub')).toBeNull()
  })

  it('switching to Board renders the Board renderer and persists the preference', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    await fireEvent.click(screen.getByRole('radio', { name: /Board mode/i }))
    await flush()

    // Board is no longer a stub (#421): the real renderer mounts.
    expect(screen.getByTestId('tasks-board')).toBeInTheDocument()
    expect(screen.queryByTestId('tasks-board-stub')).toBeNull()
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'silt-tasks',
      'default_display_mode',
      'board'
    )
  })

  it('switching to Calendar renders the Calendar stub and persists', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    await fireEvent.click(screen.getByRole('radio', { name: /Calendar mode/i }))
    await flush()

    expect(screen.getByTestId('tasks-calendar-stub')).toBeInTheDocument()
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'silt-tasks',
      'default_display_mode',
      'calendar'
    )
  })

  it('hydrates the display mode from the persisted vault setting on mount', async () => {
    mocks.settings.config.plugins.plugin_settings['silt-tasks'] = {
      default_display_mode: 'calendar'
    }

    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    // The persisted 'calendar' mode is restored without an explicit click.
    expect(screen.getByTestId('tasks-calendar-stub')).toBeInTheDocument()
    expect(
      screen
        .getByRole('radio', { name: /Calendar mode/i })
        .getAttribute('aria-checked')
    ).toBe('true')
  })

  it('mode switcher uses roving tabindex (checked radio is tabbable)', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const listRadio = screen.getByRole('radio', { name: /List mode/i })
    const boardRadio = screen.getByRole('radio', { name: /Board mode/i })
    // List is checked → tabindex 0; Board is not → tabindex -1.
    expect(listRadio.getAttribute('tabindex')).toBe('0')
    expect(boardRadio.getAttribute('tabindex')).toBe('-1')

    await fireEvent.click(boardRadio)
    await flush()

    // After the switch, roving tabindex follows the new selection.
    expect(boardRadio.getAttribute('tabindex')).toBe('0')
    expect(listRadio.getAttribute('tabindex')).toBe('-1')
  })

  it('Ctrl+Shift+V cycles List → Board → Calendar', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    window.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'V', ctrlKey: true, shiftKey: true })
    )
    await flush()

    // Board renderer mounts (no longer a stub, #421).
    expect(screen.getByTestId('tasks-board')).toBeInTheDocument()

    window.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'V', ctrlKey: true, shiftKey: true })
    )
    await flush()

    expect(screen.getByTestId('tasks-calendar-stub')).toBeInTheDocument()
  })

  it('header count reflects counts reported by the active renderer', async () => {
    // Return one open + one done task so the list reports 1 active / 1 done.
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes("status != 'DONE'")) {
        return {
          rows: [
            {
              id: 'o1',
              notebook: '.silt',
              section: '',
              page: 'tasks',
              file_date: '2026-07-06',
              clean_content: 'an open task',
              status: 'TODO',
              owner: '',
              start_date: '',
              due_date: '',
              priority: 0
            }
          ],
          truncated: false
        }
      }
      if (sql.includes("status = 'DONE'")) {
        return {
          rows: [
            {
              id: 'd1',
              notebook: '.silt',
              section: '',
              page: 'tasks',
              file_date: '2026-07-06',
              clean_content: 'a done task',
              status: 'DONE'
            }
          ],
          truncated: false
        }
      }
      return { rows: [], truncated: false }
    })

    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const count = screen.getByTestId('tasks-hub-count')
    expect(count.textContent).toContain('1 active')
    expect(count.textContent).toContain('1 done')
  })
})

describe('Tasks hub — group-by + sort selectors (#423)', () => {
  beforeEach(() => {
    mocks.sqliteQuery.mockReset()
    mocks.sqliteQuery.mockResolvedValue({ rows: [], truncated: false })
    mocks.updatePluginSetting.mockReset().mockResolvedValue(true)
    mocks.settings.config.plugins.plugin_settings = {}
    mocks.blockChangedCallbacks.length = 0
    resetTaskHubState()
  })

  afterEach(() => {
    cleanup()
  })

  it('renders Group-by and Sort selects in the header', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const groupBy = screen.getByTestId('tasks-hub-group-by')
    const sort = screen.getByTestId('tasks-hub-sort')
    expect(groupBy).toBeInTheDocument()
    expect(sort).toBeInTheDocument()
    // Both expose an accessible label.
    expect(groupBy.getAttribute('aria-label')).toBe('Group tasks by')
    expect(sort.getAttribute('aria-label')).toBe('Sort tasks by')
  })

  it('changing Group-by updates state and persists the preference', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    await fireEvent.change(screen.getByTestId('tasks-hub-group-by'), {
      target: { value: 'status' }
    })
    await flush()

    expect(getTaskHubState().groupBy).toBe('status')
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'silt-tasks',
      'default_group_by',
      'status'
    )
  })

  it('changing Sort updates state and persists the preference', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    await fireEvent.change(screen.getByTestId('tasks-hub-sort'), {
      target: { value: 'priority' }
    })
    await flush()

    expect(getTaskHubState().sort).toBe('priority')
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'silt-tasks',
      'default_sort',
      'priority'
    )
  })

  it('hydrates Group-by + Sort from the persisted vault settings on mount', async () => {
    mocks.settings.config.plugins.plugin_settings['silt-tasks'] = {
      default_group_by: 'owner',
      default_sort: 'title'
    }

    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    expect(getTaskHubState().groupBy).toBe('owner')
    expect(getTaskHubState().sort).toBe('title')
    // The select reflects the hydrated value.
    const groupBy = screen.getByTestId(
      'tasks-hub-group-by'
    ) as HTMLSelectElement
    expect(groupBy.value).toBe('owner')
  })
})
