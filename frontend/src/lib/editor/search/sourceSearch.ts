/**
 * Plain-text / regexp find+replace for Source mode textarea buffers (#884).
 * TipTap uses prosemirror-search; Source has no PM doc, so matches are
 * character offsets into the raw markdown string.
 */

export interface SourceMatch {
  from: number
  to: number
}

export interface SourceSearchOptions {
  caseSensitive?: boolean
  wholeWord?: boolean
  regexp?: boolean
}

/** Imperative handle FindBar uses against MarkdownSourceViewer. */
export interface SourceSearchTarget {
  getText: () => string
  getCaret: () => number
  setSelection: (from: number, to: number) => void
  /** Replace [from,to) with text; must mark dirty, push history, schedule save. */
  replaceRange: (from: number, to: number, text: string) => void
  /** Full buffer replace as one history entry (replace-all). */
  setText: (text: string) => void
  /** Subscribe to buffer changes; return unsubscribe. */
  subscribe: (cb: () => void) => () => void
}

function isWordChar(ch: string | undefined): boolean {
  if (!ch) return false
  return /\w/.test(ch)
}

function isWholeWordAt(haystack: string, from: number, to: number): boolean {
  const before = from > 0 ? haystack[from - 1] : undefined
  const after = to < haystack.length ? haystack[to] : undefined
  return !isWordChar(before) && !isWordChar(after)
}

/**
 * Find all matches in haystack. Invalid regexp → []. Empty query → [].
 * Zero-length regexp matches advance by one code unit to avoid infinite loops.
 */
export function findSourceMatches(
  haystack: string,
  query: string,
  opts: SourceSearchOptions = {}
): SourceMatch[] {
  if (!query) return []
  const caseSensitive = opts.caseSensitive ?? false
  const wholeWord = opts.wholeWord ?? false
  const regexp = opts.regexp ?? false
  const out: SourceMatch[] = []

  if (regexp) {
    let re: RegExp
    try {
      const flags = caseSensitive ? 'g' : 'gi'
      const body = wholeWord ? `\\b(?:${query})\\b` : query
      re = new RegExp(body, flags)
    } catch {
      return []
    }
    let m: RegExpExecArray | null
    while ((m = re.exec(haystack)) !== null) {
      const from = m.index
      const to = from + m[0].length
      out.push({ from, to })
      if (m[0].length === 0) {
        // Zero-width match: advance or we loop forever.
        if (re.lastIndex === m.index) re.lastIndex++
        if (re.lastIndex > haystack.length) break
      }
    }
    return out
  }

  // Literal search via indexOf (case-fold when needed).
  const needle = caseSensitive ? query : query.toLowerCase()
  const hay = caseSensitive ? haystack : haystack.toLowerCase()
  let start = 0
  while (start <= hay.length - needle.length) {
    const idx = hay.indexOf(needle, start)
    if (idx === -1) break
    const from = idx
    const to = idx + query.length
    if (!wholeWord || isWholeWordAt(haystack, from, to)) {
      out.push({ from, to })
    }
    start = idx + Math.max(1, query.length)
  }
  return out
}

/** Expand replace string with $& and $1..$9 when regexp mode (basic). */
export function expandReplace(
  replacement: string,
  matchText: string,
  groups: string[]
): string {
  return replacement.replace(/\$(&|\d)/g, (_, token: string) => {
    if (token === '&') return matchText
    const n = Number(token)
    if (n >= 1 && n <= 9) return groups[n - 1] ?? ''
    return `$${token}`
  })
}

function captureGroups(m: RegExpExecArray): string[] {
  const groups: string[] = []
  for (let i = 1; i < m.length; i++) {
    groups.push(m[i] ?? '')
  }
  return groups
}

/**
 * Apply replace-all from end to start so offsets stay valid.
 * Returns new string and number of replacements performed.
 */
export function replaceAllSource(
  haystack: string,
  query: string,
  replacement: string,
  opts: SourceSearchOptions = {}
): { text: string; count: number } {
  if (!query) return { text: haystack, count: 0 }
  const regexp = opts.regexp ?? false

  if (regexp) {
    const caseSensitive = opts.caseSensitive ?? false
    const wholeWord = opts.wholeWord ?? false
    let re: RegExp
    try {
      const flags = caseSensitive ? 'g' : 'gi'
      const body = wholeWord ? `\\b(?:${query})\\b` : query
      re = new RegExp(body, flags)
    } catch {
      return { text: haystack, count: 0 }
    }
    // Collect matches first (with groups), then splice from the end.
    type Hit = { from: number; to: number; text: string; groups: string[] }
    const hits: Hit[] = []
    let m: RegExpExecArray | null
    while ((m = re.exec(haystack)) !== null) {
      const from = m.index
      const to = from + m[0].length
      hits.push({ from, to, text: m[0], groups: captureGroups(m) })
      if (m[0].length === 0) {
        if (re.lastIndex === m.index) re.lastIndex++
        if (re.lastIndex > haystack.length) break
      }
    }
    if (hits.length === 0) return { text: haystack, count: 0 }
    let text = haystack
    for (let i = hits.length - 1; i >= 0; i--) {
      const h = hits[i]
      const rep = expandReplace(replacement, h.text, h.groups)
      text = text.slice(0, h.from) + rep + text.slice(h.to)
    }
    return { text, count: hits.length }
  }

  const matches = findSourceMatches(haystack, query, opts)
  if (matches.length === 0) return { text: haystack, count: 0 }
  let text = haystack
  for (let i = matches.length - 1; i >= 0; i--) {
    const { from, to } = matches[i]
    const matchText = haystack.slice(from, to)
    // Literal mode: $& still expands to the matched text; numbered groups empty.
    const rep = expandReplace(replacement, matchText, [])
    text = text.slice(0, from) + rep + text.slice(to)
  }
  return { text, count: matches.length }
}
