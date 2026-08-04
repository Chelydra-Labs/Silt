import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { EventName } from '../generated/enums'
import type { SystemConfig } from '../settings/store.svelte'
import type { SettingsDialogsController } from './useSettingsDialogs.svelte'
import type { TabManagerController } from '../lib/tabs/useTabManager.svelte'

// Events.On subscriptions are captured so a test can (a) assert the event names
// attach() registered, (b) invoke a handler directly to simulate a live emit,
// and (c) verify dispose() called each captured off function. The three IPC
// calls the controller makes (MarkFrontendReady, GetStartupEvents drain,
// ResolveQuarantinedLinks) are stubbed — never real IPC.
const mocks = vi.hoisted(() => ({
  eventsOn: vi.fn(),
  MarkFrontendReady: vi.fn().mockResolvedValue(undefined),
  GetStartupEvents: vi.fn().mockResolvedValue([]),
  ResolveQuarantinedLinks: vi.fn().mockResolvedValue([]),
  pushNotification: vi.fn()
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

// pushNotification is imported directly by the controller; stub so incomplete
// navigate-to-block feedback is observable without the real toast store.
vi.mock('../notifications/store.svelte', () => ({
  pushNotification: mocks.pushNotification
}))

// Avoid pulling the whole EditorUtilityBar component; only its event constant
// is needed by the controller.
vi.mock('../components/editor/EditorUtilityBar.svelte', () => ({
  OPEN_TASKS_FOR_PAGE_EVENT: 'silt:open-tasks-for-page'
}))

import {
  createStartupEvents,
  type StartupEventsDeps
} from './useStartupEvents.svelte'

const INCOMPLETE_BLOCK_NAV_TOAST =
  "Couldn't open that block — the link is missing page information."
const INCOMPLETE_PAGE_NAV_TOAST =
  "Couldn't open that page — the link is missing page information."

interface HandlerBus {
  handlers: Map<string, (...args: unknown[]) => void>
  offSpies: ReturnType<typeof vi.fn>[]
}

function makeBus(): HandlerBus {
  return { handlers: new Map(), offSpies: [] }
}

function makeDeps(): {
  deps: StartupEventsDeps
  settingsDialogs: SettingsDialogsController
  tabManager: TabManagerController
} {
  const nav = { notebook: 'Work', section: 'Inbox', page: 'Alpha' }
  const settingsDialogs = {
    openSettingsMismatch: vi.fn(),
    openGrantsMigration: vi.fn(),
    setQuarantinedLinks: vi.fn()
  } as unknown as SettingsDialogsController
  const tabManager = {
    initBaseline: vi.fn(),
    handleConfigChangedTabRehydrate: vi.fn(),
    resetTabs: vi.fn(),
    invalidateRecentPages: vi.fn(),
    pageRenamed: vi.fn(),
    openPage: vi.fn()
  } as unknown as TabManagerController
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
      ({
        plugins: { disabled: [] },
        ui: { open_tabs: [] }
      }) as unknown as SystemConfig,
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
    (name: string, handler: (...args: unknown[]) => void) => {
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

    bus.handlers.get(EventName.EventMenuSave)!({ data: undefined })
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

  it('does not replay queued events when dispose() races the replay drain', async () => {
    const bus = makeBus()
    wireEventsOn(bus)
    // Stall MarkFrontendReady so dispose() can interleave between the awaits
    // in the replay IIFE (MarkFrontendReady → GetStartupEvents → fan-out).
    let resolveReady!: () => void
    mocks.MarkFrontendReady.mockReturnValueOnce(
      new Promise<void>((resolve) => {
        resolveReady = resolve
      })
    )
    mocks.GetStartupEvents.mockResolvedValue([
      { Name: EventName.EventSettingsFingerprintMismatch, Payload: undefined }
    ])
    const { deps, settingsDialogs } = makeDeps()
    controller = createStartupEvents(deps)
    controller.attach()

    // Tear down while MarkFrontendReady is still pending, then let the drain
    // proceed. The disposed guard must short-circuit BEFORE GetStartupEvents,
    // so neither the fetch nor the handler fan-out runs.
    controller.dispose()
    resolveReady()
    // Flush the microtask queue so the stalled IIFE resumes past the await.
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(mocks.GetStartupEvents).not.toHaveBeenCalled()
    expect(settingsDialogs.openSettingsMismatch).not.toHaveBeenCalled()
  })

  it('dispose() is idempotent (safe under double teardown)', () => {
    const bus = makeBus()
    wireEventsOn(bus)
    const { deps, tabManager } = makeDeps()
    controller = createStartupEvents(deps)
    controller.attach()
    controller.dispose()
    // A second dispose (e.g. afterEach after a manual dispose) must be a no-op.
    controller.dispose()
    expect(tabManager.invalidateRecentPages).toHaveBeenCalledOnce()
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

  // #875: AI citation clicks must deliver a full page locator so handleSearchJump
  // can openPage. #877: incomplete detail must not jump and must toast.
  it('navigate-to-block with full locator calls handleSearchJump with notebook/section/page/blockId', () => {
    const bus = makeBus()
    wireEventsOn(bus)
    const { deps } = makeDeps()
    controller = createStartupEvents(deps)
    controller.attach()

    window.dispatchEvent(
      new CustomEvent('navigate-to-block', {
        detail: {
          notebook: 'Work',
          section: 'Notes',
          page: 'Plan',
          blockId: 'block-1'
        }
      })
    )

    expect(deps.handleSearchJump).toHaveBeenCalledTimes(1)
    expect(deps.handleSearchJump).toHaveBeenCalledWith(
      {
        source: undefined,
        notebook: 'Work',
        section: 'Notes',
        page: 'Plan'
      },
      undefined,
      'block-1'
    )
    expect(deps.openTasksView).not.toHaveBeenCalled()
    expect(mocks.pushNotification).not.toHaveBeenCalled()
  })

  it('navigate-to-block with empty section (root page) still jumps', () => {
    const bus = makeBus()
    wireEventsOn(bus)
    const { deps } = makeDeps()
    controller = createStartupEvents(deps)
    controller.attach()

    window.dispatchEvent(
      new CustomEvent('navigate-to-block', {
        detail: {
          notebook: 'Work',
          section: '',
          page: 'RootNote',
          blockId: 'block-root'
        }
      })
    )

    expect(deps.handleSearchJump).toHaveBeenCalledTimes(1)
    expect(deps.handleSearchJump).toHaveBeenCalledWith(
      {
        source: undefined,
        notebook: 'Work',
        section: '',
        page: 'RootNote'
      },
      undefined,
      'block-root'
    )
    expect(mocks.pushNotification).not.toHaveBeenCalled()
  })

  function expectIncompleteBlockNavRejected(deps: StartupEventsDeps): void {
    expect(deps.handleSearchJump).not.toHaveBeenCalled()
    expect(deps.openTasksView).not.toHaveBeenCalled()
    expect(mocks.pushNotification).toHaveBeenCalledWith({
      kind: 'info',
      message: INCOMPLETE_BLOCK_NAV_TOAST,
      autoDismissMs: 5000
    })
  }

  it('navigate-to-block with only blockId does not jump and toasts', () => {
    const bus = makeBus()
    wireEventsOn(bus)
    const { deps } = makeDeps()
    controller = createStartupEvents(deps)
    controller.attach()

    window.dispatchEvent(
      new CustomEvent('navigate-to-block', {
        detail: { blockId: 'orphan-block' }
      })
    )

    expectIncompleteBlockNavRejected(deps)
  })

  it('navigate-to-block missing page does not jump and toasts', () => {
    const bus = makeBus()
    wireEventsOn(bus)
    const { deps } = makeDeps()
    controller = createStartupEvents(deps)
    controller.attach()

    window.dispatchEvent(
      new CustomEvent('navigate-to-block', {
        detail: { notebook: 'Work', section: 'Notes', blockId: 'b1' }
      })
    )

    expectIncompleteBlockNavRejected(deps)
  })

  it('navigate-to-block missing notebook does not jump and toasts', () => {
    const bus = makeBus()
    wireEventsOn(bus)
    const { deps } = makeDeps()
    controller = createStartupEvents(deps)
    controller.attach()

    window.dispatchEvent(
      new CustomEvent('navigate-to-block', {
        detail: { page: 'Plan', section: 'Notes', blockId: 'b1' }
      })
    )

    expectIncompleteBlockNavRejected(deps)
  })

  it('navigate-to-block with empty-string notebook/page does not jump and toasts', () => {
    const bus = makeBus()
    wireEventsOn(bus)
    const { deps } = makeDeps()
    controller = createStartupEvents(deps)
    controller.attach()

    window.dispatchEvent(
      new CustomEvent('navigate-to-block', {
        detail: {
          notebook: '',
          section: 'Notes',
          page: '',
          blockId: 'b1'
        }
      })
    )

    expectIncompleteBlockNavRejected(deps)
  })

  it('navigate-to-block with no detail does not jump and toasts', () => {
    const bus = makeBus()
    wireEventsOn(bus)
    const { deps } = makeDeps()
    controller = createStartupEvents(deps)
    controller.attach()

    window.dispatchEvent(new CustomEvent('navigate-to-block'))

    expectIncompleteBlockNavRejected(deps)
  })

  it('navigate-to-block with non-string notebook/page does not jump and toasts', () => {
    const bus = makeBus()
    wireEventsOn(bus)
    const { deps } = makeDeps()
    controller = createStartupEvents(deps)
    controller.attach()

    window.dispatchEvent(
      new CustomEvent('navigate-to-block', {
        detail: { notebook: 1, page: true, blockId: 'b1' }
      })
    )

    expectIncompleteBlockNavRejected(deps)
  })

  it('navigate-to-page with full locator calls handleSearchJump', () => {
    const bus = makeBus()
    wireEventsOn(bus)
    const { deps } = makeDeps()
    controller = createStartupEvents(deps)
    controller.attach()

    window.dispatchEvent(
      new CustomEvent('navigate-to-page', {
        detail: {
          notebook: 'Work',
          section: 'Notes',
          page: 'Plan'
        }
      })
    )

    expect(deps.handleSearchJump).toHaveBeenCalledTimes(1)
    expect(deps.handleSearchJump).toHaveBeenCalledWith(
      {
        source: undefined,
        notebook: 'Work',
        section: 'Notes',
        page: 'Plan'
      },
      '',
      ''
    )
    expect(mocks.pushNotification).not.toHaveBeenCalled()
  })

  it('navigate-to-page missing notebook/page does not jump and toasts', () => {
    const bus = makeBus()
    wireEventsOn(bus)
    const { deps } = makeDeps()
    controller = createStartupEvents(deps)
    controller.attach()

    window.dispatchEvent(
      new CustomEvent('navigate-to-page', {
        detail: { notebook: 'Work', section: 'Notes' }
      })
    )

    expect(deps.handleSearchJump).not.toHaveBeenCalled()
    expect(mocks.pushNotification).toHaveBeenCalledWith({
      kind: 'info',
      message: INCOMPLETE_PAGE_NAV_TOAST,
      autoDismissMs: 5000
    })
  })

  it('navigate-to-page with no detail does not jump and toasts', () => {
    const bus = makeBus()
    wireEventsOn(bus)
    const { deps } = makeDeps()
    controller = createStartupEvents(deps)
    controller.attach()

    window.dispatchEvent(new CustomEvent('navigate-to-page'))

    expect(deps.handleSearchJump).not.toHaveBeenCalled()
    expect(mocks.pushNotification).toHaveBeenCalledWith({
      kind: 'info',
      message: INCOMPLETE_PAGE_NAV_TOAST,
      autoDismissMs: 5000
    })
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
