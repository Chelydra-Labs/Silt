import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

const mocks = vi.hoisted(() => {
  const baseConfig = {
    notebooks: { path: '/vault', default_active: 'Work' },
    editor: {
      font_family: 'Plus Jakarta Sans',
      mono_font_family: 'JetBrains Mono',
      font_size_px: 14,
      line_height: 1.6,
      tab_indent_spaces: 4,
      auto_save_delay_ms: 500,
      focus_highlight_ancestors: true
    },
    parsing: {
      auto_inject_uuid: true,
      default_task_priority: 3
    },
    hotkeys: { open_search: 'Ctrl+P' },
    plugins: { active: [], disabled: [], plugin_settings: {} }
  }
  return {
    settings: {
      config: baseConfig,
      loading: false,
      saving: false,
      error: '',
      dirty: false,
      pendingExternal: false
    },
    saveConfig: vi.fn(async () => true),
    reloadFromBackend: vi.fn(async () => {}),
    themeState: {
      id: 'cyber_forest',
      name: 'Cyber Forest',
      mode: 'dark' as 'dark' | 'light' | 'system',
      darkTokens: {
        '--color-void': '#0c0c0e',
        '--font-body': "'Plus Jakarta Sans', sans-serif",
        '--font-mono': "'JetBrains Mono', monospace",
        '--font-headline': "'Hanken Grotesk', sans-serif"
      } as Record<string, string>,
      lightTokens: {} as Record<string, string>,
      error: null as string | null
    }
  }
})

vi.mock('../../settings/store.svelte', () => ({
  settings: mocks.settings,
  saveConfig: mocks.saveConfig,
  reloadFromBackend: mocks.reloadFromBackend
}))
vi.mock('../../theme/store.svelte', () => ({ themeState: mocks.themeState }))

import EditorTab from './EditorTab.svelte'

function resetThemeState(withTypography: boolean) {
  mocks.themeState.darkTokens = withTypography
    ? {
        '--color-void': '#0c0c0e',
        '--font-body': "'Plus Jakarta Sans', sans-serif",
        '--font-mono': "'JetBrains Mono', monospace"
      }
    : { '--color-void': '#0c0c0e' }
}

describe('EditorTab font picker (#82)', () => {
  beforeEach(() => {
    mocks.settings.config.editor.font_family = 'Plus Jakarta Sans'
    mocks.settings.config.editor.mono_font_family = 'JetBrains Mono'
    mocks.settings.dirty = false
    mocks.saveConfig.mockClear()
  })
  afterEach(() => cleanup())

  it('renders a combobox for the body font reflecting the current config value', async () => {
    resetThemeState(true)
    render(EditorTab)
    await tick()
    const combo = screen.getByRole('combobox', { name: 'Font family' })
    expect(combo).toBeInTheDocument()
    expect(combo.textContent).toContain('Plus Jakarta Sans')
  })

  it('shows a Reset button for the body font when the theme overrides it', async () => {
    resetThemeState(true)
    render(EditorTab)
    await tick()
    expect(
      screen.getByLabelText('Reset body font to theme default')
    ).toBeInTheDocument()
  })

  it('hides the Reset button when the active theme has no typography override', async () => {
    resetThemeState(false)
    render(EditorTab)
    await tick()
    expect(
      screen.queryByLabelText('Reset body font to theme default')
    ).toBeNull()
    expect(
      screen.queryByLabelText('Reset monospace font to theme default')
    ).toBeNull()
  })

  it('clicking Reset clears the config field (so the CSS fallback resolves to the theme font)', async () => {
    resetThemeState(true)
    render(EditorTab)
    await tick()
    const reset = screen.getByLabelText('Reset body font to theme default')
    await fireEvent.click(reset)
    await tick()
    const combo = screen.getByRole('combobox', { name: 'Font family' })
    expect(combo.textContent).toContain('Theme default')
    expect(mocks.settings.dirty).toBe(true)
  })
})
