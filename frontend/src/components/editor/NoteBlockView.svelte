<script lang="ts">
  import { NodeViewWrapper, NodeViewContent } from 'svelte-tiptap'
  import type { NodeViewProps } from '@tiptap/core'
  import {
    parseOrderedBullet,
    formatOrderedOutlineLabel
  } from '../../lib/editor/orderedList'

  // SvelteNodeViewRenderer auto-applies a `node-{type.name}` (camelCase) class
  // to the wrapper, so we don't redeclare it here (#179).
  let { node, editor, getPos }: NodeViewProps = $props()
  let isEmpty = $derived(!node.content.size || node.textContent.trim() === '')
  let align = $derived(node.attrs.align || 'left')
  let bullet = $derived(node.attrs.bullet || '')
  let quote = $derived(node.attrs.quote || '')
  let depth = $derived(node.attrs.depth || 0)

  // Outline labels depend on sibling run position, not only this node's bullet.
  // TipTap does not re-prop sibling NodeViews when another block renumbers, so
  // bump an epoch on every transaction to recompute display labels (#837).
  let docEpoch = $state(0)
  $effect(() => {
    if (!editor || editor.isDestroyed) return
    const bump = (): void => {
      docEpoch++
    }
    editor.on('transaction', bump)
    return () => {
      editor.off('transaction', bump)
    }
  })

  // Hierarchical outline label for ordered items (1.1, 1.2.3) — display only;
  // on-disk bullet stays GFM-simple (#837).
  let markerLabel = $derived.by(() => {
    void docEpoch
    const b = bullet || ''
    if (!parseOrderedBullet(b)) return b.trim()
    try {
      const pos = typeof getPos === 'function' ? getPos() : null
      if (typeof pos !== 'number' || !editor || editor.isDestroyed) {
        return b.trim()
      }
      return formatOrderedOutlineLabel(editor.state.doc, pos) ?? b.trim()
    } catch {
      return b.trim()
    }
  })
</script>

<NodeViewWrapper
  class="group flex items-start gap-3 py-1 min-h-8{bullet && !quote
    ? ' silt-list-item'
    : ''}"
  data-align={align}
  data-depth={depth}
  data-id={node.attrs.id}
  data-bullet={bullet || undefined}
>
  <span
    class="silt-drag-handle-inline material-symbols-outlined text-text-muted hover:text-primary transition-colors duration-150 mt-0.5 select-none text-icon-lg opacity-0 group-hover:opacity-100"
    class:group-hover:opacity-100={!isEmpty}
    spellcheck="false"
    draggable="true"
    aria-hidden="true"
    title="Drag to move block (Alt+Up/Down to move by keyboard)"
    data-drag-handle
  >
    drag_indicator
  </span>

  {#if bullet && bullet !== ''}
    {#if /^\d+/.test(bullet)}
      <!-- Numbered marker (outline label when nested) -->
      <span
        class="silt-ordered-marker text-text-muted/70 text-icon-sm leading-[22px] select-none font-mono min-w-[1.125rem] text-right tabular-nums"
        aria-hidden="true"
      >
        {markerLabel}
      </span>
    {:else}
      <!-- Bullet dot -->
      <div
        class="w-1.5 h-1.5 rounded-full bg-text-muted/50 mt-2.5 flex-shrink-0 select-none"
        aria-hidden="true"
      ></div>
    {/if}
  {/if}

  <!-- A quote renders as a native <blockquote> (implicit semantics: paragraph
       grouping, distinct SR announcement) rather than a synthetic role on a
       div. No aria-label is set on purpose — it would override the screen
       reader reading the quote's own content. -->
  <svelte:element
    this={quote ? 'blockquote' : 'div'}
    class="flex-1 min-w-0"
    class:silt-quote={!!quote}
    data-quote={quote || undefined}
    style="text-align: {align}"
  >
    <NodeViewContent
      class="whitespace-pre-wrap break-words min-h-5.5 focus:outline-none silt-note-content"
    />
  </svelte:element>
</NodeViewWrapper>
