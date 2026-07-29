// Small helpers shared across the agent tools.

/** Clamp a value into [min, max], defaulting when absent/non-finite. */
export function clampInt(
  v: unknown,
  def: number,
  min: number,
  max: number
): number {
  const n = typeof v === 'number' && Number.isFinite(v) ? v : def
  return Math.min(max, Math.max(min, Math.floor(n)))
}

/** Render a `notebook > section > page` breadcrumb, skipping empty segments. */
export function breadcrumb(
  notebook: string,
  section: string,
  page: string
): string {
  return [notebook, section, page].filter((s) => s && s.length > 0).join(' > ')
}

/**
 * Validate a YYYY-MM-DD string is a real calendar date, not just well-formed.
 * Agent tools route due dates to Go setters / atomic writes after the task may
 * already exist, so rejecting impossible dates (e.g. 2026-13-40, Feb 30) up
 * front avoids a partial-failure where the block is created but its due setter
 * then rejects. Round-trips the parsed value through Date to catch month/day
 * overflow.
 */
export function isValidYMD(s: string): boolean {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s)
  if (!m) return false
  const y = Number(m[1])
  const mo = Number(m[2])
  const d = Number(m[3])
  const dt = new Date(Date.UTC(y, mo - 1, d))
  return (
    dt.getUTCFullYear() === y &&
    dt.getUTCMonth() === mo - 1 &&
    dt.getUTCDate() === d
  )
}
