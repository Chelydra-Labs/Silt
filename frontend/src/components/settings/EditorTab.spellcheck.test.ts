/**
 * Focused tests for spellcheck pack download / cancel / retry UI (#336).
 * Mocks IPC at the binding boundary (AGENTS.md).
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'
import { tick } from 'svelte'
import { Events } from '@wailsio/runtime'

const appMocks = vi.hoisted(() =>
  createAppIpcMocks({
    ListLanguagePacks: vi.fn(),
    ListDomainPacks: vi.fn(),
    EnsureLanguagePack: vi.fn(),
    EnsureDomainPack: vi.fn(),
    CancelSpellcheckDownload: vi.fn(),
    GetLanguagePackContent: vi.fn(),
    GetDomainPackWords: vi.fn()
  })
)

vi.mock('$silt-app', () => appMocks)

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn(() => () => {})
  }
}))

const settingsMocks = vi.hoisted(() => {
  const baseConfig = {
    notebooks: { path: '/vault', default_active: 'Work' },
    editor: {
      font_family: 'Plus Jakarta Sans',
      mono_font_family: 'JetBrains Mono',
      font_size_px: 14,
      line_height: 1.6,
      tab_indent_spaces: 4,
      auto_save_delay_ms: 500,
      focus_highlight_ancestors: true,
      spellcheck_enabled: true,
      spellcheck_language: 'en-US',
      spellcheck_domains: ['software-terms'],
      typewriter_mode: false,
      typewriter_mode_ratio: 0.5,
      custom_dictionary: []
    },
    ui: {
      show_format_toolbar: true,
      formatting: { typography_enabled: true, color_enabled: true }
    },
    parsing: { auto_inject_uuid: true, default_task_priority: 3 },
    hotkeys: {},
    plugins: { active: [], disabled: [], plugin_settings: {} }
  }
  return {
    baseConfig,
    settings: {
      config: baseConfig,
      loading: false,
      saving: false,
      error: '',
      dirty: false,
      pendingExternal: false
    },
    saveConfig: vi.fn(async () => true),
    reloadFromBackend: vi.fn(async () => {})
  }
})

vi.mock('../../settings/store.svelte', () => ({
  settings: settingsMocks.settings,
  saveConfig: settingsMocks.saveConfig,
  reloadFromBackend: settingsMocks.reloadFromBackend
}))

vi.mock('../../theme/store.svelte', () => ({
  themeState: {
    id: 'cyber_forest',
    name: 'Cyber Forest',
    mode: 'dark',
    darkTokens: {
      '--font-body': "'Plus Jakarta Sans', sans-serif",
      '--font-mono': "'JetBrains Mono', monospace",
      '--font-headline': "'Hanken Grotesk', sans-serif"
    },
    lightTokens: {},
    error: null
  }
}))

vi.mock('../../theme/fonts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../theme/fonts')>()
  return {
    ...actual
  }
})

vi.mock('../../lib/editor/spellcheck/dictionaryStatus.svelte', () => ({
  dictionaryStatus: {
    loadError: null,
    domainError: null
  },
  friendlyPackError: (e: unknown) =>
    e instanceof Error ? e.message : String(e)
}))

import EditorTab from './EditorTab.svelte'

const enUS = {
  id: 'en-US',
  label: 'English (US)',
  license: 'MIT AND BSD',
  approx_bytes: 600000,
  bundled: true,
  downloadable: false,
  installed: true,
  version: '3.0.0'
}
const enGB = {
  id: 'en-GB',
  label: 'English (UK)',
  license: 'MIT AND BSD',
  approx_bytes: 555000,
  bundled: false,
  downloadable: true,
  installed: false,
  version: '3.0.0'
}

describe('EditorTab spellcheck packs', () => {
  beforeEach(() => {
    settingsMocks.settings.config = structuredClone(settingsMocks.baseConfig)
    settingsMocks.settings.dirty = false
    settingsMocks.settings.error = ''
    appMocks.ListLanguagePacks.mockResolvedValue([enUS, enGB])
    appMocks.ListDomainPacks.mockResolvedValue([
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
    ])
    appMocks.EnsureLanguagePack.mockReset()
    appMocks.EnsureDomainPack.mockReset()
    appMocks.CancelSpellcheckDownload.mockReset()
  })

  afterEach(() => cleanup())

  function languageSelect(): HTMLSelectElement {
    const card = document.getElementById('editor-spellcheck-packs')
    const sel = card?.querySelector('select') as HTMLSelectElement | null
    if (!sel) throw new Error('language select not found')
    return sel
  }

  it('reverts language select and shows error on download failure', async () => {
    appMocks.EnsureLanguagePack.mockRejectedValue(new Error('network timeout'))
    render(EditorTab)

    await waitFor(() => {
      expect(screen.getByText(/Spellcheck dictionaries/i)).toBeTruthy()
    })
    await waitFor(() => {
      expect(languageSelect().options.length).toBeGreaterThan(1)
    })

    const select = languageSelect()
    expect(select.value).toBe('en-US')

    await fireEvent.change(select, { target: { value: 'en-GB' } })
    await waitFor(() => {
      expect(appMocks.EnsureLanguagePack).toHaveBeenCalledWith('en-GB')
    })
    await waitFor(() => {
      expect(languageSelect().value).toBe('en-US')
    })
    expect(screen.getByRole('alert').textContent).toMatch(/network/i)
    expect(screen.getByRole('button', { name: /Retry download/i })).toBeTruthy()
  })

  it('treats cancel-flavored errors without a Retry banner', async () => {
    appMocks.EnsureLanguagePack.mockRejectedValue(
      new Error('download cancelled: context canceled')
    )
    render(EditorTab)
    await waitFor(() => {
      expect(languageSelect().options.length).toBeGreaterThan(1)
    })

    await fireEvent.change(languageSelect(), { target: { value: 'en-GB' } })
    await waitFor(() => {
      expect(appMocks.EnsureLanguagePack).toHaveBeenCalled()
    })
    await waitFor(() => {
      expect(screen.getByText(/Download cancelled/i)).toBeTruthy()
    })
    expect(screen.queryByRole('button', { name: /Retry download/i })).toBeNull()
    expect(languageSelect().value).toBe('en-US')
  })

  it('Retry re-invokes EnsureLanguagePack after a network failure', async () => {
    appMocks.EnsureLanguagePack.mockRejectedValueOnce(
      new Error('network timeout')
    ).mockResolvedValueOnce(undefined)
    appMocks.ListLanguagePacks.mockResolvedValueOnce([
      enUS,
      enGB
    ]).mockResolvedValueOnce([enUS, { ...enGB, installed: true }])

    render(EditorTab)
    await waitFor(() => {
      expect(languageSelect().options.length).toBeGreaterThan(1)
    })

    await fireEvent.change(languageSelect(), { target: { value: 'en-GB' } })
    await waitFor(() => screen.getByRole('button', { name: /Retry download/i }))

    await fireEvent.click(
      screen.getByRole('button', { name: /Retry download/i })
    )
    await waitFor(() => {
      expect(appMocks.EnsureLanguagePack).toHaveBeenCalledTimes(2)
    })
    await waitFor(() => {
      expect(
        screen.queryByRole('button', { name: /Retry download/i })
      ).toBeNull()
    })
    await tick()
  })

  it('renders download progress (percent + stage) from the progress event', async () => {
    // Hold the install in flight so packBusy stays set while we synthesize a
    // progress event from the backend. The download only resolves once we
    // release it, letting the component tear down cleanly.
    let releaseEnsure: () => void = () => {}
    appMocks.EnsureLanguagePack.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          releaseEnsure = resolve
        })
    )
    let progressHandler: ((ev: unknown) => void) | undefined
    vi.mocked(Events.On).mockImplementation(((_name, cb) => {
      progressHandler = cb as (ev: unknown) => void
      return () => {}
    }) as typeof Events.On)

    render(EditorTab)
    await waitFor(() => {
      expect(languageSelect().options.length).toBeGreaterThan(1)
    })

    // Picking a non-installed language kicks off downloadLanguage, which
    // sets packBusy and registers the progress subscription.
    await fireEvent.change(languageSelect(), { target: { value: 'en-GB' } })
    await waitFor(() => {
      expect(appMocks.EnsureLanguagePack).toHaveBeenCalledWith('en-GB')
    })
    await tick()

    // The backend emits EventSpellcheckDownloadProgress with byte counters +
    // the current file; the UI turns those into a percent + a human stage.
    expect(progressHandler).toBeTruthy()
    progressHandler!({
      data: { received: 50, total: 100, file: 'index.aff' }
    })
    await tick()

    const card = document.getElementById('editor-spellcheck-packs')!
    expect(card.textContent).toMatch(/50%/)
    // stageLabel('index.aff') === 'rules'
    expect(card.textContent).toMatch(/rules/)

    releaseEnsure()
    await waitFor(() => {
      expect(screen.queryByText(/50%/)).toBeNull()
    })
  })
})
