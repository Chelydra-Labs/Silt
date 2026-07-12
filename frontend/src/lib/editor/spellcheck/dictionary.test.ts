import { describe, it, expect, beforeEach, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  EnsureLanguagePack: vi.fn(),
  GetLanguagePackContent: vi.fn(),
  EnsureDomainPack: vi.fn(),
  GetDomainPackWords: vi.fn()
}))

vi.mock('../../../../bindings/silt/app.js', () => mocks)

import {
  parseWordListText,
  setCustomWords,
  setDomainWords,
  checkWord,
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

  it('does not let a slower load clobber a newer language', async () => {
    let releaseSlow: (() => void) | undefined
    const slowGate = new Promise<void>((resolve) => {
      releaseSlow = resolve
    })

    mocks.EnsureLanguagePack.mockImplementation(async (lang: string) => {
      if (lang === 'en-GB') await slowGate
    })
    mocks.GetLanguagePackContent.mockImplementation(async (lang: string) => {
      // Minimal valid-looking aff/dic payloads for Typo constructor.
      return {
        aff: 'SET UTF-8\n',
        dic: `2\n${lang === 'en-GB' ? 'colour' : 'farbe'}\nx\n`
      }
    })

    const slow = loadDictionary('en-GB')
    // Start a second load before the first resolves.
    const fast = loadDictionary('de')
    // de is not en-US so it also goes through Ensure — resolve immediately.
    await fast
    expect(isDictionaryLoaded()).toBe(true)

    releaseSlow!()
    await slow

    // Active language must remain the later request (de), not the slow en-GB.
    expect(getActiveLanguage()).toBe('de')
    expect(isDictionaryLoaded()).toBe(true)
  })
})
