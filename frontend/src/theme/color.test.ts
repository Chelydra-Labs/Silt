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
  contrastRatioWCAG
} from './color'

describe('color helper (#385)', () => {
  it('hex round-trips through toHex', () => {
    expect(toHex('#abcdef')).toBe('#abcdef')
    expect(toHex('#000000')).toBe('#000000')
    // 3-digit hex normalizes to 6-digit.
    expect(toHex('#fff')).toBe('#ffffff')
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
})
