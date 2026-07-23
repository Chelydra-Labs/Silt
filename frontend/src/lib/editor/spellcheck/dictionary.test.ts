import { describe, it, expect, beforeEach, vi } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'

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
  suggest,
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

  it('accepts title-case place names and rejects wrong casing', async () => {
    // Load the real bundled pack the same way production does (fetch → public/).
    // spellcheck/ → editor/ → lib/ → src/ → frontend/public/
    const dictDir = path.resolve(
      __dirname,
      '../../../../public/dictionaries/en-US'
    )
    const aff = fs.readFileSync(path.join(dictDir, 'index.aff'), 'utf8')
    const dic = fs.readFileSync(path.join(dictDir, 'index.dic'), 'utf8')
    vi.stubGlobal(
      'fetch',
      vi.fn(async (url: string) => {
        const body = String(url).endsWith('.aff') ? aff : dic
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
