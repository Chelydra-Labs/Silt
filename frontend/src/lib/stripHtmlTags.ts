/**
 * Strip well-formed HTML tags for plain-text display (#1153) — display-layer
 * only; stored NOTE content is never modified. Deliberately not folded into
 * `stripMarkdown` (D7): that helper feeds the Settings release-notes excerpt,
 * whose fenced-code/autolink behavior a tag rule would silently change.
 */
export function stripHtmlTags(input: string): string {
  // Pass 1 reads quoted attribute runs as opaque, so a quoted `>` cannot end
  // the tag (title="5 > 3"); pass 2 sweeps openers stranded by an unbalanced
  // quote. Leading [a-zA-Z] spares prose (count < limit); `i<n and j>m` is eaten.
  return input
    .replace(/<\/?[a-zA-Z](?:[^>"']|"[^"]*"|'[^']*')*>/g, '')
    .replace(/<\/?[a-zA-Z][^>]*>/g, '')
}
