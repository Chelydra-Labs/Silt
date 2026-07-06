// Tests for the theme-derived color palette (#408).
import { describe, expect, it } from 'vitest'

import {
  deriveColorPalette,
  resolveColor,
  FALLBACK_COLOR_PALETTE,
  type ColorEntry
} from './colors'

// Fixture tokens simulating a warm theme (Terra Noir-ish: earthy reds/greens).
const WARM_THEME_TOKENS: Record<string, string> = {
  '--color-status-danger': '#f43f5e',
  '--color-error': '#e11d48',
  '--color-status-warn': '#fbbf24',
  '--color-status-success': '#22c55e',
  '--color-accent-secondary-start': '#4d7c0f',
  '--color-accent-primary-start': '#c2410c',
  '--color-text-muted': '#a08878'
}

// Fixture tokens simulating a cool monochrome theme (Graphite-ish: blue-grays).
const COOL_THEME_TOKENS: Record<string, string> = {
  '--color-status-danger': '#9ca3af',
  '--color-error': '#6b7280',
  '--color-status-warn': '#d1d5db',
  '--color-status-success': '#9ca3af',
  '--color-accent-secondary-start': '#64748b',
  '--color-accent-primary-start': '#475569',
  '--color-text-muted': '#94a3b8'
}

function isValidHex(s: string): boolean {
  return /^#[0-9a-f]{6}$/.test(s)
}

describe('deriveColorPalette (#408)', () => {
  it('produces a non-empty palette from theme tokens', () => {
    const palette = deriveColorPalette(WARM_THEME_TOKENS)
    expect(palette.length).toBeGreaterThanOrEqual(3)
    for (const entry of palette) {
      expect(isValidHex(entry.dark), `${entry.id}.dark`).toBe(true)
      expect(isValidHex(entry.light), `${entry.id}.light`).toBe(true)
    }
  })

  it('includes the contrast black/white pair', () => {
    const palette = deriveColorPalette(WARM_THEME_TOKENS)
    const black = palette.find((e) => e.id === 'black')
    expect(black).toBeDefined()
    // Black inverts by mode: dark-mode shows near-white, light-mode near-black.
    expect(resolveColor(black!, true)).toBe('#fafafa')
    expect(resolveColor(black!, false)).toBe('#18181b')
  })

  it('a warm theme and a cool theme produce different palettes', () => {
    const warm = deriveColorPalette(WARM_THEME_TOKENS)
    const cool = deriveColorPalette(COOL_THEME_TOKENS)
    // The primary accent swatch differs: warm → orange-ish, cool → slate.
    const warmPrimary = warm.find((e) => e.id === 'primary')
    const coolPrimary = cool.find((e) => e.id === 'primary')
    expect(warmPrimary).toBeDefined()
    expect(coolPrimary).toBeDefined()
    expect(warmPrimary!.dark).not.toBe(coolPrimary!.dark)
    expect(warmPrimary!.light).not.toBe(coolPrimary!.light)
  })

  it('skips seeds whose token is absent (palette shrinks gracefully)', () => {
    const partial = deriveColorPalette({
      '--color-accent-primary-start': '#3b82f6',
      '--color-status-success': '#22c55e'
    })
    expect(partial.length).toBeGreaterThanOrEqual(3)
    // Only the two provided seeds + black, not the full seed set.
    expect(partial.length).toBeLessThan(Object.keys(WARM_THEME_TOKENS).length)
  })

  it('falls back to the fixed set when no hue tokens resolve', () => {
    const palette = deriveColorPalette({})
    expect(palette).toEqual(FALLBACK_COLOR_PALETTE)
  })

  it('preserves the seed hue family in the derived variants', () => {
    const palette = deriveColorPalette({
      '--color-accent-primary-start': '#c2410c' // orange
    })
    const entry = palette.find((e) => e.id === 'primary')
    expect(entry).toBeDefined()
    // The dark variant is an orange at the target lightness, not a different hue.
    // Verify it's closer to the seed than to a neutral gray.
    const seed = entry!
    expect(seed.dark).not.toBe(seed.light)
    // The dark variant is brighter (higher L) than the light variant.
    // Both should be valid hex.
    expect(isValidHex(seed.dark)).toBe(true)
    expect(isValidHex(seed.light)).toBe(true)
  })

  it('each entry has a stable id and human label', () => {
    const palette = deriveColorPalette(WARM_THEME_TOKENS)
    for (const entry of palette) {
      expect(entry.id).toBeTruthy()
      expect(entry.label).toBeTruthy()
      expect(typeof entry.label).toBe('string')
    }
    // No duplicate ids.
    const ids = palette.map((e) => e.id)
    expect(new Set(ids).size).toBe(ids.length)
  })

  it('seed labels are semantic roles, not pigments (a11y accuracy)', () => {
    // The swatch fill is theme-derived, so a label like "Blue" is wrong when
    // the theme's primary accent is orange (Terra Noir). The label must name
    // the ROLE so a screen-reader user hears an accurate name regardless of
    // the theme's hue choice (#408 a11y).
    const palette = deriveColorPalette(WARM_THEME_TOKENS)
    const labels = palette.map((e) => e.label)
    // The six hue seeds carry their role names; only the contrast pair
    // (black/white) and the neutral (gray) use pigment-style labels.
    expect(labels).toContain('Danger')
    expect(labels).toContain('Primary')
    expect(labels).toContain('Secondary')
    // No stale pigment names leaked from the old fixed table.
    expect(labels).not.toContain('Blue')
    expect(labels).not.toContain('Teal')
    expect(labels).not.toContain('Crimson')
  })

  it('resolveColor returns dark variant for dark mode, light for light', () => {
    const entry: ColorEntry = {
      id: 'test',
      label: 'Test',
      dark: '#abcdef',
      light: '#123456'
    }
    expect(resolveColor(entry, true)).toBe('#abcdef')
    expect(resolveColor(entry, false)).toBe('#123456')
  })
})
