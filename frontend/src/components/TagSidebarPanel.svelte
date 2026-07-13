<script lang="ts">
  import { onMount, untrack } from 'svelte'
  import { QueryTagHierarchy } from '../../bindings/silt/app.js'
  import TagTreeNode from './TagTreeNode.svelte'

  interface TagNode {
    name: string
    path: string
    count: number
    children: TagNode[]
  }

  interface Props {
    selectedTag: string
  }

  let { selectedTag = $bindable() }: Props = $props()

  let tree = $state<TagNode[]>([])
  let expanded = $state<Set<string>>(new Set())
  let query = $state('')

  let filteredTree = $derived.by(() => {
    if (!query.trim()) return tree
    const q = query.toLowerCase()
    const filter = (nodes: TagNode[]): TagNode[] => {
      const out: TagNode[] = []
      for (const n of nodes) {
        const kids = filter(n.children)
        if (
          n.name.toLowerCase().includes(q) ||
          n.path.toLowerCase().includes(q) ||
          kids.length > 0
        ) {
          out.push({ ...n, children: kids })
        }
      }
      return out
    }
    return filter(tree)
  })

  async function loadTree() {
    try {
      tree = (await QueryTagHierarchy()) || []
    } catch (e) {
      console.error('QueryTagHierarchy failed:', e)
      tree = []
    }
  }

  // Bind key methods for tags
  function toggle(path: string) {
    const next = new Set(expanded)
    if (next.has(path)) next.delete(path)
    else next.add(path)
    expanded = next
  }

  function selectTag(path: string) {
    selectedTag = path
  }

  onMount(() => {
    loadTree()
    const refresh = () => loadTree()
    window.addEventListener('refresh-navigation', refresh)
    return () => window.removeEventListener('refresh-navigation', refresh)
  })

  // Synchronize expanded status when selectedTag updates externally (e.g. from editor)
  $effect(() => {
    const tag = selectedTag
    if (!tag) return
    const parts = tag.split('/')
    const acc: string[] = []
    const next = new Set(untrack(() => expanded))
    for (const part of parts) {
      acc.push(part)
      next.add(acc.join('/'))
    }
    expanded = next
  })
</script>

<div class="flex-grow flex flex-col min-h-0 bg-surface-sidebar">
  <div class="px-3 py-3 border-b border-surface-sidebar-border flex-shrink-0">
    <div class="flex items-center gap-2 mb-2">
      <span
        class="material-symbols-outlined text-accent-primary-start text-type-2xl"
        >label</span
      >
      <h2 class="font-headline-md text-headline-md text-text-primary">Tags</h2>
    </div>
    <div class="relative w-full">
      <input
        bind:value={query}
        type="text"
        placeholder="Filter tags…"
        class="w-full bg-surface-sidebar border border-surface-sidebar-border rounded-lg pl-3 pr-8 py-1.5 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start transition-colors"
      />
      {#if query}
        <button
          type="button"
          aria-label="Clear filter"
          onclick={() => {
            query = ''
          }}
          class="absolute right-2.5 top-1/2 -translate-y-1/2 p-0.5 rounded hover:bg-hover text-text-muted hover:text-text-primary border-none bg-transparent cursor-pointer flex items-center justify-center focus:outline-none"
        >
          <span class="material-symbols-outlined text-icon-md">close</span>
        </button>
      {/if}
    </div>
  </div>
  <div class="flex-grow overflow-y-auto custom-scrollbar p-2">
    {#if filteredTree.length === 0}
      <div class="text-text-muted text-center py-10 font-body-md text-type-md">
        {#if tree.length === 0}
          <div class="flex flex-col items-center gap-2 px-4 py-6 select-none">
            <span
              class="material-symbols-outlined text-text-muted/60 text-icon-2xl"
              >label_off</span
            >
            <p class="font-medium text-text-primary">No tags yet</p>
            <p class="text-type-xs text-text-muted leading-relaxed">
              Type <code
                class="px-1.5 py-0.5 rounded bg-surface-sidebar-text/10 text-accent-secondary-start font-mono text-type-2xs"
                >#project</code
              >
              or
              <code
                class="px-1.5 py-0.5 rounded bg-surface-sidebar-text/10 text-accent-secondary-start font-mono text-type-2xs"
                >#todo/urgent</code
              > in any note to create tags.
            </p>
          </div>
        {:else}
          No tags match "{query}".
        {/if}
      </div>
    {:else}
      {#each filteredTree as node (node.path)}
        <TagTreeNode
          {node}
          depth={0}
          {expanded}
          activeTag={selectedTag}
          onToggle={toggle}
          onSelect={selectTag}
        />
      {/each}
    {/if}
  </div>
</div>
