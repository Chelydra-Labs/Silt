import { describe, expect, it } from 'vitest'
import { stripHtmlTags } from './stripHtmlTags'

describe('stripHtmlTags (#1153 comment display)', () => {
  it('strips a simple paired tag, keeping the inner text', () => {
    expect(stripHtmlTags('<b>bold</b>')).toBe('bold')
    expect(stripHtmlTags('a <i>italic</i> b')).toBe('a italic b')
  })

  it('strips nested tags', () => {
    expect(stripHtmlTags('<div><p>hello</p></div>')).toBe('hello')
    expect(stripHtmlTags('<ul><li>one</li><li>two</li></ul>')).toBe('onetwo')
  })

  it('strips tags with attributes', () => {
    expect(stripHtmlTags('<a href="https://x/y" title=\'t\'>label</a>')).toBe(
      'label'
    )
    expect(stripHtmlTags('<br class="x"/>')).toBe('')
    expect(stripHtmlTags('<span data-a=1 data-b=2>text</span>')).toBe('text')
  })

  it('strips tags whose quoted attribute values contain >', () => {
    expect(stripHtmlTags('<div title="5 > 3">hello</div>')).toBe('hello')
    expect(stripHtmlTags("<span data-x='a > b'>t</span>")).toBe('t')
    const hostile = '<img alt="a > b" src=x onerror=alert(1)>'
    expect(stripHtmlTags(hostile)).toBe('')
  })

  it('strips tags whose tag body has an unbalanced quote', () => {
    expect(stripHtmlTags('<div class="foo>text</div>')).toBe('text')
    expect(stripHtmlTags('<a href="broken>click me</a>')).toBe('click me')
    expect(stripHtmlTags('<img src="x onerror=alert(1)>')).toBe('')
    expect(stripHtmlTags("<p class='x>oops</p>")).toBe('oops')
  })

  it('known tradeoff: eats unspaced prose comparisons (i<n and j>m)', () => {
    // Pinned heuristic limitation, not a bug: without surrounding spaces the
    // span reads as a tag. Spaced forms (count < limit, a > b) are spared.
    expect(stripHtmlTags('for i<n and j>m do x')).toBe('for im do x')
  })

  it('strips multi-line block tags, leaving inner text untouched', () => {
    expect(stripHtmlTags('<div>\nfoo\n</div>')).toBe('\nfoo\n')
  })

  it('preserves a lone < that does not open a tag', () => {
    expect(stripHtmlTags('count < limit')).toBe('count < limit')
    expect(stripHtmlTags('if (a < b && b < c) ok')).toBe(
      'if (a < b && b < c) ok'
    )
    expect(stripHtmlTags('5 < 6 > 4')).toBe('5 < 6 > 4')
  })

  it('preserves a bare > in prose', () => {
    expect(stripHtmlTags('a > b')).toBe('a > b')
    expect(stripHtmlTags('pick a > b?')).toBe('pick a > b?')
  })

  it('leaves URLs in prose intact', () => {
    expect(stripHtmlTags('see https://example.com/r/x now')).toBe(
      'see https://example.com/r/x now'
    )
    expect(stripHtmlTags('docs: https://a.b/c?d=e&f=g end')).toBe(
      'docs: https://a.b/c?d=e&f=g end'
    )
  })

  it('leaves &...; entities alone', () => {
    expect(stripHtmlTags('a &amp; b')).toBe('a &amp; b')
    expect(stripHtmlTags('&lt;not a tag&gt;')).toBe('&lt;not a tag&gt;')
    expect(stripHtmlTags('fish &chips;')).toBe('fish &chips;')
  })

  it('is idempotent', () => {
    const inputs = [
      '<b>ship it</b>',
      '<div>Please <b>review</b> the PR</div>',
      'count < limit and a > b',
      'x <img src=y> z <script>s()</script>'
    ]
    for (const input of inputs) {
      const once = stripHtmlTags(input)
      expect(stripHtmlTags(once)).toBe(once)
    }
  })

  it('neutralizes hostile payloads: no tag syntax survives', () => {
    expect(stripHtmlTags('<img src=x onerror=alert(1)>')).toBe('')
    expect(stripHtmlTags('<script>alert(1)</script>')).toBe('alert(1)')
    const combined = stripHtmlTags(
      'hi <img src=x onerror=alert(1)> there <script>alert(1)</script>.'
    )
    // Implementation-independent oracle: no markup delimiter or payload
    // keyword survives at all.
    expect(combined).not.toContain('<')
    expect(combined).not.toContain('onerror')
    expect(combined).not.toContain('script')
    expect(combined).toBe('hi  there alert(1).')
  })
})
