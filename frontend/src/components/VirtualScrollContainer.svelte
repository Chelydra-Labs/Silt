<script lang="ts">
  import { SvelteSet } from 'svelte/reactivity'
  import { onDestroy, tick, untrack } from 'svelte'
  import { FetchPageBlocks, RenamePage } from '../../bindings/silt/app.js'
  import { Events } from '@wailsio/runtime'
  import { EventName } from '../generated/enums'
  import TipTapEditor from './TipTapEditor.svelte'
  import MarkdownSourceViewer from './editor/MarkdownSourceViewer.svelte'
  import OutlinePanel from './editor/OutlinePanel.svelte'
  import FindBar from './editor/FindBar.svelte'
  import { findBarState } from '../lib/editor/search/findBarState.svelte'
  import type { ParsedBlock } from '../lib/editor'
  import {
    snapshotEditCaret,
    applyEditCaret,
    type EditCaretSnapshot
  } from '../lib/editor/editCaretRestore'
  import type { Editor } from 'svelte-tiptap'
  import type { ViewMode } from '../lib/tabs'
  import EditorUtilityBar from './editor/EditorUtilityBar.svelte'
  import DateGlanceChip from './DateGlanceChip.svelte'
  import {
    settings,
    toggleFocusMode,
    toggleFormatToolbar
  } from '../settings/store.svelte'
  import { shortcutBinding } from '../settings/shortcutActions'
  import {
    noteZoom,
    NOTE_ZOOM_DEFAULT,
    NOTE_ZOOM_MAX,
    NOTE_ZOOM_MIN
  } from '../lib/noteZoom.svelte'

  interface Props {
    /** Canonical content root (`vault` or `linked:<id>`). */
    source?: string
    notebook: string
    section: string
    page: string
    /** Editor view for this tab (#195). Owned by App.svelte's TabEntry. */
    viewMode: ViewMode
    /** Toggle this tab's view mode (floating button). */
    onToggleViewMode?: () => void
    targetBlockId?: string
    /** Wiki-link heading scroll target (#545); matches HEADER clean_text. */
    targetHeading?: string
    targetKey?: string
    onBlockFocus?: (blockId: string, ancestors: string[]) => void
    onBlockBlur?: () => void
    activeFocusedBlockAncestors?: string[]
    onPageRenamed?: (newName: string) => void
    onFirstEdit?: () => void
    isActive?: boolean
    /** Forwarded to TipTapEditor; surfaces save-state changes (#167, #546). */
    onSaveStateChange?: (state: {
      phase: 'idle' | 'pending' | 'saving' | 'saved' | 'error'
      dirty: boolean
      error: string | null
    }) => void
  }

  let {
    source = 'vault',
    notebook,
    section,
    page,
    viewMode,
    onToggleViewMode,
    targetBlockId = '',
    targetHeading = '',
    targetKey = '',
    onBlockFocus,
    onBlockBlur,
    activeFocusedBlockAncestors = [],
    onPageRenamed,
    onFirstEdit,
    isActive: _isActive = true,
    onSaveStateChange
  }: Props = $props()

  let pageLocator = $derived(
    notebook && page && notebook !== '.silt'
      ? { source, notebook, section, page }
      : null
  )

  // Editor bindings
  let editorInstance = $state<Editor | null>(null)
  // eslint-disable-next-line svelte/no-unnecessary-state-wrap -- bindable-style reassignment from editor
  let activeMarks = $state(new SvelteSet<string>())

  let showFormatToolbar = $derived(
    settings.config?.ui?.show_format_toolbar !== false
  )
  // Note chrome (zoom + optional format/page tasks) for real notebooks only —
  // not the standalone `.silt` tasks surface.
  let showEditorUtilityBar = $derived(notebook !== '.silt')
  // The view-mode hotkey is per-vault remappable; read it live so the toggle's
  // tooltip + aria-keyshortcuts never go stale after a remap (the binding in
  // config.yaml is already in display form, e.g. "Ctrl+Shift+V").
  let viewModeHotkey = $derived(
    shortcutBinding('toggle_view_mode', settings.config?.hotkeys ?? {})
  )
  let focusModeHotkey = $derived(
    shortcutBinding('toggle_focus_mode', settings.config?.hotkeys ?? {})
  )
  let formatToolbarHotkey = $derived(
    shortcutBinding('toggle_format_toolbar', settings.config?.hotkeys ?? {})
  )

  let blocks = $state<ParsedBlock[]>([])
  let loading = $state(false)
  let loadError = $state('')
  let containerEl = $state<HTMLDivElement | null>(null)
  // hasFirstEdit is intentionally NOT reset: each VirtualScrollContainer
  // instance is bound to one tab for its lifetime (the display:none
  // architecture mounts a fresh component per tab). The one-shot semantics
  // ensure edit-to-pin promotion fires exactly once per tab mount.
  let hasFirstEdit = false
  let handledTargetKey = $state('')
  let scrollAttemptCount = 0
  let outlineOpen = $state(false)

  // Editor status state
  let saveError = $state<string | null>(null)
  let wordCount = $state(0)
  let showWordCount = $derived(
    settings.config?.editor?.show_word_count === true
  )
  // Keep the top-right action tray open when a control is already engaged so
  // the user can reverse it without hunting for a collapsed affordance.
  let editorActionsPinned = $derived(
    settings.config?.editor?.focus_mode === true ||
      outlineOpen ||
      viewMode === 'source'
  )
  // Bottom status pill stays open for save failures (fail-loud) or non-default
  // zoom so the user can see/reset without hunting a collapsed control.
  let editorStatusPinned = $derived(
    !!saveError || noteZoom.factor !== NOTE_ZOOM_DEFAULT
  )

  $effect(() => {
    if (notebook && page) {
      untrack(() => {
        void loadPage(true)
      })
    }
  })

  // Ctrl/Meta + wheel zooms note content only (#843). Gated to real notebooks
  // (same as utility-bar chrome) so `.silt` tasks cannot inherit orphan zoom.
  // Non-passive so we can preventDefault and stop webview page zoom.
  $effect(() => {
    const el = containerEl
    if (!el || !showEditorUtilityBar) return
    const onWheel = (e: WheelEvent) => {
      if (!(e.ctrlKey || e.metaKey)) return
      e.preventDefault()
      if (e.deltaY < 0) noteZoom.zoomIn()
      else if (e.deltaY > 0) noteZoom.zoomOut()
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
  })

  $effect(() => {
    if ((targetBlockId || targetHeading) && targetKey !== handledTargetKey) {
      scrollAttemptCount = 0
      void tryScrollToTarget(targetKey)
    }
  })

  // Retry heading/block scroll once blocks finish loading (#545 harden).
  // tryScrollToTarget only marks the key handled on success; until then a
  // blocks update re-attempts so navigate-to-page#heading doesn't race load.
  $effect(() => {
    if (
      blocks.length > 0 &&
      (targetBlockId || targetHeading) &&
      targetKey &&
      targetKey !== handledTargetKey
    ) {
      void tryScrollToTarget(targetKey)
    }
  })

  // Subscribe to block:changed events (#64). When an external mutation
  // (embed edit, external edit) changes a block on the current page, reload
  // the block list so the editor sees the update. The editor's own $effect
  // handles applying the update when the user is not actively editing.
  $effect(() => {
    // Read props at the top of the effect so it re-subscribes when the user
    // navigates to a different page (#64). Without this, the Events.On closure
    // would filter against stale values after navigation.
    const nb = notebook,
      sec = section,
      pg = page
    const off = Events.On(EventName.EventBlockChanged, (event) => {
      const ev: { notebook: string; section: string; page: string } = event.data
      if (ev.notebook === nb && ev.section === sec && ev.page === pg) {
        void loadPage(false)
      }
    })
    return () => off()
  })

  async function loadPage(showLoader = true) {
    if (showLoader) {
      loading = true
    }
    loadError = ''
    const reqNotebook = notebook
    const reqSection = section
    const reqPage = page
    try {
      const result = await FetchPageBlocks(reqNotebook, reqSection, reqPage)
      if (notebook !== reqNotebook || page !== reqPage) {
        return
      }
      blocks = result || []
    } catch (e) {
      if (notebook !== reqNotebook || page !== reqPage) return
      loadError = e instanceof Error ? e.message : String(e)
      console.error('[VSC] loadPage error:', loadError)
    } finally {
      if (showLoader) {
        loading = false
      }
    }
  }

  /** Scroll to targetBlockId or targetHeading. Returns true if the target was
   *  found and scrolled; false when the page is still loading (caller retries).
   *  Bounded by MAX_SCROLL_ATTEMPTS so a permanently-absent target can't cause
   *  unbounded re-scrolls after later block updates (e.g. autosave). */
  const MAX_SCROLL_ATTEMPTS = 5
  async function tryScrollToTarget(key: string): Promise<boolean> {
    await tick()
    scrollAttemptCount++
    // After too many attempts, give up — the target is genuinely absent.
    if (scrollAttemptCount > MAX_SCROLL_ATTEMPTS) {
      handledTargetKey = key
      return false
    }
    if (targetBlockId) {
      const el = document.querySelector(`[data-id="${targetBlockId}"]`)
      if (el instanceof HTMLElement) {
        el.scrollIntoView({ block: 'center', behavior: 'smooth' })
        handledTargetKey = key
        return true
      }
      // DOM may lag setContent; only mark handled if blocks list is empty
      // (nothing will appear) or the id is known missing after load.
      if (!loading && blocks.length > 0) {
        const known = blocks.some((b) => b.id === targetBlockId)
        if (!known) {
          handledTargetKey = key
          return false
        }
      }
      return false
    }
    // Wiki-link #heading: scroll to the HEADER whose clean_text matches (#545).
    if (targetHeading) {
      if (loading || blocks.length === 0) return false
      const header = blocks.find(
        (b) =>
          b.type === 'HEADER' &&
          (b.clean_text === targetHeading ||
            b.clean_text?.replace(/^#+\s*/, '') === targetHeading)
      )
      if (header?.id) {
        const el = document.querySelector(`[data-id="${header.id}"]`)
        if (el instanceof HTMLElement) {
          el.scrollIntoView({ block: 'center', behavior: 'smooth' })
          handledTargetKey = key
          return true
        }
        // Header known in index but not yet in DOM — retry on next tick cycle.
        return false
      }
      // No matching header after load — give up so we don't loop forever.
      handledTargetKey = key
      return false
    }
    handledTargetKey = key
    return false
  }

  function handleBlocksUpdated(updatedBlocks: ParsedBlock[]) {
    blocks = updatedBlocks
    // Fire onFirstEdit on the first content change — used by the tab strip
    // to promote a preview tab to pinned (edit-to-pin, #142).
    if (!hasFirstEdit) {
      hasFirstEdit = true
      onFirstEdit?.()
    }
  }

  function formatDate(d: string): string {
    const parsed = new Date(d + 'T00:00:00')
    if (isNaN(parsed.getTime())) return d
    return parsed.toLocaleDateString('en-US', {
      weekday: 'long',
      month: 'long',
      day: 'numeric',
      year: 'numeric'
    })
  }

  // --- Inline title editing (#83) ---
  let titleEl = $state<HTMLHeadingElement | null>(null)
  let renameTimer: ReturnType<typeof setTimeout> | null = null
  let lastRenamedFrom = ''
  // Track whether the title is actively focused so we can guard reactive
  // re-patching from the `page` prop (#259). Without this guard, Svelte
  // patches the `<h1>` text whenever `page` changes — which happens after
  // every debounced rename round-trip, collapsing the caret to position 0.
  let titleFocused = $state(false)
  let displayTitle = $state(untrack(() => page))

  // Sync displayTitle from the page prop ONLY when the user is not editing.
  // When focused, the DOM is the source of truth (the user's caret position
  // must be preserved across rename round-trips).
  $effect(() => {
    if (!titleFocused) {
      displayTitle = page
    }
  })

  function handleFocusTitle() {
    if (titleEl) {
      titleEl.focus()
      // Select all text so typing replaces "Untitled"
      const range = document.createRange()
      range.selectNodeContents(titleEl)
      const sel = window.getSelection()
      sel?.removeAllRanges()
      sel?.addRange(range)
    }
  }

  // Listen for the focus-page-title event (from sidebar page creation/rename).
  $effect(() => {
    const handler = () => handleFocusTitle()
    window.addEventListener('focus-page-title', handler)
    return () => window.removeEventListener('focus-page-title', handler)
  })

  function handleTitleInput() {
    if (!titleEl) return
    const newName = titleEl.textContent?.trim() ?? ''
    if (newName === '' || newName === page) return
    // Debounce the rename (500ms after last keystroke).
    if (renameTimer) clearTimeout(renameTimer)
    renameTimer = setTimeout(() => {
      void doRename(newName)
    }, 500)
  }

  function handleTitleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault()
      titleEl?.blur()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      displayTitle = page
      // eslint-disable-next-line svelte/no-dom-manipulating -- imperative title sync avoids full re-render thrash during virtual scroll
      if (titleEl) titleEl.textContent = page
      titleEl?.blur()
    }
  }

  function handleTitleBlur() {
    titleFocused = false
    if (renameTimer) {
      clearTimeout(renameTimer)
      renameTimer = null
    }
    const newName = titleEl?.textContent?.trim() ?? ''
    if (newName === '' || newName === page) {
      displayTitle = page
      // eslint-disable-next-line svelte/no-dom-manipulating -- imperative title sync avoids full re-render thrash during virtual scroll
      if (titleEl) titleEl.textContent = page
      return
    }
    void doRename(newName)
  }

  async function doRename(newName: string) {
    if (newName === page || newName === lastRenamedFrom) return
    lastRenamedFrom = newName
    try {
      await RenamePage(notebook, section, page, newName)
      onPageRenamed?.(newName)
      window.dispatchEvent(new CustomEvent('refresh-navigation'))
    } catch (e) {
      console.error('RenamePage failed:', e)
      displayTitle = page
      // eslint-disable-next-line svelte/no-dom-manipulating -- imperative title sync avoids full re-render thrash during virtual scroll
      if (titleEl) titleEl.textContent = page
      lastRenamedFrom = ''
    }
  }

  let pageDate = $derived.by(() => {
    const dates = blocks
      .map((b) => b.file_date)
      .filter((d): d is string => !!d)
      .sort()
    if (dates.length > 0) return dates[0]
    return new Date().toISOString().slice(0, 10)
  })

  // --- Edit↔Source restore: scroll (#319) + caret (#331) ---
  // Sprint 15's editor-teardown unmounts TipTap in Source view. Raw PM
  // positions die with the editor, so we snapshot scrollTop plus a stable
  // blockId + relative offset, then re-apply after onReady.
  let prevViewMode: ViewMode = untrack(() => viewMode)
  let savedEditScroll = 0
  let savedEditCaret: EditCaretSnapshot | null = null
  let pendingRestore = false
  // TipTap's blocks→setContent $effect often runs after onCreate/onReady and
  // wipes the first setTextSelection. Keep the snapshot for one re-apply pass
  // after content sync + layout frames (PLAN #331 task 3).
  let pendingCaretReapply: EditCaretSnapshot | null = null
  let caretRestoreGen = 0

  // A remount restore can still have tick/rAF callbacks queued when Source
  // navigation or test cleanup destroys this instance. Invalidate them so a
  // stale snapshot cannot target the next editor instance.
  onDestroy(() => {
    caretRestoreGen++
    pendingCaretReapply = null
  })

  // $effect.pre runs ahead of the DOM update, so containerEl.scrollTop and the
  // live editor selection still reflect Edit mode at the unmount boundary —
  // a regular $effect would read post-teardown state.
  $effect.pre(() => {
    const cur = viewMode
    if (prevViewMode === 'edit' && cur === 'source' && containerEl) {
      savedEditScroll = containerEl.scrollTop
      savedEditCaret = snapshotEditCaret(editorInstance)
      pendingRestore = true
      pendingCaretReapply = null
      caretRestoreGen++
    }
    prevViewMode = cur
  })

  function tryApplyCaret(snap: EditCaretSnapshot | null): void {
    applyEditCaret(editorInstance, snap)
  }

  async function handleEditorReady() {
    if (!pendingRestore) return
    pendingRestore = false
    const target = savedEditScroll
    const caret = savedEditCaret
    savedEditCaret = null
    const gen = ++caretRestoreGen
    pendingCaretReapply = caret

    // Wait for bind:editorInstance + NodeView flush before touching selection.
    await tick()
    if (gen !== caretRestoreGen) return

    // Caret first: setTextSelection can scroll the view; we re-apply scrollTop
    // afterward so #319 still wins for viewport position.
    tryApplyCaret(caret)

    // TipTap may setContent from blocks in a sibling $effect after onReady —
    // re-apply once the microtask/tick queue drains so we win that race.
    await tick()
    if (gen !== caretRestoreGen) return
    tryApplyCaret(pendingCaretReapply)

    if (target > 0 && containerEl) {
      // Restore once the remounted NodeViews have flushed. Async renderers
      // (KaTeX/Mermaid lazy load, Shiki debounce) settle AFTER the first frame
      // and grow scrollHeight, so a single rAF would clamp too early on
      // math/diagram-heavy pages. Re-clamp across a couple of frames: each clamps
      // to the largest valid offset, settling at `target` once the doc is tall
      // enough (and never overscrolling if it shrank).
      const restoreScroll = () => {
        if (!containerEl) return
        containerEl.scrollTop = Math.min(target, containerEl.scrollHeight)
      }
      requestAnimationFrame(() => {
        if (gen !== caretRestoreGen) return
        tryApplyCaret(pendingCaretReapply)
        restoreScroll()
        requestAnimationFrame(() => {
          if (gen !== caretRestoreGen) return
          tryApplyCaret(pendingCaretReapply)
          restoreScroll()
          // End the remount re-apply window; later external edits must not jump.
          if (gen === caretRestoreGen) pendingCaretReapply = null
        })
      })
    } else {
      // No scroll target — still clear after a second tick for setContent race.
      await tick()
      if (gen === caretRestoreGen) {
        tryApplyCaret(pendingCaretReapply)
        pendingCaretReapply = null
      }
    }
  }

  // When blocks change while a remount restore is still open, TipTap's
  // setContent path may have just run — re-apply caret once after that flush.
  // Early-return when no restore is pending so we do not subscribe to blocks
  // during normal typing. While open, read length + ends only (not a full id join).
  $effect(() => {
    const snap = pendingCaretReapply
    if (!snap || !editorInstance) return
    const n = blocks.length
    void n
    if (n > 0) {
      void blocks[0].id
      void blocks[n - 1].id
    }
    const gen = caretRestoreGen
    void tick().then(() => {
      if (gen !== caretRestoreGen || pendingCaretReapply !== snap) return
      tryApplyCaret(snap)
    })
  })
</script>

<div
  class="flex-1 flex flex-col min-h-0 h-full overflow-hidden bg-surface-app relative"
>
  {#if viewMode === 'edit' && findBarState.open}
    <FindBar editor={editorInstance!} onClose={() => findBarState.close()} />
  {/if}
  {#if showEditorUtilityBar}
    <EditorUtilityBar
      editor={editorInstance}
      {activeMarks}
      {pageLocator}
      showFormatting={viewMode === 'edit' && showFormatToolbar}
    />
  {/if}

  <div class="flex flex-1 min-h-0">
    <!-- containerEl is intentionally NOT a flex container. The .silt-texture-
       surface::before overlay (index.css) uses position:sticky with
       height:100vh + margin-bottom:-100vh to pin the theme's paper texture
       across the scroll viewport. That cancel trick relies on BLOCK flow —
       when this element was `flex flex-col`, the ::before became a flex item
       and the vh/negative-margin pair stopped cancelling, which collapsed the
       content geometry and broke scrolling on textured themes (Linen). -->
    <div
      bind:this={containerEl}
      class="silt-texture-surface flex-1 overflow-y-auto px-12 py-10 custom-scrollbar bg-surface-editor min-h-0"
    >
      <!-- Page zoom scales title + editor/source only — not utility bar/find/chrome (#843). -->
      <div
        class="relative z-[1] flex flex-col note-page-zoom"
        style={showEditorUtilityBar ? `zoom: ${noteZoom.factor}` : undefined}
        data-testid="note-page-zoom"
      >
        <header class="mb-8">
          <h1
            bind:this={titleEl}
            contenteditable="true"
            spellcheck="false"
            oninput={handleTitleInput}
            onkeydown={handleTitleKeydown}
            onblur={handleTitleBlur}
            onfocus={() => (titleFocused = true)}
            class="font-headline-lg text-headline-lg text-text-primary tracking-tight mb-1 outline-none rounded-sm transition-colors"
            style="border-bottom: 1px solid transparent; padding-bottom: 1px;"
            aria-label="Page title"
          >
            {displayTitle}
          </h1>
          <p class="text-text-muted/60 text-sm font-body-sm">
            {formatDate(pageDate)}
          </p>
        </header>

        <div class="max-w-4xl w-full flex-1 flex flex-col gap-4">
          {#if loadError}
            <div
              class="text-error py-8 text-center font-body-md border border-error-border bg-error-bg rounded-lg flex flex-col items-center gap-3"
            >
              <div>Failed to load page: {loadError}</div>
              <button
                onclick={() => loadPage()}
                class="px-4 py-1.5 rounded-lg bg-error/20 border border-error-border text-error font-label-sm-bold hover:brightness-110 transition-all cursor-pointer"
              >
                Retry
              </button>
            </div>
          {:else}
            {#if viewMode === 'source'}
              <!-- Source view (#171/#194/#660): editable markdown. TipTapEditor
                 is NOT mounted here — Svelte tears the whole editor down on
                 the switch (#178). Returning to Edit remounts from `blocks`
                 (updated via onBlocksSaved after source save). -->
              <MarkdownSourceViewer
                {blocks}
                {notebook}
                {section}
                {page}
                filePath="{notebook}/{section}/{page}.md"
                onBlocksSaved={(saved) => {
                  blocks = saved
                }}
              />
            {:else}
              <TipTapEditor
                {notebook}
                {section}
                {page}
                {blocks}
                {activeFocusedBlockAncestors}
                {onBlockFocus}
                {onBlockBlur}
                onUpdate={handleBlocksUpdated}
                onReady={handleEditorReady}
                bind:editorInstance
                bind:activeMarks
                bind:wordCount
                onSaveStateChange={(s) => {
                  saveError = s.error
                  onSaveStateChange?.(s)
                }}
              />
            {/if}
          {/if}

          {#if loading}
            <div class="flex justify-center py-6">
              <span class="text-accent-primary-start font-body-md animate-pulse"
                >Loading...</span
              >
            </div>
          {/if}
        </div>
      </div>
    </div>

    {#if outlineOpen && viewMode === 'edit'}
      <OutlinePanel
        editor={editorInstance}
        scrollParent={containerEl}
        open={true}
        onToggle={() => {
          outlineOpen = false
        }}
      />
    {/if}
  </div>

  <!-- Top-right: flyout only on the ⋯ cluster — calendar is outside so it
       does not open the tray. -->
  <div
    class="editor-float-row"
    class:top-4={!showEditorUtilityBar}
    class:top-14={showEditorUtilityBar}
    data-testid="editor-float-actions"
  >
    <div
      class="editor-float-actions"
      class:editor-float-actions--pinned={editorActionsPinned}
      role="toolbar"
      aria-label="Editor actions"
    >
      <!-- Tray grows left of the ⋯ peek. -->
      <div class="editor-float-actions__tray">
        <button
          type="button"
          onclick={toggleFocusMode}
          class="editor-float-actions__btn"
          class:editor-float-actions__btn--on={settings.config?.editor
            ?.focus_mode === true}
          title={settings.config?.editor?.focus_mode === true
            ? `Exit Focus Mode${focusModeHotkey ? ` (${focusModeHotkey})` : ''}`
            : `Enter Focus Mode${focusModeHotkey ? ` (${focusModeHotkey})` : ''}`}
          aria-label="Toggle Focus Mode"
        >
          <span
            class="material-symbols-outlined text-icon-lg"
            aria-hidden="true">center_focus_strong</span
          >
        </button>

        <button
          type="button"
          onclick={toggleFormatToolbar}
          class="editor-float-actions__btn"
          class:editor-float-actions__btn--on={showFormatToolbar}
          title={showFormatToolbar
            ? `Hide Formatting Toolbar${formatToolbarHotkey ? ` (${formatToolbarHotkey})` : ''}`
            : `Show Formatting Toolbar${formatToolbarHotkey ? ` (${formatToolbarHotkey})` : ''}`}
          aria-label="Toggle Formatting Toolbar"
        >
          <span
            class="material-symbols-outlined text-icon-lg"
            aria-hidden="true">text_format</span
          >
        </button>

        {#if viewMode === 'edit'}
          <button
            type="button"
            onclick={() => {
              outlineOpen = !outlineOpen
            }}
            class="editor-float-actions__btn"
            class:editor-float-actions__btn--on={outlineOpen}
            title={outlineOpen ? 'Hide outline' : 'Show outline'}
            aria-label="Toggle document outline"
            aria-pressed={outlineOpen}
          >
            <span
              class="material-symbols-outlined text-icon-lg"
              aria-hidden="true">list</span
            >
          </button>
        {/if}

        <div class="editor-float-actions__sep" aria-hidden="true"></div>

        <button
          type="button"
          onclick={() => onToggleViewMode?.()}
          class="editor-float-actions__btn"
          title={viewMode === 'edit'
            ? `View Markdown Source (${viewModeHotkey})`
            : `View Rich Text (${viewModeHotkey})`}
          aria-label="Toggle source view"
          aria-pressed={viewMode === 'source'}
          aria-keyshortcuts={viewModeHotkey}
        >
          <span
            class="material-symbols-outlined text-icon-lg"
            aria-hidden="true"
          >
            {viewMode === 'edit' ? 'code' : 'menu_book'}
          </span>
        </button>
      </div>

      <span class="editor-float-actions__peek" aria-hidden="true">
        <span class="material-symbols-outlined text-icon-lg">more_horiz</span>
      </span>
    </div>

    <!-- Outside the hover group — does not open the actions tray. -->
    <DateGlanceChip active={_isActive} />
  </div>

  {#if viewMode === 'edit'}
    <!-- Persistent live region: a fresh-mount aria-live block can be missed by
         screen readers, so the save-error text lives in a stable region. -->
    <div class="sr-only" role="status" aria-live="assertive">
      {saveError ?? ''}
    </div>
  {/if}
  <!-- Bottom status: word count always on when enabled. Zoom flyout only from
       the % control (not word count). Outside .note-page-zoom. -->
  {#if viewMode === 'edit' && showEditorUtilityBar}
    <div
      class="editor-status-pill"
      data-testid="editor-status-pill"
      aria-label="Editor status"
    >
      {#if saveError}
        <div class="editor-status-pill__error">
          <span class="editor-status-pill__error-dot" aria-hidden="true"></span>
          <span class="editor-status-pill__error-label">Save failed</span>
        </div>
        <div class="editor-status-pill__sep" aria-hidden="true"></div>
      {/if}

      <!-- Hover/focus only on this wrap opens zoom − / +. % always visible. -->
      <div
        class="editor-status-pill__zoom-wrap"
        class:editor-status-pill__zoom-wrap--pinned={editorStatusPinned}
        role="group"
        aria-label="Page zoom"
      >
        <button
          type="button"
          class="editor-status-pill__zoom-btn editor-status-pill__zoom-extra"
          onclick={() => noteZoom.zoomOut()}
          disabled={noteZoom.factor <= NOTE_ZOOM_MIN}
          aria-label="Zoom out"
          title="Zoom out (Ctrl+scroll)"
        >
          <span class="material-symbols-outlined" aria-hidden="true"
            >zoom_out</span
          >
        </button>
        <button
          type="button"
          class="editor-status-pill__zoom-pct font-label-sm"
          onclick={() => noteZoom.reset()}
          aria-label={`Zoom ${noteZoom.percent}%. Reset to 100%`}
          title="Zoom (Ctrl+scroll). Click to reset to 100%"
        >
          {noteZoom.percent}%
        </button>
        <button
          type="button"
          class="editor-status-pill__zoom-btn editor-status-pill__zoom-extra"
          onclick={() => noteZoom.zoomIn()}
          disabled={noteZoom.factor >= NOTE_ZOOM_MAX}
          aria-label="Zoom in"
          title="Zoom in (Ctrl+scroll)"
        >
          <span class="material-symbols-outlined" aria-hidden="true"
            >zoom_in</span
          >
        </button>
      </div>

      {#if showWordCount}
        <div class="editor-status-pill__sep" aria-hidden="true"></div>
        <div
          class="editor-status-pill__words font-mono"
          role="status"
          aria-live="off"
        >
          {wordCount}
          {wordCount === 1 ? 'word' : 'words'}
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  h1[contenteditable] {
    transition: border-bottom-color 0.25s ease-in-out;
  }
  h1[contenteditable]:hover {
    border-bottom-color: var(--color-surface-editor-border) !important;
  }
  h1[contenteditable]:focus {
    border-bottom-color: var(--color-accent-primary-start) !important;
  }
  h1[contenteditable]:empty::before {
    content: 'Untitled';
    color: var(--color-text-muted);
    opacity: 0.4;
  }

  /* --- Top-right: row positions cluster + calendar; only ⋯ cluster expands --- */
  .editor-float-row {
    position: absolute;
    right: 1.5rem;
    z-index: 40;
    display: flex;
    align-items: center;
    gap: 0.35rem;
  }

  .editor-float-row.top-4 {
    top: 1rem;
  }

  .editor-float-row.top-14 {
    top: 3.5rem;
  }

  .editor-float-actions {
    display: flex;
    align-items: center;
    gap: 0.1rem;
    padding: 0.2rem;
    border-radius: 9999px;
    background: color-mix(
      in srgb,
      var(--color-surface-popover) 55%,
      transparent
    );
    backdrop-filter: blur(10px);
    border: 1px solid
      color-mix(in srgb, var(--color-surface-popover-border) 45%, transparent);
    box-shadow: 0 4px 18px color-mix(in srgb, black 8%, transparent);
    transition:
      background 160ms ease,
      border-color 160ms ease,
      box-shadow 160ms ease,
      opacity 160ms ease;
    opacity: 0.42;
  }

  .editor-float-actions__peek {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 1.75rem;
    height: 1.75rem;
    color: var(--color-text-muted);
    flex-shrink: 0;
    transition:
      width 140ms ease,
      opacity 120ms ease;
  }

  .editor-float-actions__tray {
    display: flex;
    align-items: center;
    gap: 0.15rem;
    max-width: 0;
    opacity: 0;
    overflow: hidden;
    transition:
      max-width 200ms ease,
      opacity 140ms ease;
  }

  .editor-float-actions:hover,
  .editor-float-actions:focus-within,
  .editor-float-actions--pinned {
    opacity: 1;
    background: color-mix(
      in srgb,
      var(--color-surface-popover) 82%,
      transparent
    );
    border-color: color-mix(
      in srgb,
      var(--color-surface-popover-border) 65%,
      transparent
    );
    box-shadow: 0 6px 22px color-mix(in srgb, black 12%, transparent);
  }

  .editor-float-actions:hover .editor-float-actions__peek,
  .editor-float-actions:focus-within .editor-float-actions__peek,
  .editor-float-actions--pinned .editor-float-actions__peek {
    width: 0;
    opacity: 0;
    overflow: hidden;
    pointer-events: none;
    transition:
      width 120ms ease 80ms,
      opacity 80ms ease 60ms;
  }

  .editor-float-actions:hover .editor-float-actions__tray,
  .editor-float-actions:focus-within .editor-float-actions__tray,
  .editor-float-actions--pinned .editor-float-actions__tray {
    max-width: 18rem;
    opacity: 1;
  }

  .editor-float-actions__btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 2rem;
    height: 2rem;
    padding: 0;
    border: none;
    border-radius: 9999px;
    background: transparent;
    color: var(--color-text-muted);
    cursor: pointer;
    flex-shrink: 0;
    transition:
      color 120ms ease,
      background 120ms ease;
  }

  .editor-float-actions__btn:hover {
    background: var(--color-hover);
  }

  .editor-float-actions__btn:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: 1px;
  }

  .editor-float-actions__btn--on {
    color: var(--color-accent-primary-start);
  }

  .editor-float-actions__sep {
    width: 1px;
    height: 1rem;
    margin-inline: 0.15rem;
    background: var(--color-surface-popover-border);
    flex-shrink: 0;
  }

  /* --- Bottom: word count always visible; zoom flyout only from % wrap --- */
  .editor-status-pill {
    position: absolute;
    bottom: 1.5rem;
    right: 1.5rem;
    z-index: 40;
    display: flex;
    align-items: center;
    gap: 0.35rem;
    padding: 0.3rem 0.55rem 0.3rem 0.4rem;
    border-radius: 9999px;
    background: color-mix(
      in srgb,
      var(--color-surface-popover) 78%,
      transparent
    );
    backdrop-filter: blur(10px);
    border: 1px solid
      color-mix(in srgb, var(--color-surface-popover-border) 55%, transparent);
    box-shadow: 0 4px 16px color-mix(in srgb, black 10%, transparent);
    color: var(--color-text-muted);
    font-size: var(--text-type-xs, 0.75rem);
    font-weight: 500;
    letter-spacing: 0.01em;
    user-select: none;
    opacity: 0.78;
  }

  .editor-status-pill__zoom-wrap {
    display: flex;
    align-items: center;
    flex-shrink: 0;
    border-radius: 9999px;
    transition: background 120ms ease;
  }

  .editor-status-pill__zoom-wrap:hover,
  .editor-status-pill__zoom-wrap:focus-within,
  .editor-status-pill__zoom-wrap--pinned {
    background: color-mix(in srgb, var(--color-hover) 55%, transparent);
  }

  /* − / + only appear when the zoom wrap (not word count) is hovered. */
  .editor-status-pill__zoom-extra {
    max-width: 0;
    opacity: 0;
    overflow: hidden;
    margin: 0;
    padding: 0;
    border: none;
    pointer-events: none;
    transition:
      max-width 180ms ease,
      opacity 120ms ease;
  }

  .editor-status-pill__zoom-wrap:hover .editor-status-pill__zoom-extra,
  .editor-status-pill__zoom-wrap:focus-within .editor-status-pill__zoom-extra,
  .editor-status-pill__zoom-wrap--pinned .editor-status-pill__zoom-extra {
    max-width: 1.75rem;
    opacity: 1;
    pointer-events: auto;
  }

  .editor-status-pill__sep {
    width: 1px;
    height: 0.85rem;
    background: var(--color-surface-popover-border);
    flex-shrink: 0;
  }

  .editor-status-pill__error {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    padding-inline: 0.15rem;
  }

  .editor-status-pill__error-dot {
    width: 0.45rem;
    height: 0.45rem;
    border-radius: 9999px;
    background: var(--color-status-danger);
    animation: editor-status-pulse 1.4s ease-in-out infinite;
    flex-shrink: 0;
  }

  .editor-status-pill__error-label {
    color: var(--color-status-danger);
    font-weight: 600;
    white-space: nowrap;
  }

  .editor-status-pill__zoom-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 1.55rem;
    height: 1.55rem;
    padding: 0;
    border: none;
    border-radius: 9999px;
    background: transparent;
    color: var(--color-text-muted);
    cursor: pointer;
    transition:
      color 120ms ease,
      background 120ms ease;
  }

  .editor-status-pill__zoom-btn .material-symbols-outlined {
    font-size: 1rem;
  }

  .editor-status-pill__zoom-btn:hover:not(:disabled) {
    color: var(--color-text-primary);
    background: var(--color-hover);
  }

  .editor-status-pill__zoom-btn:disabled {
    opacity: 0.35;
    cursor: default;
  }

  .editor-status-pill__zoom-btn:focus-visible,
  .editor-status-pill__zoom-pct:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: 1px;
  }

  .editor-status-pill__zoom-pct {
    min-width: 2.6rem;
    height: 1.55rem;
    padding: 0 0.2rem;
    border: none;
    border-radius: 9999px;
    background: transparent;
    color: var(--color-text-muted);
    font-size: inherit;
    text-align: center;
    cursor: pointer;
    transition:
      color 120ms ease,
      background 120ms ease;
  }

  .editor-status-pill__zoom-pct:hover {
    color: var(--color-text-primary);
    background: var(--color-hover);
  }

  .editor-status-pill__words {
    padding-inline: 0.25rem 0.35rem;
    color: color-mix(in srgb, var(--color-text-muted) 88%, transparent);
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }

  @keyframes editor-status-pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.45;
    }
  }
</style>
