// Focused coverage for the native menu Save (#503). The full App shell is
// heavy to mount, so this keeps the vault on the onboarding branch (onMount
// still runs — including tab hydration, which sets the active page triple)
// and asserts only the menu:save -> active-editor-flush contract. IPC, the
// Wails runtime, and the boot/store initializers are mocked; the editor
// registry and notification store run for real so the wiring under test is
// exercised end-to-end.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, cleanup } from '@testing-library/svelte'
import { tick } from 'svelte'
import { waitFor } from '@testing-library/dom'
import EmptyStub from './components/EmptyStub.stub.svelte'
import {
  registerEditor,
  _resetEditorRegistryForTests
} from './lib/editor/editorRegistry.svelte'
import {
  notificationsState,
  _resetForTests as resetNotifications
} from './notifications/store.svelte'

// Events.On callbacks captured by event name so the test can invoke the
// menu:save handler exactly as App registered it.
const eventHandlers = new Map<string, (...args: any[]) => void>()

const bindings = vi.hoisted(() => ({
  IsVaultInitialized: vi.fn(async () => false),
  GetOpenTabs: vi.fn(async (): Promise<any> => ({
    open_tabs: [],
    active_tab: null
  })),
  GetStartupEvents: vi.fn(async () => [])
}))

vi.mock('../bindings/silt/app.js', () => {
  const noop = () => Promise.resolve(undefined)
  return {
    IsVaultInitialized: bindings.IsVaultInitialized,
    InitializeVault: vi.fn(noop),
    CloseVault: vi.fn(noop),
    GetSidebarWidth: vi.fn(async () => 256),
    SetSidebarWidth: vi.fn(noop),
    GetOpenTabs: bindings.GetOpenTabs,
    SetOpenTabs: vi.fn(noop),
    ConfirmSettingsChange: vi.fn(noop),
    ConfirmGrantsMigration: vi.fn(noop),
    DeclineGrantsMigration: vi.fn(noop),
    ResolveQuarantinedLinks: vi.fn(async () => []),
    PickLinkedNotebook: vi.fn(noop),
    UnlinkNotebook: vi.fn(noop),
    CreateStandaloneTask: vi.fn(async () => 'tsk'),
    MarkFrontendReady: vi.fn(noop),
    GetStartupEvents: bindings.GetStartupEvents
  }
})

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn((name: string, cb: (...args: any[]) => void) => {
      eventHandlers.set(name, cb)
      return () => {
        eventHandlers.delete(name)
      }
    })
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
    Nullable: (fn: any) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

