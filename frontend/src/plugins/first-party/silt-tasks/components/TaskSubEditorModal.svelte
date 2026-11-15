<script lang="ts">
  import { onMount, onDestroy, untrack } from 'svelte'
  import { createEditor, EditorContent } from 'svelte-tiptap'
  import type { Editor } from 'svelte-tiptap'
  import StarterKit from '@tiptap/starter-kit'
  import { Extension } from '@tiptap/core'
  import Placeholder from '@tiptap/extension-placeholder'
  import { CharacterCount, Focus, TrailingNode } from '@tiptap/extensions'
  import {
    SiltBlockExtensionsWithNodeViews,
    SiltInlineMarkExtensions,
    SiltColorMarkExtensions,
    SiltDetailsExtensions,
    SiltTableExtensions,
    UniqueBlockIds,
    ExternalPasteNormalizer,
    SiltHardBreak,
    SiltBlockKeymaps,
    blocksToDoc,
    docToBlocks,
    findActiveBlock
  } from '../../../../lib/editor'
  import type { ParsedBlock } from '../../../../lib/editor'
  import { siltPasteProps } from '../../../../lib/editor/schema'
  import type { PluginContext } from '../../../sdk'
  import { STANDALONE_TASKS_NOTEBOOK } from '../../../../lib/standaloneTasksNav'
  import TaskMetadataSidebar from './TaskMetadataSidebar.svelte'
  import { friendlyCaughtError } from '../errors'
  import { partitionComments } from '../commentBlocks'
  import type { TaskDetail } from '../types'
  import { fetchTaskDetail } from '../query'
  import Dialog from '../../../../components/ui/Dialog.svelte'

  /**
   * Focused Task Sub-Editor Modal (#304, #780). Opens as a glassy overlay
   * hosting a scoped TipTap instance seeded with the task's child sub-tree.
   * On save (debounced), the edited sub-tree is spliced back into the parent
   * file atomically via saveSubtreeBlocks (#305).
   *
   * Two-column body (#780): main = scoped TipTap editor with its autosave
   * pipeline; right sidebar = TaskMetadataSidebar (the same metadata surface
   * the TaskEditDrawer uses). On narrow viewports the sidebar collapses into
   * a disclosure so the editor keeps space.
   *
   * Standard modal pattern: focus-trap + Esc-with-unsaved-prompt +
   * focus-restore. The status footer carries "Convert note to comment"
   * (#1159): it flushes the autosave, flips the block server-side, then
   * reloads so the note leaves this editor's document.
   */
  interface Props {
    blockId: string
    notebook: string
    section: string
    page: string
    parentTaskText: string
    ctx: PluginContext
    onClose: () => void
    /** Fired after a successful metadata write so the host can re-query. */
    onMetaChanged?: () => void
    /** Pre-fetched task detail; when supplied the modal seeds the metadata
     *  sidebar synchronously and skips its own fetch (avoids a late sidebar
     *  pop-in on open). */
    initialTask?: TaskDetail
  }

  let {
    blockId,
    notebook,
    section,
    page,
    parentTaskText,
    initialTask,
    ctx,
    onClose,
    onMetaChanged
  }: Props = $props()

  let isStandalone = $derived(notebook === STANDALONE_TASKS_NOTEBOOK)

  // --- Task detail hydration (#780) ---
  // The modal is opened with parentTaskText (the task's clean_content from the
  // host view). When the host supplies initialTask (the page-editor path), the
  // sidebar seeds from it synchronously and skips the fetch. Otherwise we
  // re-fetch the full TaskDetail via fetchTaskDetail so the sidebar has every
  // field; parentTaskText is the optimistic title before the fetch resolves.
  let task = $state<TaskDetail | null>(untrack(() => initialTask ?? null))

  // --- Editor setup ---
  let editorInstance: Editor | null = $state(null)
  let loading = $state(true)
  let loadError = $state('')
  let unsavedChanges = $state(false)
  let saveError = $state('')
  let saving = $state(false)
  // Set whenever the editor doc does not match disk: it starts true because
  // nothing has loaded yet (empty doc, subtree unknown), and a conversion that
  // wrote to disk followed by a failed reload leaves a doc still holding the
  // converted block as unmarked prose. Saving either splices it over the file
  // (caller-supplied content wins) — losing the task's sub-notes, or undoing
  // the conversion. Read-only plus a persist() veto until a load succeeds.
  let docStale = $state(true)
  let saveTimer: ReturnType<typeof setTimeout> | null = null
  let pendingSnapshot: ParsedBlock[] | null = null
  let pendingSnapshotVersion = -1
  // Monotonic doc-version, bumped on every user edit. A save only owns the
  // unsaved flag it started with: an edit landing during the IPC flight bumps
  // this, and the completion must then leave the flag set.
  let docVersion = 0
  // Set when a close was requested but held back by a save in flight; a later
  // clean settle honours the close instead of leaving the modal open silently.
  let closePending = false
  // Single-flight save pump: one write at a time, drained by one loop that
  // records `flushPromise`. `pumping` is only true while that promise is still
  // pending, so awaiting it can never spin on a settled promise.
  let pumping = false
  let flushPromise: Promise<void> = Promise.resolve()
  let closed = false

  let suppressUpdate = false

  // --- Focus trap (shared focusTrap; Esc stays surface-owned) ---
  let dialogRef = $state<HTMLDivElement | null>(null)
  let previouslyFocused: HTMLElement | null = null

  const extensions = [
    StarterKit.configure({
      heading: false,
      bulletList: false,
      orderedList: false,
      listItem: false,
      blockquote: false,
      codeBlock: false,
      horizontalRule: false,
      trailingNode: false,
      // Stock HardBreak refuses isolating parents; SiltHardBreak replaces it (#828).
      hardBreak: false,
      link: { openOnClick: false, autolink: true }
    }),
    SiltHardBreak,
    ...SiltBlockExtensionsWithNodeViews,
    ...SiltInlineMarkExtensions,
    ...SiltColorMarkExtensions,
    ...SiltDetailsExtensions,
    ...SiltTableExtensions,
    UniqueBlockIds,
    ExternalPasteNormalizer,
    // Outliner Enter + Shift-Enter soft break (#828). siltSubEditorHost below
    // keeps task Shift-Enter from re-opening the modal for nested tasks.
    SiltBlockKeymaps,
    TrailingNode.configure({
      node: 'noteBlock',
      notAfter: ['taskBlock', 'headerBlock', 'calloutBlock']
    }),
    Placeholder.configure({
      placeholder: 'Write sub-notes, sub-tasks…'
    }),
    CharacterCount,
    Focus,
    // Marks this editor as the Task Sub-Editor modal host so TaskBlockView can
    // suppress its "open in modal" pencil for nested sub-tasks (#781). Drilling
    // into a sub-task from inside the modal would swap it via {#key} with no
    // autosave-flush guarantee, risking dropped keystrokes and a confusing
    // affordance — the in-page editor is the only intended entry point.
    Extension.create({
      name: 'siltSubEditorHost',
      addStorage: () => ({ active: true })
    })
  ]

  const editorStore = untrack(() =>
    createEditor({
      extensions,
      content: blocksToDoc([]),
      // Read-only until the first load succeeds: the empty doc does not match
      // disk, so it must not accept keystrokes (or autosave) before then.
      editable: false,
      // Same paste semantics as the page editor (shared helper).
      editorProps: siltPasteProps,
      onUpdate: () => {
        // Fold toggles are content now that `open` is disk state (#1123) —
        // collapsing a section arms the unsaved-changes prompt like any edit.
        if (suppressUpdate) return
        docVersion += 1
        unsavedChanges = true
        saveError = ''
        // The user went back to editing, so the earlier close intent is stale.
        closePending = false
        scheduleSave()
      },
      onCreate: ({ editor }) => {
        editorInstance = editor as Editor
        void loadSubtree().then(() => {
          queueMicrotask(() => editorInstance?.commands.focus())
        })
      }
    })
  )

  // Returns whether the editor document now reflects disk. Callers that act on
  // the reload (the conversion) must not infer success from a resolved promise:
  // the fetch failure is captured in loadError, and a torn-down editor skips
  // the update entirely.
  async function loadSubtree(): Promise<boolean> {
    loading = true
    loadError = ''
    try {
      const subtree = await ctx.fetchSubtree(blockId)
      if (!editorInstance || editorInstance.isDestroyed) return false
      suppressUpdate = true
      try {
        const safeSubtree = (subtree ?? []) as ParsedBlock[]
        // Comments belong to CommentThread; keeping them out of the editor
        // doc is what stops a notes save from rewriting their attribution.
        const content = partitionComments(safeSubtree).content
        let normalized = content
        if (content.length > 0) {
          const depths = content
            .map((b) => b.depth)
            .filter((d): d is number => typeof d === 'number')
          const minDepth = depths.length ? Math.min(...depths) : 0
          if (minDepth > 0) {
            normalized = content.map((b) => ({
              ...b,
              depth: Math.max(0, (b.depth ?? minDepth) - minDepth)
            }))
          }
        }
        editorInstance.commands.setContent(blocksToDoc(normalized), {
          emitUpdate: false
        })
      } finally {
        suppressUpdate = false
      }
      unsavedChanges = false
      // The doc matches disk again, so the stale doc (nothing loaded yet, a
      // failed initial load, or a failed post-conversion reload) is no longer a
      // save hazard.
      docStale = false
      applyEditable()
      return true
    } catch (e) {
      // A failed load leaves a doc that does not match disk: empty on the
      // initial load, stale after a conversion. It must not be editable or
      // saveable until a reload succeeds.
      docStale = true
      applyEditable()
      loadError = e instanceof Error ? e.message : String(e)
      return false
    } finally {
      loading = false
    }
  }

  // Hydrate the full TaskDetail so the metadata sidebar has every field. Also
  // the refresh path after a write: a failed or empty re-read keeps the
  // last-known snapshot rather than blanking a sidebar that already rendered
  // (at mount there is none, so the sidebar simply stays hidden).
  async function loadTaskDetail() {
    try {
      const fresh = await fetchTaskDetail(ctx, blockId)
      if (fresh) task = fresh
    } catch {
      // Non-fatal: the previous snapshot stands.
    }
  }

  // The sidebar reports metadata writes and comment-thread mutations through
  // one callback, so every forward also re-reads this task: the Activity
  // summary renders `task.comments_count`, and a comment added, deleted or
  // converted in the sidebar's own thread would otherwise leave it at its
  // mount-time value. The host's callback still runs for its own re-query.
  function handleMetaChanged() {
    onMetaChanged?.()
    void loadTaskDetail()
  }

  function scheduleSave() {
    if (saveTimer) clearTimeout(saveTimer)
    saveTimer = setTimeout(() => {
      persist()
    }, 600)
  }

  function persist(): void {
    // Single veto for every save trigger (debounce, close drain, retry): a doc
    // that disagrees with disk must never reach it.
    if (docStale) return
    const ed = editorInstance
    if (!ed || ed.isDestroyed) return
    // Capturing the snapshot here (not in the pump) means a queued request is
    // written even after the editor is torn down.
    pendingSnapshot = docToBlocks(ed.getJSON())
    pendingSnapshotVersion = docVersion
    // Only the pump starts a write; a request that arrives mid-write just
    // replaces the queued snapshot and is drained by the running loop.
    if (pumping) return
    flushPromise = runSavePump()
  }

  async function runSavePump(): Promise<void> {
    pumping = true
    try {
      while (pendingSnapshot !== null) {
        const snapshot = pendingSnapshot
        const version = pendingSnapshotVersion
        pendingSnapshot = null
        pendingSnapshotVersion = -1
        saving = true
        try {
          await ctx.saveSubtreeBlocks(blockId, snapshot)
          settleSave(version)
        } catch (e) {
          // The modal stays open for Retry/Discard, so a pending close must not
          // fire if a later write happens to succeed.
          closePending = false
          saveError = friendlyCaughtError(e)
        } finally {
          saving = false
        }
      }
    } finally {
      pumping = false
    }
  }

  // A save only vouches for the doc it snapshotted. If an edit landed while the
  // write was in flight the doc-version moved on, so the flag stays set and the
  // close path re-checks (keeping the modal open) instead of discarding the
  // trailing edit.
  function settleSave(version: number) {
    if (docVersion !== version) return
    unsavedChanges = false
    saveError = ''
    // The close that this write was holding back: the newest doc is on disk, so
    // honour it rather than leaving the modal open with no explanation.
    if (closePending) {
      closePending = false
      close()
    }
  }

  async function drainSave(): Promise<void> {
    if (saveTimer) {
      clearTimeout(saveTimer)
      saveTimer = null
    }
    if (unsavedChanges) persist()
    // The pump's loop only exits with an empty queue, so awaiting it covers
    // every request queued so far. The loop here only covers another caller
    // starting a fresh pump while this one unwinds: each iteration awaits a
    // pump that is genuinely still running, so it cannot busy-wait.
    await flushPromise
    while (pumping) await flushPromise
  }

  function close(): void {
    // Closing from a save settle can race the drain that is still unwinding, so
    // this is the one place onClose is invoked.
    if (closed) return
    closed = true
    onClose()
  }

  async function attemptClose() {
    // A stale doc is known to disagree with disk, and persist() vetoes every
    // write from it — so whatever the unsaved flag says, there is nothing
    // recoverable to keep the modal open for. Close explicitly rather than
    // letting the flag dead-end the X/Esc.
    if (docStale) {
      await drainSave()
      close()
      return
    }
    if (unsavedChanges || saving) {
      await drainSave()
      // A failed final save keeps the modal open so the user can retry or
      // explicitly discard the edits instead of losing them on close.
      if (unsavedChanges || saveError) {
        // The Discard/Retry row renders only behind saveError. Without one the
        // close would be inert with no explanation, so surface the block. This
        // fires when an edit landed while the final save was in flight: the
        // save settles but cannot vouch for the newer doc (settleSave), so the
        // flag is still set and no error exists yet.
        //
        // The close intent outlives the message: if the still-armed autosave
        // then lands cleanly, settleSave closes instead of leaving the modal
        // open with a banner that has just cleared.
        closePending = true
        if (!saveError) {
          saveError = 'the latest edits could not be confirmed on disk'
        }
        return
      }
    }
    closePending = false
    close()
  }

  function retrySave(): void {
    if (saving || docStale) return
    // The retry writes the newest doc itself, so a still-armed debounce would
    // only duplicate that write.
    if (saveTimer) {
      clearTimeout(saveTimer)
      saveTimer = null
    }
    saveError = ''
    // Explicit retry is a fresh decision about closing: never auto-close on it.
    closePending = false
    persist()
  }

  function discardChanges(): void {
    if (saving) return
    if (saveTimer) {
      clearTimeout(saveTimer)
      saveTimer = null
    }
    pendingSnapshot = null
    pendingSnapshotVersion = -1
    closePending = false
    unsavedChanges = false
    saveError = ''
    close()
  }

  // --- Explicit comment conversion (#1159) ---
  // The sub-editor is block-scoped, so the affordance targets the note the
  // caret is in (the doc only holds prose: comments are partitioned out).
  // selectionVersion is the reactive edge — moving the caret/typing changes
  // which block is active without any other Svelte state moving.
  let selectionVersion = $state(0)
  let converting = $state(false)
  let convertError = $state('')
  let convertMessage = $state('')

  $effect(() => {
    const ed = editorInstance
    if (!ed) return
    const bump = ({
      transaction
    }: {
      transaction: { docChanged: boolean }
    }) => {
      selectionVersion += 1
      // A conversion result is transient: the next edit restores the hint.
      if (transaction.docChanged) convertMessage = ''
    }
    ed.on('transaction', bump)
    return () => ed.off('transaction', bump)
  })

  function findActiveNoteId(_selectionVersion: number): string | null {
    const ed = editorInstance
    if (!ed || ed.isDestroyed) return null
    const active = findActiveBlock(ed)
    if (!active || active.node.type.name !== 'noteBlock') return null
    const blockID = active.node.attrs.id
    if (typeof blockID !== 'string' || blockID === '') return null
    // An empty note has not reached disk, so there is no block to convert.
    if (active.node.textContent.trim() === '') return null
    return blockID
  }

  let activeNoteId = $derived(findActiveNoteId(selectionVersion))

  // Why the action is unavailable: used as the tooltip and read out through the
  // aria-describedby hint, so a disabled control still states its reason. A
  // stale doc is not the disk state (its converted block still reads as prose,
  // or it never loaded), so converting from it could target the wrong block.
  let convertDisabledReason = $derived(
    docStale
      ? 'Reload the sub-notes before converting again'
      : activeNoteId
        ? 'Move this note to the task comment thread'
        : 'Put the cursor in a note first'
  )

  // Single place that flips the editor's read-only state. Every flip is silent
  // (emitUpdate=false): the doc itself did not change, so it must never mark
  // it dirty or arm an autosave. TipTap's default emitUpdate=true would fire
  // onUpdate, and on the conversion path that would save the pre-conversion
  // doc over the marker the server just wrote.
  function setEditorEditable(editable: boolean) {
    const ed = editorInstance
    if (!ed || ed.isDestroyed) return
    if (ed.isEditable === editable) return
    ed.setEditable(editable, false)
  }

  // Read-only is the visible half of the stale guard: the doc must not accept
  // keystrokes while it disagrees with disk.
  function applyEditable() {
    setEditorEditable(!docStale)
  }

  // The way out of a failed load/conversion reload: re-read the subtree. On
  // success loadSubtree clears the stale flag and loadError, and editing
  // resumes on a doc that matches disk.
  async function retryLoad() {
    if (loading) return
    if (await loadSubtree()) {
      convertError = ''
    }
    applyEditable()
  }

  async function convertActiveNoteToComment() {
    const noteId = activeNoteId
    if (!noteId || converting || docStale) return
    converting = true
    convertError = ''
    convertMessage = ''
    // Freeze the doc: a keystroke landing between the flush and the reload
    // below would be overwritten by loadSubtree (the block leaves the doc).
    // Silent (setEditorEditable), so the freeze does not itself dirty the doc
    // and trigger a redundant subtree write.
    setEditorEditable(false)
    try {
      // Flush the debounced autosave FIRST: the conversion re-reads the file
      // on the server, so unsaved editor content must land before it or the
      // body written back would be pre-edit.
      await drainSave()
      if (saveError || unsavedChanges) {
        convertError =
          'Your edits could not be saved, so the note was not converted.'
        return
      }
      const changed = await ctx.setBlockCommentClassification(noteId, true)
      // Reload so the converted block leaves the editor document; it now
      // belongs to the task's comment thread. A failed or skipped reload
      // leaves the block in the doc as unmarked prose, so that doc must be
      // reported as a failure rather than a success.
      if (!(await loadSubtree())) {
        // loadSubtree's catch marks the doc stale; set it again for the
        // torn-down-editor false path, which is likewise not a disk match.
        docStale = true
        convertError = changed
          ? 'Note converted to a comment on disk, but the editor could not refresh. Saving from this view would undo the conversion — reload the sub-notes, or reopen the sub-editor.'
          : 'The editor could not refresh after the conversion. Saving from this view would undo its comment state — reload the sub-notes, or reopen the sub-editor.'
        return
      }
      convertMessage = changed
        ? 'Note converted to a comment; it now shows in the comment thread.'
        : 'This note was already a comment.'
      // comments_count counts only comment blocks now (#1159), so the
      // conversion moves it. Re-read rather than hand-increment: the derived
      // count is the server's, and the host's card badge refreshes from the
      // block:changed the server emitted.
      if (changed) await loadTaskDetail()
    } catch (e) {
      convertError = friendlyCaughtError(e)
    } finally {
      converting = false
      applyEditable()
    }
  }

  // Mirrors whether the sidebar has a popover/dialog open so Esc doesn't close
  // the modal mid-interaction.
  let sidebarBusy = $state(false)

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      if (sidebarBusy) return
      e.preventDefault()
      e.stopPropagation()
      void attemptClose()
    }
  }

  // --- Responsive sidebar (#780 / #826) ---
  // isNarrow is layout-only (column vs side-by-side). sidebarOpen is the user's
  // open/closed preference: seeded once from the initial viewport, then only
  // changed by the toggle — resize must not clobber a manual choice.
  // Seed synchronously so the first paint matches the viewport (no flash of
  // open sidebar on narrow).
  const initialNarrow =
    typeof window !== 'undefined' &&
    window.matchMedia('(max-width: 768px)').matches
  let isNarrow = $state(initialNarrow)
  let sidebarOpen = $state(!initialNarrow)
  let wideDetailsBtn = $state<HTMLButtonElement | undefined>(undefined)
  let narrowDetailsBtn = $state<HTMLButtonElement | undefined>(undefined)

  $effect(() => {
    const mq = window.matchMedia('(max-width: 768px)')
    const sync = () => {
      isNarrow = mq.matches
    }
    sync()
    mq.addEventListener('change', sync)
    return () => mq.removeEventListener('change', sync)
  })

  function toggleSidebar() {
    const next = !sidebarOpen
    // Before unmounting the sidebar, pull focus back to the toggle so the
    // modal focus trap does not land on a destroyed control.
    if (!next && typeof document !== 'undefined') {
      const active = document.activeElement as HTMLElement | null
      const aside = document.getElementById('sub-editor-sidebar')
      if (active && aside?.contains(active)) {
        const btn = isNarrow ? narrowDetailsBtn : wideDetailsBtn
        btn?.focus()
      }
    }
    sidebarOpen = next
  }

  // Header title prefers the fetched task's clean_content; parentTaskText is
  // the optimistic fallback before the fetch resolves.
  let headerTitle = $derived(task?.clean_content ?? parentTaskText)

  onMount(() => {
    previouslyFocused = document.activeElement as HTMLElement
    // Lock background scroll while the full-screen modal is open.
    const prevOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    if (!initialTask) void loadTaskDetail()
    return () => {
      // Drain before clearing the stale flag: persist() vetoes on it, so
      // clearing first could let a stale doc reach disk on the way out.
      void drainSave().finally(() => {
        docStale = false
      })
      document.body.style.overflow = prevOverflow
      if (previouslyFocused?.isConnected) previouslyFocused.focus?.()
    }
  })

  onDestroy(() => {
    if (editorInstance && !editorInstance.isDestroyed) {
      editorInstance.destroy()
    }
  })

  // The blocking condition reads first: a stale/unsaveable doc is why nothing
  // is landing, so "Load failed" must not sit behind "Unsaved edits".
  let statusText = $derived(
    saving
      ? 'Saving…'
      : saveError
        ? `Save failed: ${saveError}`
        : loadError
          ? `Load failed: ${loadError}`
          : unsavedChanges
            ? 'Unsaved edits'
            : 'Saved'
  )
