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
