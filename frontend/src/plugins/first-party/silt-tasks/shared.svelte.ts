// Shared reactive primitives for the Tasks display surfaces (Board / List /
// Calendar) + the TaskEditDrawer. Both hooks here were lifted verbatim from
// near-identical inline copies in those four files; consolidating them keeps
// the debounce window, event name, and blocker-fetch shape map in one place.
//
// Must live in a `.svelte.ts` module because the primitives use runes
// ($effect / $state). Callers must invoke them synchronously during component
// init (the same constraint as $effect) — every existing caller does.
import type { PluginContext } from '../../sdk'

/**
 * Debounce a `block:changed` event into a single `reload()` call. The backend
 * emits block:changed for every created/mutated/rescheduled block; a burst
 * (e.g. a bulk paste) would otherwise trigger N round-trips. A trailing
 * debounce coalesces them into one reload `ms` after the last event.
 *
 * The teardown clears any pending timer AND unsubscribes the listener, so a
 * component destruction mid-burst can't fire a reload into a dead component.
 */
export function useBlockChangedReload(
  ctx: PluginContext,
  reload: () => void | Promise<void>,
  ms = 80
): void {
  let timer: ReturnType<typeof setTimeout> | null = null
  $effect(() => {
    const off = ctx.on('block:changed', () => {
      if (timer) clearTimeout(timer)
      timer = setTimeout(() => {
        void reload()
      }, ms)
    })
    return () => {
      if (timer) clearTimeout(timer)
      off()
    }
  })
}

/** A shaped prerequisite row the BlockedDoneDialog lists. */
export interface BlockerRef {
  id: string
  clean_content?: string
}

/**
 * Result of the DONE-on-blocked pre-check.
 * - 'dialog': the task has open prerequisites and the guard opened the
 *   confirmation dialog (caller should bail out of its persist path).
 * - 'clear': no blockers — proceed with the persist.
 * - 'error': the blocker fetch threw and onError already ran (ListView and
 *   Drawer abort; BoardView uses resolveBlockers directly instead).
 */
export type BlockedCheckResult = 'dialog' | 'clear' | 'error'

/**
 * Shared DONE-on-blocked guard state + blocker-fetch logic for the three
 * task-edit surfaces (Board, List, Drawer). Each surface renders its own
 * BlockedDoneDialog bound to `pending`; the confirm/cancel handlers stay in
 * the surface (they diverge too much to share — Board reverts an optimistic
 * column move, List re-runs commitMarkDown, Drawer reverts the status radio
 * and re-points focus).
 *
 * The guard owns: the pending state, the getTaskBlockers fetch + shape map,
 * and an onError side-effect hook. Callers attach a typed `context` payload
 * (the card/item/columns they need in their confirm/cancel handlers) so the
 * state that lands in `pending` carries everything the handler needs without
 * the guard knowing about columns or drawers.
 *
 * Two entry points:
 *  - `check()` — the simple path (ListView, Drawer). Returns the
 *    BlockedCheckResult and opens the dialog itself when blockers exist.
 *  - `resolveBlockers()` + `open()` — the low-level path (BoardView), which
 *    needs a moveSeq concurrency guard between the await resolving and the
 *    dialog opening (a second drop during the await must not strand a dialog).
 */
export function useBlockedDoneGuard<TContext>(
  ctx: PluginContext,
  onError: (e: unknown) => void = () => {}
) {
  let pending = $state<{
    blockers: BlockerRef[]
    context: TContext
  } | null>(null)

  /** Resolve + shape blockers without touching `pending`. */
  async function resolveBlockers(
    taskId: string
  ): Promise<{ ok: true; blockers: BlockerRef[] } | { ok: false }> {
    try {
      const blockers = await ctx.getTaskBlockers(taskId)
      return {
        ok: true,
        blockers: blockers.map((b) => ({
          id: b.id,
          clean_content: b.clean_content
        }))
      }
    } catch (e) {
      onError(e)
      return { ok: false }
    }
  }

  /**
   * High-level guard: returns 'dialog' (opened), 'clear' (proceed), or
   * 'error' (fetch threw; onError already ran). Opens the dialog itself when
   * blockers exist. BoardView does NOT use this — it needs the moveSeq
   * interlock, so it calls resolveBlockers + open directly.
   */
  async function check(
    taskId: string,
    isBlocked: boolean | number | null | undefined,
    context: TContext
  ): Promise<BlockedCheckResult> {
    if (!isBlocked) return 'clear'
    const result = await resolveBlockers(taskId)
    if (!result.ok) return 'error'
    if (result.blockers.length > 0) {
      pending = { blockers: result.blockers, context }
      return 'dialog'
    }
    return 'clear'
  }

  /** Set pending directly (pairs with resolveBlockers for the interlocked path). */
  function open(blockers: BlockerRef[], context: TContext): void {
    pending = { blockers, context }
  }

  /** Clear the dialog state (confirm/cancel paths call this first). */
  function dismiss(): void {
    pending = null
  }

  return {
    get pending() {
      return pending
    },
    check,
    resolveBlockers,
    open,
    dismiss
  }
}
