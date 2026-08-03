<script lang="ts">
  // Page-link autocomplete for `page` (single) / `pages` (multi) properties.
  // Replaces the plain text input in PropertyField. The nav list is fetched
  // once on first focus (ListNavigation, flattened), optionally narrowed to
  // pages of the declared `target` type (QueryPagesByType). The component is
  // value-controlled: it never calls IPC itself — every change routes through
  // onCommit, so PropertyField's optimistic field owns the snapshot/revert.
  import { onMount, tick } from 'svelte'
  import { SvelteSet } from 'svelte/reactivity'
  import { ListNavigation, QueryPagesByType } from '../../bindings/silt/app.js'
  import { flattenNavigation } from '../lib/navigationCatalog'
  import { indexNav, resolveRef, toRef, type NavIndex } from './pageRef'
  import type { NavigationCatalogItem } from '../lib/navigationCatalog'

  interface Props {
    /** Wire value: a single ref string for `page`, a string[] for `pages`. */
    value: string | string[]
    /** True for `pages` (multi-relation). */
    multi: boolean
    /** Declared target type id/name; '' = any page. Narrows the dropdown. */
    target?: string
    label: string
    fieldId: string
    disabled?: boolean
    mismatched?: boolean
    /** Forwards aria-required to the combobox input. */
    required?: boolean
    onCommit: (next: string | string[]) => void
  }

  let {
    value,
    multi,
    target = '',
    label,
    fieldId,
    disabled = false,
    mismatched = false,
    required = false,
    onCommit
  }: Props = $props()

  let query = $state('')
  let open = $state(false)
  let activeIndex = $state(-1)
  let idx = $state<NavIndex | null>(null)
  let loading = $state(false)
  let loadError = $state('')
  let inputRef = $state<HTMLInputElement | null>(null)
  let listboxId = $derived(`${fieldId}-listbox`)

  // Current refs as an array regardless of cardinality, for chip rendering.
  let refs = $derived(Array.isArray(value) ? value : value ? [value] : [])

  let loaded = false
  async function ensureLoaded(): Promise<void> {
    if (loaded) return
    loading = true
    loadError = ''
    try {
      const tree = (await ListNavigation()) as Parameters<
        typeof flattenNavigation
      >[0]
      let items = flattenNavigation(tree)
      if (target) {
        const rows = (await QueryPagesByType(target, {}, '', false)) as Array<{
          source: string
          notebook: string
          section: string
          page: string
        }> | null
        // Include `source` so a linked notebook sharing notebook/section/page
        // names with the vault can't collide — only the matching source's
        // pages are eligible.
        const wanted = new Set(
          (rows ?? []).map(
            (r) => `${r.source}|${r.notebook}|${r.section}|${r.page}`
          )
        )
        items = items.filter((it) =>
          wanted.has(`${it.source}|${it.notebook}|${it.section}|${it.page}`)
        )
      }
      idx = indexNav(items)
      loaded = true
    } catch (e) {
      loadError = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

  // Load on mount so dangling chips (refs to deleted pages) render with the
  // correct visual state immediately, before the user ever focuses the field.
  // `loaded` guards the focus path from re-fetching.
  onMount(() => {
    void ensureLoaded()
  })

  // Filtered, capped results for the open dropdown.
  let results = $derived.by<NavigationCatalogItem[]>(() => {
    if (!idx) return []
    const q = query.trim().toLowerCase()
    const filtered = q
      ? idx.refs.filter((it) => it.page.toLowerCase().includes(q))
      : idx.refs
    // Hide already-selected pages in multi mode so the list stays focused.
    // Normalize stored refs (resolve bare leaf names to their canonical path)
    // so an MCP-written bare-name duplicate of an already-linked page is
    // detected rather than offered again.
    const rawSelected = multi ? (Array.isArray(value) ? value : []) : []
    const selected = new SvelteSet<string>()
    for (const ref of rawSelected) {
      const hit = resolveRef(ref, idx)
      selected.add(hit ? toRef(hit.notebook, hit.section, hit.page) : ref)
    }
    return filtered
      .filter((it) => !selected.has(toRef(it.notebook, it.section, it.page)))
      .slice(0, 10)
  })

  function openDropdown(): void {
    if (disabled) return
    open = true
    activeIndex = results.length > 0 ? 0 : -1
    void ensureLoaded()
  }

  function closeDropdown(): void {
    open = false
    activeIndex = -1
    query = ''
  }

  function select(it: NavigationCatalogItem): void {
    const ref = toRef(it.notebook, it.section, it.page)
    if (multi) {
      const current = Array.isArray(value) ? [...value] : []
      if (!current.includes(ref)) current.push(ref)
      onCommit(current)
    } else {
      onCommit(ref)
    }
    query = ''
    activeIndex = results.length > 0 ? 0 : -1
    // Single-relation select commits and can close; multi keeps the picker
    // open for the next entry. Refocus the input so keyboard nav continues.
    if (!multi) {
      closeDropdown()
    }
    void tick().then(() => inputRef?.focus())
  }

  function removeChip(ref: string): void {
    if (multi) {
      onCommit((Array.isArray(value) ? value : []).filter((r) => r !== ref))
    } else {
      onCommit('')
    }
  }

  function onKeyDown(e: KeyboardEvent): void {
    // Backspace on an empty multi input removes the last chip regardless of
    // whether the dropdown is open — matches the standard chip-input idiom.
    if (e.key === 'Backspace' && multi && query === '') {
      const current = Array.isArray(value) ? value : []
      if (current.length > 0) {
        e.preventDefault()
        onCommit(current.slice(0, -1))
      }
      return
    }
    if (!open) {
      if (e.key === 'ArrowDown' && results.length > 0) {
        openDropdown()
        e.preventDefault()
      }
      return
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      if (results.length > 0) {
        activeIndex = (activeIndex + 1) % results.length
      }
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      if (results.length > 0) {
        activeIndex = (activeIndex - 1 + results.length) % results.length
      }
    } else if (e.key === 'Enter') {
      if (activeIndex >= 0 && activeIndex < results.length) {
        e.preventDefault()
        select(results[activeIndex])
      }
    } else if (e.key === 'Escape') {
      // stopPropagation so the panel's window-level Esc handler doesn't also
      // fire and dismiss the whole panel — the open dropdown consumes Esc.
      e.preventDefault()
      e.stopPropagation()
      closeDropdown()
    }
  }

  // Keep activeIndex inside the results window as the query filters.
  $effect(() => {
    void results.length
    if (activeIndex >= results.length) activeIndex = results.length - 1
    if (activeIndex < 0 && results.length > 0 && open) activeIndex = 0
  })

  function optionId(i: number): string {
    return `${fieldId}-opt-${i}`
  }
  let activeDescendant = $derived(
    open && activeIndex >= 0 ? optionId(activeIndex) : undefined
  )

  // Dangling detection: which stored refs no longer resolve.
  function isDangling(ref: string): boolean {
    if (!idx) return false
    return !resolveRef(ref, idx)
  }
  // Resolve a ref to a display label (page name) when possible.
  function refLabel(ref: string): string {
    if (idx) {
      const hit = resolveRef(ref, idx)
      if (hit) return hit.page
    }
    // Fallback: the last path segment, or the whole ref.
    const segs = ref.split('/')
    return segs[segs.length - 1] || ref
  }
</script>

<div class="plf" class:mismatched>
  {#if refs.length > 0}
    <div class="chips" aria-label={label}>
      {#each refs as ref (ref)}
        <span class="chip" class:dangling={idx ? isDangling(ref) : false}>
          <span class="chip-label">{refLabel(ref)}</span>
          <button
            type="button"
            class="chip-x"
            aria-label="Remove {refLabel(ref)}"
            onclick={() => removeChip(ref)}
            {disabled}
          >
            <span class="material-symbols-outlined" aria-hidden="true"
              >close</span
            >
          </button>
        </span>
      {/each}
    </div>
  {/if}

  <div class="input-wrap">
    <input
      bind:this={inputRef}
      id={fieldId}
      type="text"
      class="input"
      role="combobox"
      aria-expanded={open}
      aria-autocomplete="list"
      aria-controls={open ? listboxId : undefined}
      aria-activedescendant={activeDescendant}
      aria-label={label}
      aria-required={required}
      placeholder={multi ? 'Add a page…' : 'Link a page…'}
      value={query}
      {disabled}
      onfocus={openDropdown}
      oninput={(e) => {
        query = e.currentTarget.value
        if (!open) open = true
        activeIndex = results.length > 0 ? 0 : -1
      }}
      onkeydown={onKeyDown}
      onblur={() => {
        // Defer so an option click (mousedown-guarded) lands before close.
        setTimeout(() => closeDropdown(), 120)
      }}
    />
    {#if loading}
      <span class="status" aria-hidden="true">…</span>
    {/if}
  </div>

  {#if open && loadError}
    <p class="err" role="alert">{loadError}</p>
  {/if}

  {#if open && idx}
    {#if results.length > 0}
      <ul class="listbox" id={listboxId} role="listbox" aria-label={label}>
        {#each results as it, i (it.key)}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- Combobox options are keyboard-operated through the input (Arrow/
               Enter via aria-activedescendant); the li onclick is mouse-only. -->
          <li
            id={optionId(i)}
            role="option"
            aria-selected={i === activeIndex}
            class="option"
            class:active={i === activeIndex}
            onmousedown={(e) => e.preventDefault()}
            onclick={() => select(it)}
            onmouseenter={() => (activeIndex = i)}
          >
            <span class="opt-name">{it.page}</span>
            <span class="opt-sub"
              >{it.notebook}{it.section ? ' / ' + it.section : ''}</span
            >
          </li>
        {/each}
      </ul>
    {:else}
      <div class="empty-list" role="status">No matching pages</div>
    {/if}
  {/if}
</div>

<style>
  .plf {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    position: relative;
    min-width: 0;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem;
  }
  .chip {
    display: inline-flex;
    align-items: center;
    gap: 0.2rem;
    padding: 0.1rem 0.1rem 0.1rem 0.5rem;
    border-radius: 9999px;
    border: 1px solid var(--color-accent-secondary-start);
    background: var(--color-accent-secondary-glow);
    color: var(--color-accent-secondary-start);
    font-size: var(--text-type-xs);
  }
  .chip.dangling {
    border-color: var(--color-surface-panel-border);
    background: transparent;
    color: var(--color-text-muted);
    text-decoration: line-through;
  }
  .chip-x {
    display: inline-flex;
    align-items: center;
    border: 0;
    background: transparent;
    color: inherit;
    cursor: pointer;
    padding: 0.05rem;
    border-radius: 9999px;
  }
  .chip-x .material-symbols-outlined {
    font-size: var(--text-icon-xs);
  }
  .chip-x:hover {
    background: var(--color-hover);
  }
  .chip-x:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .input-wrap {
    position: relative;
    display: flex;
    align-items: center;
  }
  .input {
    width: 100%;
    background: var(--color-surface-app);
    border: 1px solid var(--color-surface-panel-border);
    color: var(--color-text-primary);
    border-radius: 0.375rem;
    padding: 0.3rem 0.5rem;
    font-size: var(--text-type-sm);
    line-height: 1.4;
  }
  .input:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .input:disabled {
    opacity: 0.6;
  }
  .status {
    position: absolute;
    right: 0.5rem;
    color: var(--color-text-muted);
    font-size: var(--text-type-xs);
  }
  .listbox {
    position: absolute;
    z-index: 50;
    top: calc(100% + 0.15rem);
    left: 0;
    right: 0;
    max-height: 16rem;
    overflow-y: auto;
    margin: 0;
    padding: 0.2rem;
    list-style: none;
    background: var(--color-surface-popover);
    border: 1px solid var(--color-surface-popover-border);
    border-radius: 0.5rem;
    box-shadow: var(--shadow-lg);
  }
  .option {
    display: flex;
    flex-direction: column;
    padding: 0.3rem 0.5rem;
    border-radius: 0.3rem;
    cursor: pointer;
  }
  .option.active {
    background: var(--color-hover);
  }
  .opt-name {
    color: var(--color-text-primary);
    font-weight: 600;
    font-size: var(--text-type-sm);
  }
  .opt-sub {
    color: var(--color-text-muted);
    font-size: var(--text-type-2xs);
  }
  .empty-list {
    color: var(--color-text-muted);
    font-size: var(--text-type-xs);
    padding: 0.4rem 0.5rem;
  }
  .err {
    color: var(--color-error-fg);
    font-size: var(--text-type-2xs);
    margin: 0;
  }
  .mismatched .input {
    border-color: var(--color-status-warn);
  }
</style>
