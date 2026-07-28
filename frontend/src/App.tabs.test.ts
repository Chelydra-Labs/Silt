// Golden-master characterization for the tab-manager cluster (#768).
//
// Locks the App-level tab behaviors that the pure state machine in tabs.ts
// does NOT cover: hydration from config.yaml, the `.silt-stray` fail-loud
// guard, the pinned-only persistence payload shape, and per-notebook
// displayedTabs scoping. These MUST pass unchanged after the controller
// extraction — the oracle does not move. (The pure open/close/promote/
// reorder/cycle/view-mode transitions are already exhaustively covered by
// tabs.test.ts; the hotkey-driven close/cycle/view-mode wiring is covered by
// App.hotkeys.test.ts. This file owns the integration layer the manager adds.)
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, cleanup } from '@testing-library/svelte'
import { tick } from 'svelte'
import { waitFor } from '@testing-library/dom'
import EmptyStub from './components/EmptyStub.stub.svelte'
import { _resetForTests as resetNotifications } from './notifications/store.svelte'
import {
  clearTaskPageRoute,
  resetTaskHubState
} from './plugins/first-party/silt-tasks/state.svelte'

const bindings = vi.hoisted(() => ({
  IsVaultInitialized: vi.fn(async () => false),
  GetOpenTabs: vi.fn(
    async (): Promise<{
      open_tabs: unknown[]
      active_tab: unknown
    }> => ({ open_tabs: [], active_tab: null })
  ),
  GetStartupEvents: vi.fn(async () => []),
  SetOpenTabs: vi.fn(
    async (
      _tabs: Array<{
        notebook: string
        section: string
        page: string
        view_mode?: string
      }>,
      _active: {
        notebook: string
        section: string
        page: string
        view_mode?: string
      } | null
    ): Promise<void> => undefined
  ),
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
    CreateStandaloneTask: vi.fn(async () => 'tsk'),
    MarkFrontendReady: vi.fn(noop),
    GetStartupEvents: bindings.GetStartupEvents,
    RecordRecentPage: bindings.RecordRecentPage,
    UpdateAIFeatures: vi.fn(noop)
  })
})

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn(() => () => {}),
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
      hotkeys: {
        // Only the tab-strip actions are driven here; the full dispatch table
        // is owned by App.hotkeys.test.ts.
        next_tab: 'Ctrl+Tab',
        prev_tab: 'Ctrl+Shift+Tab',
        close_tab: 'Ctrl+W',
        toggle_view_mode: 'Ctrl+Shift+V'
      },
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
vi.mock('./components/Onboarding.svelte', () => ({ default: EmptyStub }))
vi.mock('./components/PluginModalHost.svelte', () => ({ default: EmptyStub }))
vi.mock('./components/PluginStatusBar.svelte', () => ({ default: EmptyStub }))
vi.mock('./components/ToastContainer.svelte', () => ({ default: EmptyStub }))
vi.mock('./plugins/shared/ai-chat/AIChatDrawer.svelte', () => ({
  default: EmptyStub
}))

import App from './App.svelte'

async function mountApp(): Promise<void> {
  render(App)
  await tick()
  await waitFor(() => expect(bindings.IsVaultInitialized).toHaveBeenCalled())
}

function press(chord: string): void {
  const parts = chord.split('+')
  const key = parts.pop()!
  const mods: Record<string, boolean> = {
    ctrlKey: false,
    altKey: false,
    shiftKey: false
  }
  for (const p of parts) {
    if (p === 'Ctrl') mods.ctrlKey = true
    if (p === 'Alt') mods.altKey = true
    if (p === 'Shift') mods.shiftKey = true
  }
  window.dispatchEvent(
    new KeyboardEvent('keydown', { key, bubbles: true, ...mods })
  )
}

// Wait for the debounced (250ms) SetOpenTabs write to flush after a mutation.
function awaitPersist(): Promise<unknown> {
  return waitFor(() => expect(bindings.SetOpenTabs).toHaveBeenCalled())
}

