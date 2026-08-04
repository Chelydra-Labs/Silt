// Global-hotkey dispatch controller (#768).
//
// Owns the window 'keydown' registration that resolves a config-driven chord
// (via the pure resolveGlobalHotkey) and switch-dispatches the resulting action
// to the shell's side effects. Resolution — the editor-focus guard and the
// first-match-wins mutual-exclusivity ordering — already lives in
// globalHotkeys.ts (unit-tested); this controller is only the wiring layer
// between a resolved action and the shell state it mutates.
//
// Extracted via the proven createX(deps) factory idiom (mirrors
// useEditorEvents): every side effect the switch fans out to is a callback on
// the deps interface, so the controller has no direct reference to App's $state
// or the tab manager. App calls attach() at onMount init and detach() in the
// onMount cleanup; detach removes the window listener.
import { tick } from 'svelte'
import { resolveGlobalHotkey } from './globalHotkeys'
// Leaf store toggles with no shell state dependency are imported directly so
// the deps interface stays focused on state the controller can't own.
import {
  toggleFormatToolbar,
  toggleFocusMode,
  toggleTypewriterMode
} from '../settings/store.svelte'

export interface GlobalHotkeyDispatchDeps {
  /** Live read of the resolved hotkey map (after defaults + user overrides). */
  getHotkeys: () => Record<string, string | undefined>
  /** Whether the active notebook has any displayed tabs (gates the
   *  tab-strip fallback actions next/prev/close). */
  getHasDisplayedTabs: () => boolean
  /** The active tab id (toggle_view_mode / close_tab target). */
  getActiveTabId: () => string
  /** Whether the active tab is in the displayed set (close_tab guard). */
  isActiveTabDisplayed: () => boolean
  /** Current sidebar collapsed flag (toggle_sidebar reads the inverse). */
  getSidebarCollapsed: () => boolean
  // Overlay toggles.
  toggleSearch: () => void
  toggleQuickSwitcher: () => void
  toggleGlobalReplace: () => void
  toggleQuickAdd: () => void
  // Sets BOTH the sidebar collapsed flag and the manuallyCollapsed mirror so
  // a programmatic expand doesn't clobber the user's collapse preference.
  setSidebarCollapsed: (collapsed: boolean) => void
  toggleShortcutHelp: () => void
  /** Caret placement when an editor is focused; chip otherwise (App picks). */
  toggleDateGlance: () => void
  openFind: () => void
  openReplace: () => void
  // Navigation / view.
  cycleView: () => void
  openTemplatePicker: () => void
  requestNavigationCreation: (kind: 'page' | 'section' | 'notebook') => void
  openSettings: () => void
  // Tab actions.
  toggleViewMode: (tabId: string) => void
  togglePropertiesPanel: () => void
  /** Whether the properties panel is mounted in the current view. The panel
   *  only lives in the editor-tab view, so the toggle must no-op elsewhere —
   *  otherwise it mutates `panelOpen` with no visible effect and the next time
   *  the editor opens the panel is in a surprising state. */
  isPropertiesPanelAvailable: () => boolean
  closeTab: (tabId: string) => void
  cycleTab: (dir: 1 | -1) => void
}

export interface GlobalHotkeyDispatchController {
  attach: () => void
  detach: () => void
}

/**
 * Build the global-hotkey dispatch. The window listener is registered by
 * attach() and removed by detach(); the host wires those into onMount init +
 * cleanup (mirrors useEditorEvents).
 */
export function createGlobalHotkeyDispatch(
  deps: GlobalHotkeyDispatchDeps
): GlobalHotkeyDispatchController {
  // Move keyboard focus into the active sidebar. Expands the sidebar if
  // collapsed, then focuses the first focusable element inside it. Not a
  // format shortcut, so it fires globally even while the editor is focused.
  async function focusSidebar(): Promise<void> {
    if (deps.getSidebarCollapsed()) {
      deps.setSidebarCollapsed(false)
    }
    await tick()
    // One rAF so the expand's width transition has started and the target is
    // laid out before we focus it.
    requestAnimationFrame(() => {
      const aside = document.querySelector<HTMLElement>('[data-sidebar]')
      if (!aside) return
      const focusable = aside.querySelector<HTMLElement>(
        'button:not([disabled]), input:not([disabled]), [href], [tabindex]:not([tabindex="-1"])'
      )
      focusable?.focus()
    })
  }

  function handleGlobalKeyDown(e: KeyboardEvent): void {
    const hotkeys = deps.getHotkeys()
    const eventTarget = e.target
    const editorFocused =
      eventTarget instanceof Element && !!eventTarget.closest('.ProseMirror')
    const action = resolveGlobalHotkey(
      e,
      hotkeys,
      editorFocused,
      deps.getHasDisplayedTabs()
    )
    if (!action) return
    e.preventDefault()
    switch (action) {
      case 'open_search':
        deps.toggleSearch()
        break
      case 'new_page':
        deps.requestNavigationCreation('page')
        break
      case 'new_section':
        deps.requestNavigationCreation('section')
        break
      case 'new_notebook':
        deps.requestNavigationCreation('notebook')
        break
      case 'open_quick_switcher':
        deps.toggleQuickSwitcher()
        break
      case 'open_shortcuts_help':
        deps.toggleShortcutHelp()
        break
      case 'open_date_glance':
        deps.toggleDateGlance()
        break
      case 'find_in_page':
        deps.openFind()
        break
      case 'replace':
        deps.openReplace()
        break
      case 'global_replace':
        deps.toggleGlobalReplace()
        break
      case 'toggle_sidebar':
        deps.setSidebarCollapsed(!deps.getSidebarCollapsed())
        break
      case 'focus_sidebar':
        void focusSidebar()
        break
      case 'cycle_view_layout':
        deps.cycleView()
        break
      case 'open_template_picker':
        deps.openTemplatePicker()
        break
      case 'new_task':
        deps.toggleQuickAdd()
        break
      case 'toggle_view_mode':
        // Flip the active tab's view mode directly — no window-event
        // indirection, the tab manager owns the per-tab state.
        if (deps.getActiveTabId()) deps.toggleViewMode(deps.getActiveTabId())
        break
      case 'toggle_properties_panel':
        // Guarded: the panel is only mounted in the editor-tab view. Toggling
        // on a dashboard/other view would flip `panelOpen` with no effect and
        // resurface as a surprise when the user returns to the editor.
        if (deps.isPropertiesPanelAvailable()) deps.togglePropertiesPanel()
        break
      case 'toggle_format_toolbar':
        void toggleFormatToolbar()
        break
      case 'toggle_focus_mode':
        void toggleFocusMode()
        break
      case 'toggle_typewriter_mode':
        void toggleTypewriterMode()
        break
      case 'open_settings':
        deps.openSettings()
        break
      case 'next_tab':
        deps.cycleTab(1)
        break
      case 'prev_tab':
        deps.cycleTab(-1)
        break
      case 'close_tab':
        // Only close if the active tab is visible in the current notebook's
        // displayed set — closing a hidden tab from another notebook would be
        // surprising to the user.
        if (deps.isActiveTabDisplayed()) deps.closeTab(deps.getActiveTabId())
        break
    }
  }

  function attach(): void {
    window.addEventListener('keydown', handleGlobalKeyDown)
  }

  function detach(): void {
    window.removeEventListener('keydown', handleGlobalKeyDown)
  }

  return { attach, detach }
}
