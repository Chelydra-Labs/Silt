// #1005 search-jump refused-open contract: handleSearchJump must honor
// openPage's boolean. When every tab is conflict-busy the open is refused,
// and proceeding as-if-opened would set the search scroll targets against
// the STILL-active page — a same-named heading scrolls the wrong page and a
// wrong block id burns its scroll-attempt budget. Pinned at the App level
// through the startup-events deep-link bus (navigate-to-block), one of the
// three caller families the review flagged (search modal, deep links,
// backlinks panel — all funnel into the same handler).
import { afterEach, describe, expect, it, vi, beforeEach } from 'vitest'
import { render, cleanup } from '@testing-library/svelte'
import { tick } from 'svelte'
import { waitFor } from '@testing-library/dom'
import EmptyStub from './components/EmptyStub.stub.svelte'
import VscConflictBusyStub from './components/VirtualScrollContainer.stub.svelte'
import {
  notificationsState,
  _resetForTests as resetNotifications
} from './notifications/store.svelte'
import { createAppIpcMocks } from './lib/testing/ipcMock'

const bindings = vi.hoisted(() => ({
  IsVaultInitialized: vi.fn(async () => true),
  GetOpenTabs: vi.fn(
    async (): Promise<{
      open_tabs: unknown[]
      active_tab: unknown
    }> => ({ open_tabs: [], active_tab: null })
  ),
  GetStartupEvents: vi.fn(async () => []),
  SetOpenTabs: vi.fn(async (): Promise<void> => undefined),
  RecordRecentPage: vi.fn(async () => undefined)
}))

vi.mock('$silt-app', () => {
  const noop = () => Promise.resolve(undefined)
  return createAppIpcMocks({
    IsVaultInitialized: bindings.IsVaultInitialized,
    InitializeVault: vi.fn(noop),
    CloseVault: vi.fn(noop),
    GetSidebarWidth: vi.fn(async () => 256),
    SetSidebarWidth: vi.fn(noop),
    GetOpenTabs: bindings.GetOpenTabs,
    SetOpenTabs: bindings.SetOpenTabs,
    ConfirmSettingsChange: vi.fn(noop),
    ConfirmGrantsMigration: vi.fn(noop),
    DeclineGrantsMigration: vi.fn(noop),
    ResolveQuarantinedLinks: vi.fn(async () => []),
    PickLinkedNotebook: vi.fn(noop),
    UnlinkNotebook: vi.fn(noop),
    MarkFrontendReady: vi.fn(noop),
    GetStartupEvents: bindings.GetStartupEvents,
    RecordRecentPage: bindings.RecordRecentPage,
    // The initialized shell region renders the notes sidebar / type pill;
    // their best-effort loads must not throw on a missing export.
    ListTypes: vi.fn(async () => []),
    SetNavigationSectionExpanded: vi.fn(noop),
    GetNavigationTree: vi.fn(async () => ({ notebooks: [] })),
    QueryPagesByType: vi.fn(async () => []),
    ReloadTypes: vi.fn(async () => undefined),
    GetTypesReprojectionStatus: vi.fn(async () => ({
      active: false,
      processed: 0,
      total: 0
    }))
  })
})

vi.mock('./properties/pageTypeState.svelte', () => ({
  createPageTypeController: () => ({
    info: { isSet: false, typeId: '', type: null, rawType: '' },
    values: [],
    mismatched: [],
    error: '',
    loading: false,
    types: [],
    typesLoading: false,
    typeLoadErrors: [],
    panelOpen: false,
    modalOpen: false,
    heroValue: '',
    core: { notebook: '', section: '', page: '' },
    typeMenuRequest: 0,
    refresh: () => Promise.resolve(),
    open: () => {},
    close: () => {},
    toggle: () => {},
    openModal: () => {},
    closeModal: () => {},
    requestTypeMenu: () => {},
    setMismatched: () => {},
    setError: () => {},
    commitCore: () => Promise.resolve(),
    attach: () => () => {}
  })
}))

const eventHandlers = new Map<string, (...args: unknown[]) => void>()
vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn((name: string, cb: (...args: unknown[]) => void) => {
      eventHandlers.set(name, cb)
      return () => {
        eventHandlers.delete(name)
      }
    }),
    Off: vi.fn()
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

vi.mock('./plugins/loader', () => ({
  loadPlugins: vi.fn(() => Promise.resolve())
}))

