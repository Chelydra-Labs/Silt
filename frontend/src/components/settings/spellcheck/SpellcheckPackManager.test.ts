/**
 * Isolated unit tests for SpellcheckPackManager (#764). The component owns the
 * entire pack lifecycle; here we render it directly (no EditorTab wrapper) and
 * exercise the download / cancel / retry / progress / enable paths. The
 * integration guard (rendering through EditorTab) lives in
 * EditorTab.spellcheck.test.ts. Mocks IPC at the binding boundary (AGENTS.md).
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
import type { ComponentProps } from 'svelte'
import { Events } from '@wailsio/runtime'

const appMocks = vi.hoisted(() =>
  createAppIpcMocks({
    ListLanguagePacks: vi.fn(),
    ListDomainPacks: vi.fn(),
    EnsureLanguagePack: vi.fn(),
    EnsureDomainPack: vi.fn(),
    CancelSpellcheckDownload: vi.fn()
  })
)

vi.mock('$silt-app', () => appMocks)

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn(() => () => {})
  }
}))

vi.mock('../../../lib/editor/spellcheck/dictionaryStatus.svelte', () => ({
  dictionaryStatus: { loadError: null, domainError: null },
  friendlyPackError: (e: unknown) =>
    e instanceof Error ? e.message : String(e)
}))

import SpellcheckPackManager from './SpellcheckPackManager.svelte'

type PackProps = ComponentProps<typeof SpellcheckPackManager>

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

function languageSelect(): HTMLSelectElement {
  const card = document.getElementById('editor-spellcheck-packs')
  const sel = card?.querySelector('select') as HTMLSelectElement | null
  if (!sel) throw new Error('language select not found')
  return sel
}

describe('SpellcheckPackManager', () => {
  let touch = vi.fn()

  beforeEach(() => {
    touch = vi.fn()
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

  function renderIt(overrides: Partial<PackProps> = {}) {
    return render(SpellcheckPackManager, {
      spellcheckEnabled: true,
      spellcheckLanguage: 'en-US',
      spellcheckDomains: ['software-terms'],
      touch,
      ...overrides
    })
  }

  it('renders the dictionaries card and loads the language list', async () => {
    renderIt()
    await waitFor(() => {
      expect(languageSelect().options.length).toBeGreaterThan(1)
    })
    expect(screen.getByText(/Spellcheck dictionaries/i)).toBeTruthy()
    expect(languageSelect().value).toBe('en-US')
  })

  it('downloads a non-installed language on select and keeps it on success', async () => {
    appMocks.EnsureLanguagePack.mockResolvedValue(undefined)
    appMocks.ListLanguagePacks.mockResolvedValueOnce([
      enUS,
      enGB
    ]).mockResolvedValueOnce([enUS, { ...enGB, installed: true }])
    renderIt()
    await waitFor(() => {
      expect(languageSelect().options.length).toBeGreaterThan(1)
    })

    await fireEvent.change(languageSelect(), { target: { value: 'en-GB' } })
    await waitFor(() => {
      expect(appMocks.EnsureLanguagePack).toHaveBeenCalledWith('en-GB')
    })
    await waitFor(() => {
      // Optimistic select survives a successful download.
      expect(languageSelect().value).toBe('en-GB')
    })
    expect(screen.queryByRole('alert')).toBeNull()
    // Dirty flag propagates to the parent via touch().
    expect(touch).toHaveBeenCalled()
  })

  it('reverts the select and shows a Retry button on download failure', async () => {
    appMocks.EnsureLanguagePack.mockRejectedValue(new Error('network timeout'))
    renderIt()
    await waitFor(() => {
      expect(languageSelect().options.length).toBeGreaterThan(1)
    })

    await fireEvent.change(languageSelect(), { target: { value: 'en-GB' } })
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
    renderIt()
    await waitFor(() => {
      expect(languageSelect().options.length).toBeGreaterThan(1)
    })

    await fireEvent.change(languageSelect(), { target: { value: 'en-GB' } })
    await waitFor(() => {
      expect(screen.getByText(/Download cancelled/i)).toBeTruthy()
    })
    expect(screen.queryByRole('button', { name: /Retry download/i })).toBeNull()
    expect(languageSelect().value).toBe('en-US')
  })

  it('renders the percent + stage label from a progress event', async () => {
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

    renderIt()
    await waitFor(() => {
      expect(languageSelect().options.length).toBeGreaterThan(1)
    })
    await fireEvent.change(languageSelect(), { target: { value: 'en-GB' } })
    await waitFor(() => {
      expect(appMocks.EnsureLanguagePack).toHaveBeenCalledWith('en-GB')
    })
    await tick()

    expect(progressHandler).toBeTruthy()
    progressHandler!({ data: { received: 25, total: 100, file: 'index.dic' } })
    await tick()

    const card = document.getElementById('editor-spellcheck-packs')!
    expect(card.textContent).toMatch(/25%/)
    // stageLabel('index.dic') === 'word list'
    expect(card.textContent).toMatch(/word list/)

    releaseEnsure()
    await waitFor(() => {
      expect(screen.queryByText(/25%/)).toBeNull()
    })
  })

  it('toggling the enable checkbox writes back through the bindable prop', async () => {
    renderIt({ spellcheckEnabled: true })
    await waitFor(() => {
      expect(languageSelect().options.length).toBeGreaterThan(1)
    })

    const enableCheckbox = screen.getByRole('checkbox', {
      name: /Enable spellcheck/i
    }) as HTMLInputElement
    expect(enableCheckbox.checked).toBe(true)

    await fireEvent.click(enableCheckbox)
    await tick()
    // touch() fires on the dirty-path write.
    expect(touch).toHaveBeenCalled()
  })

  it('downloads a non-installed domain pack when its checkbox is enabled', async () => {
    appMocks.EnsureDomainPack.mockResolvedValue(undefined)
    appMocks.ListDomainPacks.mockResolvedValue([
      {
        id: 'software-terms',
        label: 'Software terms',
        license: 'MIT',
        approx_bytes: 8000,
        bundled: true,
        installed: true,
        default_on: true,
        version: ''
      },
      {
        id: 'medical',
        label: 'Medical terms',
        license: 'MIT',
        approx_bytes: 12000,
        bundled: false,
        downloadable: true,
        installed: false,
        default_on: false,
        version: ''
      }
    ])
    renderIt()
    await waitFor(() => {
      expect(screen.getByLabelText(/Medical terms/i)).toBeTruthy()
    })

    const medical = screen.getByRole('checkbox', {
      name: /Medical terms/i
    }) as HTMLInputElement
    await fireEvent.click(medical)

    await waitFor(() => {
      expect(appMocks.EnsureDomainPack).toHaveBeenCalledWith('medical')
    })
  })

  it('retries the download when Retry is clicked after a failure', async () => {
    appMocks.EnsureLanguagePack.mockRejectedValueOnce(
      new Error('network timeout')
    ).mockResolvedValueOnce(undefined)
    appMocks.ListLanguagePacks.mockResolvedValue([enUS, enGB])

    renderIt()
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
      // Retry succeeds → the Retry affordance disappears.
      expect(
        screen.queryByRole('button', { name: /Retry download/i })
      ).toBeNull()
    })
  })

  it('cancel invokes CancelSpellcheckDownload, shows cancelled status, and clears busy', async () => {
    // Hold the install in flight so packBusy is set and the Cancel button renders.
    let releaseEnsure: () => void = () => {}
    appMocks.EnsureLanguagePack.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          releaseEnsure = resolve
        })
    )
    appMocks.CancelSpellcheckDownload.mockResolvedValue(undefined)

    renderIt()
    await waitFor(() => {
      expect(languageSelect().options.length).toBeGreaterThan(1)
    })
    await fireEvent.change(languageSelect(), { target: { value: 'en-GB' } })
    await waitFor(() => {
      expect(appMocks.EnsureLanguagePack).toHaveBeenCalledWith('en-GB')
    })

    const cancelBtn = await waitFor(() =>
      screen.getByRole('button', { name: 'Cancel' })
    )
    await fireEvent.click(cancelBtn)
    await tick()

    expect(appMocks.CancelSpellcheckDownload).toHaveBeenCalled()
    expect(screen.getByText(/Download cancelled/i)).toBeTruthy()
    // packBusy cleared → the Cancel affordance disappears.
    expect(screen.queryByRole('button', { name: 'Cancel' })).toBeNull()

    // Let the held install settle so no promise dangles past unmount.
    releaseEnsure()
    await tick()
  })

  it('swallows a rejecting CancelSpellcheckDownload without surfacing an error', async () => {
    // Guards the .catch(() => {}) fix: a backend cancel that rejects must not
    // produce an error banner (the UI still resets to "Download cancelled").
    let releaseEnsure: () => void = () => {}
    appMocks.EnsureLanguagePack.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          releaseEnsure = resolve
        })
    )
    appMocks.CancelSpellcheckDownload.mockRejectedValue(
      new Error('no active download')
    )

    renderIt()
    await waitFor(() => {
      expect(languageSelect().options.length).toBeGreaterThan(1)
    })
    await fireEvent.change(languageSelect(), { target: { value: 'en-GB' } })
    await waitFor(() => {
      expect(appMocks.EnsureLanguagePack).toHaveBeenCalledWith('en-GB')
    })

    await fireEvent.click(
      await waitFor(() => screen.getByRole('button', { name: 'Cancel' }))
    )
    await tick()

    expect(appMocks.CancelSpellcheckDownload).toHaveBeenCalled()
    expect(screen.getByText(/Download cancelled/i)).toBeTruthy()
    // The rejection was swallowed by .catch — no error alert appears.
    expect(screen.queryByRole('alert')).toBeNull()

    releaseEnsure()
    await tick()
  })
})
