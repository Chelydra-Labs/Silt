<script lang="ts">
  import { onMount, onDestroy, untrack } from 'svelte'
  import { createEditor, EditorContent } from 'svelte-tiptap'
  import type { Editor } from 'svelte-tiptap'
  import { fly } from 'svelte/transition'
  import StarterKit from '@tiptap/starter-kit'
  import Placeholder from '@tiptap/extension-placeholder'
  import { CharacterCount, Focus, TrailingNode } from '@tiptap/extensions'
  import {
    SiltBlockExtensionsWithNodeViews,
    SiltInlineMarkExtensions,
    SiltColorMarkExtensions,
    SiltDetailsExtensions,
    SiltTableExtensions,
    UniqueBlockIds,
    blocksToDoc,
    docToBlocks
  } from '../../../../lib/editor'
  import type { ParsedBlock } from '../../../../lib/editor'
  import type { PluginContext } from '../../../sdk'
  import { STANDALONE_TASKS_NOTEBOOK } from '../../../../lib/standaloneTasksNav'
  import TaskMetadataSidebar from './TaskMetadataSidebar.svelte'
  import type { TaskDetail } from '../types'
  import { fetchTaskDetail } from '../query'

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
   * focus-restore.
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
  }

  let {
    blockId,
    notebook,
    section,
    page,
    parentTaskText,
    ctx,
    onClose,
    onMetaChanged
  }: Props = $props()

  let isStandalone = $derived(notebook === STANDALONE_TASKS_NOTEBOOK)

  // --- Task detail hydration (#780) ---
  // The modal is opened with parentTaskText (the task's clean_content from the
  // host view). We re-fetch the full TaskDetail via fetchTaskDetail so the
  // metadata sidebar has every field. parentTaskText is the optimistic title
  // before the fetch resolves.
  let task = $state<TaskDetail | null>(null)

  // --- Editor setup ---
  let editorInstance: Editor | null = $state(null)
  let loading = $state(true)
  let loadError = $state('')
  let unsavedChanges = $state(false)
  let saveError = $state('')
  let saving = $state(false)
  let saveTimer: ReturnType<typeof setTimeout> | null = null
  let saveRequested = false
  let pendingSnapshot: ParsedBlock[] | null = null
  let inFlight: Promise<void> = Promise.resolve()

  let suppressUpdate = false

  // --- Focus trap (Tab/Shift+Tab cycle within the dialog) ---
  let dialogRef = $state<HTMLDivElement | null>(null)
  let previouslyFocused: HTMLElement | null = null
  const FOCUSABLE =
    'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

  function focusableElements(): HTMLElement[] {
    if (!dialogRef) return []
    return Array.from(dialogRef.querySelectorAll<HTMLElement>(FOCUSABLE))
  }

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
      link: { openOnClick: false, autolink: true }
    }),
    ...SiltBlockExtensionsWithNodeViews,
    ...SiltInlineMarkExtensions,
    ...SiltColorMarkExtensions,
    ...SiltDetailsExtensions,
    ...SiltTableExtensions,
    UniqueBlockIds,
    TrailingNode.configure({
      node: 'noteBlock',
      notAfter: ['taskBlock', 'headerBlock', 'calloutBlock']
    }),
    Placeholder.configure({
      placeholder: 'Write sub-notes, sub-tasks…'
    }),
    CharacterCount,
    Focus
  ]

  const editorStore = untrack(() =>
    createEditor({
      extensions,
      content: blocksToDoc([]),
      onUpdate: () => {
        if (suppressUpdate) return
        unsavedChanges = true
        saveError = ''
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

  async function loadSubtree() {
    loading = true
    loadError = ''
    try {
      const subtree = await ctx.fetchSubtree(blockId)
      if (!editorInstance || editorInstance.isDestroyed) return
      suppressUpdate = true
      try {
        const safeSubtree = (subtree ?? []) as ParsedBlock[]
        let normalized = safeSubtree
        if (safeSubtree.length > 0) {
          const depths = safeSubtree
            .map((b) => b.depth)
            .filter((d): d is number => typeof d === 'number')
          const minDepth = depths.length ? Math.min(...depths) : 0
          if (minDepth > 0) {
            normalized = safeSubtree.map((b) => ({
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
    } catch (e) {
      loadError = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

  // Hydrate the full TaskDetail so the metadata sidebar has every field.
  // A fetch failure (missing binding in test, deleted block) is non-fatal —
  // the sidebar simply doesn't render and the editor works standalone.
  async function loadTaskDetail() {
    try {
      task = await fetchTaskDetail(ctx, blockId)
    } catch {
      task = null
    }
  }

  function scheduleSave() {
    if (saveTimer) clearTimeout(saveTimer)
    saveTimer = setTimeout(() => {
      void persist()
    }, 600)
  }

  async function persist() {
    if (!editorInstance || editorInstance.isDestroyed) return
    if (saving) {
      saveRequested = true
      pendingSnapshot = docToBlocks(editorInstance.getJSON())
      return
    }
    const edited = docToBlocks(editorInstance.getJSON())
    saving = true
    inFlight = (async () => {
      try {
        await ctx.saveSubtreeBlocks(blockId, edited)
        unsavedChanges = false
        saveError = ''
      } catch (e) {
        saveError = e instanceof Error ? e.message : String(e)
      } finally {
        saving = false
        if (saveRequested && pendingSnapshot) {
          const next = pendingSnapshot
          saveRequested = false
          pendingSnapshot = null
          saving = true
          inFlight = (async () => {
            try {
              await ctx.saveSubtreeBlocks(blockId, next)
              unsavedChanges = false
              saveError = ''
            } catch (e) {
              saveError = e instanceof Error ? e.message : String(e)
            } finally {
              saving = false
            }
          })()
        }
      }
    })()
    await inFlight
  }

  async function drainSave(): Promise<void> {
    if (saveTimer) {
      clearTimeout(saveTimer)
      saveTimer = null
    }
    if (unsavedChanges) {
      void persist()
    }
    await inFlight
    while (saveRequested || saving) {
      await inFlight
    }
  }

  async function attemptClose() {
    if (unsavedChanges || saving) {
      await drainSave()
      unsavedChanges = false
    }
    onClose()
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
      return
    }
    if (e.key === 'Tab' && dialogRef) {
      const els = focusableElements()
      if (els.length === 0) return
      const first = els[0]
      const last = els[els.length - 1]
      const active = document.activeElement as HTMLElement | null
      if (e.shiftKey) {
        if (active === first || !dialogRef.contains(active)) {
          e.preventDefault()
          last.focus()
        }
      } else {
        if (active === last) {
          e.preventDefault()
          first.focus()
        }
      }
    }
  }

  // --- Responsive sidebar (#780) ---
  // On narrow viewports the sidebar collapses into a disclosure so the editor
  // keeps space. Default open on wide viewports.
  let isNarrow = $state(false)
  let sidebarOpen = $state(true)

  $effect(() => {
    const mq = window.matchMedia('(max-width: 768px)')
    const sync = () => {
      isNarrow = mq.matches
      if (mq.matches) sidebarOpen = false
      else sidebarOpen = true
    }
    sync()
    mq.addEventListener('change', sync)
    return () => mq.removeEventListener('change', sync)
  })

  // Header title prefers the fetched task's clean_content; parentTaskText is
  // the optimistic fallback before the fetch resolves.
  let headerTitle = $derived(task?.clean_content ?? parentTaskText)

  onMount(() => {
    previouslyFocused = document.activeElement as HTMLElement
    void loadTaskDetail()
    return () => {
      void drainSave()
      previouslyFocused?.focus?.()
    }
  })

  onDestroy(() => {
    if (editorInstance && !editorInstance.isDestroyed) {
      editorInstance.destroy()
    }
  })

  let statusText = $derived(
    saving
      ? 'Saving…'
      : saveError
        ? `Save failed: ${saveError}`
        : unsavedChanges
          ? 'Unsaved edits'
          : loadError
            ? `Load failed: ${loadError}`
            : 'Saved'
  )
</script>

<svelte:window onkeydown={handleKeydown} />

<div
  class="fixed inset-0 z-[180] flex items-center justify-center p-6 bg-black/40 backdrop-blur-[2px]"
  transition:fly={{ y: -12, duration: 150 }}
>
  <!-- Backdrop -->
  <button
    tabindex="-1"
    aria-label="Close sub-editor"
    class="absolute inset-0 cursor-default border-none bg-transparent p-0"
    onclick={attemptClose}
  ></button>
  <div
    bind:this={dialogRef}
    role="dialog"
    aria-modal="true"
    aria-labelledby="sub-editor-title"
    tabindex="-1"
    class="relative z-10 w-full {isNarrow
      ? 'max-w-3xl'
      : 'max-w-5xl'} h-[80vh] rounded-xl border border-border-active shadow-2xl flex flex-col overflow-hidden"
    style="backdrop-filter: blur(16px) saturate(140%); background: color-mix(in srgb, var(--color-surface-modal) 94%, transparent);"
  >
    <!-- Header: breadcrumbs + parent task title + status -->
    <header
      class="flex items-center gap-3 px-5 py-3 border-b border-surface-modal-border flex-shrink-0"
    >
      <span
        class="material-symbols-outlined text-accent-primary-start text-type-2xl"
        aria-hidden="true">zoom_in</span
      >
      <div class="min-w-0 flex-1">
        <div
          class="text-type-2xs text-text-muted uppercase tracking-widest font-label-sm-bold truncate"
        >
          {#if isStandalone}
            Standalone task
          {:else}
            {notebook}<span
              class="material-symbols-outlined text-type-2xs align-middle"
              aria-hidden="true">chevron_right</span
            >{section || '(none)'}<span
              class="material-symbols-outlined text-type-2xs align-middle"
              aria-hidden="true">chevron_right</span
            >{page}
          {/if}
        </div>
        <h2
          id="sub-editor-title"
          class="text-text-primary font-label-md text-base truncate"
        >
          {headerTitle}
        </h2>
      </div>
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
            class="flex items-center justify-between px-5 py-2 border-b border-surface-modal-border text-type-sm font-label-sm-bold text-text-primary hover:bg-hover transition-colors flex-shrink-0"
            aria-expanded={sidebarOpen}
            aria-controls="sub-editor-sidebar"
            onclick={() => (sidebarOpen = !sidebarOpen)}
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
            <div class="text-status-danger text-center py-10 font-body-md">
              Load failed: {loadError}
            </div>
          {/if}
          {#if $editorStore}
            <EditorContent editor={$editorStore} />
          {/if}
        </div>

        <!-- Status footer -->
        <footer
          class="flex items-center justify-between px-5 py-2 border-t border-surface-modal-border flex-shrink-0"
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
          <span class="text-type-2xs text-text-muted font-label-sm">
            Esc to close
          </span>
        </footer>
      </div>

      <!-- Sidebar: metadata (#780) -->
      {#if task && !isNarrow}
        <aside
          class="w-80 flex-shrink-0 border-l border-surface-modal-border overflow-y-auto custom-scrollbar px-4 py-4"
        >
          <TaskMetadataSidebar
            {task}
            {ctx}
            {onMetaChanged}
            bind:busy={sidebarBusy}
          />
        </aside>
      {:else if task && isNarrow && sidebarOpen}
        <aside
          id="sub-editor-sidebar"
          class="flex-shrink-0 border-t border-surface-modal-border overflow-y-auto custom-scrollbar px-4 py-4 max-h-[40vh]"
        >
          <TaskMetadataSidebar
            {task}
            {ctx}
            {onMetaChanged}
            bind:busy={sidebarBusy}
          />
        </aside>
      {/if}
    </div>
  </div>
</div>
