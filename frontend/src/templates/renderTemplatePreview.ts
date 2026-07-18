/**
 * Render a template body for the picker preview (#663).
 * Uses a local Marked instance (not the shared marked singleton) so options
 * do not leak into AI chat or other consumers, then wraps unresolved
 * `{{placeholder}}` tokens as styled chip spans.
 */
import { Marked } from 'marked'
import DOMPurify from 'dompurify'

const previewMarked = new Marked({
  gfm: true,
  breaks: true
})

const PLACEHOLDER_RE = /\{\{([a-z][a-z0-9_]*)\}\}/g

/** Escape HTML for safe insertion into chip markup. */
function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

/**
 * Replace `{{name}}` tokens with chip HTML *before* markdown parse so they
 * survive as inline HTML (marked leaves raw HTML through in GFM mode).
 */
export function injectPlaceholderChips(source: string): string {
  return source.replace(PLACEHOLDER_RE, (_m, name: string) => {
    const safe = escapeHtml(name)
    return `<span class="tpl-placeholder-chip" data-placeholder="${safe}">{{${safe}}}</span>`
  })
}

export function renderTemplatePreview(source: string): string {
  if (!source) return ''
  const withChips = injectPlaceholderChips(source)
  const raw = previewMarked.parse(withChips, { async: false }) as string
  return DOMPurify.sanitize(raw, {
    USE_PROFILES: { html: true },
    ADD_ATTR: ['data-placeholder', 'class'],
    FORBID_TAGS: ['script', 'style', 'iframe', 'object', 'embed', 'form'],
    FORBID_ATTR: ['onerror', 'onclick', 'onload']
  })
}
