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
