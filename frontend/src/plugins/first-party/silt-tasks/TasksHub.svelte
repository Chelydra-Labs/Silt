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
  import ListView from './views/ListView.svelte'
  import BoardView from './views/BoardView.svelte'
  import CalendarView from './views/CalendarView.svelte'
  import {
    getTaskHubState,
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
    persistDefaultSort
  } from './settings'
  import { viewMatchesState } from './savedViews'

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

  let hubState = $derived(getTaskHubState())

  // Counts reported upward by the active renderer (List today).
  let openCount = $state(0)
  let doneCount = $state(0)
  function handleCountChange(open: number, done: number) {
    openCount = open
    doneCount = done
  }

  // --- Display mode ------------------------------------------------------
  // Hydrate from the persisted default once on mount; afterwards every user
  // switch is persisted. untrack so the initial set doesn't loop through the
  // $derived that reads hubState.displayMode.
  onMount(() => {
    // Pull the settings slice through the SDK (per-active-notebook override
    // layer #133) before any load* read. initTasksSettings is async because
    // getPluginSettings hits the Go binding, so hydration + facet reload run
    // in its .then().
    void initTasksSettings(ctx).then(() => {
      untrack(() => {
        const persisted = loadDefaultDisplayMode()
        if (persisted !== getTaskHubState().displayMode) {
          setDisplayMode(persisted)
        }
        // Group-by + sort hydrate the same way (#423). Independent of display
        // mode so the user's preferred grouping survives a List → Board hop.
        const persistedGroup = loadDefaultGroupBy()
        if (persistedGroup !== getTaskHubState().groupBy) {
          setGroupBy(persistedGroup)
        }
        const persistedSort = loadDefaultSort()
        if (persistedSort !== getTaskHubState().sort) {
          setSort(persistedSort)
        }
        // Status columns (#421) hydrate into the unified state so saved
        // views can snapshot them. BoardView keeps its own local mirror
        // today; Phase 7 reconciles the two.
        // Length check is load-bearing: Array.some walks only persistedCols
        // indices, so a TRIMMED config ([TODO,DOING] vs default [TODO,DOING,
        // DONE]) would otherwise compare equal and silently revert the trim.
        const persistedCols = loadColumns()
        const currentCols = getTaskHubState().columns
        if (
          persistedCols.length &&
          (persistedCols.length !== currentCols.length ||
            persistedCols.some((c, i) => c !== currentCols[i]))
        ) {
          setColumns(persistedCols)
        }
        // Saved views (#427): system defaults (code-defined) + user views
        // (YAML). Seeded once on mount; the in-memory list is the single
        // source afterwards.
        const views = loadSavedViews()
        if (views.length) {
          getTaskHubState().savedViews = views
        }
        // Mount-time hydration is not a user edit; clear any dirty flag a
        // stray setter might have flipped during the seed above.
        getTaskHubState().savedViewsDirty = false
        getTaskHubState().activeSavedViewId = ''
      })
      void reloadFacets()
    })

    // Subsequent external edits (e.g. co-located config.yaml change on a
    // linked notebook) arrive as config:changed. initTasksSettings already
    // got the latest config on mount; this re-hydrates after the re-read —
    // but never clobbers an in-session edit (savedViewsDirty wins).
    const unsubConfig = ctx.on('config:changed', () => {
      if (getTaskHubState().savedViewsDirty) return
      void reloadTasksSettings(ctx).then(() => {
        untrack(() => {
          const views = loadSavedViews()
          if (views.length) getTaskHubState().savedViews = views
          const mode = loadDefaultDisplayMode()
          if (mode !== getTaskHubState().displayMode) setDisplayMode(mode)
          const group = loadDefaultGroupBy()
          if (group !== getTaskHubState().groupBy) setGroupBy(group)
          const sortVal = loadDefaultSort()
          if (sortVal !== getTaskHubState().sort) setSort(sortVal)
          const cols = loadColumns()
          const curCols = getTaskHubState().columns
          if (
            cols.length &&
            (cols.length !== curCols.length ||
              cols.some((c, i) => c !== curCols[i]))
          ) {
            setColumns(cols)
          }
        })
      })
    })
    return () => unsubConfig()
  })

  function chooseMode(mode: DisplayMode) {
    setDisplayMode(mode)
    void persistDefaultDisplayMode(mode)
  }

  function chooseGroupBy(g: GroupBy) {
    setGroupBy(g)
    void persistDefaultGroupBy(g)
  }

  function chooseSort(s: SortMode) {
    setSort(s)
    void persistDefaultSort(s)
  }

  // Ctrl+Shift+V cycles List → Board → Calendar → List. Guard against the
  // browser's paste-without-formatting shortcut when the user is typing in
  // an input/textarea/contenteditable so the hub doesn't steal the keystroke
  // mid-composition.
  function onGlobalKeydown(e: KeyboardEvent) {
    if (!(e.ctrlKey && e.shiftKey) || e.key !== 'V') return
    const t = e.target as HTMLElement
    if (
      t instanceof HTMLInputElement ||
      t instanceof HTMLTextAreaElement ||
      t.isContentEditable
    )
      return
    e.preventDefault()
    const order: DisplayMode[] = ['list', 'board', 'calendar']
    const idx = order.indexOf(getTaskHubState().displayMode)
    chooseMode(order[(idx + 1) % order.length])
  }
  // One always-on keydown listener dispatches to both the mode-cycle
  // shortcut and the saved-view-popover escape (cheaper than two
  // window-level listeners and avoids an $effect re-run race between
  // the popover's open/close transitions).
  function onWindowKeydown(e: KeyboardEvent) {
    onGlobalKeydown(e)
    onPopoverKeydown(e)
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
    clearScopeOverride()
    untrack(() => setScope(defaultScope()))
  }
  // Navigation auto-narrow (#124) — a no-op once the user picks a scope.
  $effect(() => {
    // Track the active nav triple so the effect re-runs on navigation.
    void ctx.activeNotebook
    void ctx.activeSection
    void ctx.activePage
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
    setFilters(f)
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

  function closePopover() {
    savedViewPopover = 'closed'
    composerName = ''
    savedViewError = ''
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
        tags: [...hubState.filters.tags]
      },
      calendarSubMode: hubState.calendarSubMode,
      columns: [...hubState.columns],
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
    closePopover()
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
    if (savedViewPopover !== 'closed' && e.key === 'Escape') closePopover()
  }

  // Click-away backdrop closes whichever popover is open (mirrors FilterBar).
  // Separate from the escape handler so a user tabbing through the form
  // can still escape via pointer.

  let totalCount = $derived(openCount + doneCount)
</script>

<div class="flex-1 flex flex-col min-h-0 overflow-hidden" data-tasks-hub>
  <header
    class="px-6 py-3 border-b border-surface-panel-border flex items-center gap-3 flex-wrap"
  >
    <span class="material-symbols-outlined text-accent-primary-start"
      >checklist</span
    >
    <h1
      class="font-headline-lg text-headline-lg text-text-primary flex items-baseline gap-2"
    >
      {manifest?.name ?? 'Tasks'}
      <span
        class="text-text-muted text-[12px] font-body-md normal-case font-normal ml-1"
        aria-live="polite"
        data-testid="tasks-hub-count"
      >
        {openCount} active task{openCount === 1 ? '' : 's'}{doneCount > 0
          ? ` · ${doneCount} done`
          : ''}
      </span>
      {#if activeSavedView}
        <!-- Active saved view name next to the title. The "(modified)" dirty
             signal is carried by the accent dot on the bookmark button (below)
             rather than dimming this label, so the dirty state is prominent
             without making the name harder to read. -->
        <span
          class="text-text-muted text-[12px] font-body-md normal-case font-normal ml-2 flex items-center gap-1"
          data-testid="tasks-hub-active-view"
        >
          <span aria-hidden="true">·</span>
          <span
            >{activeSavedView.name}{hubState.savedViewsDirty
              ? ' (modified)'
              : ''}</span
          >
        </span>
      {/if}
    </h1>

    <!-- Saved-view bookmark (#427). Three regimes: no view active →
         save composer; view active + modified → update/save-as menu;
         view active + clean → rename/delete menu. Popover is a positioned
         dialog with Escape + click-away close (mirrors FilterBar). -->
    <div
      class="relative flex items-center"
      data-testid="tasks-hub-saved-view-control"
    >
      <button
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
        class="flex items-center gap-1 px-2 py-1 rounded border border-surface-panel-border bg-surface-panel text-[12px] font-label-sm text-text-muted hover:bg-hover hover:text-text-primary transition-colors"
      >
        <span
          class="material-symbols-outlined text-[16px] {activeViewMatchesState
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
          transition:fly={{ y: -4, duration: 100 }}
          class="absolute z-50 mt-1 top-full left-0 min-w-[240px] bg-surface-popover border border-surface-popover-border rounded-lg shadow-xl p-2"
          role="dialog"
          aria-label="Save current view"
          data-testid="tasks-hub-save-view-popover"
        >
          <label class="block text-[11px] font-label-sm text-text-muted mb-1">
            View name
            <input
              type="text"
              bind:this={composerInput}
              bind:value={composerName}
              placeholder="e.g. Sprint 15"
              data-testid="tasks-hub-save-view-name"
              onkeydown={(e) => {
                if (e.key === 'Enter') void commitSaveNew()
                else if (e.key === 'Escape') closePopover()
              }}
              class="mt-1 w-full px-2 py-1 rounded bg-surface-panel border border-surface-panel-border text-text-primary text-[12px] outline-none focus:border-accent-primary-start"
            />
          </label>
          {#if savedViewError}
            <p
              class="mt-1 text-error text-[11px] font-body-md"
              role="alert"
              data-testid="tasks-hub-save-view-error"
            >
              {savedViewError}
            </p>
          {/if}
          <div class="mt-2 flex items-center justify-end gap-1">
            <button
              type="button"
              onclick={closePopover}
              data-testid="tasks-hub-save-view-cancel"
              class="px-2 py-1 rounded text-[11px] font-label-sm text-text-muted hover:bg-hover border-none bg-transparent cursor-pointer"
              >Cancel</button
            >
            <button
              type="button"
              onclick={() => void commitSaveNew()}
              data-testid="tasks-hub-save-view-commit"
              class="px-2 py-1 rounded text-[11px] font-label-sm bg-accent-primary-start text-text-primary border-none cursor-pointer"
              >Save</button
            >
          </div>
        </div>
      {:else if savedViewPopover === 'rename'}
        <div
          transition:fly={{ y: -4, duration: 100 }}
          class="absolute z-50 mt-1 top-full left-0 min-w-[240px] bg-surface-popover border border-surface-popover-border rounded-lg shadow-xl p-2"
          role="dialog"
          aria-label={`Rename ${activeSavedView?.name ?? 'view'}`}
          data-testid="tasks-hub-rename-view-popover"
        >
          <label class="block text-[11px] font-label-sm text-text-muted mb-1">
            New name
            <input
              type="text"
              bind:this={composerInput}
              bind:value={composerName}
              data-testid="tasks-hub-rename-view-name"
              onkeydown={(e) => {
                if (e.key === 'Enter') void commitRename()
                else if (e.key === 'Escape') closePopover()
              }}
              class="mt-1 w-full px-2 py-1 rounded bg-surface-panel border border-surface-panel-border text-text-primary text-[12px] outline-none focus:border-accent-primary-start"
            />
          </label>
          {#if savedViewError}
            <p class="mt-1 text-error text-[11px] font-body-md" role="alert">
              {savedViewError}
            </p>
          {/if}
          <div class="mt-2 flex items-center justify-end gap-1">
            <button
              type="button"
              onclick={closePopover}
              data-testid="tasks-hub-rename-view-cancel"
              class="px-2 py-1 rounded text-[11px] font-label-sm text-text-muted hover:bg-hover border-none bg-transparent cursor-pointer"
              >Cancel</button
            >
            <button
              type="button"
              onclick={() => void commitRename()}
              data-testid="tasks-hub-rename-view-commit"
              class="px-2 py-1 rounded text-[11px] font-label-sm bg-accent-primary-start text-text-primary border-none cursor-pointer"
              >Rename</button
            >
          </div>
        </div>
      {:else if savedViewPopover === 'menu'}
        <div
          transition:fly={{ y: -4, duration: 100 }}
          class="absolute z-50 mt-1 top-full left-0 min-w-[200px] bg-surface-popover border border-surface-popover-border rounded-lg shadow-xl py-1"
          role="dialog"
          aria-label={`Actions for ${activeSavedView?.name ?? 'view'}`}
          data-testid="tasks-hub-view-menu"
        >
          {#if hubState.savedViewsDirty}
            <button
              type="button"
              onclick={() => void commitUpdateActive()}
              data-testid="tasks-hub-update-view"
              class="w-full flex items-center gap-2 px-3 py-1.5 hover:bg-hover text-[12px] font-label-sm text-text-primary text-left border-none bg-transparent cursor-pointer"
            >
              <span
                class="material-symbols-outlined text-[14px]"
                aria-hidden="true">save</span
              >
              <span>Update “{activeSavedView?.name}”</span>
            </button>
            <button
              type="button"
              onclick={openSaveComposer}
              data-testid="tasks-hub-save-as-new"
              class="w-full flex items-center gap-2 px-3 py-1.5 hover:bg-hover text-[12px] font-label-sm text-text-primary text-left border-none bg-transparent cursor-pointer"
            >
              <span
                class="material-symbols-outlined text-[14px]"
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
              class="w-full flex items-center gap-2 px-3 py-1.5 hover:bg-hover text-[12px] font-label-sm text-text-primary text-left border-none bg-transparent cursor-pointer"
            >
              <span
                class="material-symbols-outlined text-[14px]"
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
              class="w-full flex items-center gap-2 px-3 py-1.5 hover:bg-hover text-[12px] font-label-sm text-error text-left border-none bg-transparent cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed disabled:hover:bg-transparent"
            >
              <span
                class="material-symbols-outlined text-[14px]"
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
      class="ml-auto flex items-center gap-1 p-1 rounded-lg border border-surface-panel-border bg-surface-panel"
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
          class="flex items-center gap-1.5 px-2.5 py-1 rounded text-[12px] font-label-sm transition-colors border-none cursor-pointer {hubState.displayMode ===
          m.value
            ? 'bg-accent-primary-start text-text-primary'
            : 'bg-transparent text-text-muted hover:bg-hover hover:text-text-primary'}"
        >
          <span class="material-symbols-outlined text-[14px]" aria-hidden="true"
            >{m.icon}</span
          >
          <span aria-hidden="true">{m.label}</span>
        </button>
      {/each}
    </div>
  </header>

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
    onScopeChange={(s) => setScope(s)}
    {isScopeDisabled}
    {scopeCrumb}
    scopeUserOverride={hubState.scopeUserOverride}
    onResetScope={resetScopeToContext}
    {totalCount}
  />

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
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div
      class="fixed inset-0 z-40"
      role="presentation"
      onclick={closePopover}
      tabindex="-1"
      aria-hidden="true"
      data-testid="tasks-hub-saved-view-backdrop"
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
</div>
