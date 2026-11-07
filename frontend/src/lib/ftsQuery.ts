// Frontend mirror of the backend's FTS query builder
// (backend/db/search.go: searchTerms + buildFTSQuery). The hub's text filter
// and the AI agent's query_tasks tool build `blocks_fts MATCH` expressions in
// TypeScript, so the two surfaces must normalize terms exactly like the
// global search — otherwise the same words return different result sets.
//
// Semantics (pinned by ftsQuery.test.ts against the Go test table):
//   - split the query on whitespace runs (Go strings.Fields)
//   - keep Unicode letters and decimal digits only (the unicode61 tokenizer's
//     word characters; FTS5 syntax chars are all ASCII punctuation, so they
//     are removed by construction)
//   - drop terms shorter than 2 UTF-8 bytes
//   - quote every term and append `*` for prefix matching
//   - join terms with a space → FTS5 implicit AND

/** True for runes unicode61 treats as word characters (letters/digits). */
const WORD_CHAR = /[\p{L}\p{Nd}]/u

/** UTF-8 byte length, matching Go's `len(string)` for the <2 cutoff. */
function utf8ByteLength(s: string): number {
  let n = 0
  for (const ch of s) {
    const cp = ch.codePointAt(0) ?? 0
    n += cp <= 0x7f ? 1 : cp <= 0x7ff ? 2 : cp <= 0xffff ? 3 : 4
  }
  return n
}

/** Extract the usable MATCH terms from a free-text query. */
export function ftsSearchTerms(query: string): string[] {
  const terms: string[] = []
  for (const word of query.split(/\s+/)) {
    if (!word) continue
    let clean = ''
    for (const ch of word) {
      if (WORD_CHAR.test(ch)) clean += ch
    }
    if (utf8ByteLength(clean) < 2) continue
    terms.push(clean)
  }
  return terms
}

/**
 * Build a safe FTS5 MATCH expression from user input. Returns '' when no term
 * survives normalization — callers treat that as "no text predicate" (the
 * same no-op the backend applies to the body leg).
 */
export function buildFtsMatch(query: string): string {
  const terms = ftsSearchTerms(query)
  if (terms.length === 0) return ''
  return terms.map((t) => `"${t}"*`).join(' ')
}
