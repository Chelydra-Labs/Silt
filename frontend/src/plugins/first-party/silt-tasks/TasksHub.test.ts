import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

// The hub persists through the PluginContext SDK (ctx.updatePluginSetting)
// and reads its slice via ctx.getPluginSettings — TasksHub's onMount calls
// initTasksSettings(ctx) before any load* read, so the mock ctx just needs
// both bindings wired to the hoisted spies.
const mocks = vi.hoisted(() => ({
  sqliteQuery: vi.fn(),
  updatePluginSetting: vi.fn().mockResolvedValue(true),
  updateBlockState: vi.fn(),
  getTaskUndoState: vi.fn(),
  undoLastTaskMutation: vi.fn(),
  tasksSettings: {},
  blockChangedCallbacks: [] as Array<() => void>,
  configChangedCallbacks: [] as Array<() => void>
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn(() => () => {})
  },
  Call: { ByID: vi.fn(), ByName: vi.fn() },
  CancellablePromise: class {
    then() {
      return this
    }
    catch() {
      return this
    }
    finally() {
      return this
    }
  },
  Create: {
    Nullable: (fn: unknown) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

import TasksHub from './TasksHub.svelte'
import type {
  PluginContext,
  PluginManifest,
  PluginEventName,
  PluginEventPayload
} from '../../sdk'
import { v2CtxStubs } from '../../test-helpers'
import {
  clearTaskPageRoute,
  enterTaskPageRoute,
  applySavedView,
  getTaskHubState,
  getTaskHubViewState,
  getTaskPageRoute,
  resetTaskHubState
} from './state.svelte'
import { getTasksUndoView } from './undo.svelte'
import { getTaskWeekStart } from '../../../lib/taskWeekStart.svelte'
import { settings } from '../../../settings/store.svelte'

// jsdom polyfills: ListView pulls in TaskEditDrawer/TaskSubEditorModal, whose
// transition:fly + TipTap need Element.animate / elementFromPoint / Range rects.
if (!Element.prototype.getAnimations) {
  Element.prototype.getAnimations = () => []
}
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

function makeCtx(overrides: Partial<PluginContext> = {}): PluginContext {
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
    getPluginSettings: vi.fn(() => Promise.resolve(mocks.tasksSettings)),
    updatePluginSetting: mocks.updatePluginSetting,
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
      if (event === 'config:changed') {
        const cbAny = cb as unknown as () => void
        mocks.configChangedCallbacks.push(cbAny)
        return () => {
          const i = mocks.configChangedCallbacks.indexOf(cbAny)
          if (i >= 0) mocks.configChangedCallbacks.splice(i, 1)
        }
      }
      return () => {}
    },
    ...overrides
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
    mocks.tasksSettings = {}
    mocks.blockChangedCallbacks.length = 0
    mocks.configChangedCallbacks.length = 0
    clearTaskPageRoute()
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
    // Scope selector toggle.
    expect(screen.getByTestId('tasks-hub-scope-toggle')).toBeInTheDocument()
    // Shared FilterBar chip row.
    expect(screen.getByRole('button', { name: /Owner/i })).toBeInTheDocument()
  })

  it('stacks header actions at narrow widths and preserves the desktop row', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const header = screen.getByTestId('tasks-hub-header')
    const primary = screen.getByTestId('tasks-hub-toolbar-primary')
    expect(header).toContainElement(primary)
    expect(header).toContainElement(screen.getByTestId('tasks-filter-bar'))
    expect(primary).toHaveClass('flex-wrap', 'sm:flex-nowrap')
    const actions = screen.getByTestId('tasks-hub-header-actions')
    expect(actions).toHaveClass('basis-full', 'sm:basis-auto', 'sm:ml-auto')
    expect(actions).toContainElement(
      screen.getByTestId('tasks-hub-mode-switcher')
    )
    const utilityCluster = screen.getByTestId('tasks-hub-utility-cluster')
    expect(actions).toContainElement(utilityCluster)
    expect(utilityCluster).toContainElement(
      screen.getByTestId('tasks-hub-bookmark')
    )
    expect(utilityCluster).toContainElement(
      screen.getByTestId('tasks-preferences')
    )
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

  it('enters from main navigation at Vault even when a page is active', async () => {
    render(TasksHub, {
      ctx: makeCtx({
        activeNotebook: 'Team',
        activeSection: 'Meetings',
        activePage: 'Weekly sync'
      }),
      manifest: MANIFEST
    })
    await flush()

    expect(getTaskHubState().scope).toBe('vault')
    expect(getTaskHubState().scopeUserOverride).toBe(false)
    expect(screen.getByTestId('tasks-hub-scope-toggle')).toHaveTextContent(
      'Scope: Vault'
    )
  })

  it('keeps deliberate Page scope selectable after entering from navigation', async () => {
    render(TasksHub, {
      ctx: makeCtx({
        activeNotebook: 'Team',
        activeSection: 'Meetings',
        activePage: 'Weekly sync'
      }),
      manifest: MANIFEST
    })
    await flush()

    await fireEvent.click(screen.getByTestId('tasks-hub-scope-toggle'))
    await fireEvent.click(screen.getByTestId('scope-option-page'))
    await flush()

    expect(getTaskHubState().scope).toBe('page')
    expect(getTaskHubState().scopeUserOverride).toBe(true)
  })

  it('preserves an active saved view across remount and settings hydration', async () => {
    const savedView = {
      id: 'persisted-view',
      name: 'Planning board',
      displayMode: 'board' as const,
      groupBy: 'status' as const,
      sort: 'priority' as const,
      scope: 'page' as const,
      filters: { owners: [], priorities: [], dueDate: '' as const, tags: [] }
    }
    mocks.tasksSettings = {
      default_display_mode: 'calendar',
      default_group_by: 'owner',
      default_sort: 'title',
      saved_views: [savedView]
    }

    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    applySavedView(savedView)
    expect(getTaskHubState().activeSavedViewId).toBe(savedView.id)

    cleanup()
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    expect(getTaskHubState()).toMatchObject({
      displayMode: 'board',
      groupBy: 'status',
      sort: 'priority',
      scope: 'page',
      scopeUserOverride: true,
      activeSavedViewId: savedView.id,
      savedViewsDirty: false
    })
    expect(screen.getByTestId('tasks-board')).toBeInTheDocument()
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
      'default_display_mode',
      'board'
    )
  })

  it('switching to Calendar renders the Calendar renderer and persists', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    await fireEvent.click(screen.getByRole('radio', { name: /Calendar mode/i }))
    await flush()

    // Calendar is no longer a stub (#425): the real grid renderer mounts.
    expect(screen.getByTestId('tasks-calendar')).toBeInTheDocument()
    expect(screen.queryByTestId('tasks-calendar-stub')).toBeNull()
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'default_display_mode',
      'calendar'
    )
  })

  it('hydrates the display mode from the persisted vault setting on mount', async () => {
    mocks.tasksSettings = {
      default_display_mode: 'calendar',
      week_start: 'monday'
    }

    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    // The persisted 'calendar' mode is restored without an explicit click.
    expect(screen.getByTestId('tasks-calendar')).toBeInTheDocument()
    expect(
      screen
        .getByRole('radio', { name: /Calendar mode/i })
        .getAttribute('aria-checked')
    ).toBe('true')
    expect(getTaskWeekStart()).toBe('monday')
  })

  it('surfaces a task-settings load failure without applying stale hydration', async () => {
    const notify = vi.fn().mockResolvedValue(true)
    render(TasksHub, {
      ctx: makeCtx({
        getPluginSettings: vi
          .fn()
          .mockRejectedValue(new Error('vault is closing')),
        notify
      }),
      manifest: MANIFEST
    })
    await flush()

    expect(notify).toHaveBeenCalledWith({
      title: 'Tasks',
      body: "Couldn't load task preferences: vault is closing"
    })
    expect(getTaskWeekStart()).toBe('sunday')
  })

  it('exposes the vault-scoped week start in compact Task preferences', async () => {
    mocks.tasksSettings = { week_start: 'monday' }
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const trigger = screen.getByRole('button', { name: 'Task preferences' })
    expect(trigger).toHaveAttribute('aria-expanded', 'false')

    await fireEvent.click(trigger)
    await flush()

    expect(
      screen.getByRole('dialog', { name: 'Task preferences' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('group', { name: 'First day of week' })
    ).toBeInTheDocument()
    const monday = screen.getByRole('radio', { name: 'Monday' })
    expect(monday).toBeChecked()
    expect(monday).toHaveFocus()
  })

  it('updates week boundaries immediately and persists through Tasks settings', async () => {
    let finishSave: ((saved: boolean) => void) | undefined
    mocks.updatePluginSetting.mockImplementationOnce(
      () =>
        new Promise<boolean>((resolve) => {
          finishSave = resolve
        })
    )
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    await fireEvent.click(
      screen.getByRole('button', { name: 'Task preferences' })
    )
    await fireEvent.click(screen.getByRole('radio', { name: 'Monday' }))
    await tick()

    expect(getTaskWeekStart()).toBe('monday')
    expect(screen.getByRole('radio', { name: 'Monday' })).toBeChecked()
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'week_start',
      'monday'
    )

    finishSave?.(true)
    await flush()
  })

  it('restores the prior week start and announces a persistence failure', async () => {
    mocks.updatePluginSetting.mockResolvedValueOnce(false)
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    await fireEvent.click(
      screen.getByRole('button', { name: 'Task preferences' })
    )
    await fireEvent.click(screen.getByRole('radio', { name: 'Monday' }))
    await flush()

    expect(getTaskWeekStart()).toBe('sunday')
    expect(screen.getByRole('radio', { name: 'Sunday' })).toBeChecked()
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Couldn’t save this preference. Try again.'
    )
  })

  it('closes Task preferences with Escape and returns focus to its trigger', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const trigger = screen.getByRole('button', { name: 'Task preferences' })
    await fireEvent.click(trigger)
    await flush()
    expect(screen.getByTestId('tasks-preferences-popover')).toBeInTheDocument()

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flush()

    expect(trigger).toHaveAttribute('aria-expanded', 'false')
    expect(trigger).toHaveFocus()
  })

  it('keeps the Task preferences click-away backdrop out of the tab and accessibility trees', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const trigger = screen.getByRole('button', { name: 'Task preferences' })
    await fireEvent.click(trigger)
    await flush()

    const backdrop = screen.getByTestId('tasks-preferences-backdrop')
    expect(backdrop.tagName).toBe('DIV')
    expect(backdrop).toHaveAttribute('role', 'presentation')
    expect(backdrop).toHaveAttribute('aria-hidden', 'true')
    expect(backdrop).toHaveAttribute('tabindex', '-1')

    await fireEvent.click(backdrop)
    await flush()
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
    expect(trigger).toHaveFocus()
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

    expect(screen.getByTestId('tasks-calendar')).toBeInTheDocument()
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

  it('presents routed page context in List mode, announces it, focuses the heading, and exits cleanly', async () => {
    mocks.tasksSettings = { default_display_mode: 'board' }
    getTaskHubState().displayMode = 'board'
    getTaskHubState().filters = {
      owners: ['Alex'],
      priorities: [2],
      dueDate: 'week',
      tags: ['meeting']
    }
    enterTaskPageRoute(
      {
        source: 'linked:meetings',
        notebook: 'Team',
        section: 'Meetings',
        page: 'Weekly sync',
        nonce: 'route-ui-1'
      },
      {
        displayMode: 'list',
        activeFilter: 'all',
        filters: { owners: [], priorities: [], dueDate: '', tags: [] }
      }
    )

    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    expect(screen.getByTestId('tasks-page-context')).toHaveTextContent(
      'Team › Meetings › Weekly sync'
    )
    expect(screen.getByRole('radio', { name: /List mode/i })).toHaveAttribute(
      'aria-checked',
      'true'
    )
    expect(getTaskHubViewState().filters).toEqual({
      owners: [],
      priorities: [],
      dueDate: '',
      tags: []
    })
    expect(screen.getByTestId('tasks-page-route-live')).toHaveTextContent(
      'Showing tasks from Weekly sync.'
    )
    expect(screen.getByRole('heading', { name: /^Tasks/ })).toHaveFocus()
    expect(screen.getByTestId('tasks-page-empty')).toHaveTextContent(
      'No tasks on this page.'
    )

    await fireEvent.click(
      screen.getByRole('button', { name: 'Exit page task context' })
    )
    await flush()

    expect(getTaskPageRoute()).toBeNull()
    expect(screen.queryByTestId('tasks-page-context')).toBeNull()
    expect(screen.getByTestId('tasks-page-route-live')).toHaveTextContent(
      'Page task context cleared.'
    )
    expect(getTaskHubState().displayMode).toBe('board')
    expect(getTaskHubState().filters.owners).toEqual(['Alex'])
  })

  it('leaves an active saved base view unchanged while routed settings hydrate', async () => {
    mocks.tasksSettings = {
      default_display_mode: 'calendar',
      default_group_by: 'owner',
      default_sort: 'title',
      saved_views: [
        {
          id: 'persisted-other',
          name: 'Persisted other view',
          display_mode: 'calendar'
        }
      ]
    }
    const baseView = {
      id: 'base-view',
      name: 'My base view',
      displayMode: 'board' as const,
      groupBy: 'status' as const,
      sort: 'priority' as const,
      scope: 'notebook' as const,
      filters: {
        owners: ['Alex'],
        priorities: [2],
        dueDate: 'week' as const,
        tags: ['planning']
      }
    }
    const state = getTaskHubState()
    state.displayMode = 'board'
    state.groupBy = 'status'
    state.sort = 'priority'
    state.scope = 'notebook'
    state.scopeUserOverride = true
    state.filters = {
      owners: ['Alex'],
      priorities: [2],
      dueDate: 'week',
      tags: ['planning']
    }
    state.savedViews = [baseView]
    state.activeSavedViewId = baseView.id
    state.savedViewsDirty = false

    enterTaskPageRoute(
      {
        source: 'vault',
        notebook: 'Team',
        section: 'Meetings',
        page: 'Planning',
        nonce: 'route-preserve-base'
      },
      {
        displayMode: 'list',
        groupBy: 'dueDate',
        sort: 'dueDate',
        activeFilter: 'all',
        filters: { owners: [], priorities: [], dueDate: '', tags: [] }
      }
    )

    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    expect(getTaskHubViewState().displayMode).toBe('list')
    expect(getTaskHubState()).toMatchObject({
      displayMode: 'board',
      groupBy: 'status',
      sort: 'priority',
      scope: 'notebook',
      scopeUserOverride: true,
      filters: baseView.filters,
      savedViews: [baseView],
      activeSavedViewId: 'base-view',
      savedViewsDirty: false
    })

    await fireEvent.click(screen.getByRole('radio', { name: /Board mode/i }))
    await fireEvent.click(screen.getByTestId('tasks-hub-group-by-toggle'))
    await fireEvent.click(screen.getByTestId('group-option-owner'))
    await fireEvent.click(screen.getByTestId('tasks-hub-sort-toggle'))
    await fireEvent.click(screen.getByTestId('sort-option-title'))
    await flush()
    expect(getTaskHubViewState()).toMatchObject({
      displayMode: 'board',
      groupBy: 'owner',
      sort: 'title'
    })

    await fireEvent.click(
      screen.getByRole('button', { name: 'Exit page task context' })
    )
    await flush()

    expect(getTaskHubState()).toMatchObject({
      displayMode: 'board',
      groupBy: 'status',
      sort: 'priority',
      scope: 'notebook',
      scopeUserOverride: true,
      filters: baseView.filters,
      savedViews: [baseView],
      activeSavedViewId: 'base-view',
      savedViewsDirty: false
    })
  })

  it('applies deferred persisted settings and saved views after a route-first clear', async () => {
    mocks.tasksSettings = {
      default_display_mode: 'calendar',
      default_group_by: 'status',
      default_sort: 'priority',
      saved_views: [
        {
          id: 'deferred-view',
          name: 'Deferred view',
          displayMode: 'calendar',
          groupBy: 'status',
          sort: 'priority',
          scope: 'vault',
          filters: { owners: [], priorities: [], dueDate: '', tags: [] }
        }
      ]
    }
    enterTaskPageRoute(
      {
        source: 'vault',
        notebook: 'Team',
        section: 'Meetings',
        page: 'Planning',
        nonce: 'route-first-hydration'
      },
      { displayMode: 'list', groupBy: 'dueDate', sort: 'dueDate' }
    )

    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    // Route-local interaction must remain disposable rather than becoming base.
    await fireEvent.click(screen.getByRole('radio', { name: /Board mode/i }))
    await fireEvent.click(screen.getByTestId('tasks-hub-group-by-toggle'))
    await fireEvent.click(screen.getByTestId('group-option-owner'))
    await fireEvent.click(screen.getByTestId('tasks-hub-sort-toggle'))
    await fireEvent.click(screen.getByTestId('sort-option-title'))
    await flush()
    expect(getTaskHubViewState()).toMatchObject({
      displayMode: 'board',
      groupBy: 'owner',
      sort: 'title'
    })
    expect(getTaskHubState()).toMatchObject({
      displayMode: 'list',
      groupBy: 'dueDate',
      sort: 'dueDate'
    })

    await fireEvent.click(
      screen.getByRole('button', { name: 'Exit page task context' })
    )
    await flush()

    expect(getTaskHubState()).toMatchObject({
      displayMode: 'calendar',
      groupBy: 'status',
      sort: 'priority'
    })
    expect(getTaskHubState().savedViews.map((view) => view.id)).toContain(
      'deferred-view'
    )
  })

  it('retains a config rehydrate received during a route and applies the latest snapshot on exit', async () => {
    mocks.tasksSettings = {
      default_display_mode: 'board',
      default_group_by: 'status',
      default_sort: 'priority'
    }
    enterTaskPageRoute(
      {
        source: 'vault',
        notebook: 'Team',
        section: 'Meetings',
        page: 'Planning',
        nonce: 'route-config-refresh'
      },
      { displayMode: 'list' }
    )
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    mocks.tasksSettings = {
      default_display_mode: 'calendar',
      default_group_by: 'owner',
      default_sort: 'title',
      saved_views: [
        {
          id: 'latest-view',
          name: 'Latest view',
          displayMode: 'calendar',
          groupBy: 'owner',
          sort: 'title',
          scope: 'vault',
          filters: { owners: [], priorities: [], dueDate: '', tags: [] }
        }
      ]
    }
    for (const callback of mocks.configChangedCallbacks) callback()
    await flush()

    expect(getTaskHubState()).toMatchObject({
      displayMode: 'list',
      groupBy: 'dueDate',
      sort: 'dueDate'
    })

    await fireEvent.click(
      screen.getByRole('button', { name: 'Exit page task context' })
    )
    await flush()

    expect(getTaskHubState()).toMatchObject({
      displayMode: 'calendar',
      groupBy: 'owner',
      sort: 'title'
    })
    expect(getTaskHubState().savedViews.map((view) => view.id)).toContain(
      'latest-view'
    )
  })

  it('keeps routed mode, group, and sort changes out of global plugin settings', async () => {
    enterTaskPageRoute(
      {
        source: 'vault',
        notebook: 'Team',
        section: 'Meetings',
        page: 'Planning',
        nonce: 'route-local-controls'
      },
      { displayMode: 'list' }
    )
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    mocks.updatePluginSetting.mockClear()

    await fireEvent.click(screen.getByRole('radio', { name: /Board mode/i }))
    await fireEvent.click(screen.getByTestId('tasks-hub-group-by-toggle'))
    await fireEvent.click(screen.getByTestId('group-option-owner'))
    await fireEvent.click(screen.getByTestId('tasks-hub-sort-toggle'))
    await fireEvent.click(screen.getByTestId('sort-option-title'))
    await flush()

    expect(getTaskHubViewState()).toMatchObject({
      displayMode: 'board',
      groupBy: 'owner',
      sort: 'title'
    })
    expect(mocks.updatePluginSetting).not.toHaveBeenCalledWith(
      'default_display_mode',
      expect.anything()
    )
    expect(mocks.updatePluginSetting).not.toHaveBeenCalledWith(
      'default_group_by',
      expect.anything()
    )
    expect(mocks.updatePluginSetting).not.toHaveBeenCalledWith(
      'default_sort',
      expect.anything()
    )
  })

  it('cycles from the effective routed mode instead of the hidden base mode', async () => {
    mocks.tasksSettings = { default_display_mode: 'calendar' }
    getTaskHubState().displayMode = 'calendar'
    enterTaskPageRoute(
      {
        source: 'vault',
        notebook: 'Team',
        section: 'Meetings',
        page: 'Planning',
        nonce: 'route-keyboard-mode'
      },
      { displayMode: 'list' }
    )
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()
    mocks.updatePluginSetting.mockClear()

    window.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'V', ctrlKey: true, shiftKey: true })
    )
    await flush()

    expect(getTaskHubViewState().displayMode).toBe('board')
    expect(getTaskHubState().displayMode).toBe('calendar')
    expect(mocks.updatePluginSetting).not.toHaveBeenCalledWith(
      'default_display_mode',
      expect.anything()
    )
  })
})

