<script lang="ts">
  import { tick, type Snippet } from 'svelte'
  import { portal } from '../lib/portal'
  import {
    clampToViewport,
    POPOVER_MARGIN,
    type Viewport
  } from '../lib/editor/popoverPositioning'

  /**
   * Shared floating-layer popover (#376). Renders its `content` snippet into a
   * portaled, `position: fixed` layer measured against `anchor`, so it escapes
   * any ancestor `overflow`/containing-block context (the recurrence dropdown
   * was clipped by CardDetailPanel's `overflow-y-auto`). Owns backdrop
   * click-away + Esc-to-close so callers don't reimplement them; the content
   * snippet owns its own semantics (role="listbox"/"menu", options, roving
   * tabindex) and its own arrow-key/Enter handling on the trigger.
   *
   * The `anchor` is a ref, not a trigger snippet: the trigger button stays
   * inline in the caller (it owns its aria + keyboard handler), and only the
   * floating content is owned here. Repositions on scroll (capture-phase, so
   * any ancestor scroll container is observed), on resize, and on anchor
   * resize, then clamps to the viewport via the shared clampToViewport helper.
   *
   * Future upgrade path: the native HTML `popover="auto"` attribute (Baseline
   * Widely Available since Apr 2025) would give top-layer + light-dismiss for
   * free and could let us drop the bespoke backdrop/Esc; it does not position
   * relative to the anchor, so the measured-coord logic stays. CSS Anchor
   * Positioning is still settling cross-browser, so portal+measure remains the
   * durable choice for now.
   */
  interface Props {
    open: boolean
    onClose: () => void
    anchor: HTMLElement | null
    content: Snippet
    /** Make the floating layer as wide as the anchor (dropdown-style). */
    matchWidth?: boolean
    /** Vertical gap between the anchor's bottom and the popover's top, in px. */
    gap?: number
    /**
     * Extra classes on the floating layer (caller styling). The wrapper owns
     * `max-h-[80vh] overflow-auto`: that caps the layer to the viewport (so
     * clampToViewport always has room to land) AND clips children to the
     * `rounded` corners (any non-`visible` overflow respects border-radius).
     * Do NOT pass a conflicting `overflow-*` utility here — two on one element
     * resolve by compiled-stylesheet source order, not class order, so the
     * winner is non-deterministic. If you need inner scrolling, put it on a
     * child element (DependencyPicker's `<ul class="overflow-y-auto">` does
     * this).
     */
    class?: string
  }

  let {
    open,
    onClose,
    anchor,
    content,
    matchWidth = false,
    gap = 4,
    class: klass = ''
  }: Props = $props()

  let popoverEl = $state<HTMLElement | null>(null)
  // Start off-screen so the first paint never flashes at the viewport origin;
  // measure() (called synchronously in the $effect) pins it to the anchor
  // before the browser paints.
  let left = $state(-9999)
  let top = $state(-9999)
  let width = $state<number | null>(null)

  function measure() {
    if (!anchor) return
    const rect = anchor.getBoundingClientRect()
    const pw = popoverEl?.offsetWidth ?? rect.width
    const ph = popoverEl?.offsetHeight ?? 0
    const viewport: Viewport = {
      width: window.innerWidth,
      height: window.innerHeight
    }
    const clamped = clampToViewport(
      { x: rect.left, y: rect.bottom + gap, width: pw, height: ph },
      viewport
    )
    left = clamped.left
    top = clamped.top
    if (matchWidth) {
      // Floor the anchor width but keep it within the viewport.
      width = Math.min(rect.width, viewport.width - 2 * POPOVER_MARGIN)
    }
  }

  // Position on open / anchor change; re-position on any ancestor scroll,
  // viewport resize, or anchor resize. The capture-phase scroll listener
  // catches scrolls bubbling from every overflow container up to document.
  $effect(() => {
    if (!open || !anchor) return
    measure()
    void tick().then(measure)
    const onScroll = () => measure()
    document.addEventListener('scroll', onScroll, {
      capture: true,
      passive: true
    })
    window.addEventListener('resize', onScroll, { passive: true })
    // ResizeObserver is optional (absent in jsdom; present in the webview) —
    // it only adds anchor-resize re-measurement on top of the scroll/resize
    // listeners, so feature-detect rather than depend on it.
    const ro =
      typeof ResizeObserver !== 'undefined'
        ? new ResizeObserver(() => measure())
        : null
    ro?.observe(anchor)
    return () => {
      document.removeEventListener('scroll', onScroll, { capture: true })
      window.removeEventListener('resize', onScroll)
      ro?.disconnect()
    }
  })

  // Esc closes (fallback for when focus is not on the trigger, which keeps its
  // own stopPropagation Esc handler). Bound only while open. stopPropagation
  // so a parent window Esc handler (e.g. PropertiesPanel) does not also fire
  // and dismiss the whole panel when only the dropdown should close.
  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      onClose()
    }
  }
  // Outside-click close via document mousedown (not a full-viewport click
  // catcher): a pointer-events:none backdrop lets the anchor input receive
  // caret clicks while the dropdown is open (PageLinkField combobox).
  function onDocMouseDown(e: MouseEvent) {
    const t = e.target
    if (!(t instanceof Node)) return
    if (popoverEl?.contains(t)) return
    if (anchor?.contains(t)) return
    onClose()
  }
  $effect(() => {
    if (!open) return
    // Capture-phase Esc so we run before parent window handlers (e.g.
    // PropertiesPanel) and stopPropagation actually prevents the panel close.
    window.addEventListener('keydown', onKeydown, true)
    // Capture so we see the event before focus moves; skip anchor + content.
    document.addEventListener('mousedown', onDocMouseDown, true)
    return () => {
      window.removeEventListener('keydown', onKeydown, true)
      document.removeEventListener('mousedown', onDocMouseDown, true)
    }
  })
</script>

{#if open && anchor}
  <!-- The whole layer is portaled to document.body so neither the backdrop nor
       the content is clipped by the caller's overflow/stacking context. -->
  <div use:portal>
    <!-- pointer-events:none: visual stacking only; outside-click is handled by
         onDocMouseDown so the anchor (e.g. combobox input) stays clickable. -->
    <div
      class="fixed inset-0 z-[100] pointer-events-none"
      aria-hidden="true"
    ></div>
    <div
      bind:this={popoverEl}
      class="fixed z-[101] max-h-[80vh] overflow-auto {klass}"
      style:left="{left}px"
      style:top="{top}px"
      style:width={width !== null ? `${width}px` : null}
    >
      {@render content()}
    </div>
  </div>
{/if}
