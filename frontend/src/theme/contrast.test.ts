// Contrast classification + auto-fix for the theme editor.

import { describe, expect, it } from 'vitest'
import {
  autoFixLightness,
  classifyContrast,
  contrastRatioWCAG,
  coreContrastPairs,
  effectiveBackgroundWithScrim
} from './contrast'

describe('classifyContrast', () => {
  it('classifies text AA bands', () => {
    expect(classifyContrast(7)).toBe('pass')
    expect(classifyContrast(4.5)).toBe('pass')
    expect(classifyContrast(4.2)).toBe('warn')
    expect(classifyContrast(4.0)).toBe('warn')
    expect(classifyContrast(3.9)).toBe('fail')
    expect(classifyContrast(null)).toBe('fail')
  })

  it('classifies UI chrome bands when text=false', () => {
    expect(classifyContrast(3.0, false)).toBe('pass')
    expect(classifyContrast(2.7, false)).toBe('warn')
    expect(classifyContrast(2.0, false)).toBe('fail')
  })
})

describe('autoFixLightness', () => {
  it('raises L for light-on-dark until AA is met', () => {
    // Mid grey on near-black is below AA for body text.
    const fg = 'oklch(0.45 0.02 250)'
    const bg = 'oklch(0.15 0.02 250)'
    const before = contrastRatioWCAG(fg, bg)!
    expect(before).toBeLessThan(4.5)

    const fixed = autoFixLightness(fg, bg, 4.5)
    expect(fixed).not.toBeNull()
    const after = contrastRatioWCAG(fixed!, bg)!
    expect(after).toBeGreaterThanOrEqual(4.5)
    // Polarity preserved: still lighter than bg.
    expect(fixed!.startsWith('oklch(')).toBe(true)
  })

  it('lowers L for dark-on-light until AA is met', () => {
    const fg = 'oklch(0.65 0.02 250)'
    const bg = 'oklch(0.97 0.01 250)'
    const before = contrastRatioWCAG(fg, bg)!
    expect(before).toBeLessThan(4.5)

    const fixed = autoFixLightness(fg, bg, 4.5)
    expect(fixed).not.toBeNull()
    expect(contrastRatioWCAG(fixed!, bg)!).toBeGreaterThanOrEqual(4.5)
  })

  it('returns null when already passing', () => {
    expect(autoFixLightness('#ffffff', '#000000', 4.5)).toBeNull()
  })
})

describe('coreContrastPairs', () => {
  it('measures the five core pairs from a token map', () => {
    const pairs = coreContrastPairs({
      '--color-surface-app': '#0e0f12',
      '--color-surface-app-text': '#dee3e6',
      '--color-text-muted': '#8b8b94',
      '--color-accent-primary-start': '#2dd4bf',
      '--color-error': '#e8728a',
      '--color-error-bg': '#171015',
      '--color-surface-editor': '#111216',
      '--color-surface-editor-text': '#dee3e6'
    })
    expect(pairs).toHaveLength(5)
    expect(pairs.map((p) => p.id)).toEqual([
      'app-text',
      'muted-text',
      'accent',
      'error',
      'editor-text'
    ])
    for (const p of pairs) {
      expect(p.ratio).not.toBeNull()
      expect(['pass', 'warn', 'fail']).toContain(p.level)
    }
  })

  it('resolves var() inheritance for editor text', () => {
    const pairs = coreContrastPairs({
      '--color-surface-app': '#0e0f12',
      '--color-surface-app-text': '#dee3e6',
      '--color-text-muted': '#8b8b94',
      '--color-accent-primary-start': '#2dd4bf',
      '--color-error': '#e8728a',
      '--color-error-bg': '#171015',
      '--color-surface-editor': 'var(--color-surface-app)',
      '--color-surface-editor-text': 'var(--color-surface-app-text)'
    })
    const editor = pairs.find((p) => p.id === 'editor-text')!
    expect(editor.ratio).not.toBeNull()
    expect(editor.level).toBe('pass')
  })

  it('uses scrim as effective bg for app/editor pairs when present', () => {
    const pairs = coreContrastPairs({
      '--color-surface-app': '#0e0f12',
      '--color-surface-app-text': '#dee3e6',
      '--color-text-muted': '#8b8b94',
      '--color-accent-primary-start': '#2dd4bf',
      '--color-error': '#e8728a',
      '--color-error-bg': '#171015',
      '--color-surface-editor': '#111216',
      '--color-surface-editor-text': '#dee3e6',
      '--silt-bg-app-image': 'url("asset://bg.png")',
      '--silt-bg-app-scrim': '#000000',
      '--silt-bg-editor-image': 'url("asset://ed.png")',
      '--silt-bg-editor-scrim': '#111111'
    })
    const app = pairs.find((p) => p.id === 'app-text')!
    const editor = pairs.find((p) => p.id === 'editor-text')!
    expect(app.bg).toBe('#000000')
    expect(editor.bg).toBe('#111111')
  })
})

describe('effectiveBackgroundWithScrim', () => {
  it('prefers scrim when set; otherwise solid fallback', () => {
    expect(
      effectiveBackgroundWithScrim('url(x)', '#0a0a0a', 0.4, '#ffffff')
    ).toBe('#0a0a0a')
    expect(
      effectiveBackgroundWithScrim('url(x)', undefined, 0.4, '#ffffff')
    ).toBe('#ffffff')
    expect(effectiveBackgroundWithScrim(undefined, '  ', 0.2, '#abc')).toBe(
      '#abc'
    )
  })
})