describe('Tasks hub — group-by + sort selectors (#423)', () => {
  beforeEach(() => {
    mocks.sqliteQuery.mockReset()
    mocks.sqliteQuery.mockResolvedValue({ rows: [], truncated: false })
    mocks.updatePluginSetting.mockReset().mockResolvedValue(true)
    mocks.tasksSettings = {}
    mocks.blockChangedCallbacks.length = 0
    mocks.configChangedCallbacks.length = 0
    clearTaskPageRoute()
    resetTaskHubState()
  })

  afterEach(() => {
    cleanup()
  })

  it('renders Group-by and Sort dropdown toggles in the header', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const groupBy = screen.getByTestId('tasks-hub-group-by-toggle')
    const sort = screen.getByTestId('tasks-hub-sort-toggle')
    expect(groupBy).toBeInTheDocument()
    expect(sort).toBeInTheDocument()
  })

  it('changing Group-by updates state and persists the preference', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    await fireEvent.click(screen.getByTestId('tasks-hub-group-by-toggle'))
    await flush()
    await fireEvent.click(screen.getByTestId('group-option-status'))
    await flush()

    expect(getTaskHubState().groupBy).toBe('status')
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'default_group_by',
      'status'
    )
  })

  it('changing Sort updates state and persists the preference', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    await fireEvent.click(screen.getByTestId('tasks-hub-sort-toggle'))
    await flush()
    await fireEvent.click(screen.getByTestId('sort-option-priority'))
    await flush()

    expect(getTaskHubState().sort).toBe('priority')
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'default_sort',
      'priority'
    )
  })

  it('hydrates Group-by + Sort from the persisted vault settings on mount', async () => {
    mocks.tasksSettings = {
      default_group_by: 'owner',
      default_sort: 'title'
    }

    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    expect(getTaskHubState().groupBy).toBe('owner')
    expect(getTaskHubState().sort).toBe('title')

    // The toggle button reflects the hydrated value.
    const groupBy = screen.getByTestId('tasks-hub-group-by-toggle')
    expect(groupBy.textContent).toContain('Group: Owner')
  })
})

