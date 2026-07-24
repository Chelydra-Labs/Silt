// Component tests for the settings search box + results popover.
//
// Pins the combobox/listbox contract: typing opens the popover and announces
// the match count, Arrow keys move the highlight, Enter jumps to the section,
// and the scoped keyboard model doesn't leak into the nav's roving-tabindex
// handler.
import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

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

import SettingsSearch from './SettingsSearch.svelte'

describe('SettingsSearch — popover + jump', () => {
  let jumped: { section: string; anchor?: string } | null
  beforeEach(() => {
    mocks.loadedPlugins.plugins.clear()
    mocks.loadedPlugins.errors = []
    mocks.settings.config = { ui: {} }
    mocks.surfaces = []
    jumped = null
  })
  afterEach(() => cleanup())

  it('renders a combobox input with the right aria wiring', () => {
    render(SettingsSearch, {
      props: { onJump: () => {} }
    })
    const combobox = screen.getByRole('combobox')
    expect(combobox.getAttribute('aria-autocomplete')).toBe('list')
    expect(combobox.getAttribute('aria-controls')).toBe(
      'silt-settings-search-listbox'
    )
  })

  it('does not show the listbox before typing', () => {
    render(SettingsSearch, {
      props: { onJump: () => {} }
    })
    expect(screen.queryByRole('listbox')).toBeNull()
  })

  it('opens the listbox and announces a result count on type', async () => {
    render(SettingsSearch, {
      props: { onJump: () => {} }
    })
    const input = screen.getByRole('combobox')
    await fireEvent.input(input, { target: { value: 'font' } })
    const listbox = await screen.findByRole('listbox')
    expect(listbox).toBeInTheDocument()
    const options = screen.getAllByRole('option')
    expect(options.length).toBeGreaterThan(0)
    // SR-only live region announces the count.
    expect(screen.getByText(/setting/i)).toBeInTheDocument()
  })

  it('announces "No settings match." for a dead-end query', async () => {
    render(SettingsSearch, {
      props: { onJump: () => {} }
    })
    const input = screen.getByRole('combobox')
    await fireEvent.input(input, { target: { value: 'zzz-nope' } })
    expect(screen.getByText(/No settings match/i)).toBeInTheDocument()
  })

  it('Enter on the highlighted result jumps to that section', async () => {
    const onJump = vi.fn((section: string, anchor?: string) => {
      jumped = { section, anchor }
    })
    render(SettingsSearch, { props: { onJump } })
    const input = screen.getByRole('combobox')
    // "theme" → all results are in the 'appearance' section.
    await fireEvent.input(input, { target: { value: 'theme' } })
    await fireEvent.keyDown(input, { key: 'Enter' })
    expect(jumped).not.toBeNull()
    expect(jumped!.section).toBe('appearance')
  })

  it('ArrowDown moves the highlighted option (aria-activedescendant)', async () => {
    render(SettingsSearch, {
      props: { onJump: () => {} }
    })
    const input = screen.getByRole('combobox')
    await fireEvent.input(input, { target: { value: 'font' } })
    const before = input.getAttribute('aria-activedescendant')
    await fireEvent.keyDown(input, { key: 'ArrowDown' })
    const after = input.getAttribute('aria-activedescendant')
    expect(after).not.toBe(before)
    expect(after).toMatch(/^silt-settings-search-result-/)
  })

  it('clicking a result jumps to that section', async () => {
    const onJump = vi.fn()
    render(SettingsSearch, { props: { onJump } })
    const input = screen.getByRole('combobox')
    await fireEvent.input(input, { target: { value: 'api key' } })
    const options = screen.getAllByRole('option')
    expect(options.length).toBeGreaterThan(0)
    // Click the button inside the first option.
    const btn = options[0].querySelector('button')
    expect(btn).not.toBeNull()
    await fireEvent.click(btn!)
    expect(onJump).toHaveBeenCalled()
  })

  it('Escape clears the query and closes the popover', async () => {
    render(SettingsSearch, {
      props: { onJump: () => {} }
    })
    const input = screen.getByRole('combobox') as HTMLInputElement
    await fireEvent.input(input, { target: { value: 'font' } })
    expect(screen.getByRole('listbox')).toBeInTheDocument()
    await fireEvent.keyDown(input, { key: 'Escape' })
    expect(input.value).toBe('')
    expect(screen.queryByRole('listbox')).toBeNull()
  })

  it('option result rows show the section label as a suffix', async () => {
    render(SettingsSearch, {
      props: { onJump: () => {} }
    })
    const input = screen.getByRole('combobox')
    await fireEvent.input(input, { target: { value: 'theme' } })
    // Every option for "theme" should be tagged "Appearance".
    const options = screen.getAllByRole('option')
    for (const opt of options) {
      expect(opt.textContent).toContain('Appearance')
    }
  })
})
