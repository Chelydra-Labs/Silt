<script lang="ts">
  import type { PluginContext, SearchHit } from '../../../sdk'
  import Popover from '../../../../components/Popover.svelte'

  /**
   * Dependency picker for the CardDetailPanel (#303). Lists a task's current
   * `[blocked_by::]` prerequisites as removable chips, with a typeahead input
   * backed by ctx.searchBlocks to add new ones. Mirrors the recurrence editor's
   * optimistic-commit + revert-on-failure contract and the BlockPickerModal's
   * debounced search + keyboard nav.
   */
  interface Props {
    cardId: string
    blockedBy: string[]
    ctx: PluginContext
    onMetaChanged?: () => void
  }

  let { cardId, blockedBy, ctx, onMetaChanged }: Props = $props()

  // Each dep chip needs a display label; we resolve the current set once on
  // mount / when the card changes. The chip's uuid is the source of truth;
  // the label is a friendly convenience (a fetch failure falls back to the
  // truncated uuid).
  interface DepDisplay {
    id: string
    label: string
  }
  let deps = $state<DepDisplay[]>([])

  let query = $state('')
  let results = $state<SearchHit[]>([])
  // Anchor for the results <Popover>; the search input binds this so the
  // floating listbox can be portaled out of CardDetailPanel's scroll container.
  let depInput = $state<HTMLInputElement | null>(null)
  let selectedIdx = $state(0)
  let loading = $state(false)
  let pending = $state(false)
  let errorMsg = $state('')
  let debounceTimer: ReturnType<typeof setTimeout> | null = null

  // Resolve the whole blocked-by set to display labels in a single query
  // (one IPC round-trip regardless of how many prerequisites the task has).
  // Falls back to the truncated uuid for any id not present in the index.
  async function refreshDeps() {
    if (blockedBy.length === 0) {
      deps = []
      return
    }
    const placeholders = blockedBy.map(() => '?').join(', ')
    let labels = new Map<string, string>()
    try {
      const { rows } = await ctx.sqliteQuery(
        `SELECT id, clean_content FROM blocks WHERE id IN (${placeholders})`,
        blockedBy
      )
      for (const r of rows) {
        const row = r as { id?: string; clean_content?: string }
        if (row.id) labels.set(row.id, row.clean_content || row.id.slice(0, 8))
      }
    } catch {
      // Fall back to truncated uuids for all deps.
    }
    deps = blockedBy.map((id) => ({
      id,
      label: labels.get(id) ?? id.slice(0, 8)
    }))
  }

  // Seed the chip list once per task (on mount / when the card changes). The
  // picker treats its own `deps` as the source of truth for the panel's
  // lifetime — addDep/removeDep are the only mutators — so a stale
  // block:changed re-render of the parent (passing the pre-edit blocked_by
  // projection before the new one propagates) can't transiently revert the
  // chips. The parent's reload still eventually feeds back the canonical edge
  // list, but by then the picker's optimistic state matches it.
  let seededCardId = ''
  $effect(() => {
    if (seededCardId !== cardId) {
      seededCardId = cardId
      void refreshDeps()
    }
  })

  async function runSearch() {
    const q = query.trim()
    if (q === '') {
      results = []
      return
    }
    loading = true
    try {
      // Task-only search: a non-task block can't be a meaningful prerequisite
      // (OpenBlockers JOINs tasks), so filter server-side via searchTasks.
      const raw = await ctx.searchTasks(q)
      // Filter out self and already-added deps.
      const existing = new Set(deps.map((d) => d.id))
      results = raw.filter(
        (r) => r.id && r.id !== cardId && !existing.has(r.id)
      )
      selectedIdx = 0
    } catch {
      results = []
    } finally {
      loading = false
    }
  }

  function onInput() {
    if (debounceTimer) clearTimeout(debounceTimer)
    debounceTimer = setTimeout(runSearch, 180)
  }

  async function addDep(id: string, label: string) {
    if (pending) return
    if (deps.some((d) => d.id === id)) return
    const prev = deps
    deps = [...deps, { id, label }]
    query = ''
    results = []
    errorMsg = ''
    pending = true
    try {
      await ctx.setTaskBlockedBy(
        cardId,
        deps.map((d) => d.id)
      )
      onMetaChanged?.()
    } catch (e) {
      // Revert the optimistic add. The cycle-rejection error reads as a
      // user-facing message in the aria-live region.
      deps = prev
      errorMsg =
        e instanceof Error && /circular/i.test(e.message)
          ? 'Cannot add: would create a circular dependency.'
          : e instanceof Error
            ? e.message
            : String(e)
    } finally {
      pending = false
      // Clearing results unmounts the focused result button; restore focus to
      // the input so keyboard/SR users can keep adding deps without re-tabbing.
      depInput?.focus()
    }
  }

  async function removeDep(id: string) {
    if (pending) return
    const prev = deps
    deps = deps.filter((d) => d.id !== id)
    errorMsg = ''
    pending = true
    try {
      await ctx.setTaskBlockedBy(
        cardId,
        deps.map((d) => d.id)
      )
      onMetaChanged?.()
    } catch (e) {
      deps = prev
      errorMsg = e instanceof Error ? e.message : String(e)
    } finally {
      pending = false
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (results.length === 0) return
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      selectedIdx = Math.min(selectedIdx + 1, results.length - 1)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      selectedIdx = Math.max(selectedIdx - 1, 0)
    } else if (e.key === 'Enter') {
      e.preventDefault()
      const r = results[selectedIdx]
      if (r) {
        void addDep(r.id, r.clean_content || r.id.slice(0, 8))
      }
    } else if (e.key === 'Escape') {
      results = []
      query = ''
    }
  }
