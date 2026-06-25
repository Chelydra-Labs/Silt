<script lang="ts">
  import { onMount } from 'svelte'
  import { QueryBlocksByTag } from '../../wailsjs/go/main/App.js'

  interface Props {
    selectedTag?: string
  }

  let { selectedTag = '' }: Props = $props()

  let activeTag = $state('')
  let results = $state<any[]>([])
  let loadingResults = $state(false)

  async function selectTag(path: string) {
    activeTag = path
    loadingResults = true
    try {
      results = await QueryBlocksByTag(path)
    } catch (e) {
      console.error('QueryBlocksByTag failed:', e)
      results = []
    } finally {
      loadingResults = false
    }
  }

  function openBlock(res: any) {
    window.dispatchEvent(
      new CustomEvent('navigate-to-block', {
        detail: {
          notebook: res.notebook,
          section: res.section,
          page: res.page,
          date: res.file_date,
          blockId: res.id
        }
      })
    )
  }

  // When selectedTag updates from props, load the blocks
  $effect(() => {
    const tag = selectedTag
    if (tag) {
      void selectTag(tag)
    }
  })
</script>

<div class="flex-1 flex flex-col min-h-0 bg-void">
  <div class="px-6 py-3 border-b border-border-muted flex items-center gap-2">
    {#if activeTag}
      <span class="material-symbols-outlined text-accent-secondary-start"
        >label</span
      >
      <span class="text-accent-secondary-start font-label-sm-bold"
        >#{activeTag}</span
      >
      <span class="text-text-muted text-[12px]"
        >· {results.length} block{results.length === 1 ? '' : 's'}</span
      >
    {:else}
      <span class="text-text-muted font-body-md"
        >Select a tag in the sidebar to see its blocks.</span
      >
    {/if}
  </div>
  <div class="flex-1 overflow-y-auto custom-scrollbar">
    {#if !activeTag}
      <div class="text-text-muted text-center py-16 font-body-md">
        Pick a tag in the sidebar.
      </div>
    {:else}
      {#if loadingResults}
        <div class="text-text-muted text-center py-10 animate-pulse">
          Loading…
        </div>
      {:else}
        {#if results.length === 0}
          <div class="text-text-muted text-center py-10 font-body-md">
            No blocks tagged.
          </div>
        {:else}
          {#each results as res (res.id)}
            <button
              onclick={() => openBlock(res)}
              class="w-full text-left px-6 py-3 border-b border-border-muted/50 hover:bg-hover transition-colors border-none bg-transparent cursor-pointer flex flex-col gap-1"
            >
              <div
                class="flex items-center gap-1.5 text-[10px] text-text-muted uppercase tracking-widest font-label-sm-bold"
              >
                <span>{res.notebook}</span>
                <span class="material-symbols-outlined text-[10px]"
                  >chevron_right</span
                >
                <span>{res.section}</span>
                <span class="material-symbols-outlined text-[10px]"
                  >chevron_right</span
                >
                <span>{res.page}</span>
              </div>
              <div class="font-body-md text-sm text-text-primary">
                {res.clean_content}
              </div>
            </button>
          {/each}
        {/if}
      {/if}
    {/if}
  </div>
</div>
