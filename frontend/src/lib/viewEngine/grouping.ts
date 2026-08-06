// Generic, framework-agnostic grouping primitives shared by every surface
// that bins a flat row list into ordered sections (silt-tasks list/board,
// typed-notes dashboard table/board). Extracted from silt-tasks/grouping.ts
// and dashboards/dashboards.ts so the binning algorithm has one source of
// truth instead of two parallel copies.
//
// Pure: no Svelte, no Wails bindings, no plugin-session assumptions. Callers
// inject the row-specific shape via `keyOf`/`sectionKey`/`sectionLabel`. The
// task-specific date/status bucketing stays in silt-tasks (hard-coded task
// semantics); only the scalar/tag/multi-membership binning lives here.

/** A labelled bucket of rows produced by the binning primitives. */
export interface GroupSection<T> {
  /** Stable data-group key (DOM attribute + test selector). */
  key: string
  /** Human-readable heading (also used as the aria-label). */
  label: string
  items: T[]
}

/** Sentinel value the unassigned bucket is keyed under before `sectionKey`. */
export const UNASSIGNED_VALUE = '__unassigned__'

export interface BinByKeyOptions<T> {
  /**
   * Extract the bucket value(s) for a row.
   *
   * - Return a `string` → single-membership (the row lands in one bucket).
   * - Return a `string[]` → multi-membership: the row appears once per value
   *   (mirrors tag/multiselect binning, where one row can belong to several
   *   groups). An empty array routes the row to the trailing unassigned
   *   bucket.
   * - Return null/undefined/'' → the row has no value and lands in the
   *   trailing unassigned bucket.
   *
   * Values are trimmed; empty strings inside an array are dropped.
   */
  keyOf: (row: T) => string | string[] | null | undefined
  /**
   * Map a bucket value to its stable data-group key. Default: identity (the
   * value IS the key). Callers namespace it to avoid collisions across
   * dimensions (e.g. `v => \`owner-${v}\`` or `v => \`${prop}::${v}\``).
   * Applied to {@link UNASSIGNED_VALUE} to derive the unassigned bucket's key.
   */
  sectionKey?: (value: string) => string
  /** Map a bucket value to its display label. Default: identity. */
  sectionLabel?: (value: string) => string
  /**
   * Bucket-value comparator. Default: `localeCompare` (alphabetical). Override
   * for numeric dimensions (priority sorts `Number(a) - Number(b)`).
   */
  compareKeys?: (a: string, b: string) => number
  /** Bucket ordering. `'asc'` (default) / `'desc'` reverse the comparator;
   *  `'none'` keeps first-seen insertion order. */
  order?: 'asc' | 'desc' | 'none'
  /** Optional within-section row sort (applied per bucket). */
  sortBy?: (a: T, b: T) => number
  /** Label for the trailing unassigned bucket. Default `'Unassigned'`. */
  unassignedLabel?: string
}

/**
 * Bin rows into ordered `GroupSection`s by the value returned from `keyOf`.
 *
 * Always emits one section per distinct non-empty value (in the requested
 * order), then a trailing unassigned section IF any rows had no value.
 * Sections are never empty (empty buckets are omitted — only buckets that
 * received rows appear, plus the unassigned tail when non-empty).
 */
export function binByKey<T>(
  rows: T[],
  opts: BinByKeyOptions<T>
): GroupSection<T>[] {
  const sectionKey = opts.sectionKey ?? ((v) => v)
  const sectionLabel = opts.sectionLabel ?? ((v) => v)
  const compareKeys = opts.compareKeys ?? ((a, b) => a.localeCompare(b))
  const order = opts.order ?? 'asc'
  const sortBy = opts.sortBy

  // Buckets are keyed by the RAW value so two distinct values that happen to
  // share a sectionKey (unlikely, but possible with custom keyers) stay in
  // separate sections rather than silently merging.
  const buckets = new Map<string, T[]>()
  const unassigned: T[] = []

  for (const row of rows) {
    const raw = opts.keyOf(row)
    const values = normalizeValues(raw)
    if (values.length === 0) {
      unassigned.push(row)
      continue
    }
    for (const v of values) {
      let bucket = buckets.get(v)
      if (!bucket) {
        bucket = []
        buckets.set(v, bucket)
      }
      bucket.push(row)
    }
  }

  const keys = [...buckets.keys()]
  if (order === 'asc') keys.sort(compareKeys)
  else if (order === 'desc') keys.sort((a, b) => compareKeys(b, a))
  // 'none': preserve first-seen insertion order (Map already does this).

  const sections: GroupSection<T>[] = keys.map((v) => {
    const items = sortBy
      ? [...(buckets.get(v) ?? [])].sort(sortBy)
      : (buckets.get(v) ?? [])
    return { key: sectionKey(v), label: sectionLabel(v), items }
  })

  if (unassigned.length > 0) {
    const items = sortBy ? [...unassigned].sort(sortBy) : unassigned
    sections.push({
      key: sectionKey(UNASSIGNED_VALUE),
      label: opts.unassignedLabel ?? 'Unassigned',
      items
    })
  }
  return sections
}

function normalizeValues(raw: string | string[] | null | undefined): string[] {
  if (Array.isArray(raw)) {
    const out: string[] = []
    for (const v of raw) {
      if (typeof v !== 'string') continue
      const t = v.trim()
      if (t) out.push(t)
    }
    return out
  }
  if (typeof raw === 'string') {
    const t = raw.trim()
    return t ? [t] : []
  }
  return []
}

/**
 * The "no dimension selected" single-section mode: every row in one bucket.
 * Callers reach this when the user picks "None" / no group-by dimension, so
 * the renderer can still map over a uniform `GroupSection[]` shape.
 */
export function singleSection<T>(
  rows: T[],
  key: string,
  label: string
): GroupSection<T>[] {
  return [{ key, label, items: [...rows] }]
}
