import { describe, expect, it } from 'vitest'
import { CONTEXT_LINES, diffPageBodies } from './pageDiff'

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

  it('collapses equal runs longer than CONTEXT_LINES', () => {
    const block = Array.from(
      { length: CONTEXT_LINES + 2 },
      (_, i) => `L${i}`
    ).join('\n')
    const d = diffPageBodies(`${block}\n`, `${block}\n`)
    expect(d.hunks[0].collapsed).toBe(true)
    expect(d.hunks[0].hiddenLines).toBeGreaterThan(CONTEXT_LINES)
  })

  it('diffs empty vs text as a single add', () => {
    const d = diffPageBodies('', 'hello\n')
    expect(d.removedLines).toBe(0)
    expect(d.addedLines).toBe(1)
    expect(d.hunks.some((h) => h.kind === 'add')).toBe(true)
  })
})
