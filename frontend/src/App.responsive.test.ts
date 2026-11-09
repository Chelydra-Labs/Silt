import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { tick } from 'svelte'
import { waitFor } from '@testing-library/dom'
import EmptyStub from './components/EmptyStub.stub.svelte'

const bindings = vi.hoisted(() => ({
  IsVaultInitialized: vi.fn(async () => true),
  GetOpenTabs: vi.fn(async () => ({ open_tabs: [], active_tab: null })),
  GetStartupEvents: vi.fn(async () => []),
  SetOpenTabs: vi.fn(async () => undefined),
  GetSidebarWidth: vi.fn(async () => 256),
  SetSidebarWidth: vi.fn(async () => undefined),
  ListNavigation: vi.fn(async () => ({ notebooks: [] })),
  GetNavigationPreferences: vi.fn(async () => ({})),
  ResolveQuarantinedLinks: vi.fn(async () => [])
}))

vi.mock('$silt-app', () => {
  const noop = () => Promise.resolve(undefined)
  return createAppIpcMocks({
    IsVaultInitialized: bindings.IsVaultInitialized,
    InitializeVault: vi.fn(noop),
    CloseVault: vi.fn(noop),
    GetSidebarWidth: bindings.GetSidebarWidth,
    SetSidebarWidth: bindings.SetSidebarWidth,
    GetOpenTabs: bindings.GetOpenTabs,
    SetOpenTabs: bindings.SetOpenTabs,
    ConfirmSettingsChange: vi.fn(noop),
    ConfirmGrantsMigration: vi.fn(noop),
    DeclineGrantsMigration: vi.fn(noop),
    ResolveQuarantinedLinks: bindings.ResolveQuarantinedLinks,
    PickLinkedNotebook: vi.fn(noop),
    UnlinkNotebook: vi.fn(noop),
    CreateStandaloneTask: vi.fn(async () => 'tsk'),
    MarkFrontendReady: vi.fn(noop),
    GetStartupEvents: bindings.GetStartupEvents,
    RecordRecentPage: vi.fn(async () => undefined),
    UpdateAIFeatures: vi.fn(noop),
    GetSettings: vi.fn(async () => ({})),
    ListNavigation: bindings.ListNavigation,
    GetNavigationPreferences: bindings.GetNavigationPreferences,
    ListPlugins: vi.fn(async () => []),
    GetGrantedCapabilities: vi.fn(async () => ({})),
    PluginRawQuery: vi.fn(async () => ({})),
    ListThemes: vi.fn(async () => []),
    ListTypes: vi.fn(async () => [])
  })
})

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn(() => () => {}),
    Emit: vi.fn()
  },
  Window: {
    SetTitle: vi.fn(),
    SetMinSize: vi.fn(),
    Minimise: vi.fn(),
    Maximise: vi.fn(),
    UnMaximise: vi.fn(),
    IsMaximised: vi.fn(async () => false),
    Close: vi.fn()
  },
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
  loadPlugins: vi.fn(() => Promise.resolve()),
  loadedPlugins: { plugins: new Map(), errors: [] }
}))
vi.mock('./settings/store.svelte', () => ({
  settings: {
    config: {
      hotkeys: { toggle_sidebar: 'Ctrl+B' },
      ui: {},
      editor: {}
    }
  },
  loadConfig: vi.fn(() => Promise.resolve()),
  initConfigHotReload: vi.fn(() => () => {})
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

describe('App narrow-window responsiveness & drawer overlay (#1075)', () => {
  const originalWidth = window.innerWidth

  beforeEach(() => {
    vi.clearAllMocks()
    window.innerWidth = 1024
  })

  afterEach(() => {
    cleanup()
    window.innerWidth = originalWidth
  })

  it('auto-collapses sidebar when initial window width is <= 700px', async () => {
    window.innerWidth = 650
    render(App)
    await tick()
    await waitFor(() => expect(bindings.IsVaultInitialized).toHaveBeenCalled())

    const sidebar = document.querySelector('aside[data-sidebar]')
    expect(sidebar).toBeInTheDocument()
    expect(sidebar?.getAttribute('style')).toContain('width: 0px')
  })

  it('auto-collapses sidebar when window resizes below 700px', async () => {
    window.innerWidth = 1024
    render(App)
    await tick()
    await waitFor(() => expect(bindings.IsVaultInitialized).toHaveBeenCalled())

    const sidebar = document.querySelector('aside[data-sidebar]')
    expect(sidebar?.getAttribute('style')).toContain('width: 256px')

    // Resize below 700px
    window.innerWidth = 650
    window.dispatchEvent(new Event('resize'))
    await tick()

    expect(sidebar?.getAttribute('style')).toContain('width: 0px')
  })

  it('allows manual toggle in narrow mode to open drawer with backdrop and dismiss on backdrop click', async () => {
    window.innerWidth = 650
    render(App)
    await tick()
    await waitFor(() => expect(bindings.IsVaultInitialized).toHaveBeenCalled())

    const sidebar = document.querySelector('aside[data-sidebar]')
    expect(sidebar?.getAttribute('style')).toContain('width: 0px')
    expect(sidebar).toHaveAttribute('data-drawer', 'true')

    // Click "Notes" in ActivityBar to open sidebar
    const notesBtn = screen.getByRole('button', { name: 'Notes' })
    await fireEvent.click(notesBtn)
    await tick()

    // Sidebar drawer opens and does NOT immediately re-collapse
    expect(sidebar?.getAttribute('style')).not.toContain('width: 0px')
    const backdrop = screen.getByRole('button', { name: 'Dismiss sidebar' })
    expect(backdrop).toBeInTheDocument()

    // Clicking backdrop dismisses drawer
    await fireEvent.click(backdrop)
    await tick()
    expect(sidebar?.getAttribute('style')).toContain('width: 0px')
    expect(screen.queryByRole('button', { name: 'Dismiss sidebar' })).toBeNull()
  })
})
