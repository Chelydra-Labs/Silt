<script lang="ts">
  import type { Editor } from 'svelte-tiptap'
  import { Browser } from '@wailsio/runtime'
  import {
    clampToViewport,
    flipOrClamp
  } from '../../lib/editor/popoverPositioning'

  // Compact floating format toolbar for non-empty text selection (#689 / #168).
  // Primary marks stay direct; lower-frequency marks live under More. Existing
  // links open an edit menu instead of unsetting on first click. Placement uses
  // shared flip/clamp helpers and dismisses on scroll/resize (#594). No auto-
  // focus on show so mouse/Shift+Arrow selection stays in the editor (#643).

  interface Props {
    editor: Editor | null
    activeMarks: Set<string>
    selectionEmpty: boolean
    selectionCoords: { left: number; top: number; bottom: number } | null
  }

  let { editor, activeMarks, selectionEmpty, selectionCoords }: Props = $props()

  type PrimaryBtn = {
    id: string
    icon: string
    label: string
    mark: string
  }

  const PRIMARY: PrimaryBtn[] = [
    { id: 'bold', icon: 'format_bold', label: 'Bold', mark: 'bold' },
    { id: 'italic', icon: 'format_italic', label: 'Italic', mark: 'italic' },
    {
      id: 'underline',
      icon: 'format_underlined',
      label: 'Underline',
      mark: 'underline'
    },
    { id: 'link', icon: 'link', label: 'Link', mark: 'link' },
    { id: 'code', icon: 'code', label: 'Inline code', mark: 'code' }
  ]

  const MORE_MARKS: PrimaryBtn[] = [
    {
      id: 'strike',
      icon: 'format_strikethrough',
      label: 'Strikethrough',
      mark: 'strike'
    },
    {
      id: 'highlight',
      icon: 'highlight',
      label: 'Highlight',
      mark: 'highlight'
    }
  ]

  type LinkAction = {
    id: 'edit' | 'open' | 'copy' | 'remove'
    icon: string
    label: string
  }

  const LINK_ACTIONS: LinkAction[] = [
    { id: 'edit', icon: 'edit', label: 'Edit link' },
    { id: 'open', icon: 'open_in_new', label: 'Open link' },
    { id: 'copy', icon: 'content_copy', label: 'Copy link' },
    { id: 'remove', icon: 'link_off', label: 'Remove link' }
  ]

  let show = $derived(!selectionEmpty && selectionCoords !== null)
  let focusIdx = $state(0)
  let menuEl = $state<HTMLDivElement | null>(null)
  let pos = $state({ left: -9999, top: -9999 })
  let dismissed = $state(false)
  let moreOpen = $state(false)
  let linkMenuOpen = $state(false)
  let copyStatus = $state('')

  // Primary + More trigger participate in the top-level roving set.
  const TOP_COUNT = PRIMARY.length + 1 // + More button

  let visible = $derived(show && !dismissed && selectionCoords !== null)

  // Cache once per reactive tick — template + handlers must not re-query ProseMirror.
  let currentHref = $derived.by(() => {
    if (!editor) return ''
    try {
      const attrs = editor.getAttributes('link') as { href?: string }
      return attrs?.href ?? ''
    } catch {
      return ''
    }
  })

  function toggleMark(mark: string): void {
    if (!editor) return
    editor.chain().focus().toggleMark(mark).run()
  }

  function openLinkInput(prefill?: string): void {
    window.dispatchEvent(
      new CustomEvent('silt:open-link-input', {
        detail: prefill != null ? { href: prefill } : undefined
      })
    )
  }

  function handlePrimary(btn: PrimaryBtn): void {
    if (!editor) return
    if (btn.id === 'link') {
      if (editor.isActive('link')) {
        linkMenuOpen = !linkMenuOpen
        moreOpen = false
        return
      }
      linkMenuOpen = false
      openLinkInput()
      return
    }
    linkMenuOpen = false
    moreOpen = false
    toggleMark(btn.mark)
  }

  function handleLinkAction(action: LinkAction): void {
    if (!editor) return
    const href = currentHref
    if (action.id === 'edit') {
      linkMenuOpen = false
      openLinkInput(href)
      return
    }
    if (action.id === 'open') {
      // Only http(s) — note content is user-controlled; reject file:/javascript: etc.
      if (href && /^https?:\/\//i.test(href)) {
        void Browser.OpenURL(href)
      } else if (href) {
        copyStatus = 'Invalid URL scheme'
        window.setTimeout(() => {
          copyStatus = ''
        }, 2000)
      }
      linkMenuOpen = false
      return
    }
    if (action.id === 'copy') {
      if (href && navigator.clipboard?.writeText) {
        void navigator.clipboard.writeText(href).then(() => {
          copyStatus = 'Link copied'
          window.setTimeout(() => {
            copyStatus = ''
          }, 1500)
        })
      }
      return
    }
    if (action.id === 'remove') {
      editor.chain().focus().unsetLink().run()
      linkMenuOpen = false
    }
  }

  function focusTop(idx: number): void {
    const n = TOP_COUNT
    const next = ((idx % n) + n) % n
    focusIdx = next
    queueMicrotask(() => {
      const btn =
        menuEl?.querySelectorAll<HTMLElement>('[data-bubble-tb]')[next]
      btn?.focus()
    })
  }

  let submenuFocus = $state(0)
  let moreMenuEl: HTMLElement | null = $state(null)
  let linkMenuEl: HTMLElement | null = $state(null)
  let moreMenuPos = $state<{ left: number; top: number } | null>(null)
  let linkMenuPos = $state<{ left: number; top: number } | null>(null)

  function positionSubmenu(
    el: HTMLElement | null
  ): { left: number; top: number } | null {
    if (!el || !menuEl) return null
    const bubble = menuEl.getBoundingClientRect()
    const width = el.offsetWidth || 160
    const height = el.offsetHeight || 120
    // Prefer below the bubble; flip above when near the bottom edge.
    return flipOrClamp(
      { top: bubble.top, bottom: bubble.bottom, left: bubble.left },
      { width, height },
      { width: window.innerWidth, height: window.innerHeight }
    )
  }

  $effect(() => {
    if (!moreOpen) {
      moreMenuPos = null
      return
    }
    void moreMenuEl
    queueMicrotask(() => {
      moreMenuPos = positionSubmenu(moreMenuEl)
    })
  })

  $effect(() => {
    if (!linkMenuOpen) {
      linkMenuPos = null
      return
    }
    void linkMenuEl
    queueMicrotask(() => {
      // Prefer right-align near the link control (end of primary strip).
      const el = linkMenuEl
      const host = menuEl
      if (!el || !host) {
        linkMenuPos = null
        return
      }
      const bubble = host.getBoundingClientRect()
      const width = el.offsetWidth || 180
      const height = el.offsetHeight || 140
      const preferredLeft = bubble.right - width
      const flipped = flipOrClamp(
        { top: bubble.top, bottom: bubble.bottom, left: preferredLeft },
        { width, height },
        { width: window.innerWidth, height: window.innerHeight }
      )
      // Horizontal clamp only if flipOrClamp left is off; keep preferred when possible.
      linkMenuPos = clampToViewport(
        {
          x: preferredLeft,
          y: flipped.top,
          width,
          height
        },
        { width: window.innerWidth, height: window.innerHeight }
      )
    })
  })

  function submenuItems(): HTMLButtonElement[] {
    if (!menuEl) return []
    return Array.from(
      menuEl.querySelectorAll<HTMLButtonElement>('.bubble-submenu button')
    )
  }

  function focusSubmenu(i: number): void {
    const items = submenuItems()
    if (!items.length) return
    const next = ((i % items.length) + items.length) % items.length
    submenuFocus = next
    items[next]?.focus()
  }

  function handleKeydown(e: KeyboardEvent): void {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      if (linkMenuOpen) {
        linkMenuOpen = false
        submenuFocus = 0
        return
      }
      if (moreOpen) {
        moreOpen = false
        submenuFocus = 0
        return
      }
      editor?.chain().focus().run()
      return
    }
    // Arrow roving inside More / link submenus (#689 harden).
    if (linkMenuOpen || moreOpen) {
      if (e.key === 'ArrowDown' || e.key === 'ArrowRight') {
        e.preventDefault()
        focusSubmenu(submenuFocus + 1)
        return
      }
      if (e.key === 'ArrowUp' || e.key === 'ArrowLeft') {
        e.preventDefault()
        focusSubmenu(submenuFocus - 1)
        return
      }
      if (e.key === 'Home') {
        e.preventDefault()
        focusSubmenu(0)
        return
      }
      if (e.key === 'End') {
        e.preventDefault()
        focusSubmenu(submenuItems().length - 1)
        return
      }
      return
    }
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
      e.preventDefault()
      focusTop(focusIdx + 1)
      return
    }
    if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
      e.preventDefault()
      focusTop(focusIdx - 1)
      return
    }
    if (e.key === 'Home') {
      e.preventDefault()
      focusTop(0)
      return
    }
    if (e.key === 'End') {
      e.preventDefault()
      focusTop(TOP_COUNT - 1)
      return
    }
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      if (focusIdx < PRIMARY.length) {
        handlePrimary(PRIMARY[focusIdx])
      } else {
        moreOpen = !moreOpen
        linkMenuOpen = false
        if (moreOpen) {
          submenuFocus = 0
          queueMicrotask(() => focusSubmenu(0))
        }
      }
    }
  }

  function updatePosition(): void {
    if (!selectionCoords || !visible) return
    const el = menuEl
    const width = el?.offsetWidth || 200
    const height = el?.offsetHeight || 40
    // Prefer above the selection so the bubble does not cover selected text.
    pos = flipOrClamp(
      {
        top: selectionCoords.top,
        bottom: selectionCoords.bottom,
        left: selectionCoords.left - width / 2
      },
      { width, height },
      { width: window.innerWidth, height: window.innerHeight }
    )
  }

  $effect(() => {
    if (show) {
      focusIdx = 0
      dismissed = false
      moreOpen = false
      linkMenuOpen = false
    }
  })

  $effect(() => {
    if (!visible) return
    void selectionCoords
    void menuEl
    updatePosition()
  })

  // Dismiss when the anchor becomes invalid through scroll/resize (#594 / #689).
  $effect(() => {
    if (!show) return
    const dismiss = (): void => {
      dismissed = true
      moreOpen = false
      linkMenuOpen = false
    }
    document.addEventListener('scroll', dismiss, {
      capture: true,
      passive: true
    })
    window.addEventListener('resize', dismiss, { passive: true })
    return () => {
      document.removeEventListener('scroll', dismiss, { capture: true })
      window.removeEventListener('resize', dismiss)
    }
  })

  // Close submenus on outside click (match FormatToolbar / HeadingLevelMenu).
  $effect(() => {
    if (!visible || (!moreOpen && !linkMenuOpen)) return
    const onDocClick = (e: MouseEvent): void => {
      const t = e.target as Node | null
      if (menuEl && t && !menuEl.contains(t)) {
        moreOpen = false
        linkMenuOpen = false
      }
    }
    // Next tick so the opening click does not immediately close.
    const id = window.setTimeout(() => {
      document.addEventListener('click', onDocClick, true)
    }, 0)
    return () => {
      window.clearTimeout(id)
      document.removeEventListener('click', onDocClick, true)
    }
  })
