import { describe, it, expect } from 'vitest'
import {
  findSourceMatches,
  expandReplace,
  replaceAllSource
} from './sourceSearch'

describe('findSourceMatches', () => {
  it('returns [] for empty query', () => {
    expect(findSourceMatches('hello', '')).toEqual([])
  })

  it('finds literal case-insensitive matches by default', () => {
    expect(findSourceMatches('Foo foo FOO', 'foo')).toEqual([
      { from: 0, to: 3 },
      { from: 4, to: 7 },
      { from: 8, to: 11 }
    ])
  })

  it('respects caseSensitive', () => {
    expect(
      findSourceMatches('Foo foo FOO', 'foo', { caseSensitive: true })
    ).toEqual([{ from: 4, to: 7 }])
  })

  it('wholeWord skips partials', () => {
    expect(
      findSourceMatches('cat catalog cat', 'cat', { wholeWord: true })
    ).toEqual([
      { from: 0, to: 3 },
      { from: 12, to: 15 }
    ])
  })

  it('regexp mode finds groups and returns [] on invalid pattern', () => {
    expect(findSourceMatches('a1 b2', '\\w(\\d)', { regexp: true })).toEqual([
      { from: 0, to: 2 },
      { from: 3, to: 5 }
    ])
    expect(findSourceMatches('abc', '(', { regexp: true })).toEqual([])
  })

  it('avoids infinite loop on zero-length regexp matches', () => {
    const hits = findSourceMatches('ab', '(?=.)', { regexp: true })
    expect(hits.length).toBeGreaterThan(0)
    expect(hits.length).toBeLessThanOrEqual(3)
  })

  it('regexp wholeWord wraps with boundaries', () => {
    expect(
      findSourceMatches('cat catalog cat', 'cat', {
        regexp: true,
        wholeWord: true
      })
    ).toEqual([
      { from: 0, to: 3 },
      { from: 12, to: 15 }
    ])
  })
})

describe('expandReplace', () => {
  it('expands $& and numbered groups', () => {
    expect(expandReplace('X$&Y', 'hit', [])).toBe('XhitY')
    expect(expandReplace('$2-$1', 'ab', ['a', 'b'])).toBe('b-a')
    expect(expandReplace('$9', 'x', [])).toBe('')
  })
})

describe('replaceAllSource', () => {
  it('replaces all literal matches from end to start', () => {
    const { text, count } = replaceAllSource('aa aa aa', 'aa', 'b')
    expect(text).toBe('b b b')
    expect(count).toBe(3)
  })

  it('supports regexp capture substitution', () => {
    const { text, count } = replaceAllSource('a1 b2', '(\\w)(\\d)', '$2$1', {
      regexp: true
    })
    expect(count).toBe(2)
    expect(text).toBe('1a 2b')
  })

  it('returns original on empty query or invalid regexp', () => {
    expect(replaceAllSource('hi', '', 'x')).toEqual({ text: 'hi', count: 0 })
    expect(replaceAllSource('hi', '(', 'x', { regexp: true })).toEqual({
      text: 'hi',
      count: 0
    })
  })

  it('wholeWord literal replace', () => {
    const { text, count } = replaceAllSource('cat catalog cat', 'cat', 'dog', {
      wholeWord: true
    })
    expect(count).toBe(2)
    expect(text).toBe('dog catalog dog')
  })
})
