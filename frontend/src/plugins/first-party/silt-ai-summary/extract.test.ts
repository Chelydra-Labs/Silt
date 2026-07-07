import { describe, expect, it, vi } from 'vitest'
import {
  buildSummaryPrompt,
  extractJsonObject,
  extractSummary,
  parseSummary,
  providerError,
  truncateForPrompt
} from './extract'
import { DEFAULT_SETTINGS } from './settings'
import type { SummarySettings } from './types'

const settings = { ...DEFAULT_SETTINGS, facets: { ...DEFAULT_SETTINGS.facets } }

describe('truncateForPrompt', () => {
  it('passes content under the limit through untouched', () => {
    const content = 'x'.repeat(100)
    expect(truncateForPrompt(content, 200)).toBe(content)
  })
  it('keeps a head + tail with an ellipsis seam when oversized', () => {
    const content = 'H' + 'a'.repeat(40) + 'TAIL' // 45 chars
    const out = truncateForPrompt(content, 20)
    expect(out.length).toBeLessThanOrEqual(20)
    expect(out.startsWith('H')).toBe(true)
    expect(out.endsWith('TAIL')).toBe(true)
    expect(out).toContain('…')
  })
})

describe('buildSummaryPrompt', () => {
  it('emits a system + user message and forbids markdown fences in the schema text', () => {
    const msgs = buildSummaryPrompt('the note', settings)
    expect(msgs).toHaveLength(2)
    expect(msgs[0].role).toBe('system')
    expect(msgs[1].role).toBe('user')
    expect(msgs[1].content).toBe('the note')
    const sys = msgs[0].content
    // The schema keys must be named; "only JSON" instruction must be present.
    expect(sys).toContain('"summary"')
    expect(sys).toContain('"tasks"')
    expect(sys).toContain('"risks"')
    expect(sys).toContain('"decisions"')
    expect(sys.toLowerCase()).toContain('only')
    expect(sys).toContain('[]')
  })
  it('length hint reflects summary_length', () => {
    const short = buildSummaryPrompt('n', { ...settings, summary_length: 'short' })[0].content
    expect(short).toContain('2 concise sentences')
    const long = buildSummaryPrompt('n', { ...settings, summary_length: 'long' })[0].content
    expect(long).toContain('3–4 sentences')
  })
  it('truncates oversized note content in the user message', () => {
    const big = 'H' + 'x'.repeat(200) + 'TAIL'
    const msgs = buildSummaryPrompt(big, { ...settings, max_note_chars: 50 })
    expect(msgs[1].content.length).toBeLessThanOrEqual(50)
  })
})

describe('extractJsonObject', () => {
  it('returns the balanced object substring', () => {
    expect(extractJsonObject('noise {"a":1} trailing')).toBe('{"a":1}')
  })
  it('handles nested braces', () => {
    expect(extractJsonObject('{"a":{"b":2}}')).toBe('{"a":{"b":2}}')
  })
  it('ignores braces inside string values', () => {
    expect(extractJsonObject('{"a":"}"}')).toBe('{"a":"}"}')
  })
  it('strips ```json fences', () => {
    expect(extractJsonObject('```json\n{"a":1}\n```')).toBe('{"a":1}')
  })
  it('returns empty when no object is present', () => {
    expect(extractJsonObject('no json here')).toBe('')
  })
  it('returns empty on unbalanced braces', () => {
    expect(extractJsonObject('{"a":1')).toBe('')
  })
})

