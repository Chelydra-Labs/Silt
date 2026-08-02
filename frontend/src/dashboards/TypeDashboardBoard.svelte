<script lang="ts">
  // Dashboard board view — kanban-style columns rendered from the same
  // GroupSection[] the table consumes. One column per group; one card per
  // page row. Read-only (no DnD) — this is a browse/scan surface for users
  // who think about their pages by the grouped dimension, complementing the
  // dense table. Cards are keyboard-activatable (role=button) so screen
  // readers and keyboard users reach the same onOpenPage path as a click.
  import type {
    DashboardColumn,
    GroupSection,
    TypeDashboardProp,
    TypeDashboardRow
  } from './dashboards'

  interface Props {
    sections: GroupSection[]
    columns: DashboardColumn[]
    grouped: boolean
    heroField?: string
    onOpenPage: (locator: {
      source: string
      notebook: string
      section: string
      page: string
    }) => void
  }

  let {
    sections,
    columns,
    grouped,
    heroField = '',
    onOpenPage
  }: Props = $props()

  // Chip up to two non-page, non-hero properties so cards stay scannable.
  // Page-name is the title, hero is the subtitle — chips surface the next
  // most identifying attrs without recreating the full table grid.
  let chipColumns = $derived(
    columns.filter((c) => c.kind !== 'page-name' && c.key !== heroField)
  )

  function heroOf(row: TypeDashboardRow): string {
    if (!heroField) return ''
    const hit = row.properties.find((p) => p.name === heroField)
    return hit?.valueText ?? ''
  }

  function propOf(
    row: TypeDashboardRow,
    name: string
  ): TypeDashboardProp | undefined {
    return row.properties.find((p) => p.name === name)
  }

  function open(row: TypeDashboardRow): void {
    onOpenPage({
      source: row.source,
      notebook: row.notebook,
      section: row.section,
      page: row.page
    })
  }

  // Enter / Space activate the focused card (role=button convention).
  function onCardKeydown(e: KeyboardEvent, row: TypeDashboardRow): void {
    if (e.key !== 'Enter' && e.key !== ' ' && e.key !== 'Spacebar') return
    e.preventDefault()
    open(row)
  }
</script>

<div
  class="board-scroll custom-scrollbar"
  role="list"
  aria-label="Pages of this type, board view"
>
  {#if !grouped}
    <!-- No group-by active: render a single "All pages" lane so the board
         stays a board (not a flat list). The empty label keeps the count
         readable. -->
    {@render lane({
      key: '__all__',
      label: 'All pages',
      rows: sections[0]?.rows ?? []
    })}
  {:else}
    {#each sections as section (section.key)}
      {@render lane(section)}
    {/each}
  {/if}
</div>

{#snippet lane(section: GroupSection)}
  {@const count = section.rows.length}
  <section
    class="column"
    role="group"
    aria-label="{section.label || 'All pages'} ({count})"
  >
    <header class="column-head">
      <span class="column-label">{section.label || 'All pages'}</span>
      <span class="column-count" aria-hidden="true">{count}</span>
    </header>
    <div class="column-body custom-scrollbar">
      {#if count === 0}
        <p class="column-empty">No pages</p>
      {:else}
        {#each section.rows as row (row.source + ':' + row.notebook + ':' + row.section + ':' + row.page)}
          {@render card(row)}
        {/each}
      {/if}
    </div>
  </section>
{/snippet}

{#snippet card(row: TypeDashboardRow)}
  {@const hero = heroOf(row)}
  {@const chips = chipColumns
    .slice(0, 2)
    .map((c) => ({ col: c, prop: propOf(row, c.key) }))
    .filter((c) => c.prop && c.prop.valueText !== '')}
  <div
    class="card"
    role="button"
    tabindex="0"
    aria-label="{row.page}{hero ? ', ' + hero : ''}{row.notebook
      ? ', ' + row.notebook
      : ''}{row.section ? ' › ' + row.section : ''}"
    onclick={() => open(row)}
    onkeydown={(e) => onCardKeydown(e, row)}
  >
    <span class="card-name">{row.page}</span>
    {#if hero}
      <span class="card-hero">{hero}</span>
    {/if}
    <span class="card-loc"
      >{row.notebook}{row.section ? ' › ' + row.section : ''}</span
    >
    {#if chips.length > 0}
      <div class="card-chips" aria-hidden="true">
        {#each chips as { col, prop } (col.key)}
          {#if col.kind === 'checkbox'}
            <span class="chip chip-bool">
              {prop!.valueText.toLowerCase() === 'true' ? '✓' : '—'}
              {col.label}</span
            >
          {:else}
            <span class="chip">{prop!.valueText}</span>
          {/if}
        {/each}
      </div>
    {/if}
  </div>
{/snippet}

<style>
  .board-scroll {
    display: flex;
    gap: 1rem;
    padding: 1rem;
    overflow-x: auto;
    overflow-y: hidden;
    flex: 1 1 auto;
    min-height: 0;
  }
  .column {
    display: flex;
    flex-direction: column;
    width: 17rem;
    min-width: 17rem;
    max-height: 100%;
    background: var(--color-surface-panel);
    border: 1px solid var(--color-surface-panel-border);
    border-radius: var(--radius-md);
  }
  .column-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    padding: 0.5rem 0.6rem;
    border-bottom: 1px solid var(--color-surface-panel-border);
    color: var(--color-text-muted);
    font-size: var(--text-type-xs);
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .column-label {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .column-count {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 1.25rem;
    padding: 0 0.35rem;
    border-radius: 9999px;
    background: var(--color-hover);
    color: var(--color-text-muted);
    font-size: var(--text-type-2xs);
  }
  .column-body {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    padding: 0.5rem;
    overflow-y: auto;
    min-height: 4rem;
  }
  .column-empty {
    margin: 0;
    padding: 0.75rem 0.5rem;
    text-align: center;
    color: var(--color-text-muted);
    font-size: var(--text-type-xs);
    border: 1px dashed var(--color-surface-panel-border);
    border-radius: var(--radius-sm);
  }
  .card {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    padding: 0.5rem 0.6rem;
    background: var(--color-surface-card);
    border: 1px solid var(--color-surface-card-border);
    border-radius: var(--radius-sm);
    cursor: pointer;
    transition:
      background 120ms var(--transition-standard),
      border-color 120ms var(--transition-standard);
  }
  .card:hover {
    background: var(--color-hover);
    border-color: var(--color-border-active);
  }
  .card:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 2px;
  }
  .card-name {
    color: var(--color-text-primary);
    font-size: var(--text-type-sm);
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .card-hero {
    color: var(--color-text-muted);
    font-size: var(--text-type-xs);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .card-loc {
    color: var(--color-text-muted);
    font-size: var(--text-type-2xs);
    opacity: 0.75;
  }
  .card-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem;
    margin-top: 0.25rem;
  }
  .chip {
    display: inline-block;
    padding: 0.05rem 0.4rem;
    border-radius: 9999px;
    border: 1px solid var(--color-accent-secondary-start);
    background: var(--color-accent-secondary-glow);
    color: var(--color-accent-secondary-start);
    font-size: var(--text-type-2xs);
  }
  .chip-bool {
    border-color: var(--color-surface-panel-border);
    background: var(--color-hover);
    color: var(--color-text-muted);
  }
</style>
