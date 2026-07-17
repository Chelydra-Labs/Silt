import { describe, it, expect } from 'vitest'
import {
  injectPlaceholderChips,
  renderTemplatePreview
} from './renderTemplatePreview'

describe('renderTemplatePreview (#663)', () => {
  it('wraps unresolved placeholders as chips', () => {
    const html = injectPlaceholderChips('Hello {{meeting_title}} on {{date}}')
    expect(html).toContain('tpl-placeholder-chip')
    expect(html).toContain('data-placeholder="meeting_title"')
    expect(html).toContain('{{meeting_title}}')
  })

  it('renders headings as HTML', () => {
    const html = renderTemplatePreview('# Agenda\n\n- item one')
    expect(html).toMatch(/<h1[^>]*>Agenda<\/h1>/i)
    expect(html).toMatch(/<li[^>]*>item one<\/li>/i)
  })

  it('keeps placeholder chips after sanitize', () => {
    const html = renderTemplatePreview('Meet {{person}}')
    expect(html).toContain('tpl-placeholder-chip')
    expect(html).toContain('person')
  })
})
