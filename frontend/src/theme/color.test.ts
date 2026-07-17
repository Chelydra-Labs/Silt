// Unit coverage for the OKLCH color helper (#385, frontend half of Theme
// System v2). The derive math mirrors backend/themes/derivation.go; these
// tests pin the contract: format preservation, the L/chroma shift direction,
// and the WCAG ratio boundary.

import { describe, expect, it } from 'vitest'
import {
  toHex,
  toOklch,
  formatOklch,
  deriveHover,
  deriveActive,
  deriveDisabled,
  contrastRatioWCAG,
  deriveInkOnAccent,
  effectiveAccentFill
} from './color'

describe('color helper (#385)', () => {
  it('hex round-trips through toHex', () => {
    expect(toHex('#abcdef')).toBe('#abcdef')
    expect(toHex('#000000')).toBe('#000000')
    // 3-digit hex normalizes to 6-digit.
    expect(toHex('#fff')).toBe('#ffffff')
  })

  it('toHex drops alpha, producing opaque #rrggbb (Go canonical form)', () => {
    // rgba with alpha < 1 must still produce a 6-digit opaque hex — the Go
    // canonical form strips alpha, and toHex mirrors that.
    expect(toHex('rgba(0,0,0,0.5)')).toBe('#000000')
    // 8-digit hex (#rrggbbaa) must also collapse to opaque #rrggbb.
    expect(toHex('#ff000080')).toBe('#ff0000')
  })

  it('oklch parses and formatOklch produces a parseable string', () => {
    const lch = toOklch('oklch(0.7 0.15 250)')
    expect(lch).not.toBeNull()
    expect(lch!.L).toBeCloseTo(0.7, 5)
    expect(lch!.C).toBeCloseTo(0.15, 5)
    expect(lch!.H).toBeCloseTo(250, 5)

    const formatted = formatOklch(lch!)
    expect(formatted).toMatch(/^oklch\(/)
    // The formatted string must re-parse.
    expect(toOklch(formatted)).not.toBeNull()
    expect(toOklch(formatted)!.L).toBeCloseTo(0.7, 4)
  })

  it('greyscale hex toOklch/formatOklch does not throw; H is 0', () => {
    // Achromatic colors have undefined hue in culori — must not crash the editor.
    expect(() => toOklch('#808080')).not.toThrow()
    const lch = toOklch('#808080')
    expect(lch).not.toBeNull()
    expect(lch!.H).toBe(0)
    expect(lch!.C).toBeCloseTo(0, 4)
    expect(() => formatOklch(lch!)).not.toThrow()
    expect(formatOklch(lch!)).toMatch(/^oklch\(/)
  })

  it('deriveHover preserves the input format (hex in → hex out; oklch in → oklch out)', () => {
    const hexOut = deriveHover('#4a4a4a')
    expect(hexOut).not.toBeNull()
    expect(hexOut!.startsWith('#')).toBe(true)
    expect(hexOut!.startsWith('oklch(')).toBe(false)

    const oklchOut = deriveHover('oklch(0.5 0.1 30)')
    expect(oklchOut).not.toBeNull()
    expect(oklchOut!.startsWith('oklch(')).toBe(true)
  })

  it('deriveHover produces a perceptually lighter color for a mid-tone', () => {
    const seed = '#7a7a7a'
    const inputL = toOklch(seed)!.L
    const out = deriveHover(seed)!
    const outputL = toOklch(out)!.L
    expect(outputL).toBeGreaterThan(inputL)
  })

  it('deriveDisabled reduces chroma', () => {
    const seed = 'oklch(0.6 0.2 250)'
    const inputC = toOklch(seed)!.C
    const out = deriveDisabled(seed)!
    const outputC = toOklch(out)!.C
    expect(outputC).toBeLessThan(inputC)
    // Disabled scales by 0.4 — verify the magnitude, not just the direction.
    expect(outputC).toBeCloseTo(inputC * 0.4, 4)
  })

  it('deriveActive lowers lightness', () => {
    const seed = 'oklch(0.6 0.2 250)'
    const out = deriveActive(seed)!
    expect(toOklch(out)!.L).toBeLessThan(toOklch(seed)!.L)
  })

  it('contrastRatioWCAG returns 21 for pure black on white', () => {
    expect(contrastRatioWCAG('#000000', '#ffffff')).toBe(21)
  })

  it('contrastRatioWCAG returns null for unparseable input', () => {
    expect(contrastRatioWCAG('not-a-color', '#ffffff')).toBeNull()
    expect(contrastRatioWCAG('#000000', '!!!')).toBeNull()
  })

  it('derive helpers return null for unparseable input', () => {
    expect(deriveHover('nope')).toBeNull()
    expect(deriveActive('nope')).toBeNull()
    expect(deriveDisabled('nope')).toBeNull()
  })

  it('effectiveAccentFill composites translucent start over surface', () => {
    // rgba(255,255,255,0.1) on #0c0c0e → near-black, not solid white
    const fill = effectiveAccentFill('rgba(255,255,255,0.1)', '#0c0c0e')
    expect(fill).toMatch(/^#[0-9a-f]{6}$/i)
    // Painted fill must be dark (not #ffffff)
    expect(fill.toLowerCase()).not.toBe('#ffffff')
    const ink = deriveInkOnAccent(fill)
    expect(ink).toBe('#ffffff')
  })

  it('deriveInkOnAccent picks dark ink on solid teal', () => {
    const ink = deriveInkOnAccent('#0d9488')
    expect(ink === '#0a0a0a' || ink === '#000000').toBe(true)
  })

  it('opaque start passes through effectiveAccentFill', () => {
    expect(effectiveAccentFill('#0d9488', '#0c0c0e')).toBe('#0d9488')
  })
})
