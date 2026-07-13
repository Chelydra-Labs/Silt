<script lang="ts">
  // NodeView wrapper for PageLinkNode (#545). Thin pass-through to the
  // PageLinkChip; the chip handles resolution + navigation on click.
  import { NodeViewWrapper } from 'svelte-tiptap'
  import type { NodeViewProps } from '@tiptap/core'
  import PageLinkChip from '../PageLinkChip.svelte'

  let { node }: NodeViewProps = $props()
</script>

<NodeViewWrapper>
  <PageLinkChip
    target={node.attrs.target}
    heading={node.attrs.heading}
    alias={node.attrs.alias}
  />
</NodeViewWrapper>
