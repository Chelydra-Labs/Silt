// Parallel test to backend/db/db_test.go TestBuildFTSQuery_PreservesUnicode.
// Pins the TS term normalization to the Go builder so the hub text filter and
// query_tasks agent tool can never drift from global search semantics.
import { describe, expect, it } from 'vitest'
import { buildFtsMatch, ftsSearchTerms } from './ftsQuery'

describe('buildFtsMatch — mirrors backend buildFTSQuery', () => {
  it.each([
    ['café', '"café"*'],
    ['über road', '"über"* "road"*'],
    ['中文 笔记', '"中文"* "笔记"*'],
    ['кирриллица', '"кирриллица"*'],
    // FTS5 query-syntax chars are stripped; the surviving uppercase OR is
    // quoted so FTS5 reads it as a term, not a boolean operator.
    ['"inject" OR (*)', '"inject"* "OR"*'],
    // <2-byte noise dropped: "a"/"b" are too short, "*" is all punctuation.
    ['a * b', '']
  ])('%j → %j', (input, expected) => {
    expect(buildFtsMatch(input)).toBe(expected)
  })

  it('returns "" for blank / whitespace-only input', () => {
    expect(buildFtsMatch('')).toBe('')
    expect(buildFtsMatch('   ')).toBe('')
  })

  it('joins multiple terms with implicit AND in input order', () => {
    expect(buildFtsMatch('ship postgres now')).toBe(
      '"ship"* "postgres"* "now"*'
    )
  })

  it('keeps punctuation-bearing words as their letter/digit core', () => {
    expect(ftsSearchTerms('sprint_17 (v2)')).toEqual(['sprint17', 'v2'])
  })

  it('measures the sub-2-byte cutoff in UTF-8 bytes, not code units', () => {
    // "é" is one code unit but two UTF-8 bytes → survives Go's len() cutoff.
    expect(ftsSearchTerms('é')).toEqual(['é'])
    // Two-letter ASCII survives; a single ASCII letter does not.
    expect(ftsSearchTerms('ab')).toEqual(['ab'])
    expect(ftsSearchTerms('a')).toEqual([])
  })
})
