<script lang="ts">
  // Tasks hub shell (#424). One "Tasks" destination hosting a List / Board /
  // Calendar mode switcher over the unified hub state. The shell owns the
  // shared chrome — title + count header, mode segmented control, FilterBar,
  // scope breadcrumb — and routes to the active display-mode renderer in the
  // content area. Each renderer owns its own QuickAdd placement.
  //
  // Phase 1: List is the live renderer (views/ListView.svelte); Board and
  // Calendar render placeholder stubs until their issues land. Group-by /
  // sort selectors arrive with the grouping-engine issue (#423).
  import { onMount, untrack, tick } from 'svelte'
  import { fly } from 'svelte/transition'
  import type { PluginContext, PluginManifest } from '../../sdk'
  import FilterBar from './components/FilterBar.svelte'
  import ConfirmModal from './components/ConfirmModal.svelte'
  import TasksCommandPalette from './components/TasksCommandPalette.svelte'
  import ListView from './views/ListView.svelte'
  import BoardView from './views/BoardView.svelte'
  import CalendarView from './views/CalendarView.svelte'
  import { settings } from '../../../settings/store.svelte'
  import { matchHotkey } from '../../../settings/hotkeys'
  import {
    getTaskHubState,
    getTaskHubViewState,
    getTaskPageRoute,
    clearTaskPageRoute,
    setDisplayMode,
    setFilters,
    setScope,
    setGroupBy,
    setSort,
    setColumns,
    saveView,
    applySavedView,
    deleteSavedView,
    clearScopeOverride,
    narrowScopeTo,
    enterTasksFromMainNavigation,
    setWeekStart,
    type DisplayMode,
    type GroupBy,
    type SavedView,
    type Scope,
    type SortMode,
    type TaskFilters
  } from './state.svelte'
  import {
    loadDefaultDisplayMode,
    loadDefaultGroupBy,
    loadDefaultSort,
    loadColumns,
    loadSavedViews,
    persistSavedViews,
    initTasksSettings,
    reloadTasksSettings,
    persistDefaultDisplayMode,
    persistDefaultGroupBy,
    persistDefaultSort,
    loadWeekStart,
    persistWeekStart
  } from './settings'
  import type { WeekStart } from '../../../lib/dateGrid'
  import { getTaskWeekStart } from '../../../lib/taskWeekStart.svelte'
  import { cloneColumns, columnsEqual } from './columns'
  import { viewMatchesState } from './savedViews'
  import { motionDuration } from './motion'

  interface Props {
    ctx: PluginContext
    manifest?: PluginManifest
    focusBlockId?: string
    focusKey?: string
  }

  let { ctx, manifest, focusBlockId = '', focusKey = '' }: Props = $props()

  const MODES: { value: DisplayMode; label: string; icon: string }[] = [
    { value: 'list', label: 'List', icon: 'checklist' },
    { value: 'board', label: 'Board', icon: 'view_kanban' },
    { value: 'calendar', label: 'Calendar', icon: 'calendar_month' }
  ]

  let hubState = $derived(getTaskHubViewState())
  let weekStart = $derived(getTaskWeekStart())
  let pageRoute = $derived(getTaskPageRoute())
  let hubHeading = $state<HTMLHeadingElement | null>(null)
  let routeAnnouncement = $state('')
  let handledRouteNonce = $state('')
  let countsReady = $state(false)
  let routeWasActive = $state(false)
  let deferredSettingsHydration = $state<'initial' | 'refresh' | null>(null)
  let blockDeferredHydration = false
  let awaitingRouteFirstHydration = false
  let settingsSnapshotLoaded = false
  let settingsHydrationSeq = 0
  let navigationEntryHandled = false

  // Counts reported upward by the active renderer (List today).
  let openCount = $state(0)
  let doneCount = $state(0)
  function handleCountChange(open: number, done: number) {
    openCount = open
    doneCount = done
    countsReady = true
  }

  $effect(() => {
    const target = pageRoute?.target
    if (target) {
      routeWasActive = true
      if (target.nonce === handledRouteNonce) return
      handledRouteNonce = target.nonce
      countsReady = false
      routeAnnouncement = `Showing tasks from ${target.page}.`
      void tick().then(() => hubHeading?.focus())
      return
    }
    if (!routeWasActive) return
    routeWasActive = false
    untrack(flushDeferredSettingsHydration)
  })

  function exitPageRoute(): void {
    clearTaskPageRoute()
    routeAnnouncement = 'Page task context cleared.'
    void tick().then(() => hubHeading?.focus())
  }

  let pageRoutePath = $derived.by(() => {
    const target = pageRoute?.target
    if (!target) return ''
    return [target.notebook, target.section, target.page]
      .filter(Boolean)
      .join(' › ')
  })

  // --- Display mode ------------------------------------------------------
  // Hydrate from the persisted default once on mount; afterwards every user
  // switch is persisted. untrack so the initial set doesn't loop through the
  // $derived that reads hubState.displayMode.
  function hydrateSavedViews(): void {
    const state = getTaskHubState()
    const views = loadSavedViews()
    if (!views.length) return

    // The active view is session state. Keep it in the list when a remount
    // races a settings snapshot that has not caught up with the last write.
    const activeView = state.activeSavedViewId
      ? state.savedViews.find((view) => view.id === state.activeSavedViewId)
      : undefined
    if (
      activeView &&
      !views.some((view) => view.id === state.activeSavedViewId)
    ) {
      views.push(activeView)
    }
    state.savedViews = views
  }

  function hydrateInitialSettings(): void {
    setWeekStart(loadWeekStart())
    // A page route is an isolated projection over the user's base/saved view.
    // Settings can be cached while it is open, but must not rewrite that base.
    if (getTaskPageRoute()) return

    const state = getTaskHubState()
    const activeSavedView = state.activeSavedViewId !== ''
    if (!activeSavedView) {
      const persisted = loadDefaultDisplayMode()
      if (persisted !== state.displayMode) setDisplayMode(persisted)
      const persistedGroup = loadDefaultGroupBy()
      if (persistedGroup !== state.groupBy) setGroupBy(persistedGroup)
      const persistedSort = loadDefaultSort()
      if (persistedSort !== state.sort) setSort(persistedSort)
      const persistedCols = loadColumns()
      if (persistedCols.length && !columnsEqual(persistedCols, state.columns)) {
        setColumns(persistedCols)
      }
    }
    hydrateSavedViews()
    if (!activeSavedView) state.savedViewsDirty = false
  }

  function rehydrateFromSettings(): void {
    setWeekStart(loadWeekStart())
    const state = getTaskHubState()
    if (state.savedViewsDirty) return
    hydrateSavedViews()
    // An active saved view owns the effective dimensions until the user
    // changes or clears it; defaults must not replace that view on refresh.
    if (state.activeSavedViewId !== '') return
    const mode = loadDefaultDisplayMode()
    if (mode !== state.displayMode) setDisplayMode(mode)
    const group = loadDefaultGroupBy()
    if (group !== state.groupBy) setGroupBy(group)
    const sortVal = loadDefaultSort()
    if (sortVal !== state.sort) setSort(sortVal)
    const cols = loadColumns()
    if (cols.length && !columnsEqual(cols, state.columns)) setColumns(cols)
  }

  function applyOrDeferSettingsHydration(kind: 'initial' | 'refresh'): void {
    if (getTaskPageRoute()) {
      // Keep only the newest loaded snapshot. load* reads the module cache, so
      // a later refresh supersedes an initial route-first hydration.
      deferredSettingsHydration = kind
      return
    }
    if (awaitingRouteFirstHydration) {
      awaitingRouteFirstHydration = false
      const state = getTaskHubState()
      const baseWasDeliberatelyChanged =
        blockDeferredHydration ||
        state.savedViewsDirty ||
        state.activeSavedViewId !== ''
      blockDeferredHydration = false
      if (baseWasDeliberatelyChanged) return
    }
    untrack(kind === 'initial' ? hydrateInitialSettings : rehydrateFromSettings)
  }

  function flushDeferredSettingsHydration(): void {
    const kind = deferredSettingsHydration
    deferredSettingsHydration = null
    if (!kind) {
      if (!settingsSnapshotLoaded) {
        // Settings may still be in flight. The eventual snapshot must make the
        // same base-preservation decision this route exit would have made.
        awaitingRouteFirstHydration = true
      } else {
        blockDeferredHydration = false
      }
      return
    }
    const state = getTaskHubState()
    const baseWasDeliberatelyChanged =
      blockDeferredHydration ||
      state.savedViewsDirty ||
      state.activeSavedViewId !== ''
    blockDeferredHydration = false
    if (baseWasDeliberatelyChanged) return
    if (kind === 'initial') hydrateInitialSettings()
    else rehydrateFromSettings()
  }

  onMount(() => {
    let mounted = true
    const hydrationSeq = () => ++settingsHydrationSeq
    const reportSettingsFailure = (error: unknown) => {
      const detail = error instanceof Error ? error.message : String(error)
      const message = `Couldn't load task preferences${detail ? `: ${detail}` : ''}`
      weekStartError = message
      void ctx.notify({ title: 'Tasks', body: message }).catch(() => {})
    }
    const requestSettings = (
      load: () => Promise<boolean>,
      kind: 'initial' | 'refresh',
      onApplied?: () => void | Promise<void>
    ) => {
      const request = hydrationSeq()
      void load()
        .then((applied) => {
          if (!mounted || request !== settingsHydrationSeq || !applied) return
          settingsSnapshotLoaded = true
          applyOrDeferSettingsHydration(kind)
          void onApplied?.()
        })
        .catch((error: unknown) => {
          if (!mounted || request !== settingsHydrationSeq) return
          reportSettingsFailure(error)
        })
    }

    // Pull the settings slice through the SDK (per-active-notebook override
    // layer #133) before any load* read. initTasksSettings is async because
    // getPluginSettings hits the Go binding, so hydration + facet reload run
    // in its .then().
    requestSettings(() => initTasksSettings(ctx), 'initial', reloadFacets)

    // Subsequent external edits (e.g. co-located config.yaml change on a
    // linked notebook) arrive as config:changed. initTasksSettings already
    // got the latest config on mount; this re-hydrates after the re-read —
    // but never clobbers an in-session edit (savedViewsDirty wins).
    //
    // Navigation to a different notebook also triggers a reload because the
    // SDK's getPluginSettings resolves per-active-notebook overrides (#133):
    // a linked notebook with its own co-located config.yaml can carry
    // different columns / default modes / saved views.
    const unsubConfig = ctx.on('config:changed', () => {
      requestSettings(() => reloadTasksSettings(ctx), 'refresh')
    })
    const unsubNav = ctx.on('active-notebook:changed', () => {
      requestSettings(() => reloadTasksSettings(ctx), 'refresh')
    })
    return () => {
      mounted = false
      settingsHydrationSeq++
      unsubConfig()
      unsubNav()
    }
  })

  function chooseMode(mode: DisplayMode) {
    setDisplayMode(mode)
    if (!getTaskPageRoute()) void persistDefaultDisplayMode(mode)
  }

  function chooseGroupBy(g: GroupBy) {
    setGroupBy(g)
    if (!getTaskPageRoute()) void persistDefaultGroupBy(g)
  }

  function chooseSort(s: SortMode) {
    setSort(s)
    if (!getTaskPageRoute()) void persistDefaultSort(s)
  }

  // Week boundaries belong to Tasks rather than the app at large. Keep the
  // affordance in the hub header and write through the same per-vault plugin
  // settings path that hydrates the calendars and task queries.
  let preferencesOpen = $state(false)
  let preferencesButton = $state<HTMLButtonElement | null>(null)
  let weekStartError = $state('')
  let weekStartSaveSeq = 0

  function togglePreferences(): void {
    preferencesOpen = !preferencesOpen
    weekStartError = ''
    if (preferencesOpen) {
      closePopover()
      void tick().then(() => {
        document
          .querySelector<HTMLInputElement>(
            'input[name="tasks-week-start"]:checked'
          )
          ?.focus()
      })
    }
  }

  function closePreferences(returnFocus = false): void {
    if (!preferencesOpen) return
    preferencesOpen = false
    weekStartError = ''
    if (returnFocus) void tick().then(() => preferencesButton?.focus())
  }

  async function chooseWeekStart(value: WeekStart): Promise<void> {
    const previous = getTaskWeekStart()
    if (value === previous) return
    const saveSeq = ++weekStartSaveSeq
    weekStartError = ''
    setWeekStart(value)
    const saved = await persistWeekStart(value)
    if (saveSeq !== weekStartSaveSeq) return
    if (!saved) {
      setWeekStart(previous)
      weekStartError = 'Couldn’t save this preference. Try again.'
    }
  }

  // Hub-scoped command palette (#436). Opened by tasks_command_palette
  // (default Ctrl+K). Same chord as format_link — only fires when focus is
  // not an input/textarea/contenteditable/ProseMirror so the editor keeps
  // its link shortcut.
  let paletteOpen = $state(false)

  function isEditableTarget(t: EventTarget | null): boolean {
    if (!(t instanceof HTMLElement)) return false
    if (
      t instanceof HTMLInputElement ||
      t instanceof HTMLTextAreaElement ||
      t.isContentEditable
    )
      return true
    return !!t.closest?.('.ProseMirror')
  }

  // Ctrl+Shift+V cycles List → Board → Calendar → List. This chord is owned
  // SOLELY by the TasksHub display-mode cycle: the global toggle_view_mode was
  // relocated to Ctrl+Alt+R so the OS paste-without-formatting convention and
  // this hub cycle no longer collide with it on the same key. The
  // editable-target guard below keeps in-editor paste-plain working (the hub
  // never steals the keystroke mid-composition).
  function onGlobalKeydown(e: KeyboardEvent) {
    if (!(e.ctrlKey && e.shiftKey) || e.key !== 'V') return
    if (isEditableTarget(e.target)) return
    e.preventDefault()
    const order: DisplayMode[] = ['list', 'board', 'calendar']
    const idx = order.indexOf(getTaskHubViewState().displayMode)
    chooseMode(order[(idx + 1) % order.length])
  }

  function onPaletteHotkey(e: KeyboardEvent) {
    if (paletteOpen) return
    const hotkeys = settings.config?.hotkeys ?? {}
    if (!matchHotkey(e, hotkeys.tasks_command_palette)) return
    if (isEditableTarget(e.target)) return
    e.preventDefault()
    paletteOpen = true
  }

  // One always-on keydown listener dispatches mode-cycle, palette open, and
  // saved-view-popover escape (cheaper than three window-level listeners).
  function onWindowKeydown(e: KeyboardEvent) {
    onGlobalKeydown(e)
    onPaletteHotkey(e)
    onPopoverKeydown(e)
  }

  function handlePaletteFindTask() {
    // Delegate to the app-level SearchModal via the same CustomEvent pattern
    // as open-settings / open-template-picker (PLAN #436: actual open_search
    // path, not a invented Ctrl+P binding).
    window.dispatchEvent(new CustomEvent('open-search'))
  }

  function handlePaletteAddTask() {
    window.dispatchEvent(new CustomEvent('open-quick-add'))
  }

  function handlePaletteApplyView(view: SavedView) {
    if (getTaskPageRoute()) blockDeferredHydration = true
    applySavedView(view)
  }
  $effect(() => {
    window.addEventListener('keydown', onWindowKeydown)
    return () => window.removeEventListener('keydown', onWindowKeydown)
  })

  // WAI-ARIA radiogroup keyboard pattern for the mode switcher: ArrowLeft/
  // Right move between options (wrapping), Home/End jump to the boundaries.
  // Roving tabindex — the checked option is tabbable (0), others are not (-1).
  function onModeKeydown(e: KeyboardEvent) {
    if (!['ArrowRight', 'ArrowLeft', 'Home', 'End'].includes(e.key)) return
    e.preventDefault()
    const dir = e.key === 'ArrowLeft' || e.key === 'End' ? -1 : 1
    let start: number
    if (e.key === 'Home') start = 0
    else if (e.key === 'End') start = MODES.length - 1
    else
      start =
        (MODES.findIndex((m) => m.value === getTaskHubState().displayMode) +
          dir +
          MODES.length) %
        MODES.length
    chooseMode(MODES[start].value)
    ;(e.currentTarget as HTMLElement)
      .querySelector<HTMLElement>(`[data-mode="${MODES[start].value}"]`)
      ?.focus()
  }

  // --- Scope breadcrumb --------------------------------------------------
  function defaultScope(): Scope {
    if (ctx.activePage) return 'page'
    if (ctx.activeSection) return 'section'
    if (ctx.activeNotebook) return 'notebook'
    return 'vault'
  }
  function isScopeDisabled(s: string): boolean {
    if (s === 'notebook') return !ctx.activeNotebook
    if (s === 'section') return !ctx.activeSection
    if (s === 'page') return !ctx.activePage
    return false
  }

  function resetScopeToContext() {
    if (getTaskPageRoute()) blockDeferredHydration = true
    clearScopeOverride()
    untrack(() => setScope(defaultScope()))
  }
  // Navigation auto-narrow (#124) — a no-op once the user picks a scope.
  $effect(() => {
    // Track the active nav triple so the effect re-runs on navigation.
    void ctx.activeNotebook
    void ctx.activeSection
    void ctx.activePage
    const pageRouteActive = !!getTaskPageRoute()
    if (!navigationEntryHandled) {
      navigationEntryHandled = true
      enterTasksFromMainNavigation()
      return
    }
    if (pageRouteActive) return
    narrowScopeTo(defaultScope())
  })
  let scopeCrumb = $derived.by(() => {
    const s = getTaskHubState().scope
    if (s === 'vault') return 'Vault'
    if (s === 'notebook')
      return ctx.activeNotebook
        ? `Notebook · ${ctx.activeNotebook}`
        : 'Notebook'
    if (s === 'section')
      return ctx.activeSection ? `Section · ${ctx.activeSection}` : 'Section'
    return ctx.activePage ? `Page · ${ctx.activePage}` : 'Page'
  })

  // --- Filter facets (owners + tags universe for the shared FilterBar) ---
  // Lightweight DISTINCT queries; refreshed on block:changed. Kept in the
  // shell so every display mode sees the same filter universe.
  let allOwners = $state<string[]>([])
  let allTags = $state<string[]>([])
  let facetTimer: ReturnType<typeof setTimeout> | null = null
  async function reloadFacets() {
    try {
      const [ownerRes, tagRes] = await Promise.all([
        ctx.sqliteQuery(
          `SELECT DISTINCT owner FROM tasks WHERE owner IS NOT NULL AND owner != '' ORDER BY owner ASC LIMIT 200`
        ),
        ctx.sqliteQuery(
          `SELECT DISTINCT level_0 FROM tags ORDER BY level_0 ASC LIMIT 200`
        )
      ])
      allOwners = (
        (ownerRes.rows as unknown as Array<{ owner: string }>) ?? []
      ).map((r) => r.owner)
      allTags = (
        (tagRes.rows as unknown as Array<{ level_0: string }>) ?? []
      ).map((r) => r.level_0)
    } catch {
      // Facets are best-effort; the FilterBar simply shows an empty universe
      // if the query fails (e.g. before a vault is fully indexed).
    }
  }
  $effect(() => {
    const off = ctx.on('block:changed', () => {
      if (facetTimer) clearTimeout(facetTimer)
      facetTimer = setTimeout(() => {
        void reloadFacets()
      }, 200)
    })
    return () => {
      if (facetTimer) clearTimeout(facetTimer)
      off()
    }
  })

  function handleFiltersChange(f: TaskFilters) {
    if (getTaskPageRoute()) blockDeferredHydration = true
    setFilters(f)
  }

  function handleScopeChange(s: Scope): void {
    if (getTaskPageRoute()) blockDeferredHydration = true
    setScope(s)
  }

  // --- Saved views (#427) --------------------------------------------------
  // The bookmark button next to the title has three regimes driven by
  // (activeSavedViewId, savedViewsDirty):
  //
  //   no view active            → click opens the save composer
  //   view active + dirty       → click opens Update / Save-as-new menu
  //   view active + clean       → click opens Rename / Delete menu
  //
  // The composer is a tiny inline form (name input + Save/Cancel). The
  // menus are short button lists. Escape closes whichever is open
  // (mirrors FilterBar's escape pattern).
  let savedViewPopover = $state<'closed' | 'save' | 'menu' | 'rename'>('closed')
  let composerName = $state('')
  let savedViewError = $state('')
  let savedViewLiveMsg = $state('')
  let composerInput = $state<HTMLInputElement | null>(null)
  let bookmarkButton = $state<HTMLButtonElement | null>(null)

  let activeSavedView = $derived(
    hubState.activeSavedViewId
      ? hubState.savedViews.find((v) => v.id === hubState.activeSavedViewId)
      : undefined
  )

  // When the active view is clean, every dim it defines matches the live
  // state; when the user diverges, the mismatch is what the dirty flag
  // tracks. viewMatchesState is the source of truth for the bookmark icon
  // (filled when matching, outline when diverged or no view active).
  let activeViewMatchesState = $derived(
    !!activeSavedView && viewMatchesState(activeSavedView, hubState)
  )

  function openSaveComposer() {
    composerName = ''
    savedViewError = ''
    savedViewPopover = 'save'
    void tick().then(() => composerInput?.focus())
  }

  function openMenu() {
    // No-op when there's nothing to act on (defensive; the button itself
    // is the only entry point and it's not rendered in this state).
    savedViewError = ''
    savedViewPopover = 'menu'
  }

  function closePopover(returnFocus = false) {
    savedViewPopover = 'closed'
    composerName = ''
    savedViewError = ''
    if (returnFocus) void tick().then(() => bookmarkButton?.focus())
  }

  // Build a SavedView snapshot from the current hub state. Used by both
  // "Save current view" and "Update saved view" (the latter reuses the
  // existing id + name; the former mints a fresh id).
  function snapshotState(overrides: Partial<SavedView> = {}): SavedView {
    return {
      id: overrides.id ?? freshId(),
      name: overrides.name ?? (composerName.trim() || 'Untitled view'),
      displayMode: hubState.displayMode,
      groupBy: hubState.groupBy,
      sort: hubState.sort,
      scope: hubState.scope,
      filters: {
        owners: [...hubState.filters.owners],
        priorities: [...hubState.filters.priorities],
        dueDate: hubState.filters.dueDate,
        tags: [...hubState.filters.tags],
        stale: hubState.filters.stale
      },
      calendarSubMode: hubState.calendarSubMode,
      columns: cloneColumns(hubState.columns),
      system: false,
      ...overrides
    }
  }

  function freshId(): string {
    return typeof crypto !== 'undefined' && 'randomUUID' in crypto
      ? crypto.randomUUID()
      : `view-${Date.now()}-${Math.random().toString(36).slice(2)}`
  }

  async function persistAndAnnounce(
    next: SavedView[],
    msg: string
  ): Promise<void> {
    const ok = await persistSavedViews(next)
    if (!ok) {
      savedViewError = 'Failed to save view'
      return
    }
    savedViewError = ''
    savedViewLiveMsg = msg
    closePopover(true)
  }

  // Save current state as a NEW view, then mark it active.
  async function commitSaveNew() {
    const name = composerName.trim()
    if (!name) {
      savedViewError = 'Enter a view name'
      return
    }
    const view = snapshotState({ name })
    saveView(view)
    applySavedView(view)
    await persistAndAnnounce(
      getTaskHubState().savedViews,
      `View "${name}" saved`
    )
  }

  // Overwrite the active view's dimensions with the current state.
  async function commitUpdateActive() {
    if (!activeSavedView) return
    const view = snapshotState({
      id: activeSavedView.id,
      name: activeSavedView.name,
      system: activeSavedView.system
    })
    saveView(view)
    applySavedView(view)
    await persistAndAnnounce(
      getTaskHubState().savedViews,
      `View "${view.name}" updated`
    )
  }

  async function commitRename() {
    if (!activeSavedView) return
    const name = composerName.trim()
    if (!name) {
      savedViewError = 'Enter a view name'
      return
    }
    if (activeSavedView.system) {
      // System views are code-defined; a rename would diverge the
      // code-derived name on next hydrate (the loader always re-seeds
      // the SYSTEM_VIEWS name). Refuse rather than fight the seed.
      savedViewError = 'System views cannot be renamed'
      return
    }
    const view: SavedView = { ...activeSavedView, name }
    saveView(view)
    await persistAndAnnounce(
      getTaskHubState().savedViews,
      `View renamed to "${name}"`
    )
  }

  // Delete confirmation now flows through an in-app modal (#470) instead of
  // window.confirm(). `deleteConfirmTarget` holds the view awaiting the user's
  // choice; performDelete runs after the user confirms.
  let deleteConfirmTarget = $state<SavedView | null>(null)

  function requestDelete() {
    if (!activeSavedView || activeSavedView.system) return
    closePopover()
    deleteConfirmTarget = activeSavedView
  }

  function cancelDeleteModal() {
    deleteConfirmTarget = null
  }

  async function performDelete() {
    const target = deleteConfirmTarget
    deleteConfirmTarget = null
    if (!target || target.system) return
    deleteSavedView(target.id)
    await persistAndAnnounce(
      getTaskHubState().savedViews,
      `View "${target.name}" deleted`
    )
  }

  // Escape closes the popover (mirrors FilterBar's escape pattern). The
  // listener is always-attached (like the Ctrl+Shift+V handler below) and
  // no-ops when the popover is closed — avoids any $effect re-run race
  // when the popover transitions from closed → open.
  function onPopoverKeydown(e: KeyboardEvent) {
    if (e.key !== 'Escape') return
    if (savedViewPopover !== 'closed') closePopover(true)
    if (preferencesOpen) closePreferences(true)
  }

  // Click-away backdrop closes whichever popover is open (mirrors FilterBar).
  // Separate from the escape handler so a user tabbing through the form
  // can still escape via pointer.

  let totalCount = $derived(openCount + doneCount)
