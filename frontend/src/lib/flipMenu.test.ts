import { afterEach, describe, expect, it, vi } from 'vitest'
import { flipMenu, type FlipMenuOptions } from './flipMenu'

// jsdom ships no layout, so `getBoundingClientRect` is all zeros by default.
// Each test pins the anchor's rect + window height to drive the flip math
// deterministically — these are pure-math assertions, not pixel rendering.
function makeAnchor(rect: { top: number; bottom: number }): HTMLElement {
  const el = document.createElement('button')
  el.getBoundingClientRect = () =>
    ({
      top: rect.top,
      bottom: rect.bottom,
      left: 0,
      right: 0,
      width: 0,
      height: 0,
      x: 0,
      y: 0,
      toJSON() {}
    }) as DOMRect
  return el
}

function options(
  anchor: HTMLElement,
  overrides: Partial<FlipMenuOptions> = {}
): FlipMenuOptions {
  return { getAnchor: () => anchor, ...overrides }
}

/** Captures the placement decisions reported by `onPlacement`. */
function placementSpy(): {
  calls: boolean[]
  fn: (flipped: boolean) => void
} {
  const calls: boolean[] = []
  return { calls, fn: (flipped: boolean) => calls.push(flipped) }
}

const realInnerHeight = Object.getOwnPropertyDescriptor(window, 'innerHeight')

function setInnerHeight(px: number): void {
  Object.defineProperty(window, 'innerHeight', {
    configurable: true,
    writable: true,
    value: px
  })
}

afterEach(() => {
  if (realInnerHeight) {
    Object.defineProperty(window, 'innerHeight', realInnerHeight)
  }
})

describe('flipMenu', () => {
  it('opens below and clamps max-height to the available space below when it is the larger side', () => {
    setInnerHeight(800)
    // Anchor sits near the top: lots of room below, little above.
    const anchor = makeAnchor({ top: 100, bottom: 140 })
    const node = document.createElement('div')
    const placement = placementSpy()
    const handle = flipMenu(
      node,
      options(anchor, { maxHeightPx: 400, onPlacement: placement.fn })
    )

    // spaceBelow = 800 − 140 − 4 − 8 = 748 → capped at 400.
    expect(node.style.maxHeight).toBe('400px')
    // Reported "open below" (not flipped).
    expect(placement.calls.at(-1)).toBe(false)

    handle.destroy()
  })

  it('clamps below the soft cap when the viewport is the limiting factor', () => {
    setInnerHeight(300)
    const anchor = makeAnchor({ top: 100, bottom: 140 })
    const node = document.createElement('div')
    const handle = flipMenu(node, options(anchor, { maxHeightPx: 400 }))

    // spaceBelow = 300 − 140 − 12 = 148 (< 400 cap) → wins.
    expect(node.style.maxHeight).toBe('148px')

    handle.destroy()
  })

  it('flips above (reports flipped) and clamps to space-above when there is more room above', () => {
    setInnerHeight(800)
    // Anchor near the floor: tiny space below, plenty above.
    const anchor = makeAnchor({ top: 700, bottom: 740 })
    const node = document.createElement('div')
    const placement = placementSpy()
    const handle = flipMenu(
      node,
      options(anchor, { maxHeightPx: 400, onPlacement: placement.fn })
    )

    // spaceBelow = 800 − 740 − 12 = 48; spaceAbove = 700 − 12 = 688 → flip.
    expect(placement.calls.at(-1)).toBe(true)
    // Clamped to the smaller of cap (400) and spaceAbove (688).
    expect(node.style.maxHeight).toBe('400px')

    handle.destroy()
  })

  it('clamps space-above when it is smaller than the cap', () => {
    setInnerHeight(800)
    // Anchor near the floor with a moderate space-above.
    const anchor = makeAnchor({ top: 200, bottom: 740 })
    const node = document.createElement('div')
    const placement = placementSpy()
    const handle = flipMenu(
      node,
      options(anchor, { maxHeightPx: 400, onPlacement: placement.fn })
    )

    // spaceBelow = 48; spaceAbove = 188 → flip; 188 < 400 cap.
    expect(placement.calls.at(-1)).toBe(true)
    expect(node.style.maxHeight).toBe('188px')

    handle.destroy()
  })

  it('prefers below on a tie (matches the CSS default placement)', () => {
    setInnerHeight(500)
    // Symmetric: 200 above, 200 below (after gap/pad subtract equally).
    const anchor = makeAnchor({ top: 250, bottom: 250 })
    const node = document.createElement('div')
    const placement = placementSpy()
    const handle = flipMenu(
      node,
      options(anchor, { maxHeightPx: 400, onPlacement: placement.fn })
    )

    expect(placement.calls.at(-1)).toBe(false)

    handle.destroy()
  })

  it('leaves the CSS fallback in place when the anchor is not ready', () => {
    const node = document.createElement('div')
    const placement = placementSpy()
    const handle = flipMenu(node, {
      getAnchor: () => null,
      maxHeightPx: 400,
      onPlacement: placement.fn
    })

    // No inline override → the host's CSS max-height applies.
    expect(node.style.maxHeight).toBe('')
    // No placement decision reported until a measure can run.
    expect(placement.calls).toHaveLength(0)

    handle.destroy()
  })

  it('re-measures on window resize', () => {
    setInnerHeight(1000)
    // Anchor pinned near the top so both before/after it opens below — isolates
    // the resize-triggered re-clamp from the flip decision.
    const anchor = makeAnchor({ top: 10, bottom: 50 })
    const node = document.createElement('div')
    const placement = placementSpy()
    const handle = flipMenu(
      node,
      options(anchor, { maxHeightPx: 400, onPlacement: placement.fn })
    )

    // spaceBelow = 1000 − 50 − 12 = 938 → capped at 400.
    expect(node.style.maxHeight).toBe('400px')
    expect(placement.calls.at(-1)).toBe(false)

    // Viewport shrinks so space-below is now the limiter; the resize listener
    // re-measures + re-reports.
    const callsBefore = placement.calls.length
    setInnerHeight(200)
    window.dispatchEvent(new Event('resize'))
    // spaceBelow = 200 − 50 − 12 = 138.
    expect(node.style.maxHeight).toBe('138px')
    expect(placement.calls.length).toBeGreaterThan(callsBefore)

    handle.destroy()
  })

  it('removes the resize listener on destroy', () => {
    const removeSpy = vi.spyOn(window, 'removeEventListener')
    const anchor = makeAnchor({ top: 0, bottom: 0 })
    const handle = flipMenu(nodeForTest(), options(anchor))

    handle.destroy()
    expect(removeSpy).toHaveBeenCalledWith('resize', expect.any(Function))

    removeSpy.mockRestore()
  })

  it('update() refreshes options and re-measures', () => {
    setInnerHeight(800)
    const anchor = makeAnchor({ top: 100, bottom: 140 })
    const node = document.createElement('div')
    const handle = flipMenu(node, options(anchor, { maxHeightPx: 400 }))

    expect(node.style.maxHeight).toBe('400px')

    // Tighter cap → re-clamp downward.
    handle.update(options(anchor, { maxHeightPx: 100 }))
    expect(node.style.maxHeight).toBe('100px')

    handle.destroy()
  })
})

function nodeForTest(): HTMLElement {
  return document.createElement('div')
}
