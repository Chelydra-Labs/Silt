/**
 * Viewport-aware flip + clamp for an absolutely-positioned dropdown menu or
 * listbox. On mount (and again on `resize` + one animation frame later) it
 * reads the anchor's `getBoundingClientRect()`, compares space-above vs
 * space-below, and:
 *   - clamps the node's inline `max-height` to the winning side (capped at
 *     `maxHeightPx`), and
 *   - reports the winning side through `onPlacement` so the host can render a
 *     placement class (`top`/`bottom` swap lives in the host's CSS, where the
 *     gap already lives — the action never touches the node's `top`/`bottom`).
 *
 * The host keeps a CSS `max-height` (e.g. `16rem`/`18rem`) as the non-JS /
 * pre-measure fallback; this action overrides it inline once it has measured.
 *
 * Mounting IS the open signal: attach this to a node that is conditionally
 * rendered (`{#if open}`), so the action runs precisely while the menu is on
 * screen and tears down its listeners on close. The next-frame re-measure
 * covers a layout shift between mount and first paint (a fly transition
 * settling, the host panel resizing to fit content) without touching host
 * focus — `onPlacement` only flips a host boolean, and the inline `max-height`
 * writes to the absolutely-positioned menu node, neither of which moves focus.
 */
export interface FlipMenuOptions {
  /** Returns the anchor the node is positioned against. Re-invoked on each
   *  measure so a late-binding ref is fine. */
  getAnchor: () => HTMLElement | null
  /** Hard cap on `max-height` regardless of available space (px). Match the
   *  host's CSS fallback (`18rem` → `288`, `16rem` → `256`). */
  maxHeightPx?: number
  /** Anchor→node gap (px). Roughly match the CSS `top`/`bottom` calc so the
   *  measurement accounts for the painted offset. */
  gapPx?: number
  /** Inset from the viewport edge (px) so the node never kisses the border. */
  viewportPadPx?: number
  /** Called with `true` when the node should open above the anchor (more room
   *  above), `false` to open below. Wire it to a host `$state` and bind the
   *  placement class in markup so Svelte's CSS analyzer sees it used. */
  onPlacement?: (flipped: boolean) => void
}

const DEFAULT_MAX_HEIGHT_PX = 16 * 16 // 16rem
const DEFAULT_GAP_PX = 4
const DEFAULT_VIEWPORT_PAD_PX = 8

interface ResolvedOptions {
  getAnchor: () => HTMLElement | null
  maxHeightPx: number
  gapPx: number
  viewportPadPx: number
  onPlacement: ((flipped: boolean) => void) | null
}

/** Lifecycle handle returned by {@link flipMenu}. Exposed for test/tooling use. */
export interface FlipMenuHandle {
  update(next: FlipMenuOptions): void
  destroy(): void
}

function resolve(options: FlipMenuOptions): ResolvedOptions {
  return {
    getAnchor: options.getAnchor,
    maxHeightPx: options.maxHeightPx ?? DEFAULT_MAX_HEIGHT_PX,
    gapPx: options.gapPx ?? DEFAULT_GAP_PX,
    viewportPadPx: options.viewportPadPx ?? DEFAULT_VIEWPORT_PAD_PX,
    onPlacement: options.onPlacement ?? null
  }
}

export function flipMenu(
  node: HTMLElement,
  initial: FlipMenuOptions
): FlipMenuHandle {
  let options = resolve(initial)
  let frame = 0

  function measure(): void {
    const anchor = options.getAnchor()
    // No anchor yet → leave the CSS fallback in place (no inline override, no
    // flip class). The host's `max-height` carries until a measure can run.
    if (!anchor) return
    const rect = anchor.getBoundingClientRect()
    const spaceBelow =
      window.innerHeight - rect.bottom - options.gapPx - options.viewportPadPx
    const spaceAbove = rect.top - options.gapPx - options.viewportPadPx
    // Prefer opening below on ties — matches the CSS default placement so the
    // no-flip path stays the common one.
    const flip = spaceAbove > spaceBelow
    const space = flip ? spaceAbove : spaceBelow
    const bounded = Math.min(options.maxHeightPx, Math.max(0, space))
    node.style.maxHeight = `${bounded}px`
    options.onPlacement?.(flip)
  }

  measure()
  frame = requestAnimationFrame(measure)
  window.addEventListener('resize', measure)

  return {
    update(next: FlipMenuOptions) {
      options = resolve(next)
      measure()
    },
    destroy() {
      cancelAnimationFrame(frame)
      window.removeEventListener('resize', measure)
    }
  }
}
