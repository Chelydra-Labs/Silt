// Registry invariants for the preloaded font picker (#82). The registry is
// the single source of truth for both the General-tab <select> and the
// Appearance-tab typography indicator, so its shape is pinned here: the
// defaults exist, system fallbacks exist, ids are unique, and the helper
// selectors return the right slices.

import { describe, expect, it } from 'vitest'
import {
  FONT_REGISTRY,
  DEFAULT_BODY_ID,
  DEFAULT_MONO_ID,
  DEFAULT_HEADLINE_ID,
  bundledByCategory,
  systemFonts,
  findByCssFamily,
  displayNameForCssFamily
} from './fonts'

describe('font registry (#82)', () => {
  it('includes the curated sans/mono/display families plus system fallbacks', () => {
    const ids = new Set(FONT_REGISTRY.map((f) => f.id))
    // Sans body fonts (issue #82 minimum set).
    for (const id of ['plus-jakarta-sans', 'inter', 'lexend', 'work-sans', 'manrope']) {
      expect(ids.has(id), `expected ${id}`).toBe(true)
    }
    // Monospace fonts.
    for (const id of ['jetbrains-mono', 'fira-code', 'ibm-plex-mono', 'space-mono']) {
      expect(ids.has(id), `expected ${id}`).toBe(true)
    }
    // Display / headline fonts.
    for (const id of ['hanken-grotesk', 'sora', 'bricolage-grotesque']) {
      expect(ids.has(id), `expected ${id}`).toBe(true)
    }
    // System fallbacks are always present (offline).
    for (const id of ['system-ui', 'sans-serif', 'monospace']) {
      expect(ids.has(id), `expected system fallback ${id}`).toBe(true)
    }
  })

  it('the three defaults are bundled and resolve to the canonical families', () => {
    const body = findByCssFamily('Plus Jakarta Sans')
    const mono = findByCssFamily('JetBrains Mono')
    const headline = findByCssFamily('Hanken Grotesk')
    expect(body?.id).toBe(DEFAULT_BODY_ID)
    expect(mono?.id).toBe(DEFAULT_MONO_ID)
    expect(headline?.id).toBe(DEFAULT_HEADLINE_ID)
    // Defaults are bundled (not system) so they render offline.
    expect(body?.source).toBe('bundled')
    expect(mono?.source).toBe('bundled')
    expect(headline?.source).toBe('bundled')
  })

  it('has unique ids and a non-empty cssFamily per entry', () => {
    const seen = new Set<string>()
    for (const f of FONT_REGISTRY) {
      expect(seen.has(f.id), `duplicate id ${f.id}`).toBe(false)
      seen.add(f.id)
      expect(f.cssFamily.length, `empty cssFamily for ${f.id}`).toBeGreaterThan(0)
      expect(f.displayName.length, `empty displayName for ${f.id}`).toBeGreaterThan(0)
    }
  })

  it('bundledByCategory returns only bundled entries of that category', () => {
    const sans = bundledByCategory('sans')
    const mono = bundledByCategory('mono')
    const display = bundledByCategory('display')
    expect(sans.every((f) => f.source === 'bundled' && f.category === 'sans')).toBe(true)
    expect(mono.every((f) => f.source === 'bundled' && f.category === 'mono')).toBe(true)
    expect(display.every((f) => f.source === 'bundled' && f.category === 'display')).toBe(true)
    // System fallbacks are never returned by bundledByCategory.
    expect(sans.some((f) => f.source === 'system')).toBe(false)
    // Each category has at least one bundled family.
    expect(sans.length).toBeGreaterThan(0)
    expect(mono.length).toBeGreaterThan(0)
    expect(display.length).toBeGreaterThan(0)
  })

  it('systemFonts returns only system-source entries', () => {
    const sys = systemFonts()
    expect(sys.length).toBeGreaterThan(0)
    expect(sys.every((f) => f.source === 'system')).toBe(true)
  })

  it('displayNameForCssFamily falls back to the raw value for unknown families', () => {
    expect(displayNameForCssFamily('Plus Jakarta Sans')).toBe('Plus Jakarta Sans')
    // A hand-edited config value not in the registry is shown verbatim
    // (the picker never blanks a value it doesn't curate).
    expect(displayNameForCssFamily('Some Custom Font')).toBe('Some Custom Font')
  })
})
