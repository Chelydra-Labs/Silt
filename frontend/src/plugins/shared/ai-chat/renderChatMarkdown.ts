/**
 * Render AI chat markdown to sanitized HTML for {@html} in ChatShell.
 * Assistant replies are authored as markdown; user messages stay plain text.
 */
import { marked } from 'marked'
import DOMPurify from 'dompurify'

marked.setOptions({
  gfm: true,
  breaks: true
})

export function renderChatMarkdown(source: string): string {
  if (!source) return ''
  const raw = marked.parse(source, { async: false }) as string
  return DOMPurify.sanitize(raw, {
    USE_PROFILES: { html: true },
    // Keep common markdown output; strip scripts/handlers.
    FORBID_TAGS: ['script', 'style', 'iframe', 'object', 'embed', 'form'],
    FORBID_ATTR: ['style', 'onerror', 'onclick', 'onload']
  })
}
