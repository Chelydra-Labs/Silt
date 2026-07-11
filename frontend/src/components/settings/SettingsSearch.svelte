<script lang="ts">
  // Settings search box + results popover, rendered in the SettingsPanel
  // header. On query, matches the hand-curated index (settingsIndex.ts) and
  // lists results in a combobox-style listbox. Arrow/Enter/click jumps to the
  // section (sets the bound `section`) and, if the entry has an anchorId,
  // scrolls to + briefly rings the matching control.
  //
  // Keyboard model is scoped to the combobox (input + listbox) so it doesn't
  // collide with the sidebar nav's roving-tabindex handler: Arrow/Enter here
  // only act when the popover is open and focus is in the search field.
  import { searchSettings, type SettingsIndexEntry } from './settingsIndex'
  import { getSettingsSections } from './settingsSections.svelte'

  interface Props {
    onJump: (sectionId: string, anchorId?: string) => void
  }

  let { onJump }: Props = $props()

  let query = $state('')
  let results = $state<SettingsIndexEntry[]>([])
  let activeIndex = $state(0) // highlighted result in the listbox
  let open = $state(false)
  let inputEl = $state<HTMLInputElement | null>(null)

  // Section labels for the "· Section" suffix in each result row.
  let sections = $derived(getSettingsSections())
  function sectionLabel(id: string): string {
    return sections.find((s) => s.id === id)?.label ?? id
  }

  // Recompute results on query change; reset the highlight to the top and
  // open the popover whenever there's at least one match.
  $effect(() => {
    const next = searchSettings(query)
    results = next
    activeIndex = 0
    open = next.length > 0
  })

  // Live-region announcement text (visually hidden, aria-live=polite). SR
  // users hear how many matches landed the moment they type.
  let announcement = $derived(
    query.trim()
      ? results.length === 0
        ? 'No settings match.'
        : `${results.length} setting${results.length === 1 ? '' : 's'} match.`
      : ''
  )

  function commit(entry: SettingsIndexEntry) {
    if (!entry) return
    query = ''
    open = false
    inputEl?.blur()
    onJump(entry.sectionId, entry.anchorId)
  }

  // Scoped keyboard handler: only the combobox navigation keys are
  // intercepted, and only while the popover is open. This keeps Tab (and
  // every other key) flowing normally, and avoids swallowing the sidebar
  // nav's Arrow/Home/End model (that handler is on a different element).
  function onInputKeydown(e: KeyboardEvent) {
    if (!open) return
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      activeIndex = Math.min(results.length - 1, activeIndex + 1)
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      activeIndex = Math.max(0, activeIndex - 1)
    } else if (e.key === 'Enter') {
      e.preventDefault()
      const entry = results[activeIndex]
      if (entry) commit(entry)
    } else if (e.key === 'Escape') {
      e.preventDefault()
      query = ''
      open = false
    }
  }

  function onInputBlur() {
    // Defer close so a click on a result row registers before the popover is
    // torn down (mousedown focuses, then blur would fire, then click).
    setTimeout(() => {
      if (!inputEl || document.activeElement !== inputEl) open = false
    }, 120)
  }

  function onInputFocus() {
    if (results.length > 0) open = true
  }
</script>

<div class="relative w-full max-w-sm" data-test-settings-search>
  <div class="relative">
    <span
      class="material-symbols-outlined text-text-muted text-[18px] absolute left-2.5 top-1/2 -translate-y-1/2 pointer-events-none"
      aria-hidden="true">search</span
    >
    <input
      bind:this={inputEl}
      bind:value={query}
      onkeydown={onInputKeydown}
      onblur={onInputBlur}
      onfocus={onInputFocus}
      type="text"
      placeholder="Search settings…"
      role="combobox"
      aria-expanded={open}
      aria-controls="silt-settings-search-listbox"
      aria-autocomplete="list"
      aria-activedescendant={open && results[activeIndex]
        ? `silt-settings-search-result-${activeIndex}`
        : undefined}
      autocomplete="off"
      spellcheck="false"
      class="w-full pl-8 pr-3 py-1.5 rounded-lg bg-surface-panel border border-surface-panel-border text-text-primary text-[12px] font-body-md placeholder:text-text-muted outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start transition-all"
    />
  </div>

  <!-- SR-only live region: announces the result count so SR users hear how
       many matches landed without leaving the input. -->
  <div class="sr-only" aria-live="polite">{announcement}</div>

  {#if open}
    <ul
      id="silt-settings-search-listbox"
      role="listbox"
      aria-label="Matching settings"
      class="absolute z-20 left-0 right-0 top-full mt-1 max-h-72 overflow-y-auto custom-scrollbar rounded-lg border border-surface-popover-border bg-surface-popover shadow-xl py-1"
    >
      {#each results as entry, i (entry.label + entry.sectionId)}
        <li
          id="silt-settings-search-result-{i}"
          role="option"
          aria-selected={i === activeIndex}
          tabindex="-1"
        >
          <button
            type="button"
            onclick={() => commit(entry)}
            onpointerenter={() => (activeIndex = i)}
            class="w-full text-left px-3 py-1.5 border-none cursor-pointer flex items-center justify-between gap-2 transition-colors {i ===
            activeIndex
              ? 'bg-hover text-text-primary'
              : 'bg-transparent text-text-primary hover:bg-hover'}"
          >
            <span class="text-[12px] font-body-md truncate">{entry.label}</span>
            <span
              class="text-[10px] font-label-sm text-text-muted flex-shrink-0 truncate"
            >
              {sectionLabel(entry.sectionId)}
            </span>
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  /* Visually hidden but available to assistive tech (no global .sr-only
     utility exists; mirrors AppearanceTab.svelte's local copy). */
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }
</style>
