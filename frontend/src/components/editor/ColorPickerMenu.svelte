<script lang="ts">
  import type { Editor } from 'svelte-tiptap'
  import {
    deriveColorPalette,
    readActiveThemeColorTokens,
    resolveColor,
    FALLBACK_COLOR_PALETTE,
    type ColorEntry
  } from '../../lib/editor/colors'

  interface Props {
    editor: Editor | null
    markType: 'textColor' | 'backgroundColor'
    isDark: boolean
    /** Roving tabindex from the parent format toolbar (#690). */
    toolbarTabIndex?: number
    onToolbarFocus?: () => void
  }

  let {
    editor,
    markType,
    isDark,
    toolbarTabIndex = 0,
    onToolbarFocus
  }: Props = $props()

  let menuOpen = $state(false)
  let wrapperEl = $state<HTMLDivElement | null>(null)
  let triggerEl = $state<HTMLButtonElement | null>(null)

  $effect(() => {
    if (!menuOpen) return
    const onClick = (e: MouseEvent) => {
      if (wrapperEl && !wrapperEl.contains(e.target as Node)) {
        menuOpen = false
      }
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        e.stopPropagation()
        menuOpen = false
        triggerEl?.focus()
      }
    }
    document.addEventListener('click', onClick)
    document.addEventListener('keydown', onKey, true)
    return () => {
      document.removeEventListener('click', onClick)
      document.removeEventListener('keydown', onKey, true)
    }
  })

  function applyColor(entry: ColorEntry): void {
    if (!editor) return
    const hex = resolveColor(entry, isDark)
    editor.chain().focus().setMark(markType, { color: hex }).run()
    menuOpen = false
  }

  function applyCustom(event: Event): void {
    const input = event.target as HTMLInputElement
    if (input.value && editor) {
      editor.chain().focus().setMark(markType, { color: input.value }).run()
    }
    menuOpen = false
  }

  function removeColor(): void {
    if (!editor) return
    editor.chain().focus().unsetMark(markType).run()
    menuOpen = false
  }

  const triggerIcon = $derived(
    markType === 'textColor' ? 'format_color_text' : 'format_color_fill'
  )
  const triggerLabel = $derived(
    markType === 'textColor' ? 'Text color' : 'Background color'
  )

  // Theme-derived palette (#408): re-read the active theme's color anchors
  // from :root each time the menu opens so the swatch row tracks the theme.
  // Falls back to the fixed set only if the theme tokens are unavailable.
  const palette = $derived.by(() => {
    void menuOpen
    const tokens = readActiveThemeColorTokens()
    return Object.keys(tokens).length > 0
      ? deriveColorPalette(tokens)
      : FALLBACK_COLOR_PALETTE
  })
</script>

<div class="color-picker-wrapper" bind:this={wrapperEl}>
  <button
    type="button"
    class="color-trigger"
    bind:this={triggerEl}
    aria-expanded={menuOpen}
    aria-haspopup="menu"
    aria-label={triggerLabel}
    data-tb
    tabindex={toolbarTabIndex}
    onclick={() => (menuOpen = !menuOpen)}
    onfocus={() => onToolbarFocus?.()}
  >
    <span class="material-symbols-outlined" aria-hidden="true"
      >{triggerIcon}</span
    >
  </button>

  {#if menuOpen}
    <div class="color-menu" role="menu" aria-label={triggerLabel}>
      <button
        type="button"
        class="color-action"
        role="menuitem"
        onclick={removeColor}
      >
        <span class="material-symbols-outlined" aria-hidden="true"
          >format_color_reset</span
        >
        <span>No color</span>
      </button>
      <div class="swatch-grid" role="group" aria-label="Color palette">
        {#each palette as entry (entry.id)}
          <button
            type="button"
            class="swatch"
            style="background-color: {resolveColor(entry, isDark)}"
            aria-label={entry.label}
            role="menuitem"
            onclick={() => applyColor(entry)}
          >
          </button>
        {/each}
      </div>
      <label class="custom-color-row">
        <span class="custom-label">Custom</span>
        <input
          type="color"
          class="custom-input"
          onchange={applyCustom}
          aria-label="Custom color"
        />
      </label>
    </div>
  {/if}
</div>

<style>
  .color-picker-wrapper {
    position: relative;
    display: inline-flex;
  }

  .color-trigger {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--color-text-muted);
    cursor: pointer;
    transition:
      background 0.1s,
      color 0.1s;
  }

  .color-trigger:hover {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 15%,
      transparent
    );
    color: var(--color-text-primary);
  }

  .color-trigger .material-symbols-outlined {
    font-size: 18px;
  }

  .color-menu {
    position: absolute;
    top: 100%;
    left: 0;
    z-index: 50;
    min-width: 200px;
    padding: 6px;
    border-radius: 8px;
    background: var(--color-surface-popover);
    border: 1px solid var(--color-surface-popover-border);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .color-action {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 4px 8px;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--color-text-primary);
    font-size: 0.75rem;
    text-align: left;
    cursor: pointer;
  }

  .color-action:hover {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 15%,
      transparent
    );
  }

  .color-action .material-symbols-outlined {
    font-size: 16px;
    color: var(--color-text-muted);
  }

  .swatch-grid {
    display: grid;
    grid-template-columns: repeat(6, 1fr);
    gap: 3px;
    padding: 4px 0;
  }

  .swatch {
    width: 24px;
    height: 24px;
    border: 2px solid transparent;
    border-radius: 5px;
    cursor: pointer;
    padding: 0;
    transition:
      border-color 0.1s,
      transform 0.1s;
  }

  .swatch:hover {
    border-color: var(--color-text-primary);
    transform: scale(1.1);
  }

  .custom-color-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 4px 8px;
    font-size: 0.75rem;
    color: var(--color-text-muted);
  }

  .custom-input {
    width: 28px;
    height: 22px;
    border: 1px solid var(--color-surface-popover-border);
    border-radius: 4px;
    background: transparent;
    cursor: pointer;
    padding: 0;
  }

  @media (prefers-reduced-motion: reduce) {
    .swatch {
      transition: none;
    }
    .color-trigger {
      transition: none;
    }
  }
</style>
