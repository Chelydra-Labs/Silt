<script lang="ts">
  import { NodeViewWrapper, NodeViewContent } from 'svelte-tiptap'
  import type { NodeViewProps } from '@tiptap/core'

  let { node, updateAttributes, editor }: NodeViewProps = $props()
  let summary = $derived(node.attrs.summary || '')
  let isOpen = $derived(node.attrs.open ? true : false)

  function toggleOpen(): void {
    updateAttributes({ open: !isOpen })
    editor.commands.focus()
  }
</script>

<NodeViewWrapper
  class="my-2 rounded-lg border border-border overflow-hidden"
  data-type="silt-details"
  role="group"
  aria-label={summary || 'Foldable section'}
>
  <button
    class="flex items-center gap-2 w-full px-3 py-2 bg-bg-interface/50 hover:bg-bg-interface transition-colors text-left cursor-pointer select-none text-sm font-medium border-b border-border"
    onclick={toggleOpen}
    aria-expanded={isOpen}
  >
    <span
      class="material-symbols-outlined text-[18px] text-text-muted transition-transform duration-150"
      aria-hidden="true"
    >
      {isOpen ? 'expand_more' : 'chevron_right'}
    </span>
    {#if summary}
      <span class="text-text">{summary}</span>
    {:else}
      <span class="text-text-muted italic">Details</span>
    {/if}
  </button>
  {#if isOpen}
    <div class="px-4 py-3">
      <NodeViewContent
        class="whitespace-pre-wrap break-words min-h-[22px] focus:outline-none"
      />
    </div>
  {/if}
</NodeViewWrapper>
