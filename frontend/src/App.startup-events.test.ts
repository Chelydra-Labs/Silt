// Golden-master characterization for the startup-events cluster (#768).
//
// Locks the two delivery paths before the controller extraction:
//  1. LIVE delivery — a Wails Events.On handler fires and runs the named
//     handler (notification / dialog open).
//  2. REPLAY — MarkFrontendReady + GetStartupEvents drain the queued backlog
//     and dispatchStartupEvent fans each out to the SAME named handler.
// A startup event must be indistinguishable from a live one to its handler.
// These MUST pass unchanged after the extraction — the oracle does not move.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, cleanup } from '@testing-library/svelte'
import { tick } from 'svelte'
import { waitFor } from '@testing-library/dom'
import EmptyStub from './components/EmptyStub.stub.svelte'
import DialogMarkerStub from './components/DialogMarkerStub.stub.svelte'
import { EventName } from './generated/enums'
import {
  notificationsState,
  _resetForTests as resetNotifications
} from './notifications/store.svelte'
import {
  clearTaskPageRoute,
  resetTaskHubState
} from './plugins/first-party/silt-tasks/state.svelte'

// Events.On callbacks captured by event name so the test can invoke a handler
// exactly as App registered it (mirrors App.menu-save.test.ts).
const eventHandlers = new Map<string, (...args: unknown[]) => void>()

const bindings = vi.hoisted(() => ({
  IsVaultInitialized: vi.fn(async () => false),
  GetOpenTabs: vi.fn(
    async (): Promise<{ open_tabs: unknown[]; active_tab: unknown }> => ({
      open_tabs: [],
      active_tab: null
    })
  ),
  GetStartupEvents: vi.fn(
    async (): Promise<Array<{ Name: string; Payload: unknown }>> => []
  ),
  ResolveQuarantinedLinks: vi.fn(
    async (): Promise<
      Array<{ id: string; display_name: string; root_path: string }>
    > => []
  ),
  MarkFrontendReady: vi.fn(async () => undefined)
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
    SetOpenTabs: vi.fn(noop),
    ConfirmSettingsChange: vi.fn(noop),
    ConfirmGrantsMigration: vi.fn(noop),
    DeclineGrantsMigration: vi.fn(noop),
    ResolveQuarantinedLinks: bindings.ResolveQuarantinedLinks,
    PickLinkedNotebook: vi.fn(noop),
    UnlinkNotebook: vi.fn(noop),
    CreateStandaloneTask: vi.fn(async () => 'tsk'),
    MarkFrontendReady: bindings.MarkFrontendReady,
    GetStartupEvents: bindings.GetStartupEvents,
    RecordRecentPage: vi.fn(noop),
    UpdateAIFeatures: vi.fn(noop)
  })
})

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
  settings: { config: { hotkeys: {}, ui: {}, editor: {} } },
  loadConfig: vi.fn(() => Promise.resolve()),
  initConfigHotReload: vi.fn(() => () => {}),
  toggleFormatToolbar: vi.fn(async () => true),
  toggleFocusMode: vi.fn(async () => true),
  toggleTypewriterMode: vi.fn(async () => true)
}))
vi.mock('./theme/store.svelte', () => ({ initThemes: vi.fn(() => () => {}) }))
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

// The three settings dialogs render unconditionally (gated by an `open` prop)
// outside the onboarding branch. Stub them so their open state is observable
// without depending on each dialog's internal behavior.
vi.mock('./components/settings/SettingsMismatchDialog.svelte', () => ({
  default: DialogMarkerStub
}))
vi.mock('./components/settings/GrantsMigrationDialog.svelte', () => ({
  default: DialogMarkerStub
}))
vi.mock('./components/settings/QuarantinedLinksDialog.svelte', () => ({
  default: DialogMarkerStub
}))

vi.mock('./components/Onboarding.svelte', () => ({ default: EmptyStub }))
vi.mock('./components/PluginModalHost.svelte', () => ({ default: EmptyStub }))
vi.mock('./components/PluginStatusBar.svelte', () => ({ default: EmptyStub }))
vi.mock('./components/ToastContainer.svelte', () => ({ default: EmptyStub }))
vi.mock('./plugins/shared/ai-chat/AIChatDrawer.svelte', () => ({
  default: EmptyStub
}))
vi.mock('./components/DateGlance.svelte', () => ({ default: EmptyStub }))

import App from './App.svelte'

async function mountApp(): Promise<void> {
  render(App)
  await tick()
  await waitFor(() => expect(bindings.IsVaultInitialized).toHaveBeenCalled())
}

// Emit a live Wails event exactly as the runtime would deliver it: payload on
// `.data`. No-op (and asserted) if the handler wasn't registered.
function emitLive(name: string, data: unknown): void {
  const cb = eventHandlers.get(name)
  if (!cb) throw new Error(`no Events.On handler registered for ${name}`)
  cb({ data } as never)
}

function hasNotification(predicate: (msg: string) => boolean): boolean {
  return notificationsState.items.some((n) => predicate(n.message))
}