describe('Tasks hub — saved views bookmark (#427)', () => {
  beforeEach(() => {
    mocks.sqliteQuery.mockReset()
    mocks.sqliteQuery.mockResolvedValue({ rows: [], truncated: false })
    mocks.updatePluginSetting.mockReset().mockResolvedValue(true)
    mocks.tasksSettings = {}
    mocks.blockChangedCallbacks.length = 0
    mocks.configChangedCallbacks.length = 0
    clearTaskPageRoute()
    resetTaskHubState()
  })

  afterEach(() => {
    cleanup()
  })

  it('renders the bookmark button in the header', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    expect(screen.getByTestId('tasks-hub-bookmark')).toBeInTheDocument()
  })

  it('hydrates only the 3 system views (legacy kanban boards are migrated Go-side, not forward-read)', async () => {
    // loadSavedViews() must not forward-read silt-kanban.boards[]: a
    // user-deleted view would resurrect from the uncleared legacy key on
    // every load. The Go migrator (#431) writes legacy boards into
    // saved_views[] once at startup; this test locks that boundary. The
    // settings slice is empty here, so only SYSTEM_VIEWS load.
    mocks.tasksSettings = {}

    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const views = getTaskHubState().savedViews
    const ids = views.map((v) => v.id)
    // Only the 3 system views — legacy board b1 is NOT forward-read.
    expect(views).toHaveLength(3)
    expect(ids).toContain('sys-today-board')
    expect(ids).toContain('sys-by-owner')
    expect(ids).toContain('sys-week-calendar')
    expect(ids).not.toContain('b1')
    // System views carry the read-only marker.
    expect(views.find((v) => v.id === 'sys-today-board')?.system).toBe(true)
  })

  it('clicking the bookmark with no active view opens the save composer', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    expect(screen.queryByTestId('tasks-hub-save-view-popover')).toBeNull()
    await fireEvent.click(screen.getByTestId('tasks-hub-bookmark'))
    await flush()

    expect(
      screen.getByTestId('tasks-hub-save-view-popover')
    ).toBeInTheDocument()
    expect(screen.getByTestId('tasks-hub-save-view-name')).toBeInTheDocument()
  })

  it('submitting the composer saves a view + persists to saved_views', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    await fireEvent.click(screen.getByTestId('tasks-hub-bookmark'))
    await flush()

    const input = screen.getByTestId('tasks-hub-save-view-name')
    await fireEvent.input(input, { target: { value: 'Sprint 15' } })
    await flush()

    await fireEvent.click(screen.getByTestId('tasks-hub-save-view-commit'))
    await flush()

    // The view was added to the in-memory list and marked active.
    const s = getTaskHubState()
    const saved = s.savedViews.find((v) => v.name === 'Sprint 15')
    expect(saved).toBeDefined()
    expect(saved?.system).toBeFalsy()
    expect(s.activeSavedViewId).toBe(saved?.id)
    expect(s.savedViewsDirty).toBe(false)

    // persistSavedViews wrote through to updatePluginSetting under the
    // silt-tasks / saved_views key. System views are stripped before write
    // (the persisted array contains only the user view, no `sys-` ids).
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'saved_views',
      expect.arrayContaining([expect.objectContaining({ name: 'Sprint 15' })])
    )
    const [, written] = mocks.updatePluginSetting.mock.calls.find(
      (c) => c[0] === 'saved_views'
    )!
    expect(
      (written as Array<{ id?: string }>).find((v) => v.id?.startsWith('sys-'))
    ).toBeUndefined()

    // The aria-live region announces the save.
    expect(
      screen.getByTestId('tasks-hub-saved-view-live').textContent
    ).toContain('Sprint 15')
    // The active view name shows next to the title.
    expect(screen.getByTestId('tasks-hub-active-view').textContent).toContain(
      'Sprint 15'
    )
  })

  it('shows the modified (dirty) indicator when the user diverges from the active view', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    // Save a view (becomes active + clean).
    await fireEvent.click(screen.getByTestId('tasks-hub-bookmark'))
    await flush()
    await fireEvent.input(screen.getByTestId('tasks-hub-save-view-name'), {
      target: { value: 'My View' }
    })
    await fireEvent.click(screen.getByTestId('tasks-hub-save-view-commit'))
    await flush()

    // Clean state — no "(modified)" suffix and no dirty accent dot.
    expect(screen.queryByText(/\(modified\)/)).toBeNull()
    expect(screen.queryByTestId('tasks-hub-dirty-dot')).toBeNull()

    // Switch display mode — diverges from the saved snapshot.
    await fireEvent.click(screen.getByRole('radio', { name: /Board mode/i }))
    await flush()

    expect(getTaskHubState().savedViewsDirty).toBe(true)
    // The active-view label surfaces the divergence.
    const label = screen.getByTestId('tasks-hub-active-view')
    expect(label.textContent).toContain('modified')
    // #460: the dirty signal is now an accent dot on the bookmark button
    // (prominent) instead of opacity-60 dimming on the label.
    expect(screen.getByTestId('tasks-hub-dirty-dot')).toBeTruthy()
  })

  it('clears a search-only saved view without marking it modified (#1071)', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    await fireEvent.click(screen.getByTestId('tasks-hub-bookmark'))
    await flush()
    await fireEvent.input(screen.getByTestId('tasks-hub-save-view-name'), {
      target: { value: 'My View' }
    })
    await fireEvent.click(screen.getByTestId('tasks-hub-save-view-commit'))
    await flush()

    const input = screen.getByTestId('tasks-hub-text-filter')
    await fireEvent.input(input, { target: { value: 'postgres' } })
    await vi.waitFor(
      () => expect(getTaskHubState().filters.text).toBe('postgres'),
      { timeout: 1000 }
    )
    // Typing narrows without diverging from the saved view.
    expect(getTaskHubState().savedViewsDirty).toBe(false)

    await fireEvent.click(screen.getByRole('button', { name: /Clear all/ }))
    await flush()

    expect(getTaskHubState().filters.text).toBe('')
    expect(getTaskHubState().savedViewsDirty).toBe(false)
    expect(screen.queryByText(/\(modified\)/)).toBeNull()
  })

  it('keeps the transient search out of the saved_views snapshot (#1071)', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    const input = screen.getByTestId('tasks-hub-text-filter')
    await fireEvent.input(input, { target: { value: 'postgres' } })
    await vi.waitFor(
      () => expect(getTaskHubState().filters.text).toBe('postgres'),
      { timeout: 1000 }
    )

    await fireEvent.click(screen.getByTestId('tasks-hub-bookmark'))
    await flush()
    await fireEvent.input(screen.getByTestId('tasks-hub-save-view-name'), {
      target: { value: 'Searchless' }
    })
    await fireEvent.click(screen.getByTestId('tasks-hub-save-view-commit'))
    await flush()

    const written = mocks.updatePluginSetting.mock.calls.find(
      (c) => c[0] === 'saved_views'
    )?.[1] as Array<{ name?: string; filters?: Record<string, unknown> }>
    const saved = written.find((v) => v.name === 'Searchless')
    expect(saved?.filters).toBeDefined()
    expect(saved?.filters).not.toHaveProperty('text')
  })

  it('offers Update / Save-as-new when the active view is dirty', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    // Save a view.
    await fireEvent.click(screen.getByTestId('tasks-hub-bookmark'))
    await flush()
    await fireEvent.input(screen.getByTestId('tasks-hub-save-view-name'), {
      target: { value: 'My View' }
    })
    await fireEvent.click(screen.getByTestId('tasks-hub-save-view-commit'))
    await flush()

    // Diverge.
    await fireEvent.click(screen.getByRole('radio', { name: /Board mode/i }))
    await flush()

    // Re-open the bookmark — should be in "menu" mode with Update + Save-as-new.
    await fireEvent.click(screen.getByTestId('tasks-hub-bookmark'))
    await flush()

    expect(screen.getByTestId('tasks-hub-view-menu')).toBeInTheDocument()
    expect(screen.getByTestId('tasks-hub-update-view')).toBeInTheDocument()
    expect(screen.getByTestId('tasks-hub-save-as-new')).toBeInTheDocument()
  })

  it('clicking Update overwrites the active view dimensions + persists', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    // Save a view in default list mode.
    await fireEvent.click(screen.getByTestId('tasks-hub-bookmark'))
    await flush()
    await fireEvent.input(screen.getByTestId('tasks-hub-save-view-name'), {
      target: { value: 'My View' }
    })
    await fireEvent.click(screen.getByTestId('tasks-hub-save-view-commit'))
    await flush()
    const savedId = getTaskHubState().activeSavedViewId

    // Switch to Board, then Update.
    await fireEvent.click(screen.getByRole('radio', { name: /Board mode/i }))
    await flush()
    await fireEvent.click(screen.getByTestId('tasks-hub-bookmark'))
    await flush()
    await fireEvent.click(screen.getByTestId('tasks-hub-update-view'))
    await flush()

    const s = getTaskHubState()
    expect(s.savedViewsDirty).toBe(false)
    expect(s.activeSavedViewId).toBe(savedId)
    // The view's snapshot now carries the Board displayMode.
    const updated = s.savedViews.find((v) => v.id === savedId)
    expect(updated?.displayMode).toBe('board')
  })

  it('Find task from the palette lands focus on the hub text filter', async () => {
    // Pins the tick-order handoff: the palette closes (restoring its prior
    // focus) before the scheduled focus() runs, so an ordering change cannot
    // silently no-op the command's visible effect.
    settings.config = {
      hotkeys: { tasks_command_palette: 'Ctrl+K' }
    } as never
    try {
      render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
      await flush()

      window.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'k', ctrlKey: true })
      )
      await flush()
      await fireEvent.click(screen.getByText('Find task…'))
      await flush()

      expect(document.activeElement).toBe(screen.getByLabelText('Find tasks'))
    } finally {
      settings.config = null
    }
  })

  it('Escape closes the open popover (mirrors FilterBar escape pattern)', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    await fireEvent.click(screen.getByTestId('tasks-hub-bookmark'))
    await flush()
    expect(
      screen.getByTestId('tasks-hub-save-view-popover')
    ).toBeInTheDocument()

    // The escape handler attaches to window via $effect (same pattern
    // the Ctrl+Shift+V mode-cycle handler uses). Dispatching a window
    // keydown is the proven delivery path.
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flush()

    // The popover uses transition:fly (defers DOM removal until the fly-out
    // completes), so assert on the underlying state via the bookmark's
    // data-popover-state attribute (which updates synchronously with
    // closePopover) instead of waiting for the transition timer.
    const bm = screen.getByTestId('tasks-hub-bookmark')
    expect(bm.getAttribute('data-popover-state')).toBe('closed')
    expect(document.activeElement).toBe(bm)
  })

  it('delete uses the in-app confirm modal, not window.confirm (#470)', async () => {
    // jsdom's window.confirm returns false by default — if commitDelete still
    // used it, the deletion would silently no-op. We assert the modal opens
    // instead and the choice flows through it.
    const confirmSpy = vi.spyOn(window, 'confirm')
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    // Save a view (becomes active + clean → menu shows rename/delete).
    await fireEvent.click(screen.getByTestId('tasks-hub-bookmark'))
    await flush()
    await fireEvent.input(screen.getByTestId('tasks-hub-save-view-name'), {
      target: { value: 'Doomed' }
    })
    await fireEvent.click(screen.getByTestId('tasks-hub-save-view-commit'))
    await flush()

    // Open the menu + click Delete.
    await fireEvent.click(screen.getByTestId('tasks-hub-bookmark'))
    await flush()
    await fireEvent.click(screen.getByTestId('tasks-hub-delete-view'))
    await flush()

    // The in-app modal opens (not window.confirm).
    expect(confirmSpy).not.toHaveBeenCalled()
    expect(screen.getByTestId('tasks-hub-delete-confirm')).toBeInTheDocument()
    const modal = screen.getByTestId('tasks-hub-delete-confirm')
    expect(modal.textContent).toContain('Doomed')
  })

  it('confirming the delete modal removes the view; cancelling keeps it (#470)', async () => {
    render(TasksHub, { ctx: makeCtx(), manifest: MANIFEST })
    await flush()

    await fireEvent.click(screen.getByTestId('tasks-hub-bookmark'))
    await flush()
    await fireEvent.input(screen.getByTestId('tasks-hub-save-view-name'), {
      target: { value: 'Keep Me' }
    })
    await fireEvent.click(screen.getByTestId('tasks-hub-save-view-commit'))
    await flush()
    const savedId = getTaskHubState().activeSavedViewId
    expect(savedId).toBeTruthy()

    // Open delete modal.
    await fireEvent.click(screen.getByTestId('tasks-hub-bookmark'))
    await flush()
    await fireEvent.click(screen.getByTestId('tasks-hub-delete-view'))
    await flush()

    // Cancel — view survives.
    await fireEvent.click(screen.getByTestId('tasks-hub-delete-confirm-cancel'))
    await flush()
    expect(
      getTaskHubState().savedViews.find((v) => v.id === savedId)
    ).toBeDefined()
    expect(screen.queryByTestId('tasks-hub-delete-confirm')).toBeNull()

    // Re-open + confirm — view is gone.
    await fireEvent.click(screen.getByTestId('tasks-hub-bookmark'))
    await flush()
    await fireEvent.click(screen.getByTestId('tasks-hub-delete-view'))
    await flush()
    await fireEvent.click(
      screen.getByTestId('tasks-hub-delete-confirm-confirm')
    )
    await flush()
    expect(
      getTaskHubState().savedViews.find((v) => v.id === savedId)
    ).toBeUndefined()
    expect(getTaskHubState().activeSavedViewId).toBe('')
  })
})

