<script lang="ts">
  import { NodeViewWrapper, NodeViewContent } from 'svelte-tiptap'
  import type { NodeViewProps } from '@tiptap/core'

  let { node }: NodeViewProps = $props()
  let level = $derived(node.attrs.depth || 1)
</script>

<NodeViewWrapper class="header-block flex items-start gap-3 py-1">
  <span
    class="material-symbols-outlined text-text-muted/30 hover:text-primary transition-colors cursor-move mt-0.5 select-none text-[18px]"
    data-drag-handle
  >
    drag_indicator
  </span>
  <div
    class="flex-1 min-w-0 font-bold"
    class:text-headline-lg={level === 1}
    class:text-headline-md={level === 2}
    class:text-headline-sm={level >= 3}
  >
    <NodeViewContent class="focus:outline-none" />
  </div>
</NodeViewWrapper>
