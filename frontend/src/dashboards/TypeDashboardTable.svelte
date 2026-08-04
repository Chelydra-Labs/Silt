<script lang="ts">
  // Dashboard table — a semantic <table> with sortable column headers (aria-sort
  // on the <th>, a <button> inside for keyboard sortability) and cells rendered
  // to the property's type. Grouping is rendered as collapsible group-header
  // rows interleaved with the group's data rows inside the same table, so a
  // single set of column headers stays visible and each group stays
  // self-describing for assistive tech.
  import {
    splitMultiValueText,
    type DashboardColumn,
    type GroupSection,
    type SortState,
    type TypeDashboardRow
  } from './dashboards'

  interface Props {
    columns: DashboardColumn[]
    sections: GroupSection[]
    /** True when group headers should render (group-by active). */
    grouped: boolean
    sort: SortState
    heroField?: string
    collapsed: Set<string>
    onSort: (property: string) => void
    onToggleGroup: (key: string) => void
    onOpenPage: (locator: {
      source: string
      notebook: string
      section: string
      page: string
    }) => void
  }

  let {
    columns,
    sections,
    grouped,
    sort,
    heroField = '',
    collapsed,
    onSort,
    onToggleGroup,
    onOpenPage
  }: Props = $props()

  function ariaSort(key: string): 'ascending' | 'descending' | 'none' {
    if (sort.property !== key) return 'none'
    return sort.desc ? 'descending' : 'ascending'
  }

  function sortIcon(key: string): string {
    if (sort.property !== key) return 'unfold_more'
    return sort.desc ? 'arrow_downward' : 'arrow_upward'
  }

  function heroOf(row: TypeDashboardRow): string {
    if (!heroField) return ''
    const hit = row.properties.find((p) => p.name === heroField)
    return hit?.valueText ?? ''
  }

  // Page-cell accessible name mirrors the board card's: include the hero,
  // notebook, and section so screen-reader users get the same disambiguation
  // as sighted users (who see them rendered in the cell).
  function pageCellLabel(row: TypeDashboardRow): string {
    const hero = heroOf(row)
    let label = `Open page ${row.page}`
    if (hero) label += `, ${hero}`
    if (row.notebook) label += `, ${row.notebook}`
    if (row.section) label += ` › ${row.section}`
    return label
  }
</script>

<div
  class="table-scroll custom-scrollbar"
  role="region"
  aria-label="Pages of this type"
