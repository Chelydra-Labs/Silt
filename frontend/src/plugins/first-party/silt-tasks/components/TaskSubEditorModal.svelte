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

  /**
   * Focused Task Sub-Editor Modal (#304). Double-clicking a Kanban task card
   * opens this glassy overlay hosting a scoped TipTap instance seeded with the
   * task's child sub-tree. On save (debounced), the edited sub-tree is spliced
   * back into the parent file atomically via saveSubtreeBlocks (#305).
   *
   * Mirrors the main editor's TipTap setup (createEditor + extension set) but
   * with a self-contained autosave targeting the subtree splice IPC, plus the
   * SettingsShell focus-trap + Esc-with-unsaved-prompt + focus-restore pattern.
   */
  interface Props {
    blockId: string
    notebook: string
    section: string
    page: string
    parentTaskText: string
    ctx: PluginContext
    onClose: () => void
  }

  let {
    blockId,
    notebook,
    section,
    page,
    parentTaskText,
    ctx,
    onClose
  }: Props = $props()

  // Standalone (.silt) tasks have no source page; show a friendly label
  // instead of the synthetic `.silt › (none) › tasks.md` path.
  let isStandalone = $derived(notebook === STANDALONE_TASKS_NOTEBOOK)

  // --- Editor setup ---
  let editorInstance: Editor | null = $state(null)
  let loading = $state(true)
  let loadError = $state('')
  let unsavedChanges = $state(false)
  let saveError = $state('')
  let saving = $state(false)
  let saveTimer: ReturnType<typeof setTimeout> | null = null
  // Re-queue signal: when persist() is called while a save is in flight, the
  // early-return sets this so the in-flight save re-runs once it resolves —
  // otherwise edits made during the IPC window would be silently dropped.
  let saveRequested = false
  // Snapshot of the doc captured at early-return time. The retry after an
  // in-flight save flushes this snapshot directly (bypassing the live editor)
  // so an unmount that destroys the editor before the save resolves can't
  // drop the edit — the doc is already serialized.
  let pendingSnapshot: ParsedBlock[] | null = null
  // The currently in-flight save promise, so close/teardown can await a full
  // drain (persist → any re-queued flush) before unmounting.
  let inFlight: Promise<void> = Promise.resolve()

  // Suppress the onUpdate handler during a programmatic setContent so it
  // doesn't register as user input (mirrors TipTapEditor's suppressUpdate).
  let suppressUpdate = false

  // --- Focus trap (mirrors SettingsShell.svelte:100-177) ---
  let dialogRef = $state<HTMLDivElement | null>(null)
  let previouslyFocused: HTMLElement | null = null
  const FOCUSABLE =
    'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

  function focusableElements(): HTMLElement[] {
    if (!dialogRef) return []
    return Array.from(dialogRef.querySelectorAll<HTMLElement>(FOCUSABLE))
  }

  // Build the editor with the same extension surface as the main editor, minus
  // the suggest/drag-handle/keymap features that assume the host page context.
  // The seed is fetched async, so createEditor starts empty and we setContent
  // once the subtree loads.
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

  // Capture the editor store once under untrack so the one-shot creation
  // doesn't establish a reactive dependency. The Editor instance is captured
  // from onCreate (the svelte-tiptap convention, mirroring TipTapEditor.svelte).
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
        // Load the subtree once the editor exists so setContent can run, then
        // focus so keyboard users land in the editor on open.
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
        // `fetchSubtree` can resolve to null when the task has no children
        // yet (a Go nil slice serializes to JSON null, not []). Treat that
        // as an empty subtree so the editor opens blank for the user to
        // add notes, instead of crashing on blocksToDoc(null).map.
        const safeSubtree = (subtree ?? []) as ParsedBlock[]
        editorInstance.commands.setContent(blocksToDoc(safeSubtree), {
          emitUpdate: false
        })
      } finally {
        // ensure suppressUpdate resets even if setContent throws, so a
        // later failed load can't permanently silence user-typing saves.
        suppressUpdate = false
      }
      unsavedChanges = false
    } catch (e) {
      loadError = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
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
    // A save is already in flight: don't drop this edit. Capture the doc
    // snapshot NOW so the in-flight save's finally can flush it directly,
    // without needing the live editor (which onDestroy may tear down before
    // the IPC resolves on the unmount-without-close path).
    if (saving) {
      saveRequested = true
      pendingSnapshot = docToBlocks(editorInstance.getJSON()) as ParsedBlock[]
      return
    }
    const edited = docToBlocks(editorInstance.getJSON()) as ParsedBlock[]
    saving = true
    // Track the in-flight save so drainSave (close/teardown) can await it.
    inFlight = (async () => {
      try {
        await ctx.saveSubtreeBlocks(blockId, edited)
        unsavedChanges = false
        saveError = ''
      } catch (e) {
        saveError = e instanceof Error ? e.message : String(e)
      } finally {
        saving = false
        // If an edit landed while this save was in flight, flush the captured
        // snapshot directly — bypassing the editor entirely so an unmount
        // that destroyed it can't drop the edit.
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

  // drainSave awaits the full save pipeline (the in-flight save plus any
  // edit-triggered re-queue) so close/teardown can't unmount before the
  // latest edits are persisted. Returns once no save is pending.
  async function drainSave(): Promise<void> {
    // Cancel any debounced save not yet fired so persist() runs immediately.
    if (saveTimer) {
      clearTimeout(saveTimer)
      saveTimer = null
    }
    if (unsavedChanges) {
      void persist()
    }
    await inFlight
    // If persist re-queued itself, chase the tail until the queue drains.
    while (saveRequested || saving) {
      await inFlight
    }
  }

  // --- Close with unsaved-edits guard (#306) ---
  async function attemptClose() {
    if (unsavedChanges || saving) {
      // Flush the full save pipeline before closing so no edit is dropped.
      await drainSave()
      unsavedChanges = false
    }
    onClose()
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      attemptClose()
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

  onMount(() => {
    previouslyFocused = document.activeElement as HTMLElement
    // Keydown is handled by <svelte:window onkeydown={handleKeydown}> in the
    // template (the SettingsShell pattern) — no addEventListener here, or
    // every Esc/Tab would fire the handler twice.
    return () => {
      // Flush any in-flight + pending save before teardown so an unmount
      // (navigation) can't drop edits. attemptClose is the user-facing path
      // and already drains; this is the safety net for unmount-without-close.
      // Fire-and-forget drainSave — Svelte's cleanup can't await, but the
      // IPC call still reaches the backend and persists.
      void drainSave()
      previouslyFocused?.focus?.()
    }
  })

  onDestroy(() => {
    // svelte-tiptap editors are destroyed on unmount automatically; this is a
    // belt-and-suspenders guard for HMR / rapid re-mount during tests.
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
  <!-- Backdrop: a sibling button so the click is keyboard/AT-reachable but
       excluded from the tab order. -->
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
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
    class="relative z-10 w-full max-w-3xl h-[80vh] rounded-xl border border-border-active shadow-2xl flex flex-col overflow-hidden"
    style="backdrop-filter: blur(16px) saturate(140%); background: color-mix(in srgb, var(--color-surface-modal) 94%, transparent);"
  >
    <!-- Header: breadcrumbs + parent task title + status -->
    <header
      class="flex items-center gap-3 px-5 py-3 border-b border-surface-modal-border flex-shrink-0"
    >
      <span
        class="material-symbols-outlined text-accent-primary-start text-[20px]"
        >zoom_in</span
      >
      <div class="min-w-0 flex-1">
        <div
          class="text-[10px] text-text-muted uppercase tracking-widest font-label-sm-bold truncate"
        >
          {#if isStandalone}
            Standalone task
          {:else}
            {notebook}<span
              class="material-symbols-outlined text-[10px] align-middle"
              >chevron_right</span
            >{section || '(none)'}<span
              class="material-symbols-outlined text-[10px] align-middle"
              >chevron_right</span
            >{page}
          {/if}
        </div>
        <h2
          id="sub-editor-title"
          class="text-text-primary font-label-md text-base truncate"
        >
          {parentTaskText}
        </h2>
      </div>
      <button
        type="button"
        onclick={attemptClose}
        class="text-text-muted hover:text-text-primary transition-colors p-1 rounded"
        aria-label="Close sub-editor"
      >
        <span class="material-symbols-outlined text-[20px]">close</span>
      </button>
    </header>

    <!-- Editor body -->
    <div class="flex-1 overflow-y-auto custom-scrollbar px-5 py-4 min-h-0">
      {#if loading}
        <div class="text-text-muted text-center py-10 font-body-md">
          Loading sub-notes…
        </div>
      {:else}
        <EditorContent editor={$editorStore} />
      {/if}
    </div>

    <!-- Status footer -->
    <footer
      class="flex items-center justify-between px-5 py-2 border-t border-surface-modal-border flex-shrink-0"
    >
      <span
        class="text-[11px] font-label-sm {saveError
          ? 'text-status-danger'
          : unsavedChanges
            ? 'text-status-warn'
            : 'text-text-muted'}"
        role={saveError ? 'alert' : 'status'}
        aria-live={saveError ? 'assertive' : 'polite'}
      >
        {statusText}
      </span>
      <span class="text-[10px] text-text-muted font-label-sm">
        Esc to close
      </span>
    </footer>
  </div>
</div>
