// State types + pure helpers for the per-type dashboard view.
//
// The dashboard is read-only: QueryPagesByType returns a flat filtered+sorted
// list, and the frontend bins it into groups client-side (mirrors the silt-tasks
// grouping model). The TypeDef/PropertyDef/PropertyType types are reused from
// the properties module so the schema has one source of truth.
import type { PropertyDef, PropertyType, TypeDef } from '../properties/types'

/** One property value on a dashboard row (the wire shape from QueryPagesByType). */
export interface TypeDashboardProp {
  name: string
  /** Human-readable value (already rendered by the backend projection). */
  valueText: string
  /** The property's value type, so the cell picks the right renderer. */
  valueType: PropertyType
}

/** One page of the queried type. */
export interface TypeDashboardRow {
  source: string
  notebook: string
  section: string
  page: string
  properties: TypeDashboardProp[]
}

/** Per-property filter state. A missing key = no filter on that property;
 *  an empty-string value selects only rows where the property is unset. */
export type FilterState = Record<string, string>

export interface SortState {
  /** '' sorts by page path; otherwise a property name. */
  property: string
  desc: boolean
}

export interface GroupSection {
  /** Stable key (used as the DOM data-group + collapse-set entry). */
  key: string
  label: string
  rows: TypeDashboardRow[]
}

/** The page-name pseudo-column, always first. Sort key '' is the IPC's
 *  "sort by path" sentinel. */
export const PAGE_COLUMN_KEY = ''

export interface DashboardColumn {
  key: string
  label: string
  /** Property type driving the cell renderer; 'page-name' for the first column. */
  kind: PropertyType | 'page-name'
  /** Schema options for select/multiselect (cell chips + filter control). */
  options?: string[]
}

/** Build the column list: the page column first, then one per schema property. */
export function buildColumns(type: TypeDef | null): DashboardColumn[] {
  const cols: DashboardColumn[] = [
    { key: PAGE_COLUMN_KEY, label: 'Page', kind: 'page-name' }
  ]
  if (!type) return cols
  for (const p of type.properties ?? []) {
    cols.push({
      key: p.name,
      label: p.label || p.name,
      kind: p.type,
      options: p.options
    })
  }
  return cols
}

/** Find a row's rendered value for a property (empty string when unset). */
export function rowPropertyValue(
  row: TypeDashboardRow,
  propName: string
): string {
  const hit = row.properties.find((p) => p.name === propName)
  return hit?.valueText ?? ''
}

/** Tokenize a projection multi-value (joined with ", "). */
export function splitMultiValueText(raw: string): string[] {
  if (!raw.trim()) return []
  return raw
    .split(', ')
    .map((v) => v.trim())
    .filter(Boolean)
}

/**
 * Bin rows into GroupSections by a property's value. Multiselect/pages use
 * multi-membership (a row appears once per value); every other type is
 * single-membership. Empty values fall into a trailing "Unassigned" bucket so
 * they stay discoverable. Group order is alphabetical with Unassigned last.
 */
export function binByProperty(
  rows: TypeDashboardRow[],
  prop: PropertyDef | undefined,
  propName: string
): GroupSection[] {
  const multi = prop?.type === 'multiselect' || prop?.type === 'pages'
  const buckets = new Map<string, TypeDashboardRow[]>()
  const unassigned: TypeDashboardRow[] = []
  for (const row of rows) {
    const raw = rowPropertyValue(row, propName)
    // Multi-values are joined with ", " by the projection layer (see
    // formatPropertyValue). Split on that separator — bare "," would fracture
    // a genuine option like "a, b" into phantom buckets.
    const values = multi
      ? splitMultiValueText(raw)
      : raw.trim()
        ? [raw.trim()]
        : []
    if (values.length === 0) {
      unassigned.push(row)
      continue
    }
    for (const v of values) {
      if (!buckets.has(v)) buckets.set(v, [])
      buckets.get(v)!.push(row)
    }
  }
  const keys = [...buckets.keys()].sort((a, b) => a.localeCompare(b))
  const sections = keys.map((k) => ({
    key: `${propName}::${k}`,
    label: k,
    rows: buckets.get(k) ?? []
  }))
  if (unassigned.length > 0) {
    sections.push({
      key: `${propName}::__unassigned__`,
      label: 'Unassigned',
      rows: unassigned
    })
  }
  return sections
}