// --- Session undo (#1011) -------------------------------------------------

function journalState(overrides: Record<string, unknown> = {}) {
  return {
    canUndo: true,
    remaining: 3,
    changedFields: ['status'],
    blockIds: ['t1'],
    notebook: 'Work',
    section: 'Journal',
    page: 'Daily',
    source: 'vault',
    ...overrides
  }
}

function undoResult(overrides: Record<string, unknown> = {}) {
  return {
    undone: true,
    skipped: false,
    blockIds: ['t1'],
    skippedBlockIds: [],
    notebook: 'Work',
    section: 'Journal',
    page: 'Daily',
    removedSpawnedId: '',
    changedFields: ['status'],
    remaining: 0,
    ...overrides
  }
}

function boardRow(overrides: Record<string, unknown> = {}) {
  return {
    id: 't1',
    source: 'vault',
    notebook: 'Work',
    section: 'Journal',
    page: 'Daily',
    file_date: '2026-07-01',
    clean_content: 'Task',
    status: 'TODO',
    owner: '',
    start_date: '',
    due_date: '',
    priority: 3,
    pinned: 0,
    progress: 0,
    recurrence: '',
    comments_count: 0,
    links_count: 0,
    created_at: '',
    completed_at: '',
    manual_order: 0,
    modified_at: '',
    estimate_minutes: null,
    subtask_total: 0,
    subtask_done: 0,
    tags: '',
    is_blocked: 0,
    ...overrides
  }
}