vi.mock('./settings/store.svelte', () => ({
  settings: {
    config: {
      hotkeys: {},
      // Capacity 1: with the single hydrated tab reporting conflict-busy,
      // any open of a DIFFERENT page is refused (no eviction candidate).
      ui: { max_open_tabs: 1 },
      editor: {}
    }
  },
  loadConfig: vi.fn(() => Promise.resolve()),
  initConfigHotReload: vi.fn(() => () => {}),
  toggleFormatToolbar: vi.fn(async () => true),
  toggleFocusMode: vi.fn(async () => true),
  toggleTypewriterMode: vi.fn(async () => true)
}))
vi.mock('./theme/store.svelte', () => ({
  initThemes: vi.fn(() => () => {})
}))
vi.mock('./templates/store.svelte', () => ({
  initTemplates: vi.fn(() => () => {})
}))
vi.mock('./updates/store.svelte', () => ({
  initStartupUpdateCheck: vi.fn(() => Promise.resolve()),
  disposeUpdateStore: vi.fn()
}))
vi.mock('./settings/editor-tokens.svelte', () => ({
  initEditorTokens: vi.fn(() => () => {})
}))
vi.mock('./components/VirtualScrollContainer.svelte', () => ({
  default: VscConflictBusyStub
}))
vi.mock('./components/settings/SettingsPanel.svelte', () => ({
  default: EmptyStub
}))
vi.mock('./components/Onboarding.svelte', () => ({ default: EmptyStub }))
vi.mock('./components/PluginModalHost.svelte', () => ({ default: EmptyStub }))
vi.mock('./components/PluginStatusBar.svelte', () => ({ default: EmptyStub }))
vi.mock('./components/ToastContainer.svelte', () => ({ default: EmptyStub }))
vi.mock('./plugins/shared/ai-chat/AIChatDrawer.svelte', () => ({
  default: EmptyStub
}))

import App from './App.svelte'

describe('handleSearchJump refused-open contract (#1005)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    resetNotifications()
    eventHandlers.clear()
    bindings.IsVaultInitialized.mockResolvedValue(true)
    bindings.GetStartupEvents.mockResolvedValue([])
  })

  afterEach(() => cleanup())

  async function mountWithBusyTab(): Promise<void> {
    bindings.GetOpenTabs.mockResolvedValue({
      open_tabs: [
        { notebook: 'nb', section: 's', page: 'alpha', view_mode: '' }
      ],
      active_tab: { notebook: 'nb', section: 's', page: 'alpha', view_mode: '' }
    })
    render(App)
    await tick()
    await waitFor(() => expect(bindings.GetOpenTabs).toHaveBeenCalled())
    await waitFor(() =>
      expect(document.querySelector('[data-testid="vsc-stub"]')).toBeTruthy()
    )
    document
      .querySelector<HTMLElement>('[data-testid="vsc-report-busy"]')!
      .click()
    bindings.SetOpenTabs.mockClear()
  }

  it('stops search-target follow-through and notifies when the open is refused', async () => {
    await mountWithBusyTab()
    const stub = () =>
      document.querySelector<HTMLElement>('[data-testid="vsc-stub"]')!

    // Deep link to a different page's block while every tab is busy.
    window.dispatchEvent(
      new CustomEvent('navigate-to-block', {
        detail: {
          notebook: 'nb',
          section: 's',
          page: 'beta',
          blockId: 'blk-1',
          date: '2026-01-01'
        }
      })
    )
    await tick()
    await new Promise((r) => setTimeout(r, 50))

    // The refused open is reported…
    expect(
      notificationsState.items.some((n) =>
        n.message.includes('every tab is busy resolving a save conflict')
      )
    ).toBe(true)
    // …and no search target was armed against the still-active page —
    // without the guard this renders blk-1 and scrolls the WRONG page.
    expect(stub().dataset.targetblock).toBe('')
    expect(stub().dataset.targetkey).toBe('')
    expect(bindings.SetOpenTabs).not.toHaveBeenCalled()
  })

  it('arms the search target once a tab settles and the open succeeds', async () => {
    await mountWithBusyTab()
    const stub = () =>
      document.querySelector<HTMLElement>('[data-testid="vsc-stub"]')!

    document
      .querySelector<HTMLElement>('[data-testid="vsc-report-idle"]')!
      .click()
    window.dispatchEvent(
      new CustomEvent('navigate-to-block', {
        detail: {
          notebook: 'nb',
          section: 's',
          page: 'beta',
          blockId: 'blk-2',
          date: '2026-01-01'
        }
      })
    )
    await tick()
    await waitFor(() => expect(stub().dataset.targetblock).toBe('blk-2'))
    expect(stub().dataset.targetkey).toContain('blk-2')
  })
})
