<script lang="ts">
  import { tick } from 'svelte'
  import type { Snippet } from 'svelte'
  import {
    clampToViewport,
    findScrollableAncestor
  } from '../lib/editor/popoverPositioning'

  interface Props {
    /** Whether the menu is open. When false, the menu is not rendered. */
    open: boolean
    /** The anchor position (cursor coordinates). null when menu is closed. */
    anchor: { x: number; y: number } | null
    /**
     * The anchor element (the element the menu is attached to, e.g. the sidebar
     * row that was right-clicked). Used to find the nearest scrollable ancestor
     * for scoped scroll-dismissal (#492). When null, scroll-dismiss falls back
     * to the document.
     */
    anchorEl?: HTMLElement | null
    /** Called when the menu is dismissed (scroll, resize, Escape, backdrop click). */
    onClose: () => void
    /** Accessible label for the menu container. Defaults to "Actions". */
    ariaLabel?: string
    /** Optional data-testid for the backdrop button (used by consumer tests). */
    backdropTestId?: string
    /** Optional data-testid for the menu container (used by consumer tests). */
    menuTestId?: string
    menuId?: string
    /** Menu item slot. */
    children?: Snippet
  }

  let {
    open,
    anchor,
    anchorEl = null,
    onClose,
    ariaLabel = 'Actions',
    backdropTestId,
    menuTestId,
    menuId,
    children
  }: Props = $props()

  let menuEl = $state<HTMLDivElement | null>(null)
  let menuPos = $state<{ left: number; top: number } | null>(null)

  // --- positioning + dismissal $effect ------------------------------------

  $effect(() => {
    if (!open || !anchor) {
      menuPos = null
      return
    }
    const currentAnchor = anchor

    const measure = () => {
      const w = menuEl?.offsetWidth ?? 180
      const h = menuEl?.offsetHeight ?? 0
      menuPos = clampToViewport(
        { x: currentAnchor.x, y: currentAnchor.y, width: w, height: h },
        { width: window.innerWidth, height: window.innerHeight }
      )
    }

    // offsetWidth is 0 before the menu mounts; measure now (rough) and again
    // after the DOM flushes (accurate) — same pattern as both sidebars today.
    measure()
    void tick().then(measure)

    const dismiss = () => {
      onClose()
    }

    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onClose()
      }
    }

    // Scoped scroll listener (#492): listen on the anchor's nearest scrollable
    // ancestor, falling back to document. This avoids dismissing when an
    // unrelated region (e.g. the editor) scrolls.
    const scrollRoot = findScrollableAncestor(anchorEl)
    scrollRoot.addEventListener('scroll', dismiss, {
      capture: true,
      passive: true
    })

    window.addEventListener('resize', dismiss, { passive: true })
    window.addEventListener('keydown', onKey)

    return () => {
      scrollRoot.removeEventListener('scroll', dismiss, { capture: true })
      window.removeEventListener('resize', dismiss)
      window.removeEventListener('keydown', onKey)
    }
  })

  // --- focus management ---------------------------------------------------

  // Capture the trigger element at open so we can restore focus on close.
  let triggerEl: HTMLElement | null = null
  $effect(() => {
    if (open && anchorEl) {
      triggerEl = anchorEl
    }
    return () => {
      if (triggerEl) {
        triggerEl.focus()
        triggerEl = null
      }
    }
  })

  $effect(() => {
    if (open && menuEl) {
      const first = menuEl.querySelector<HTMLElement>(
        '[role="menuitem"]:not([disabled]):not([aria-disabled="true"])'
      )
      if (first) {
        first.focus()
      } else {
        menuEl.focus()
      }
    }
  })

  // --- arrow-key navigation (WAI-ARIA menu pattern) -----------------------

  function onMenuKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      onClose()
      return
    }
    if (
      e.key !== 'ArrowDown' &&
      e.key !== 'ArrowUp' &&
      e.key !== 'Home' &&
      e.key !== 'End'
    ) {
      return
    }
    e.preventDefault()
    const menu = menuEl
    if (!menu) return

    const items = Array.from(
      menu.querySelectorAll<HTMLElement>('[role="menuitem"]')
    ).filter((el) => {
      const disabled = el instanceof HTMLButtonElement ? el.disabled : false
      return !disabled && el.getAttribute('aria-disabled') !== 'true'
    })

    if (items.length === 0) return

    const active = document.activeElement as HTMLElement | null
    const idx = active ? items.indexOf(active) : -1

    let next: number
    if (e.key === 'Home') {
      next = 0
    } else if (e.key === 'End') {
      next = items.length - 1
    } else if (e.key === 'ArrowDown') {
      next = (idx + 1) % items.length
    } else {
      // ArrowUp
      next = (idx - 1 + items.length) % items.length
    }
    items[next]?.focus()
  }
</script>

{#if open && anchor}
  <div class="fixed inset-0 z-[180]">
    <!-- Backdrop: click or right-click dismisses. Tabindex -1 so it doesn't
         enter the tab order but still captures clicks. -->
    <button
      tabindex="-1"
      aria-label="Close context menu"
      data-testid={backdropTestId}
      onclick={onClose}
      oncontextmenu={(e) => {
        e.preventDefault()
        onClose()
      }}
      class="absolute inset-0 cursor-default border-none bg-transparent p-0"
    ></button>

    <!-- Menu card: fixed-position, clamped to viewport. Hidden until the
         first measure so it never flashes at the viewport origin. -->
    <div
      bind:this={menuEl}
      class="fixed context-menu-card"
      data-context-menu-root
      style:left={(menuPos?.left ?? anchor.x) + 'px'}
      style:top={(menuPos?.top ?? anchor.y) + 'px'}
      style:visibility={menuPos ? 'visible' : 'hidden'}
      role="menu"
      id={menuId}
      tabindex="-1"
      aria-label={ariaLabel}
      data-testid={menuTestId}
      onkeydown={onMenuKeydown}
    >
      {@render children?.()}
    </div>
  </div>
{/if}

<style>
  .context-menu-card {
    background-color: color-mix(
      in srgb,
      var(--color-surface-popover) 94%,
      transparent
    );
    backdrop-filter: blur(12px) saturate(140%);
    border: 1px solid var(--color-border-active);
    border-radius: 8px;
    box-shadow: 0 10px 25px -5px rgba(0, 0, 0, 0.5);
    padding: 4px;
    min-width: 180px;
    z-index: 181;
  }
  :global([data-context-menu-root] [role='menuitem']) {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 6px 10px;
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
  :global([data-context-menu-root] [role='menuitem']:hover:not(:disabled)) {
    background-color: var(--color-hover);
  }
  :global(
    [data-context-menu-root] [role='menuitem']:focus-visible:not(:disabled)
  ) {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: -2px;
  }
  :global(
    [data-context-menu-root] [role='menuitem']:disabled,
    [data-context-menu-root] [role='menuitem'][aria-disabled='true']
  ) {
    opacity: 0.4;
    cursor: not-allowed;
  }
  :global([data-context-menu-root] .context-menu-separator) {
    height: 1px;
    margin: 4px 6px;
    background-color: var(--color-border);
  }
</style>
