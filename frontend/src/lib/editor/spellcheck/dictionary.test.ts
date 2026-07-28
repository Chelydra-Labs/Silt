import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
// Bundler-resolved fixtures — no fs/__dirname layout coupling.
import enUsAff from '../../../../public/dictionaries/en-US/index.aff?raw'
import enUsDic from '../../../../public/dictionaries/en-US/index.dic?raw'

const mocks = vi.hoisted(() =>
  createAppIpcMocks({
    EnsureLanguagePack: vi.fn(),
    GetLanguagePackContent: vi.fn(),
    EnsureDomainPack: vi.fn(),
    GetDomainPackWords: vi.fn()
  })
)

vi.mock('$silt-app', () => mocks)

import {
  parseWordListText,
  setCustomWords,
  setDomainWords,
  checkWord,
  suggest,
  ignoreWordSession,
  hasDomainWord,
  resetDictionary,
  loadDomainPacks,
  loadDictionary,
  isDictionaryLoaded,
  getActiveLanguage
} from './dictionary'
import { dictionaryStatus } from './dictionaryStatus.svelte'

describe('parseWordListText', () => {
  it('parses comments, blanks, and lowercases', () => {
    const words = parseWordListText(`# header
TypeScript
oauth

# skip
word # trailing
`)
    expect(words).toEqual(['typescript', 'oauth', 'word'])
  })
})

describe('checkWord layers', () => {
  beforeEach(() => {
    resetDictionary()
    setCustomWords([])
    setDomainWords([])
  })

  it('returns true when dictionary not loaded', () => {
    expect(checkWord('anything')).toBe(true)
  })

  it('tracks domain words via hasDomainWord', () => {
    setDomainWords(['TypeScript', 'OAuth'])
    expect(hasDomainWord('typescript')).toBe(true)
    expect(hasDomainWord('oauth')).toBe(true)
    expect(hasDomainWord('notindomain')).toBe(false)
  })
})

describe('proper-noun casing (bundled en-US)', () => {
  beforeEach(() => {
    resetDictionary()
    setCustomWords([])
    setDomainWords([])
  })

  afterEach(() => {
    // loadDictionary('en-US') stubs fetch; restore so later describes see the
    // real global (Vitest only auto-unstubs between files).
    vi.unstubAllGlobals()
  })

  it('accepts title-case place names and rejects wrong casing', async () => {
    // Load the real bundled pack the same way production does (fetch → public/).
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        const body = String(url).endsWith('.aff') ? enUsAff : enUsDic
        return {
          ok: true,
          status: 200,
          text: async () => body
        }
      })
    )
    await loadDictionary('en-US')

    // Dictionary stores proper nouns in title case (e.g. Rockford/M, Chicago).
    // Checking the token as written accepts them; lowercasing used to reject all.
    expect(checkWord('Rockford')).toBe(true)
    expect(checkWord('Chicago')).toBe(true)
    expect(checkWord('ROCKFORD')).toBe(true)
    // Lowercase is not the dictionary form for those entries.
    expect(checkWord('rockford')).toBe(false)
    expect(checkWord('chicago')).toBe(false)
    // Casing fix is offered among suggestions (order is Hunspell's).
    expect(suggest('rockford')).toContain('Rockford')
    // Never offer a no-op self-replace for the correct form.
    expect(suggest('Rockford')).not.toContain('Rockford')
  })
})

