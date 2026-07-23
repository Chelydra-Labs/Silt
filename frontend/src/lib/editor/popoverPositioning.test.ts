import { describe, it, expect } from 'vitest'
import {
  clampToViewport,
  findScrollableAncestor,
  flipOrClamp,
  POPOVER_MARGIN
} from './popoverPositioning'

describe('clampToViewport', () => {
  it('returns the input position when fully inside the viewport', () => {
    const result = clampToViewport(
      { x: 100, y: 100, width: 200, height: 100 },
      { width: 1000, height: 800 }
    )
    expect(result).toEqual({ left: 100, top: 100 })
  })

  it('shifts left when the popover would overflow the right edge', () => {
    const result = clampToViewport(
      { x: 900, y: 100, width: 200, height: 100 },
      { width: 1000, height: 800 }
    )
    // 900 + 200 = 1100 > 1000 → left = 1000 - 200 - 8 = 792
    expect(result.left).toBe(1000 - 200 - POPOVER_MARGIN)
    expect(result.top).toBe(100)
  })

  it('shifts up when the popover would overflow the bottom edge', () => {
    const result = clampToViewport(
      { x: 100, y: 750, width: 200, height: 100 },
      { width: 1000, height: 800 }
    )
    // 750 + 100 = 850 > 800 → top = 800 - 100 - 8 = 692
    expect(result.left).toBe(100)
    expect(result.top).toBe(800 - 100 - POPOVER_MARGIN)
  })

  it('enforces the 8px minimum on both axes', () => {
    const result = clampToViewport(
      { x: 0, y: 0, width: 100, height: 50 },
      { width: 1000, height: 800 }
    )
    expect(result).toEqual({ left: POPOVER_MARGIN, top: POPOVER_MARGIN })
  })

  it('clamps negative coordinates to the margin', () => {
    const result = clampToViewport(
      { x: -50, y: -30, width: 100, height: 50 },
      { width: 1000, height: 800 }
    )
    expect(result).toEqual({ left: POPOVER_MARGIN, top: POPOVER_MARGIN })
  })

  it('handles a popover larger than the viewport by pinning to top-left', () => {
    // Width 1200 > viewport 1000 → left would go negative; the Math.max
    // floor keeps it at the margin.
    const result = clampToViewport(
      { x: 500, y: 500, width: 1200, height: 900 },
      { width: 1000, height: 800 }
    )
    expect(result).toEqual({ left: POPOVER_MARGIN, top: POPOVER_MARGIN })
  })

  it('handles both axes overflowing simultaneously', () => {
    const result = clampToViewport(
      { x: 900, y: 750, width: 200, height: 100 },
      { width: 1000, height: 800 }
    )
    expect(result.left).toBe(1000 - 200 - POPOVER_MARGIN)
    expect(result.top).toBe(800 - 100 - POPOVER_MARGIN)
  })
})

describe('findScrollableAncestor', () => {
  it('returns document when el is null', () => {
    expect(findScrollableAncestor(null)).toBe(document)
  })

  it('returns the nearest overflow scroll/auto ancestor', () => {
    const outer = document.createElement('div')
    outer.style.overflow = 'hidden'
    const scroller = document.createElement('div')
    scroller.style.overflowY = 'auto'
    const inner = document.createElement('div')
    scroller.appendChild(inner)
    outer.appendChild(scroller)
    document.body.appendChild(outer)
    try {
      expect(findScrollableAncestor(inner)).toBe(scroller)
    } finally {
      outer.remove()
    }
  })

  it('falls back to document when no scrollable ancestor exists', () => {
    const el = document.createElement('div')
    document.body.appendChild(el)
    try {
      expect(findScrollableAncestor(el)).toBe(document)
    } finally {
      el.remove()
    }
  })
})

describe('flipOrClamp', () => {
  it('opens below the anchor when there is room', () => {
    const result = flipOrClamp(
      { top: 100, bottom: 120, left: 200 },
      { width: 256, height: 300 },
      { width: 1000, height: 800 }
    )
    expect(result.top).toBe(120) // anchor.bottom
    expect(result.left).toBe(200)
  })

  it('flips above the anchor when there is no room below', () => {
    // Anchor near the bottom: bottom(760) + height(300) > 800 - 8 → flip up.
    const result = flipOrClamp(
      { top: 720, bottom: 760, left: 200 },
      { width: 256, height: 300 },
      { width: 1000, height: 800 }
    )
    // top = anchor.top(720) - height(300) - margin(8) = 412
    expect(result.top).toBe(720 - 300 - POPOVER_MARGIN)
    expect(result.top).toBeLessThan(720)
  })

  it('does not flip above when there is also no room above (clamps instead)', () => {
    // Anchor so high that flipping up would go negative: prefer clamp-down so
    // the popover never overlaps the anchor by going off-screen the other way.
    const result = flipOrClamp(
      { top: 40, bottom: 60, left: 200 },
      { width: 256, height: 600 },
      { width: 1000, height: 500 }
    )
    expect(result.top).toBeGreaterThanOrEqual(POPOVER_MARGIN)
  })

  it('clamps horizontally on the right edge', () => {
    const result = flipOrClamp(
      { top: 100, bottom: 120, left: 900 },
      { width: 256, height: 300 },
      { width: 1000, height: 800 }
    )
    expect(result.left).toBe(1000 - 256 - POPOVER_MARGIN)
  })
})
