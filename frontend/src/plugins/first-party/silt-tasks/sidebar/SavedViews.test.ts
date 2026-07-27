import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

// devModeInspect (imported by SavedViews) pulls in the Wails bindings +
// settings store; mock both so the component compiles under jsdom.
const mocks = vi.hoisted(() => ({
  updatePluginSetting: vi.fn().mockResolvedValue(true),
  openDevTools: vi.fn().mockResolvedValue(undefined),
  tasksSettings: {} as Record<string, unknown>
}))

const settingsMock = vi.hoisted(() => ({
  config: null as null | { ui?: { open_devtools_on_startup?: boolean } }
}))

vi.mock('@wailsio/runtime', () => ({
  Events: { On: vi.fn(() => () => {}) },
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

vi.mock('../../../../../bindings/silt/app.js', () => ({
  OpenDevTools: mocks.openDevTools
}))

vi.mock('../../../../settings/store.svelte', () => ({
  settings: settingsMock
}))

import SavedViews from './SavedViews.svelte'
import type { PluginContext } from '../../../sdk'
import { v2CtxStubs } from '../../../test-helpers'
import {
  getTaskHubState,
  resetTaskHubState,
  type SavedView
} from '../state.svelte'
import { SYSTEM_VIEWS } from '../savedViews'
import { initTasksSettings } from '../settings'

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

function makeCtx(overrides: Partial<PluginContext> = {}): PluginContext {
  return {
    ...v2CtxStubs,
    getPluginSettings: vi.fn(() => Promise.resolve(mocks.tasksSettings)),
    updatePluginSetting: mocks.updatePluginSetting,
    ...overrides
  } as PluginContext
}

function seedSavedViews(user: SavedView[] = []): void {
  getTaskHubState().savedViews = [...SYSTEM_VIEWS, ...user]
}

async function flush() {
  await tick()
  await new Promise((r) => setTimeout(r, 0))
}

describe('sidebar/SavedViews (#763)', () => {
  beforeEach(async () => {
    settingsMock.config = null
    mocks.updatePluginSetting.mockReset().mockResolvedValue(true)
    mocks.tasksSettings = {}
    resetTaskHubState()
    await initTasksSettings(makeCtx())
  })
  afterEach(cleanup)

  it('renders seeded user views', async () => {
    const userView: SavedView = {
      id: 'u1',
      name: 'My View',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([userView])
    render(SavedViews)
    await flush()
    expect(screen.getByTestId('view-u1')).toBeInTheDocument()
    // System views render too.
    expect(screen.getByTestId('view-sys-today-board')).toBeInTheDocument()
  })

  it('clicking a saved view calls applySavedView', async () => {
    const fullView: SavedView = {
      id: 'u-match',
      name: 'Match Me',
      displayMode: 'list',
      groupBy: 'owner',
      sort: 'priority',
      scope: 'vault',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] },
      calendarSubMode: 'month',
      columns: [{ name: 'TODO' }, { name: 'DOING' }, { name: 'DONE' }]
    }
    seedSavedViews([fullView])
    render(SavedViews)
    await flush()
    const activateBtn = document.querySelector<HTMLElement>(
      '[data-testid="view-u-match"] button'
    )
    expect(activateBtn).toBeTruthy()
    await fireEvent.click(activateBtn!)
    await flush()
    expect(getTaskHubState().activeSavedViewId).toBe(fullView.id)
  })

  it('delete-confirm flow removes + persists', async () => {
    const userView: SavedView = {
      id: 'u1',
      name: 'My View',
      displayMode: 'list',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    }
    seedSavedViews([userView])
    render(SavedViews)
    await flush()
    await fireEvent.click(screen.getByTestId('manage-view-u1'))
    await flush()
    await fireEvent.click(screen.getByTestId('manage-delete-view'))
    await flush()
    expect(screen.getByTestId('delete-view-confirm')).toBeInTheDocument()
    await fireEvent.click(screen.getByTestId('delete-view-confirm-confirm'))
    await flush()
    expect(mocks.updatePluginSetting).toHaveBeenCalledWith(
      'saved_views',
      expect.not.arrayContaining([expect.objectContaining({ id: 'u1' })])
    )
    expect(screen.queryByTestId('view-u1')).toBeNull()
  })
})
