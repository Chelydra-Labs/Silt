// OklchColorField: 2D LC plane, hue strip, channel sliders disclosure (#528).

import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  within
} from '@testing-library/svelte'
import OklchColorField from './OklchColorField.svelte'
import { toOklch } from '../color'

function mockRect(
  el: Element,
  rect: { left: number; top: number; width: number; height: number }
) {
  vi.spyOn(el, 'getBoundingClientRect').mockReturnValue({
    left: rect.left,
    top: rect.top,
    width: rect.width,
    height: rect.height,
    right: rect.left + rect.width,
    bottom: rect.top + rect.height,
    x: rect.left,
    y: rect.top,
    toJSON: () => ({})
  } as DOMRect)
}

describe('OklchColorField (#528)', () => {
  beforeEach(() => {
    // jsdom lacks pointer capture APIs used by the plane / hue strip.
    Element.prototype.setPointerCapture = vi.fn()
    Element.prototype.releasePointerCapture = vi.fn()
    Element.prototype.hasPointerCapture = vi.fn(() => false)
  })

  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it('opens popover with LC plane, hue strip, and channel sliders in details', async () => {
    const onchange = vi.fn()
    render(OklchColorField, {
      props: {
        label: 'Accent',
        value: 'oklch(0.6500 0.1500 250.00)',
        onchange
      }
    })

    expect(screen.queryByTestId('oklch-lc-plane')).toBeNull()

    await fireEvent.click(
      screen.getByRole('button', { name: /Accent color swatch/i })
    )
    await tick()

    const plane = screen.getByTestId('oklch-lc-plane')
    expect(plane).toBeTruthy()
    expect(plane.getAttribute('aria-label')).toBe('Accent lightness and chroma')
    expect(plane.getAttribute('role')).toBe('slider')
    expect(plane.getAttribute('aria-valuetext')).toMatch(/percent lightness/i)

    expect(screen.getByTestId('oklch-hue-strip')).toBeTruthy()

    const details = screen.getByText('Channel sliders').closest('details')
    expect(details).toBeTruthy()
    const panel = details as HTMLDetailsElement
    // Sliders live inside details (keyboard path); may be closed by default.
    expect(within(panel).getByLabelText('Lightness')).toBeTruthy()
    expect(within(panel).getByLabelText('Chroma')).toBeTruthy()
    expect(within(panel).getByLabelText('Hue')).toBeTruthy()
  })

  it('updates color when the LC plane is pointer-dragged', async () => {
    const onchange = vi.fn()
    render(OklchColorField, {
      props: {
        label: 'Accent',
        value: 'oklch(0.6500 0.1500 250.00)',
        onchange
      }
    })

    await fireEvent.click(
      screen.getByRole('button', { name: /Accent color swatch/i })
    )
    await tick()

    const plane = screen.getByTestId('oklch-lc-plane')
    mockRect(plane, { left: 0, top: 0, width: 200, height: 100 })

    // Midpoint → L≈0.5, C≈0.2 (x=0.5 * 0.4)
    await fireEvent.pointerDown(plane, {
      clientX: 100,
      clientY: 50,
      pointerId: 1,
      button: 0
    })
    await tick()

    expect(onchange).toHaveBeenCalled()
    const next = onchange.mock.calls.at(-1)?.[0] as string
    const lch = toOklch(next)
    expect(lch).toBeTruthy()
    expect(lch!.L).toBeCloseTo(0.5, 2)
    expect(lch!.C).toBeCloseTo(0.2, 2)
    // Hue preserved from seed.
    expect(lch!.H).toBeCloseTo(250, 0)
  })

  it('updates hue when the hue strip is pointer-dragged', async () => {
    const onchange = vi.fn()
    render(OklchColorField, {
      props: {
        label: 'Accent',
        value: 'oklch(0.6500 0.1500 250.00)',
        onchange
      }
    })

    await fireEvent.click(
      screen.getByRole('button', { name: /Accent color swatch/i })
    )
    await tick()

    const strip = screen.getByTestId('oklch-hue-strip')
    mockRect(strip, { left: 0, top: 0, width: 360, height: 16 })

    // 90/360 → hue 90
    await fireEvent.pointerDown(strip, {
      clientX: 90,
      clientY: 8,
      pointerId: 1,
      button: 0
    })
    await tick()

    expect(onchange).toHaveBeenCalled()
    const next = onchange.mock.calls.at(-1)?.[0] as string
    const lch = toOklch(next)
    expect(lch).toBeTruthy()
    expect(lch!.H).toBeCloseTo(90, 0)
  })

  it('closes on Escape and restores focus to the swatch', async () => {
    const onchange = vi.fn()
    render(OklchColorField, {
      props: {
        label: 'Accent',
        value: 'oklch(0.6500 0.1500 250.00)',
        onchange
      }
    })

    const swatch = screen.getByRole('button', { name: /Accent color swatch/i })
    await fireEvent.click(swatch)
    await tick()
    expect(screen.getByTestId('oklch-lc-plane')).toBeTruthy()

    await fireEvent.keyDown(document, { key: 'Escape' })
    await tick()

    expect(screen.queryByTestId('oklch-lc-plane')).toBeNull()
    expect(document.activeElement).toBe(swatch)
  })

  it('keeps hex/oklch text field visible outside the popover', async () => {
    render(OklchColorField, {
      props: {
        label: 'Accent',
        value: 'oklch(0.6500 0.1500 250.00)',
        onchange: vi.fn()
      }
    })

    const text = screen.getByLabelText('Accent color value') as HTMLInputElement
    expect(text).toBeTruthy()
    expect(text.value).toMatch(/oklch/)

    await fireEvent.click(
      screen.getByRole('button', { name: /Accent color swatch/i })
    )
    await tick()
    // Still present while popover is open.
    expect(screen.getByLabelText('Accent color value')).toBeTruthy()
  })

  it('channel sliders call onchange via the advanced details path', async () => {
    const onchange = vi.fn()
    render(OklchColorField, {
      props: {
        label: 'Accent',
        value: 'oklch(0.6500 0.1500 250.00)',
        onchange
      }
    })

    await fireEvent.click(
      screen.getByRole('button', { name: /Accent color swatch/i })
    )
    await tick()

    const details = screen.getByText('Channel sliders').closest('details')!
    details.open = true
    await tick()

    const lSlider = within(details).getByLabelText(
      'Lightness'
    ) as HTMLInputElement
    await fireEvent.input(lSlider, { target: { value: '40' } })
    await tick()

    expect(onchange).toHaveBeenCalled()
    const next = onchange.mock.calls.at(-1)?.[0] as string
    const lch = toOklch(next)
    expect(lch!.L).toBeCloseTo(0.4, 2)
  })
})
