// #1018 refresh-spin contract: App's locator $effect must track ONLY the
// locator signals. Calling pageType.refresh() tracked read it the effect's
// synchronous prologue (`values.length`) and its fetch completion rewrites
// info/values/core — so every fetch completion re-triggered the effect and
// scheduled another refresh of the same page: an unbounded spin throttled
// only by IPC latency (~50k refresh/s under instant mocks). Pinned here by
// mounting the REAL App with the REAL pageTypeState controller and instant
// mocks that resolve EMPTY values — the exact spin shape — and asserting the
// refresh count converges.
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { render, cleanup } from '@testing-library/svelte'
import { tick } from 'svelte'
import { waitFor } from '@testing-library/dom'
import EmptyStub from './components/EmptyStub.stub.svelte'
import VscConflictBusyStub from './components/VirtualScrollContainer.stub.svelte'
import { _resetForTests as resetNotifications } from './notifications/store.svelte'
import { createAppIpcMocks } from './lib/testing/ipcMock'

const bindings = vi.hoisted(() => ({
  IsVaultInitialized: vi.fn(async () => true),
  GetOpenTabs: vi.fn(
    async (): Promise<{
      open_tabs: unknown[]
      active_tab: unknown
    }> => {
      throw new Error('set per-test')
    }
  ),
  SetOpenTabs: vi.fn(async (): Promise<void> => undefined),
  GetStartupEvents: vi.fn(async () => []),
  RecordRecentPage: vi.fn(async () => undefined),
  // Fresh EMPTY payloads on every call: a new [] reference per resolution is
  // what the real IPC round-trip produces and what re-triggers a tracking
  // effect (mockResolvedValue would reuse one array ref and mask the spin).
  GetPageType: vi.fn(async () => null),
  GetPageProperties: vi.fn(async () => []),
  GetPageCoreMetadata: vi.fn(async () => ({
    notebook: 'nb',
    section: 's',
    page: 'alpha',
    type: '',
    date: '',
    tags: [],
    aliases: [],
    created: '',
    modified: '',
    tagsAreReadOnly: false
  }))
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
    })),
    GetPageType: bindings.GetPageType,
    GetPageProperties: bindings.GetPageProperties,
    GetPageCoreMetadata: bindings.GetPageCoreMetadata
  })
})

// NOTE: no vi.mock for './properties/pageTypeState.svelte' — this suite pins
// the REAL controller inside the REAL App effect; stubbing the module would
// hide the very dependency-tracking defect under test.

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

const eventHandlers = new Map<string, (...args: unknown[]) => void>()

vi.mock('./plugins/loader', () => ({
  loadPlugins: vi.fn(() => Promise.resolve())
}))
vi.mock('./settings/store.svelte', () => ({
  settings: {
    config: {
      hotkeys: {},
      ui: {},
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

const settle = (ms: number) => new Promise((r) => setTimeout(r, ms))

describe('pageType refresh spin (#1018)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    resetNotifications()
    eventHandlers.clear()
    bindings.IsVaultInitialized.mockResolvedValue(true)
    bindings.GetStartupEvents.mockResolvedValue([])
    // Rehydrate straight onto a notes page so the locator effect runs with a
    // full locator and the real controller refreshes.
    bindings.GetOpenTabs.mockResolvedValue({
      open_tabs: [
        { notebook: 'nb', section: 's', page: 'alpha', view_mode: '' }
      ],
      active_tab: { notebook: 'nb', section: 's', page: 'alpha', view_mode: '' }
    })
  })

  afterEach(() => cleanup())

  it('locator effect does not re-trigger on refresh writes — count converges', async () => {
    render(App)
    await tick()
    // The controller fetched the rehydrated page at least once.
    await waitFor(() => expect(bindings.GetPageType).toHaveBeenCalled())

    // Let any (unfixed) spin accumulate for two independent windows. Fixed
    // code performs one refresh per locator change; a tracking effect would
    // rack up hundreds of calls per window under these instant mocks.
    await settle(80)
    const first = bindings.GetPageType.mock.calls.length
    expect(first).toBeGreaterThanOrEqual(1)
    expect(first).toBeLessThanOrEqual(3)

    await settle(80)
    const second = bindings.GetPageType.mock.calls.length
    expect(second).toBeLessThanOrEqual(first + 1)
  })
})
