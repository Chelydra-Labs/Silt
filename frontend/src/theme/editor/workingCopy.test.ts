// Working-copy path reset, dirty tracking, spine clone, derived locks.

import { describe, expect, it, beforeEach, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const mocks = vi.hoisted(() => ({
  injectTokens: vi.fn(),
  restoreActiveTheme: vi.fn()
}))

vi.mock('../inject', () => ({ injectTokens: mocks.injectTokens }))
vi.mock('../store.svelte', () => ({
  restoreActiveTheme: mocks.restoreActiveTheme
}))

import {
  createWorkingCopy,
  setAtPath,
  getAtPath,
  colorsMatch
} from './workingCopy.svelte'
import { concreteEditorDefaults } from './concreteEditorDefaults'
import { deriveHover } from '../color'
import type { ThemeDoc } from '../types'

const appOnly: ThemeDoc = {
  schema_version: '2.0.0',
  id: 'app-only',
  name: 'App Only',
  modes: {
    dark: {
      surfaces: {
        app: { bg: '#0e0f12', border: '#1c1d24', text: '#dee3e6' }
      },
      hover: '#1b1d25',
      active: '#20232e',
      border_active: '#3e4153',
      border_focus: '#535770',
      text_muted: '#8b8b94',
      text_disabled: '#4b5563',
      accent: {
        primary: { start: '#2dd4bf', end: '#0d9488', glow: 'rgba(0,0,0,0.1)' },
        secondary: {
          start: '#6366f1',
          end: '#a855f7',
          glow: 'rgba(0,0,0,0.1)'
        }
      },
      status: { warn: '#e0b04a', danger: '#dd5a72', success: '#37b594' },
      error: { fg: '#e8728a', bg: '#171015', border: '#4a2a38' }
    },
    light: {
      surfaces: {
        app: { bg: '#f8fafc', border: '#e2e8f0', text: '#0f172a' }
      },
      hover: '#e2e8f0',
      active: '#cbd5e1',
      border_active: '#94a3b8',
      border_focus: '#64748b',
      text_muted: '#4d5667',
      text_disabled: '#94a3b8',
      accent: {
        primary: { start: '#0d9488', end: '#115e59', glow: 'rgba(0,0,0,0.1)' },
        secondary: {
          start: '#4f46e5',
          end: '#7c3aed',
          glow: 'rgba(0,0,0,0.1)'
        }
      },
      status: { warn: '#b45309', danger: '#be123c', success: '#0f766e' },
      error: { fg: '#be123c', bg: '#fff1f2', border: '#fecdd3' }
    }
  }
}

describe('workingCopy resetPath', () => {
  beforeEach(() => {
    mocks.injectTokens.mockReset()
    mocks.restoreActiveTheme.mockReset()
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })
  })

  it('deletes incomplete optional surfaces after partial leaf reset', () => {
    const wc = createWorkingCopy()
    wc.loadFromJson(JSON.stringify(appOnly))
    expect(wc.draft).not.toBeNull()

    wc.setAt('modes.dark.surfaces.sidebar', {
      bg: '#111',
      border: '#222',
      text: '#eee'
    })
    expect(wc.draft!.modes.dark.surfaces.sidebar).toBeTruthy()

    wc.resetPath('modes.dark.surfaces.sidebar.bg')
    expect(wc.draft!.modes.dark.surfaces.sidebar).toBeUndefined()
    expect(wc.draft!.modes.dark.surfaces).not.toHaveProperty('sidebar')
  })

  it('deletes the whole zone when resetPath targets the zone key', () => {
    const wc = createWorkingCopy()
    wc.loadFromJson(JSON.stringify(appOnly))

    wc.setAt('modes.dark.surfaces.sidebar', {
      bg: '#111',
      border: '#222',
      text: '#eee'
    })
    wc.resetPath('modes.dark.surfaces.sidebar')
    expect(wc.draft!.modes.dark.surfaces.sidebar).toBeUndefined()
    expect(wc.draft!.modes.dark.surfaces).not.toHaveProperty('sidebar')
  })
})