function undoCtx(overrides: Partial<PluginContext> = {}): PluginContext {
  return makeCtx({
    updateBlockState: mocks.updateBlockState,
    getTaskUndoState: mocks.getTaskUndoState,
    undoLastTaskMutation: mocks.undoLastTaskMutation,
    ...overrides
  })
}

describe('Tasks hub undo (#1011)', () => {
  beforeEach(() => {
    mocks.sqliteQuery.mockReset().mockResolvedValue({
      rows: [],
      truncated: false
    })
    mocks.updatePluginSetting.mockReset().mockResolvedValue(true)
    mocks.updateBlockState
      .mockReset()
      .mockResolvedValue({ ok: true, spawnedId: '' })
    mocks.getTaskUndoState.mockReset().mockResolvedValue(journalState())
    mocks.undoLastTaskMutation.mockReset().mockResolvedValue(undoResult())
    mocks.tasksSettings = {}
    mocks.blockChangedCallbacks.length = 0
    mocks.configChangedCallbacks.length = 0
    clearTaskPageRoute()
    resetTaskHubState()
  })

  afterEach(() => {
    cleanup()
  })

  it('keeps the undo affordance visible but disabled with an empty journal', async () => {
    mocks.getTaskUndoState
      .mockReset()
      .mockResolvedValue(
        journalState({ canUndo: false, remaining: 0, changedFields: [] })
      )

    render(TasksHub, { ctx: undoCtx(), manifest: MANIFEST })
    await flush()

    const button = screen.getByTestId('tasks-undo')
    expect(button).toBeInTheDocument()
    expect(button).toBeDisabled()
    expect(button).toHaveTextContent('Undo')
    expect(button).toHaveAttribute('aria-keyshortcuts', 'Control+Z')
    expect(button).toHaveAttribute('aria-label', 'Nothing to undo')
  })

  it('names the journal head and the remaining depth in the label', async () => {
    mocks.getTaskUndoState.mockResolvedValue(journalState({ remaining: 3 }))

    render(TasksHub, { ctx: undoCtx(), manifest: MANIFEST })
    await flush()

    const button = screen.getByTestId('tasks-undo')
    expect(button).not.toBeDisabled()
    expect(button).toHaveAttribute(
      'aria-label',
      'Undo status change in Work › Journal › Daily — 3 changes left'
    )
    // The visible chip is decorative (the label already says it), but it keeps
    // the leftover depth visible for sighted users after a cross-file undo.
    expect(button).toHaveTextContent('3')
  })

  it('scopes Ctrl+Z to hub focus and leaves the editor and editable fields alone', async () => {
    render(TasksHub, { ctx: undoCtx(), manifest: MANIFEST })
    await flush()

    // The editor is a sibling subtree. Its Ctrl+Z must never reach the hub,
    // and the hub must not consume it before the editor keymap can run.
    const editor = document.createElement('div')
    editor.className = 'ProseMirror'
    editor.setAttribute('contenteditable', 'true')
    document.body.appendChild(editor)
    const editorUndo = new KeyboardEvent('keydown', {
      key: 'z',
      ctrlKey: true,
      bubbles: true,
      cancelable: true
    })
    editor.dispatchEvent(editorUndo)
    expect(editorUndo.defaultPrevented).toBe(false)
    expect(mocks.undoLastTaskMutation).not.toHaveBeenCalled()
    editor.remove()

    // Editable hub fields keep their native undo.
    const fieldUndo = new KeyboardEvent('keydown', {
      key: 'z',
      ctrlKey: true,
      bubbles: true,
      cancelable: true
    })
    screen.getByTestId('tasks-hub-text-filter').dispatchEvent(fieldUndo)
    expect(fieldUndo.defaultPrevented).toBe(false)
    expect(mocks.undoLastTaskMutation).not.toHaveBeenCalled()

    // Nested dialogs own their own context (the drawer opts back in).
    await fireEvent.click(screen.getByTestId('tasks-hub-bookmark'))
    await flush()
    const dialogUndo = new KeyboardEvent('keydown', {
      key: 'z',
      ctrlKey: true,
      bubbles: true,
      cancelable: true
    })
    screen.getByTestId('tasks-hub-save-view-popover').dispatchEvent(dialogUndo)
    expect(dialogUndo.defaultPrevented).toBe(false)
    expect(mocks.undoLastTaskMutation).not.toHaveBeenCalled()
    await fireEvent.keyDown(screen.getByTestId('tasks-hub-save-view-popover'), {
      key: 'Escape'
    })
    await flush()

    // Ctrl+Shift+Z is redo, which the session journal does not offer.
    const redo = new KeyboardEvent('keydown', {
      key: 'z',
      ctrlKey: true,
      shiftKey: true,
      bubbles: true,
      cancelable: true
    })
    screen.getByTestId('tasks-hub-mode-switcher').dispatchEvent(redo)
    expect(redo.defaultPrevented).toBe(false)
    expect(mocks.undoLastTaskMutation).not.toHaveBeenCalled()

    // A non-editable hub surface is the valid scope.
    const hubUndo = new KeyboardEvent('keydown', {
      key: 'z',
      ctrlKey: true,
      bubbles: true,
      cancelable: true
    })
    screen.getByTestId('tasks-hub-mode-switcher').dispatchEvent(hubUndo)
    await flush()
    expect(hubUndo.defaultPrevented).toBe(true)
    expect(mocks.undoLastTaskMutation).toHaveBeenCalledTimes(1)
    expect(getTasksUndoView().announcement).toBe(
      'Undid status change in Work › Journal › Daily. No more changes to undo.'
    )
  })

  it('opts Ctrl+Z back in inside the task drawer but not its popovers', async () => {
    const rows = [
      boardRow({
        id: 't1',
        line_number: 1,
        clean_content: 'Drawer undo',
        due_date: ''
      })
    ]
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes('SELECT DISTINCT owner'))
        return { rows: [], truncated: false }
      if (sql.includes('SELECT DISTINCT level_0'))
        return { rows: [], truncated: false }
      if (/(?:WHERE|AND) t\.status != 'DONE'/.test(sql))
        return { rows, truncated: false }
      return { rows: [], truncated: false }
    })

    render(TasksHub, { ctx: undoCtx(), manifest: MANIFEST })
    await flush()

    await fireEvent.click(
      document.querySelector(
        '[data-block-id="t1"] button[aria-label^="Edit metadata for"]'
      )!
    )
    await flush()

    // The overlay drawer is role=dialog, so its opt-in attribute is the only
    // thing re-admitting hub undo inside it.
    const drawer = screen.getByTestId('task-edit-drawer')
    expect(drawer).toHaveAttribute('role', 'dialog')
    expect(drawer).toHaveAttribute('data-tasks-undo-scope')

    // Popovers portal out of the hub subtree, so Ctrl+Z inside one stays the
    // popover's own — never the hub journal.
    await fireEvent.click(document.getElementById('task-due-date-trigger')!)
    await flush()
    const popoverUndo = new KeyboardEvent('keydown', {
      key: 'z',
      ctrlKey: true,
      bubbles: true,
      cancelable: true
    })
    screen.getByRole('button', { name: /^Tomorrow/ }).dispatchEvent(popoverUndo)
    expect(popoverUndo.defaultPrevented).toBe(false)
    expect(mocks.undoLastTaskMutation).not.toHaveBeenCalled()

    // A non-editable control on the opted-in drawer still reaches the journal.
    const drawerUndo = new KeyboardEvent('keydown', {
      key: 'z',
      ctrlKey: true,
      bubbles: true,
      cancelable: true
    })
    screen.getByTestId('tasks-undo-drawer').dispatchEvent(drawerUndo)
    await flush()
    expect(drawerUndo.defaultPrevented).toBe(true)
    expect(mocks.undoLastTaskMutation).toHaveBeenCalledTimes(1)
  })

  it('leaves Ctrl+Z to the blocked-DONE alertdialog', async () => {
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes('SELECT DISTINCT owner'))
        return { rows: [], truncated: false }
      if (sql.includes('SELECT DISTINCT level_0'))
        return { rows: [], truncated: false }
      if (/(?:WHERE|AND) t\.status != 'DONE'/.test(sql))
        return { rows: [boardRow({ is_blocked: 1 })], truncated: false }
      return { rows: [], truncated: false }
    })
    const getTaskBlockers = vi
      .fn()
      .mockResolvedValue([{ id: 'dep-1', clean_content: 'Prereq' }])

    render(TasksHub, { ctx: undoCtx({ getTaskBlockers }), manifest: MANIFEST })
    await flush()

    await fireEvent.click(
      document.querySelector(
        '[data-block-id="t1"] button[role="checkbox"]'
      ) as HTMLElement
    )
    await flush()

    // The modal's confirm button holds focus while it is open. role=alertdialog
    // is a distinct attribute value from role=dialog, so the overlay exclusion
    // has to name it explicitly.
    const confirm = screen.getByText('Complete anyway')
    expect(confirm.closest('[role="alertdialog"]')).not.toBeNull()
    const modalUndo = new KeyboardEvent('keydown', {
      key: 'z',
      ctrlKey: true,
      bubbles: true,
      cancelable: true
    })
    confirm.dispatchEvent(modalUndo)
    await flush()

    expect(modalUndo.defaultPrevented).toBe(false)
    expect(mocks.undoLastTaskMutation).not.toHaveBeenCalled()
  })

  it('restores board focus after confirming a blocked DONE move', async () => {
    mocks.tasksSettings = {
      default_display_mode: 'board',
      default_group_by: 'status'
    }
    const rows = [
      boardRow({
        id: 't1',
        clean_content: 'Blocked move',
        status: 'DOING',
        is_blocked: 1
      })
    ]
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes('SELECT DISTINCT owner'))
        return { rows: [], truncated: false }
      if (sql.includes('SELECT DISTINCT level_0'))
        return { rows: [], truncated: false }
      return { rows, truncated: false }
    })
    const getTaskBlockers = vi
      .fn()
      .mockResolvedValue([{ id: 'dep-1', clean_content: 'Prereq' }])

    render(TasksHub, { ctx: undoCtx({ getTaskBlockers }), manifest: MANIFEST })
    await flush()

    const card = screen
      .getByRole('group', { name: 'In Progress' })
      .querySelector<HTMLElement>('[data-card="t1"]')!
    card.focus()
    await fireEvent.keyDown(card, { key: 'ArrowRight' })
    await flush()

    // The guard holds focus on its confirm button while open.
    const confirm = screen.getByText('Complete anyway')
    await fireEvent.click(confirm)
    await flush()
    await flush()

    // The optimistic relocation replaced the card node and the dialog restores
    // focus on unmount, so without an explicit hand-back focus lands on <body>
    // and the hub's Ctrl+Z scope is gone until the user clicks back in.
    const doneCard = screen
      .getByRole('group', { name: 'Done' })
      .querySelector<HTMLElement>('[data-card="t1"]')
    expect(doneCard).not.toBeNull()
    expect(document.activeElement).toBe(doneCard)

    const undoEvent = new KeyboardEvent('keydown', {
      key: 'z',
      ctrlKey: true,
      bubbles: true,
      cancelable: true
    })
    doneCard!.dispatchEvent(undoEvent)
    await flush()
    expect(undoEvent.defaultPrevented).toBe(true)
    expect(mocks.undoLastTaskMutation).toHaveBeenCalledTimes(1)
  })

  it('renders the undo failure banner and dismisses it', async () => {
    mocks.undoLastTaskMutation.mockRejectedValue(new Error('focus lock held'))

    render(TasksHub, { ctx: undoCtx(), manifest: MANIFEST })
    await flush()

    await fireEvent.click(screen.getByTestId('tasks-undo'))
    await flush()

    const banner = screen.getByTestId('tasks-undo-error')
    expect(banner).toHaveAttribute('role', 'alert')
    expect(banner).toHaveTextContent("Couldn't undo: focus lock held")

    await fireEvent.click(screen.getByTestId('tasks-undo-error-dismiss'))
    await flush()
    expect(screen.queryByTestId('tasks-undo-error')).toBeNull()
  })

  it('undoes a board status change and restores the card column', async () => {
    mocks.tasksSettings = {
      default_display_mode: 'board',
      default_group_by: 'status'
    }
    const rows = [
      boardRow({ id: 't1', clean_content: 'Ship it', status: 'TODO' })
    ]
    mocks.sqliteQuery.mockImplementation(async (sql: string) => {
      if (sql.includes('SELECT DISTINCT owner'))
        return { rows: [], truncated: false }
      if (sql.includes('SELECT DISTINCT level_0'))
        return { rows: [], truncated: false }
      return { rows, truncated: false }
    })

    let journal = journalState({
      canUndo: false,
      remaining: 0,
      changedFields: []
    })
    mocks.getTaskUndoState.mockImplementation(async () => ({ ...journal }))

    let resolveMove: (value: unknown) => void = () => {}
    mocks.updateBlockState.mockImplementationOnce(() => {
      rows[0].status = 'DOING'
      return new Promise((resolve) => {
        resolveMove = resolve
      })
    })
    mocks.undoLastTaskMutation.mockImplementation(async () => {
      rows[0].status = 'TODO'
      journal = journalState({
        canUndo: false,
        remaining: 0,
        changedFields: []
      })
      return undoResult({ remaining: 0 })
    })

    render(TasksHub, { ctx: undoCtx(), manifest: MANIFEST })
    await flush()

    const todoCard = screen
      .getByRole('group', { name: 'To Do' })
      .querySelector<HTMLElement>('[data-card]')!
    const doingCol = screen.getByRole('group', { name: 'In Progress' })
    await fireEvent.dragStart(todoCard)
    await fireEvent.drop(doingCol)
    await flush()

    // Optimistic placement already landed.
    expect(
      doingCol.querySelector<HTMLElement>('[data-card]')?.textContent
    ).toContain('Ship it')

    // While the status write is in flight, undo is disabled and says why.
    const undoButton = screen.getByTestId('tasks-undo')
    expect(undoButton).toBeDisabled()
    expect(undoButton).toHaveAttribute(
      'aria-label',
      'Undo unavailable while a change is saving'
    )

    journal = journalState({ remaining: 1, changedFields: ['status'] })
    resolveMove({ ok: true, spawnedId: '' })
    await flush()
    await flush()

    expect(undoButton).not.toBeDisabled()
    expect(undoButton).toHaveAttribute(
      'aria-label',
      'Undo status change in Work › Journal › Daily'
    )

    await fireEvent.click(undoButton)
    await flush()
    expect(mocks.undoLastTaskMutation).toHaveBeenCalledTimes(1)

    // The backend undo writes the block; its event drives the existing reload.
    mocks.blockChangedCallbacks.forEach((cb) => cb())
    await new Promise((resolve) => setTimeout(resolve, 200))
    await flush()

    expect(screen.getByRole('group', { name: 'To Do' }).textContent).toContain(
      'Ship it'
    )
    expect(screen.getByTestId('tasks-undo-live')).toHaveTextContent(
      'Undid status change in Work › Journal › Daily. No more changes to undo.'
    )
  })
})
