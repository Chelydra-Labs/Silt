// Coverage for the settings section nav (#511 rework): the sidebar tablist
// half of the WAI-ARIA tabs pattern. The matching panel lives in
// SettingsPanel.test.ts. Together they pin the #214 (dynamic plugin tabs)
// and #356 (tablist/tab/ tabpanel ARIA contract) guarantees that previously
// lived in SettingsShell.test.ts.
import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

const mocks = vi.hoisted(() => ({
  loadedPlugins: {
    plugins: new Map<string, any>(),
    errors: [] as { id: string; message: string }[]
  },
  settings: {
    config: { ui: {} }
  },
  surfaces: [] as { pluginID: string }[]
}))

vi.mock('../../plugins/store.svelte', () => ({
  loadedPlugins: mocks.loadedPlugins
}))
vi.mock('../../settings/store.svelte', () => ({ settings: mocks.settings }))
vi.mock('../../plugins/surfaces', () => ({
  getSurfaces: vi.fn(() => mocks.surfaces as any),
  onSurfacesChanged: vi.fn(() => () => {})
}))

import SettingsNav from './SettingsNav.svelte'

describe('SettingsNav — section list (sidebar tablist)', () => {
  beforeEach(() => {
    mocks.loadedPlugins.plugins.clear()
    mocks.loadedPlugins.errors = []
    mocks.settings.config = { ui: {} }
    mocks.surfaces = []
  })
  afterEach(() => cleanup())

  it('renders a vertical tablist with the core sections', () => {
    render(SettingsNav, { props: { section: 'general' } })
    const tablist = screen.getByRole('tablist', {
      name: 'Settings sections'
    })
    expect(tablist.getAttribute('aria-orientation')).toBe('vertical')
    expect(screen.getByRole('tab', { name: /General/i })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /Appearance/i })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /^AI$/i })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /About/i })).toBeInTheDocument()
  })

  it('marks the active section with aria-selected and roving tabindex', () => {
    render(SettingsNav, { props: { section: 'appearance' } })
    const general = screen.getByRole('tab', { name: /General/i })
    const appearance = screen.getByRole('tab', { name: /Appearance/i })
    expect(appearance.getAttribute('aria-selected')).toBe('true')
    expect(appearance.getAttribute('tabindex')).toBe('0')
    expect(general.getAttribute('aria-selected')).toBe('false')
    expect(general.getAttribute('tabindex')).toBe('-1')
  })

  it('every tab aria-controls the shared settings panel id', () => {
    render(SettingsNav, { props: { section: 'general' } })
    const tabs = screen.getAllByRole('tab')
    expect(tabs.length).toBeGreaterThan(0)
    for (const tab of tabs) {
      expect(tab.getAttribute('aria-controls')).toBe('silt-settings-panel')
      expect(tab.id).toMatch(/^silt-settings-tab-/)
    }
  })

  it('ArrowDown moves the active section (roving tabindex)', async () => {
    render(SettingsNav, {
      props: { section: 'general' }
    })
    const tablist = screen.getByRole('tablist')
    const general = screen.getByRole('tab', { name: 'General' })
    general.focus()
    await fireEvent.keyDown(tablist, { key: 'ArrowDown' })
    // Active section moved to the next one (Editor); its tab is now selected
    // and tabbable.
    const editor = screen.getByRole('tab', { name: 'Editor' })
    expect(editor.getAttribute('aria-selected')).toBe('true')
    expect(editor.getAttribute('tabindex')).toBe('0')
    expect(general.getAttribute('tabindex')).toBe('-1')
  })

  it('Home/End jump to the first/last section', async () => {
    render(SettingsNav, {
      props: { section: 'editor' }
    })
    const tablist = screen.getByRole('tablist')
    await fireEvent.keyDown(tablist, { key: 'Home' })
    expect(
      screen.getByRole('tab', { name: 'General' }).getAttribute('aria-selected')
    ).toBe('true')
    await fireEvent.keyDown(tablist, { key: 'End' })
    expect(
      screen.getByRole('tab', { name: 'About' }).getAttribute('aria-selected')
    ).toBe('true')
  })

  it('hides the Dev section when dev mode is off', () => {
    mocks.settings.config = { ui: {} }
    render(SettingsNav, { props: { section: 'general' } })
    expect(screen.queryByRole('tab', { name: /^Dev$/i })).toBeNull()
  })

  it('shows the Dev section when open_devtools_on_startup is true', () => {
    mocks.settings.config = {
      ui: { open_devtools_on_startup: true }
    }
    render(SettingsNav, { props: { section: 'general' } })
    expect(screen.getByRole('tab', { name: /^Dev$/i })).toBeInTheDocument()
  })
})

