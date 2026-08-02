import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// The three format/view toggles are imported directly by the controller from
// the settings store (no shell-state dependency). Stub them so the switch fan-
// out is observable.
const storeMocks = vi.hoisted(() => ({
  toggleFormatToolbar: vi.fn().mockResolvedValue(undefined),
  toggleFocusMode: vi.fn().mockResolvedValue(undefined),
  toggleTypewriterMode: vi.fn().mockResolvedValue(undefined)
}))

vi.mock('../settings/store.svelte', () => ({
  toggleFormatToolbar: storeMocks.toggleFormatToolbar,
  toggleFocusMode: storeMocks.toggleFocusMode,
  toggleTypewriterMode: storeMocks.toggleTypewriterMode,
  settings: { config: null },
  loadConfig: vi.fn()
}))

import {
  createGlobalHotkeyDispatch,
  type GlobalHotkeyDispatchDeps
} from './useGlobalHotkeyDispatch.svelte'

// Build a deps bag where every callback is a spy. getHotkeys returns a chord
// map; the rest read state the controller can't own.
function makeDeps(overrides: Partial<GlobalHotkeyDispatchDeps> = {}): {
  deps: GlobalHotkeyDispatchDeps
  spies: Record<string, (...args: unknown[]) => unknown>
} {
  const spies: Record<string, (...args: unknown[]) => unknown> = {
    toggleSearch: vi.fn(),
    toggleQuickSwitcher: vi.fn(),
    toggleGlobalReplace: vi.fn(),
    toggleQuickAdd: vi.fn(),
    setSidebarCollapsed: vi.fn(),
    toggleShortcutHelp: vi.fn(),
    toggleDateGlance: vi.fn(),
    openFind: vi.fn(),
    openReplace: vi.fn(),
    cycleView: vi.fn(),
    openTemplatePicker: vi.fn(),
    requestNavigationCreation: vi.fn(),
    openSettings: vi.fn(),
    toggleViewMode: vi.fn(),
    togglePropertiesPanel: vi.fn(),
    closeTab: vi.fn(),
    cycleTab: vi.fn()
  }
  let sidebarCollapsed = false
  const deps: GlobalHotkeyDispatchDeps = {
    getHotkeys: () => ({
      open_search: 'Ctrl+K',
      new_page: 'Ctrl+N',
      new_section: 'Ctrl+Shift+N',
      new_notebook: 'Ctrl+Shift+M',
      open_quick_switcher: 'Ctrl+P',
      open_shortcuts_help: 'Ctrl+/',
      open_date_glance: 'Ctrl+D',
      find_in_page: 'Ctrl+F',
      replace: 'Ctrl+H',
      global_replace: 'Ctrl+Shift+H',
      toggle_sidebar: 'Ctrl+E',
      focus_sidebar: 'Ctrl+0',
      cycle_view_layout: 'Ctrl+L',
      open_template_picker: 'Ctrl+T',
      new_task: 'Ctrl+J',
      toggle_view_mode: 'Ctrl+Shift+V',
      toggle_format_toolbar: 'Ctrl+Shift+B',
      toggle_focus_mode: 'Ctrl+Shift+L',
      toggle_typewriter_mode: 'Ctrl+Shift+T',
      open_settings: 'Ctrl+,',
      next_tab: 'Ctrl+Tab',
      prev_tab: 'Ctrl+Shift+Tab',
      close_tab: 'Ctrl+W'
    }),
    getHasDisplayedTabs: () => true,
    getActiveTabId: () => 'tab-1',
    isActiveTabDisplayed: () => true,
    getSidebarCollapsed: () => sidebarCollapsed,
    setSidebarCollapsed: (v: boolean) => {
      sidebarCollapsed = v
      spies.setSidebarCollapsed(v)
    },
    toggleSearch: spies.toggleSearch,
    toggleQuickSwitcher: spies.toggleQuickSwitcher,
    toggleGlobalReplace: spies.toggleGlobalReplace,
    toggleQuickAdd: spies.toggleQuickAdd,
    toggleShortcutHelp: spies.toggleShortcutHelp,
    toggleDateGlance: spies.toggleDateGlance,
    openFind: spies.openFind,
    openReplace: spies.openReplace,
    cycleView: spies.cycleView,
    openTemplatePicker: spies.openTemplatePicker,
    requestNavigationCreation: spies.requestNavigationCreation,
    openSettings: spies.openSettings,
    toggleViewMode: spies.toggleViewMode,
    togglePropertiesPanel: spies.togglePropertiesPanel,
    closeTab: spies.closeTab,
    cycleTab: spies.cycleTab,
    ...overrides
  }
  return { deps, spies }
}

