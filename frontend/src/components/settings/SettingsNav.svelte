<script lang="ts">
  // The settings section list rendered in the sidebar when Settings is the
  // active view (#511 rework). This is the `role="tablist"` half of the
  // WAI-ARIA tabs pattern; the matching `role="tabpanel"` lives in
  // SettingsPanel (the content area). The two are wired together by stable
  // element ids (silt-settings-tab-<id> ↔ silt-settings-panel), so they read
  // as one tab widget despite living in separate layout slots.
  import { getSettingsSections } from './settingsSections.svelte'

  interface Props {
    section?: string
  }

  let { section = $bindable('general') }: Props = $props()

  let sections = $derived(getSettingsSections())
  let navRefs: HTMLButtonElement[] = $state([])

  function selectSection(id: string) {
    section = id
    const idx = sections.findIndex((s) => s.id === id)
    navRefs[idx]?.focus()
  }

  // Roving tabindex: Arrow/Home/End move between sections without consuming
  // Tab (the browser's tab-sequence leaves the list).
  function handleKeydown(e: KeyboardEvent) {
    const idx = sections.findIndex((s) => s.id === section)
    if (e.key === 'ArrowDown' || e.key === 'ArrowRight') {
      e.preventDefault()
      selectSection(sections[(idx + 1) % sections.length].id)
    } else if (e.key === 'ArrowUp' || e.key === 'ArrowLeft') {
      e.preventDefault()
      selectSection(sections[(idx - 1 + sections.length) % sections.length].id)
    } else if (e.key === 'Home') {
      e.preventDefault()
      selectSection(sections[0].id)
    } else if (e.key === 'End') {
      e.preventDefault()
      selectSection(sections[sections.length - 1].id)
    }
  }
</script>

<div
  class="flex-1 min-h-0 overflow-y-auto custom-scrollbar flex flex-col py-3"
  role="tablist"
  aria-label="Settings sections"
  aria-orientation="vertical"
  tabindex="-1"
  onkeydown={handleKeydown}
  data-test-settings-nav
>
  {#each sections as sec, i (sec.id)}
    <button
      bind:this={navRefs[i]}
      onclick={() => selectSection(sec.id)}
      role="tab"
      id="silt-settings-tab-{sec.id}"
      aria-selected={section === sec.id}
      aria-controls="silt-settings-panel"
      tabindex={section === sec.id ? 0 : -1}
      class="relative flex items-center gap-3 pl-5 pr-4 py-2.5 mx-2 rounded-lg font-label-sm text-label-sm transition-all border-none cursor-pointer text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 {section ===
      sec.id
        ? 'bg-active text-accent-primary-start'
        : 'text-text-muted hover:bg-hover hover:text-text-primary'}"
    >
      {#if section === sec.id}
        <div
          class="absolute left-1.5 top-2.5 bottom-2.5 w-0.5 rounded-full bg-accent-primary-start shadow-[0_0_8px_var(--color-accent-primary-start)]"
        ></div>
      {/if}
      <span class="material-symbols-outlined text-[20px]" aria-hidden="true"
        >{sec.icon}</span
      >
      {sec.label}
    </button>
  {/each}
</div>
