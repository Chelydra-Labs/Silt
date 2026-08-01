<script lang="ts">
  import type { Editor } from 'svelte-tiptap'
  import HeadingLevelMenu from './HeadingLevelMenu.svelte'
  import ColorPickerMenu from './ColorPickerMenu.svelte'
  import {
    toggleBlockQuote,
    toggleUnorderedList,
    toggleOrderedList,
    selectionIsListKind,
    insertCallout,
    insertCodeBlock,
    insertDetails,
    insertTable
  } from '../../lib/editor'
  import { nearestEnabledIndex } from '../../lib/editor/rovingTabindex'
  import { settings } from '../../settings/store.svelte'
  import { resolveHotkeyDisplay } from '../../settings/hotkeys'

  // Hierarchical format toolbar (#690 / #168). Priority strip always shows
  // block style + Bold + Italic + Underline + Link + Inline code. Advanced
  // marks, align, and inserts live in labelled overflow menus so a ≤600px
  // editor never clips or horizontally scrolls.

  interface Props {
    editor: Editor | null
    activeMarks: Set<string>
    isDark: boolean
    colorEnabled: boolean
  }

  let { editor, activeMarks, isDark, colorEnabled }: Props = $props()

  let hotkeys = $derived(settings.config?.hotkeys ?? {})
  function hk(action: string): string {
    return resolveHotkeyDisplay(action, hotkeys)
  }

  interface FormatButton {
    id: string
    label: string
    icon: string
    hotkey: string
    mark: string
  }

  const PRIMARY_MARKS: FormatButton[] = [
    {
      id: 'bold',
      label: 'Bold',
      icon: 'format_bold',
      hotkey: 'format_bold',
      mark: 'bold'
    },
    {
      id: 'italic',
      label: 'Italic',
      icon: 'format_italic',
      hotkey: 'format_italic',
      mark: 'italic'
    },
    {
      id: 'underline',
      label: 'Underline',
      icon: 'format_underlined',
      hotkey: 'format_underline',
      mark: 'underline'
    },
    {
      id: 'code',
      label: 'Inline code',
      icon: 'code',
      hotkey: 'format_code',
      mark: 'code'
    }
  ]

  const MORE_MARKS: FormatButton[] = [
    {
      id: 'strike',
      label: 'Strikethrough',
      icon: 'format_strikethrough',
      hotkey: 'format_strike',
      mark: 'strike'
    },
    {
      id: 'highlight',
      label: 'Highlight',
      icon: 'highlight',
      hotkey: 'format_highlight',
      mark: 'highlight'
    },
    {
      id: 'subscript',
      label: 'Subscript',
      icon: 'subscript',
      hotkey: 'format_subscript',
      mark: 'subscript'
    },
    {
      id: 'superscript',
      label: 'Superscript',
      icon: 'superscript',
      hotkey: 'format_superscript',
      mark: 'superscript'
    }
  ]

  const ALIGN_BUTTONS = [
    {
      id: 'left',
      label: 'Align left',
      icon: 'format_align_left',
      hotkey: 'align_left'
    },
    {
      id: 'center',
      label: 'Align center',
      icon: 'format_align_center',
      hotkey: 'align_center'
    },
    {
      id: 'right',
      label: 'Align right',
      icon: 'format_align_right',
      hotkey: 'align_right'
    },
    {
      id: 'justify',
      label: 'Align justify',
      icon: 'format_align_justify',
      hotkey: 'align_justify'
    }
  ]

  interface InsertButton {
    id: string
    label: string
    icon: string
    run: () => void
  }
  const INSERT_BUTTONS: InsertButton[] = [
    {
      id: 'quote',
      label: 'Quote',
      icon: 'format_quote',
      run: () => editor && toggleBlockQuote(editor)
    },
    {
      id: 'code-block',
      label: 'Code block',
      icon: 'code_blocks',
      run: () => editor && insertCodeBlock(editor)
    },
    {
      id: 'callout',
      label: 'Callout',
      icon: 'info',
      run: () => editor && insertCallout(editor, 'note')
    },
    {
      id: 'details',
      label: 'Foldable section',
      icon: 'unfold_more',
      run: () => editor && insertDetails(editor)
    },
    {
      id: 'table',
      label: 'Table',
      icon: 'table_view',
      run: () => editor && insertTable(editor, 3, 3)
    }
  ]

  // Tick so can() / align reflect the live selection.
  let selTick = $state(0)
  $effect(() => {
    if (!editor) return
    const bump = (): void => {
      selTick++
    }
    editor.on('selectionUpdate', bump)
    editor.on('transaction', bump)
    return () => {
      editor.off('selectionUpdate', bump)
      editor.off('transaction', bump)
    }
  })

  function canToggleMark(mark: string): boolean {
    void selTick
    if (!editor) return false
    try {
      const can = editor.can() as {
        toggleMark?: (m: string) => boolean
        chain?: () => {
          focus: () => {
            toggleMark: (m: string) => { run: () => boolean }
          }
        }
      }
      if (typeof can.toggleMark === 'function') return !!can.toggleMark(mark)
      return !!can.chain?.().focus().toggleMark(mark).run()
    } catch {
      // Fail closed: never enable a silent no-op when can() throws (#690).
      return false
    }
  }

  function canLink(): boolean {
    void selTick
    if (!editor) return false
    if (editor.isActive('link')) return true
    return !editor.state.selection.empty
  }

  function handleClick(btn: FormatButton): void {
    if (!editor || !canToggleMark(btn.mark)) return
    editor.chain().focus().toggleMark(btn.mark).run()
  }

  function handleLink(): void {
    if (!editor || !canLink()) return
    window.dispatchEvent(new CustomEvent('silt:open-link-input'))
  }

  function handleClear(): void {
    if (!editor) return
    editor.chain().focus().unsetAllMarks().run()
  }

  function handleAlign(align: string): void {
    if (!editor) return
    window.dispatchEvent(
      new CustomEvent('silt:set-block-align', { detail: align })
    )
    alignOpen = false
  }

  function isActive(mark: string): boolean {
    return activeMarks.has(mark)
  }

  function currentAlign(): string {
    void selTick
    if (!editor) return 'left'
    const pos = editor.state.selection.$from
    for (let d = pos.depth; d >= 1; d--) {
      const node = pos.node(d)
      const attrs = node.attrs as Record<string, unknown>
      if (attrs.align) return attrs.align as string
    }
    return 'left'
  }

  let moreOpen = $state(false)
  let alignOpen = $state(false)
  let insertOpen = $state(false)
  let headingOpen = $state(false)
  let colorTextOpen = $state(false)
  let colorBgOpen = $state(false)
  let moreWrap = $state<HTMLDivElement | null>(null)
  let alignWrap = $state<HTMLDivElement | null>(null)
  let insertWrap = $state<HTMLDivElement | null>(null)

  $effect(() => {
    if (!moreOpen && !alignOpen && !insertOpen) return
    const onClick = (e: MouseEvent) => {
      const t = e.target as Node
      if (moreOpen && moreWrap && !moreWrap.contains(t)) moreOpen = false
      if (alignOpen && alignWrap && !alignWrap.contains(t)) alignOpen = false
      if (insertOpen && insertWrap && !insertWrap.contains(t))
        insertOpen = false
    }
    document.addEventListener('click', onClick)
    return () => document.removeEventListener('click', onClick)
  })

  let rovingIdx = $state(0)
  let toolbarEl: HTMLElement | null = $state(null)

  function toolbarButtons(): HTMLButtonElement[] {
    if (!toolbarEl) return []
    return Array.from(
      toolbarEl.querySelectorAll<HTMLButtonElement>('[data-tb]')
    )
  }

  function openMenuItems(): HTMLButtonElement[] {
    if (!toolbarEl) return []
    // Prefer the visible overflow panel under More/Align/Insert.
    const panel =
      toolbarEl.querySelector<HTMLElement>('.menu-panel:not([hidden])') ||
      Array.from(toolbarEl.querySelectorAll<HTMLElement>('.menu-panel')).find(
        (el) => el.offsetParent !== null
      )
    if (!panel) return []
    return Array.from(
      panel.querySelectorAll<HTMLButtonElement>('button.menu-item, button')
    ).filter((b) => !b.disabled)
  }

  function handleKeydown(e: KeyboardEvent): void {
    // When a nested menu is open, Esc closes it first (menu → trigger).
    // Heading/Color capture Escape themselves; still clear our flags.
    if (e.key === 'Escape') {
      if (moreOpen || alignOpen || insertOpen) {
        e.preventDefault()
        e.stopPropagation()
        moreOpen = false
        alignOpen = false
        insertOpen = false
        toolbarButtons()[rovingIdx]?.focus()
        return
      }
      if (headingOpen || colorTextOpen || colorBgOpen) {
        // Child already handled Esc; do not move toolbar roving.
        return
      }
      e.preventDefault()
      editor?.chain().focus().run()
      return
    }

    // Arrow roving inside open overflow menus (match SelectionBubble).
    // Heading/Color own their menus — do not steal arrows while they are open.
    if (headingOpen || colorTextOpen || colorBgOpen) {
      return
    }
    if (moreOpen || alignOpen || insertOpen) {
      const items = openMenuItems()
      if (items.length === 0) return
      const cur = items.findIndex((b) => b === document.activeElement)
      let idx = cur < 0 ? 0 : cur
      if (e.key === 'ArrowDown' || e.key === 'ArrowRight') {
        e.preventDefault()
        idx = (idx + 1) % items.length
        items[idx]?.focus()
        return
      }
      if (e.key === 'ArrowUp' || e.key === 'ArrowLeft') {
        e.preventDefault()
        idx = (idx - 1 + items.length) % items.length
        items[idx]?.focus()
        return
      }
      if (e.key === 'Home') {
        e.preventDefault()
        items[0]?.focus()
        return
      }
      if (e.key === 'End') {
        e.preventDefault()
        items[items.length - 1]?.focus()
        return
      }
      return
    }

    const btns = toolbarButtons()
    if (btns.length === 0) return
    const disabled = btns.map((b) => b.disabled)
    let next: number
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
      e.preventDefault()
      next = nearestEnabledIndex(disabled, rovingIdx, 1)
    } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
      e.preventDefault()
      next = nearestEnabledIndex(disabled, rovingIdx, -1)
    } else if (e.key === 'Home') {
      e.preventDefault()
      next = nearestEnabledIndex(disabled, -1, 1)
    } else if (e.key === 'End') {
      e.preventDefault()
      next = nearestEnabledIndex(disabled, disabled.length, -1)
    } else {
      return
    }
    rovingIdx = next
    btns[next]?.focus()
  }

  function onTbFocus(i: number): void {
    rovingIdx = i
  }

  // Roving indices follow paragraph-toolbar convention (Zendesk/CKEditor/Word):
  // block style → inline marks → more → lists + align (paragraph) → insert → color.
  // 0: heading, 1..4: primary marks, 5: link, 6: more,
  // 7: bullet list, 8: ordered list, 9: align, 10: insert, then colors, clear.
  const HEADING_IDX = 0
  const PRIMARY_START = 1
  const LINK_IDX = PRIMARY_START + PRIMARY_MARKS.length
  const MORE_IDX = LINK_IDX + 1
  const BULLET_LIST_IDX = MORE_IDX + 1
  const ORDERED_LIST_IDX = BULLET_LIST_IDX + 1
  const ALIGN_IDX = ORDERED_LIST_IDX + 1
  const INSERT_IDX = ALIGN_IDX + 1
  const COLOR_START = INSERT_IDX + 1
  let clearIdx = $derived(COLOR_START + (colorEnabled ? 2 : 0))

  function canToggleList(): boolean {
    void selTick
    if (!editor || editor.isDestroyed) return false
    try {
      if (
        selectionIsListKind(editor, 'unordered') ||
        selectionIsListKind(editor, 'ordered')
      ) {
        return true
      }
      // Enable when caret/selection intersects at least one noteBlock.
      const { from, to, empty } = editor.state.selection
      if (empty) {
        const pos = editor.state.selection.$from
        for (let d = pos.depth; d >= 1; d--) {
          if (pos.node(d).type.name === 'noteBlock') return true
        }
        return false
      }
      let found = false
      editor.state.doc.nodesBetween(from, to, (node) => {
        if (node.type.name === 'noteBlock') {
          found = true
          return false
        }
        return true
      })
      return found
    } catch {
      return false
    }
  }

  function listActive(kind: 'unordered' | 'ordered'): boolean {
    void selTick
    if (!editor) return false
    try {
      return selectionIsListKind(editor, kind)
    } catch {
      return false
    }
  }

  function handleBulletList(): void {
    if (!editor || !canToggleList()) return
    toggleUnorderedList(editor)
  }

  function handleOrderedList(): void {
    if (!editor || !canToggleList()) return
    toggleOrderedList(editor)
  }