</script>

<section class="flex flex-col gap-2">
  <h3
    class="font-label-sm-bold uppercase tracking-widest text-[10px] text-text-muted"
  >
    Dependencies
  </h3>

  <!-- Current dependency chips -->
  {#if deps.length > 0}
    <ul class="flex flex-wrap gap-1.5">
      {#each deps as dep (dep.id)}
        <li
          class="flex items-center gap-1 px-2 py-0.5 rounded-sm border border-surface-popover-border bg-surface-popover text-[11px] font-label-sm text-text-primary"
        >
          <span class="truncate max-w-[180px]">{dep.label}</span>
          <button
            type="button"
            onclick={() => removeDep(dep.id)}
            disabled={pending}
            class="text-text-muted hover:text-status-danger transition-colors disabled:opacity-50"
            aria-label="Remove dependency {dep.label}"
          >
            <span class="material-symbols-outlined text-[12px]">close</span>
          </button>
        </li>
      {/each}
    </ul>
  {:else}
    <p class="text-[11px] text-text-muted font-body-md">No prerequisites.</p>
  {/if}

  <!-- Typeahead search input -->
  <div>
    <input
      bind:this={depInput}
      type="text"
      bind:value={query}
      oninput={onInput}
      onkeydown={handleKeydown}
      placeholder="Search tasks to add…"
      aria-label="Search tasks to add as dependencies"
      role="combobox"
      aria-expanded={results.length > 0}
      aria-controls="dep-search-results"
      aria-autocomplete="list"
      aria-activedescendant={results.length > 0
        ? `dep-result-${selectedIdx}`
        : undefined}
      class="w-full px-2 py-1.5 rounded border border-surface-popover-border bg-surface-popover text-[12px] font-body-md text-text-primary placeholder:text-text-muted focus:outline-none focus:border-accent-primary-start"
    />
  </div>
  <Popover
    open={results.length > 0}
    onClose={() => {
      results = []
    }}
    anchor={depInput}
    matchWidth
    class="rounded border border-border-active bg-surface-popover shadow-2xl"
  >
    {#snippet content()}
      <ul
        id="dep-search-results"
        role="listbox"
        aria-label="Matching tasks"
        class="max-h-48 overflow-y-auto"
      >
        {#each results as res, idx (res.id)}
          <li
            id="dep-result-{idx}"
            role="option"
            aria-selected={idx === selectedIdx}
          >
            <button
              type="button"
              onclick={() =>
                addDep(res.id, res.clean_content || res.id.slice(0, 8))}
              disabled={pending}
              class="w-full px-2 py-1.5 border-none flex flex-col gap-0.5 text-left cursor-pointer transition-colors hover:bg-hover disabled:opacity-50"
              class:bg-accent-primary-glow={idx === selectedIdx}
            >
              <span class="text-[12px] text-text-primary truncate"
                >{res.clean_content || '(untitled)'}</span
              >
              <span
                class="text-[9px] text-text-muted uppercase tracking-widest font-label-sm truncate"
              >
                {res.notebook}{res.section ? ` › ${res.section}` : ''} › {res.page}
              </span>
            </button>
          </li>
        {/each}
      </ul>
    {/snippet}
  </Popover>

  <!-- Cycle / error region, announced to assistive tech -->
  {#if errorMsg}
    <p
      class="text-[11px] text-status-danger font-body-md"
      role="status"
      aria-live="polite"
    >
      {errorMsg}
    </p>
  {/if}
</section>
