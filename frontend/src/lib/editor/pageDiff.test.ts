import { describe, expect, it } from 'vitest'
import {
  COMPARE_MAX_CHARS,
  COMPARE_MAX_LINES,
  CONTEXT_LINES,
  diffPageBodies
} from './pageDiff'

describe('diffPageBodies', () => {
  it('returns a single equal hunk for identical bodies', () => {
    const d = diffPageBodies('# same\nline\n', '# same\nline\n')
    expect(d.addedLines).toBe(0)
    expect(d.removedLines).toBe(0)
    expect(d.hunks).toHaveLength(1)
    expect(d.hunks[0].kind).toBe('equal')
    expect(d.hunks[0].collapsed).toBeUndefined()
  })

  it('counts an inserted line', () => {
    const d = diffPageBodies('a\n', 'a\nb\n')
    expect(d.addedLines).toBe(1)
    expect(d.removedLines).toBe(0)
    expect(
      d.hunks.some((h) => h.kind === 'add' && h.current.includes('b'))
    ).toBe(true)
  })

  it('counts a deleted line', () => {
    const d = diffPageBodies('a\nb\n', 'a\n')
    expect(d.addedLines).toBe(0)
    expect(d.removedLines).toBe(1)
    expect(
      d.hunks.some((h) => h.kind === 'remove' && h.previous.includes('b'))
    ).toBe(true)
  })

  it('marks a replaced line and word-diffs it', () => {
    const d = diffPageBodies('hello world\n', 'hello there\n')
    expect(d.addedLines).toBe(1)
    expect(d.removedLines).toBe(1)
    const rep = d.hunks.find((h) => h.kind === 'replace')
    expect(rep).toBeTruthy()
    expect(rep?.previousWords?.some((w) => w.kind === 'remove')).toBe(true)
    expect(rep?.currentWords?.some((w) => w.kind === 'add')).toBe(true)
  })

  it('treats CRLF as equivalent via stripTrailingCr', () => {
    const d = diffPageBodies('a\r\nb\r\n', 'a\nb\n')
    expect(d.addedLines).toBe(0)
    expect(d.removedLines).toBe(0)
  })

  it('keeps context lines and collapses only the middle of a long equal run', () => {
    const block = Array.from(
      { length: CONTEXT_LINES * 2 + 4 },
      (_, i) => `L${i}`
    ).join('\n')
    const d = diffPageBodies(`${block}\n`, `${block}\n`)
    expect(d.hunks[0].collapsed).toBeUndefined()
    const mid = d.hunks.find((h) => h.collapsed)
    expect(mid?.hiddenLines).toBe(4)
    expect(d.hunks[d.hunks.length - 1].collapsed).toBeUndefined()
  })

  it('skips word-diff on huge replace hunks', () => {
    const prev = `${'a'.repeat(3000)}\n`
    const next = `${'b'.repeat(3000)}\n`
    const d = diffPageBodies(prev, next)
    const rep = d.hunks.find((h) => h.kind === 'replace')
    expect(rep?.previousWords).toBeUndefined()
    expect(rep?.currentWords).toBeUndefined()
  })

  it('marks oversized bodies as too large to compare', () => {
    const huge = 'x'.repeat(COMPARE_MAX_CHARS + 1)
    const d = diffPageBodies(huge, `${huge}y`)
    expect(d.tooLarge).toBe(true)
    expect(d.hunks).toHaveLength(0)
  })

  it('marks high line-count bodies as too large to compare', () => {
    const huge = Array.from(
      { length: COMPARE_MAX_LINES + 1 },
      (_, i) => `L${i}`
    ).join('\n')
    const d = diffPageBodies(huge, `${huge}\nextra`)
    expect(d.tooLarge).toBe(true)
    expect(d.hunks).toHaveLength(0)
  })

  it('diffs empty vs text as a single add', () => {
    const d = diffPageBodies('', 'hello\n')
    expect(d.removedLines).toBe(0)
    expect(d.addedLines).toBe(1)
    expect(d.hunks.some((h) => h.kind === 'add')).toBe(true)
  })
})
