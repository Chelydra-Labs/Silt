import { describe, it, expect } from 'vitest'
import { renderChatMarkdown } from './renderChatMarkdown'

describe('renderChatMarkdown', () => {
  it('renders bold and headings', () => {
    const html = renderChatMarkdown('**hello**\n\n# Title')
    expect(html).toContain('<strong>hello</strong>')
    expect(html).toMatch(/<h1[^>]*>Title<\/h1>/)
    expect(html).not.toContain('**hello**')
  })

  it('strips script tags', () => {
    const html = renderChatMarkdown('<script>alert(1)</script>safe')
    expect(html.toLowerCase()).not.toContain('<script')
    expect(html).toContain('safe')
  })

  it('returns empty string for empty input', () => {
    expect(renderChatMarkdown('')).toBe('')
  })
})
