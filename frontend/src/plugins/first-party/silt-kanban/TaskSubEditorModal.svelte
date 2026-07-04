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
  } from '../../../lib/editor'
  import type { ParsedBlock } from '../../../lib/editor'
  import type { PluginContext } from '../../sdk'

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

  // --- Editor setup ---
  let editorInstance: Editor | null = $state(null)
  let loading = $state(true)
  let loadError = $state('')
  let unsavedChanges = $state(false)
  let saveError = $state('')
  let saving = $state(false)
  let saveTimer: ReturnType<typeof setTimeout> | null = null

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
      editorInstance.commands.setContent(
        blocksToDoc(subtree as ParsedBlock[]),
        {
          emitUpdate: false
        }
      )
      suppressUpdate = false
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
    if (saving) return
    const edited = docToBlocks(editorInstance.getJSON()) as ParsedBlock[]
    saving = true
    try {
      await ctx.saveSubtreeBlocks(blockId, edited)
      unsavedChanges = false
      saveError = ''
    } catch (e) {
      saveError = e instanceof Error ? e.message : String(e)
    } finally {
      saving = false
    }
  }

  // --- Close with unsaved-edits guard (#306) ---
  function attemptClose() {
    if (unsavedChanges || saving) {
      // Flush the pending save first, then close. A debounced edit hasn't
      // landed yet; persisting now ensures no data loss on close.
      if (saveTimer) {
        clearTimeout(saveTimer)
        saveTimer = null
      }
      void persist().then(() => {
        unsavedChanges = false
        onClose()
      })
      return
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
      if (saveTimer) clearTimeout(saveTimer)
      // Flush any last edit before teardown so nothing is lost.
      if (unsavedChanges) void persist()
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
    style="backdrop-filter: blur(16px) saturate(140%); background: color-mix(in srgb, var(--color-panel) 94%, transparent);"
    transition:fly={{ y: -12, duration: 150 }}
  >
    <!-- Header: breadcrumbs + parent task title + status -->
    <header
      class="flex items-center gap-3 px-5 py-3 border-b border-border-muted flex-shrink-0"
    >
      <span
        class="material-symbols-outlined text-accent-primary-start text-[20px]"
        >zoom_in</span
      >
      <div class="min-w-0 flex-1">
        <div
          class="text-[10px] text-text-muted uppercase tracking-widest font-label-sm-bold truncate"
        >
          {notebook}<span
            class="material-symbols-outlined text-[10px] align-middle"
            >chevron_right</span
          >{section || '(none)'}<span
            class="material-symbols-outlined text-[10px] align-middle"
            >chevron_right</span
          >{page}
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
      class="flex items-center justify-between px-5 py-2 border-t border-border-muted flex-shrink-0"
    >
      <span
        class="text-[11px] font-label-sm {saveError
          ? 'text-status-danger'
          : unsavedChanges
            ? 'text-status-warn'
            : 'text-text-muted'}"
        role="status"
        aria-live="polite"
      >
        {statusText}
      </span>
      <span class="text-[10px] text-text-muted font-label-sm">
        Esc to close
      </span>
    </footer>
  </div>
</div>
