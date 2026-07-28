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
    vi.mocked(Events.On).mockReset()
    vi.mocked(Events.On).mockImplementation(() => () => {})
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
      // Live region + visible status both carry the same string (#788).
      expect(screen.getAllByText(/Download cancelled/i).length).toBeGreaterThan(
        0
      )
    })
    expect(screen.queryByRole('button', { name: /Retry download/i })).toBeNull()
    expect(languageSelect().value).toBe('en-US')
  })

  it('updates a native progress element from progress events (#788)', async () => {
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

    const card = document.getElementById('editor-spellcheck-packs')!
    const progressEl = () =>
      card.querySelector('progress') as HTMLProgressElement

    expect(progressEl()).toBeTruthy()
    expect(progressEl().max).toBe(100)
    // Indeterminate until the first progress event (no value attribute).
    expect(progressEl().hasAttribute('value')).toBe(false)

    expect(progressHandler).toBeTruthy()
    progressHandler!({ data: { received: 25, total: 100, file: 'index.dic' } })
    await tick()

    expect(progressEl().hasAttribute('value')).toBe(true)
    expect(progressEl().value).toBe(25)
    // Visible percent/stage for sighted users (aria-hidden; not live).
    expect(card.textContent).toMatch(/25%/)
    expect(card.textContent).toMatch(/word list/)

    progressHandler!({ data: { received: 50, total: 100, file: 'index.dic' } })
    await tick()
    expect(progressEl().value).toBe(50)

    progressHandler!({ data: { received: 100, total: 100, file: 'index.dic' } })
    await tick()
    expect(progressEl().value).toBe(100)

    releaseEnsure()
    await waitFor(() => {
      expect(card.querySelector('progress')).toBeNull()
    })
  })

  it('live region announces state transitions only, not percent ticks (#788)', async () => {
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

    const live = document.querySelector(
      '#editor-spellcheck-packs [aria-live="polite"]'
    ) as HTMLElement
    expect(live).toBeTruthy()
    expect(live.classList.contains('sr-only')).toBe(true)
    // Start announcement (lang name), not percent.
    expect(live.textContent).toMatch(/Downloading English \(UK\)/i)
    expect(live.textContent).not.toMatch(/%/)

    const snapshots: string[] = [live.textContent?.trim() ?? '']
    for (const pct of [0, 10, 25, 50, 75, 100]) {
      progressHandler!({
        data: { received: pct, total: 100, file: 'index.dic' }
      })
      await tick()
      snapshots.push(live.textContent?.trim() ?? '')
    }

    // Percent ticks must not rewrite the polite live region.
    const unique = new Set(snapshots)
    expect(unique.size).toBeLessThanOrEqual(3)
    expect(unique.size).toBe(1)

    releaseEnsure()
    await waitFor(() => {
      expect(live.textContent).toMatch(/downloaded|Save settings/i)
    })
    // Success is a second distinct announcement after start.
    expect(live.textContent).not.toMatch(/%/)
  })

  it('Cancel is outside the polite live region while download is in flight (#788)', async () => {
    let releaseEnsure: () => void = () => {}
    appMocks.EnsureLanguagePack.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          releaseEnsure = resolve
        })
    )

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
    const live = document.querySelector(
      '#editor-spellcheck-packs [aria-live="polite"]'
    ) as HTMLElement
    expect(live).toBeTruthy()
    expect(live.contains(cancelBtn)).toBe(false)
    expect(cancelBtn.closest('[aria-live]')).toBeNull()

    releaseEnsure()
    await tick()
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
    expect(screen.getAllByText(/Download cancelled/i).length).toBeGreaterThan(0)
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
    expect(screen.getAllByText(/Download cancelled/i).length).toBeGreaterThan(0)
    // The rejection was swallowed by .catch — no error alert appears.
    expect(screen.queryByRole('alert')).toBeNull()

    releaseEnsure()
    await tick()
  })
})
