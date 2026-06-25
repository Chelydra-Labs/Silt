<script lang="ts">
  import { onMount, untrack } from 'svelte'
  import { QueryTagHierarchy } from '../../wailsjs/go/main/App.js'
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

<div class="flex-grow flex flex-col min-h-0 bg-surface">
  <div class="px-3 py-3 border-b border-border-muted flex-shrink-0">
    <div class="flex items-center gap-2 mb-2">
      <span
        class="material-symbols-outlined text-accent-primary-start text-[20px]"
        >label</span
      >
      <h2 class="font-headline-md text-headline-md text-text-primary">Tags</h2>
    </div>
    <input
      bind:value={query}
      type="text"
      placeholder="Filter tags…"
      class="w-full bg-surface border border-border-zinc rounded-lg px-3 py-1.5 text-text-primary text-[13px] font-body-md outline-none focus:border-accent-primary-start transition-colors"
    />
  </div>
  <div class="flex-grow overflow-y-auto custom-scrollbar p-2">
    {#if filteredTree.length === 0}
      <div class="text-text-muted text-center py-10 font-body-md text-[13px]">
        {#if tree.length === 0}
          No tags yet. Add <span class="text-accent-secondary-start"
            >#tag/path</span
          > to a block.
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
