<script lang="ts">
  import { tick } from 'svelte'
  import { NodeViewWrapper, NodeViewContent } from 'svelte-tiptap'
  import type { NodeViewProps } from '@tiptap/core'
  import {
    CALLOUT_VARIANTS,
    type CalloutVariant
  } from '../../lib/editor/schema'

  // Obsidian-style callout / admonition (#180). Variant chrome opens an
  // in-place picker (#658) so users can change type without re-inserting.
  let { node, editor, updateAttributes }: NodeViewProps = $props()
  let variant = $derived((node.attrs.variant as CalloutVariant) || 'note')
  let cfg = $derived(CALLOUT_VARIANTS[variant] ?? CALLOUT_VARIANTS.note)

  const VARIANTS = Object.entries(CALLOUT_VARIANTS) as Array<
    [CalloutVariant, (typeof CALLOUT_VARIANTS)[CalloutVariant]]
  >

  let menuOpen = $state(false)
  let wrapperEl = $state<HTMLDivElement | null>(null)
  let triggerEl = $state<HTMLButtonElement | null>(null)
  let menuEl = $state<HTMLDivElement | null>(null)
  let activeIndex = $state(0)
  let menuDomId = $derived(
    `callout-variant-menu-${(node.attrs.id as string) || 'pending'}`
  )

  function focusActiveMenuitem() {
    if (!menuEl) return
    const items = menuEl.querySelectorAll<HTMLButtonElement>(
      '[role=menuitemradio]'
    )
    const btn = items[activeIndex]
    if (btn) btn.focus()
    else menuEl.focus()
  }

  $effect(() => {
    if (!menuOpen) return
    const onClick = (e: MouseEvent) => {
      if (wrapperEl && !wrapperEl.contains(e.target as Node)) {
        menuOpen = false
      }
    }
    document.addEventListener('click', onClick)
    return () => document.removeEventListener('click', onClick)
  })

  // Keep keyboard focus on the active menuitem while open.
  $effect(() => {
    if (!menuOpen) return
    void activeIndex
    tick().then(() => focusActiveMenuitem())
  })

  function openMenu() {
    activeIndex = Math.max(
      0,
      VARIANTS.findIndex(([key]) => key === variant)
    )
    menuOpen = true
    tick().then(() => focusActiveMenuitem())
  }

  function selectVariant(next: CalloutVariant) {
    if (next === variant) {
      menuOpen = false
      return
    }
    // Prefer updateAttributes from the NodeView host; fall back to commands.
    if (typeof updateAttributes === 'function') {
      updateAttributes({ variant: next })
    } else if (editor && !editor.isDestroyed) {
      editor.commands.updateAttributes('calloutBlock', { variant: next })
    }
    menuOpen = false
    triggerEl?.focus()
  }

  function onTriggerKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ' || e.key === 'ArrowDown') {
      e.preventDefault()
      e.stopPropagation()
      openMenu()
    }
  }

  function onMenuKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      menuOpen = false
      triggerEl?.focus()
      return
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      activeIndex = (activeIndex + 1) % VARIANTS.length
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      activeIndex = (activeIndex - 1 + VARIANTS.length) % VARIANTS.length
      return
    }
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      const [key] = VARIANTS[activeIndex] ?? []
      if (key) selectVariant(key)
      return
    }
    // Tab leaves the menu; close so focus does not land in the editor behind
    // an open popup.
    if (e.key === 'Tab') {
      menuOpen = false
    }
  }
</script>

<NodeViewWrapper
  class="silt-callout group flex items-start gap-2 py-1.5 my-1 min-h-8"
  data-variant={variant}
>
  <div class="relative shrink-0" bind:this={wrapperEl}>
    <button
      bind:this={triggerEl}
      type="button"
      class="silt-callout-variant-btn material-symbols-outlined silt-callout-icon select-none text-type-2xl mt-0.5 border-none bg-transparent p-0 cursor-pointer rounded focus:outline-none focus-visible:ring-2 focus-visible:ring-border-focus"
      style="color: {cfg.accent}"
      aria-label="Callout type: {cfg.label}. Change variant"
      aria-haspopup="menu"
      aria-expanded={menuOpen}
      aria-controls={menuOpen ? menuDomId : undefined}
      contenteditable="false"
      onclick={(e) => {
        e.preventDefault()
        e.stopPropagation()
        if (menuOpen) menuOpen = false
        else openMenu()
      }}
      onkeydown={onTriggerKeydown}
    >
      {cfg.icon}
    </button>
    {#if menuOpen}
      <div
        bind:this={menuEl}
        id={menuDomId}
        class="silt-callout-variant-menu absolute left-0 top-full z-50 mt-1 min-w-[10rem] rounded-lg border border-surface-panel-border bg-surface-panel shadow-xl py-1"
        role="menu"
        aria-label="Callout variant"
        tabindex="-1"
        onkeydown={onMenuKeydown}
      >
        {#each VARIANTS as [key, meta], i (key)}
          <button
            type="button"
            role="menuitemradio"
            aria-checked={key === variant}
            class="w-full flex items-center gap-2 px-2.5 py-1.5 text-left border-none bg-transparent cursor-pointer text-type-sm font-body-md text-text-primary hover:bg-hover {i ===
            activeIndex
              ? 'bg-hover'
              : ''}"
            contenteditable="false"
            onclick={(e) => {
              e.preventDefault()
              e.stopPropagation()
              selectVariant(key)
            }}
            onmouseenter={() => (activeIndex = i)}
          >
            <span
              class="material-symbols-outlined text-icon-md"
              style="color: {meta.accent}"
              aria-hidden="true">{meta.icon}</span
            >
            <span class="flex-1">{meta.label}</span>
            {#if key === variant}
              <span
                class="material-symbols-outlined text-icon-sm text-accent-primary-start"
                aria-hidden="true">check</span
              >
            {/if}
          </button>
        {/each}
      </div>
    {/if}
  </div>
  <div class="flex-1 min-w-0" role={cfg.role} aria-label={cfg.label}>
    <NodeViewContent
      class="silt-callout-body break-words min-h-5.5 focus:outline-none"
    />
  </div>
</NodeViewWrapper>

<style>
  /* block+ content: paragraphs and nested blocks (code, tables, task lists)
     inside the callout body get compact spacing so a multi-block callout reads
     cleanly. Fully global because the children are rendered by TipTap's
     NodeViewContent at runtime. */
  :global(.silt-callout-body p) {
    margin: 0 0 0.25rem 0;
  }
  :global(.silt-callout-body p:last-child) {
    margin-bottom: 0;
  }
  /* Nested code blocks sit flush inside the callout, not as a detached card. */
  :global(.silt-callout-body .silt-code) {
    margin: 0.25rem 0;
  }
  /* Nested tables + task lists keep tight vertical rhythm. */
  :global(.silt-callout-body table) {
    margin: 0.25rem 0;
  }
  :global(.silt-callout-body ul),
  :global(.silt-callout-body ol) {
    margin: 0.25rem 0;
    padding-left: 1.25rem;
  }
</style>