describe('tab manager (golden master)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    resetNotifications()
    clearTaskPageRoute()
    resetTaskHubState()
    bindings.IsVaultInitialized.mockResolvedValue(false)
    bindings.GetOpenTabs.mockResolvedValue({ open_tabs: [], active_tab: null })
  })

  afterEach(() => cleanup())

  it('hydrates pinned tabs from GetOpenTabs and syncs the active triple', async () => {
    bindings.GetOpenTabs.mockResolvedValue({
      open_tabs: [
        { notebook: 'nb', section: 's', page: 'p1', view_mode: '' },
        { notebook: 'nb', section: 's', page: 'p2', view_mode: '' }
      ],
      active_tab: { notebook: 'nb', section: 's', page: 'p1', view_mode: '' }
    })
    await mountApp()
    await waitFor(() => expect(bindings.GetOpenTabs).toHaveBeenCalled())
    // Hydration does not itself record recents (only an explicit open does).
    expect(bindings.RecordRecentPage).not.toHaveBeenCalled()
    // A subsequent close reduces the set from 2 → 1, proving the tabs loaded.
    bindings.SetOpenTabs.mockClear()
    press('Ctrl+W')
    await awaitPersist()
    const [tabs] = bindings.SetOpenTabs.mock.calls.at(-1)!
    expect(tabs).toHaveLength(1)
  })

  it('the .silt-stray guard removes a stray standalone-task tab and persists the cleanup', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    bindings.GetOpenTabs.mockResolvedValue({
      // A `.silt` notebook tab must never survive into openTabs; the guard
      // $effect drops it and re-persists.
      open_tabs: [
        { notebook: 'nb', section: 's', page: 'real', view_mode: '' },
        { notebook: '.silt', section: '', page: 'tasks', view_mode: '' }
      ],
      active_tab: { notebook: 'nb', section: 's', page: 'real', view_mode: '' }
    })
    await mountApp()
    await waitFor(() => expect(bindings.GetOpenTabs).toHaveBeenCalled())
    // The guard fires on the openTabs mutation. (Svelte's own $state-proxy
    // meta-warning may interleave, so search all calls for the guard message.)
    await waitFor(() => expect(warn).toHaveBeenCalled())
    expect(
      warn.mock.calls.some((c) => /\.silt tab was added/.test(String(c[0])))
    ).toBe(true)
    // The cleanup re-persists the cleaned list (stray dropped).
    await awaitPersist()
    const [tabs] = bindings.SetOpenTabs.mock.calls.at(-1)!
    expect(tabs.every((t) => t.notebook !== '.silt')).toBe(true)
    expect(tabs).toHaveLength(1)
    warn.mockRestore()
  })

  it('persists only pinned tabs + the active tab, with view_mode only for source', async () => {
    bindings.GetOpenTabs.mockResolvedValue({
      open_tabs: [
        { notebook: 'nb', section: 's', page: 'p1', view_mode: 'source' },
        { notebook: 'nb', section: 's', page: 'p2', view_mode: '' }
      ],
      active_tab: {
        notebook: 'nb',
        section: 's',
        page: 'p1',
        view_mode: 'source'
      }
    })
    await mountApp()
    await waitFor(() => expect(bindings.GetOpenTabs).toHaveBeenCalled())
    // A mutation triggers the debounced persist, whose payload shape is the
    // cross-component contract: pinned tabs + active, view_mode '' unless source.
    bindings.SetOpenTabs.mockClear()
    press('Ctrl+W') // close the active (p1) → p2 becomes active
    await awaitPersist()
    const [tabs, active] = bindings.SetOpenTabs.mock.calls.at(-1)!
    // Every persisted tab carries the locator + a view_mode field.
    for (const t of tabs) {
      expect(t).toHaveProperty('notebook')
      expect(t).toHaveProperty('section')
      expect(t).toHaveProperty('page')
      expect(t).toHaveProperty('view_mode')
    }
    // The new active tab is p2 (edit → empty view_mode string).
    expect(active?.page).toBe('p2')
    expect(active?.view_mode).toBe('')
  })

  it('cycles within the active notebook only (per-notebook displayedTabs scoping)', async () => {
    bindings.GetOpenTabs.mockResolvedValue({
      open_tabs: [
        { notebook: 'nb1', section: 's', page: 'p1', view_mode: '' },
        { notebook: 'nb1', section: 's', page: 'p2', view_mode: '' },
        { notebook: 'nb2', section: 's', page: 'p3', view_mode: '' }
      ],
      active_tab: { notebook: 'nb1', section: 's', page: 'p1', view_mode: '' }
    })
    await mountApp()
    await waitFor(() => expect(bindings.GetOpenTabs).toHaveBeenCalled())
    // Ctrl+Tab cycles within nb1 (p1 → p2); it must NOT jump to nb2's p3.
    bindings.SetOpenTabs.mockClear()
    press('Ctrl+Tab')
    await awaitPersist()
    let [, active] = bindings.SetOpenTabs.mock.calls.at(-1)!
    expect(active?.page).toBe('p2')

    // Another cycle wraps back to p1 (still within nb1).
    bindings.SetOpenTabs.mockClear()
    press('Ctrl+Tab')
    await awaitPersist()
    ;[, active] = bindings.SetOpenTabs.mock.calls.at(-1)!
    expect(active?.page).toBe('p1')
  })

  it('toggle_view_mode flips the active tab edit↔source and persists the mode', async () => {
    bindings.GetOpenTabs.mockResolvedValue({
      open_tabs: [{ notebook: 'nb', section: 's', page: 'p1', view_mode: '' }],
      active_tab: { notebook: 'nb', section: 's', page: 'p1', view_mode: '' }
    })
    await mountApp()
    await waitFor(() => expect(bindings.GetOpenTabs).toHaveBeenCalled())
    bindings.SetOpenTabs.mockClear()
    press('Ctrl+Shift+V')
    await awaitPersist()
    const [tabs] = bindings.SetOpenTabs.mock.calls.at(-1)!
    expect(tabs[0].view_mode).toBe('source')
  })
})