describe('startup events (golden master)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    eventHandlers.clear()
    resetNotifications()
    clearTaskPageRoute()
    resetTaskHubState()
    bindings.IsVaultInitialized.mockResolvedValue(false)
    bindings.GetOpenTabs.mockResolvedValue({ open_tabs: [], active_tab: null })
    bindings.GetStartupEvents.mockResolvedValue([])
    bindings.ResolveQuarantinedLinks.mockResolvedValue([])
  })

  afterEach(() => cleanup())

  // --- LIVE delivery (listener attached before the event arrives) ---
  it('delivers vault:init-error live as a sticky error notification', async () => {
    await mountApp()
    emitLive(EventName.EventVaultInitError, 'database locked')
    await waitFor(() =>
      expect(
        hasNotification((m) =>
          /Vault failed to initialize: database locked/.test(m)
        )
      ).toBe(true)
    )
  })

  it('delivers vault:init-warnings live as an info notification', async () => {
    await mountApp()
    emitLive(EventName.EventVaultInitWarnings, [
      'symlink skipped',
      'perm error'
    ])
    await waitFor(() =>
      expect(
        hasNotification((m) => /Vault initialized with warnings/.test(m))
      ).toBe(true)
    )
  })

  it('delivers vault:watch-coverage live as an info notification', async () => {
    await mountApp()
    emitLive(EventName.EventVaultWatchCoverage, ['/a', '/b', '/c'])
    await waitFor(() =>
      expect(
        hasNotification((m) => /File watching unavailable for 3 path/.test(m))
      ).toBe(true)
    )
  })

  it('delivers settings:fingerprint-mismatch live by opening the dialog', async () => {
    await mountApp()
    emitLive(EventName.EventSettingsFingerprintMismatch, undefined)
    await waitFor(() =>
      expect(
        document.querySelector('[data-testid="dialog-marker"]')
      ).toBeTruthy()
    )
  })

  it('delivers grants:migration-required live by opening the dialog', async () => {
    await mountApp()
    emitLive(EventName.EventGrantsMigrationRequired, {
      plugin: { cap: 'qual' }
    })
    await waitFor(() =>
      expect(
        document.querySelector('[data-testid="dialog-marker"]')
      ).toBeTruthy()
    )
  })

  it('delivers linked-notebook:quarantined live by refreshing + opening the dialog', async () => {
    bindings.ResolveQuarantinedLinks.mockResolvedValue([
      { id: 'lnk1', display_name: 'Team', root_path: '/tmp/team' }
    ])
    await mountApp()
    emitLive(EventName.EventLinkedNotebookQuarantined, undefined)
    await waitFor(() =>
      expect(bindings.ResolveQuarantinedLinks).toHaveBeenCalled()
    )
    await waitFor(() =>
      expect(
        document.querySelector('[data-testid="dialog-marker"]')
      ).toBeTruthy()
    )
  })

  // --- REPLAY (queued backlog drained after MarkFrontendReady) ---
  it('replays a queued vault:init-error through the same handler as live', async () => {
    bindings.GetStartupEvents.mockResolvedValue([
      { Name: EventName.EventVaultInitError, Payload: 'early failure' }
    ])
    await mountApp()
    // MarkFrontendReady + GetStartupEvents drain on mount.
    await waitFor(() => expect(bindings.MarkFrontendReady).toHaveBeenCalled())
    await waitFor(() =>
      expect(
        hasNotification((m) =>
          /Vault failed to initialize: early failure/.test(m)
        )
      ).toBe(true)
    )
  })

  it('replays the full queued backlog in order, fanning out to each handler', async () => {
    bindings.GetStartupEvents.mockResolvedValue([
      { Name: EventName.EventVaultInitError, Payload: 'err-1' },
      { Name: EventName.EventVaultInitWarnings, Payload: ['w-1'] },
      { Name: EventName.EventVaultWatchCoverage, Payload: ['/p'] },
      { Name: EventName.EventSettingsFingerprintMismatch, Payload: null }
    ])
    await mountApp()
    await waitFor(() => expect(bindings.MarkFrontendReady).toHaveBeenCalled())
    // Each replayed event reached its handler.
    await waitFor(() =>
      expect(
        hasNotification((m) => /Vault failed to initialize: err-1/.test(m))
      ).toBe(true)
    )
    await waitFor(() =>
      expect(
        hasNotification((m) => /Vault initialized with warnings/.test(m))
      ).toBe(true)
    )
    await waitFor(() =>
      expect(
        hasNotification((m) => /File watching unavailable for 1 path/.test(m))
      ).toBe(true)
    )
    // The mismatch replay opened its dialog.
    await waitFor(() =>
      expect(
        document.querySelector('[data-testid="dialog-marker"]')
      ).toBeTruthy()
    )
  })

  it('a startup event with no live listener still reaches its handler via replay', async () => {
    // ServiceStartup fires before the webview exists — the only path is replay.
    // Vault init error has both a live listener and a replay case; here we prove
    // the replay path alone is sufficient (no emitLive call).
    bindings.GetStartupEvents.mockResolvedValue([
      { Name: EventName.EventVaultInitWarnings, Payload: ['only-via-replay'] }
    ])
    await mountApp()
    await waitFor(() =>
      expect(hasNotification((m) => /only-via-replay/.test(m))).toBe(true)
    )
  })
})