function press(key: string, mods: KeyboardEventInit = {}): KeyboardEvent {
  return new KeyboardEvent('keydown', {
    key,
    bubbles: true,
    cancelable: true,
    ...mods
  })
}

describe('useGlobalHotkeyDispatch (#768)', () => {
  let dispatch: ReturnType<typeof createGlobalHotkeyDispatch>

  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    dispatch?.detach()
  })

  it('open_search fires toggleSearch and preventDefaults', () => {
    const { deps, spies } = makeDeps()
    dispatch = createGlobalHotkeyDispatch(deps)
    dispatch.attach()
    const e = press('k', { ctrlKey: true })
    window.dispatchEvent(e)
    expect(spies.toggleSearch).toHaveBeenCalledOnce()
    expect(e.defaultPrevented).toBe(true)
  })

  it('new_page routes to requestNavigationCreation("page")', () => {
    const { deps, spies } = makeDeps()
    dispatch = createGlobalHotkeyDispatch(deps)
    dispatch.attach()
    window.dispatchEvent(press('n', { ctrlKey: true }))
    expect(spies.requestNavigationCreation).toHaveBeenCalledWith('page')
  })

  it('toggle_format_toolbar calls the store toggle directly', () => {
    const { deps } = makeDeps()
    dispatch = createGlobalHotkeyDispatch(deps)
    dispatch.attach()
    window.dispatchEvent(press('b', { ctrlKey: true, shiftKey: true }))
    expect(storeMocks.toggleFormatToolbar).toHaveBeenCalledOnce()
  })

  it('toggle_sidebar writes the inverse of the current collapse flag', () => {
    const { deps, spies } = makeDeps()
    dispatch = createGlobalHotkeyDispatch(deps)
    dispatch.attach()
    window.dispatchEvent(press('e', { ctrlKey: true }))
    expect(spies.setSidebarCollapsed).toHaveBeenCalledWith(true)
  })

  it('close_tab fires only when the active tab is displayed', () => {
    const { deps, spies } = makeDeps()
    dispatch = createGlobalHotkeyDispatch(deps)
    dispatch.attach()
    window.dispatchEvent(press('w', { ctrlKey: true }))
    expect(spies.closeTab).toHaveBeenCalledWith('tab-1')
  })

  it('close_tab is suppressed when the active tab is not displayed', () => {
    const { deps, spies } = makeDeps({ isActiveTabDisplayed: () => false })
    dispatch = createGlobalHotkeyDispatch(deps)
    dispatch.attach()
    window.dispatchEvent(press('w', { ctrlKey: true }))
    expect(spies.closeTab).not.toHaveBeenCalled()
  })

  it('an unmatched chord fires no callback and does not preventDefault', () => {
    const { deps, spies } = makeDeps()
    dispatch = createGlobalHotkeyDispatch(deps)
    dispatch.attach()
    const e = press('q', { ctrlKey: true })
    window.dispatchEvent(e)
    expect(spies.toggleSearch).not.toHaveBeenCalled()
    expect(e.defaultPrevented).toBe(false)
  })

  it('detach removes the listener so no callback fires afterward', () => {
    const { deps, spies } = makeDeps()
    dispatch = createGlobalHotkeyDispatch(deps)
    dispatch.attach()
    dispatch.detach()
    window.dispatchEvent(press('k', { ctrlKey: true }))
    expect(spies.toggleSearch).not.toHaveBeenCalled()
  })
})
