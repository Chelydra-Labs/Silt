<script lang="ts">
  import { SvelteSet } from 'svelte/reactivity'
  import type { Editor } from 'svelte-tiptap'
  import {
    extractHeadingsFromEditor,
    jumpToHeading,
    activeHeadingId,
    type OutlineHeading
  } from '../../lib/editor/outline'

  interface Props {
    editor: Editor | null
    /** Scroll container for scroll-spy (note scroller, not window). */
    scrollParent?: HTMLElement | null
    open?: boolean
    onToggle?: () => void
  }

  let {
    editor = null,
    scrollParent = null,
    open = true,
    onToggle
  }: Props = $props()

  let headings = $state<OutlineHeading[]>([])
  let activeId = $state<string | null>(null)
  /** Heading ids whose subtree is collapsed. */
  // eslint-disable-next-line svelte/no-unnecessary-state-wrap -- whole-set reassignment
  let collapsedIds = $state(new SvelteSet<string>())

  function refresh() {
    headings = extractHeadingsFromEditor(editor)
    activeId = activeHeadingId(headings, scrollParent ?? null)
  }

  $effect(() => {
    if (!editor || editor.isDestroyed) {
      headings = []
      return
    }
    refresh()
    const onUpdate = () => {
      // Debounce via rAF so rapid typing doesn't thrash.
      requestAnimationFrame(refresh)
    }
    editor.on('update', onUpdate)
    editor.on('selectionUpdate', onUpdate)
    return () => {
      editor.off('update', onUpdate)
      editor.off('selectionUpdate', onUpdate)
    }
  })

  $effect(() => {
    const el = scrollParent
    if (!el) return
    const onScroll = () => {
      activeId = activeHeadingId(headings, el)
    }
    el.addEventListener('scroll', onScroll, { passive: true })
    return () => el.removeEventListener('scroll', onScroll)
  })

  function isVisible(item: OutlineHeading, index: number): boolean {
    // Hide if any ancestor heading id is collapsed (walk parent chain only).
    let depth = item.depth
    for (let i = index - 1; i >= 0; i--) {
      const prev = headings[i]
      if (!prev) continue
      if (prev.depth < depth) {
        if (collapsedIds.has(prev.id)) return false
        depth = prev.depth
      }
    }
    return true
  }

  function hasChildren(index: number): boolean {
    const item = headings[index]
    if (!item) return false
    const next = headings[index + 1]
    return !!next && next.depth > item.depth
  }

  function toggleCollapse(id: string) {
    const next = new SvelteSet(collapsedIds)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    collapsedIds = next
  }

  function onClick(item: OutlineHeading) {
    if (!editor) return
    jumpToHeading(editor, item.pos)
    activeId = item.id
  }
</script>

<aside
  class="outline-panel flex flex-col border-l border-surface-panel-border bg-surface-panel/40 min-w-[11rem] max-w-[16rem]"
  aria-label="Document outline"
  data-testid="outline-panel"
>
  <div
    class="flex items-center justify-between gap-1 px-2 py-1.5 border-b border-surface-panel-border"
  >
    <h2
      class="text-type-xs font-label-sm-bold uppercase tracking-wide text-text-muted m-0"
    >
      Outline
    </h2>
    {#if onToggle}
      <button
        type="button"
        class="p-0.5 rounded border-none bg-transparent text-text-muted hover:text-text-primary cursor-pointer"
        aria-label={open ? 'Hide outline' : 'Show outline'}
        aria-expanded={open}
        onclick={onToggle}
      >
        <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
          >{open ? 'right_panel_close' : 'right_panel_open'}</span
        >
      </button>
    {/if}
  </div>
  {#if open}
    <nav class="flex-1 overflow-y-auto py-1 px-1" aria-label="Headings">
      {#if headings.length === 0}
        <p class="px-2 py-3 text-type-xs text-text-muted m-0">
          No headings yet. Type # then space for a heading.
        </p>
      {:else}
        <ul class="list-none m-0 p-0">
          {#each headings as item, i (item.id + '-' + i)}
            {#if isVisible(item, i)}
              <li class="m-0 p-0">
                <div
                  class="flex items-center gap-0.5"
                  style="padding-left: {(item.depth - 1) * 0.65}rem"
                >
                  {#if hasChildren(i)}
                    <button
                      type="button"
                      class="shrink-0 p-0 w-5 h-5 flex items-center justify-center border-none bg-transparent text-text-muted cursor-pointer rounded hover:bg-hover"
                      aria-label={collapsedIds.has(item.id)
                        ? 'Expand'
                        : 'Collapse'}
                      aria-expanded={!collapsedIds.has(item.id)}
                      onclick={() => toggleCollapse(item.id)}
                    >
                      <span
                        class="material-symbols-outlined text-icon-sm"
                        aria-hidden="true"
                        >{collapsedIds.has(item.id)
                          ? 'chevron_right'
                          : 'expand_more'}</span
                      >
                    </button>
                  {:else}
                    <span class="w-5 shrink-0" aria-hidden="true"></span>
                  {/if}
                  <button
                    type="button"
                    class="flex-1 min-w-0 text-left truncate px-1.5 py-1 rounded border-none cursor-pointer text-type-xs font-body-md transition-colors {activeId ===
                    item.id
                      ? 'bg-active text-text-primary'
                      : 'bg-transparent text-text-muted hover:bg-hover hover:text-text-primary'}"
                    aria-current={activeId === item.id ? 'location' : undefined}
                    onclick={() => onClick(item)}
                    title={item.text}
                  >
                    {item.text}
                  </button>
                </div>
              </li>
            {/if}
          {/each}
        </ul>
      {/if}
    </nav>
  {/if}
</aside>
