<script lang="ts">
  import { onMount, tick } from 'svelte'
  import type { OpenPageMode } from '../lib/tabs'
  import type { RecentPageRef } from '../lib/sidebar/types'
  import {
    rankNavigation,
    type NavigationCatalogItem
  } from '../lib/navigationCatalog'

  interface Props {
    catalog: NavigationCatalogItem[]
    recents?: RecentPageRef[]
    loading?: boolean
    error?: string
    onRetry: () => void
    onOpen: (item: NavigationCatalogItem, mode: OpenPageMode) => void
    onClose: () => void
  }

  let {
    catalog,
    recents = [],
    loading = false,
    error = '',
    onRetry,
    onOpen,
    onClose
  }: Props = $props()

  let query = $state('')
  let activeIndex = $state(-1)
  let activeKey = $state('')
  let input = $state<HTMLInputElement | null>(null)
  let dialog = $state<HTMLDivElement | null>(null)
  let previousFocus: HTMLElement | null = null
  let results = $derived(rankNavigation(catalog, query, recents))
  let selectableIndices = $derived(
    results.flatMap((item, index) => (item.disconnected ? [] : [index]))
  )
  let allOffline = $derived(
    !loading && !error && results.length > 0 && selectableIndices.length === 0
  )

  $effect(() => {
    results
    selectableIndices
    const retainedIndex = results.findIndex(
      (item) => item.key === activeKey && !item.disconnected
    )
    setActiveIndex(
      retainedIndex >= 0 ? retainedIndex : (selectableIndices[0] ?? -1)
    )
  })

  function setActiveIndex(index: number) {
    activeIndex = index
    activeKey = index >= 0 ? (results[index]?.key ?? '') : ''
  }

  function optionId(index: number) {
    return `quick-switcher-option-${index}`
  }

  function activate(item: NavigationCatalogItem, mode: OpenPageMode) {
    if (item.disconnected) return
    onOpen(item, mode)
    onClose()
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      const current = selectableIndices.indexOf(activeIndex)
      setActiveIndex(
        selectableIndices[(current + 1) % selectableIndices.length] ?? -1
      )
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      const current = selectableIndices.indexOf(activeIndex)
      setActiveIndex(
        selectableIndices[
          (current - 1 + selectableIndices.length) % selectableIndices.length
        ] ?? -1
      )
    } else if (event.key === 'Home') {
      event.preventDefault()
      setActiveIndex(selectableIndices[0] ?? -1)
    } else if (event.key === 'End') {
      event.preventDefault()
      setActiveIndex(selectableIndices.at(-1) ?? -1)
    } else if (event.key === 'Enter') {
      event.preventDefault()
      const item = results[activeIndex]
      if (item)
        activate(item, event.ctrlKey || event.metaKey ? 'pin' : 'preview')
    }
    void tick().then(() =>
      document
        .getElementById(optionId(activeIndex))
        ?.scrollIntoView?.({ block: 'nearest' })
    )
  }

  function handleDialogKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.preventDefault()
      onClose()
      return
    }
    if (event.key !== 'Tab' || !dialog) return
    const focusable = Array.from(
      dialog.querySelectorAll<HTMLElement>(
        'input:not([disabled]), button:not([disabled]):not([tabindex="-1"])'
      )
    )
    if (focusable.length < 2) {
      event.preventDefault()
      input?.focus()
      return
    }
    const current = focusable.indexOf(document.activeElement as HTMLElement)
    const next = event.shiftKey
      ? (current - 1 + focusable.length) % focusable.length
      : (current + 1) % focusable.length
    event.preventDefault()
    focusable[next]?.focus()
  }

  onMount(() => {
    previousFocus = document.activeElement as HTMLElement | null
    input?.focus()
    return () => previousFocus?.focus()
  })
</script>

<div
  class="fixed inset-0 z-[190] flex justify-center items-start pt-[12vh] px-3 bg-black/45 backdrop-blur-[3px]"
