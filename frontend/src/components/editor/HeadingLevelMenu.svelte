<script lang="ts">
  import type { Editor } from 'svelte-tiptap'
  import { findActiveBlock } from '../../lib/editor/keymaps'

  interface Props {
    editor: Editor | null
    /** Roving tabindex from the parent format toolbar (#690). */
    toolbarTabIndex?: number
    onToolbarFocus?: () => void
    /** Notifies parent when the dropdown opens/closes (toolbar arrow roving). */
    onMenuOpenChange?: (open: boolean) => void
  }

  let {
    editor,
    toolbarTabIndex = 0,
    onToolbarFocus,
    onMenuOpenChange
  }: Props = $props()

  let menuOpen = $state(false)

  $effect(() => {
    onMenuOpenChange?.(menuOpen)
  })
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

  type Option = {
    id: string
    label: string
    type: string
    depth?: number
    icon: string
  }

  const OPTIONS: Option[] = [
    {
      id: 'h1',
      label: 'Heading 1',
      type: 'headerBlock',
      depth: 1,
      icon: 'title'
    },
    {
      id: 'h2',
      label: 'Heading 2',
      type: 'headerBlock',
      depth: 2,
      icon: 'title'
    },
    {
      id: 'h3',
      label: 'Heading 3',
      type: 'headerBlock',
      depth: 3,
      icon: 'title'
    },
    {
      id: 'h4',
      label: 'Heading 4',
      type: 'headerBlock',
      depth: 4,
      icon: 'title'
    },
    {
      id: 'h5',
      label: 'Heading 5',
      type: 'headerBlock',
      depth: 5,
      icon: 'title'
    },
    {
      id: 'h6',
      label: 'Heading 6',
      type: 'headerBlock',
      depth: 6,
      icon: 'title'
    },
    { id: 'note', label: 'Text', type: 'noteBlock', icon: 'notes' },
    { id: 'task', label: 'Task', type: 'taskBlock', icon: 'check_box' }
  ]

  function currentLabel(): string {
    if (!editor) return 'Note'
    const active = findActiveBlock(editor)
    if (!active) return 'Note'
    const node = active.node
    if (node.type.name === 'headerBlock') {
      const depth = node.attrs.depth || 1
      return `H${depth}`
    }
    if (node.type.name === 'taskBlock') return 'Task'
    return 'Note'
  }

  function isCurrent(opt: Option): boolean {
    if (!editor) return false
    const active = findActiveBlock(editor)
    if (!active) return false
    const node = active.node
    if (opt.type === 'headerBlock' && node.type.name === 'headerBlock') {
      return (node.attrs.depth || 1) === (opt.depth || 1)
    }
    return opt.type === node.type.name
  }

  function select(opt: Option): void {
    if (!editor) return
    window.dispatchEvent(
      new CustomEvent('silt:change-block-type', { detail: opt })
    )
    menuOpen = false
  }

  let label = $derived(currentLabel())
</script>

<div class="heading-menu-wrapper" bind:this={wrapperEl}>
  <button
    type="button"
    class="heading-trigger"
    bind:this={triggerEl}
    aria-expanded={menuOpen}
    aria-haspopup="menu"
    aria-label="Block type"
    data-tb
    tabindex={toolbarTabIndex}
    onclick={() => (menuOpen = !menuOpen)}
    onfocus={() => onToolbarFocus?.()}
  >
    {label}
    <span class="material-symbols-outlined chevron" aria-hidden="true"
      >expand_more</span
    >
  </button>

  {#if menuOpen}
    <div class="heading-menu" role="menu" aria-label="Block type">
      {#each OPTIONS as opt (opt.id)}
        <button
          type="button"
          class="menu-item"
          class:active={isCurrent(opt)}
          role="menuitemradio"
          aria-checked={isCurrent(opt)}
          onclick={() => select(opt)}
        >
          <span class="material-symbols-outlined" aria-hidden="true"
            >{opt.icon}</span
          >
          <span>{opt.label}</span>
          {#if isCurrent(opt)}
            <span class="material-symbols-outlined check" aria-hidden="true"
              >check</span
            >
          {/if}
        </button>
      {/each}
    </div>
  {/if}
</div>

<style>
  .heading-menu-wrapper {
    position: relative;
    display: inline-flex;
  }

  .heading-trigger {
    display: flex;
    align-items: center;
    gap: 2px;
    height: 28px;
    padding: 0 6px;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--color-text-muted);
    font-size: 0.78rem;
    cursor: pointer;
    transition:
      background 0.1s,
      color 0.1s;
  }

  .heading-trigger:hover {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 15%,
      transparent
    );
    color: var(--color-text-primary);
  }

  .chevron {
    font-size: 16px;
  }

  .heading-menu {
    position: absolute;
    top: 100%;
    left: 0;
    z-index: 50;
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

  .menu-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 10px;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--color-text-primary);
    font-size: 0.78rem;
    text-align: left;
    cursor: pointer;
  }

  .menu-item:hover {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 15%,
      transparent
    );
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

  .check {
    margin-left: auto;
  }
</style>
