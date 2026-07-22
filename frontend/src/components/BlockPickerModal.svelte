<script lang="ts">
  import { onMount } from 'svelte'
  import { SearchBlocks } from '../../bindings/silt/app.js'

  interface Props {
    onPick: (blockId: string) => void
    onClose: () => void
  }

  let { onPick, onClose }: Props = $props()

  type BlockSearchResult = {
    id: string
    notebook?: string
    section?: string
    page?: string
    clean_content?: string
  }

  let query = $state('')
  let results = $state<BlockSearchResult[]>([])
  let selectedIdx = $state(0)
  let loading = $state(false)
  let inputEl = $state<HTMLInputElement | null>(null)

  let debounceTimer: ReturnType<typeof setTimeout> | null = null

  async function runSearch() {
    if (query.trim() === '') {
      results = []
      return
    }
    loading = true
    try {
      results = (await SearchBlocks(query)) as BlockSearchResult[]
      selectedIdx = 0
    } catch (e) {
      console.error('BlockPicker search failed:', e)
      results = []
    } finally {
      loading = false
    }
  }

  function onInput() {
    if (debounceTimer) clearTimeout(debounceTimer)
    debounceTimer = setTimeout(runSearch, 180)
  }

  let dialogEl = $state<HTMLDivElement | null>(null)
  let previouslyFocused: HTMLElement | null = null

  const FOCUSABLE =
    'button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])'

  function focusableEls(): HTMLElement[] {
    if (!dialogEl) return []
    return Array.from(dialogEl.querySelectorAll<HTMLElement>(FOCUSABLE))
  }

  function pick(res: BlockSearchResult) {
    onPick(res.id)
    onClose()
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      onClose()
      return
    }
    // Dialog-scoped trap (same pattern as ConfirmDialog): runs on window
    // capture so Tab still wraps when focus is on clear/result buttons, not
    // only when the search input is focused.
    if (e.key === 'Tab' && dialogEl) {
      const els = focusableEls()
      if (els.length === 0) return
      const first = els[0]
      const last = els[els.length - 1]
      const active = document.activeElement as HTMLElement | null
      if (e.shiftKey) {
        if (active === first || !dialogEl.contains(active)) {
          e.preventDefault()
          last.focus()
        }
      } else if (active === last || !dialogEl.contains(active)) {
        e.preventDefault()
        first.focus()
      }
      return
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      if (results.length > 0) {
        selectedIdx = Math.min(selectedIdx + 1, results.length - 1)
      }
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      if (results.length > 0) {
        selectedIdx = Math.max(selectedIdx - 1, 0)
      }
    } else if (e.key === 'Enter') {
      e.preventDefault()
      if (results[selectedIdx]) pick(results[selectedIdx])
    }
  }

  onMount(() => {
    previouslyFocused = document.activeElement as HTMLElement | null
    window.addEventListener('keydown', handleKeydown, true)
    inputEl?.focus()
    return () => {
      window.removeEventListener('keydown', handleKeydown, true)
      // Only restore if the prior element is still in the document (e.g. a
      // tab/button that wasn't unmounted while the picker was open).
      if (previouslyFocused?.isConnected) {
        previouslyFocused.focus()
      }
    }
  })
</script>

<div
  class="fixed inset-0 bg-black/45 backdrop-blur-[3px] z-[170] flex items-start justify-center pt-32"
>
  <button
    type="button"
    tabindex="-1"
    aria-label="Close block picker"
    onclick={onClose}
    class="absolute inset-0 cursor-default border-none p-0 bg-transparent"
  ></button>
  <div
    bind:this={dialogEl}
    role="dialog"
    aria-modal="true"
    aria-label="Embed a block"
    tabindex="-1"
    class="relative w-full max-w-2xl glass-palette border border-surface-modal-border rounded-xl shadow-2xl overflow-hidden flex flex-col max-h-[31.25rem]"
    style="backdrop-filter: blur(16px) saturate(140%); background: color-mix(in srgb, var(--color-surface-modal) 92%, transparent);"
  >
    <div class="px-5 py-3 border-b border-surface-modal-border">
      <h2 class="font-headline-md text-headline-md text-text-primary">
        Embed a block
      </h2>
      <p class="text-text-muted text-type-sm font-body-md mt-0.5">
        Search for the block to embed live.
      </p>
    </div>
    <div
      class="flex items-center gap-3 px-4 py-3 border-b border-surface-modal-border"
    >
      <span class="material-symbols-outlined text-text-muted text-icon-xl"
        >search</span
      >
      <input
        bind:this={inputEl}
        bind:value={query}
        oninput={onInput}
        type="text"
        placeholder="Search blocks to embed…"
        class="bg-transparent border-none outline-none text-text-primary text-type-lg font-body-md w-full focus:ring-0 placeholder:text-text-muted"
      />
      {#if query}
        <button
          type="button"
          aria-label="Clear search"
          onclick={() => {
            query = ''
            results = []
            inputEl?.focus()
          }}
          class="p-1 rounded hover:bg-hover text-text-muted hover:text-text-primary border-none bg-transparent cursor-pointer flex items-center justify-center focus:outline-none flex-shrink-0"
        >
          <span class="material-symbols-outlined text-icon-lg">close</span>
        </button>
      {/if}
      {#if loading}
        <span
          class="material-symbols-outlined text-accent-primary-start animate-spin text-type-2xl flex-shrink-0"
          >sync</span
        >
      {/if}
    </div>
    <div class="flex-1 overflow-y-auto custom-scrollbar py-2">
      {#if query.trim() === ''}
        <div class="text-text-muted text-center py-10 font-body-md">
          Type to search for a block…
        </div>
      {:else if results.length === 0 && !loading}
        <div class="text-text-muted text-center py-10 font-body-md">
          No blocks found.
        </div>
      {:else}
        {#each results as res, idx (res.id)}
          <button
            onclick={() => pick(res)}
            class="w-full px-5 py-3 border-none flex flex-col gap-1 text-left cursor-pointer transition-colors focus:outline-none hover:bg-hover/50"
            class:bg-accent-primary-glow={idx === selectedIdx}
          >
            <div
              class="flex items-center gap-1.5 text-type-2xs text-text-muted uppercase tracking-widest font-label-sm-bold"
            >
              <span>{res.notebook}</span>
              <span class="material-symbols-outlined text-type-2xs"
                >chevron_right</span
              >
              <span>{res.section}</span>
              <span class="material-symbols-outlined text-type-2xs"
                >chevron_right</span
              >
              <span>{res.page}</span>
            </div>
            <div class="font-body-md text-sm text-text-primary truncate">
              {res.clean_content}
            </div>
          </button>
        {/each}
      {/if}
    </div>
    <div
      class="px-4 py-2 border-t border-surface-modal-border text-type-2xs text-text-muted font-label-sm flex items-center justify-between bg-surface-modal/30"
    >
      <span
        >{results.length > 0
          ? `${results.length} match${results.length === 1 ? '' : 'es'}`
          : 'Embed block'}</span
      >
      <span class="opacity-60">↑↓ navigate · ⏎ embed · esc close</span>
    </div>
  </div>
</div>