describe('parseSummary', () => {
  it('parses a well-formed object', () => {
    expect(parseSummary('{"summary":"s","tasks":["t"],"risks":[],"decisions":[]}')).toEqual({
      summary: 's',
      tasks: ['t'],
      risks: [],
      decisions: []
    })
  })
  it('strips fences and surrounding prose', () => {
    expect(parseSummary('Here you go:\n```json\n{"summary":"s"}\n```')).toEqual({
      summary: 's',
      tasks: [],
      risks: [],
      decisions: []
    })
  })
  it('coerces a bare-string facet into a single-element array', () => {
    expect(parseSummary('{"summary":"s","tasks":"one task"}')).toEqual({
      summary: 's',
      tasks: ['one task'],
      risks: [],
      decisions: []
    })
  })
  it('drops non-string / empty facet entries', () => {
    const parsed = parseSummary('{"summary":"s","tasks":["ok","",42]}')
    expect(parsed).not.toBeNull()
    expect(parsed?.tasks).toEqual(['ok'])
  })
  it('returns null when summary is missing or empty', () => {
    expect(parseSummary('{"tasks":["t"]}')).toBeNull()
    expect(parseSummary('{"summary":"  "}')).toBeNull()
  })
  it('returns null on no JSON', () => {
    expect(parseSummary('totally not json')).toBeNull()
  })
})

describe('providerError', () => {
  it('maps any thrown value to a provider-error SummaryError', () => {
    const e = providerError({ code: 'server', message: 'boom' })
    expect(e.code).toBe('provider-error')
    expect(e.message).toBe('boom')
  })
  it('falls back to a generic message when none is present', () => {
    const e = providerError({})
    expect(e.code).toBe('provider-error')
    expect(e.message).toMatch(/failed/i)
  })
})

describe('extractSummary', () => {
  const ok = (content: string, model = 'test-model') =>
    vi.fn().mockResolvedValue({ content, model })

  it('parses a first-shot JSON response', async () => {
    const complete = ok('{"summary":"s","tasks":["t"]}')
    const r = await extractSummary({ complete, content: 'note', settings })
    expect(r.ok).toBe(true)
    if (r.ok) {
      expect(r.extraction.summary).toBe('s')
      expect(r.extraction.tasks).toEqual(['t'])
      expect(r.model).toBe('test-model')
    }
    expect(complete).toHaveBeenCalledTimes(1)
  })

  it('retries once on a parse failure, then succeeds', async () => {
    const complete = vi
      .fn()
      .mockResolvedValueOnce({ content: 'garbage', model: 'm' })
      .mockResolvedValueOnce({ content: '{"summary":"ok"}', model: 'm' })
    const r = await extractSummary({ complete, content: 'note', settings })
    expect(r.ok).toBe(true)
    expect(complete).toHaveBeenCalledTimes(2)
  })

  it('falls back to a prose-only summary after two parse failures', async () => {
    const complete = vi
      .fn()
      .mockResolvedValueOnce({ content: 'garbage1', model: 'm' })
      .mockResolvedValueOnce({ content: 'garbage2', model: 'm' })
      .mockResolvedValueOnce({ content: 'A clean two-sentence summary.', model: 'm' })
    const r = await extractSummary({ complete, content: 'note', settings })
    expect(r.ok).toBe(true)
    if (r.ok) {
      expect(r.extraction.summary).toBe('A clean two-sentence summary.')
      expect(r.extraction.tasks).toEqual([])
      expect(r.extraction.risks).toEqual([])
      expect(r.extraction.decisions).toEqual([])
    }
    // structured attempt + retry + prose fallback = 3 calls.
    expect(complete).toHaveBeenCalledTimes(3)
  })

  it('returns a provider-error when the first call rejects', async () => {
    const complete = vi.fn().mockRejectedValue({ code: 'server', message: 'down' })
    const r = await extractSummary({ complete, content: 'note', settings })
    expect(r.ok).toBe(false)
    if (!r.ok) expect(r.error.code).toBe('provider-error')
    expect(complete).toHaveBeenCalledTimes(1)
  })

  it('returns a provider-error when the prose fallback returns empty', async () => {
    const complete = vi
      .fn()
      .mockResolvedValueOnce({ content: 'garbage', model: 'm' })
      .mockResolvedValueOnce({ content: 'garbage', model: 'm' })
      .mockResolvedValueOnce({ content: '   ', model: 'm' })
    const r = await extractSummary({ complete, content: 'note', settings })
    expect(r.ok).toBe(false)
    if (!r.ok) expect(r.error.code).toBe('provider-error')
  })
})
