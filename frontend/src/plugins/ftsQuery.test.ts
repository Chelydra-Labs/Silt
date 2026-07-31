import { describe, expect, it } from 'vitest'
import { buildFTSQuery, PLUGIN_FULL_TEXT_SEARCH_SQL } from './ftsQuery'

describe('buildFTSQuery', () => {
  it('strips hyphens so FTS5 does not treat them as NOT', () => {
    expect(buildFTSQuery('long-term')).toBe('longterm*')
  })

  it('tokenizes multi-word queries with prefix wildcards', () => {
    expect(buildFTSQuery('Atomic Blocks')).toBe('Atomic* Blocks*')
  })

  it('drops tokens shorter than 2 characters after cleaning', () => {
    expect(buildFTSQuery('a to be')).toBe('to* be*')
  })

  it('returns empty string when nothing survives cleaning', () => {
    expect(buildFTSQuery('---')).toBe('')
    expect(buildFTSQuery('')).toBe('')
    expect(buildFTSQuery('   ')).toBe('')
  })

  it('keeps unicode letters', () => {
    expect(buildFTSQuery('Café notes')).toBe('Café* notes*')
  })
})

describe('PLUGIN_FULL_TEXT_SEARCH_SQL', () => {
  it('joins blocks on rowid (not id UUID) matching Go SearchBlocks', () => {
    expect(PLUGIN_FULL_TEXT_SEARCH_SQL).toMatch(/b\.rowid\s*=\s*f\.rowid/)
    expect(PLUGIN_FULL_TEXT_SEARCH_SQL).not.toMatch(/b\.id\s*=\s*f\.rowid/)
  })
})
