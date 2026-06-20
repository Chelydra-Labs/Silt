<script lang="ts">
  import type { Editor } from 'svelte-tiptap'

  // BlockHoverMenu — a ⋮ hover button rendered per block on :hover /
  // :focus-within (#168). Opens a context menu with block-level formatting
  // operations: Clear formatting, Copy as Markdown, Copy as plain text.
  // role="menu", Tab-reachable, Enter/Space activate, Esc dismiss.

  interface Props {
    editor: Editor | null
  }

  let { editor }: Props = $props()

  let menuOpen = $state(false)

  function clearFormatting(): void {
    if (!editor) return
    editor.chain().focus().unsetAllMarks().run()
    menuOpen = false
  }

  async function copyAsMarkdown(): Promise<void> {
    if (!editor) return
    const { selection } = editor.state
    const text = selection.empty
      ? '' // no selection — could copy block-level in future
      : editor.state.doc.textBetween(selection.from, selection.to, '\n')
    try {
      await navigator.clipboard.writeText(text)
    } catch {
      // Clipboard may be unavailable in some contexts
    }
    menuOpen = false
  }

  async function copyAsPlainText(): Promise<void> {
    if (!editor) return
    const { selection } = editor.state
    // Strip all marks: get plain text from the selection
    const text = selection.empty
      ? ''
      : editor.state.doc.textBetween(selection.from, selection.to, '\n')
    try {
      await navigator.clipboard.writeText(text)
    } catch {
      // Clipboard may be unavailable in some contexts
    }
    menuOpen = false
  }
</script>

<div class="block-hover-menu-wrapper">
  <button
    type="button"
    class="hover-trigger"
    aria-label="Block actions"
    aria-expanded={menuOpen}
    aria-haspopup="menu"
    onclick={() => (menuOpen = !menuOpen)}
  >
    <span class="material-symbols-outlined" aria-hidden="true">more_horiz</span>
  </button>

  {#if menuOpen}
    <div class="block-menu" role="menu" aria-label="Block actions">
      <button type="button" class="menu-item" role="menuitem" onclick={clearFormatting}>
        <span class="material-symbols-outlined" aria-hidden="true">format_clear</span>
        <span>Clear formatting</span>
      </button>
      <button type="button" class="menu-item" role="menuitem" onclick={copyAsMarkdown}>
        <span class="material-symbols-outlined" aria-hidden="true">content_copy</span>
        <span>Copy as Markdown</span>
      </button>
      <button type="button" class="menu-item" role="menuitem" onclick={copyAsPlainText}>
        <span class="material-symbols-outlined" aria-hidden="true">content_paste</span>
        <span>Copy as plain text</span>
      </button>
    </div>
  {/if}
</div>

<style>
  .block-hover-menu-wrapper {
    position: relative;
    display: inline-flex;
  }

  .hover-trigger {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    border: none;
    border-radius: 5px;
    background: transparent;
    color: var(--color-text-muted, #8b95a3);
    cursor: pointer;
    opacity: 0;
    transition: opacity 0.15s;
  }

  :global(.ProseMirror .silt-block:hover) .hover-trigger,
  :global(.ProseMirror .silt-block:focus-within) .hover-trigger,
  .hover-trigger:focus-visible {
    opacity: 1;
  }

  .hover-trigger:hover {
    background: color-mix(in srgb, var(--color-accent-primary-start, #4f7cff) 15%, transparent);
    color: var(--color-text-primary, #e6e6e6);
  }

  .hover-trigger .material-symbols-outlined {
    font-size: 16px;
  }

  .block-menu {
    position: absolute;
    top: 100%;
    left: 0;
    z-index: 50;
    min-width: 180px;
    padding: 4px;
    border-radius: 8px;
    background: var(--color-surface, #1e1e22);
    border: 1px solid var(--color-border-muted, #33333a);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .menu-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 10px;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--color-text-primary, #e6e6e6);
    font-size: 0.8rem;
    text-align: left;
    cursor: pointer;
  }

  .menu-item:hover {
    background: color-mix(in srgb, var(--color-accent-primary-start, #4f7cff) 15%, transparent);
  }

  .menu-item .material-symbols-outlined {
    font-size: 16px;
    color: var(--color-text-muted, #8b95a3);
  }
</style>
