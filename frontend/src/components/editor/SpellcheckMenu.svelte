<script lang="ts">
  import type { Editor } from '@tiptap/core'
  import { tick } from 'svelte'
  import {
    suggest,
    ignoreWordSession
  } from '../../lib/editor/spellcheck/dictionary'
  import { requestSpellcheckRecheck } from '../../lib/editor/spellcheck/SpellcheckExtension'
  import { customDictionary } from '../../lib/editor/spellcheck/customDictionary.svelte'

  let {
    editor,
    word,
    range,
    anchor,
    onClose
  }: {
    editor: Editor
    word: string
    range: { from: number; to: number }
    anchor: { x: number; y: number }
    onClose: () => void
  } = $props()

  const suggestions = $derived(suggest(word))

  function enabledItems(): HTMLButtonElement[] {
    if (!menuEl) return []
    return Array.from(
      menuEl.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')
    )
  }

  function closeAndRestoreFocus(): void {
    onClose()
    editor.commands.focus()
  }

  function apply(suggestion: string): void {
    // Replace the misspelled word with the chosen suggestion in one tx.
    editor
      .chain()
      .focus()
      .deleteRange(range)
      .insertContentAt(range.from, { type: 'text', text: suggestion })
      .run()
    closeAndRestoreFocus()
  }

  async function addToDictionary(): Promise<void> {
    await customDictionary.add(word)
    // The config:changed event refreshes the editor's $effect (which calls
    // setCustomWords + recheck), so the word un-flags immediately. No reload.
    closeAndRestoreFocus()
  }

  function ignore(): void {
    ignoreWordSession(word)
    requestSpellcheckRecheck(editor)
    closeAndRestoreFocus()
  }

  function handleKeydown(e: KeyboardEvent): void {
    const actions = enabledItems()
    if (actions.length === 0) return

    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault()
      const current = actions.indexOf(
        document.activeElement as HTMLButtonElement
      )
      const direction = e.key === 'ArrowDown' ? 1 : -1
      const next =
        current === -1
          ? direction === 1
            ? 0
            : actions.length - 1
          : (current + direction + actions.length) % actions.length
      actions[next]?.focus()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      closeAndRestoreFocus()
    }
  }

  let menuEl = $state<HTMLDivElement | null>(null)
  // Clamp the menu to the viewport so right-clicks near the screen edge don't
  // push it off-screen. The $effect reads `anchor` reactively + measures the
  // element after render to compute the clamped position.
  let clampedAnchor = $state({ x: -9999, y: -9999 })

  $effect(() => {
    void suggestions
    if (!menuEl) return
    void tick().then(() => enabledItems()[0]?.focus())
  })

  $effect(() => {
    if (!menuEl) return
    const { x: ax, y: ay } = anchor
    const rect = menuEl.getBoundingClientRect()
    let x = ax
    let y = ay
    if (x + rect.width > window.innerWidth) {
      x = window.innerWidth - rect.width - 8
    }
    if (y + rect.height > window.innerHeight) {
      y = window.innerHeight - rect.height - 8
    }
    clampedAnchor = { x: Math.max(8, x), y: Math.max(8, y) }
  })
</script>

<svelte:window onkeydown={handleKeydown} />

<!-- Full-viewport layer so click/right-click outside the card dismisses
     (same pattern as ContextMenu). -->
<div class="spell-menu-layer">
  <button
    type="button"
    tabindex="-1"
    aria-label="Close spelling suggestions"
    class="spell-menu-backdrop"
    onclick={closeAndRestoreFocus}
    oncontextmenu={(e) => {
      e.preventDefault()
      closeAndRestoreFocus()
    }}
  ></button>
  <div
    bind:this={menuEl}
    class="spell-menu"
    role="menu"
    aria-label="Spelling suggestions"
    style="left:{Math.round(clampedAnchor.x)}px; top:{Math.round(
      clampedAnchor.y
    )}px;"
    tabindex="-1"
  >
    {#if suggestions.length === 0}
      <button type="button" class="menu-item disabled" role="menuitem" disabled
        >No suggestions</button
      >
    {:else}
      {#each suggestions as s, i (s + '-' + i)}
        <button
          type="button"
          class="menu-item"
          role="menuitem"
          aria-label="Replace with {s}"
          onclick={() => apply(s)}>{s}</button
        >
      {/each}
    {/if}
    <div class="menu-separator"></div>
    <button
      type="button"
      class="menu-item"
      role="menuitem"
      onclick={addToDictionary}>Add to dictionary</button
    >
    <button type="button" class="menu-item" role="menuitem" onclick={ignore}
      >Ignore</button
    >
  </div>
</div>

<style>
  .spell-menu-layer {
    position: fixed;
    inset: 0;
    z-index: 100;
  }
  .spell-menu-backdrop {
    position: absolute;
    inset: 0;
    margin: 0;
    padding: 0;
    border: none;
    background: transparent;
    cursor: default;
  }
  .spell-menu {
    position: fixed;
    z-index: 1;
    min-width: 180px;
    padding: 4px;
    background: var(--color-surface-popover);
    border: 1px solid var(--color-surface-popover-border);
    border-radius: 8px;
    box-shadow: var(--shadow-md, 0 8px 24px rgba(0, 0, 0, 0.45));
    font-size: 13px;
  }
  .menu-item {
    display: block;
    width: 100%;
    text-align: left;
    padding: 0.375rem 0.625rem;
    background: transparent;
    color: var(--color-text-primary);
    border: none;
    border-radius: 4px;
    cursor: pointer;
    font: inherit;
  }
  .menu-item:hover:not(.disabled),
  .menu-item:focus-visible {
    background: var(--color-hover);
    outline: none;
  }
  .menu-item.disabled {
    color: var(--color-text-muted);
    cursor: default;
  }
  .menu-separator {
    height: 1px;
    margin: 4px 0;
    background: var(--color-surface-popover-border);
  }
</style>
