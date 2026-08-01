import { describe, expect, it } from 'vitest'
import {
  buildFTSOrQuery,
  buildFTSQuery,
  PLUGIN_FULL_TEXT_SEARCH_SQL,
  sanitizeFTSToken
} from './ftsQuery'

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

  it('would collapse intentional OR into AND if used on keyword unions', () => {
    // Documents the gatherCandidates regression: free-text sanitization must
    // not be applied to pre-built OR expressions.
    expect(buildFTSQuery('database OR durability')).toBe(
      'database* OR* durability*'
    )
  })
})

describe('buildFTSOrQuery', () => {
  it('joins sanitized prefixes with FTS5 OR', () => {
    expect(buildFTSOrQuery(['database', 'durability'])).toBe(
      'database* OR durability*'
    )
  })

  it('strips hyphens per term and drops unusable tokens', () => {
    expect(buildFTSOrQuery(['long-term', 'a', '---', 'notes'])).toBe(
      'longterm* OR notes*'
    )
  })

  it('dedupes identical sanitized terms', () => {
    expect(buildFTSOrQuery(['Note', 'note', 'NOTE'])).toBe('Note*')
  })

  it('returns empty when no terms survive', () => {
    expect(buildFTSOrQuery([])).toBe('')
    expect(buildFTSOrQuery(['a', '-'])).toBe('')
  })
})

describe('sanitizeFTSToken', () => {
  it('returns empty for short or punctuation-only input', () => {
    expect(sanitizeFTSToken('a')).toBe('')
    expect(sanitizeFTSToken('--')).toBe('')
  })
})

describe('PLUGIN_FULL_TEXT_SEARCH_SQL', () => {
  it('joins blocks on rowid (not id UUID) matching Go SearchBlocks', () => {
    expect(PLUGIN_FULL_TEXT_SEARCH_SQL).toMatch(/b\.rowid\s*=\s*f\.rowid/)
    expect(PLUGIN_FULL_TEXT_SEARCH_SQL).not.toMatch(/b\.id\s*=\s*f\.rowid/)
  })
})