>
  <table class="dtable">
    <thead>
      <tr>
        {#each columns as col (col.key)}
          <th scope="col" aria-sort={ariaSort(col.key)}>
            <button
              type="button"
              class="th-button"
              onclick={() => onSort(col.key)}
              aria-label="Sort by {col.label}"
            >
              <span>{col.label}</span>
              <span
                class="material-symbols-outlined text-icon-xs sort-icon"
                aria-hidden="true">{sortIcon(col.key)}</span
              >
            </button>
          </th>
        {/each}
      </tr>
    </thead>
    <tbody>
      {#each sections as section (section.key)}
        {#if grouped}
          <tr class="group-row">
            <th scope="rowgroup" colspan={columns.length}>
              <button
                type="button"
                class="group-toggle"
                aria-expanded={!collapsed.has(section.key)}
                aria-label="{section.label} group, {section.rows.length} pages"
                onclick={() => onToggleGroup(section.key)}
              >
                <span
                  class="material-symbols-outlined text-icon-sm"
                  aria-hidden="true"
                  >{collapsed.has(section.key)
                    ? 'chevron_right'
                    : 'expand_more'}</span
                >
                <span class="group-label">{section.label}</span>
                <span class="group-count" aria-hidden="true"
                  >{section.rows.length}</span
                >
              </button>
            </th>
          </tr>
        {/if}
        {#if !grouped || !collapsed.has(section.key)}
          {#each section.rows as row (row.source + ':' + row.notebook + ':' + row.section + ':' + row.page)}
            <tr class="data-row">
              <td class="page-cell">
                <button
                  type="button"
                  class="page-link"
                  onclick={() =>
                    onOpenPage({
                      source: row.source,
                      notebook: row.notebook,
                      section: row.section,
                      page: row.page
                    })}
                  aria-label={pageCellLabel(row)}
                >
                  <span class="page-name">{row.page}</span>
                  {#if heroOf(row)}
                    <span class="page-hero">{heroOf(row)}</span>
                  {/if}
                  <span class="page-loc"
                    >{row.notebook}{row.section
                      ? ' › ' + row.section
                      : ''}</span
                  >
                </button>
              </td>
              {#each columns.slice(1) as col (col.key)}
                <td class="cell cell-{col.kind}">
                  {@render cell(row, col)}
                </td>
              {/each}
            </tr>
          {/each}
        {/if}
      {/each}
    </tbody>
  </table>
</div>

{#snippet cell(row: TypeDashboardRow, col: DashboardColumn)}
  {@const value =
    row.properties.find((p) => p.name === col.key)?.valueText ?? ''}
  {#if col.kind === 'checkbox'}
    {#if value.toLowerCase() === 'true'}
      <span
        class="material-symbols-outlined text-icon-sm check"
        role="img"
        aria-label="Yes">check</span
      >
    {:else if value.toLowerCase() === 'false'}
      <span class="dash" role="img" aria-label="No">—</span>
    {/if}
  {:else if col.kind === 'select' && value}
    <span class="chip">{value}</span>
  {:else if (col.kind === 'multiselect' || col.kind === 'pages') && value}
    {#each splitMultiValueText(value) as v (v)}
      <span class="chip">{v}</span>
    {/each}
  {:else}
    <span class="text-val">{value}</span>
  {/if}
{/snippet}

<style>
  .table-scroll {
    overflow: auto;
    min-height: 0;
    flex: 1 1 auto;
  }
  .dtable {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--text-type-sm);
    font-family: var(--font-body, sans-serif);
  }
  .dtable thead th {
    position: sticky;
    top: 0;
    z-index: 1;
    background: var(--color-surface-panel);
    border-bottom: 1px solid var(--color-surface-panel-border);
    text-align: left;
    padding: 0;
    font-weight: 600;
    color: var(--color-text-muted);
  }
  .th-button {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    width: 100%;
    border: 0;
    background: transparent;
    color: inherit;
    font: inherit;
    font-weight: 600;
    padding: 0.5rem 0.6rem;
    cursor: pointer;
    text-align: left;
  }
  .th-button:hover {
    color: var(--color-text-primary);
    background: var(--color-hover);
  }
  .th-button:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: -2px;
  }
  .sort-icon {
    opacity: 0.5;
  }
  .data-row {
    border-bottom: 1px solid var(--color-surface-panel-border);
  }
  .data-row:hover {
    background: var(--color-hover);
  }
  .cell,
  .page-cell {
    padding: 0.4rem 0.6rem;
    vertical-align: top;
    color: var(--color-text-primary);
  }
  .page-cell {
    min-width: 12rem;
  }
  .page-link {
    display: flex;
    flex-direction: column;
    gap: 0.1rem;
    border: 0;
    background: transparent;
    padding: 0;
    text-align: left;
    cursor: pointer;
    color: var(--color-text-primary);
    font: inherit;
    max-width: 100%;
  }
  .page-link:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 2px;
    border-radius: 0.25rem;
  }
  .page-name {
    font-weight: 600;
    color: var(--color-text-primary);
  }
  .page-hero {
    color: var(--color-text-muted);
    font-size: var(--text-type-xs);
  }
  .page-loc {
    color: var(--color-text-muted);
    font-size: var(--text-type-2xs);
    opacity: 0.7;
  }
  .chip {
    display: inline-block;
    padding: 0.05rem 0.4rem;
    margin: 0.05rem;
    border-radius: 9999px;
    border: 1px solid var(--color-accent-secondary-start);
    background: var(--color-accent-secondary-glow);
    color: var(--color-accent-secondary-start);
    font-size: var(--text-type-2xs);
  }
  .check {
    color: var(--color-accent-primary-start);
  }
  .dash {
    color: var(--color-text-muted);
  }
  .text-val {
    white-space: pre-wrap;
    word-break: break-word;
  }
  .group-row th {
    background: var(--color-surface-panel);
    border-bottom: 1px solid var(--color-surface-panel-border);
    padding: 0;
  }
  .group-toggle {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    border: 0;
    background: transparent;
    color: var(--color-text-muted);
    font: inherit;
    font-weight: 600;
    padding: 0.4rem 0.6rem;
    cursor: pointer;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    font-size: var(--text-type-xs);
  }
  .group-toggle:hover {
    color: var(--color-text-primary);
  }
  .group-toggle:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: -2px;
  }
  .group-label {
    color: inherit;
  }
  .group-count {
    color: var(--color-text-muted);
    opacity: 0.7;
  }
</style>
