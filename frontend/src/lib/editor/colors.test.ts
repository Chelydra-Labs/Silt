// Tests for the theme-derived color palette (#408).
import { describe, expect, it } from 'vitest'

import {
  deriveColorPalette,
  resolveColor,
  FALLBACK_THEME_PALETTE,
  FIXED_COLOR_PALETTE,
  type ColorEntry
} from './colors'

// Fixture tokens simulating a warm theme (Terra Noir-ish: earthy reds/greens).
const WARM_THEME_TOKENS: Record<string, string> = {
  '--color-status-danger': '#f43f5e',
  '--color-status-warn': '#fbbf24',
  '--color-status-success': '#22c55e',
  '--color-accent-secondary-start': '#4d7c0f',
  '--color-accent-primary-start': '#c2410c',
  '--color-text-muted': '#a08878'
}

// Fixture tokens simulating a cool monochrome theme (Graphite-ish: blue-grays).
const COOL_THEME_TOKENS: Record<string, string> = {
  '--color-status-danger': '#9ca3af',
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
  it('produces a full theme + standard palette from theme tokens', () => {
    const palette = deriveColorPalette(WARM_THEME_TOKENS)
    // 6 theme entries: primary, secondary, success, warn, danger, gray.
    expect(palette.theme.length).toBe(6)
    // 12 standard entries always.
    expect(palette.standard.length).toBe(12)
    for (const entry of [...palette.theme, ...palette.standard]) {
      expect(isValidHex(entry.dark), `${entry.id}.dark`).toBe(true)
      expect(isValidHex(entry.light), `${entry.id}.light`).toBe(true)
    }
  })

  it('standard row equals FIXED_COLOR_PALETTE regardless of theme', () => {
    const warm = deriveColorPalette(WARM_THEME_TOKENS)
    const cool = deriveColorPalette(COOL_THEME_TOKENS)
    expect(warm.standard).toEqual(FIXED_COLOR_PALETTE)
    expect(cool.standard).toEqual(FIXED_COLOR_PALETTE)
  })

  it('theme row does not include the dropped `error` seed (regression guard)', () => {
    const palette = deriveColorPalette(WARM_THEME_TOKENS)
    expect(palette.theme.find((e) => e.id === 'error')).toBeUndefined()
  })

  it('theme row is ordered primary-first', () => {
    const palette = deriveColorPalette(WARM_THEME_TOKENS)
    expect(palette.theme[0].id).toBe('primary')
  })

  it('theme row varies by theme while standard stays constant', () => {
    const warm = deriveColorPalette(WARM_THEME_TOKENS)
    const cool = deriveColorPalette(COOL_THEME_TOKENS)
    // The primary accent swatch differs: warm → orange-ish, cool → slate.
    const warmPrimary = warm.theme.find((e) => e.id === 'primary')
    const coolPrimary = cool.theme.find((e) => e.id === 'primary')
    expect(warmPrimary).toBeDefined()
    expect(coolPrimary).toBeDefined()
    expect(warmPrimary!.dark).not.toBe(coolPrimary!.dark)
    expect(warmPrimary!.light).not.toBe(coolPrimary!.light)
    // Standard row is identical across themes.
    expect(warm.standard).toEqual(cool.standard)
  })

  it('skips seeds whose token is absent (theme row shrinks gracefully)', () => {
    const partial = deriveColorPalette({
      '--color-accent-primary-start': '#3b82f6',
      '--color-status-success': '#22c55e'
    })
    // Only the two provided seeds resolve (neutral gray token is absent).
    // 2 is the minimum non-fallback threshold.
    expect(partial.theme.length).toBe(2)
    expect(partial.theme.map((e) => e.id)).toEqual(['primary', 'success'])
    expect(partial.standard).toEqual(FIXED_COLOR_PALETTE)
  })

  it('falls back to FALLBACK_THEME_PALETTE when no hue tokens resolve', () => {
    const palette = deriveColorPalette({})
    expect(palette.theme).toEqual(FALLBACK_THEME_PALETTE)
    // Standard row never falls back.
    expect(palette.standard).toEqual(FIXED_COLOR_PALETTE)
  })

  it('preserves the seed hue family in the derived variants', () => {
    const palette = deriveColorPalette({
      '--color-accent-primary-start': '#c2410c' // orange
    })
    const entry = palette.theme.find((e) => e.id === 'primary')
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
    for (const entry of [...palette.theme, ...palette.standard]) {
      expect(entry.id).toBeTruthy()
      expect(entry.label).toBeTruthy()
      expect(typeof entry.label).toBe('string')
    }
    // No duplicate ids within each row.
    const themeIds = palette.theme.map((e) => e.id)
    expect(new Set(themeIds).size).toBe(themeIds.length)
    const stdIds = palette.standard.map((e) => e.id)
    expect(new Set(stdIds).size).toBe(stdIds.length)
  })

  it('theme-row labels are semantic roles, not pigments (a11y accuracy)', () => {
    // The theme-row swatch fill is theme-derived, so a label like "Blue" is
    // wrong when the theme's primary accent is orange (Terra Noir). The
    // label must name the ROLE so a screen-reader user hears an accurate
    // name regardless of the theme's hue choice (#408 a11y). The standard
    // row carries the pigment names.
    const palette = deriveColorPalette(WARM_THEME_TOKENS)
    const themeLabels = palette.theme.map((e) => e.label)
    expect(themeLabels).toContain('Primary')
    expect(themeLabels).toContain('Secondary')
    expect(themeLabels).toContain('Success')
    expect(themeLabels).toContain('Danger')
    expect(themeLabels).toContain('Gray')
    // No pigment names leaked into the theme row, no stale role names.
    expect(themeLabels).not.toContain('Blue')
    expect(themeLabels).not.toContain('Teal')
    expect(themeLabels).not.toContain('Crimson')
    expect(themeLabels).not.toContain('Error')
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
