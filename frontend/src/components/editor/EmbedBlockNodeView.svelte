<script lang="ts">
  // Default NodeView for the generic embedBlock node (#110). When `openable`
  // is true (e.g. attachments), the card is fully interactive: click or
  // Enter/Space opens the file in the OS native handler; the delete button
  // removes the block; the drag handle is keyboard-accessible (#101).
  import { NodeViewWrapper } from 'svelte-tiptap'
  import type { NodeViewProps } from '@tiptap/core'
  import { OpenAttachment } from '../../../wailsjs/go/main/App.js'

  let { node, deleteNode, selected }: NodeViewProps = $props()
  const attrs = $derived(node.attrs as Record<string, any>)
  const cardClass = $derived(
    selected
      ? 'border-accent-primary-start/60 bg-accent-primary-glow'
      : 'border-border-muted bg-bg-surface/60'
  )
  const tabIndex = $derived(attrs.openable ? 0 : undefined)

  async function open() {
    if (!attrs.openable || !attrs.src) return
    try {
      await OpenAttachment('', attrs.src)
    } catch (e) {
      console.error('[embed-block] open failed:', e)
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (attrs.openable && (e.key === 'Enter' || e.key === ' ')) {
      e.preventDefault()
      open()
    }
  }
</script>

<NodeViewWrapper>
  <div
    class="embed-block-default my-2 p-3 rounded-lg border transition-colors flex items-center gap-3 {cardClass}"
    role={attrs.openable ? 'button' : 'img'}
    tabindex={tabIndex}
    aria-label={attrs.caption || `${attrs.embedType}: ${attrs.src}`}
    data-embed-type={attrs.embedType}
    data-openable={attrs.openable ? 'true' : 'false'}
    onclick={attrs.openable ? open : undefined}
    onkeydown={attrs.openable ? handleKeydown : undefined}
  >
    <span
      class="material-symbols-outlined text-accent-primary-start/70 text-[28px]"
    >
      {attrs.embedType === 'image'
        ? 'image'
        : attrs.embedType === 'attachment'
          ? 'attach_file'
          : 'extension'}
    </span>
    <div class="flex-1 min-w-0">
      <div class="text-text-primary text-[13px] font-body-md truncate">
        {attrs.caption || attrs.src || attrs.embedType}
      </div>
      {#if attrs.src}
        <div class="text-text-muted text-[10px] font-label-sm truncate">
          {attrs.src}
        </div>
      {/if}
    </div>
    {#if attrs.pluginID}
      <span
        class="text-[9px] text-text-muted uppercase tracking-wider border border-border-muted rounded px-1.5 py-0.5"
      >
        {attrs.pluginID}
      </span>
    {/if}
    <button
      type="button"
      onclick={(e) => {
        e.stopPropagation()
        deleteNode()
      }}
      title="Remove block"
      aria-label="Remove block"
      class="text-text-muted hover:text-status-danger border-none bg-transparent cursor-pointer p-1 rounded transition-colors"
    >
      <span class="material-symbols-outlined text-[18px]">delete</span>
    </button>
    <span
      class="material-symbols-outlined text-text-muted text-[16px] cursor-grab active:cursor-grabbing"
      title="Drag to reorder"
      aria-label="Drag handle"
      data-drag-handle
    >
      drag_indicator
    </span>
  </div>
</NodeViewWrapper>
