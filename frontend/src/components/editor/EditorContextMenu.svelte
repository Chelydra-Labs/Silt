<script lang="ts">
  // EditorContextMenu — the editor's right-click menu (Cut/Copy/Paste, copy-as,
  // block-scoped actions, Clear Formatting, dev Inspect). Extracted from
  // TipTapEditor.svelte unchanged in behaviour: the host still owns the opener
  // (`handleContextMenu` on `.tiptap-editor-host`, which resolves the click
  // position + active block and gates this component's render); this component
  // owns everything about an OPEN menu — Escape/backdrop dismissal, first-item
  // focus, ArrowUp/Down/Home/End keyboard cycling, and the clipboard action
  // handlers (thin wrappers over lib/editor/clipboard).
  import type { Snippet } from 'svelte'
  import type { Editor } from 'svelte-tiptap'
  import type { Node as ProseMirrorNode } from '@tiptap/pm/model'
  import { isDevMode, openInspect } from '../../lib/devModeInspect'
  import { pushNotification } from '../../notifications/store.svelte'
  import {
    cutSelection,
    copySelection,
    pasteFromClipboard,
    copyAsMarkdown,
    copyAsPlainText,
    copyBlockReference,
    copyBlockEmbed,
    duplicateBlock,
    deleteBlock
  } from '../../lib/editor/clipboard'
  import { portal } from '../../lib/portal'

  /** Resolved menu payload (position + active block) the host passes in. */
  export interface EditorContextMenuPayload {
    x: number
    y: number
    activeBlockId?: string
    activeBlockNode?: ProseMirrorNode
  }

  interface Props {
    /** The click-resolved payload. Always present — the host gates render. */
    menu: EditorContextMenuPayload
    editor: Editor
    /** True when the selection is collapsed — disables Cut/Copy. */
    selectionEmpty: boolean
    /** True when only one block remains — disables Delete Block. */
    isLastBlock: boolean
    /** Fired after any dismiss/close so the host can clear its open state. */
    onClose: () => void
    /** Optional leading slot for items injected before the block-scoped group. */
    children?: Snippet
  }

  let { menu, editor, selectionEmpty, isLastBlock, onClose, children }: Props =
    $props()

  let devModeEnabled = $derived(isDevMode())
  let inSubEditor = $derived(
    (editor.storage as unknown as Record<string, { active?: boolean }>)
      .siltSubEditorHost?.active === true
  )
  let menuEl = $state<HTMLDivElement | null>(null)

  // Close the menu: focus the editor first (while still mounted) then signal
  // the host to drop its open state. Matches the original closeContextMenu.
  function close(): void {
    if (!editor.isDestroyed) editor.commands.focus()
    onClose()
  }

  function clipboardDeps() {
    return {
      editor,
      notify: pushNotification,
      menu: () => menu
    }
  }

  function handleCut(): void {
    cutSelection(clipboardDeps())
    close()
  }

  function handleCopy(): void {
    copySelection(clipboardDeps())
    close()
  }

  async function handlePaste(): Promise<void> {
    await pasteFromClipboard(clipboardDeps())
    close()
  }

  async function handleCopyAsMarkdown(): Promise<void> {
    await copyAsMarkdown(clipboardDeps())
    close()
  }

  async function handleCopyAsPlainText(): Promise<void> {
    await copyAsPlainText(clipboardDeps())
    close()
  }

  async function handleCopyBlockReference(): Promise<void> {
    await copyBlockReference(clipboardDeps())
    close()
  }

  async function handleCopyBlockEmbed(): Promise<void> {
    await copyBlockEmbed(clipboardDeps())
    close()
  }

  function handleDuplicateBlock(): void {
    duplicateBlock(clipboardDeps())
    close()
  }

  function handleDeleteBlock(): void {
    deleteBlock(clipboardDeps())
    close()
  }

  /** Edit task in modal (#781): dispatches the silt:open-task-editor window
   *  event consumed by TaskEditorModalHost. Only shown for taskBlock targets. */
  function handleEditTaskInModal(): void {
    if (menu.activeBlockId) {
      window.dispatchEvent(
        new CustomEvent('silt:open-task-editor', {
          detail: { blockId: menu.activeBlockId }
        })
      )
    }
    close()
  }

  function handleClearFormatting(): void {
    editor.chain().focus().unsetAllMarks().run()
    close()
  }

  /** Dev Mode Inspect (#679/#683) — opens webview DevTools when the flag is on. */
  async function handleInspect(): Promise<void> {
    close()
    await openInspect()
  }

  // Escape dismisses the menu (capture-phase so it wins over editor keymaps).
  $effect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        e.stopPropagation()
        close()
      }
    }
    window.addEventListener('keydown', handleKeyDown, true)
    return () => {
      window.removeEventListener('keydown', handleKeyDown, true)
    }
  })

  // Focus the first enabled item once the card paints, so keyboard nav starts
  // immediately and screen readers announce focus correctly.
  $effect(() => {
    const el = menuEl
    if (!el) return
    const id = requestAnimationFrame(() => {
      const first = menuEl?.querySelector<HTMLButtonElement>(
        'button:not([disabled])'
      )
      first?.focus()
    })
    return () => cancelAnimationFrame(id)
  })

  // ArrowUp/Down cycling + Home/End jumps across enabled menu buttons.
  function handleMenuKeyDown(e: KeyboardEvent): void {
    const el = menuEl
    if (!el) return
    const items = Array.from(
      el.querySelectorAll<HTMLButtonElement>('button:not([disabled])')
    )
    if (items.length === 0) return
    const currentIndex = items.findIndex(
      (item) => item === document.activeElement
    )
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        items[(currentIndex + 1) % items.length]?.focus()
        break
      case 'ArrowUp':
        e.preventDefault()
        items[(currentIndex - 1 + items.length) % items.length]?.focus()
        break
      case 'Home':
        e.preventDefault()
        items[0]?.focus()
        break
      case 'End':
        e.preventDefault()
        items[items.length - 1]?.focus()
        break
    }
  }
