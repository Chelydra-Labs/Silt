<script lang="ts">
  import { shortestPageReference } from '../lib/pageActions'

  interface Props {
    notebook: string
    section: string
    page: string
    activeView: string
    linked?: boolean
    disconnected?: boolean
    onSelectNotebook: (notebook: string) => void
    onSelectSection: (section: string) => void
    onOpenPage: () => void
  }

  let {
    notebook,
    section,
    page,
    activeView,
    linked = false,
    disconnected = false,
    onSelectNotebook,
    onSelectSection,
    onOpenPage
  }: Props = $props()

  let reference = $state('')
  let sectionSegments = $derived(
    section
      .split('/')
      .filter(Boolean)
      .map((label, index, parts) => ({
        label,
        path: parts.slice(0, index + 1).join('/')
      }))
  )
  let fullLabel = $derived(
    [notebook, section, page].filter(Boolean).join(' / ')
  )

  $effect(() => {
    const ref = { notebook, section, page }
    if (!notebook || !page || activeView !== 'notes') {
      reference = ''
      return
    }
    let current = true
    void shortestPageReference(ref).then((value) => {
      if (current) reference = value
    })
    return () => {
      current = false
    }
  })
</script>

{#if activeView === 'notes' && notebook && page}
  <nav
    aria-label={`Page location: ${fullLabel}`}
    title={`${fullLabel}${reference ? ` · ${reference}` : ''}`}
    class="h-8 flex items-center gap-1 px-3 border-b border-surface-panel-border bg-surface-panel/60 min-w-0 overflow-hidden"
  >
    <button
      type="button"
      class="crumb notebook"
      onclick={() => onSelectNotebook(notebook)}
    >
      <span class="material-symbols-outlined text-icon-sm" aria-hidden="true"
        >menu_book</span
      >
      <span class="truncate">{notebook}</span>
    </button>
    {#if linked}
      <span
        class:text-status-warn={disconnected}
        class:text-text-muted={!disconnected}
        class="material-symbols-outlined text-icon-sm flex-shrink-0"
        aria-label={disconnected
          ? 'Linked notebook offline'
          : 'Linked notebook'}
        title={disconnected ? 'Linked notebook offline' : 'Linked notebook'}
        >{disconnected ? 'cloud_off' : 'link'}</span
      >
    {/if}
    {#each sectionSegments as segment (segment.path)}
      <span class="separator" aria-hidden="true">›</span>
      <button
        type="button"
        class="crumb section-crumb"
        onclick={() => onSelectSection(segment.path)}
        title={segment.path}>{segment.label}</button
      >
    {/each}
    <span class="separator" aria-hidden="true">›</span>
    <button
      type="button"
      class="crumb page"
      disabled={disconnected}
      aria-label={`${fullLabel}${reference ? `, ${reference}` : ''}`}
      onclick={onOpenPage}>{page}</button
    >
  </nav>
{/if}

<style>
  .crumb {
    min-width: 0;
    max-width: 14rem;
    border: 0;
    background: transparent;
    color: var(--color-text-muted);
    padding: 0.2rem 0.25rem;
    border-radius: 0.3rem;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    font-size: var(--text-type-xs);
    white-space: nowrap;
  }
  .crumb:hover:not(:disabled),
  .crumb:focus-visible {
    color: var(--color-text-primary);
    background: var(--color-hover);
    outline: none;
  }
  .page {
    color: var(--color-text-primary);
    font-weight: 650;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .separator {
    color: var(--color-text-muted);
    opacity: 0.55;
    flex: 0 0 auto;
  }
  @media (max-width: 700px) {
    .section-crumb:not(:last-of-type) {
      display: none;
    }
    .crumb {
      max-width: 9rem;
    }
  }
</style>