</script>

<div
  class="flex-1 flex flex-col min-h-0 overflow-hidden bg-surface-app"
  data-tasks-hub
>
  <header
    class="tasks-hub-header relative z-30 flex flex-wrap items-center gap-3 border-b border-surface-panel-border px-3 py-3 sm:flex-nowrap sm:px-5 lg:px-6"
    data-testid="tasks-hub-header"
  >
    <span
      class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-accent-primary-start/20 bg-accent-primary-glow text-accent-primary-start"
      aria-hidden="true"
    >
      <span class="material-symbols-outlined text-icon-xl">checklist</span>
    </span>
    <div class="min-w-0 flex-1">
      <h1
        bind:this={hubHeading}
        tabindex="-1"
        class="font-headline-lg text-headline-lg truncate text-text-primary"
      >
        {manifest?.name ?? 'Tasks'}
      </h1>
      <div class="mt-0.5 flex min-w-0 items-center gap-2">
        <span
          class="text-text-muted text-type-xs font-label-sm"
          aria-live="polite"
          data-testid="tasks-hub-count"
        >
          {openCount} active task{openCount === 1 ? '' : 's'}{doneCount > 0
            ? ` · ${doneCount} done`
            : ''}
        </span>
        {#if activeSavedView && !pageRoute}
          <!-- Active saved view name next to the title. The "(modified)" dirty
             signal is carried by the accent dot on the bookmark button (below)
             rather than dimming this label, so the dirty state is prominent
             without making the name harder to read. -->
          <span
            class="flex min-w-0 items-center gap-1 text-type-xs font-label-sm text-text-muted"
            data-testid="tasks-hub-active-view"
          >
            <span aria-hidden="true">·</span>
            <span class="truncate"
              >{activeSavedView.name}{hubState.savedViewsDirty
                ? ' (modified)'
                : ''}</span
            >
          </span>
        {/if}
      </div>
    </div>

    <div
      class="order-2 flex basis-full items-center justify-between gap-2 pt-1 sm:order-none sm:ml-auto sm:basis-auto sm:justify-end sm:pt-0"
      data-testid="tasks-hub-header-actions"
    >
      <!-- Saved-view bookmark (#427). Three regimes: no view active →
         save composer; view active + modified → update/save-as menu;
         view active + clean → rename/delete menu. Popover is a positioned
         dialog with Escape + click-away close (mirrors FilterBar). -->
      <div
        class="relative flex items-center"
        class:hidden={!!pageRoute}
        aria-hidden={pageRoute ? 'true' : undefined}
        data-testid="tasks-hub-saved-view-control"
      >
        <button
          bind:this={bookmarkButton}
          type="button"
          onclick={() => {
            if (savedViewPopover !== 'closed') {
              closePopover()
              return
            }
            if (!activeSavedView) openSaveComposer()
            else openMenu()
          }}
          aria-haspopup="dialog"
          aria-expanded={savedViewPopover !== 'closed'}
          aria-label={activeSavedView
            ? hubState.savedViewsDirty
              ? `Saved view "${activeSavedView.name}" modified — open actions`
              : `Saved view "${activeSavedView.name}" active — open actions`
            : 'Save current view'}
          title={activeSavedView
            ? hubState.savedViewsDirty
              ? `"${activeSavedView.name}" (modified) — click for save options`
              : `"${activeSavedView.name}" — click for rename/delete`
            : 'Save current view'}
          data-testid="tasks-hub-bookmark"
          data-popover-state={savedViewPopover}
          class="flex min-h-8 items-center gap-1 rounded-lg border border-surface-panel-border bg-surface-panel px-2 text-type-sm font-label-sm text-text-muted shadow-sm transition-all hover:border-border-active hover:bg-hover hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start"
        >
          <span
            class="material-symbols-outlined text-icon-md {activeViewMatchesState
              ? 'fill-accent-primary-start text-accent-primary-start'
              : ''}"
            aria-hidden="true"
            >{activeSavedView ? 'bookmark' : 'bookmark_add'}</span
          >
          {#if activeSavedView && hubState.savedViewsDirty}
            <!-- Dirty accent dot: a more prominent signal than the old dimmed
               italic label alone (#460). Secondary-accent so it reads against
               the primary bookmark fill and matches the .dirty-dot convention
               used elsewhere (e.g. TabStrip). Decorative — the aria-label
               above already announces "modified". -->
            <span
              class="w-1.5 h-1.5 rounded-full bg-accent-secondary-start"
              aria-hidden="true"
              data-testid="tasks-hub-dirty-dot"
            ></span>
          {/if}
        </button>

        {#if savedViewPopover === 'save'}
          <div
            transition:fly={{ y: -4, duration: motionDuration(100) }}
            class="absolute z-50 mt-1 top-full left-0 min-w-60 bg-surface-popover border border-surface-popover-border rounded-lg shadow-xl p-2"
            role="dialog"
            aria-label="Save current view"
            data-testid="tasks-hub-save-view-popover"
          >
            <label
              class="block text-type-xs font-label-sm text-text-muted mb-1"
            >
              View name
              <input
                type="text"
                bind:this={composerInput}
                bind:value={composerName}
                placeholder="e.g. Sprint 15"
                data-testid="tasks-hub-save-view-name"
                onkeydown={(e) => {
                  if (e.key === 'Enter') void commitSaveNew()
                  else if (e.key === 'Escape') closePopover(true)
                }}
                class="mt-1 w-full px-2 py-1 rounded bg-surface-panel border border-surface-panel-border text-text-primary text-type-sm outline-none focus:border-accent-primary-start"
              />
            </label>
            {#if savedViewError}
              <p
                class="mt-1 text-error text-type-xs font-body-md"
                role="alert"
                data-testid="tasks-hub-save-view-error"
              >
                {savedViewError}
              </p>
            {/if}
            <div class="mt-2 flex items-center justify-end gap-1">
              <button
                type="button"
                onclick={() => closePopover(true)}
                data-testid="tasks-hub-save-view-cancel"
                class="px-2 py-1 rounded text-type-xs font-label-sm text-text-muted hover:bg-hover border-none bg-transparent cursor-pointer"
                >Cancel</button
              >
              <button
                type="button"
                onclick={() => void commitSaveNew()}
                data-testid="tasks-hub-save-view-commit"
                class="px-2 py-1 rounded text-type-xs font-label-sm bg-accent-primary-start text-text-on-accent border-none cursor-pointer"
                >Save</button
              >
            </div>
          </div>
        {:else if savedViewPopover === 'rename'}
          <div
            transition:fly={{ y: -4, duration: motionDuration(100) }}
            class="absolute z-50 mt-1 top-full left-0 min-w-60 bg-surface-popover border border-surface-popover-border rounded-lg shadow-xl p-2"
            role="dialog"
            aria-label={`Rename ${activeSavedView?.name ?? 'view'}`}
            data-testid="tasks-hub-rename-view-popover"
          >
            <label
              class="block text-type-xs font-label-sm text-text-muted mb-1"
            >
              New name
              <input
                type="text"
                bind:this={composerInput}
                bind:value={composerName}
                data-testid="tasks-hub-rename-view-name"
                onkeydown={(e) => {
                  if (e.key === 'Enter') void commitRename()
                  else if (e.key === 'Escape') closePopover(true)
                }}
                class="mt-1 w-full px-2 py-1 rounded bg-surface-panel border border-surface-panel-border text-text-primary text-type-sm outline-none focus:border-accent-primary-start"
              />
            </label>
            {#if savedViewError}
              <p class="mt-1 text-error text-type-xs font-body-md" role="alert">
                {savedViewError}
              </p>
            {/if}
            <div class="mt-2 flex items-center justify-end gap-1">
              <button
                type="button"
                onclick={() => closePopover(true)}
                data-testid="tasks-hub-rename-view-cancel"
                class="px-2 py-1 rounded text-type-xs font-label-sm text-text-muted hover:bg-hover border-none bg-transparent cursor-pointer"
                >Cancel</button
              >
              <button
                type="button"
                onclick={() => void commitRename()}
                data-testid="tasks-hub-rename-view-commit"
                class="px-2 py-1 rounded text-type-xs font-label-sm bg-accent-primary-start text-text-on-accent border-none cursor-pointer"
                >Rename</button
              >
            </div>
          </div>
        {:else if savedViewPopover === 'menu'}
          <div
            transition:fly={{ y: -4, duration: motionDuration(100) }}
            class="absolute z-50 mt-1 top-full left-0 min-w-50 bg-surface-popover border border-surface-popover-border rounded-lg shadow-xl py-1"
            role="dialog"
            aria-label={`Actions for ${activeSavedView?.name ?? 'view'}`}
            data-testid="tasks-hub-view-menu"
          >
            {#if hubState.savedViewsDirty}
              <button
                type="button"
                onclick={() => void commitUpdateActive()}
                data-testid="tasks-hub-update-view"
                class="w-full flex items-center gap-2 px-3 py-1.5 hover:bg-hover text-type-sm font-label-sm text-text-primary text-left border-none bg-transparent cursor-pointer"
              >
                <span
                  class="material-symbols-outlined text-icon-sm"
                  aria-hidden="true">save</span
                >
                <span>Update “{activeSavedView?.name}”</span>
              </button>
              <button
                type="button"
                onclick={openSaveComposer}
                data-testid="tasks-hub-save-as-new"
                class="w-full flex items-center gap-2 px-3 py-1.5 hover:bg-hover text-type-sm font-label-sm text-text-primary text-left border-none bg-transparent cursor-pointer"
              >
                <span
                  class="material-symbols-outlined text-icon-sm"
                  aria-hidden="true">add</span
                >
                <span>Save as new…</span>
              </button>
            {:else}
              <button
                type="button"
                onclick={() => {
                  composerName = activeSavedView?.name ?? ''
                  savedViewError = ''
                  savedViewPopover = 'rename'
                  void tick().then(() => composerInput?.focus())
                }}
                data-testid="tasks-hub-rename-view"
                class="w-full flex items-center gap-2 px-3 py-1.5 hover:bg-hover text-type-sm font-label-sm text-text-primary text-left border-none bg-transparent cursor-pointer"
              >
                <span
                  class="material-symbols-outlined text-icon-sm"
                  aria-hidden="true">edit</span
                >
                <span>Rename…</span>
              </button>
              <button
                type="button"
                disabled={activeSavedView?.system}
                aria-disabled={activeSavedView?.system}
                title={activeSavedView?.system
                  ? 'System views cannot be deleted'
                  : undefined}
                onclick={() => requestDelete()}
                data-testid="tasks-hub-delete-view"
                class="w-full flex items-center gap-2 px-3 py-1.5 hover:bg-hover text-type-sm font-label-sm text-error text-left border-none bg-transparent cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed disabled:hover:bg-transparent"
              >
                <span
                  class="material-symbols-outlined text-icon-sm"
                  aria-hidden="true">delete</span
                >
                <span>Delete{activeSavedView?.system ? ' (system)' : ''}</span>
              </button>
            {/if}
          </div>
        {/if}
      </div>

      <!-- View-mode segmented control (WAI-ARIA radiogroup, roving tabindex). -->
      <div
        class="flex shrink-0 items-center gap-0.5 rounded-lg border border-surface-panel-border bg-surface-panel p-1 shadow-sm"
        role="radiogroup"
        aria-label="Tasks display mode"
        tabindex="-1"
        data-testid="tasks-hub-mode-switcher"
        onkeydown={onModeKeydown}
      >
        {#each MODES as m (m.value)}
          <button
            type="button"
            role="radio"
            aria-checked={hubState.displayMode === m.value}
            aria-label={`${m.label} mode`}
            tabindex={hubState.displayMode === m.value ? 0 : -1}
            data-mode={m.value}
            data-testid={`tasks-hub-mode-${m.value}`}
            onclick={() => chooseMode(m.value)}
            title={`${m.label} mode (Ctrl+Shift+V)`}
            class="flex min-h-8 items-center gap-1.5 rounded-md border px-2 sm:px-2.5 text-type-sm font-label-sm transition-all cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start {hubState.displayMode ===
            m.value
              ? 'border-accent-primary-start/30 bg-accent-primary-glow text-accent-primary-start shadow-sm'
              : 'border-transparent bg-transparent text-text-muted hover:bg-hover hover:text-text-primary'}"
          >
            <span
              class="material-symbols-outlined text-icon-sm"
              aria-hidden="true">{m.icon}</span
            >
            <span class="hidden sm:inline" aria-hidden="true">{m.label}</span>
          </button>
        {/each}
      </div>

      <div class="relative flex items-center" data-testid="tasks-preferences">
        <button
          bind:this={preferencesButton}
          type="button"
          onclick={togglePreferences}
          aria-label="Task preferences"
          aria-haspopup="dialog"
          aria-expanded={preferencesOpen}
          aria-controls="tasks-preferences-popover"
          title="Task preferences"
          class="flex h-9 w-9 items-center justify-center rounded-lg border border-surface-panel-border bg-surface-panel text-text-muted shadow-sm hover:border-border-active hover:bg-hover hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start transition-all cursor-pointer"
        >
          <span
            class="material-symbols-outlined text-icon-md"
            aria-hidden="true">tune</span
          >
        </button>

        {#if preferencesOpen}
          <div
            id="tasks-preferences-popover"
            transition:fly={{ y: -4, duration: motionDuration(100) }}
            class="absolute z-50 top-full right-0 mt-2 w-64 rounded-lg border border-surface-popover-border bg-surface-popover p-3 shadow-xl"
            role="dialog"
            aria-labelledby="tasks-preferences-title"
            data-testid="tasks-preferences-popover"
          >
            <div class="mb-3 flex items-center gap-2">
              <span
                class="material-symbols-outlined text-icon-md text-accent-primary-start"
                aria-hidden="true">calendar_view_week</span
              >
              <h2
                id="tasks-preferences-title"
                class="text-type-sm font-label-sm-bold text-text-primary"
              >
                Task preferences
              </h2>
            </div>

            <fieldset>
              <legend class="text-type-xs font-label-sm-bold text-text-primary">
                First day of week
              </legend>
              <p
                id="tasks-week-start-description"
                class="mt-0.5 text-type-xs font-body-md text-text-muted"
              >
                Used by calendars, week groups, and “This Week”.
              </p>
              <div
                class="mt-2 grid grid-cols-2 gap-1 rounded-lg border border-surface-panel-border bg-surface-panel p-1"
              >
                {#each ['sunday', 'monday'] as value (value)}
                  {@const selected = weekStart === value}
                  <label
                    class="relative flex min-h-8 items-center justify-center gap-1 rounded-md px-2 text-type-xs font-label-sm cursor-pointer transition-colors focus-within:outline-none focus-within:ring-2 focus-within:ring-accent-primary-start {selected
                      ? 'bg-accent-primary-start text-text-on-accent'
                      : 'text-text-muted hover:bg-hover hover:text-text-primary'}"
                  >
                    <input
                      type="radio"
                      name="tasks-week-start"
                      {value}
                      checked={selected}
                      aria-describedby="tasks-week-start-description"
                      onchange={() => void chooseWeekStart(value as WeekStart)}
                      class="sr-only"
                    />
                    <span>{value === 'sunday' ? 'Sunday' : 'Monday'}</span>
                    {#if selected}
                      <span
                        class="material-symbols-outlined text-icon-xs"
                        aria-hidden="true">check</span
                      >
                    {/if}
                  </label>
                {/each}
              </div>
            </fieldset>

            {#if weekStartError}
              <p class="mt-2 text-type-xs font-body-md text-error" role="alert">
                {weekStartError}
              </p>
            {/if}
          </div>
        {/if}
      </div>
    </div>
  </header>

  {#if pageRoute}
    <div
      class="flex items-center gap-3 border-b border-accent-primary-start/20 bg-accent-primary-glow px-3 py-2.5 sm:px-5 lg:px-6"
      data-testid="tasks-page-context"
    >
      <span
        class="material-symbols-outlined text-icon-md text-accent-primary-start"
        aria-hidden="true">description</span
      >
      <div class="min-w-0">
        <div class="text-type-xs font-label-sm text-text-muted">
          Tasks on this page
        </div>
        <div class="text-type-sm font-body-md text-text-primary truncate">
          {pageRoutePath}
        </div>
      </div>
      <button
        type="button"
        class="ml-auto px-2.5 py-1 rounded-md border border-surface-panel-border bg-surface-panel text-type-xs font-label-sm text-text-muted hover:bg-hover hover:text-text-primary transition-colors cursor-pointer"
        onclick={exitPageRoute}
        aria-label="Exit page task context"
      >
        Clear
      </button>
    </div>
  {/if}

  <FilterBar
    filters={hubState.filters}
    owners={allOwners}
    tags={allTags}
    onFiltersChange={handleFiltersChange}
    groupBy={hubState.groupBy}
    onGroupByChange={chooseGroupBy}
    sort={hubState.sort}
    onSortChange={chooseSort}
    scope={hubState.scope}
    onScopeChange={handleScopeChange}
    {isScopeDisabled}
    {scopeCrumb}
    scopeUserOverride={hubState.scopeUserOverride}
    onResetScope={resetScopeToContext}
    {totalCount}
  />

  {#if pageRoute && countsReady && totalCount === 0}
    <div
      class="px-3 pt-4 text-type-sm font-body-md text-text-muted sm:px-5 lg:px-6"
      data-testid="tasks-page-empty"
    >
      No tasks on this page.
    </div>
  {/if}

  <div class="flex-1 flex flex-col min-h-0 overflow-hidden">
    {#if hubState.displayMode === 'list'}
      <ListView
        {ctx}
        {focusBlockId}
        {focusKey}
        onCountChange={handleCountChange}
      />
    {:else if hubState.displayMode === 'board'}
      <BoardView {ctx} onCountChange={handleCountChange} />
    {:else}
      <CalendarView {ctx} onCountChange={handleCountChange} />
    {/if}
  </div>

  <!-- Saved-view popover click-away backdrop (closes whichever popover
       is open). Mirrors FilterBar's pattern. -->
  {#if savedViewPopover !== 'closed'}
    <div
      class="fixed inset-0 z-40"
      role="presentation"
      onclick={() => closePopover(true)}
      tabindex="-1"
      aria-hidden="true"
      data-testid="tasks-hub-saved-view-backdrop"
    ></div>
  {/if}

  {#if preferencesOpen}
    <div
      class="fixed inset-0 z-40"
      role="presentation"
      onclick={() => closePreferences(true)}
      tabindex="-1"
      aria-hidden="true"
      data-testid="tasks-preferences-backdrop"
    ></div>
  {/if}

  <!-- sr-only live region for saved-view announcements (mirrors
       KanbanSidebar's pattern). -->
  <div
    class="sr-only"
    aria-live="polite"
    data-testid="tasks-hub-saved-view-live"
  >
    {savedViewLiveMsg}
  </div>
  <div class="sr-only" aria-live="polite" data-testid="tasks-page-route-live">
    {routeAnnouncement}
  </div>

  <!-- Delete confirmation modal (#470). Replaces window.confirm(). -->
  {#if deleteConfirmTarget}
    <ConfirmModal
      title="Delete saved view?"
      message={`Delete “${deleteConfirmTarget.name}”? This view will be removed from your saved views.`}
      confirmLabel="Delete"
      cancelLabel="Cancel"
      destructive={true}
      dataTestId="tasks-hub-delete-confirm"
      onConfirm={() => void performDelete()}
      onCancel={cancelDeleteModal}
    />
  {/if}

  <!-- Hub-scoped command palette (#436). Mounted only while open so focus
       trap + Escape listeners clean up with the overlay. -->
  <TasksCommandPalette
    open={paletteOpen}
    onClose={() => (paletteOpen = false)}
    onDisplayMode={chooseMode}
    onGroupBy={chooseGroupBy}
    onSort={chooseSort}
    onApplyView={handlePaletteApplyView}
    onFindTask={handlePaletteFindTask}
    onAddTask={handlePaletteAddTask}
  />
</div>

<style>
  .tasks-hub-header {
    background: color-mix(in srgb, var(--color-surface-app) 92%, transparent);
    backdrop-filter: blur(14px);
  }

  @media (prefers-reduced-transparency: reduce) {
    .tasks-hub-header {
      background: var(--color-surface-app);
      backdrop-filter: none;
    }
  }
</style>
