/**
 * Strip common markdown syntax to readable plain text.
 *
 * Used for short release-notes excerpts (#378) where a raw-markdown preview
 * leaks `**`, `##`, and `[…]()` noise. The full release page is one click
 * away ("View full notes"), so this is a plain-text render — not inline HTML.
 * Introducing a full markdown library (marked/markdown-it) for a 6-line
 * excerpt is a deliberate non-goal; that belongs to a future tier-2
 * enhancement if rich inline rendering is ever wanted.
 *
 * Order-sensitive: links/images are unwrapped before emphasis so that a
 * `[**bold**](url)` label collapses to `bold` rather than `**bold**`, and code
 * fences are stripped before inline-code so the triple backticks are consumed
 * first. Emphasis (`*`/`_`) is arithmetic-safe: a marker is only treated as
 * italics when it is bounded by non-word characters on both sides, so `5*3`,
 * `snake_case`, `$5`, and `5$ cash` are left literal (mirrors the editor's
 * inline-math currency-safe finder).
 */
export function stripMarkdown(input: string): string {
  if (!input) return ''

  let s = input

  // Fenced code blocks: ```lang\n…\n``` → inner content, fences removed.
  s = s.replace(/```[a-zA-Z0-9+-]*\n?([\s\S]*?)```/g, '$1')
  // Inline code: `code` → code.
  s = s.replace(/`([^`\n]+)`/g, '$1')
  // Images: ![alt](url) → alt.
  s = s.replace(/!\[([^\]]*)\]\([^)]*\)/g, '$1')
  // Links: [text](url) → text.
  s = s.replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
  // Bold: **x** / __x__ → x (non-greedy so adjacent runs collapse independently).
  s = s.replace(/\*\*(.+?)\*\*/g, '$1')
  s = s.replace(/__(.+?)__/g, '$1')
  // Strikethrough: ~~x~~ → x.
  s = s.replace(/~~(.+?)~~/g, '$1')
  // Italic: *x* / _x_ → x, only when the marker is word-bounded on both sides
  // (rejects arithmetic `5*3`, underscores in `snake_case`, currency `$5`).
  s = s.replace(/(?<!\w)\*(?!\s)(.+?)(?<!\s)\*(?!\w)/g, '$1')
  s = s.replace(/(?<!\w)_(?!\s)(.+?)(?<!\s)_(?!\w)/g, '$1')
  // Unordered list markers: leading "- ", "* ", "+ " → "• ".
  s = s.replace(/^[ \t]*[-*+][ \t]+/gm, '• ')
  // Blockquote prefix: "> " → "".
  s = s.replace(/^[ \t]*>[ \t]?/gm, '')
  // Heading leading hashes: "## " → "" (defense-in-depth — heading-only lines
  // are already filtered upstream by notesExcerpt, but a mixed line survives).
  s = s.replace(/^[ \t]*#{1,6}[ \t]+/gm, '')

  // Collapse 3+ blank lines to a single blank line; trim outer whitespace.
  return s.replace(/\n{3,}/g, '\n\n').trim()
}