vi.mock('./plugins/loader', () => ({
  loadPlugins: vi.fn(() => Promise.resolve())
}))
vi.mock('./settings/store.svelte', () => ({
  settings: { config: { hotkeys: {}, ui: {}, editor: {} } },
  loadConfig: vi.fn(() => Promise.resolve()),
  initConfigHotReload: vi.fn(() => () => {}),
  toggleFormatToolbar: vi.fn(() => Promise.resolve(true)),
  toggleFocusMode: vi.fn(() => Promise.resolve(true)),
  toggleTypewriterMode: vi.fn(() => Promise.resolve(true))
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

// Always-rendered chrome children that are irrelevant to menu:save are
// stubbed so their own imports/effects can't interfere with the test.
vi.mock('./components/Onboarding.svelte', () => ({ default: EmptyStub }))
vi.mock('./components/PluginModalHost.svelte', () => ({ default: EmptyStub }))
vi.mock('./components/PluginStatusBar.svelte', () => ({ default: EmptyStub }))
vi.mock('./components/ToastContainer.svelte', () => ({ default: EmptyStub }))

import App from './App.svelte'

// The page triple the GetOpenTabs mock hydrates as active. The registry key
// must match what the editor registers under (the \x00-join contract).
const NB = 'vault'
const SEC = 'notes'
const PG = 'page1'
const ACTIVE_KEY = `${NB}\x00${SEC}\x00${PG}`
const OTHER_KEY = `${NB}\x00${SEC}\x00page2`

function setOpenTabs(active = true): void {
  bindings.GetOpenTabs.mockResolvedValue(
    active
      ? {
          open_tabs: [{ notebook: NB, section: SEC, page: PG, view_mode: '' }],
          active_tab: { notebook: NB, section: SEC, page: PG, view_mode: '' }
        }
      : { open_tabs: [], active_tab: null }
  )
}

async function mountApp(): Promise<void> {
  render(App)
  // onMount registers the Events.On('menu:save') handler; let it flush.
  await tick()
  await waitFor(() => expect(eventHandlers.has('menu:save')).toBe(true))
}

function fireMenuSave(): void {
  const cb = eventHandlers.get('menu:save')
  if (!cb) throw new Error('menu:save handler was not registered')
  cb()
}

let unregister: Array<() => void> = []

function registerFake(key: string, flush: () => Promise<boolean>): void {
  unregister.push(
    registerEditor({
      key,
      isDirty: () => true,
      flush,
      forceExternalReload: vi.fn(),
      setProposedEdit: () => false,
      clearProposedEdit: () => {},
      hasProposal: () => false,
      acceptProposedEdit: () => false,
      verifySelectionText: () => false
    })
  )
}

describe('native menu Save (#503)', () => {
  beforeEach(() => {
    _resetEditorRegistryForTests()
    resetNotifications()
    eventHandlers.clear()
    unregister = []
    bindings.IsVaultInitialized.mockResolvedValue(false)
    setOpenTabs(true)
  })

  afterEach(() => {
    for (const u of unregister) u()
    cleanup()
  })

  it('flushes the active editor when menu:save fires', async () => {
    const activeFlush = vi.fn(async (): Promise<boolean> => true)
    registerFake(ACTIVE_KEY, activeFlush)

    await mountApp()
    fireMenuSave()

    await waitFor(() => expect(activeFlush).toHaveBeenCalledTimes(1))
  })

  it('does not flush a different (non-active) editor', async () => {
    const activeFlush = vi.fn(async (): Promise<boolean> => true)
    const otherFlush = vi.fn(async (): Promise<boolean> => true)
    registerFake(ACTIVE_KEY, activeFlush)
    registerFake(OTHER_KEY, otherFlush)

    await mountApp()
    fireMenuSave()

    await waitFor(() => expect(activeFlush).toHaveBeenCalledTimes(1))
    // Yield so any stray flush on the other editor could have run.
    await tick()
    expect(otherFlush).not.toHaveBeenCalled()
  })

  it('no-ops when there is no active page (non-page context)', async () => {
    const flush = vi.fn(async (): Promise<boolean> => true)
    registerFake(ACTIVE_KEY, flush)
    setOpenTabs(false) // no tabs -> activePage stays empty

    await mountApp()
    fireMenuSave()
    await tick()

    expect(flush).not.toHaveBeenCalled()
    expect(notificationsState.items).toHaveLength(0)
  })

  it('no-ops when no editor is mounted for the active page', async () => {
    // Register an editor for a DIFFERENT page only.
    const otherFlush = vi.fn(async (): Promise<boolean> => true)
    registerFake(OTHER_KEY, otherFlush)

    await mountApp()
    fireMenuSave()
    await tick()

    expect(otherFlush).not.toHaveBeenCalled()
    expect(notificationsState.items).toHaveLength(0)
  })

  it('surfaces an error notification when flush reports failure', async () => {
    registerFake(
      ACTIVE_KEY,
      vi.fn(async (): Promise<boolean> => false)
    )

    await mountApp()
    fireMenuSave()

    await waitFor(() =>
      expect(
        notificationsState.items.some((n) =>
          /unsaved edits are still pending/.test(n.message)
        )
      ).toBe(true)
    )
  })

  it('surfaces an error notification when flush rejects', async () => {
    registerFake(ACTIVE_KEY, async (): Promise<boolean> => {
      throw new Error('disk full')
    })

    await mountApp()
    fireMenuSave()

    await waitFor(() =>
      expect(
        notificationsState.items.some((n) =>
          /Could not save the current page/.test(n.message)
        )
      ).toBe(true)
    )
  })
})
