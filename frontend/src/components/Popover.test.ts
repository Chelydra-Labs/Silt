import { describe, expect, it, afterEach, vi } from 'vitest'
import { render, cleanup, fireEvent } from '@testing-library/svelte'
import PopoverHarness from './__test_helpers__/PopoverHarness.svelte'

// Component coverage for <Popover> (#376). jsdom has no real layout —
// getBoundingClientRect / offsetWidth / offsetHeight return 0 and Tailwind
// classes don't flow into element.style — so positioning assertions check the
// inline left/top px that measure() writes, and the load-bearing claim
// (portal-to-body escaping every overflow context) is proven by DOM ancestry.

function makeAnchor(): HTMLElement {
  const el = document.createElement('button')
  document.body.appendChild(el)
  return el
}

describe('Popover (#376)', () => {
  afterEach(() => {
    cleanup()
    // Remove any anchors the tests appended to body.
    document.querySelectorAll('body > button').forEach((b) => b.remove())
  })

  it('renders nothing when open=false', () => {
    const anchor = makeAnchor()
    render(PopoverHarness, {
      props: { anchor, open: false, onClose: () => {} }
    })
    expect(document.querySelector('[data-test-popover-content]')).toBeNull()
  })

  it('portals the content to document.body when open (escapes overflow)', () => {
    const anchor = makeAnchor()
    const { container } = render(PopoverHarness, {
      props: { anchor, open: true, onClose: () => {} }
    })
    const content = document.querySelector('[data-test-popover-content]')
    expect(content).not.toBeNull()
    // The portal's whole point: the floating content lives in document.body,
    // not inside the component's render subtree (so no ancestor overflow /
    // containing-block can clip it).
    expect(document.body.contains(content)).toBe(true)
    expect(container.contains(content)).toBe(false)
  })

  it('positions the floating layer with inline left/top px', () => {
    const anchor = makeAnchor()
    render(PopoverHarness, {
      props: { anchor, open: true, onClose: () => {} }
    })
    const content = document.querySelector(
      '[data-test-popover-content]'
    ) as HTMLElement
    const layer = content.parentElement as HTMLElement
    // measure() wrote inline coords — the layer is positioned, not at its
    // default off-screen (-9999) sentinel.
    expect(layer.style.left).toMatch(/px$/)
    expect(layer.style.top).toMatch(/px$/)
    expect(layer.style.left).not.toBe('-9999px')
  })

  it('clamps the floating layer inside the viewport', () => {
    const anchor = makeAnchor()
    // Anchor pinned past the bottom-right of an 800x600 viewport so
    // clampToViewport must pull the layer back on-screen.
    anchor.getBoundingClientRect = () => ({
      top: 1000,
      bottom: 1020,
      left: 1000,
      right: 1020,
      width: 20,
      height: 20,
      x: 1000,
      y: 1000,
      toJSON: () => ({})
    })
    Object.defineProperty(window, 'innerWidth', {
      value: 800,
      configurable: true
    })
    Object.defineProperty(window, 'innerHeight', {
      value: 600,
      configurable: true
    })
    render(PopoverHarness, {
      props: { anchor, open: true, onClose: () => {} }
    })
    const content = document.querySelector(
      '[data-test-popover-content]'
    ) as HTMLElement
    const layer = content.parentElement as HTMLElement
    const top = Number(layer.style.top.replace('px', ''))
    const left = Number(layer.style.left.replace('px', ''))
    // POPOVER_MARGIN (8) is the floor; viewport edge is the ceiling.
    expect(top).toBeGreaterThanOrEqual(8)
    expect(top).toBeLessThanOrEqual(600)
    expect(left).toBeGreaterThanOrEqual(8)
    expect(left).toBeLessThanOrEqual(800)
  })

  it('sets the layer width to the anchor width when matchWidth is true', () => {
    const anchor = makeAnchor()
    anchor.getBoundingClientRect = () => ({
      top: 10,
      bottom: 30,
      left: 10,
      right: 270,
      width: 260,
      height: 20,
      x: 10,
      y: 10,
      toJSON: () => ({})
    })
    render(PopoverHarness, {
      props: { anchor, open: true, onClose: () => {}, matchWidth: true }
    })
    const content = document.querySelector(
      '[data-test-popover-content]'
    ) as HTMLElement
    const layer = content.parentElement as HTMLElement
    expect(layer.style.width).toBe('260px')
  })

  it('calls onClose when the backdrop is clicked', async () => {
    const anchor = makeAnchor()
    const onClose = vi.fn()
    render(PopoverHarness, {
      props: { anchor, open: true, onClose }
    })
    // The backdrop is the full-viewport click-away layer portaled to body.
    const backdrop = document.querySelector('.fixed.inset-0') as HTMLElement
    expect(backdrop).toBeTruthy()
    await fireEvent.click(backdrop)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('calls onClose on Escape', async () => {
    const anchor = makeAnchor()
    const onClose = vi.fn()
    render(PopoverHarness, {
      props: { anchor, open: true, onClose }
    })
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
