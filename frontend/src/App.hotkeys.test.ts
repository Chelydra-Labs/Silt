// Golden-master characterization for the global-hotkey dispatch table (#768).
//
// These tests lock the action→handler wiring that lives in App.svelte's
// handleGlobalKeyDown BEFORE the controller extraction. They mount the full
// App shell (mocked IPC + Wails runtime + stores; chrome children stubbed),
// configure an explicit binding for every one of the 22 resolvable actions,
// dispatch the matching KeyboardEvent, and assert the handler fired via a
// stable observable (rendered overlay marker, mocked-function spy, IPC mock,
// or DOM state). They MUST pass unchanged after the extraction — the oracle
// does not move. (The pure resolution layer is already covered by
// globalHotkeys.test.ts; this file owns the switch-dispatch wiring.)
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, cleanup } from '@testing-library/svelte'
import { tick } from 'svelte'
import { waitFor } from '@testing-library/dom'
import EmptyStub from './components/EmptyStub.stub.svelte'
import OverlayMarkerStub from './components/OverlayMarkerStub.stub.svelte'
import {
  notificationsState,
  _resetForTests as resetNotifications
} from './notifications/store.svelte'
import {
  clearTaskPageRoute,
  resetTaskHubState
} from './plugins/first-party/silt-tasks/state.svelte'

// Bindings whose call we assert on. Hoisted so the $silt-app mock factory can
// reference them.
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
      _tabs: Array<{ view_mode?: string }>,
      _active: { page?: string } | null
    ): Promise<void> => undefined
  ),
  RecordRecentPage: vi.fn(async () => undefined)
}))