describe('SettingsNav — group dividers (visual clustering)', () => {
  beforeEach(() => {
    mocks.loadedPlugins.plugins.clear()
    mocks.loadedPlugins.errors = []
    mocks.settings.config = { ui: {} }
    mocks.surfaces = []
  })
  afterEach(() => cleanup())

  it('renders group divider labels between clusters', () => {
    render(SettingsNav, { props: { section: 'general' } })
    // Each group label is a presentational divider element. Some labels
    // ("About") also appear as a section tab, so scope the assertion to the
    // role=presentation elements only.
    const presentational = screen
      .getAllByText(/^(Workspace|Look & feel|Intelligence|Customize|About)$/)
      .filter((el) => el.getAttribute('role') === 'presentation')
    expect(presentational.length).toBeGreaterThanOrEqual(5)
  })

  it('group dividers are role=presentation and excluded from the tab sequence', () => {
    render(SettingsNav, { props: { section: 'general' } })
    const divider = screen.getByText('Look & feel')
    expect(divider.getAttribute('role')).toBe('presentation')
    // The tablist contains exactly the section tabs — no divider leaks in as
    // a tab. Divider labels are not focusable elements.
    const tabs = screen.getAllByRole('tab')
    const tabTexts = tabs.map((t) => t.textContent?.trim())
    for (const label of [
      'Workspace',
      'Look & feel',
      'Intelligence',
      'Customize'
    ]) {
      expect(tabTexts).not.toContain(label)
    }
    // No divider is a button or link (none are interactive).
    expect(divider.tagName).not.toBe('BUTTON')
    expect(divider.tagName).not.toBe('A')
  })

  it('ArrowDown traverses across group boundaries (flat tablist)', async () => {
    // General (Workspace) → Editor (Look & feel): crosses a group divider.
    render(SettingsNav, { props: { section: 'general' } })
    const tablist = screen.getByRole('tablist')
    screen.getByRole('tab', { name: 'General' }).focus()
    await fireEvent.keyDown(tablist, { key: 'ArrowDown' })
    expect(
      screen.getByRole('tab', { name: 'Editor' }).getAttribute('aria-selected')
    ).toBe('true')
  })
})

describe('SettingsNav — dynamic plugin tabs (#214)', () => {
  beforeEach(() => {
    mocks.loadedPlugins.plugins.clear()
    mocks.settings.config = { ui: {} }
    mocks.surfaces = []
  })
  afterEach(() => cleanup())

  it('renders a section for a plugin with settingsPageComponent', () => {
    mocks.loadedPlugins.plugins.set('bespoke-plugin', {
      manifest: {
        id: 'bespoke-plugin',
        name: 'Bespoke Plugin',
        version: '1.0.0',
        icon: 'tune'
      },
      component: vi.fn(),
      settingsPageComponent: vi.fn(),
      source: 'first-party'
    })
    render(SettingsNav, { props: { section: 'general' } })
    expect(
      screen.getByRole('tab', { name: /Bespoke Plugin/i })
    ).toBeInTheDocument()
  })

  it('renders a section for a third-party settings-panel surface', () => {
    mocks.loadedPlugins.plugins.set('third-party', {
      manifest: {
        id: 'third-party',
        name: 'Third Party',
        version: '1.0.0'
      },
      component: vi.fn(),
      source: 'disk'
    })
    mocks.surfaces = [{ pluginID: 'third-party' }]
    render(SettingsNav, { props: { section: 'general' } })
    expect(
      screen.getByRole('tab', { name: /Third Party/i })
    ).toBeInTheDocument()
  })

  it('does not render a section for a plugin without settings', () => {
    mocks.loadedPlugins.plugins.set('plain-plugin', {
      manifest: {
        id: 'plain-plugin',
        name: 'Plain Plugin',
        version: '1.0.0'
      },
      component: vi.fn(),
      source: 'first-party'
    })
    render(SettingsNav, { props: { section: 'general' } })
    expect(screen.queryByText('Plain Plugin')).toBeNull()
  })
})
