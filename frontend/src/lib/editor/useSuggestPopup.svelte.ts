// Shared primitives for the editor's typeahead suggest popups (meta / mention /
// block-ref / tag / page-link). Each subsystem keeps its own fetch shape, error
// handling, and popup state wiring; these helpers consolidate only the pieces
// that are genuinely identical — and subtly load-bearing — across all of them:
//
//   - a monotonic request-generation counter that discards superseded async
//     results (a late-resolving fetch whose popup has moved on),
//   - a self-clearing debounce timer,
//   - the up/down index cycle used by onNavigate,
//   - the "popup context still matches" stale-result check.
//
// Behaviour must match the original inline implementations exactly.

/**
 * Monotonic generation counter used to discard superseded async results. A
 * fetch captures the generation at issue time (`begin`); when it resolves it
 * asks `isCurrent` — if a later keystroke has since begun a new generation,
 * the stale result is dropped.
 */
export interface RequestRace {
  /** Start a new generation; returns its id. */
  begin(): number
  /** True iff `id` is the current (latest) generation. */
  isCurrent(id: number): boolean
}

export function createRequestRace(): RequestRace {
  let current = 0
  return {
    begin: () => ++current,
    isCurrent: (id) => id === current
  }
}

/**
 * A self-clearing debounce timer. Scheduling again clears any pending fire,
 * and the handle is dropped before the callback runs so a re-schedule or
 * cancel() inside the callback sees no pending timer (matches the inline
 * pattern where the timer variable is nulled at the top of the callback).
 */
export interface DebouncedRunner {
  schedule(ms: number, fn: () => void): void
  cancel(): void
}

export function createDebouncedRunner(): DebouncedRunner {
  let timer: ReturnType<typeof setTimeout> | null = null
  return {
    schedule(ms, fn) {
      if (timer) clearTimeout(timer)
      timer = setTimeout(() => {
        timer = null
        fn()
      }, ms)
    },
    cancel() {
      if (timer) {
        clearTimeout(timer)
        timer = null
      }
    }
  }
}

/**
 * Wrap `selected` by `dir` (±1) around [0, length). Returns null when there is
 * nothing to cycle (callers are guarded upstream so length is > 0 in practice,
 * but the guard keeps the arithmetic safe if ever called empty).
 */
export function cycleSelected(
  selected: number,
  dir: 1 | -1,
  length: number
): number | null {
  if (length <= 0) return null
  return (selected + dir + length) % length
}

/**
 * True iff the popup's context still matches `ctx` (same range + query). Used
 * to drop a late-resolving fetch whose popup has since moved on — the partner
 * of the request-generation guard, for the case where the popup state itself
 * changed (closed, or the query was edited in place). Callers pair it with an
 * explicit `!popup ||` null check so TypeScript keeps the non-null narrowing
 * the original inline `!current || …` pattern provided.
 */
export function ctxStillMatches<Ctx extends { from: number; query: string }>(
  popup: { ctx: Ctx } | null,
  ctx: Ctx
): boolean {
  return (
    popup !== null &&
    popup.ctx.from === ctx.from &&
    popup.ctx.query === ctx.query
  )
}
