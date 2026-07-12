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
  resetDictionary,
  loadDomainPacks
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

  it('accepts custom and domain words without Hunspell', () => {
    setCustomWords(['MyAcronym'])
    setDomainWords(['TypeScript', 'OAuth'])
    expect(checkWord('typescript')).toBe(true)
  })
})

describe('loadDomainPacks', () => {
  beforeEach(() => {
    resetDictionary()
    mocks.EnsureDomainPack.mockReset()
    mocks.GetDomainPackWords.mockReset()
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({
        ok: true,
        text: async () => 'docker\noauth\n'
      }))
    )
  })

  it('loads bundled software-terms via fetch', async () => {
    await loadDomainPacks(['software-terms'])
    expect(checkWord('docker')).toBe(true) // no dict → true always
    expect(dictionaryStatus.domainError).toBeNull()
  })

  it('surfaces partial failures', async () => {
    mocks.EnsureDomainPack.mockRejectedValue(new Error('network timeout'))
    await expect(loadDomainPacks(['typescript'])).rejects.toThrow()
    expect(dictionaryStatus.domainError).toBeTruthy()
  })
})
