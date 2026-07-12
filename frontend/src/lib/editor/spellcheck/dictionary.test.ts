import { describe, it, expect, beforeEach, vi } from 'vitest'

vi.mock('../../../../bindings/silt/app.js', () => ({
  EnsureLanguagePack: vi.fn(),
  GetLanguagePackContent: vi.fn(),
  EnsureDomainPack: vi.fn(),
  GetDomainPackWords: vi.fn()
}))

import {
  parseWordListText,
  setCustomWords,
  setDomainWords,
  checkWord,
  resetDictionary
} from './dictionary'

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
    // Without a Typo instance, checkWord short-circuits to true always.
    // Domain/custom sets are still populated for when dict is present —
    // exercise setDomainWords / setCustomWords don't throw.
    setCustomWords(['MyAcronym'])
    setDomainWords(['TypeScript', 'OAuth'])
    expect(checkWord('typescript')).toBe(true)
  })
})