</script>

{#if visible && selectionCoords}
  <div
    bind:this={menuEl}
    class="selection-bubble"
    role="toolbar"
    tabindex="-1"
    aria-label="Format selection"
    aria-orientation="horizontal"
    style="left: {pos.left}px; top: {pos.top}px"
    onkeydown={handleKeydown}
  >
    {#each PRIMARY as btn, i (btn.id)}
      <button
        type="button"
        class="bubble-btn"
        class:active={activeMarks.has(btn.mark)}
        data-bubble-tb
        aria-pressed={activeMarks.has(btn.mark)}
        aria-label={btn.label}
        aria-haspopup={btn.id === 'link' ? 'menu' : undefined}
        aria-expanded={btn.id === 'link' ? linkMenuOpen : undefined}
        tabindex={i === focusIdx ? 0 : -1}
        onclick={() => {
          focusIdx = i
          handlePrimary(btn)
        }}
      >
        <span class="material-symbols-outlined" aria-hidden="true"
          >{btn.icon}</span
        >
      </button>
    {/each}

    <button
      type="button"
      class="bubble-btn"
      class:active={moreOpen}
      data-bubble-tb
      aria-label="More formatting"
      aria-haspopup="menu"
      aria-expanded={moreOpen}
      tabindex={focusIdx === PRIMARY.length ? 0 : -1}
      onclick={() => {
        focusIdx = PRIMARY.length
        moreOpen = !moreOpen
        linkMenuOpen = false
      }}
    >
      <span class="material-symbols-outlined" aria-hidden="true"
        >more_horiz</span
      >
    </button>

    {#if moreOpen}
      <div
        class="bubble-submenu"
        class:bubble-submenu-fixed={!!moreMenuPos}
        bind:this={moreMenuEl}
        role="menu"
        aria-label="More formatting"
        style={moreMenuPos
          ? `left:${moreMenuPos.left}px;top:${moreMenuPos.top}px`
          : undefined}
      >
        {#each MORE_MARKS as btn (btn.id)}
          <button
            type="button"
            class="bubble-menu-item"
            class:active={activeMarks.has(btn.mark)}
            role="menuitemcheckbox"
            aria-checked={activeMarks.has(btn.mark)}
            aria-label={btn.label}
            onclick={() => {
              toggleMark(btn.mark)
              moreOpen = false
            }}
          >
            <span class="material-symbols-outlined" aria-hidden="true"
              >{btn.icon}</span
            >
            <span>{btn.label}</span>
          </button>
        {/each}
      </div>
    {/if}

    {#if linkMenuOpen}
      <div
        class="bubble-submenu bubble-link-menu"
        class:bubble-submenu-fixed={!!linkMenuPos}
        bind:this={linkMenuEl}
        role="menu"
        aria-label="Link actions"
        style={linkMenuPos
          ? `left:${linkMenuPos.left}px;top:${linkMenuPos.top}px`
          : undefined}
      >
        {#if currentHref}
          <div class="bubble-link-href" title={currentHref}>
            {currentHref}
          </div>
        {/if}
        {#each LINK_ACTIONS as action (action.id)}
          <button
            type="button"
            class="bubble-menu-item"
            class:danger={action.id === 'remove'}
            role="menuitem"
            aria-label={action.label}
            onclick={() => handleLinkAction(action)}
          >
            <span class="material-symbols-outlined" aria-hidden="true"
              >{action.icon}</span
            >
            <span>{action.label}</span>
          </button>
        {/each}
      </div>
    {/if}

    {#if copyStatus}
      <span class="bubble-status" role="status">{copyStatus}</span>
    {/if}
  </div>
{/if}

<style>
  .selection-bubble {
    position: fixed;
    z-index: 100;
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 4px;
    border-radius: 8px;
    background: var(--color-surface-popover);
    border: 1px solid var(--color-surface-popover-border);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
  }

  .bubble-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    min-width: 32px;
    min-height: 32px;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--color-text-muted);
    cursor: pointer;
    position: relative;
  }

  .bubble-btn:hover {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 20%,
      transparent
    );
    color: var(--color-text-primary);
  }

  .bubble-btn:focus-visible {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 20%,
      transparent
    );
    color: var(--color-text-primary);
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: 1px;
  }

  .bubble-btn.active {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-glow) 25%,
      transparent
    );
    color: var(--color-accent-primary-glow);
  }

  .bubble-btn .material-symbols-outlined {
    font-size: 18px;
  }

  .bubble-submenu {
    position: absolute;
    top: calc(100% + 4px);
    left: 0;
    z-index: 1;
    min-width: 160px;
    padding: 4px;
    border-radius: 8px;
    background: var(--color-surface-popover);
    border: 1px solid var(--color-surface-popover-border);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  /* Measured placement uses fixed coords so flip/clamp can keep menus on-screen. */
  .bubble-submenu-fixed {
    position: fixed;
    top: auto;
    left: auto;
    right: auto;
  }

  .bubble-link-menu:not(.bubble-submenu-fixed) {
    left: auto;
    right: 0;
  }

  .bubble-link-href {
    max-width: 220px;
    padding: 6px 10px 4px;
    font-size: 11px;
    color: var(--color-text-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    border-bottom: 1px solid var(--color-surface-popover-border);
    margin-bottom: 2px;
  }

  .bubble-menu-item {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 32px;
    padding: 4px 10px;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--color-text-primary);
    font-size: 0.8rem;
    text-align: left;
    cursor: pointer;
  }

  .bubble-menu-item:hover,
  .bubble-menu-item:focus-visible {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 15%,
      transparent
    );
    outline: none;
  }

  .bubble-menu-item.active {
    color: var(--color-accent-primary-glow);
  }

  .bubble-menu-item.danger {
    color: var(--color-status-danger);
  }

  .bubble-menu-item .material-symbols-outlined {
    font-size: 16px;
    color: inherit;
  }

  .bubble-status {
    position: absolute;
    bottom: calc(100% + 4px);
    left: 50%;
    transform: translateX(-50%);
    padding: 2px 8px;
    border-radius: 4px;
    background: var(--color-surface-popover);
    border: 1px solid var(--color-surface-popover-border);
    font-size: 11px;
    color: var(--color-text-primary);
    white-space: nowrap;
  }
</style>