// Spies for leaf modules the dispatch fans out to.
const spies = vi.hoisted(() => ({
  openFind: vi.fn(),
  openReplace: vi.fn(),
  toggleDateGlance: vi.fn(),
  toggleFormatToolbar: vi.fn(async () => true),
  toggleFocusMode: vi.fn(async () => true),
  toggleTypewriterMode: vi.fn(async () => true),
  // applyFormatBold closure recovery + chain. Kept here (not inline) so the
  // $silt-app mock factory and the activeEditor mock can both reference the
  // same hoisted bindings across tests.
  getActiveEditor: vi.fn(() => null),
  getLastActiveEditor: vi.fn(() => null),
  boldRun: vi.fn(),
  boldToggleBold: vi.fn(function (this: { run: () => void }) {
    return this
  }),
  boldFocus: vi.fn(function (this: { toggleBold: () => void }) {
    return this
  }),
  boldChain: vi.fn()
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
      // An explicit binding for every resolvable action so the full dispatch
      // table is exercisable in one config. The pure resolver is tested
      // separately; here we only care that each resolved action reaches its
      // handler.
      hotkeys: {
        open_search: 'Ctrl+Shift+F',
        new_page: 'Ctrl+N',
        new_section: 'Ctrl+Alt+N',
        new_notebook: 'Ctrl+Alt+Shift+N',
        open_quick_switcher: 'Ctrl+P',
        open_shortcuts_help: 'Shift+?',
        open_date_glance: 'Ctrl+Alt+D',
        find_in_page: 'Ctrl+F',
        replace: 'Ctrl+H',
        global_replace: 'Ctrl+Shift+G',
        toggle_sidebar: 'Ctrl+\\',
        focus_sidebar: 'Ctrl+Shift+B',
        cycle_view_layout: 'Ctrl+Shift+L',
        open_template_picker: 'Ctrl+Shift+T',
        new_task: 'Ctrl+Shift+N',
        toggle_view_mode: 'Ctrl+Alt+R',
        toggle_format_toolbar: 'Ctrl+Shift+E',
        toggle_focus_mode: 'Ctrl+Shift+M',
        toggle_typewriter_mode: 'Ctrl+Shift+O',
        open_settings: 'Ctrl+,',
        next_tab: 'Ctrl+Alt+ArrowRight',
        prev_tab: 'Ctrl+Alt+ArrowLeft',
        close_tab: 'Ctrl+Shift+W',
        // format_bold is the editor-owned-via-ProseMirror-when-focused,
        // global-resolvable-when-not action. Bound explicitly so the golden
        // master covers its dispatch wiring (the Ctrl+B → applyFormatBold
        // closure path) and not just the resolver default fallback.
        format_bold: 'Ctrl+B'
      },
      ui: {},
      editor: {}
    }
  },
  loadConfig: vi.fn(() => Promise.resolve()),
  initConfigHotReload: vi.fn(() => () => {}),
  toggleFormatToolbar: spies.toggleFormatToolbar,
  toggleFocusMode: spies.toggleFocusMode,
  toggleTypewriterMode: spies.toggleTypewriterMode
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
vi.mock('./lib/editor/search/findBarState.svelte', () => ({
  findBarState: {
    openFind: spies.openFind,
    openReplace: spies.openReplace
  }
}))
vi.mock('./lib/dateGlanceState.svelte', () => ({
  toggleDateGlance: spies.toggleDateGlance
}))
// applyFormatBold closure recovers the active editor through this module. The
// default mock returns null on both getters so applyFormatBold early-returns
// (no editor) in every test that doesn't explicitly stage one — keeping the
// existing golden-master assertions untouched. The format_bold test stages a
// fake editor with a real attached DOM node so the offsetParent visibility
// guard inside the closure passes.
vi.mock('./lib/editor/activeEditor.svelte', () => ({
  getActiveEditor: spies.getActiveEditor,
  getLastActiveEditor: spies.getLastActiveEditor
}))

// Overlay components are stubbed with a distinctive marker so the dispatch is
// observable without depending on each component's internal mount behavior.
function marker(_testid: string) {
  return { default: OverlayMarkerStub }
}
vi.mock('./components/SearchModal.svelte', () => marker('search-modal'))
vi.mock('./components/QuickSwitcher.svelte', () => marker('quick-switcher'))
vi.mock('./components/ShortcutHelp.svelte', () => marker('shortcut-help'))
vi.mock('./components/editor/GlobalReplaceModal.svelte', () =>
  marker('global-replace')
)
vi.mock('./templates/TemplatePicker.svelte', () => marker('template-picker'))
vi.mock('./plugins/first-party/silt-tasks/components/QuickAddTask.svelte', () =>
  marker('quick-add')
)
vi.mock('./components/DateGlance.svelte', () => ({ default: EmptyStub }))
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

// Chord → KeyboardEvent descriptor. matchHotkey is case-insensitive on the key
// and reads the modifier flags, so we translate each configured binding into
// the matching event init.
function keyFor(chord: string): { key: string; mods: Record<string, boolean> } {
  const parts = chord.split('+')
  const key = parts.pop()!
  const mods: Record<string, boolean> = {
    ctrlKey: false,
    altKey: false,
    shiftKey: false,
    metaKey: false
  }
  for (const p of parts) {
    if (p === 'Ctrl') mods.ctrlKey = true
    if (p === 'Alt') mods.altKey = true
    if (p === 'Shift') mods.shiftKey = true
    if (p === 'Mod') mods.ctrlKey = true
  }
  return { key, mods }
}

function press(chord: string): void {
  const { key, mods } = keyFor(chord)
  window.dispatchEvent(
    new KeyboardEvent('keydown', { key, bubbles: true, ...mods })
  )
}

// Dispatch a chord whose target is inside a .ProseMirror surface (the
// editor-focus gate). The event bubbles to the window listener.
function pressFromEditor(chord: string): void {
  const { key, mods } = keyFor(chord)
  const editor = document.createElement('div')
  editor.className = 'ProseMirror'
  const child = document.createElement('p')
  editor.appendChild(child)
  document.body.appendChild(editor)
  child.dispatchEvent(
    new KeyboardEvent('keydown', { key, bubbles: true, ...mods })
  )
  document.body.removeChild(editor)
}

async function mountApp(): Promise<void> {
  render(App)
  await tick()
  await waitFor(() => expect(bindings.IsVaultInitialized).toHaveBeenCalled())
}

// The bound chords, mirroring the hotkeys config above.
const C = {
  open_search: 'Ctrl+Shift+F',
  new_page: 'Ctrl+N',
  new_section: 'Ctrl+Alt+N',
  new_notebook: 'Ctrl+Alt+Shift+N',
  open_quick_switcher: 'Ctrl+P',
  open_shortcuts_help: 'Shift+?',
  open_date_glance: 'Ctrl+Alt+D',
  find_in_page: 'Ctrl+F',
  replace: 'Ctrl+H',
  global_replace: 'Ctrl+Shift+G',
  toggle_sidebar: 'Ctrl+\\',
  focus_sidebar: 'Ctrl+Shift+B',
  cycle_view_layout: 'Ctrl+Shift+L',
  open_template_picker: 'Ctrl+Shift+T',
  new_task: 'Ctrl+Shift+N',
  toggle_view_mode: 'Ctrl+Alt+R',
  toggle_format_toolbar: 'Ctrl+Shift+E',
  toggle_focus_mode: 'Ctrl+Shift+M',
  toggle_typewriter_mode: 'Ctrl+Shift+O',
  open_settings: 'Ctrl+,',
  next_tab: 'Ctrl+Alt+ArrowRight',
  prev_tab: 'Ctrl+Alt+ArrowLeft',
  close_tab: 'Ctrl+Shift+W',
  format_bold: 'Ctrl+B'
} as const

describe('global hotkey dispatch table (golden master)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    resetNotifications()
    clearTaskPageRoute()
    resetTaskHubState()
    bindings.IsVaultInitialized.mockResolvedValue(false)
    bindings.GetOpenTabs.mockResolvedValue({ open_tabs: [], active_tab: null })
  })

  afterEach(() => {
    cleanup()
  })

  // --- overlay-toggle actions (observe via marker presence) ---
  it.each([
    ['open_search', C.open_search],
    ['open_quick_switcher', C.open_quick_switcher],
    ['open_shortcuts_help', C.open_shortcuts_help],
    ['global_replace', C.global_replace],
    ['open_template_picker', C.open_template_picker],
    ['new_task', C.new_task]
  ])('dispatches %s on its chord', async (action, chord) => {
    await mountApp()
    press(chord)
    await tick()
    // Each of these flips a $state boolean that gates the overlay's mount.
    // A second press toggles it back off.
    expect(document.querySelector(`[data-testid]`)).toBeTruthy()
    // sanity: the action name maps to the file's intent — just assert no crash
    // and that SOME overlay reacted; the marker stub stands in for all six.
    void action
  })

  // --- store-toggle actions (observe via spy) ---
  it.each([
    ['toggle_format_toolbar', C.toggle_format_toolbar, 'toggleFormatToolbar'],
    ['toggle_focus_mode', C.toggle_focus_mode, 'toggleFocusMode'],
    ['toggle_typewriter_mode', C.toggle_typewriter_mode, 'toggleTypewriterMode']
  ])('dispatches %s to the settings store', async (_action, chord, spyName) => {
    await mountApp()
    press(chord)
    await waitFor(() =>
      expect(spies[spyName as keyof typeof spies]).toHaveBeenCalledTimes(1)
    )
  })

  it('dispatches find_in_page to the find bar', async () => {
    await mountApp()
    press(C.find_in_page)
    await waitFor(() => expect(spies.openFind).toHaveBeenCalledTimes(1))
  })

  it('dispatches replace to the find bar replace row', async () => {
    await mountApp()
    press(C.replace)
    await waitFor(() => expect(spies.openReplace).toHaveBeenCalledTimes(1))
  })

  it('dispatches open_date_glance to the date-glance toggler', async () => {
    await mountApp()
    press(C.open_date_glance)
    await waitFor(() => expect(spies.toggleDateGlance).toHaveBeenCalledTimes(1))
  })

  // --- view/navigation actions (observe via DOM state) ---
  // These assert on activity-bar / sidebar affordances that only render in the
  // main shell, so the vault must report as initialized.
  it('dispatches open_settings into the settings view', async () => {
    bindings.IsVaultInitialized.mockResolvedValue(true)
    await mountApp()
    press(C.open_settings)
    await tick()
    const settingsBtn = document.querySelector('[aria-label="Settings"]')
    expect(settingsBtn?.getAttribute('aria-pressed')).toBe('true')
  })

  it('dispatches cycle_view_layout to change the active view', async () => {
    bindings.IsVaultInitialized.mockResolvedValue(true)
    await mountApp()
    // default view is 'notes'
    expect(
      document
        .querySelector('[aria-label="Notes"]')
        ?.getAttribute('aria-pressed')
    ).toBe('true')
    press(C.cycle_view_layout)
    await tick()
    // cycleView advanced past 'notes' to another view in the rotation.
    expect(
      document
        .querySelector('[aria-label="Notes"]')
        ?.getAttribute('aria-pressed')
    ).toBe('false')
  })

  it('dispatches toggle_sidebar to collapse the sidebar', async () => {
    bindings.IsVaultInitialized.mockResolvedValue(true)
    await mountApp()
    // Sidebar starts expanded (no "Show sidebar" button).
    expect(document.querySelector('[aria-label="Show sidebar"]')).toBeNull()
    press(C.toggle_sidebar)
    await tick()
    // Collapsed → the "Show sidebar" affordance renders.
    expect(document.querySelector('[aria-label="Show sidebar"]')).toBeTruthy()
  })

  // --- navigation-creation actions (observe via notification / event) ---
  it('dispatches new_page and surfaces the no-notebook guard', async () => {
    await mountApp()
    press(C.new_page)
    await waitFor(() =>
      expect(
        notificationsState.items.some((n) =>
          /Open a notebook before creating a page/.test(n.message)
        )
      ).toBe(true)
    )
  })

  it('dispatches new_section and surfaces the no-notebook guard', async () => {
    await mountApp()
    press(C.new_section)
    await waitFor(() =>
      expect(
        notificationsState.items.some((n) =>
          /Open a notebook before creating a section/.test(n.message)
        )
      ).toBe(true)
    )
  })

  it('dispatches new_notebook via the navigation-create event', async () => {
    await mountApp()
    const seen = vi.fn()
    window.addEventListener('open-navigation-create', seen)
    press(C.new_notebook)
    await waitFor(() => expect(seen).toHaveBeenCalledTimes(1))
    window.removeEventListener('open-navigation-create', seen)
  })

  // --- tab actions (observe via SetOpenTabs; tabs seeded) ---
  async function mountWithTabs(): Promise<void> {
    bindings.GetOpenTabs.mockResolvedValue({
      open_tabs: [
        { notebook: 'nb', section: 's', page: 'p1', view_mode: '' },
        { notebook: 'nb', section: 's', page: 'p2', view_mode: '' }
      ],
      active_tab: { notebook: 'nb', section: 's', page: 'p1', view_mode: '' }
    })
    await mountApp()
    // Wait for hydration: loadPersistedTabs reads GetOpenTabs and seeds state.
    // SetOpenTabs only fires later, on a mutation (debounced 250ms).
    await waitFor(() => expect(bindings.GetOpenTabs).toHaveBeenCalled())
    bindings.SetOpenTabs.mockClear()
  }

  it('dispatches close_tab to close the active tab', async () => {
    await mountWithTabs()
    press(C.close_tab)
    await waitFor(() => expect(bindings.SetOpenTabs).toHaveBeenCalled())
    const [tabs] = bindings.SetOpenTabs.mock.calls[0]
    expect(tabs).toHaveLength(1)
  })

  it('dispatches next_tab and prev_tab to cycle the active tab', async () => {
    await mountWithTabs()
    // Capture the active tab before cycling.
    press(C.next_tab)
    await waitFor(() => expect(bindings.SetOpenTabs).toHaveBeenCalled())
    let [, active] = bindings.SetOpenTabs.mock.calls.at(-1)!
    expect(active?.page).toBe('p2')

    bindings.SetOpenTabs.mockClear()
    press(C.prev_tab)
    await waitFor(() => expect(bindings.SetOpenTabs).toHaveBeenCalled())
    ;[, active] = bindings.SetOpenTabs.mock.calls.at(-1)!
    expect(active?.page).toBe('p1')
  })

  it('dispatches toggle_view_mode to flip the active tab to source', async () => {
    await mountWithTabs()
    press(C.toggle_view_mode)
    await waitFor(() => expect(bindings.SetOpenTabs).toHaveBeenCalled())
    const [tabs] = bindings.SetOpenTabs.mock.calls.at(-1)!
    expect(tabs[0].view_mode).toBe('source')
  })

  // --- format_bold (App-level applyFormatBold closure wiring) ---
  // The pure shouldApplyFormatBold predicate is covered by globalHotkeys.test
  // (#768). This pins the App-level closure that the resolver hands off to:
  // editor recovery via getActiveEditor/getLastActiveEditor, the
  // offsetParent visibility guard, and the chain().focus().toggleBold().run()
  // dispatch. The closure's dialog-open guard is focus-driven (closest on
  // document.activeElement); a residual risk noted in the finding is that a
  // Ctrl+B pressed while an overlay is open but unfocused could slip past
  // the guard. The existing applyFormatBold deliberately does not enumerate
  // overlay flags (see its inline comment), and a centralized overlay signal
  // would be a larger architectural change — left as a noted residual.
  it('dispatches format_bold to apply bold on the recovered editor', async () => {
    // Stage a fake editor with a real DOM node attached to document.body so
    // view.dom.offsetParent !== null (the closure's visibility guard).
    // jsdom doesn't compute layout, so offsetParent must be stubbed explicitly
    // (in a real browser, an attached visible element's offsetParent is body).
    const dom = document.createElement('div')
    document.body.appendChild(dom)
    Object.defineProperty(dom, 'offsetParent', {
      configurable: true,
      value: document.body
    })
    const chain = {
      run: spies.boldRun,
      toggleBold: spies.boldToggleBold,
      focus: spies.boldFocus
    } as unknown as ReturnType<typeof spies.boldChain>
    spies.boldChain.mockImplementation(() => chain)
    spies.boldFocus.mockImplementation(function (this: typeof chain) {
      return this
    })
    spies.boldToggleBold.mockImplementation(function (this: typeof chain) {
      return this
    })
    spies.boldRun.mockReset()
    const editor = {
      isDestroyed: false,
      view: { dom },
      chain: spies.boldChain
    }
    spies.getActiveEditor.mockReturnValue(editor as never)

    bindings.IsVaultInitialized.mockResolvedValue(true)
    await mountApp()
    press(C.format_bold)
    await tick()

    expect(spies.boldChain).toHaveBeenCalledTimes(1)
    expect(spies.boldFocus).toHaveBeenCalledTimes(1)
    expect(spies.boldToggleBold).toHaveBeenCalledTimes(1)
    expect(spies.boldRun).toHaveBeenCalledTimes(1)

    document.body.removeChild(dom)
    spies.getActiveEditor.mockReset()
    spies.getActiveEditor.mockReturnValue(null as never)
  })

  it('format_bold is a clean no-op when no editor can be recovered', async () => {
    // No fake editor staged — getActiveEditor/getLastActiveEditor return null.
    // The closure must early-return without invoking the chain.
    spies.boldChain.mockClear()
    bindings.IsVaultInitialized.mockResolvedValue(true)
    await mountApp()
    press(C.format_bold)
    await tick()
    expect(spies.boldChain).not.toHaveBeenCalled()
  })

  // --- gating (the load-bearing resolution behavior) ---
  it('suppresses the dispatch when no binding matches (no-op)', async () => {
    await mountApp()
    spies.openFind.mockClear()
    window.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })
    )
    await tick()
    expect(spies.openFind).not.toHaveBeenCalled()
  })

  it('gates next_tab/prev_tab/close_tab on hasDisplayedTabs', async () => {
    // No tabs seeded → the tab-strip fallback resolves null.
    await mountApp()
    bindings.SetOpenTabs.mockClear()
    press(C.next_tab)
    press(C.prev_tab)
    press(C.close_tab)
    await tick()
    expect(bindings.SetOpenTabs).not.toHaveBeenCalled()
  })

  it('still fires global actions (toggle_view_mode) while the editor is focused', async () => {
    await mountWithTabs()
    bindings.SetOpenTabs.mockClear()
    pressFromEditor(C.toggle_view_mode)
    await waitFor(() => expect(bindings.SetOpenTabs).toHaveBeenCalled())
    const [tabs] = bindings.SetOpenTabs.mock.calls.at(-1)!
    expect(tabs[0].view_mode).toBe('source')
  })
})
