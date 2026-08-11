// Load shipped product-help markdown into sectioned chunks for search_product_docs.

export interface ProductDocSection {
  /** Stable article id from frontmatter (or filename). */
  docId: string
  /** Article title from frontmatter. */
  title: string
  /** ## heading text, or empty for the lead section before the first ##. */
  sectionHeading: string
  /** Section body (no frontmatter). */
  body: string
  /** Synthetic evidence id: help:<docId>#<slug> */
  helpId: string
}

interface ParsedDoc {
  docId: string
  title: string
  body: string
}

const STOP = new Set([
  'a',
  'an',
  'the',
  'and',
  'or',
  'to',
  'of',
  'in',
  'on',
  'for',
  'is',
  'are',
  'be',
  'with',
  'your',
  'you',
  'it',
  'as',
  'at',
  'by',
  'from',
  'this',
  'that',
  'can',
  'how',
  'what',
  'when',
  'do',
  'does',
  'i'
])

/** Tokenize for ranking (lowercase alphanumerics + hyphen). */
export function tokenize(text: string): string[] {
  return text
    .toLowerCase()
    .split(/[^a-z0-9+#._-]+/)
    .filter((t) => t.length > 1 && !STOP.has(t))
}

function slugify(s: string): string {
  const slug = s
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
  return slug || 'section'
}

function parseFrontmatter(raw: string): ParsedDoc {
  const trimmed = raw.replace(/^\uFEFF/, '')
  let docId = ''
  let title = ''
  let body = trimmed
  if (trimmed.startsWith('---')) {
    const end = trimmed.indexOf('\n---', 3)
    if (end !== -1) {
      const fm = trimmed.slice(3, end).trim()
      body = trimmed.slice(end + 4).replace(/^\r?\n/, '')
      for (const line of fm.split(/\r?\n/)) {
        const m = line.match(/^(\w+)\s*:\s*(.+)$/)
        if (!m) continue
        const key = m[1].toLowerCase()
        const val = m[2].trim()
        if (key === 'id') docId = val
        if (key === 'title') title = val
      }
    }
  }
  return { docId, title, body }
}

/** Split markdown body into ## sections (lead content is sectionHeading ''). */
export function splitSections(
  docId: string,
  title: string,
  body: string
): ProductDocSection[] {
  const lines = body.replace(/\r\n/g, '\n').split('\n')
  const sections: { heading: string; lines: string[] }[] = [
    { heading: '', lines: [] }
  ]
  for (const line of lines) {
    const hm = line.match(/^##\s+(.+)$/)
    if (hm) {
      sections.push({ heading: hm[1].trim(), lines: [] })
      continue
    }
    sections[sections.length - 1].lines.push(line)
  }
  const out: ProductDocSection[] = []
  for (const sec of sections) {
    const text = sec.lines.join('\n').trim()
    if (!text && !sec.heading) continue
    const sectionHeading = sec.heading
    const helpSlug = sectionHeading
      ? `${docId}#${slugify(sectionHeading)}`
      : docId
    out.push({
      docId,
      title,
      sectionHeading,
      body: text,
      helpId: `help:${helpSlug}`
    })
  }
  return out.length > 0
    ? out
    : [
        {
          docId,
          title,
          sectionHeading: '',
          body: body.trim(),
          helpId: `help:${docId}`
        }
      ]
}

/**
 * Parse a map of path → raw markdown into searchable sections.
 * Exported for tests; production uses loadProductDocCorpus().
 */
export function buildCorpusFromRaw(
  files: Record<string, string>
): ProductDocSection[] {
  const sections: ProductDocSection[] = []
  const entries = Object.entries(files).sort(([a], [b]) => a.localeCompare(b))
  for (const [path, raw] of entries) {
    const base = path.replace(/^.*[/\\]/, '').replace(/\.md$/i, '')
    if (base.toLowerCase() === 'readme') continue
    const parsed = parseFrontmatter(raw)
    const docId = parsed.docId || base
    const title = parsed.title || docId
    sections.push(...splitSections(docId, title, parsed.body))
  }
  return sections
}

let cached: ProductDocSection[] | null = null

/**
 * Eager-load all product-docs/*.md (except README) via Vite raw imports.
 */
export function loadProductDocCorpus(): ProductDocSection[] {
  if (cached) return cached
  const modules = import.meta.glob('./*.md', {
    query: '?raw',
    import: 'default',
    eager: true
  }) as Record<string, string>
  cached = buildCorpusFromRaw(modules)
  return cached
}

/** Test helper: reset memoized corpus. */
export function resetProductDocCorpusCache(): void {
  cached = null
}
