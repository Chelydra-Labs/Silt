<script lang="ts">
  // The settings section list rendered in the sidebar when Settings is the
  // active view (#511 rework). This is the `role="tablist"` half of the
  // WAI-ARIA tabs pattern; the matching `role="tabpanel"` lives in
  // SettingsPanel (the content area). The two are wired together by stable
  // element ids (silt-settings-tab-<id> ↔ silt-settings-panel), so they read
  // as one tab widget despite living in separate layout slots.
  //
  // Sections are visually clustered under labeled group dividers (Workspace,
  // Look & feel, …). The dividers are purely presentational — the tablist
  // stays ONE flat list of tabs, so roving tabindex + Arrow/Home/End traverse
  // every section regardless of which group it sits in.
  import {
    getSettingsSections,
    SETTINGS_GROUP_LABELS,
    type SettingsGroup
  } from './settingsSections.svelte'

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
  // Tab (the browser's tab-sequence leaves the list). The handler lives on
  // the tablist, so it only fires when focus is inside it — the SettingsPanel
  // search box has its own scoped model and won't collide.
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

  // Walk the sections once, emitting a group divider each time the cluster
  // changes. The divider is a non-interactive label (role="presentation",
  // aria-hidden) — it never participates in the tab sequence, so the tablist
  // remains a flat roving-index list per the WAI-ARIA tabs contract.
  type NavRow =
    | { kind: 'divider'; group: SettingsGroup }
    | {
        kind: 'section'
        section: (typeof sections)[number]
        indent: boolean
        idx: number
      }

  let grouped = $derived.by(() => {
    const rows: NavRow[] = []
    let lastGroup: SettingsGroup | null = null
    let sectionIdx = 0
    for (const sec of sections) {
      if (sec.group !== lastGroup) {
        rows.push({ kind: 'divider', group: sec.group })
        lastGroup = sec.group
      }
      // Plugin bespoke-settings tabs are indented under Plugins to signal
      // parent/child, but remain direct nav targets (real tabs).
      const indent = sec.id.startsWith('plugin:')
      rows.push({
        kind: 'section',
        section: sec,
        indent,
        idx: sectionIdx++
      })
    }
    return rows
  })
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
  {#each grouped as row, i (row.kind + (row.kind === 'divider' ? row.group : row.section.id))}
    {#if row.kind === 'divider'}
      <!-- Group label: presentational only. aria-hidden so SR users don't
           hear a static label between tabs; the tabs themselves are
           self-describing (named by their visible label). -->
      <div
        role="presentation"
        aria-hidden="true"
        class="px-6 pt-4 pb-1 text-surface-sidebar-text-muted text-[10px] uppercase tracking-widest font-label-sm-bold select-none {i ===
        0
          ? 'pt-1'
          : ''}"
      >
        {SETTINGS_GROUP_LABELS[row.group]}
      </div>
    {:else}
      {@const sec = row.section}
      <button
        bind:this={navRefs[row.idx]}
        onclick={() => selectSection(sec.id)}
        role="tab"
        id="silt-settings-tab-{sec.id}"
        aria-selected={section === sec.id}
        aria-controls="silt-settings-panel"
        tabindex={section === sec.id ? 0 : -1}
        class="relative flex items-center gap-3 mx-2 py-2.5 rounded-lg font-label-sm text-label-sm transition-all border-none cursor-pointer text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 {row.indent
          ? 'pl-10 pr-4'
          : 'pl-5 pr-4'} {section === sec.id
          ? 'bg-surface-sidebar-text/10 text-accent-primary-start'
          : 'text-surface-sidebar-text-muted hover:bg-surface-sidebar-text/5 hover:text-surface-sidebar-text'}"
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
    {/if}
  {/each}
</div>
