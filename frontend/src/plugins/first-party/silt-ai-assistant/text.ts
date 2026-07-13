// Shared text helpers for Writing Assistant.

/** Strip block identity comments so models cannot corrupt them. */
export function stripIdentityComments(text: string): string {
  return text
    .replace(/<!--\s*id:\s*[^\n>]+-->/gi, '')
    .replace(/[ \t]+\n/g, '\n')
    .trim()
}

/** Mid-truncate: head + ellipsis + tail (same idea as AI Summary). */
export function truncateForPrompt(
  content: string,
  maxChars: number
): {
  text: string
  truncated: boolean
} {
  if (content.length <= maxChars) return { text: content, truncated: false }
  const tail = Math.floor(maxChars / 4)
  const head = maxChars - tail - 1
  return {
    text: content.slice(0, head) + '…' + content.slice(content.length - tail),
    truncated: true
  }
}

/** Strip common model preambles and outer fences. */
export function stripModelPreamble(raw: string): string {
  let s = raw.trim()
  // Drop leading "Here is..." style lines (one or two).
  s = s.replace(/^(?:here(?:'s| is| are)[^\n]*\n+)+/i, '')
  // Outer markdown fence
  const fenced = s.match(/^```(?:markdown|md|json)?\s*\n([\s\S]*?)\n```\s*$/i)
  if (fenced) s = fenced[1].trim()
  return s.trim()
}

/** Tolerant JSON object extract from model output. */
export function parseJsonObject(raw: string): Record<string, unknown> | null {
  const cleaned = stripModelPreamble(raw)
  try {
    const v = JSON.parse(cleaned)
    if (v && typeof v === 'object' && !Array.isArray(v)) {
      return v as Record<string, unknown>
    }
  } catch {
    /* fall through */
  }
  const start = cleaned.indexOf('{')
  const end = cleaned.lastIndexOf('}')
  if (start >= 0 && end > start) {
    try {
      const v = JSON.parse(cleaned.slice(start, end + 1))
      if (v && typeof v === 'object' && !Array.isArray(v)) {
        return v as Record<string, unknown>
      }
    } catch {
      return null
    }
  }
  return null
}

export function normalizeTaskTitle(title: string): string {
  return title
    .replace(/^[-*+]\s*\[[ xX/\-]?\]\s*/, '')
    .replace(/^#+\s*/, '')
    .replace(/\s+/g, ' ')
    .trim()
    .toLowerCase()
}

export function isDuplicateTask(
  title: string,
  existing: string[] | undefined
): boolean {
  if (!existing?.length) return false
  const n = normalizeTaskTitle(title)
  if (!n) return true
  return existing.some((e) => normalizeTaskTitle(e) === n)
}

/** Cosine similarity for equal-length vectors. */
export function cosineSimilarity(a: number[], b: number[]): number {
  if (a.length === 0 || a.length !== b.length) return 0
  let dot = 0
  let na = 0
  let nb = 0
  for (let i = 0; i < a.length; i++) {
    dot += a[i] * b[i]
    na += a[i] * a[i]
    nb += b[i] * b[i]
  }
  const d = Math.sqrt(na) * Math.sqrt(nb)
  return d === 0 ? 0 : dot / d
}
