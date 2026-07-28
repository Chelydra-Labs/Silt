import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { EventName } from '../generated/enums'

// Events.On subscriptions are captured so a test can (a) assert the event names
// attach() registered, (b) invoke a handler directly to simulate a live emit,
// and (c) verify dispose() called each captured off function. The three IPC
// calls the controller makes (MarkFrontendReady, GetStartupEvents drain,
// ResolveQuarantinedLinks) are stubbed — never real IPC.
const mocks = vi.hoisted(() => ({
  eventsOn: vi.fn(),
  MarkFrontendReady: vi.fn().mockResolvedValue(undefined),
  GetStartupEvents: vi.fn().mockResolvedValue([]),
  ResolveQuarantinedLinks: vi.fn().mockResolvedValue([])
}))

vi.mock('@wailsio/runtime', () => ({
  Events: { On: mocks.eventsOn },
  Call: { ByID: vi.fn(), ByName: vi.fn() },
  CancellablePromise: class {},
  Create: {
    Nullable: (fn: unknown) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    MarkFrontendReady: mocks.MarkFrontendReady,
    GetStartupEvents: mocks.GetStartupEvents,
    ResolveQuarantinedLinks: mocks.ResolveQuarantinedLinks
  })
)

// Avoid pulling the whole EditorUtilityBar component; only its event constant
// is needed by the controller.
vi.mock('../components/editor/EditorUtilityBar.svelte', () => ({
  OPEN_TASKS_FOR_PAGE_EVENT: 'silt:open-tasks-for-page'
}))

import {
  createStartupEvents,
  type StartupEventsDeps
} from './useStartupEvents.svelte'

interface HandlerBus {
  handlers: Map<string, (...args: any[]) => void>
  offSpies: ReturnType<typeof vi.fn>[]
}

function makeBus(): HandlerBus {
  return { handlers: new Map(), offSpies: [] }
}

function makeDeps(): {
  deps: StartupEventsDeps
  settingsDialogs: any
  tabManager: any
} {
  const nav = { notebook: 'Work', section: 'Inbox', page: 'Alpha' }
  const settingsDialogs: any = {
    openSettingsMismatch: vi.fn(),
    openGrantsMigration: vi.fn(),
    setQuarantinedLinks: vi.fn()
  }
  const tabManager: any = {
    initBaseline: vi.fn(),
    handleConfigChangedTabRehydrate: vi.fn(),
    resetTabs: vi.fn(),
    invalidateRecentPages: vi.fn(),
    pageRenamed: vi.fn(),
    openPage: vi.fn()
  }
  const deps: StartupEventsDeps = {
    getActiveNotebook: () => nav.notebook,
    getActiveSection: () => nav.section,
    getActivePage: () => nav.page,
    setActiveNotebook: (nb: string) => {
      nav.notebook = nb
    },
    setActiveSection: (sec: string) => {
      nav.section = sec
    },
    setActivePage: vi.fn((pg: string) => {
      nav.page = pg
    }),
    setActiveView: vi.fn(),
    getSettings: () =>
      ({ plugins: { disabled: [] }, ui: { open_tabs: [] } }) as any,
    setSettingsSection: vi.fn(),
    setShowSearch: vi.fn(),
    setShowQuickAdd: vi.fn(),
    getShowTemplatePicker: () => false,
    setShowTemplatePicker: vi.fn(),
    setTemplatePickerMode: vi.fn(),
    setSelectedTag: vi.fn(),
    getSidebarCollapsed: () => false,
    setSidebarCollapsed: vi.fn(),
    setSearchTargetHeading: vi.fn(),
    setSearchTargetKey: vi.fn(),
    getNavigationCatalog: () => [],
    settingsDialogs,
    tabManager,
    openSettings: vi.fn(),
    openTasksView: vi.fn(),
    handleSwitchVault: vi.fn().mockResolvedValue(undefined),
    handleMenuSave: vi.fn().mockResolvedValue(undefined),
    handleSearchJump: vi.fn()
  }
  return { deps, settingsDialogs, tabManager }
}

// Wire the Events.On mock to a fresh capture bus for each test.
function wireEventsOn(bus: HandlerBus): void {
  mocks.eventsOn.mockImplementation(
    (name: string, handler: (...args: any[]) => void) => {
      bus.handlers.set(name, handler)
      const off = vi.fn()
      bus.offSpies.push(off)
      return off
    }
  )
}

describe('useStartupEvents (#768)', () => {
  let controller: ReturnType<typeof createStartupEvents>

  beforeEach(() => {
    vi.clearAllMocks()
    mocks.GetStartupEvents.mockResolvedValue([])
  })

  afterEach(() => {
    controller?.dispose()
  })

  it('attach() subscribes to the config/plugin/vault/dialog/menu events', () => {
    const bus = makeBus()
    wireEventsOn(bus)
    const { deps } = makeDeps()
    controller = createStartupEvents(deps)
    controller.attach()

    const names = Array.from(bus.handlers.keys())
    expect(names).toContain(EventName.EventConfigChanged)
    expect(names).toContain(EventName.EventPluginsChanged)
    expect(names).toContain(EventName.EventVaultClosing)
    expect(names).toContain(EventName.EventVaultMoved)
    expect(names).toContain(EventName.EventSettingsFingerprintMismatch)
    expect(names).toContain(EventName.EventMenuSave)
    expect(names).toContain(EventName.EventPageLinksRewritten)
    // The native menu:* events.
    expect(names).toContain(EventName.EventMenuNewPage)
    expect(names).toContain(EventName.EventMenuSettings)
  })

  it('a live menu:save emit calls deps.handleMenuSave', () => {
    const bus = makeBus()
    wireEventsOn(bus)
    const { deps } = makeDeps()
    controller = createStartupEvents(deps)
    controller.attach()

    bus.handlers.get(EventName.EventMenuSave)!({ data: undefined } as any)
    expect(deps.handleMenuSave).toHaveBeenCalledOnce()
  })

  it('replays a queued settings-mismatch event through the same handler', async () => {
    const bus = makeBus()
    wireEventsOn(bus)
    mocks.GetStartupEvents.mockResolvedValue([
      { Name: EventName.EventSettingsFingerprintMismatch, Payload: undefined }
    ])
    const { deps, settingsDialogs } = makeDeps()
    controller = createStartupEvents(deps)
    controller.attach()

    // attach() drains the backlog asynchronously (MarkFrontendReady →
    // GetStartupEvents → dispatchStartupEvent).
    await vi.waitFor(() => {
      expect(settingsDialogs.openSettingsMismatch).toHaveBeenCalledOnce()
    })
    expect(mocks.MarkFrontendReady).toHaveBeenCalledOnce()
    expect(mocks.GetStartupEvents).toHaveBeenCalledOnce()
  })

  it('a page-renamed window event forwards to tabManager + mirrors nav', () => {
    const bus = makeBus()
    wireEventsOn(bus)
    const { deps, tabManager } = makeDeps()
    controller = createStartupEvents(deps)
    controller.attach()

    window.dispatchEvent(
      new CustomEvent('page-renamed', {
        detail: {
          notebook: 'Work',
          section: 'Inbox',
          oldName: 'Alpha',
          newName: 'Alpha II'
        }
      })
    )
    expect(tabManager.pageRenamed).toHaveBeenCalledWith(
      expect.objectContaining({ oldName: 'Alpha', newName: 'Alpha II' })
    )
    // The active nav triple pointed at the renamed page → mirrored.
    expect(deps.setActivePage).toHaveBeenCalledWith('Alpha II')
  })

  it('dispose() invokes every captured off function + drops window listeners', () => {
    const bus = makeBus()
    wireEventsOn(bus)
    const { deps, tabManager } = makeDeps()
    controller = createStartupEvents(deps)
    controller.attach()
    const offCount = bus.offSpies.length
    expect(offCount).toBeGreaterThan(0)

    controller.dispose()
    // Every Events.On cancel function was called.
    expect(bus.offSpies.every((off) => off.mock.calls.length === 1)).toBe(true)
    // Recents cache dropped on teardown.
    expect(tabManager.invalidateRecentPages).toHaveBeenCalled()

    // A subsequent window event no longer reaches the controller.
    window.dispatchEvent(
      new CustomEvent('page-renamed', {
        detail: {
          notebook: 'Work',
          section: 'Inbox',
          oldName: 'Alpha',
          newName: 'Nope'
        }
      })
    )
    expect(tabManager.pageRenamed).toHaveBeenCalledTimes(0)
  })
})
