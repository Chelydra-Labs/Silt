// Tests for the hand-curated settings search index.
//
// The headline guarantee: every `sectionId` in the static index must exist in
// getSettingsSections() (guards against index drift — a renamed/removed
// section would otherwise silently break search jumps). Plus a few
// representative query→match assertions so the matcher behaviour is pinned.
import { describe, expect, it, beforeEach, vi } from 'vitest'

// Hoisted mocks match the canonical pattern (AppearanceTab.test.ts): plain
// objects + vi.hoisted so the vi.mock factories resolve.
const mocks = vi.hoisted(() => ({
  loadedPlugins: {
    plugins: new Map<string, unknown>(),
    errors: [] as { id: string; message: string }[]
  },
  settings: { config: { ui: {} } },
  surfaces: [] as { pluginID: string }[]
}))

vi.mock('../../plugins/store.svelte', () => ({
  loadedPlugins: mocks.loadedPlugins
}))
vi.mock('../../settings/store.svelte', () => ({ settings: mocks.settings }))
vi.mock('../../plugins/surfaces', () => ({
  getSurfaces: vi.fn(() => mocks.surfaces as never),
  onSurfacesChanged: vi.fn(() => () => {})
}))

import { searchSettings, getSettingsIndex } from './settingsIndex'
import {
  getSettingsSections,
  resolveSettingsSectionId,
  FALLBACK_SETTINGS_SECTION
} from './settingsSections.svelte'

describe('settingsIndex — sectionId validity (drift guard)', () => {
  beforeEach(() => {
    mocks.loadedPlugins.plugins.clear()
    mocks.loadedPlugins.errors = []
    mocks.settings.config = { ui: {} }
    mocks.surfaces = []
  })

  it('every static entry sectionId exists in getSettingsSections()', () => {
    const validIds = new Set(getSettingsSections().map((s) => s.id))
    for (const entry of getSettingsIndex()) {
      expect(
        validIds.has(entry.sectionId),
        `unknown sectionId: ${entry.sectionId}`
      ).toBe(true)
    }
  })

  it('plugin entries appear when a plugin registers a settings surface', () => {
    mocks.loadedPlugins.plugins.set('my-plugin', {
      manifest: { id: 'my-plugin', name: 'My Plugin' },
      settingsPageComponent: vi.fn(),
      component: vi.fn(),
      source: 'first-party'
    })
    const ids = getSettingsIndex().map((e) => e.sectionId)
    expect(ids).toContain('plugin:my-plugin')
  })

  it('finds the template management destination', () => {
    expect(searchSettings('template')).toEqual(
      expect.arrayContaining([
        expect.objectContaining({ sectionId: 'templates' })
      ])
    )
  })
})

describe('resolveSettingsSectionId — invalid-section fallback', () => {
  // Pins the contract that a bad section id never renders a blank panel or a
  // dangling aria-labelledby: both the open-settings path (PluginContext →
  // App.openSettings) and the in-view jump fall back to 'general'.
  const known = ['general', 'editor', 'appearance', 'plugins']

  it('returns the requested id when it is known', () => {
    expect(resolveSettingsSectionId('appearance', known)).toBe('appearance')
    expect(resolveSettingsSectionId('plugins', known)).toBe('plugins')
  })

  it('falls back when the id is unknown', () => {
    expect(resolveSettingsSectionId('typo', known)).toBe(
      FALLBACK_SETTINGS_SECTION
    )
    expect(resolveSettingsSectionId('plugin:nope', known)).toBe(
      FALLBACK_SETTINGS_SECTION
    )
  })

  it('maps legacy AI plugin section ids to Settings → AI', () => {
    const knownWithAi = [...known, 'ai']
    expect(resolveSettingsSectionId('plugin:silt-ai-qa', knownWithAi)).toBe(
      'ai'
    )
    expect(
      resolveSettingsSectionId('plugin:silt-ai-summary', knownWithAi)
    ).toBe('ai')
    expect(
      resolveSettingsSectionId('plugin:silt-ai-assistant', knownWithAi)
    ).toBe('ai')
  })

  it('falls back when the id is absent/empty', () => {
    expect(resolveSettingsSectionId(undefined, known)).toBe(
      FALLBACK_SETTINGS_SECTION
    )
    expect(resolveSettingsSectionId('', known)).toBe(FALLBACK_SETTINGS_SECTION)
    expect(resolveSettingsSectionId(null, known)).toBe(
      FALLBACK_SETTINGS_SECTION
    )
  })

  it('resolves against the live registry (every real section id passes)', () => {
    const live = getSettingsSections().map((s) => s.id)
    for (const id of live) {
      expect(resolveSettingsSectionId(id, live)).toBe(id)
    }
    expect(resolveSettingsSectionId('not-a-section', live)).toBe(
      FALLBACK_SETTINGS_SECTION
    )
  })
})

describe('settingsIndex — searchSettings matcher', () => {
  beforeEach(() => {
    mocks.loadedPlugins.plugins.clear()
    mocks.settings.config = { ui: {} }
    mocks.surfaces = []
  })

  it('returns [] for an empty query', () => {
    expect(searchSettings('')).toEqual([])
    expect(searchSettings('   ')).toEqual([])
  })

  it('matches a label substring', () => {
    const results = searchSettings('font')
    expect(results.length).toBeGreaterThan(0)
    expect(results.some((e) => e.label === 'Font family')).toBe(true)
  })

  it('matches a keyword (synonym)', () => {
    const results = searchSettings('spell')
    expect(results.length).toBeGreaterThan(0)
    // "Spellcheck" should be in the results (matched on label or keyword).
    expect(results.some((e) => e.label === 'Spellcheck')).toBe(true)
  })

  it('returns results tagged with the right sectionId', () => {
    const results = searchSettings('theme')
    expect(results.length).toBeGreaterThan(0)
    for (const r of results) {
      expect(r.sectionId).toBe('appearance')
    }
  })

  it('respects the limit parameter', () => {
    // A broad query that matches many entries.
    const all = searchSettings('a', 100)
    const limited = searchSettings('a', 3)
    expect(limited.length).toBeLessThanOrEqual(3)
    expect(limited.length).toBeLessThanOrEqual(all.length)
  })

  it('is case-insensitive', () => {
    const lower = searchSettings('api key')
    const upper = searchSettings('API KEY')
    expect(lower).toEqual(upper)
    expect(lower.length).toBeGreaterThan(0)
  })

  it('returns [] when no entry matches', () => {
    expect(searchSettings('zzz-no-such-setting')).toEqual([])
  })

  it('includes anchorId when the entry has one', () => {
    const results = searchSettings('line height')
    const entry = results.find((e) => e.label === 'Line height')
    expect(entry).toBeDefined()
    expect(entry?.anchorId).toBe('editor-typography')
  })
})
