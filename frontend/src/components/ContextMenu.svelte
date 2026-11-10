<script lang="ts">
  import { tick } from 'svelte'
  import type { Snippet } from 'svelte'
  import {
    clampToViewport,
    findScrollableAncestor,
    submenuPosition
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

  // --- submenu flyouts (WAI-ARIA APG menu pattern) -------------------------
  //
  // Consumers opt a menuitem into a flyout by giving it aria-haspopup="menu"
  // and authoring the submenu as a sibling `[role="menu"]` immediately after
  // it, inside the same children snippet. The primitive hides those nested
  // menus, hosts open/close + positioning, and wires the parent's
  // aria-expanded / the flyout's aria-labelledby.

  /** Open flyout levels, outermost first (only one per menu level). */
  let openLevels: { parent: HTMLElement; menu: HTMLElement }[] = []
  let triggerIdSeq = 0

  function isParentItem(el: Element | null): boolean {
    return (
      !!el &&
      el.getAttribute('role') === 'menuitem' &&
      el.getAttribute('aria-haspopup') === 'menu'
    )
  }

  function isDisabledItem(el: HTMLElement): boolean {
    return (
      (el instanceof HTMLButtonElement && el.disabled) ||
      el.getAttribute('aria-disabled') === 'true'
    )
  }

  /** The submenu authored right after a parent item (APG sibling placement). */
  function findSubmenu(parent: HTMLElement): HTMLElement | null {
    let sib = parent.nextElementSibling
    while (sib) {
      if (sib.getAttribute('role') === 'menu') return sib as HTMLElement
      // Hit the next item before any menu — no submenu was authored.
      if (sib.getAttribute('role') === 'menuitem') return null
      sib = sib.nextElementSibling
    }
    return null
  }

  function focusFirstItem(container: HTMLElement) {
    const first = scopedItems(container)[0]
    if (first) first.focus()
    else container.focus()
  }

  /**
   * Items of one menu level: descendants of `menu` whose nearest menu is
   * `menu` itself, so flyout items never leak into the parent level's roving
   * order. Disabled items are skipped (existing primitive contract).
   */
  function scopedItems(menu: HTMLElement): HTMLElement[] {
    return Array.from(
      menu.querySelectorAll<HTMLElement>('[role="menuitem"]')
    ).filter(
      (el) =>
        el.closest('[role="menu"]') === menu && !isDisabledItem(el)
    )
  }

  function openSubmenu(parent: HTMLElement, sub: HTMLElement) {
    // Close levels that are not on the path to this parent (e.g. switching
    // from one root parent's flyout to another's).
    while (
      openLevels.length &&
      !openLevels[openLevels.length - 1].menu.contains(parent)
    ) {
      closeTopLevel(false)
    }

    if (!parent.id) parent.id = `ctx-sub-trigger-${++triggerIdSeq}`
    sub.setAttribute('aria-labelledby', parent.id)
    // Focusable container so an empty flyout still holds focus deterministically.
    sub.setAttribute('tabindex', '-1')
    parent.setAttribute('aria-expanded', 'true')

    // Lay out hidden so the flyout measures at its real size, then place and
    // reveal — same never-flash-at-origin pattern as the root menu.
    sub.style.visibility = 'hidden'
    sub.classList.add('is-open')
    const rect = parent.getBoundingClientRect()
    const pos = submenuPosition(
      { left: rect.left, right: rect.right, top: rect.top },
      { width: sub.offsetWidth, height: sub.offsetHeight },
      { width: window.innerWidth, height: window.innerHeight }
    )
    sub.style.left = `${pos.left}px`
    sub.style.top = `${pos.top}px`
    sub.style.visibility = ''

    openLevels.push({ parent, menu: sub })
    focusFirstItem(sub)
  }

  function closeTopLevel(focusParent: boolean) {
    const level = openLevels.pop()
    if (!level) return
    level.parent.setAttribute('aria-expanded', 'false')
    level.menu.classList.remove('is-open')
    if (focusParent) level.parent.focus()
  }

  function closeAllSubmenus() {
    while (openLevels.length) closeTopLevel(false)
  }

  /** Deepest open flyout, if focus is inside it. */
  function focusedLevel(): { parent: HTMLElement; menu: HTMLElement } | null {
    const active = document.activeElement
    const deepest = openLevels[openLevels.length - 1]
    return deepest && active instanceof Node && deepest.menu.contains(active)
      ? deepest
      : null
  }

  // --- positioning + dismissal $effect ------------------------------------

  $effect(() => {
    if (!open || !anchor) {
      menuPos = null
      openLevels.length = 0
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

    // Parent items toggle their flyout on mouse click (keyboard uses
    // ArrowRight / Enter / Space in onMenuKeydown). Leaf clicks fall through
    // to consumer handlers untouched.
    const onRootClick = (e: MouseEvent) => {
      const item = (e.target as HTMLElement | null)?.closest<HTMLElement>(
        '[role="menuitem"]'
      )
      if (!item) return
      if (!isParentItem(item) || isDisabledItem(item)) return
      const sub = findSubmenu(item)
      if (!sub) return
      e.preventDefault()
      const deepest = openLevels[openLevels.length - 1]
      if (deepest && deepest.parent === item) {
        closeTopLevel(false)
      } else {
        openSubmenu(item, sub)
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
    const root = menuEl
    root?.addEventListener('click', onRootClick)

    window.addEventListener('resize', dismiss, { passive: true })
    window.addEventListener('keydown', onKey)

    return () => {
      scrollRoot.removeEventListener('scroll', dismiss, { capture: true })
      root?.removeEventListener('click', onRootClick)
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
    // `anchor` is part of the guard: a re-anchor while open presents a new
    // menu, so seeding + initial focus run again for the new contents.
    if (open && anchor && menuEl) {
      // aria-expanded must exist in both states; seed it before first
      // interaction so parent items announce "collapsed" right away.
      menuEl
        .querySelectorAll('[role="menuitem"][aria-haspopup="menu"]')
        .forEach((el) => {
          if (!el.hasAttribute('aria-expanded')) {
            el.setAttribute('aria-expanded', 'false')
          }
        })
      // Scoped to root-level items so a hidden flyout's items can't steal
      // the initial focus.
      focusFirstItem(menuEl)
    }
  })

  // --- arrow-key navigation (WAI-ARIA menu pattern) -----------------------

  function onMenuKeydown(e: KeyboardEvent) {
    const active = document.activeElement as HTMLElement | null
    // The menu level that owns focus — root, or the flyout containing it.
    const scope = active?.closest<HTMLElement>('[role="menu"]') ?? menuEl

    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      const level = focusedLevel()
      if (level) {
        // Escape inside a flyout closes just that level and refocuses its
        // parent item; the root menu stays open.
        closeTopLevel(true)
      } else {
        onClose()
      }
      return
    }

    if (e.key === 'Tab') {
      // Never cycle within the menu: close up and let focus move on naturally.
      closeAllSubmenus()
      onClose()
      return
    }

    if (e.key === 'ArrowRight') {
      // Only parent items open a flyout; on a leaf, ArrowRight does nothing.
      if (active && isParentItem(active) && !isDisabledItem(active)) {
        const sub = findSubmenu(active)
        if (sub) {
          e.preventDefault()
          openSubmenu(active, sub)
        }
      }
      return
    }

    if (e.key === 'ArrowLeft') {
      const level = focusedLevel()
      if (level) {
        // Closes the flyout and returns focus to its parent item; at root
        // level ArrowLeft does nothing.
        e.preventDefault()
        closeTopLevel(true)
      }
      return
    }

    if (e.key === 'Enter' || e.key === ' ') {
      if (active && isParentItem(active) && !isDisabledItem(active)) {
        const sub = findSubmenu(active)
        if (sub) {
          // Suppress the native button click so the flyout isn't also
          // toggled by the delegated click handler.
          e.preventDefault()
          openSubmenu(active, sub)
        }
      }
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
    if (!scope) return

    const items = scopedItems(scope)
    if (items.length === 0) return

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
  /* Submenus authored in the children snippet: hidden until the primitive
     opens them as fixed-position flyouts beside their parent item. */
  :global([data-context-menu-root] [role='menu']) {
    display: none;
  }
  :global([data-context-menu-root] [role='menu'].is-open) {
    display: block;
    position: fixed;
    min-width: 150px;
    padding: 4px;
    background-color: color-mix(
      in srgb,
      var(--color-surface-popover) 94%,
      transparent
    );
    backdrop-filter: blur(12px) saturate(140%);
    border: 1px solid var(--color-border-active);
    border-radius: 8px;
    box-shadow: 0 10px 25px -5px rgba(0, 0, 0, 0.5);
    z-index: 182;
  }
</style>