describe('loadDomainPacks', () => {
  beforeEach(() => {
    resetDictionary()
    mocks.EnsureDomainPack.mockReset()
    mocks.GetDomainPackWords.mockReset()
    mocks.EnsureDomainPack.mockResolvedValue(undefined)
  })

  it('loads software-terms via IPC (single source of truth)', async () => {
    mocks.GetDomainPackWords.mockResolvedValue([
      'docker',
      'oauth',
      'typescript'
    ])
    await loadDomainPacks(['software-terms'])
    expect(mocks.EnsureDomainPack).toHaveBeenCalledWith('software-terms')
    expect(mocks.GetDomainPackWords).toHaveBeenCalledWith('software-terms')
    expect(hasDomainWord('docker')).toBe(true)
    expect(hasDomainWord('typescript')).toBe(true)
    expect(hasDomainWord('notloaded')).toBe(false)
    expect(dictionaryStatus.domainError).toBeNull()
  })

  it('surfaces partial failures', async () => {
    mocks.EnsureDomainPack.mockRejectedValue(new Error('network timeout'))
    await expect(loadDomainPacks(['typescript'])).rejects.toThrow()
    expect(dictionaryStatus.domainError).toBeTruthy()
  })
})

describe('loadDictionary supersede race', () => {
  beforeEach(() => {
    resetDictionary()
    mocks.EnsureLanguagePack.mockReset()
    mocks.GetLanguagePackContent.mockReset()
  })

  function mockPacks(opts?: {
    gateLang?: string
    gate?: Promise<void>
    failLang?: string
  }) {
    mocks.EnsureLanguagePack.mockImplementation(async (lang: string) => {
      if (opts?.gateLang && lang === opts.gateLang && opts.gate) await opts.gate
      if (opts?.failLang && lang === opts.failLang) {
        throw new Error(`pack ${lang} failed`)
      }
    })
    mocks.GetLanguagePackContent.mockImplementation(async (lang: string) => {
      return {
        aff: 'SET UTF-8\n',
        dic: `2\n${lang === 'en-GB' ? 'colour' : 'farbe'}\nx\n`
      }
    })
  }

  it('does not let a slower load clobber a newer language', async () => {
    let releaseSlow: (() => void) | undefined
    const slowGate = new Promise<void>((resolve) => {
      releaseSlow = resolve
    })
    mockPacks({ gateLang: 'en-GB', gate: slowGate })

    const slow = loadDictionary('en-GB')
    const fast = loadDictionary('de')
    await fast
    expect(isDictionaryLoaded()).toBe(true)
    expect(getActiveLanguage()).toBe('de')

    releaseSlow!()
    await slow

    // Installed language must remain the later request (de), not slow en-GB.
    expect(getActiveLanguage()).toBe('de')
    expect(isDictionaryLoaded()).toBe(true)
  })

  it('keeps the last-good dictionary when a language switch fails', async () => {
    mockPacks()
    await loadDictionary('en-GB')
    expect(getActiveLanguage()).toBe('en-GB')

    mockPacks({ failLang: 'de' })
    await expect(loadDictionary('de')).rejects.toThrow(/de failed/)

    // Failed switch must not wipe the working pack.
    expect(isDictionaryLoaded()).toBe(true)
    expect(getActiveLanguage()).toBe('en-GB')
  })

  it('does not report an in-flight language as active before install', async () => {
    let releaseSlow: (() => void) | undefined
    const slowGate = new Promise<void>((resolve) => {
      releaseSlow = resolve
    })
    mockPacks({ gateLang: 'de', gate: slowGate })

    const pending = loadDictionary('de')
    // While loading, nothing is installed yet.
    expect(getActiveLanguage()).toBe('')
    expect(isDictionaryLoaded()).toBe(false)

    releaseSlow!()
    await pending
    expect(getActiveLanguage()).toBe('de')
    expect(isDictionaryLoaded()).toBe(true)
  })

  it('rejoins an in-flight load for the same language', async () => {
    let release: (() => void) | undefined
    const gate = new Promise<void>((resolve) => {
      release = resolve
    })
    mockPacks({ gateLang: 'de', gate })

    const a = loadDictionary('de')
    const b = loadDictionary('de')
    expect(a).toBe(b)
    release!()
    await Promise.all([a, b])
    expect(getActiveLanguage()).toBe('de')
  })

  it('resolves a superseded slow load to the installed dict', async () => {
    let releaseSlow: (() => void) | undefined
    const slowGate = new Promise<void>((resolve) => {
      releaseSlow = resolve
    })
    mockPacks({ gateLang: 'en-GB', gate: slowGate })

    const slow = loadDictionary('en-GB')
    const fast = await loadDictionary('de')
    expect(getActiveLanguage()).toBe('de')

    releaseSlow!()
    const slowResult = await slow
    // Superseded caller must not get a detached en-GB Typo.
    expect(slowResult).toBe(fast)
    expect(getActiveLanguage()).toBe('de')
  })

  it('returns an orphan Typo when a superseded load finishes before the winner installs', async () => {
    // Empty state: A (en-GB) starts, B (de) supersedes. A finishes while B is
    // still gated → dict is still null → A gets an orphan Typo for en-GB.
    // B then installs; getActiveLanguage reports de only.
    let releaseA: (() => void) | undefined
    let releaseB: (() => void) | undefined
    const gateA = new Promise<void>((resolve) => {
      releaseA = resolve
    })
    const gateB = new Promise<void>((resolve) => {
      releaseB = resolve
    })
    mocks.EnsureLanguagePack.mockImplementation(async (lang: string) => {
      if (lang === 'en-GB') await gateA
      if (lang === 'de') await gateB
    })
    mocks.GetLanguagePackContent.mockImplementation(async (lang: string) => ({
      aff: 'SET UTF-8\n',
      dic: `2\n${lang === 'en-GB' ? 'colour' : 'farbe'}\nx\n`
    }))

    const a = loadDictionary('en-GB')
    const b = loadDictionary('de')
    expect(getActiveLanguage()).toBe('')
    expect(isDictionaryLoaded()).toBe(false)

    releaseA!()
    const orphan = await a
    expect(isDictionaryLoaded()).toBe(false)
    expect(getActiveLanguage()).toBe('')
    expect(orphan.loaded).toBe(true)

    releaseB!()
    const winner = await b
    expect(winner).not.toBe(orphan)
    expect(getActiveLanguage()).toBe('de')
    expect(isDictionaryLoaded()).toBe(true)
  })

  it('does not reject a superseded failed load when a dict is installed', async () => {
    mockPacks()
    await loadDictionary('en-GB')

    let releaseFail: (() => void) | undefined
    const failGate = new Promise<void>((resolve) => {
      releaseFail = resolve
    })
    mocks.EnsureLanguagePack.mockImplementation(async (lang: string) => {
      if (lang === 'de') {
        await failGate
        throw new Error('pack de failed')
      }
    })
    mocks.GetLanguagePackContent.mockImplementation(async () => ({
      aff: 'SET UTF-8\n',
      dic: '2\ncolour\nx\n'
    }))

    const canceled = loadDictionary('de')
    // Switch back to the installed language — cancels the de attempt.
    const kept = await loadDictionary('en-GB')
    expect(getActiveLanguage()).toBe('en-GB')

    releaseFail!()
    await expect(canceled).resolves.toBe(kept)
    expect(getActiveLanguage()).toBe('en-GB')
  })
})

describe('ignoreWordSession cache', () => {
  beforeEach(() => {
    resetDictionary()
    setCustomWords([])
    setDomainWords([])
  })

  it('clears all case variants of the ignored token from the check cache', async () => {
    mocks.EnsureLanguagePack.mockResolvedValue(undefined)
    mocks.GetLanguagePackContent.mockResolvedValue({
      aff: 'SET UTF-8\n',
      dic: '1\nhello\n'
    })
    await loadDictionary('en-TEST')

    // Seed cache with mixed-case misses for the same token.
    expect(checkWord('FooBar')).toBe(false)
    expect(checkWord('foobar')).toBe(false)
    expect(checkWord('FOOBAR')).toBe(false)

    ignoreWordSession('FooBar')

    // Session ignore is case-insensitive; all variants must pass after clear.
    expect(checkWord('FooBar')).toBe(true)
    expect(checkWord('foobar')).toBe(true)
    expect(checkWord('FOOBAR')).toBe(true)
  })
})
