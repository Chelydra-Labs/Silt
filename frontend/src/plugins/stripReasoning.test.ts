import { describe, expect, it } from 'vitest'
import { stripReasoningContent } from './stripReasoning'

describe('stripReasoningContent', () => {
  describe('well-formed blocks', () => {
    it('removes a leading block and preserves trailing prose exactly (AC3)', () => {
      expect(
        stripReasoningContent('<thought>reasoning</thought>final prose answer')
      ).toBe('final prose answer')
    })
    it('removes the canonical <think> tag', () => {
      expect(
        stripReasoningContent('<think>Let me analyze</think>Final answer')
      ).toBe('Final answer')
    })
    it('removes a mid-string block, joining the surrounding text', () => {
      expect(stripReasoningContent('Hello<think>r</think>World')).toBe(
        'HelloWorld'
      )
    })
    it('removes multi-line block content', () => {
      const input = '<think>line one\nline two\n* bullet</think>visible'
      expect(stripReasoningContent(input)).toBe('visible')
    })
    it('removes multiple sequential blocks', () => {
      expect(
        stripReasoningContent('<think>r1</think>A<think>r2</think>B')
      ).toBe('AB')
    })
  })

  describe('tag variants', () => {
    const cases: Array<[string, string]> = [
      ['<think>x</think>Out', 'Out'],
      ['<thinking>x</thinking>Out', 'Out'],
      ['<thought>x</thought>Out', 'Out'],
      ['<reasoning>x</reasoning>Out', 'Out'],
      ['<antthinking>x</antthinking>Out', 'Out'],
      ['<antml:thinking>x</antml:thinking>Out', 'Out'],
      ['<mm:think>x</mm:think>Out', 'Out']
    ]
    for (const [input, expected] of cases) {
      it(`handles ${input.split('>')[0]}>`, () => {
        expect(stripReasoningContent(input)).toBe(expected)
      })
    }
    it('is case-insensitive', () => {
      expect(stripReasoningContent('<Think>x</THINK>Out')).toBe('Out')
      expect(stripReasoningContent('<THOUGHT>x</thought>Out')).toBe('Out')
    })
    it('does not match lookalike tag names (<thinker>, <thoughtful>)', () => {
      expect(stripReasoningContent('<thinker>x</thinker>ok')).toBe(
        '<thinker>x</thinker>ok'
      )
      expect(stripReasoningContent('<thoughtful>ok</thoughtful>')).toBe(
        '<thoughtful>ok</thoughtful>'
      )
    })
    it('tolerates trailing whitespace inside the tag', () => {
      expect(stripReasoningContent('<think >x</think >Out')).toBe('Out')
    })
    it('treats mismatched open/close variants as one balanced block', () => {
      // A model bridging variants; the content is still reasoning. Depth counting
      // pairs any open with any close, so <think>r</thinking> drops r.
      expect(stripReasoningContent('<think>r</thinking>Out')).toBe('Out')
    })
  })

  describe('edge cases', () => {
    it('drops an unclosed opening tag through end-of-string (truncated generation)', () => {
      expect(stripReasoningContent('<think>reasoning that never closes')).toBe(
        ''
      )
    })
    it('preserves prose before an unclosed opening tag', () => {
      // The answer came first, then the model started thinking and got cut.
      expect(
        stripReasoningContent('real answer<think>truncated reasoning')
      ).toBe('real answer')
    })
    it('removes a stray closing tag with no opener (Qwen3 quirk)', () => {
      expect(stripReasoningContent('prose</think>more')).toBe('prosemore')
    })
    it('handles nested blocks (outer block wins, inner content dropped)', () => {
      expect(
        stripReasoningContent('<think>a<think>b</think>c</think>Out')
      ).toBe('Out')
    })
    it('collapses blank-line seams left by a removed block', () => {
      expect(stripReasoningContent('<think>r</think>\n\n\nfinal')).toBe('final')
      expect(stripReasoningContent('<think>r</think>\n\nfinal')).toBe('final')
    })
    it('trims leading/trailing whitespace exposed by removal', () => {
      expect(stripReasoningContent('   <think>r</think>   final   ')).toBe(
        'final'
      )
    })
  })

  describe('the realistic issue #483 example', () => {
    it('leaves only the polished prose', () => {
      const leaked =
        '<thought>* Input: … * Draft 1: … * 2-3 sentences? Yes …</thought>The current method of mapping backend errors…'
      expect(stripReasoningContent(leaked)).toBe(
        'The current method of mapping backend errors…'
      )
    })
    it('cleans a thought block wrapping a JSON object', () => {
      // A reasoning model planning before the structured output. After stripping,
      // the JSON is intact for downstream parsing.
      const leaked =
        '<think>planning the extraction</think>{"summary":"s","tasks":["t"]}'
      expect(stripReasoningContent(leaked)).toBe(
        '{"summary":"s","tasks":["t"]}'
      )
    })
  })

  describe('no-op / idempotence', () => {
    it('returns tag-free content unchanged (apart from trim)', () => {
      expect(stripReasoningContent('just normal text')).toBe('just normal text')
      expect(stripReasoningContent('{"summary":"clean"}')).toBe(
        '{"summary":"clean"}'
      )
    })
    it('is idempotent (running twice equals once)', () => {
      const once = stripReasoningContent('<think>r</think>final')
      expect(stripReasoningContent(once)).toBe(once)
      const mixed = '<think>a</think>text<think>b</think>'
      expect(stripReasoningContent(stripReasoningContent(mixed))).toBe(
        stripReasoningContent(mixed)
      )
    })
    it('returns falsy input unchanged', () => {
      expect(stripReasoningContent('')).toBe('')
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      expect(stripReasoningContent(undefined as any)).toBe(undefined)
    })
  })
})
