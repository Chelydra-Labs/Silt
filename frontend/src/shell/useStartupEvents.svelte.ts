// Startup-events controller (#768).
//
// Owns every onMount-registered listener that wires the shell to backend /
// window events: the Wails Events.On subscriptions (config/plugins/vault
// lifecycle, settings + grants + quarantine dialogs, init warnings, re-mint,
// page-link rewrites, the native menu:* events), their named handlers, the
// dispatchStartupEvent replay router, and the MarkFrontendReady +
// GetStartupEvents backlog drain. Also owns the startup-lifecycle window
// addEventListener registrations (navigation, view-switch, overlay-open,
// page-renamed, change-vault, settings-jump). The window 'keydown' listener
// lives in useGlobalHotkeyDispatch (Task 2).
//
// Extracted via the proven createX(deps) factory idiom: leaf helpers (pure
// resolvers, IPC, store actions) are imported directly; App's $state and
// App-defined handlers are passed through the deps interface so the controller
// has no direct reference to App's runes. Host calls attach() at onMount init
// and dispose() in the onMount cleanup. dispose() invokes every captured
// per-listener unsubscribe (the offX returns from Events.On) — NEVER
// Events.Off(name), which in Wails v3 nukes ALL listeners for a name.
import { Events } from '@wailsio/runtime'
import { EventName } from '../generated/enums'
import { bindPageExternalReload } from '../lib/editor/editorRegistry.svelte'
import {
  MarkFrontendReady,
  GetStartupEvents,
  ResolveQuarantinedLinks
} from '../../bindings/silt/app.js'
import { pushNotification } from '../notifications/store.svelte'
import {
  loadConfig,
  toggleFormatToolbar,
  toggleFocusMode,
  type SystemConfig
} from '../settings/store.svelte'
import { loadPlugins } from '../plugins/loader'
import { refreshGrants } from '../plugins/grants.svelte'
import { revokeRevokedContributions } from '../plugins/reconcile'
import { clearRetainedTemplateDraft } from '../components/settings/templateDraftSession'
import {
  resolveSettingsSectionId,
  getSettingsSections
} from '../components/settings/settingsSections.svelte'
import {
  hasPageLocator,
  resolveSourceNavigationTarget,
  type SourceNavigationRef
} from '../lib/navigationTargets'
import { clearSelectionFocus } from '../plugins/ui-location'
import { clearAllEditors } from '../lib/editor/editorRegistry.svelte'
import { routeJumpTarget, isStandaloneTaskRef } from '../lib/standaloneTasksNav'
import { enterTaskPageRoute } from '../plugins/first-party/silt-tasks/state.svelte'
import { reMintToast, type ReMintWarning } from '../lib/reMintToast'
import { findBarState } from '../lib/editor/search/findBarState.svelte'
import {
  OPEN_TASKS_FOR_PAGE_EVENT,
  type OpenTasksForPageDetail
} from '../components/editor/openTasksForPage'
import type { NavigationCatalogItem } from '../lib/navigationCatalog'
import type { TabManagerController } from '../lib/tabs/useTabManager.svelte'
import type { SettingsDialogsController } from './useSettingsDialogs.svelte'

export interface StartupEventsDeps {
  // Navigation triple (3-level: notebook > section > page) + active view.
  getActiveNotebook: () => string
  getActiveSection: () => string
  getActivePage: () => string
  setActiveNotebook: (nb: string) => void
  setActiveSection: (sec: string) => void
  setActivePage: (pg: string) => void
  setActiveView: (view: string) => void
  // Per-vault config (plugin-reload diff + tab-rehydrate baseline).
  getSettings: () => SystemConfig | null | undefined
  // Settings-section id (settings-jump chip dispatch).
  setSettingsSection: (id: string) => void
  // Overlay open-state (search / quick-add / template-picker / tag).
  setShowSearch: (v: boolean) => void
  setShowQuickAdd: (v: boolean) => void
  getShowTemplatePicker: () => boolean
  setShowTemplatePicker: (v: boolean) => void
  setTemplatePickerMode: (m: 'new-page' | 'insert') => void
  setSelectedTag: (t: string) => void
  // Sidebar collapse (menu:toggle-sidebar reads + mirrors to manuallyCollapsed).
  getSidebarCollapsed: () => boolean
  setSidebarCollapsed: (collapsed: boolean) => void
  // Block-heading scroll target (navigate-to-page optional heading).
  setSearchTargetHeading: (h: string) => void
  setSearchTargetKey: (k: string) => void
  // Live navigation catalog (navigate-to-block/page source resolution).
  getNavigationCatalog: () => NavigationCatalogItem[]
  // Composed controllers.
  settingsDialogs: SettingsDialogsController
  tabManager: TabManagerController
  // App-defined shell entry points (closures over App state).
  openSettings: (section?: string) => void
  openTasksView: (blockId: string | undefined) => void
  handleSwitchVault: () => Promise<void>
  handleMenuSave: () => Promise<void>
  handleSearchJump: (
    locator: SourceNavigationRef,
    date: string,
    blockId: string
  ) => void
}