describe('workingCopy dirty + spine clone (#532)', () => {
  beforeEach(() => {
    mocks.injectTokens.mockReset()
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })
  })

  it('marks dirty on set and clears on resetAll', () => {
    const wc = createWorkingCopy()
    wc.loadFromJson(JSON.stringify(appOnly))
    expect(wc.dirty).toBe(false)
    wc.setColor('modes.dark.surfaces.app.bg', '#112233')
    expect(wc.dirty).toBe(true)
    wc.resetAll()
    expect(wc.dirty).toBe(false)
  })

  it('setAtPath does not mutate the previous root object', () => {
    const prev = structuredClone(appOnly)
    const next = setAtPath(prev, 'modes.dark.surfaces.app.bg', '#abcdef')
    expect(prev.modes.dark.surfaces.app.bg).toBe('#0e0f12')
    expect(getAtPath(next, 'modes.dark.surfaces.app.bg')).toBe('#abcdef')
    // Unrelated sibling shared by reference (spine clone).
    expect(next.modes.dark.accent).toBe(prev.modes.dark.accent)
  })

  it('slider-drag microbench stays under budget on a full embed theme', () => {
    const themePath = resolve(
      __dirname,
      '../../../../backend/themes/themes/cyber_forest.json'
    )
    const raw = readFileSync(themePath, 'utf8')
    const wc = createWorkingCopy()
    wc.loadFromJson(raw)
    const path = 'modes.dark.surfaces.app.bg'
    const N = 400
    const t0 = performance.now()
    for (let i = 0; i < N; i++) {
      // Vary L slightly via hex-ish strings that stay valid enough for setAt
      wc.setAt(
        path,
        `oklch(0.${(i % 50).toString().padStart(2, '0')} 0.05 180)`
      )
    }
    const ms = performance.now() - t0
    // Guardrail only — generous for CI; local machines are typically << 200ms.
    expect(ms).toBeLessThan(2000)
    expect(wc.mutationGen).toBeGreaterThan(N)
  })
})

describe('workingCopy derived locks (#529)', () => {
  beforeEach(() => {
    mocks.injectTokens.mockReset()
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0)
      return 1
    })
  })

  it('re-derives unlocked hover when app bg changes', () => {
    const wc = createWorkingCopy()
    wc.loadFromJson(JSON.stringify(appOnly))
    const seed = '#224466'
    wc.setAt('modes.dark.surfaces.app.bg', seed)
    const expected = deriveHover(seed)
    expect(wc.draft!.modes.dark.hover).toBe(expected)
  })

  it('locked derived survives seed edit', () => {
    const wc = createWorkingCopy()
    wc.loadFromJson(JSON.stringify(appOnly))
    const lockedHover = '#ff00aa'
    wc.setColor('modes.dark.hover', lockedHover)
    expect(wc.isDerivedLocked('modes.dark.hover')).toBe(true)
    wc.setAt('modes.dark.surfaces.app.bg', '#112233')
    expect(wc.draft!.modes.dark.hover).toBe(lockedHover)
  })

  it('resetDerivedToFormula unlocks and restores derivation', () => {
    const wc = createWorkingCopy()
    wc.loadFromJson(JSON.stringify(appOnly))
    wc.setColor('modes.dark.hover', '#ff00aa')
    wc.resetDerivedToFormula('modes.dark.hover')
    expect(wc.isDerivedLocked('modes.dark.hover')).toBe(false)
    const bg = wc.draft!.modes.dark.surfaces.app.bg
    expect(colorsMatch(wc.draft!.modes.dark.hover, deriveHover(bg)!)).toBe(true)
    expect(wc.isDerivedMatch('modes.dark.hover')).toBe(true)
  })
})

describe('concreteEditorDefaults', () => {
  it('builds concrete colors with no var() or color-mix()', () => {
    const defaults = concreteEditorDefaults(appOnly.modes.dark)
    const serialized = JSON.stringify(defaults)
    expect(serialized).not.toMatch(/var\(/)
    expect(serialized).not.toMatch(/color-mix\(/)
    expect(defaults.caret).toBe(appOnly.modes.dark.accent.primary.start)
    expect(defaults.selection).toBe(appOnly.modes.dark.accent.primary.glow)
    expect(defaults.selection_text).toBe(appOnly.modes.dark.surfaces.app.text)
    expect(defaults.link).toBe(appOnly.modes.dark.accent.secondary.start)
    expect(defaults.link_hover).toBe(appOnly.modes.dark.accent.secondary.end)
    expect(defaults.highlight).toBe(appOnly.modes.dark.status.warn)
  })
})
