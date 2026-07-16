// Coverage for the settings panel (#511 rework): the `role="tabpanel"` half
// of the WAI-ARIA tabs pattern rendered in the content area. The matching
// tablist lives in SettingsNav (sidebar); this pins the panel's half of the
// #356 tablist/tabpanel contract plus a render smoke test.
import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, cleanup, waitFor } from '@testing-library/svelte'

const mocks = vi.hoisted(() => ({
  loadedPlugins: {
    plugins: new Map<string, any>(),
    errors: [] as { id: string; message: string }[]
  },
  settings: {
    loading: false,
    error: '',
    config: {
      ui: {},
      editor: {},
      parsing: {},
      hotkeys: {},
      plugins: { disabled: [], active: [], plugin_settings: {} },
      notebooks: { path: '/test' },
      linked_notebooks: []
    }
  }
}))

vi.mock('../../../bindings/silt/app.js', () => ({
  GetCloseToTray: vi.fn().mockResolvedValue(false),
  SetCloseToTray: vi.fn().mockResolvedValue(undefined),
  ListPlugins: vi.fn().mockResolvedValue([]),
  GetGrantedCapabilities: vi.fn().mockResolvedValue({}),
  PluginRawQuery: vi.fn(),
  GetPluginSettingsForNotebook: vi.fn().mockResolvedValue({}),
  UpdatePluginSetting: vi.fn(),
  // AppearanceTab loads the theme list in a $effect on mount.
  ListThemes: vi.fn().mockResolvedValue([]),
  // GeneralTab custom dictionary + EditorTab pack catalogs.
  GetCustomDictionary: vi.fn().mockResolvedValue([]),
  AddCustomDictionaryWord: vi.fn().mockResolvedValue([]),
  RemoveCustomDictionaryWord: vi.fn().mockResolvedValue([]),
  PickCustomDictionaryExportPath: vi.fn().mockResolvedValue(''),
  PickCustomDictionaryImportFile: vi.fn().mockResolvedValue(''),
  ExportCustomDictionary: vi.fn().mockResolvedValue(undefined),
  ImportCustomDictionary: vi
    .fn()
    .mockResolvedValue({ added: 0, skipped: 0, total_read: 0 }),
  ListLanguagePacks: vi.fn().mockResolvedValue([
    {
      id: 'en-US',
      label: 'English (US)',
      license: 'MIT',
      approx_bytes: 600000,
      bundled: true,
      downloadable: false,
      installed: true,
      version: '3.0.0'
    }
  ]),
  ListDomainPacks: vi.fn().mockResolvedValue([
    {
      id: 'software-terms',
      label: 'Software terms',
      license: 'MIT',
      approx_bytes: 8000,
      bundled: true,
      downloadable: false,
      installed: true,
      default_on: true,
      version: ''
    }
  ]),
  EnsureLanguagePack: vi.fn().mockResolvedValue(undefined),
  EnsureDomainPack: vi.fn().mockResolvedValue(undefined),
  CancelSpellcheckDownload: vi.fn(),
  GetLanguagePackContent: vi.fn(),
  GetDomainPackWords: vi.fn().mockResolvedValue([]),
  UpdateAIFeatures: vi.fn()
}))
vi.mock('../../plugins/store.svelte', () => ({
  loadedPlugins: mocks.loadedPlugins
}))
vi.mock('../../settings/store.svelte', () => ({
  settings: mocks.settings,
  loadConfig: vi.fn().mockResolvedValue(true),
  saveConfig: vi.fn().mockResolvedValue(undefined),
  reloadFromBackend: vi.fn().mockResolvedValue(undefined)
}))
vi.mock('../../plugins/surfaces', () => ({
  getSurfaces: vi.fn(() => []),
  onSurfacesChanged: vi.fn(() => () => {})
}))
vi.mock('../../plugins/loader', () => ({
  loadPlugins: vi.fn().mockResolvedValue(undefined)
}))

import SettingsPanel from './SettingsPanel.svelte'

describe('SettingsPanel — tabpanel half of the tabs contract (#356)', () => {
  beforeEach(() => {
    mocks.loadedPlugins.plugins.clear()
    mocks.loadedPlugins.errors = []
    mocks.settings.loading = false
    mocks.settings.error = ''
  })
  afterEach(() => cleanup())

  it('renders a tabpanel labelled by the active section tab', () => {
    render(SettingsPanel, {
      props: {
        section: 'general',
        activeNotebook: 'Test',
        activeSection: '',
        activePage: ''
      }
    })
    const panel = screen.getByRole('tabpanel')
    expect(panel.id).toBe('silt-settings-panel')
    // aria-labelledby points back at the active tab in SettingsNav.
    expect(panel.getAttribute('aria-labelledby')).toBe(
      'silt-settings-tab-general'
    )
  })

  it('aria-labelledby tracks the active section across re-renders', async () => {
    const { rerender } = render(SettingsPanel, {
      props: {
        section: 'general',
        activeNotebook: 'Test',
        activeSection: '',
        activePage: ''
      }
    })
    expect(screen.getByRole('tabpanel').getAttribute('aria-labelledby')).toBe(
      'silt-settings-tab-general'
    )
    await rerender({
      section: 'about',
      activeNotebook: 'Test',
      activeSection: '',
      activePage: ''
    })
    expect(screen.getByRole('tabpanel').getAttribute('aria-labelledby')).toBe(
      'silt-settings-tab-about'
    )
  })

  it('shows the active section name as the panel heading', () => {
    render(SettingsPanel, {
      props: {
        section: 'appearance',
        activeNotebook: 'Test',
        activeSection: '',
        activePage: ''
      }
    })
    expect(
      screen.getByRole('heading', { name: /Appearance/i })
    ).toBeInTheDocument()
  })

  it('renders the General section content (smoke)', () => {
    render(SettingsPanel, {
      props: {
        section: 'general',
        activeNotebook: 'Test',
        activeSection: '',
        activePage: ''
      }
    })
    // The tabpanel mounts with the GeneralTab inside it.
    expect(screen.getByRole('tabpanel')).toBeInTheDocument()
  })

  it('falls back to Plugins when an orphaned plugin:* section disappears', async () => {
    render(SettingsPanel, {
      props: {
        section: 'plugin:ghost',
        activeNotebook: 'Test',
        activeSection: '',
        activePage: ''
      }
    })
    // No plugin is registered, so the orphaned section resets to 'plugins'.
    // The panel header (h2) re-renders to "Plugins"; PluginsTab also has an
    // h3 labelled "Plugins", so target the h2 explicitly.
    await waitFor(() => {
      expect(
        screen.getByRole('heading', { name: /Plugins/i, level: 2 })
      ).toBeInTheDocument()
    })
    expect(screen.getByRole('tabpanel').getAttribute('aria-labelledby')).toBe(
      'silt-settings-tab-plugins'
    )
  })
})
