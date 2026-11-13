/**
 * Strip well-formed HTML tags for plain-text display (#1153) — display-layer
 * only; stored NOTE content is never modified. Deliberately not folded into
 * `stripMarkdown` (D7): that helper feeds the Settings release-notes excerpt,
 * whose fenced-code/autolink behavior a tag rule would silently change.
 */
export function stripHtmlTags(input: string): string {
  // The leading [a-zA-Z] spares stray `<` (count < limit) and bare `>` (a > b);
  // quoted attribute runs are opaque, so a quoted `>` (title="5 > 3") cannot
  // end the tag. Tradeoff: unspaced prose like `i<n and j>m` is eaten.
  return input.replace(/<\/?[a-zA-Z](?:[^>"']|"[^"]*"|'[^']*')*>/g, '')
}
