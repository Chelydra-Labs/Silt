<script lang="ts">
  import type { Editor } from 'svelte-tiptap'

  // SelectionBubble — a floating popover above non-collapsed text selection
  // (#168 / #643). Shows the same format buttons as the toolbar in a tighter
  // layout. Auto-dismisses on Esc, click outside, or selection collapse.
  // role="menu" with roving tabindex + arrow-key navigation (WAI-ARIA menu).

  interface Props {
    editor: Editor | null
    activeMarks: Set<string>
    selectionEmpty: boolean
    selectionCoords: { left: number; top: number; bottom: number } | null
  }

  let { editor, activeMarks, selectionEmpty, selectionCoords }: Props = $props()

  let show = $derived(!selectionEmpty && selectionCoords !== null)
  let focusIdx = $state(0)
  let menuEl = $state<HTMLDivElement | null>(null)

  const QUICK_BUTTONS = [
    { id: 'bold', icon: 'format_bold', label: 'Bold', mark: 'bold' },
    { id: 'italic', icon: 'format_italic', label: 'Italic', mark: 'italic' },
    {
      id: 'strike',
      icon: 'format_strikethrough',
      label: 'Strikethrough',
      mark: 'strike'
    },
    { id: 'code', icon: 'code', label: 'Code', mark: 'code' },
    {
      id: 'highlight',
      icon: 'highlight',
      label: 'Highlight',
      mark: 'highlight'
    },
    {
      id: 'underline',
      icon: 'format_underlined',
      label: 'Underline',
      mark: 'underline'
    },
    { id: 'link', icon: 'link', label: 'Link', mark: 'link' }
  ]

  function handleAction(id: string, mark: string): void {
    if (!editor) return
    if (id === 'link') {
      if (editor.isActive('link')) {
        editor.chain().focus().unsetLink().run()
      } else {
        window.dispatchEvent(new CustomEvent('silt:open-link-input'))
      }
    } else {
      editor.chain().focus().toggleMark(mark).run()
    }
  }

  function focusButton(idx: number): void {
    const n = QUICK_BUTTONS.length
    const next = ((idx % n) + n) % n
    focusIdx = next
    // DOM focus after Svelte applies the new tabindex.
    queueMicrotask(() => {
      const btn = menuEl?.querySelectorAll<HTMLElement>(
        '[role="menuitemcheckbox"]'
      )[next]
      btn?.focus()
    })
  }

  function handleKeydown(e: KeyboardEvent): void {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      // Restore editor focus; selection collapse is owned by the editor.
      editor?.chain().focus().run()
      return
    }
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
      e.preventDefault()
      focusButton(focusIdx + 1)
      return
    }
    if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
      e.preventDefault()
      focusButton(focusIdx - 1)
      return
    }
    if (e.key === 'Home') {
      e.preventDefault()
      focusButton(0)
      return
    }
    if (e.key === 'End') {
      e.preventDefault()
      focusButton(QUICK_BUTTONS.length - 1)
      return
    }
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      const btn = QUICK_BUTTONS[focusIdx]
      if (btn) handleAction(btn.id, btn.mark)
    }
  }

  // When the bubble appears, move focus to the first action so keyboard users
  // can operate it without a mouse (#643).
  $effect(() => {
    if (show) {
      focusButton(0)
    }
  })
</script>

{#if show && selectionCoords}
  <!-- tabindex=-1 so the menu container can receive keydown when a child is focused (bubbles). -->
  <div
    bind:this={menuEl}
    class="selection-bubble"
    role="menu"
    tabindex="-1"
    aria-label="Format selection"
    aria-orientation="horizontal"
    style="left: {selectionCoords.left}px; top: {selectionCoords.top - 8}px"
    onkeydown={handleKeydown}
  >
    {#each QUICK_BUTTONS as btn, i (btn.id)}
      <button
        type="button"
        class="bubble-btn"
        class:active={activeMarks.has(btn.mark)}
        aria-checked={activeMarks.has(btn.mark)}
        aria-label={btn.label}
        role="menuitemcheckbox"
        tabindex={i === focusIdx ? 0 : -1}
        onclick={() => {
          focusIdx = i
          handleAction(btn.id, btn.mark)
        }}
      >
        <span class="material-symbols-outlined" aria-hidden="true"
          >{btn.icon}</span
        >
      </button>
    {/each}
  </div>
{/if}

<style>
  .selection-bubble {
    position: fixed;
    z-index: 100;
    transform: translate(-50%, -100%);
    display: flex;
    align-items: center;
    gap: 1px;
    padding: 3px;
    border-radius: 8px;
    background: var(--color-surface-popover);
    border: 1px solid var(--color-surface-popover-border);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
  }

  .bubble-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 26px;
    height: 26px;
    border: none;
    border-radius: 5px;
    background: transparent;
    color: var(--color-text-muted);
    cursor: pointer;
  }

  .bubble-btn:hover,
  .bubble-btn:focus-visible {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 20%,
      transparent
    );
    color: var(--color-text-primary);
    outline: none;
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
    font-size: 16px;
  }
</style>