</script>

<Dialog
  open={true}
  onClose={() => void attemptClose()}
  closeOnEscape={!sidebarBusy}
  zIndex="z-[180]"
  maxWidth={isNarrow ? 'max-w-3xl' : 'max-w-5xl'}
  labelledBy="sub-editor-title"
  backdropLabel="Close sub-editor"
  surfaceClass="h-[80vh] rounded-xl flex flex-col overflow-hidden"
  bind:dialogRef
  onKeydown={handleKeydown}
>
  <!-- Header: breadcrumbs + chrome only. The visible title lives in the
       sidebar's editable input — a second header copy duplicated it and
       truncated against it (D6). -->
  <header
    class="flex items-center gap-3 px-5 py-3 border-b border-surface-modal-border flex-shrink-0"
  >
    <span
      class="material-symbols-outlined text-accent-primary-start text-type-2xl"
      aria-hidden="true">zoom_in</span
    >
    <div class="min-w-0 flex-1">
      {#if !isStandalone}
        <div
          class="text-type-2xs text-text-muted uppercase tracking-widest font-label-sm-bold truncate"
        >
          {notebook}<span
            class="material-symbols-outlined text-type-2xs align-middle"
            aria-hidden="true">chevron_right</span
          >{section || '(none)'}<span
            class="material-symbols-outlined text-type-2xs align-middle"
            aria-hidden="true">chevron_right</span
          >{page}
        </div>
      {/if}
      <!-- Always rendered so labelledBy="sub-editor-title" never dangles,
           including while the sidebar (and its own title input) is shut. -->
      <h2 id="sub-editor-title" class="sr-only">{headerTitle}</h2>
    </div>
    {#if task && !isNarrow}
      <button
        type="button"
        bind:this={wideDetailsBtn}
        class="flex items-center gap-1 text-text-muted hover:text-text-primary transition-colors px-2 py-1 rounded text-type-sm font-label-sm-bold"
        aria-expanded={sidebarOpen}
        aria-controls={sidebarOpen ? 'sub-editor-sidebar' : undefined}
        aria-label={sidebarOpen ? 'Hide details' : 'Show details'}
        onclick={toggleSidebar}
      >
        <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
          >tune</span
        >
        Details
      </button>
    {/if}
    <button
      type="button"
      onclick={attemptClose}
      class="text-text-muted hover:text-text-primary transition-colors p-1 rounded"
      aria-label="Close sub-editor"
    >
      <span class="material-symbols-outlined text-type-2xl" aria-hidden="true"
        >close</span
      >
    </button>
  </header>

  <!-- Two-column body (#780) -->
  <div class="flex-1 flex {isNarrow ? 'flex-col' : 'flex-row'} min-h-0">
    <!-- Main: editor column -->
    <div class="flex-1 flex flex-col min-w-0 min-h-0">
      {#if isNarrow && task}
        <button
          type="button"
          bind:this={narrowDetailsBtn}
          class="flex items-center justify-between px-5 py-2 border-b border-surface-modal-border text-type-sm font-label-sm-bold text-text-primary hover:bg-hover transition-colors flex-shrink-0"
          aria-expanded={sidebarOpen}
          aria-controls={sidebarOpen ? 'sub-editor-sidebar' : undefined}
          aria-label={sidebarOpen ? 'Hide details' : 'Show details'}
          onclick={toggleSidebar}
        >
          <span class="flex items-center gap-1.5">
            <span
              class="material-symbols-outlined text-icon-md"
              aria-hidden="true">tune</span
            >
            Details
          </span>
          <span
            class="material-symbols-outlined text-icon-sm text-text-muted"
            aria-hidden="true"
            >{sidebarOpen ? 'expand_less' : 'expand_more'}</span
          >
        </button>
      {/if}

      <!-- Editor body -->
      <div
        class="relative flex-1 overflow-y-auto custom-scrollbar px-5 py-4 min-h-0"
      >
        {#if loading}
          <div
            class="absolute inset-0 z-10 flex items-center justify-center bg-surface-modal/80 backdrop-blur-xs"
          >
            <div class="text-text-muted font-body-md">Loading sub-notes…</div>
          </div>
        {:else if loadError}
          <div class="flex flex-col items-center gap-3 py-10">
            <div class="text-status-danger text-center font-body-md">
              Load failed: {loadError}
            </div>
            <button
              type="button"
              class="text-type-xs font-label-sm text-text-muted hover:text-text-primary transition-colors px-2 py-1 rounded"
              disabled={loading}
              onclick={() => void retryLoad()}>Reload sub-notes</button
            >
          </div>
        {/if}
        {#if $editorStore}
          <EditorContent editor={$editorStore} />
        {/if}
      </div>

      <!-- Status footer -->
      <footer
        class="flex items-center justify-between gap-3 px-5 py-2 border-t border-surface-modal-border flex-shrink-0"
      >
        <span
          class="text-type-xs font-label-sm {saveError
            ? 'text-status-danger'
            : unsavedChanges
              ? 'text-status-warn'
              : 'text-text-muted'}"
          role={saveError ? 'alert' : 'status'}
          aria-live={saveError ? 'assertive' : 'polite'}
        >
          {statusText}
        </span>
        <span class="flex items-center gap-2 flex-shrink-0">
          <!-- Same text-action styling as the header chrome. Disabled (not
               hidden) when the caret is not in a non-empty note so the action
               stays in the reading order rather than appearing on caret move. -->
          <button
            type="button"
            class="text-type-2xs font-label-sm text-text-muted hover:text-accent-primary-start transition-colors px-1 rounded whitespace-nowrap disabled:opacity-50 disabled:cursor-not-allowed"
            disabled={!activeNoteId || converting || docStale}
            aria-label="Convert the note at the cursor to a comment"
            aria-describedby="sub-editor-convert-note-hint"
            title={convertDisabledReason}
            onclick={() => void convertActiveNoteToComment()}
          >
            Convert note to comment
          </button>
          <span id="sub-editor-convert-note-hint" class="sr-only">
            Converts the note your cursor is in; it moves to the task's comment
            thread.
            {#if docStale}
              Unavailable until the sub-notes are reloaded.
            {/if}
          </span>
          {#if convertMessage}
            <span
              class="text-type-2xs font-label-sm text-text-muted"
              role="status">{convertMessage}</span
            >
          {:else}
            <span class="text-type-2xs text-text-muted font-label-sm">
              Esc to close
            </span>
          {/if}
        </span>
      </footer>
      {#if convertError}
        <div class="flex items-center justify-end gap-2 px-5 pb-2">
          <span
            class="text-type-xs font-label-sm text-status-danger"
            role="alert">{convertError}</span
          >
        </div>
      {/if}
      {#if saveError}
        <div class="flex items-center justify-end gap-2 px-5 pb-2">
          <button
            type="button"
            class="text-type-xs font-label-sm text-text-muted hover:text-text-primary"
            disabled={saving}
            onclick={discardChanges}>Discard changes</button
          >
          <button
            type="button"
            class="text-type-xs font-label-sm text-accent-primary-start hover:text-text-primary"
            disabled={saving}
            onclick={retrySave}>Retry save</button
          >
        </div>
      {/if}
    </div>

    <!-- Sidebar: metadata (#780 / #826) — gated on sidebarOpen for both layouts -->
    {#if task && sidebarOpen && !isNarrow}
      <aside
        id="sub-editor-sidebar"
        class="w-80 flex-shrink-0 border-l border-surface-modal-border overflow-y-auto custom-scrollbar"
      >
        <TaskMetadataSidebar
          {task}
          {ctx}
          onMetaChanged={handleMetaChanged}
          stickyPrimary
          idPrefix="sub-editor-"
          bind:busy={sidebarBusy}
        />
      </aside>
    {:else if task && sidebarOpen && isNarrow}
      <aside
        id="sub-editor-sidebar"
        class="flex-shrink-0 border-t border-surface-modal-border overflow-y-auto custom-scrollbar max-h-[40vh]"
      >
        <TaskMetadataSidebar
          {task}
          {ctx}
          onMetaChanged={handleMetaChanged}
          idPrefix="sub-editor-"
          bind:busy={sidebarBusy}
        />
      </aside>
    {/if}
  </div>
</Dialog>