>
  <button
    type="button"
    tabindex="-1"
    aria-label="Close page switcher"
    class="absolute inset-0 border-none bg-transparent cursor-default"
    onclick={onClose}
  ></button>
  <div
    bind:this={dialog}
    role="dialog"
    tabindex="-1"
    aria-modal="true"
    aria-labelledby="quick-switcher-title"
    class="relative w-full max-w-xl max-h-[70vh] overflow-hidden rounded-2xl border border-surface-modal-border glass-palette glass-palette-strong shadow-2xl flex flex-col motion-reduce:transition-none"
    onkeydown={handleDialogKeydown}
  >
    <h2 id="quick-switcher-title" class="sr-only">Switch page</h2>
    <div
      class="flex items-center gap-3 px-4 py-3 border-b border-surface-modal-border"
    >
      <span
        class="material-symbols-outlined text-accent-primary-start"
        aria-hidden="true">quick_reference_all</span
      >
      <input
        bind:this={input}
        bind:value={query}
        onkeydown={handleKeydown}
        role="combobox"
        aria-label="Find a page"
        aria-expanded="true"
        aria-controls="quick-switcher-results"
        aria-autocomplete="list"
        aria-activedescendant={activeIndex >= 0
          ? optionId(activeIndex)
          : undefined}
        autocomplete="off"
        placeholder="Page name or path…"
        class="w-full bg-transparent border-none outline-none text-text-primary text-type-lg placeholder:text-text-muted rounded focus-visible:ring-2 focus-visible:ring-accent-primary-start"
      />
      <kbd
        class="hidden sm:block text-type-3xs text-text-muted border border-surface-modal-border rounded px-1.5 py-0.5"
        >Esc</kbd
      >
    </div>
    <div
      id="quick-switcher-results"
      role="listbox"
      aria-label="Pages"
      class="overflow-y-auto custom-scrollbar p-2"
    >
      {#if loading}
        <p class="m-0 px-3 py-8 text-center text-text-muted" role="status">
          Loading pages…
        </p>
      {:else if error}
        <div class="px-3 py-8 text-center" role="alert">
          <p class="m-0 text-status-warn">Pages could not be refreshed.</p>
          <button
            type="button"
            class="mt-2 text-accent-primary-start underline border-none bg-transparent cursor-pointer"
            onclick={onRetry}>Try again</button
          >
        </div>
      {:else if results.length === 0}
        <p class="m-0 px-3 py-8 text-center text-text-muted" role="status">
          No pages match “{query}”.
        </p>
      {:else}
        {#if allOffline}
          <p class="sr-only" role="status" aria-live="polite">
            All matching pages are offline.
          </p>
        {/if}
        {#each results as item, index (item.key)}
          <button
            type="button"
            tabindex="-1"
            id={optionId(index)}
            role="option"
            aria-selected={index === activeIndex}
            aria-disabled={item.disconnected}
            disabled={item.disconnected}
            onmouseenter={() => setActiveIndex(index)}
            onclick={() => activate(item, 'preview')}
            class="w-full rounded-lg px-3 py-2.5 border-none bg-transparent text-left flex items-center gap-3 cursor-pointer disabled:cursor-default disabled:opacity-55"
            class:bg-hover={index === activeIndex}
          >
            <span
              class="material-symbols-outlined text-icon-lg text-accent-primary-start"
              aria-hidden="true"
            >
              {item.disconnected
                ? 'cloud_off'
                : item.linked
                  ? 'link'
                  : 'description'}
            </span>
            <span class="min-w-0 flex-1">
              <span class="block truncate text-text-primary font-label-sm-bold"
                >{item.page}</span
              >
              <span class="block truncate text-text-muted text-type-xs"
                >{item.notebook}{item.section ? ` / ${item.section}` : ''}</span
              >
            </span>
            {#if item.disconnected}<span class="text-type-3xs text-status-warn"
                >Offline</span
              >{/if}
          </button>
        {/each}
      {/if}
    </div>
    <div
      class="hidden sm:flex justify-between px-4 py-2 border-t border-surface-modal-border text-type-3xs text-text-muted"
    >
      <span>Enter to preview</span><span>Ctrl/⌘ + Enter to pin</span>
    </div>
  </div>
</div>
