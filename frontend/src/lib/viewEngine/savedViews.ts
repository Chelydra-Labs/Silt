// Generic saved-view partial-snapshot matching, shared by every surface that
// shows user-defined views (silt-tasks hub, typed-notes dashboard). Extracted
// from silt-tasks/savedViews.ts so the "does this view describe the live
// state?" contract has one source of truth.
//
// Pure: no Svelte, no bindings. The matcher compares ONLY the dimensions a
// view defines (undefined view dims don't disqualify — a system template that
// omits a dim matches any state value for that dim). Callers pass the dim set
// and an optional per-dim equality override for fields that need structural
// comparison (e.g. silt-tasks columns with wipLimit, or nested filter bags).

/** Shallow element-wise array equality (used as the default for array dims). */
export function arrayEqual<T>(a: readonly T[], b: readonly T[]): boolean {
  if (a.length !== b.length) return false
  for (let i = 0; i < a.length; i++) {
    if (a[i] !== b[i]) return false
  }
  return true
}

/** Per-dimension equality hook: `true` if the view's value matches state. */
export type DimEquality = (viewValue: unknown, stateValue: unknown) => boolean

/**
 * Default dim equality: strict `===` for scalars, {@link arrayEqual} for
 * arrays. Objects fall back to reference equality — callers pass a custom
 * `equalsBy` for structural object comparisons.
 */
export function defaultValuesEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (Array.isArray(a) && Array.isArray(b)) return arrayEqual(a, b)
  return false
}

/**
 * Lenient "does this view match the live state?" check.
 *
 * Iterates `dims` and compares each one the VIEW defines (`view[dim] !==
 * undefined`). An undefined-by-view dim is skipped — system templates
 * intentionally omit dims they don't constrain, and matching them strictly
 * against a fully-populated state would never highlight. A user view that
 * snapshots every dim reduces to the strict check.
 *
 * `equalsBy` lets a caller override equality per dim (e.g. columns use a
 * structural comparator that treats null/undefined wipLimit as equal). When
 * unset for a dim, {@link defaultValuesEqual} applies.
 */
export function viewMatchesState(
  view: Record<string, unknown>,
  state: Record<string, unknown>,
  dims: Iterable<string>,
  equalsBy?: (dim: string) => DimEquality | undefined
): boolean {
  for (const dim of dims) {
    const viewVal = view[dim]
    if (viewVal === undefined) continue
    const eq = equalsBy?.(dim) ?? defaultValuesEqual
    if (!eq(viewVal, state[dim])) return false
  }
  return true
}
