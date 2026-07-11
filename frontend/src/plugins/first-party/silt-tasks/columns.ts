// Status-board column model with optional soft WIP limits (#437).
//
// Columns are the only user-managed Board dimension (groupBy='status').
// Legacy YAML stored a plain string[] of status names; the structured form
// adds an optional per-column wipLimit. normalizeColumns accepts both so
// older vaults keep working without a migration.

export interface BoardColumn {
  name: string
  /** Soft cap on cards in this column. null/undefined = unlimited. */
  wipLimit?: number | null
}

/** Default status lanes — unlimited WIP. */
export const DEFAULT_COLUMNS: BoardColumn[] = [
  { name: 'TODO' },
  { name: 'DOING' },
  { name: 'DONE' }
]

/**
 * Coerce unknown persisted input into a clean BoardColumn[].
 * Accepts legacy `string[]` and structured `{name, wipLimit?}[]`.
 * Caps at 50 entries; falls back to DEFAULT_COLUMNS when empty/invalid.
 */
export function normalizeColumns(input: unknown): BoardColumn[] {
  if (!Array.isArray(input) || input.length === 0) {
    return DEFAULT_COLUMNS.map((c) => ({ ...c }))
  }
  const out: BoardColumn[] = []
  for (const item of input) {
    if (out.length >= 50) break
    if (typeof item === 'string') {
      const name = item.trim()
      if (name) out.push({ name })
      continue
    }
    if (item && typeof item === 'object') {
      const r = item as Record<string, unknown>
      if (typeof r.name !== 'string') continue
      const name = r.name.trim()
      if (!name) continue
      const col: BoardColumn = { name }
      if (
        typeof r.wipLimit === 'number' &&
        Number.isFinite(r.wipLimit) &&
        r.wipLimit >= 1
      ) {
        col.wipLimit = Math.floor(r.wipLimit)
      } else if (r.wipLimit === null) {
        col.wipLimit = null
      }
      out.push(col)
    }
  }
  return out.length > 0 ? out : DEFAULT_COLUMNS.map((c) => ({ ...c }))
}

/** Extract status names in column order. */
export function columnNames(cols: BoardColumn[]): string[] {
  return cols.map((c) => c.name)
}

/**
 * Structural equality including wipLimit. Treats null and undefined as the
 * same (both mean unlimited) so a legacy string[] load matches a structured
 * default with no wipLimit set.
 */
export function columnsEqual(a: BoardColumn[], b: BoardColumn[]): boolean {
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i++) {
    if (a[i].name !== b[i].name) return false
    const aw = a[i].wipLimit ?? null
    const bw = b[i].wipLimit ?? null
    if (aw !== bw) return false
  }
  return true
}

/** Deep-clone a column list (safe for state snapshots / mutations). */
export function cloneColumns(cols: BoardColumn[]): BoardColumn[] {
  return cols.map((c) => ({ ...c }))
}