export interface StartupEventsController {
  /** Register every listener and drain the startup-event backlog. */
  attach: () => void
  /** Remove every listener captured by attach(). */
  dispose: () => void
}

// Order-independent string-array equality (the disabled list is a set
// semantically — config.yaml can re-order it without changing meaning).
function arraysEqual(a: readonly string[], b: readonly string[]): boolean {
  if (a.length !== b.length) return false
  const setA = new Set(a)
  return b.every((x) => setA.has(x))
}

export function createStartupEvents(
  deps: StartupEventsDeps
): StartupEventsController {
  // Per-listener unsubscribe captures. Each Events.On call returns a cancel
  // function; dispose() invokes them. (Events.Off(name) would remove every
  // listener for a name — never use it here.)
  let off: Array<() => void> = []
  // Captured window listeners for removeEventListener on dispose.
  let windowListeners: Array<[string, EventListener]> = []
  // Set by dispose(); checked between awaits in the startup-replay drain so a
  // stale handler can't fan queued events into dialogs/notifications after the
  // controller has torn down (HMR / fast vault-switch at launch).
  let disposed = false

  // --- config:changed: plugin-reload diff + tab rehydrate -----------------
  // prevDisabled is captured at attach() so the first event after mount
  // compares against the boot config.
  let prevDisabled: string[] = []

  function handleConfigChanged(ev: { data: SystemConfig }): void {
    const cfg = ev.data
    const next = cfg?.plugins?.disabled ?? []
    if (!arraysEqual(prevDisabled, next)) {
      prevDisabled = [...next]
      loadPlugins(
        deps.getActiveNotebook(),
        deps.getActiveSection(),
        deps.getActivePage()
      ).catch((e) =>
        console.error('Plugin reload after config change failed:', e)
      )
    }
    // Re-hydrate / reconcile tabs from an external ui.open_tabs edit.
    // tabSetKey is intentionally locator-only: a view-mode change must NOT
    // trigger a full re-hydrate (that would rebuild tabs and remount editors
    // on every in-app toggle, since the frontend's own persistTabs write also
    // fires config:changed). Logic lives in the persistence module.
    deps.tabManager.handleConfigChangedTabRehydrate(cfg)
  }

  // Refresh grants BEFORE re-running discovery so re-registration reads the
  // updated capabilities (fixes the stale-grant-cache race), then drop
  // contributions from plugins that lost a capability (#582). This is the
  // single orchestrator for plugins:changed.
  async function handlePluginsChanged(): Promise<void> {
    // refreshGrants can reject; without this guard the rejection is unhandled
    // and the downstream revoke + reload silently never run (the plugin-reload
    // half of plugins:changed is lost with no log).
    try {
      await refreshGrants()
      revokeRevokedContributions()
      loadPlugins(
        deps.getActiveNotebook(),
        deps.getActiveSection(),
        deps.getActivePage()
      ).catch((e) => console.error('Plugin reload failed:', e))
    } catch (e) {
      console.error('Plugin change handling failed:', e)
    }
  }

  // --- dialog / notification handlers (shared by live + replay paths) -----
  function handleSettingsMismatch(): void {
    deps.settingsDialogs.openSettingsMismatch()
  }
  function handleGrantsMigration(
    grants: Record<string, Record<string, string>>
  ): void {
    deps.settingsDialogs.openGrantsMigration(grants)
  }
  async function handleLinkedQuarantined(): Promise<void> {
    try {
      deps.settingsDialogs.setQuarantinedLinks(await ResolveQuarantinedLinks())
    } catch (e) {
      console.error('ResolveQuarantinedLinks failed:', e)
    }
  }
  function handleVaultInitError(msg: string): void {
    pushNotification({
      kind: 'error',
      message: `Vault failed to initialize: ${msg}`,
      autoDismissMs: 0
    })
  }
  function handleVaultInitWarnings(warnings: string[]): void {
    if (!warnings?.length) return
    pushNotification({
      kind: 'info',
      message: `Vault initialized with warnings: ${warnings.join('; ')}`,
      autoDismissMs: 0
    })
  }
  function handleVaultWatchCoverage(failedPaths: string[]): void {
    if (!failedPaths?.length) return
    pushNotification({
      kind: 'info',
      message: `File watching unavailable for ${failedPaths.length} path(s). External edits to these folders won't auto-sync.`,
      autoDismissMs: 0
    })
  }

  // The replay router: fans a queued startup event out to the SAME named
  // handler its live Events.On listener uses, so a startup event is
  // indistinguishable from a live one to the handler.
  function dispatchStartupEvent(name: string, data: unknown): void {
    switch (name) {
      case EventName.EventSettingsFingerprintMismatch:
        handleSettingsMismatch()
        break
      case EventName.EventGrantsMigrationRequired:
        handleGrantsMigration(data as Record<string, Record<string, string>>)
        break
      case EventName.EventLinkedNotebookQuarantined:
        void handleLinkedQuarantined()
        break
      case EventName.EventVaultInitError:
        handleVaultInitError(data as string)
        break
      case EventName.EventVaultInitWarnings:
        handleVaultInitWarnings(data as string[])
        break
      case EventName.EventVaultWatchCoverage:
        handleVaultWatchCoverage(data as string[])
        break
      default:
        break
    }
  }

  // --- window-event handlers ----------------------------------------------
  function handleOpenSettings(e: Event): void {
    const detail = (e as CustomEvent).detail
    // ctx.openSettings dispatches detail: tab ?? '' — empty/missing = general.
    const section = typeof detail === 'string' && detail ? detail : 'general'
    deps.openSettings(section)
  }
  function handleOpenSearch(): void {
    deps.setShowSearch(true)
  }
  function handleOpenQuickAdd(): void {
    deps.setShowQuickAdd(true)
  }
  function handleOpenTasksForPage(
    e: CustomEvent<OpenTasksForPageDetail>
  ): void {
    const target = e.detail
    if (
      !target?.source ||
      !target.notebook ||
      !target.page ||
      !target.nonce ||
      isStandaloneTaskRef(target.notebook)
    )
      return
    enterTaskPageRoute(target, {
      displayMode: 'list',
      activeFilter: 'all',
      filters: { owners: [], priorities: [], dueDate: '', tags: [] }
    })
    deps.openTasksView(undefined)
  }
  function handleSettingsJump(e: Event): void {
    const detail = (e as CustomEvent).detail
    if (!detail || typeof detail.section !== 'string') return
    // Validate against the live section registry so a typo'd id from a future
    // dispatcher can't render an empty panel; falls back to 'general'.
    deps.setSettingsSection(
      resolveSettingsSectionId(
        detail.section,
        getSettingsSections().map((s) => s.id)
      )
    )
  }
  // Incomplete notebook/page used to open empty chrome with no feedback (#877).
  // Toast only at the bus edge; handleSearchJump warns without a second toast.
  function rejectIncompletePageLocator(
    eventName: 'navigate-to-block' | 'navigate-to-page',
    d: {
      notebook?: unknown
      section?: unknown
      page?: unknown
      blockId?: unknown
    }
  ): boolean {
    if (hasPageLocator(d)) return false
    console.warn(`${eventName} ignored: missing notebook/page`, {
      notebook: d.notebook,
      section: d.section,
      page: d.page,
      blockId: d.blockId
    })
    const target = eventName === 'navigate-to-block' ? 'block' : 'page'
    pushNotification({
      kind: 'info',
      message: `Couldn't open that ${target} — the link is missing page information.`,
      autoDismissMs: 5000
    })
    return true
  }

  function handleNavigateToBlock(e: Event): void {
    // Normalize missing detail so null/undefined fail loud like incomplete objects.
    const d = (e as CustomEvent).detail ?? {}
    if (rejectIncompletePageLocator('navigate-to-block', d)) return
    // resolveSourceNavigationTarget walks the (possibly-malformed/external)
    // navigation catalog; a throw here would propagate into the window-event
    // dispatch loop and silently drop the navigation. Catch + log instead.
    try {
      const ref = resolveSourceNavigationTarget(deps.getNavigationCatalog(), {
        source: d.source,
        notebook: d.notebook,
        section: d.section ?? '',
        page: d.page
      })
      // Standalone-task routing guard: a `.silt` notebook ref routes to the
      // Tasks view instead of a raw page tab.
      const target = routeJumpTarget({
        notebook: ref.notebook,
        section: ref.section,
        page: ref.page,
        blockTarget: d.blockId ? { blockId: d.blockId } : undefined
      })
      if (target.kind === 'tasks-view') {
        deps.openTasksView(target.blockTarget?.blockId)
        return
      }
      deps.handleSearchJump(ref, d.date, d.blockId)
    } catch (err) {
      console.error('navigate-to-block failed:', err)
    }
  }
  function handleNavigateToPage(e: Event): void {
    const d = (e as CustomEvent).detail ?? {}
    if (rejectIncompletePageLocator('navigate-to-page', d)) return
    try {
      const ref = resolveSourceNavigationTarget(deps.getNavigationCatalog(), {
        source: d.source,
        notebook: d.notebook,
        section: d.section ?? '',
        page: d.page
      })
      deps.handleSearchJump(ref, d.date ?? '', d.blockId ?? '')
      if (d.heading) {
        deps.setSearchTargetHeading(d.heading)
        deps.setSearchTargetKey(`heading:${d.heading}:${Date.now()}`)
      }
    } catch (err) {
      console.error('navigate-to-page failed:', err)
    }
  }
  function handleNavigateToTag(e: Event): void {
    const tagPath = (e as CustomEvent).detail
    if (tagPath) {
      deps.setSelectedTag(tagPath)
      deps.setActiveView('tags')
    }
  }
  function handleSwitchView(e: Event): void {
    const detail = (e as CustomEvent).detail
    if (typeof detail === 'string' && detail) {
      deps.setActiveView(detail)
    }
  }
  function handleOpenPluginManager(): void {
    deps.openSettings('plugins')
  }
  function handleOpenTemplatePicker(): void {
    deps.setTemplatePickerMode('new-page')
    deps.setShowTemplatePicker(true)
  }
  function handlePageRenamed(e: Event): void {
    const detail = (e as CustomEvent).detail as {
      notebook: string
      section: string
      oldName: string
      newName: string
    }
    deps.tabManager.pageRenamed(detail)
    // The active navigation triple mirrors the rename only when it points at
    // the renamed page (App navigation state, not tab state).
    if (
      deps.getActiveNotebook() === detail.notebook &&
      deps.getActiveSection() === detail.section &&
      deps.getActivePage() === detail.oldName
    ) {
      deps.setActivePage(detail.newName)
    }
  }
  function onChangeVault(): void {
    void deps.handleSwitchVault()
  }

  function attach(): void {
    prevDisabled = deps.getSettings()?.plugins?.disabled ?? []
    deps.tabManager.initBaseline(deps.getSettings()?.ui?.open_tabs)

    off.push(bindPageExternalReload())
    off.push(
      Events.On(EventName.EventConfigChanged, (ev) =>
        handleConfigChanged(ev as { data: SystemConfig })
      )
    )
    off.push(
      Events.On(
        EventName.EventPluginsChanged,
        () => void handlePluginsChanged()
      )
    )
    off.push(
      Events.On(EventName.EventVaultClosing, () => {
        clearRetainedTemplateDraft()
        deps.tabManager.invalidateRecentPages()
      })
    )
    // vault:moved: backend already reinitialized services at the new path;
    // reset navigation, drop editor reconciliation handles tied to the old
    // vault, and reload the vault-scoped config store.
    off.push(
      Events.On(EventName.EventVaultMoved, (ev) => {
        const e: { from?: string; to?: string; warning?: string } = ev.data
        deps.setActiveNotebook('')
        deps.setActiveSection('')
        deps.setActivePage('')
        deps.tabManager.resetTabs()
        deps.setActiveView('notes')
        clearSelectionFocus()
        clearAllEditors()
        loadConfig().catch((e) =>
          console.error('Post-move config reload failed:', e)
        )
        window.dispatchEvent(new CustomEvent('refresh-navigation'))
        if (e?.warning) {
          pushNotification({ kind: 'error', message: e.warning })
        }
      })
    )
    off.push(
      Events.On(
        EventName.EventSettingsFingerprintMismatch,
        handleSettingsMismatch
      )
    )
    off.push(
      Events.On(EventName.EventGrantsMigrationRequired, (ev) => {
        handleGrantsMigration(ev.data)
      })
    )
    off.push(
      Events.On(EventName.EventLinkedNotebookQuarantined, () => {
        void handleLinkedQuarantined()
      })
    )
    off.push(
      Events.On(EventName.EventVaultInitError, (ev) => {
        handleVaultInitError(ev.data)
      })
    )
    off.push(
      Events.On(EventName.EventVaultInitWarnings, (ev) => {
        handleVaultInitWarnings(ev.data)
      })
    )
    off.push(
      Events.On(EventName.EventVaultWatchCoverage, (ev) => {
        handleVaultWatchCoverage(ev.data)
      })
    )
    off.push(
      Events.On(EventName.EventIndexReMintWarning, (ev) => {
        const w: ReMintWarning = ev.data
        if (!w) return
        pushNotification(reMintToast(w, deps.tabManager.openPage))
      })
    )
    off.push(
      Events.On(EventName.EventPageLinksRewritten, (ev) => {
        const d = ev?.data as
          { rewritten?: number; failed?: number } | undefined
        if (!d) return
        const rewritten = d.rewritten ?? 0
        const failed = d.failed ?? 0
        if (failed > 0) {
          pushNotification({
            kind: 'error',
            message:
              rewritten > 0
                ? `Updated ${rewritten} linked page(s); ${failed} could not be rewritten.`
                : `Could not rewrite wiki-links in ${failed} page(s). Check the log for details.`,
            autoDismissMs: 0
          })
        } else if (rewritten > 0) {
          pushNotification({
            kind: 'info',
            message: `Updated wiki-links in ${rewritten} page(s).`,
            autoDismissMs: 4000
          })
        }
      })
    )

    // Native menu events — wire Go-side menu items to the same handlers the
    // keyboard shortcuts use so menu and hotkey actions are indistinguishable.
    off.push(
      Events.On(EventName.EventMenuNewPage, () => {
        deps.setTemplatePickerMode('new-page')
        deps.setShowTemplatePicker(!deps.getShowTemplatePicker())
      })
    )
    off.push(
      Events.On(EventName.EventMenuOpenVault, () => {
        void deps.handleSwitchVault()
      })
    )
    off.push(
      Events.On(EventName.EventMenuSave, () => void deps.handleMenuSave())
    )
    off.push(
      Events.On(EventName.EventMenuToggleSidebar, () => {
        deps.setSidebarCollapsed(!deps.getSidebarCollapsed())
      })
    )
    off.push(
      Events.On(
        EventName.EventMenuToggleFormatToolbar,
        () => void toggleFormatToolbar()
      )
    )
    off.push(
      Events.On(EventName.EventMenuFind, () => {
        findBarState.openFind()
      })
    )
    off.push(
      Events.On(EventName.EventMenuFocusMode, () => void toggleFocusMode())
    )
    off.push(
      Events.On(EventName.EventMenuSettings, () => deps.openSettings('general'))
    )
    off.push(
      Events.On(EventName.EventMenuAbout, () => deps.openSettings('about'))
    )

    // Window events (DOM CustomEvents dispatched by the shell + plugins).
    const win: Array<[string, EventListener]> = [
      ['navigate-to-block', handleNavigateToBlock],
      ['navigate-to-page', handleNavigateToPage],
      ['navigate-to-tag', handleNavigateToTag],
      ['switch-view', handleSwitchView],
      ['open-plugin-manager', handleOpenPluginManager],
      ['open-settings', handleOpenSettings],
      ['open-template-picker', handleOpenTemplatePicker],
      ['open-search', handleOpenSearch],
      ['open-quick-add', handleOpenQuickAdd],
      [OPEN_TASKS_FOR_PAGE_EVENT, handleOpenTasksForPage as EventListener],
      ['silt:change-vault', onChangeVault],
      ['silt:settings-jump', handleSettingsJump],
      ['page-renamed', handlePageRenamed]
    ]
    for (const [name, handler] of win) {
      window.addEventListener(name, handler)
      windowListeners.push([name, handler])
    }

    // Wails v3 fires ServiceStartup before the webview exists, so every
    // startup-time emit is lost — no JS listener was registered yet. The
    // backend stashes those via emitOrQueue; mark the frontend ready (stop
    // queueing), drain the queue, and replay each event through the same
    // named handler its live listener uses.
    void (async () => {
      try {
        await MarkFrontendReady()
        if (disposed) return
        const missed = await GetStartupEvents()
        if (disposed) return
        for (const ev of missed ?? []) {
          dispatchStartupEvent(ev.Name, ev.Payload)
        }
      } catch (e) {
        console.error('Startup event replay failed:', e)
      }
    })()
  }

  function dispose(): void {
    if (disposed) return
    disposed = true
    for (const [name, handler] of windowListeners) {
      window.removeEventListener(name, handler)
    }
    windowListeners = []
    for (const cancel of off) cancel()
    off = []
    // Drop the recents cache on teardown so a stale MRU can't survive a
    // remount / vault switch.
    deps.tabManager.invalidateRecentPages()
  }

  return { attach, dispose }
}