</script>

<!-- Portaled to body so position:fixed uses viewport coords under note zoom. -->
<div class="fixed inset-0 z-[180]" use:portal>
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div
    class="absolute inset-0 cursor-default"
    onclick={close}
    oncontextmenu={(e) => {
      e.preventDefault()
      e.stopPropagation()
      close()
    }}
  ></div>
  <div
    bind:this={menuEl}
    class="fixed context-menu-card"
    style="left: {menu.x}px; top: {menu.y}px"
    role="menu"
    tabindex="-1"
    aria-label="Editor actions"
    oncontextmenu={(e) => e.preventDefault()}
    onkeydown={handleMenuKeyDown}
  >
    {@render children?.()}
    <button
      type="button"
      class="context-menu-item justify-between"
      role="menuitem"
      onclick={handleCut}
      disabled={selectionEmpty}
    >
      <span class="flex items-center gap-2">
        <span class="material-symbols-outlined text-icon-md">content_cut</span>
        Cut
      </span>
      <span class="ml-auto text-type-2xs text-text-muted/70 font-mono pl-4"
        >Ctrl+X</span
      >
    </button>
    <button
      type="button"
      class="context-menu-item justify-between"
      role="menuitem"
      onclick={handleCopy}
      disabled={selectionEmpty}
    >
      <span class="flex items-center gap-2">
        <span class="material-symbols-outlined text-icon-md">content_copy</span>
        Copy
      </span>
      <span class="ml-auto text-type-2xs text-text-muted/70 font-mono pl-4"
        >Ctrl+C</span
      >
    </button>
    <button
      type="button"
      class="context-menu-item justify-between"
      role="menuitem"
      onclick={handlePaste}
    >
      <span class="flex items-center gap-2">
        <span class="material-symbols-outlined text-icon-md">content_paste</span
        >
        Paste
      </span>
      <span class="ml-auto text-type-2xs text-text-muted/70 font-mono pl-4"
        >Ctrl+V</span
      >
    </button>

    <div class="context-menu-separator"></div>

    <button
      type="button"
      class="context-menu-item"
      role="menuitem"
      onclick={handleCopyAsMarkdown}
    >
      <span class="material-symbols-outlined text-icon-md">markdown</span>
      Copy as Markdown
    </button>
    <button
      type="button"
      class="context-menu-item"
      role="menuitem"
      onclick={handleCopyAsPlainText}
    >
      <span class="material-symbols-outlined text-icon-md">notes</span>
      Copy as Plain Text
    </button>

    {#if menu.activeBlockId}
      <div class="context-menu-separator"></div>
      <button
        type="button"
        class="context-menu-item"
        role="menuitem"
        onclick={handleCopyBlockReference}
      >
        <span class="material-symbols-outlined text-icon-md">link</span>
        Copy Block Reference
      </button>
      <button
        type="button"
        class="context-menu-item"
        role="menuitem"
        onclick={handleCopyBlockEmbed}
      >
        <span class="material-symbols-outlined text-icon-md"
          >integration_instructions</span
        >
        Copy Block Embed
      </button>

      {#if menu.activeBlockNode?.type.name === 'taskBlock' && !inSubEditor}
        <button
          type="button"
          class="context-menu-item"
          role="menuitem"
          onclick={handleEditTaskInModal}
        >
          <span class="material-symbols-outlined text-icon-md">edit_note</span>
          Edit task in modal…
        </button>
      {/if}

      <div class="context-menu-separator"></div>
      <button
        type="button"
        class="context-menu-item"
        role="menuitem"
        onclick={handleDuplicateBlock}
      >
        <span class="material-symbols-outlined text-icon-md">difference</span>
        Duplicate Block
      </button>
      <button
        type="button"
        class="context-menu-item text-status-danger"
        role="menuitem"
        onclick={handleDeleteBlock}
        disabled={isLastBlock}
      >
        <span class="material-symbols-outlined text-icon-md">delete</span>
        Delete Block
      </button>
    {/if}

    <div class="context-menu-separator"></div>
    <button
      type="button"
      class="context-menu-item"
      role="menuitem"
      onclick={handleClearFormatting}
    >
      <span class="material-symbols-outlined text-icon-md">format_clear</span>
      Clear Formatting
    </button>

    {#if devModeEnabled}
      <div class="context-menu-separator"></div>
      <button
        type="button"
        class="context-menu-item"
        role="menuitem"
        onclick={handleInspect}
      >
        <span class="material-symbols-outlined text-icon-md">bug_report</span>
        Inspect
      </button>
    {/if}
  </div>
</div>

<style>
  .context-menu-card {
    background-color: color-mix(
      in srgb,
      var(--color-surface-popover) 90%,
      transparent
    );
    backdrop-filter: blur(12px) saturate(140%);
    border: 1px solid var(--color-surface-popover-border);
    border-radius: 8px;
    box-shadow: 0 10px 25px -5px rgba(0, 0, 0, 0.5);
    padding: 4px;
    min-width: 180px;
    z-index: 181;
  }

  .context-menu-item {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 6px 12px;
    border: none;
    background: transparent;
    color: var(--color-text-primary);
    font-size: 12px;
    font-family: var(--font-body, inherit);
    text-align: left;
    cursor: pointer;
    border-radius: 6px;
    transition: background-color 120ms ease-out;
  }

  .context-menu-item:hover {
    background-color: var(--color-hover);
  }

  .context-menu-item:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .context-menu-item.text-status-danger {
    color: var(--color-status-danger);
  }

  .context-menu-item.text-status-danger .material-symbols-outlined {
    color: var(--color-status-danger);
  }

  .context-menu-item:hover.text-status-danger {
    background-color: color-mix(
      in srgb,
      var(--color-status-danger) 15%,
      transparent
    );
  }

  .context-menu-separator {
    height: 1px;
    background: var(--color-surface-popover-border);
    margin: 4px;
  }
</style>