</script>

<div
  class="format-toolbar"
  role="toolbar"
  aria-label="Text formatting"
  tabindex="-1"
  bind:this={toolbarEl}
  onkeydown={handleKeydown}
>
  <HeadingLevelMenu
    {editor}
    toolbarTabIndex={rovingIdx === HEADING_IDX ? 0 : -1}
    onToolbarFocus={() => onTbFocus(HEADING_IDX)}
    onMenuOpenChange={(open) => (headingOpen = open)}
  />

  <span class="toolbar-divider" aria-hidden="true"></span>

  <div class="toolbar-group" role="group" aria-label="Common formatting">
    {#each PRIMARY_MARKS as btn, i (btn.id)}
      {@const idx = PRIMARY_START + i}
      <button
        type="button"
        class="toolbar-btn"
        class:active={isActive(btn.mark)}
        aria-pressed={isActive(btn.mark)}
        aria-label={btn.label}
        aria-keyshortcuts={hk(btn.hotkey) || undefined}
        data-tb
        data-primary
        disabled={!canToggleMark(btn.mark)}
        tabindex={rovingIdx === idx ? 0 : -1}
        onclick={() => handleClick(btn)}
        onfocus={() => onTbFocus(idx)}
        title={hk(btn.hotkey) ? `${btn.label} (${hk(btn.hotkey)})` : btn.label}
      >
        <span class="material-symbols-outlined" aria-hidden="true"
          >{btn.icon}</span
        >
      </button>
    {/each}

    <button
      type="button"
      class="toolbar-btn"
      class:active={isActive('link')}
      aria-pressed={isActive('link')}
      aria-label="Insert link"
      aria-keyshortcuts={hk('format_link') || undefined}
      data-tb
      data-primary
      disabled={!canLink()}
      tabindex={rovingIdx === LINK_IDX ? 0 : -1}
      onclick={handleLink}
      onfocus={() => onTbFocus(LINK_IDX)}
      title="Insert link"
    >
      <span class="material-symbols-outlined" aria-hidden="true">link</span>
    </button>
  </div>

  <span class="toolbar-divider" aria-hidden="true"></span>

  <div
    class="toolbar-group toolbar-overflow"
    role="group"
    aria-label="More actions"
  >
    <div class="menu-wrap" bind:this={moreWrap}>
      <button
        type="button"
        class="toolbar-btn toolbar-menu-trigger"
        class:active={moreOpen}
        aria-label="More formatting"
        aria-haspopup="menu"
        aria-expanded={moreOpen}
        data-tb
        tabindex={rovingIdx === MORE_IDX ? 0 : -1}
        onclick={() => {
          moreOpen = !moreOpen
          alignOpen = false
          insertOpen = false
        }}
        onfocus={() => onTbFocus(MORE_IDX)}
        title="More formatting"
      >
        <span class="material-symbols-outlined" aria-hidden="true"
          >more_horiz</span
        >
        <span class="trigger-label">More</span>
      </button>
      {#if moreOpen}
        <div class="toolbar-menu" role="menu" aria-label="More formatting">
          {#each MORE_MARKS as btn (btn.id)}
            <button
              type="button"
              class="menu-item"
              class:active={isActive(btn.mark)}
              role="menuitemcheckbox"
              aria-checked={isActive(btn.mark)}
              aria-label={btn.label}
              disabled={!canToggleMark(btn.mark)}
              title={hk(btn.hotkey)
                ? `${btn.label} (${hk(btn.hotkey)})`
                : btn.label}
              onclick={() => {
                handleClick(btn)
                moreOpen = false
              }}
            >
              <span class="material-symbols-outlined" aria-hidden="true"
                >{btn.icon}</span
              >
              <span>{btn.label}</span>
            </button>
          {/each}
          <span class="menu-sep" aria-hidden="true"></span>
          <button
            type="button"
            class="menu-item"
            role="menuitem"
            aria-label="Check spelling"
            onclick={() => {
              moreOpen = false
              const rect = editor?.view.dom.getBoundingClientRect()
              const sel = editor?.view.coordsAtPos(editor.state.selection.head)
              window.dispatchEvent(
                new CustomEvent('silt:open-spellcheck', {
                  detail: sel
                    ? { x: sel.left, y: sel.bottom + 4 }
                    : rect
                      ? { x: rect.left + 40, y: rect.top + 40 }
                      : { x: 100, y: 100 }
                })
              )
            }}
          >
            <span class="material-symbols-outlined" aria-hidden="true"
              >spellcheck</span
            >
            <span>Check spelling</span>
          </button>
        </div>
      {/if}
    </div>
  </div>

  <span class="toolbar-divider" aria-hidden="true"></span>

  <!-- Paragraph structure: lists + alignment (Word/CKEditor/Zendesk convention). -->
  <div class="toolbar-group" role="group" aria-label="Paragraph">
    <button
      type="button"
      class="toolbar-btn"
      class:active={listActive('unordered')}
      aria-pressed={listActive('unordered')}
      aria-label="Bullet list"
      aria-keyshortcuts={hk('toggle_bullet_list') || undefined}
      data-tb
      data-primary
      disabled={!canToggleList()}
      tabindex={rovingIdx === BULLET_LIST_IDX ? 0 : -1}
      onclick={handleBulletList}
      onfocus={() => onTbFocus(BULLET_LIST_IDX)}
      title={hk('toggle_bullet_list')
        ? `Bullet list (${hk('toggle_bullet_list')})`
        : 'Bullet list'}
    >
      <span class="material-symbols-outlined" aria-hidden="true"
        >format_list_bulleted</span
      >
    </button>
    <button
      type="button"
      class="toolbar-btn"
      class:active={listActive('ordered')}
      aria-pressed={listActive('ordered')}
      aria-label="Numbered list"
      aria-keyshortcuts={hk('toggle_ordered_list') || undefined}
      data-tb
      data-primary
      disabled={!canToggleList()}
      tabindex={rovingIdx === ORDERED_LIST_IDX ? 0 : -1}
      onclick={handleOrderedList}
      onfocus={() => onTbFocus(ORDERED_LIST_IDX)}
      title={hk('toggle_ordered_list')
        ? `Numbered list (${hk('toggle_ordered_list')})`
        : 'Numbered list'}
    >
      <span class="material-symbols-outlined" aria-hidden="true"
        >format_list_numbered</span
      >
    </button>

    <div class="menu-wrap" bind:this={alignWrap}>
      <button
        type="button"
        class="toolbar-btn toolbar-menu-trigger"
        class:active={alignOpen}
        aria-label="Alignment"
        aria-haspopup="menu"
        aria-expanded={alignOpen}
        data-tb
        tabindex={rovingIdx === ALIGN_IDX ? 0 : -1}
        onclick={() => {
          alignOpen = !alignOpen
          moreOpen = false
          insertOpen = false
        }}
        onfocus={() => onTbFocus(ALIGN_IDX)}
        title="Alignment"
      >
        <span class="material-symbols-outlined" aria-hidden="true"
          >format_align_left</span
        >
        <span class="trigger-label">Align</span>
      </button>
      {#if alignOpen}
        <div class="toolbar-menu" role="menu" aria-label="Alignment">
          {#each ALIGN_BUTTONS as btn (btn.id)}
            <button
              type="button"
              class="menu-item"
              class:active={currentAlign() === btn.id}
              role="menuitemradio"
              aria-checked={currentAlign() === btn.id}
              aria-label={btn.label}
              title={hk(btn.hotkey)
                ? `${btn.label} (${hk(btn.hotkey)})`
                : btn.label}
              onclick={() => handleAlign(btn.id)}
            >
              <span class="material-symbols-outlined" aria-hidden="true"
                >{btn.icon}</span
              >
              <span>{btn.label}</span>
            </button>
          {/each}
        </div>
      {/if}
    </div>
  </div>

  <span class="toolbar-divider" aria-hidden="true"></span>

  <div class="toolbar-group" role="group" aria-label="Insert content">
    <div class="menu-wrap" bind:this={insertWrap}>
      <button
        type="button"
        class="toolbar-btn toolbar-menu-trigger"
        class:active={insertOpen}
        aria-label="Insert"
        aria-haspopup="menu"
        aria-expanded={insertOpen}
        data-tb
        tabindex={rovingIdx === INSERT_IDX ? 0 : -1}
        onclick={() => {
          insertOpen = !insertOpen
          moreOpen = false
          alignOpen = false
        }}
        onfocus={() => onTbFocus(INSERT_IDX)}
        title="Insert"
      >
        <span class="material-symbols-outlined" aria-hidden="true">add</span>
        <span class="trigger-label">Insert</span>
      </button>
      {#if insertOpen}
        <div class="toolbar-menu" role="menu" aria-label="Insert">
          {#each INSERT_BUTTONS as btn (btn.id)}
            <button
              type="button"
              class="menu-item"
              role="menuitem"
              aria-label={btn.label}
              onclick={() => {
                btn.run()
                insertOpen = false
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
    </div>
  </div>

  {#if colorEnabled}
    <span class="toolbar-divider" aria-hidden="true"></span>
    <div class="toolbar-group" role="group" aria-label="Colors">
      <ColorPickerMenu
        {editor}
        markType="textColor"
        {isDark}
        toolbarTabIndex={rovingIdx === COLOR_START ? 0 : -1}
        onToolbarFocus={() => onTbFocus(COLOR_START)}
        onMenuOpenChange={(open) => (colorTextOpen = open)}
      />
      <ColorPickerMenu
        {editor}
        markType="backgroundColor"
        {isDark}
        toolbarTabIndex={rovingIdx === COLOR_START + 1 ? 0 : -1}
        onToolbarFocus={() => onTbFocus(COLOR_START + 1)}
        onMenuOpenChange={(open) => (colorBgOpen = open)}
      />
    </div>
  {/if}

  <span class="toolbar-divider toolbar-divider-end" aria-hidden="true"></span>

  <div class="toolbar-group">
    <button
      type="button"
      class="toolbar-btn"
      aria-label="Clear formatting"
      data-tb
      data-clear
      tabindex={rovingIdx === clearIdx ? 0 : -1}
      onclick={handleClear}
      onfocus={() => onTbFocus(clearIdx)}
      title="Clear formatting"
    >
      <span class="material-symbols-outlined" aria-hidden="true"
        >format_clear</span
      >
    </button>
  </div>
</div>

<style>
  .format-toolbar {
    display: flex;
    align-items: center;
    gap: 4px;
    height: 100%;
    min-width: 0;
    max-width: 100%;
    flex-wrap: wrap;
    overflow: visible;
  }

  .toolbar-group {
    display: flex;
    align-items: center;
    gap: 2px;
    flex-shrink: 0;
  }

  .toolbar-overflow {
    gap: 4px;
  }

  .toolbar-divider {
    width: 1px;
    height: 20px;
    background: var(--color-surface-popover-border);
    margin: 0 4px;
    flex-shrink: 0;
  }

  .toolbar-divider-end {
    margin-left: auto;
  }

  .menu-wrap {
    position: relative;
    display: inline-flex;
  }

  .toolbar-btn {
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
    transition:
      background 0.1s,
      color 0.1s;
    flex-shrink: 0;
  }

  .toolbar-menu-trigger {
    width: auto;
    min-width: 32px;
    padding: 0 6px;
    gap: 2px;
  }

  .trigger-label {
    font-size: 0.72rem;
    font-weight: 500;
    line-height: 1;
  }

  /* At narrow widths hide text labels; icons + aria-labels remain. */
  @container (max-width: 600px) {
    .trigger-label {
      display: none;
    }
  }

  .toolbar-btn:hover:not(:disabled) {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 15%,
      transparent
    );
    color: var(--color-text-primary);
  }

  .toolbar-btn:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: -2px;
  }

  .toolbar-btn:disabled {
    opacity: 0.35;
    cursor: not-allowed;
  }

  .toolbar-btn.active {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-glow) 20%,
      transparent
    );
    color: var(--color-accent-primary-glow);
  }

  .toolbar-btn .material-symbols-outlined {
    font-size: 18px;
    font-variation-settings: 'wght' 400;
  }

  .toolbar-menu {
    position: absolute;
    top: calc(100% + 4px);
    left: 0;
    z-index: 60;
    min-width: 168px;
    max-width: min(240px, calc(100vw - 16px));
    padding: 4px;
    border-radius: 8px;
    background: var(--color-surface-popover);
    border: 1px solid var(--color-surface-popover-border);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .menu-item {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 32px;
    padding: 4px 10px;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--color-text-primary);
    font-size: 0.78rem;
    text-align: left;
    cursor: pointer;
  }

  .menu-item:hover:not(:disabled),
  .menu-item:focus-visible {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 15%,
      transparent
    );
    outline: none;
  }

  .menu-item:disabled {
    opacity: 0.35;
    cursor: not-allowed;
  }

  .menu-item.active {
    color: var(--color-accent-primary-glow);
  }

  .menu-item .material-symbols-outlined {
    font-size: 16px;
    color: var(--color-text-muted);
  }

  .menu-item.active .material-symbols-outlined {
    color: var(--color-accent-primary-glow);
  }

  .menu-sep {
    height: 1px;
    margin: 4px 6px;
    background: var(--color-surface-popover-border);
  }

  @media (prefers-reduced-motion: reduce) {
    .toolbar-btn {
      transition: none;
    }
  }

  /* Fallback when container queries are unavailable: hide labels under 600px viewport. */
  @media (max-width: 600px) {
    .trigger-label {
      display: none;
    }
  }
</style>
