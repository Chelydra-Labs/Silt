import { describe, expect, it } from 'vitest'
import {
  cosineSimilarity,
  isDuplicateTask,
  normalizeTaskTitle,
  parseJsonObject,
  stripIdentityComments,
  stripModelPreamble,
  truncateForPrompt
} from './text'

describe('stripIdentityComments', () => {
  it('removes block identity comments', () => {
    const s = 'Hello <!-- id: 7c2a-abcd @ 2026-06-15 --> world'
    expect(stripIdentityComments(s)).toContain('Hello')
    expect(stripIdentityComments(s)).toContain('world')
    expect(stripIdentityComments(s)).not.toMatch(/<!--/)
  })
})

describe('truncateForPrompt', () => {
  it('passes short text through', () => {
    expect(truncateForPrompt('abc', 10)).toEqual({
      text: 'abc',
      truncated: false
    })
  })
  it('mid-truncates long text', () => {
    const r = truncateForPrompt('a'.repeat(100), 20)
    expect(r.truncated).toBe(true)
    expect(r.text.length).toBeLessThanOrEqual(20)
    expect(r.text).toContain('…')
  })
})

describe('stripModelPreamble', () => {
  it('strips outer fences', () => {
    expect(stripModelPreamble('```markdown\n# Hi\n```')).toBe('# Hi')
  })
  it('strips here-is preamble', () => {
    expect(stripModelPreamble("Here's the rewrite:\n\n- a")).toBe('- a')
  })
})

describe('parseJsonObject', () => {
  it('parses clean JSON', () => {
    expect(parseJsonObject('{"tasks":["a"]}')).toEqual({ tasks: ['a'] })
  })
  it('extracts JSON from noise', () => {
    expect(parseJsonObject('Sure.\n{"tags":["work"]}\n')).toEqual({
      tags: ['work']
    })
  })
})

describe('task dedupe helpers', () => {
  it('normalizes checkbox titles', () => {
    expect(normalizeTaskTitle('- [ ] Ship it')).toBe('ship it')
  })
  it('detects duplicates', () => {
    expect(isDuplicateTask('Ship it', ['- [ ] Ship it'])).toBe(true)
    expect(isDuplicateTask('Other', ['Ship it'])).toBe(false)
  })
})

describe('cosineSimilarity', () => {
  it('returns 1 for identical vectors', () => {
    expect(cosineSimilarity([1, 0], [1, 0])).toBeCloseTo(1)
  })
  it('returns 0 for orthogonal', () => {
    expect(cosineSimilarity([1, 0], [0, 1])).toBeCloseTo(0)
  })
})
