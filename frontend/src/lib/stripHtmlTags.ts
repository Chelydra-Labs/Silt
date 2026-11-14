/**
 * Strip well-formed HTML tags for plain-text display (#1153) — display-layer
 * only; stored NOTE content is never modified. Deliberately not folded into
 * `stripMarkdown` (D7): that helper feeds the Settings release-notes excerpt,
 * whose fenced-code/autolink behavior a tag rule would silently change.
 */
export function stripHtmlTags(input: string): string {
  // Non-element forms go first because their bodies may contain `>`; the lazy
  // body keeps `<!-- a > b -->` whole. An unterminated `<!--` or CDATA opener
  // stays as typed — there is no close delimiter to anchor on.
  return (
    input
      .replace(/<!--[\s\S]*?-->/g, '')
      .replace(/<!\[CDATA\[([\s\S]*?)\]\]>/g, '$1')
      .replace(/<!doctype[^>]*>/gi, '')
      // Pass 1 reads quoted runs (straight or typographic) as opaque, so a
      // quoted `>` cannot end the tag; pass 2 sweeps openers stranded by an
      // unbalanced quote. Leading [a-zA-Z] spares prose (count < limit).
      .replace(
        /<\/?[a-zA-Z](?:[^>"'\u201C\u201D\u2018\u2019]|"[^"]*"|'[^']*'|\u201C[^\u201C\u201D]*\u201D|\u2018[^\u2018\u2019]*\u2019)*>/g,
        ''
      )
      .replace(/<\/?[a-zA-Z][^>]*>/g, '')
  )
}
