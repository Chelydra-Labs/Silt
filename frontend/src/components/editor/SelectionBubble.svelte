<script lang="ts">
  import type { Editor } from 'svelte-tiptap'
  import { Browser } from '@wailsio/runtime'
  import {
    clampToViewport,
    flipOrClamp
  } from '../../lib/editor/popoverPositioning'
  import {
    toggleUnorderedList,
    toggleOrderedList,
    selectionIsListKind
  } from '../../lib/editor/keymaps'
  import ColorPickerMenu from './ColorPickerMenu.svelte'

  // Floating format toolbar for non-empty text selection (#689 / #168).
  // Two compact rows expose all common actions (no overflow menu): marks +
  // link/code on row 1; lists + colors on row 2. Link opens an edit menu when
  // already active. Placement prefers above the selection (#594 polish).

  interface Props {
    editor: Editor | null
    activeMarks: Set<string>
    selectionEmpty: boolean
    selectionCoords: { left: number; top: number; bottom: number } | null
    isDark?: boolean
    colorEnabled?: boolean
  }

  let {
    editor,
    activeMarks,
    selectionEmpty,
    selectionCoords,
    isDark = true,
    colorEnabled = true
  }: Props = $props()

  type MarkBtn = {
    id: string
    icon: string
    label: string
    mark: string
  }

  // Row 1 — inline marks (strike promoted out of the old More menu).
  const MARK_BUTTONS: MarkBtn[] = [
    { id: 'bold', icon: 'format_bold', label: 'Bold', mark: 'bold' },
    { id: 'italic', icon: 'format_italic', label: 'Italic', mark: 'italic' },
    {
      id: 'underline',
      icon: 'format_underlined',
      label: 'Underline',
      mark: 'underline'
    },
    {
      id: 'strike',
      icon: 'format_strikethrough',
      label: 'Strikethrough',
      mark: 'strike'
    },
    { id: 'link', icon: 'link', label: 'Link', mark: 'link' },
    { id: 'code', icon: 'code', label: 'Inline code', mark: 'code' }
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
  let linkMenuOpen = $state(false)
  let colorMenuOpen = $state(false)
  let copyStatus = $state('')

  // Indices: marks (0..5), bullet, ordered, [text color], [bg color]
  const LIST_BULLET_IDX = MARK_BUTTONS.length
  const LIST_ORDERED_IDX = MARK_BUTTONS.length + 1
  const TEXT_COLOR_IDX = MARK_BUTTONS.length + 2
  const BG_COLOR_IDX = MARK_BUTTONS.length + 3
  let topCount = $derived(MARK_BUTTONS.length + 2 + (colorEnabled ? 2 : 0))

  let visible = $derived(show && !dismissed && selectionCoords !== null)

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

  function handleMark(btn: MarkBtn): void {
    if (!editor) return
    if (btn.id === 'link') {
      if (editor.isActive('link')) {
        linkMenuOpen = !linkMenuOpen
        return
      }
      linkMenuOpen = false
      openLinkInput()
      return
    }
    linkMenuOpen = false
    toggleMark(btn.mark)
  }

  function listActive(kind: 'unordered' | 'ordered'): boolean {
    if (!editor || editor.isDestroyed) return false
    try {
      return selectionIsListKind(editor, kind)
    } catch {
      return false
    }
  }

  function handleBulletList(): void {
    if (!editor || editor.isDestroyed) return
    linkMenuOpen = false
    toggleUnorderedList(editor)
  }

  function handleOrderedList(): void {
    if (!editor || editor.isDestroyed) return
    linkMenuOpen = false
    toggleOrderedList(editor)
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

  function rovingButtons(): HTMLElement[] {
    if (!menuEl) return []
    return Array.from(
      menuEl.querySelectorAll<HTMLElement>(
        '[data-bubble-tb], .selection-bubble .color-trigger'
      )
    )
  }

  function focusTop(idx: number): void {
    const btns = rovingButtons()
    const n = btns.length || topCount
    const next = ((idx % n) + n) % n
    focusIdx = next
    queueMicrotask(() => {
      rovingButtons()[next]?.focus()
    })
  }

  let submenuFocus = $state(0)
  let linkMenuEl: HTMLElement | null = $state(null)
  let linkMenuPos = $state<{ left: number; top: number } | null>(null)

  $effect(() => {
    if (!linkMenuOpen) {
      linkMenuPos = null
      return
    }
    void linkMenuEl
    queueMicrotask(() => {
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

  function activateFocused(): void {
    if (focusIdx < MARK_BUTTONS.length) {
      handleMark(MARK_BUTTONS[focusIdx])
      return
    }
    if (focusIdx === LIST_BULLET_IDX) {
      handleBulletList()
      return
    }
    if (focusIdx === LIST_ORDERED_IDX) {
      handleOrderedList()
      return
    }
    // Color triggers: activate via click on the focused element.
    rovingButtons()[focusIdx]?.click()
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
      if (colorMenuOpen) {
        // ColorPickerMenu owns Escape for its panel; still clear our flag.
        colorMenuOpen = false
        return
      }
      editor?.chain().focus().run()
      return
    }
    if (linkMenuOpen) {
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
    if (colorMenuOpen) return
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
      focusTop(topCount - 1)
      return
    }
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      activateFocused()
    }
  }

  function updatePosition(): void {
    if (!selectionCoords || !visible) return
    const el = menuEl
    const width = el?.offsetWidth || 220
    const height = el?.offsetHeight || 80
    pos = flipOrClamp(
      {
        top: selectionCoords.top,
        bottom: selectionCoords.bottom,
        left: selectionCoords.left - width / 2
      },
      { width, height },
      { width: window.innerWidth, height: window.innerHeight },
      { placement: 'above' }
    )
  }

  $effect(() => {
    if (show) {
      focusIdx = 0
      dismissed = false
      linkMenuOpen = false
      colorMenuOpen = false
    }
  })

  $effect(() => {
    if (!visible) return
    void selectionCoords
    void menuEl
    void colorEnabled
    updatePosition()
  })

  $effect(() => {
    if (!show) return
    let scrollReShowTimer: ReturnType<typeof setTimeout> | undefined
    const hideTemporarily = (): void => {
      dismissed = true
      linkMenuOpen = false
      colorMenuOpen = false
    }
    // Scroll: hide while scrolling, re-show after settle if selection remains.
    const onScroll = (): void => {
      hideTemporarily()
      if (scrollReShowTimer) clearTimeout(scrollReShowTimer)
      scrollReShowTimer = setTimeout(() => {
        if (show) dismissed = false
      }, 160)
    }
    // Resize: permanent dismiss until selection changes (coords go stale).
    const onResize = (): void => {
      hideTemporarily()
    }
    document.addEventListener('scroll', onScroll, {
      capture: true,
      passive: true
    })
    window.addEventListener('resize', onResize, { passive: true })
    return () => {
      if (scrollReShowTimer) clearTimeout(scrollReShowTimer)
      document.removeEventListener('scroll', onScroll, { capture: true })
      window.removeEventListener('resize', onResize)
    }
  })

  $effect(() => {
    if (!visible || !linkMenuOpen) return
    const onDocClick = (e: MouseEvent): void => {
      const t = e.target as Node | null
      if (menuEl && t && !menuEl.contains(t)) {
        linkMenuOpen = false
      }
    }
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
    style="left: {pos.left}px; top: {pos.top}px"
    onkeydown={handleKeydown}
  >
    <div class="bubble-row" role="group" aria-label="Text style">
      {#each MARK_BUTTONS as btn, i (btn.id)}
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
            handleMark(btn)
          }}
        >
          <span class="material-symbols-outlined" aria-hidden="true"
            >{btn.icon}</span
          >
        </button>
      {/each}
    </div>

    <div class="bubble-row" role="group" aria-label="Paragraph and color">
      <button
        type="button"
        class="bubble-btn"
        class:active={listActive('unordered')}
        data-bubble-tb
        aria-pressed={listActive('unordered')}
        aria-label="Bullet list"
        tabindex={focusIdx === LIST_BULLET_IDX ? 0 : -1}
        onclick={() => {
          focusIdx = LIST_BULLET_IDX
          handleBulletList()
        }}
      >
        <span class="material-symbols-outlined" aria-hidden="true"
          >format_list_bulleted</span
        >
      </button>
      <button
        type="button"
        class="bubble-btn"
        class:active={listActive('ordered')}
        data-bubble-tb
        aria-pressed={listActive('ordered')}
        aria-label="Numbered list"
        tabindex={focusIdx === LIST_ORDERED_IDX ? 0 : -1}
        onclick={() => {
          focusIdx = LIST_ORDERED_IDX
          handleOrderedList()
        }}
      >
        <span class="material-symbols-outlined" aria-hidden="true"
          >format_list_numbered</span
        >
      </button>

      {#if colorEnabled}
        <span class="bubble-sep" aria-hidden="true"></span>
        <div class="bubble-color">
          <ColorPickerMenu
            {editor}
            markType="textColor"
            {isDark}
            toolbarTabIndex={focusIdx === TEXT_COLOR_IDX ? 0 : -1}
            onToolbarFocus={() => (focusIdx = TEXT_COLOR_IDX)}
            onMenuOpenChange={(open) => {
              colorMenuOpen = open
              if (open) linkMenuOpen = false
            }}
          />
        </div>
        <div class="bubble-color">
          <ColorPickerMenu
            {editor}
            markType="highlight"
            {isDark}
            toolbarTabIndex={focusIdx === BG_COLOR_IDX ? 0 : -1}
            onToolbarFocus={() => (focusIdx = BG_COLOR_IDX)}
            onMenuOpenChange={(open) => {
              colorMenuOpen = open
              if (open) linkMenuOpen = false
            }}
          />
        </div>
      {/if}
    </div>

    {#if linkMenuOpen}
      <div
        class="bubble-submenu bubble-link-menu bubble-submenu-fixed"
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
    flex-direction: column;
    align-items: stretch;
    gap: 2px;
    padding: 4px;
    border-radius: 10px;
    background: var(--color-surface-popover);
    border: 1px solid var(--color-surface-popover-border);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
    animation: silt-bubble-in 140ms ease-out;
  }

  .bubble-row {
    display: flex;
    align-items: center;
    gap: 2px;
  }

  .bubble-sep {
    width: 1px;
    height: 20px;
    margin: 0 2px;
    background: var(--color-surface-popover-border);
    flex-shrink: 0;
  }

  .bubble-color {
    display: inline-flex;
  }

  /* Match bubble hit targets for embedded color triggers. */
  .bubble-color :global(.color-trigger) {
    width: 32px;
    height: 32px;
    min-width: 32px;
    min-height: 32px;
    border-radius: 6px;
  }

  .bubble-color :global(.color-menu) {
    /* Open upward when possible so the panel does not cover the selection. */
    top: auto;
    bottom: calc(100% + 4px);
    z-index: 60;
  }

  @keyframes silt-bubble-in {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .selection-bubble {
      animation: none;
    }
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

  .bubble-submenu-fixed {
    position: fixed;
    top: auto;
    left: auto;
    right: auto;
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
