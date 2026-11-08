// Parallel to backend/db/db_test.go TestBuildFTSQuery_PreservesUnicode and to
// lib/ftsQuery.test.ts. The plugin builder feeds ctx.fullTextSearch and AI
// retrieval, so it must produce byte-identical MATCH expressions to the Go
// search builder — a bare uppercase OR/AND/NOT would abort FTS5 with a
// syntax error.
import { describe, expect, it } from 'vitest'
import {
  buildFTSOrQuery,
  buildFTSQuery,
  PLUGIN_FULL_TEXT_SEARCH_SQL,
  sanitizeFTSToken
} from './ftsQuery'

describe('buildFTSQuery', () => {
  it.each([
    ['café', '"café"*'],
    ['über road', '"über"* "road"*'],
    ['中文 笔记', '"中文"* "笔记"*'],
    ['кирриллица', '"кирриллица"*'],
    // A lone CJK character is 3 UTF-8 bytes → survives Go's len() cutoff.
    ['中', '"中"*'],
    // FTS5 query-syntax chars are stripped; the surviving uppercase OR is
    // quoted so FTS5 reads it as a term, not a boolean operator.
    ['"inject" OR (*)', '"inject"* "OR"*'],
    // Bare boolean keywords are quoted terms, never operators.
    ['postgres OR sqlite', '"postgres"* "OR"* "sqlite"*'],
    ['a AND b NOT c', '"AND"* "NOT"*'],
    // <2-byte noise dropped: "a"/"b" are too short, "*" is all punctuation.
    ['a * b', '']
  ])('%j → %j', (input, expected) => {
    expect(buildFTSQuery(input)).toBe(expected)
  })

  it('strips hyphens so FTS5 does not treat them as NOT', () => {
    expect(buildFTSQuery('long-term')).toBe('"longterm"*')
  })

  it('joins multiple terms with implicit AND in input order', () => {
    expect(buildFTSQuery('ship postgres now')).toBe(
      '"ship"* "postgres"* "now"*'
    )
  })

  it('returns empty string when nothing survives cleaning', () => {
    expect(buildFTSQuery('---')).toBe('')
    expect(buildFTSQuery('')).toBe('')
    expect(buildFTSQuery('   ')).toBe('')
  })

  it('would collapse intentional OR into AND if used on keyword unions', () => {
    // Documents the gatherCandidates contract: free-text sanitization must
    // not be applied to pre-built OR expressions.
    expect(buildFTSQuery('database OR durability')).toBe(
      '"database"* "OR"* "durability"*'
    )
  })
})

describe('buildFTSOrQuery', () => {
  it('joins quoted prefix terms with an explicit FTS5 OR', () => {
    expect(buildFTSOrQuery(['database', 'durability'])).toBe(
      '"database"* OR "durability"*'
    )
  })

  it('strips hyphens per term and drops unusable tokens', () => {
    expect(buildFTSOrQuery(['long-term', 'a', '---', 'notes'])).toBe(
      '"longterm"* OR "notes"*'
    )
  })

  it('quotes a term that is itself a boolean keyword', () => {
    expect(buildFTSOrQuery(['postgres', 'OR', 'sqlite'])).toBe(
      '"postgres"* OR "OR"* OR "sqlite"*'
    )
  })

  it('keeps a single CJK term', () => {
    expect(buildFTSOrQuery(['中'])).toBe('"中"*')
  })

  it('dedupes identical sanitized terms', () => {
    expect(buildFTSOrQuery(['Note', 'note', 'NOTE'])).toBe('"Note"*')
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

  it('quotes the surviving term so FTS5 cannot read it as syntax', () => {
    expect(sanitizeFTSToken('OR')).toBe('"OR"*')
    expect(sanitizeFTSToken('sprint_17')).toBe('"sprint17"*')
  })
})

describe('PLUGIN_FULL_TEXT_SEARCH_SQL', () => {
  it('joins blocks on rowid (not id UUID) matching Go SearchBlocks', () => {
    expect(PLUGIN_FULL_TEXT_SEARCH_SQL).toMatch(/b\.rowid\s*=\s*f\.rowid/)
    expect(PLUGIN_FULL_TEXT_SEARCH_SQL).not.toMatch(/b\.id\s*=\s*f\.rowid/)
  })
})
