<script lang="ts">
  import type { Editor } from 'svelte-tiptap'

  // FormatToolbar — the persistent top toolbar for inline text formatting (#168).
  // Cyber-Ink styled, 36px tall, sticky-positioned below the titlebar. Carries
  // the 8 inline mark buttons + link + clear-formatting. The H1▾ heading slot
  // is filled by HeadingLevelMenu in #169.
  //
  // Accessibility: role="toolbar", roving tabindex (first button tabindex=0,
  // rest -1), arrow-key navigation, Home/End jump, Esc returns focus to editor.
  // Each button has aria-label + aria-keyshortcuts + aria-pressed (reflecting
  // editor.isActive(mark)).

  interface Props {
    editor: Editor | null
    activeMarks: Set<string>
  }

  let { editor, activeMarks }: Props = $props()

  // Button definitions: id, label, material icon, hotkey, mark type.
  // 'link' and 'clear' are special-cased in onclick.
  interface FormatButton {
    id: string
    label: string
    icon: string
    shortcut: string
    mark: string
  }

  const BUTTONS: FormatButton[] = [
    { id: 'bold', label: 'Bold', icon: 'format_bold', shortcut: 'Ctrl+B', mark: 'bold' },
    { id: 'italic', label: 'Italic', icon: 'format_italic', shortcut: 'Ctrl+I', mark: 'italic' },
    { id: 'underline', label: 'Underline', icon: 'format_underlined', shortcut: 'Ctrl+U', mark: 'underline' },
    { id: 'strike', label: 'Strikethrough', icon: 'format_strikethrough', shortcut: 'Ctrl+Shift+X', mark: 'strike' },
    { id: 'code', label: 'Inline code', icon: 'code', shortcut: 'Ctrl+E', mark: 'code' },
    { id: 'highlight', label: 'Highlight', icon: 'highlight', shortcut: 'Ctrl+Shift+H', mark: 'highlight' },
    { id: 'subscript', label: 'Subscript', icon: 'subscript', shortcut: 'Ctrl,', mark: 'subscript' },
    { id: 'superscript', label: 'Superscript', icon: 'superscript', shortcut: 'Ctrl.', mark: 'superscript' }
  ]

  function handleClick(btn: FormatButton): void {
    if (!editor) return
    editor.chain().focus().toggleMark(btn.mark).run()
  }

  function handleLink(): void {
    if (!editor) return
    if (editor.isActive('link')) {
      editor.chain().focus().unsetLink().run()
    } else if (!editor.state.selection.empty) {
      const url = window.prompt('Enter URL:')
      if (url) editor.chain().focus().toggleLink({ href: url }).run()
    }
  }

  function handleClear(): void {
    if (!editor) return
    editor.chain().focus().unsetAllMarks().run()
  }

  function isActive(mark: string): boolean {
    return activeMarks.has(mark)
  }

  // --- Roving tabindex keyboard navigation ---
  const ALL_BUTTONS = [...BUTTONS, { id: 'link', special: true }, { id: 'clear', special: true }]
  let focusedIdx = $state(0)

  function handleKeydown(e: KeyboardEvent): void {
    const count = ALL_BUTTONS.length
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
      e.preventDefault()
      focusedIdx = (focusedIdx + 1) % count
      focusButton(focusedIdx)
    } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
      e.preventDefault()
      focusedIdx = (focusedIdx - 1 + count) % count
      focusButton(focusedIdx)
    } else if (e.key === 'Home') {
      e.preventDefault()
      focusedIdx = 0
      focusButton(0)
    } else if (e.key === 'End') {
      e.preventDefault()
      focusedIdx = count - 1
      focusButton(count - 1)
    } else if (e.key === 'Escape') {
      e.preventDefault()
      editor?.chain().focus().run()
    }
  }

  function focusButton(idx: number): void {
    const el = document.getElementById(`fmt-btn-${ALL_BUTTONS[idx].id}`)
    el?.focus()
  }
</script>

<div class="format-toolbar" role="toolbar" aria-label="Text formatting" tabindex="-1" onkeydown={handleKeydown}>
  <div class="toolbar-group">
    {#each BUTTONS as btn, i (btn.id)}
      <button
        id="fmt-btn-{btn.id}"
        type="button"
        class="toolbar-btn"
        class:active={isActive(btn.mark)}
        aria-pressed={isActive(btn.mark)}
        aria-label={btn.label}
        aria-keyshortcuts={btn.shortcut}
        tabindex={i === 0 ? 0 : -1}
        onclick={() => handleClick(btn)}
        onfocus={() => (focusedIdx = i)}
        title={btn.label}
      >
        <span class="material-symbols-outlined" aria-hidden="true">{btn.icon}</span>
      </button>
    {/each}

    <button
      id="fmt-btn-link"
      type="button"
      class="toolbar-btn"
      class:active={isActive('link')}
      aria-pressed={isActive('link')}
      aria-label="Insert link"
      aria-keyshortcuts="Ctrl+K"
      tabindex={-1}
      onclick={handleLink}
      onfocus={() => (focusedIdx = BUTTONS.length)}
      title="Insert link"
    >
      <span class="material-symbols-outlined" aria-hidden="true">link</span>
    </button>
  </div>

  <span class="toolbar-divider" aria-hidden="true"></span>

  <div class="toolbar-group">
    <button
      id="fmt-btn-clear"
      type="button"
      class="toolbar-btn"
      aria-label="Clear formatting"
      aria-keyshortcuts="Ctrl+\\"
      tabindex={-1}
      onclick={handleClear}
      onfocus={() => (focusedIdx = BUTTONS.length + 1)}
      title="Clear formatting"
    >
      <span class="material-symbols-outlined" aria-hidden="true">format_clear</span>
    </button>
  </div>
</div>

<style>
  .format-toolbar {
    display: flex;
    align-items: center;
    gap: 4px;
    height: 36px;
    padding: 0 8px;
    position: sticky;
    top: 0;
    z-index: 10;
    background: color-mix(in srgb, var(--color-surface, #1a1d24) 95%, transparent);
    backdrop-filter: blur(8px);
    border-bottom: 1px solid var(--color-border-muted, #2a2e36);
  }

  .toolbar-group {
    display: flex;
    align-items: center;
    gap: 2px;
  }

  .toolbar-divider {
    width: 1px;
    height: 20px;
    background: var(--color-border-muted, #2a2e36);
    margin: 0 4px;
  }

  .toolbar-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--color-text-muted, #8b95a3);
    cursor: pointer;
    transition: background 0.1s, color 0.1s;
  }

  .toolbar-btn:hover {
    background: color-mix(in srgb, var(--color-accent-primary-start, #4f7cff) 15%, transparent);
    color: var(--color-text-primary, #e6e6e6);
  }

  .toolbar-btn:focus-visible {
    outline: 2px solid var(--color-accent-primary-start, #4f7cff);
    outline-offset: -2px;
  }

  .toolbar-btn.active {
    background: color-mix(in srgb, var(--color-accent-primary-glow, #6fa3ff) 20%, transparent);
    color: var(--color-accent-primary-glow, #6fa3ff);
  }

  .toolbar-btn .material-symbols-outlined {
    font-size: 18px;
    font-variation-settings: 'wght' 400;
  }

  @media (prefers-reduced-motion: reduce) {
    .toolbar-btn {
      transition: none;
    }
  }
</style>
